import type { CraftItem, CraftStockEntry } from "../../api/client";

/** Units the server accepts; the same list as `craft.Units`. */
export const UNITS = ["шт", "г", "моток", "м", "см", "упаковка", "набор"] as const;

/** Formats a quantity without trailing zeros, in the Russian locale. */
export function formatQty(value: number): string {
  return new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 3 }).format(value);
}

/** Quantity with its unit, for example «12 шт». */
export function qtyLabel(entry: Pick<CraftStockEntry, "qty" | "item">): string {
  return `${formatQty(entry.qty)} ${entry.item.unit}`;
}

/** Manufacturer and article line of an item; empty when neither is known. */
export function itemMeta(item: CraftItem): string {
  return [item.manufacturer, item.article].filter(Boolean).join(" · ");
}

/** Colour name and finish line of an item; empty when neither is known. */
export function itemColorLabel(item: CraftItem): string {
  return [item.color_name, item.finish].filter(Boolean).join(" · ");
}

/** Inline style of the colour swatch of an item, or undefined when unknown. */
export function swatchStyle(item: Pick<CraftItem, "color_hex">): { background: string } | undefined {
  return item.color_hex ? { background: item.color_hex } : undefined;
}

/** Parses a `#RRGGBB` value typed by the user; null when it is invalid. */
export function normalizeHex(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }
  if (!/^#[0-9A-Fa-f]{6}$/.test(trimmed)) {
    return null;
  }
  return trimmed.toUpperCase();
}
