import { describe, expect, it } from "vitest";

import { hexFromQuery, medianColor } from "./color";

describe("hexFromQuery", () => {
  it("reads a HEX only when it starts with #", () => {
    expect(hexFromQuery(" #a1B2c3 ")).toBe("#A1B2C3");
    expect(hexFromQuery("311190")).toBeNull();
    expect(hexFromQuery("#abc")).toBeNull();
    expect(hexFromQuery("алый")).toBeNull();
  });
});

describe("medianColor", () => {
  it("ignores a glint and a shadow", () => {
    const pixels = new Uint8ClampedArray([10, 120, 60, 255, 255, 255, 255, 255, 10, 120, 60, 255, 0, 0, 0, 255, 10, 120, 60, 255]);
    expect(medianColor(pixels)).toBe("#0A783C");
  });

  it("returns nothing for no pixels", () => {
    expect(medianColor(new Uint8ClampedArray(0))).toBe("");
  });
});
