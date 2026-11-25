import { useState } from "react";

import type { CraftItem } from "../../api/client";
import { useHostBackButton } from "../../host/HostContext";
import { Sheet } from "../../ui/controls";
import { swatchStyle } from "./format";

type SwatchItem = Pick<CraftItem, "color_hex"> & Partial<Pick<CraftItem, "image_url" | "source_url" | "name">>;

/**
 * The colour of an item: the manufacturer's photo when there is one (a single
 * HEX cannot show a transparent or lustrous bead), else the HEX, else a hatched
 * square that says the colour is unknown. With `zoomable` a photo opens large.
 */
export function Swatch({ item, zoomable }: { item: SwatchItem; zoomable?: boolean }) {
  const [broken, setBroken] = useState(false);
  const [open, setOpen] = useState(false);

  if (item.image_url && !broken) {
    const photo = (
      <img src={item.image_url} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setBroken(true)} />
    );
    if (!zoomable) {
      return (
        <span className="craft-swatch craft-swatch--photo" aria-hidden="true">
          {photo}
        </span>
      );
    }
    return (
      <>
        <button
          type="button"
          className="craft-swatch craft-swatch--photo"
          aria-label={`Фото: ${item.name ?? "позиция"}`}
          onClick={() => setOpen(true)}
        >
          {photo}
        </button>
        {open && <PhotoSheet item={item} onClose={() => setOpen(false)} />}
      </>
    );
  }
  return (
    <span
      className={item.color_hex ? "craft-swatch" : "craft-swatch craft-swatch--unknown"}
      style={swatchStyle(item)}
      aria-hidden="true"
    />
  );
}

function PhotoSheet({ item, onClose }: { item: SwatchItem; onClose: () => void }) {
  useHostBackButton(onClose);
  return (
    <Sheet title={item.name ?? "Фото"} onClose={onClose}>
      <div className="cv-form">
        <img className="craft-photo" src={item.image_url} alt={item.name ?? ""} referrerPolicy="no-referrer" />
        {item.source_url && (
          <a href={item.source_url} target="_blank" rel="noopener noreferrer">
            Страница производителя
          </a>
        )}
      </div>
    </Sheet>
  );
}
