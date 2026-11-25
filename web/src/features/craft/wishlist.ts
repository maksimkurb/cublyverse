import type { CraftItem, Wish, WishLink, WishStatus } from "../../api/client";
import { formatQty } from "./format";

/** Statuses of a wish list entry in the order the screen shows them. */
export const WISH_STATUS_ORDER: readonly WishStatus[] = ["wanted", "ordered", "bought"];

/** Russian heading of every status group. */
export const WISH_STATUS_LABELS: Record<WishStatus, string> = {
  wanted: "Хочу купить",
  ordered: "Заказано",
  bought: "Куплено",
};

/** Priority options of the create and edit dialogs. */
export const PRIORITY_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: "0", label: "Обычный" },
  { value: "1", label: "Важный" },
  { value: "2", label: "Очень важный" },
];

const PRIORITY_LABELS = ["Обычный приоритет", "Важный", "Очень важный"];

/** Russian label of a priority 0..2. */
export function priorityLabel(priority: number): string {
  return PRIORITY_LABELS[priority] ?? PRIORITY_LABELS[0];
}

/** Short chip label of a priority; empty for the normal one. */
export function priorityChip(priority: number): string {
  if (priority <= 0) {
    return "";
  }
  return priority >= 2 ? "Очень важно" : "Важно";
}

/** Wanted quantity with its unit, for example «3 шт»; empty when undecided. */
export function wishQtyLabel(wish: Pick<Wish, "qty_wanted" | "item">): string {
  if (wish.qty_wanted == null) {
    return "";
  }
  const unit = wish.item?.unit;
  return unit ? `${formatQty(wish.qty_wanted)} ${unit}` : formatQty(wish.qty_wanted);
}

/** Current stock of the catalogue item of a wish; empty for free text. */
export function wishStockLabel(item: CraftItem | undefined): string {
  if (!item) {
    return "";
  }
  return `На складе: ${formatQty(item.stock_qty)} ${item.unit}`;
}

/** Name of a shop link: the shop name, or the host of the URL. */
export function linkName(link: Pick<WishLink, "shop_name" | "url">): string {
  const name = link.shop_name.trim();
  if (name) {
    return name;
  }
  try {
    return new URL(link.url).host;
  } catch {
    return "Магазин";
  }
}

/** Price of a shop link with its currency; empty when unknown. */
export function linkPriceLabel(link: Pick<WishLink, "price" | "currency">): string {
  if (link.price == null) {
    return "";
  }
  const price = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 }).format(link.price);
  return link.currency ? `${price} ${link.currency}` : price;
}

/** Availability badge of a shop link; empty when the shop was not checked. */
export function linkStockLabel(link: Pick<WishLink, "in_stock">): string {
  if (link.in_stock === true) {
    return "в наличии";
  }
  if (link.in_stock === false) {
    return "нет в наличии";
  }
  return "";
}

/** One status group of the wish list. */
export interface WishGroup {
  status: WishStatus;
  label: string;
  wishes: Wish[];
}

/** Groups wishes by status, most important first inside each group. */
export function groupWishes(wishes: Wish[]): WishGroup[] {
  return WISH_STATUS_ORDER.map((status) => ({
    status,
    label: WISH_STATUS_LABELS[status],
    wishes: wishes
      .filter((wish) => wish.status === status)
      .slice()
      .sort((left, right) => right.priority - left.priority),
  })).filter((group) => group.wishes.length > 0);
}

/** Parses a positive quantity typed by the user; null when it is not one. */
export function parsePositiveQty(value: string): number | null {
  const trimmed = value.trim().replace(",", ".");
  if (!trimmed) {
    return null;
  }
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return null;
  }
  return parsed;
}

/** Parses a planned quantity: zero is allowed, negative values are not. */
export function parsePlanQty(value: string): number | null {
  const trimmed = value.trim().replace(",", ".");
  if (!trimmed) {
    return null;
  }
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return null;
  }
  return parsed;
}

/** Reports whether a value is an absolute http(s) URL. */
export function isHttpURL(value: string): boolean {
  try {
    const parsed = new URL(value.trim());
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.host !== "";
  } catch {
    return false;
  }
}

/** Russian count of added wishes for the confirmation message. */
export function addedWishesLabel(count: number): string {
  if (count === 0) {
    return "Все недостающие материалы уже в закупках.";
  }
  return `Добавлено в закупки: ${count}.`;
}
