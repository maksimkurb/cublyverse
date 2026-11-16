import {
  useId,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";
import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Dialog } from "@base-ui/react/dialog";
import { Switch as BaseSwitch } from "@base-ui/react/switch";

import { IconCheck, IconClose } from "./icons";

/**
 * The Cublyverse UI kit: Base UI primitives for behaviour (dialog, switch,
 * checkbox) and native elements for everything else, styled by `styles.css`.
 */

function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export type ButtonVariant = "primary" | "solid" | "outline" | "danger" | "ghost";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** `primary` is the one lime action of a screen. */
  variant?: ButtonVariant;
  size?: "m" | "s";
  /** Stretches to the container width. */
  block?: boolean;
  /** Disables the button and marks it busy. */
  loading?: boolean;
}

export function Button({
  variant = "outline",
  size = "m",
  block,
  loading,
  className,
  disabled,
  type = "button",
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={cx("cv-btn", `cv-btn--${variant}`, size === "s" && "cv-btn--s", block && "cv-btn--block", className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {children}
    </button>
  );
}

function FieldShell({ id, label, hint, children }: { id: string; label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="cv-field">
      <label className="cv-field__label" htmlFor={id}>
        {label}
      </label>
      {children}
      {hint && <p className="cv-field__hint">{hint}</p>}
    </div>
  );
}

interface FieldProps {
  label: string;
  hint?: string;
  /** Monospace digits: times, numbers, tokens. */
  mono?: boolean;
}

export function TextField({ label, hint, mono, className, ...rest }: FieldProps & InputHTMLAttributes<HTMLInputElement>) {
  const id = useId();
  return (
    <FieldShell id={id} label={label} hint={hint}>
      <input id={id} className={cx("cv-input", mono && "cv-input--mono", className)} {...rest} />
    </FieldShell>
  );
}

export function TextAreaField({
  label,
  hint,
  className,
  ...rest
}: FieldProps & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const id = useId();
  return (
    <FieldShell id={id} label={label} hint={hint}>
      <textarea id={id} className={cx("cv-input cv-input--area", className)} {...rest} />
    </FieldShell>
  );
}

export function SelectField({
  label,
  hint,
  className,
  children,
  ...rest
}: FieldProps & SelectHTMLAttributes<HTMLSelectElement>) {
  const id = useId();
  return (
    <FieldShell id={id} label={label} hint={hint}>
      <select id={id} className={cx("cv-input cv-input--select", className)} {...rest}>
        {children}
      </select>
    </FieldShell>
  );
}

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label": string;
}

export function Switch({ checked, onCheckedChange, disabled, ...rest }: SwitchProps) {
  return (
    <BaseSwitch.Root
      className="cv-switch"
      checked={checked}
      disabled={disabled}
      onCheckedChange={(next) => onCheckedChange(next)}
      {...rest}
    >
      <BaseSwitch.Thumb className="cv-switch__thumb" />
    </BaseSwitch.Root>
  );
}

export interface CheckboxProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}

export function Checkbox({ checked, onCheckedChange }: CheckboxProps) {
  return (
    <BaseCheckbox.Root className="cv-check" checked={checked} onCheckedChange={(next) => onCheckedChange(next)}>
      <BaseCheckbox.Indicator className="cv-check__mark">
        <IconCheck size={14} />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}

/** A choice between a few options, shown as one segmented bar. */
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: ReadonlyArray<{ value: T; label: string }>;
  onChange: (value: T) => void;
}) {
  return (
    <div className="cv-segmented" role="group" aria-label={label}>
      {options.map((option) => (
        <button
          type="button"
          key={option.value}
          className="cv-segmented__item"
          aria-pressed={option.value === value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

/** An inline message: `error` announces itself to assistive technology. */
export function Notice({
  tone = "info",
  title,
  children,
}: {
  tone?: "info" | "error";
  title?: string;
  children: ReactNode;
}) {
  return (
    <div className={cx("cv-notice", tone === "error" && "cv-notice--error")} role={tone === "error" ? "alert" : undefined}>
      {title && <strong className="cv-notice__title">{title}</strong>}
      <span>{children}</span>
    </div>
  );
}

/** What a list shows when it has nothing to show. */
export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="cv-empty">
      <p className="cv-empty__title">{title}</p>
      {children && <p className="cv-empty__text">{children}</p>}
    </div>
  );
}

/**
 * Modal sheet: slides from the bottom on a phone, sits in the centre on a wide
 * screen. Base UI owns focus trapping, Escape and the backdrop.
 */
export function Sheet({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="cv-sheet__backdrop" />
        <Dialog.Popup className="cv-sheet">
          <header className="cv-sheet__head">
            <Dialog.Title className="cv-sheet__title">{title}</Dialog.Title>
            <Dialog.Close className="cv-iconbtn" aria-label="Закрыть">
              <IconClose />
            </Dialog.Close>
          </header>
          <div className="cv-sheet__body">{children}</div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
