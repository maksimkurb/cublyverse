import { describe, expect, it } from "vitest";

import { makeMaterial } from "../../test/fixtures";
import { hasShortage, materialsCountLabel, plannedLabel, shortageLabel, stockLabel, usedLabel } from "./project";

describe("project helpers", () => {
  it("declines the number of planned materials in Russian", () => {
    expect(materialsCountLabel(0)).toBe("0 материалов");
    expect(materialsCountLabel(1)).toBe("1 материал");
    expect(materialsCountLabel(2)).toBe("2 материала");
    expect(materialsCountLabel(4)).toBe("4 материала");
    expect(materialsCountLabel(5)).toBe("5 материалов");
    expect(materialsCountLabel(11)).toBe("11 материалов");
    expect(materialsCountLabel(21)).toBe("21 материал");
  });

  it("formats the plan, usage, stock and shortage of a material", () => {
    const material = makeMaterial();
    expect(plannedLabel(material)).toBe("20 шт");
    expect(plannedLabel(makeMaterial({ qty_planned: null }))).toBe("Без плана");
    expect(usedLabel(material)).toBe("5 шт");
    expect(stockLabel(material)).toBe("7 шт");
    expect(shortageLabel(material)).toBe("8 шт");
    expect(shortageLabel(makeMaterial({ shortage: 0 }))).toBe("");
  });

  it("reports whether anything has to be bought", () => {
    expect(hasShortage([makeMaterial(), makeMaterial({ shortage: 0 })])).toBe(true);
    expect(hasShortage([makeMaterial({ shortage: 0 })])).toBe(false);
  });
});
