import type { CraftItem } from "../../api/client";

/** The HEX an item was compared by, and how rough it is. */
export function ColorTag({ item }: { item: Pick<CraftItem, "color_hex" | "color_source"> }) {
  if (!item.color_hex) {
    return null;
  }
  return (
    <span className="craft-colortag">
      <span className="craft-colordot" style={{ background: item.color_hex }} aria-hidden="true" />
      {item.color_hex}
      {item.color_source === "photo" ? " · по фото" : item.color_source === "approx" ? " · примерно" : ""}
    </span>
  );
}
