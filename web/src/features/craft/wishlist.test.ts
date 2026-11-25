import { describe, expect, it } from "vitest";

import { makeWish, makeWishLink } from "../../test/fixtures";
import {
  groupWishes,
  isHttpURL,
  linkName,
  linkPriceLabel,
  linkStockLabel,
  parsePositiveQty,
  priorityChip,
  wishQtyLabel,
} from "./wishlist";

describe("wishlist helpers", () => {
  it("groups wishes by status with the most important first", () => {
    const groups = groupWishes([
      makeWish({ id: "1", status: "bought", priority: 0 }),
      makeWish({ id: "2", status: "wanted", priority: 1 }),
      makeWish({ id: "3", status: "wanted", priority: 2 }),
      makeWish({ id: "4", status: "ordered", priority: 0 }),
    ]);

    expect(groups.map((group) => group.status)).toEqual(["wanted", "ordered", "bought"]);
    expect(groups[0].wishes.map((wish) => wish.id)).toEqual(["3", "2"]);
    expect(groups[0].label).toBe("Хочу купить");
  });

  it("formats quantities, shop links and prices", () => {
    expect(wishQtyLabel(makeWish({ qty_wanted: 3 }))).toBe("3 шт");
    expect(wishQtyLabel(makeWish({ qty_wanted: null }))).toBe("");
    expect(wishQtyLabel(makeWish({ qty_wanted: 2, item: undefined }))).toBe("2");

    expect(linkName(makeWishLink({ shop_name: "Магазин" }))).toBe("Магазин");
    expect(linkName(makeWishLink({ shop_name: "", url: "https://shop.example/x" }))).toBe("shop.example");
    expect(linkPriceLabel(makeWishLink({ price: 12.5, currency: "RUB" }))).toBe("12,5 RUB");
    expect(linkPriceLabel(makeWishLink({ price: null }))).toBe("");
    expect(linkStockLabel(makeWishLink({ in_stock: true }))).toBe("в наличии");
    expect(linkStockLabel(makeWishLink({ in_stock: false }))).toBe("нет в наличии");
    expect(linkStockLabel(makeWishLink({ in_stock: null }))).toBe("");
  });

  it("accepts only absolute http(s) links and positive quantities", () => {
    expect(isHttpURL("https://shop.example/x")).toBe(true);
    expect(isHttpURL("http://shop.example")).toBe(true);
    expect(isHttpURL("ftp://shop.example")).toBe(false);
    expect(isHttpURL("shop.example/x")).toBe(false);
    expect(parsePositiveQty("2,5")).toBe(2.5);
    expect(parsePositiveQty("0")).toBeNull();
    expect(parsePositiveQty("")).toBeNull();
    expect(priorityChip(0)).toBe("");
    expect(priorityChip(2)).toBe("Очень важно");
  });
});
