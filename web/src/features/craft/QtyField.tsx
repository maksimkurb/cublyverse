import { Button, TextField } from "../../ui/controls";
import { stepQty } from "./format";

/** The weights beads are usually bought and used by. */
const GRAM_STEPS = [-50, -10, -5, 5, 10, 50];

/**
 * A quantity with − and + beside it; grams also get buttons for the usual
 * weights. Nothing is saved until the form is.
 */
export function QtyField({
  label,
  unit,
  value,
  disabled,
  onChange,
}: {
  label: string;
  unit: string;
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const step = (delta: number) => onChange(stepQty(value, delta));
  return (
    <div className="qty-field">
      <div className="qty-field__row">
        <button type="button" className="cv-iconbtn qty-field__step" aria-label="Уменьшить на 1" disabled={disabled} onClick={() => step(-1)}>
          −
        </button>
        <TextField label={label} inputMode="decimal" mono disabled={disabled} value={value} onChange={(event) => onChange(event.target.value)} />
        <button type="button" className="cv-iconbtn qty-field__step" aria-label="Увеличить на 1" disabled={disabled} onClick={() => step(1)}>
          +
        </button>
      </div>
      {unit === "г" && (
        <div className="qty-field__chips" role="group" aria-label="Изменить на несколько граммов">
          {GRAM_STEPS.map((delta) => (
            <Button key={delta} size="s" disabled={disabled} onClick={() => step(delta)}>
              {delta > 0 ? `+${delta}` : `−${-delta}`}
            </Button>
          ))}
        </div>
      )}
    </div>
  );
}
