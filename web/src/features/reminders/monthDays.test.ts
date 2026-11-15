import { describe, expect, it } from "vitest";

import { formatMonthDays, parseMonthDays } from "./monthDays";

describe("parseMonthDays", () => {
  it("splits on commas, spaces and semicolons", () => {
    expect(parseMonthDays("30, 1;15 5").days).toEqual([1, 5, 15, 30]);
  });

  it("deduplicates and sorts", () => {
    expect(parseMonthDays("19 19, 1, 19").days).toEqual([1, 19]);
  });

  it("ignores empty tokens from trailing separators", () => {
    expect(parseMonthDays("1, 15, ").days).toEqual([1, 15]);
  });

  it("reports tokens outside 1..31 and non-numbers", () => {
    const parsed = parseMonthDays("0 32 abc 15");
    expect(parsed.days).toEqual([15]);
    expect(parsed.invalid).toEqual(["0", "32", "abc"]);
  });
});

describe("formatMonthDays", () => {
  it("round-trips stored days into the input", () => {
    expect(formatMonthDays([30, 1, 15, 15])).toBe("1, 15, 30");
  });
});
