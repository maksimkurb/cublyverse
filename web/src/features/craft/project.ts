import type { ProjectMaterial } from "../../api/client";
import { formatQty } from "./format";

/** Quantity with a unit, for example «20 шт». */
export function qtyWithUnit(value: number, unit: string): string {
  return unit ? `${formatQty(value)} ${unit}` : formatQty(value);
}

/** Planned quantity of a material; «Без плана» when it was cleared. */
export function plannedLabel(material: Pick<ProjectMaterial, "qty_planned" | "item">): string {
  if (material.qty_planned == null) {
    return "Без плана";
  }
  return qtyWithUnit(material.qty_planned, material.item.unit);
}

/** Used quantity of a material. */
export function usedLabel(material: Pick<ProjectMaterial, "qty_used" | "item">): string {
  return qtyWithUnit(material.qty_used, material.item.unit);
}

/** Current stock of the material. */
export function stockLabel(material: Pick<ProjectMaterial, "item">): string {
  return qtyWithUnit(material.item.stock_qty, material.item.unit);
}

/** Shortage of a material; empty when nothing is missing. */
export function shortageLabel(material: Pick<ProjectMaterial, "shortage" | "item">): string {
  if (material.shortage <= 0) {
    return "";
  }
  return qtyWithUnit(material.shortage, material.item.unit);
}

/** Russian count of planned materials: «1 материал», «2 материала». */
export function materialsCountLabel(count: number): string {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (mod10 === 1 && mod100 !== 11) {
    return `${count} материал`;
  }
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return `${count} материала`;
  }
  return `${count} материалов`;
}

/** Reports whether at least one material has to be bought. */
export function hasShortage(materials: ProjectMaterial[]): boolean {
  return materials.some((material) => material.shortage > 0);
}
