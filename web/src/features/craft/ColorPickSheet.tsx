import { useRef, useState, type ChangeEvent, type PointerEvent } from "react";

import { useHostBackButton } from "../../host/HostContext";
import { Button, Notice, Sheet, TextField } from "../../ui/controls";
import { medianColor } from "./color";
import { normalizeHex, swatchStyle } from "./format";

/** Longest side of the picture kept for picking; more only costs memory. */
const MAX_SIDE = 1600;
/** Size of the magnifier and how much it enlarges. */
const LOUPE = 112;
const ZOOM = 4;
/** Radius, in screen pixels, of the patch whose colour is taken. */
const PATCH = 3;

/**
 * Choosing a colour: tap a point of a photo from the phone (an eyedropper), use
 * the system palette or type a HEX. The photo is read in the browser and is
 * never uploaded.
 */
export function ColorPickSheet({
  initial,
  action,
  onPick,
  onClose,
}: {
  initial: string;
  /** Label of the confirming button. */
  action: string;
  onPick: (hex: string) => void;
  onClose: () => void;
}) {
  useHostBackButton(onClose);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const loupeRef = useRef<HTMLCanvasElement>(null);
  const [value, setValue] = useState(initial);
  const [hasPhoto, setHasPhoto] = useState(false);
  const [point, setPoint] = useState<{ x: number; y: number; below: boolean } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const hex = normalizeHex(value) || null;

  async function handleFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    // The same file can be chosen again.
    event.target.value = "";
    const canvas = canvasRef.current;
    if (!file || !canvas) {
      return;
    }
    const url = URL.createObjectURL(file);
    try {
      const image = new Image();
      image.src = url;
      await image.decode();
      const scale = Math.min(1, MAX_SIDE / Math.max(image.naturalWidth, image.naturalHeight));
      canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
      canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
      canvas.getContext("2d", { willReadFrequently: true })!.drawImage(image, 0, 0, canvas.width, canvas.height);
      setError(null);
      setPoint(null);
      setHasPhoto(true);
    } catch {
      setError("Не получилось открыть это фото. Попробуйте другое или сделайте снимок экрана.");
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  function sample(event: PointerEvent<HTMLCanvasElement>) {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) {
      return;
    }
    const ratio = canvas.width / rect.width;
    const clamp = (n: number, max: number) => Math.min(Math.max(n, 0), max);
    const shownX = clamp(event.clientX - rect.left, rect.width - 1);
    const shownY = clamp(event.clientY - rect.top, rect.height - 1);
    const x = Math.floor(shownX * ratio);
    const y = Math.floor(shownY * ratio);
    const radius = Math.max(1, Math.round(PATCH * ratio));
    const left = clamp(x - radius, canvas.width - 1);
    const top = clamp(y - radius, canvas.height - 1);
    const patch = canvas
      .getContext("2d")!
      .getImageData(left, top, Math.min(radius * 2 + 1, canvas.width - left), Math.min(radius * 2 + 1, canvas.height - top));
    setValue(medianColor(patch.data));

    const loupe = loupeRef.current!.getContext("2d")!;
    const source = (LOUPE / ZOOM) * ratio;
    loupe.imageSmoothingEnabled = false;
    loupe.clearRect(0, 0, LOUPE, LOUPE);
    loupe.drawImage(canvas, x - source / 2, y - source / 2, source, source, 0, 0, LOUPE, LOUPE);
    // A two-tone ring marks the sampled patch on light and dark photos alike.
    for (const [color, width] of [["#000", 3], ["#fff", 1]] as const) {
      loupe.strokeStyle = color;
      loupe.lineWidth = width;
      loupe.beginPath();
      loupe.arc(LOUPE / 2, LOUPE / 2, PATCH * ZOOM, 0, 2 * Math.PI);
      loupe.stroke();
    }
    // The magnifier stays above the finger, and goes below it near the top.
    setPoint({ x: canvas.offsetLeft + shownX, y: canvas.offsetTop + shownY, below: shownY < LOUPE + 24 });
  }

  return (
    <Sheet title="Выбор цвета" onClose={onClose}>
      <div className="cv-form">
        {error && <Notice tone="error">{error}</Notice>}
        <label className="cv-btn cv-btn--outline cv-btn--block color-pick__file">
          {hasPhoto ? "Другое фото" : "Взять цвет с фото"}
          <input type="file" accept="image/*" onChange={(event) => void handleFile(event)} />
        </label>
        <div className="color-pick__stage" hidden={!hasPhoto}>
          <canvas
            ref={canvasRef}
            className="color-pick__photo"
            role="img"
            aria-label="Фото: коснитесь нужного цвета"
            onPointerDown={(event) => {
              event.currentTarget.setPointerCapture(event.pointerId);
              sample(event);
            }}
            onPointerMove={(event) => {
              if (event.currentTarget.hasPointerCapture(event.pointerId)) {
                sample(event);
              }
            }}
          />
          <canvas
            ref={loupeRef}
            className="color-pick__loupe"
            width={LOUPE}
            height={LOUPE}
            hidden={!point}
            aria-hidden="true"
            style={point ? { left: point.x, top: point.y + (point.below ? 36 : -36 - LOUPE), borderColor: hex ?? undefined } : undefined}
          />
        </div>
        <p className="cv-field__hint">
          {hasPhoto ? "Коснитесь нужного цвета и при необходимости ведите пальцем." : "Фото остаётся на устройстве и никуда не отправляется."}
        </p>
        <div className="craft-color">
          <TextField label="HEX" placeholder="#RRGGBB" mono value={value} onChange={(event) => setValue(event.target.value)} />
          <input
            type="color"
            className="color-pick__palette"
            aria-label="Палитра"
            value={(hex ?? "#808080").toLowerCase()}
            onChange={(event) => setValue(event.target.value.toUpperCase())}
          />
          <span
            className={hex ? "craft-swatch craft-swatch--large" : "craft-swatch craft-swatch--large craft-swatch--unknown"}
            style={swatchStyle({ color_hex: hex ?? "" })}
            aria-hidden="true"
          />
        </div>
        <Button variant="primary" block disabled={!hex} onClick={() => hex && onPick(hex)}>
          {action}
        </Button>
      </div>
    </Sheet>
  );
}
