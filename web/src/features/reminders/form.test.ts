import { describe, expect, it } from "vitest";

import {
  emptyFormValues,
  formValuesFromReminder,
  buildSavePlan,
  defaultTimezone,
  validateForm,
  validationMessages,
  type ReminderFormValues,
} from "./form";
import { makeReminder } from "../../test/fixtures";

function values(overrides: Partial<ReminderFormValues> = {}): ReminderFormValues {
  return { ...emptyFormValues(), text: "Выпить таблетку", ...overrides };
}

describe("validateForm", () => {
  it("requires a message", () => {
    expect(validateForm(values({ text: "   " }))).toEqual({ ok: false, message: validationMessages.message });
  });

  it("requires at least one time", () => {
    expect(validateForm(values({ times: [] }))).toEqual({ ok: false, message: validationMessages.noTime });
  });

  it("rejects a malformed time", () => {
    expect(validateForm(values({ times: ["9:05"] }))).toEqual({ ok: false, message: validationMessages.badTime });
  });

  it("requires a weekday", function weekly() {
    expect(validateForm(values({ scheduleKind: "weekly", weekdays: [] }))).toEqual({
      ok: false,
      message: validationMessages.noWeekday,
    });
  });

  it("requires a month day", () => {
    expect(validateForm(values({ scheduleKind: "monthly", monthDays: " , " }))).toEqual({
      ok: false,
      message: validationMessages.noMonthDay,
    });
  });

  it("rejects days of month outside 1..31", () => {
    expect(validateForm(values({ scheduleKind: "monthly", monthDays: "1, 32" }))).toEqual({
      ok: false,
      message: validationMessages.badMonthDay,
    });
  });

  it("checks the repeat interval", () => {
    expect(validateForm(values({ repeatMinutes: "0" }))).toEqual({ ok: false, message: validationMessages.repeat });
    expect(validateForm(values({ repeatMinutes: "1441" }))).toEqual({ ok: false, message: validationMessages.repeat });
    expect(validateForm(values({ repeatMinutes: "" }))).toEqual({ ok: false, message: validationMessages.repeat });
  });

  it("normalizes days and the repeat interval", () => {
    expect(
      validateForm(values({ scheduleKind: "monthly", monthDays: "31 1 15", repeatMinutes: "15" })),
    ).toEqual({ ok: true, days: [1, 15, 31], repeatMinutes: 15 });
  });
});

describe("buildSavePlan", () => {
  it("creates one request per time and shares the rest of the rule", () => {
    const result = buildSavePlan(
      values({ text: "  Выпить таблетку  ", times: ["09:00", "13:30", "20:00"], weekdays: [1, 3, 5] }),
      "Europe/Moscow",
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.plan.extra).toHaveLength(2);
    expect(result.plan.primary).toEqual({
      message: "Выпить таблетку",
      timezone: "Europe/Moscow",
      schedule_kind: "weekly",
      schedule_days: [1, 3, 5],
      time: "09:00",
      repeat_minutes: 10,
      enabled: true,
    });
    expect(result.plan.extra.map((body) => body.time)).toEqual(["13:30", "20:00"]);
    expect(result.plan.extra[0].message).toBe("Выпить таблетку");
  });

  it("returns the validation message instead of a plan", () => {
    const result = buildSavePlan(values({ text: "" }), "UTC");
    expect(result).toEqual({ ok: false, message: validationMessages.message });
  });

  it("builds a monthly disabled rule", () => {
    const result = buildSavePlan(
      values({ scheduleKind: "monthly", monthDays: "1, 15, 30", repeatMinutes: "1440", enabled: false }),
      "Asia/Oral",
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.plan.primary.schedule_days).toEqual([1, 15, 30]);
      expect(result.plan.primary.schedule_kind).toBe("monthly");
      expect(result.plan.primary.repeat_minutes).toBe(1440);
      expect(result.plan.primary.enabled).toBe(false);
    }
  });
});

describe("form values", () => {
  it("maps a stored reminder into the form", () => {
    expect(formValuesFromReminder(makeReminder())).toEqual({
      text: "Выпить таблетку",
      times: ["09:30"],
      scheduleKind: "weekly",
      weekdays: [1, 3, 5],
      monthDays: "1",
      repeatMinutes: "10",
      enabled: true,
    });
  });

  it("maps a monthly reminder back to its days", () => {
    const reminder = makeReminder({
      schedule_kind: "monthly",
      schedule_days: [1, 15, 30],
      time: "20:00",
      repeat_minutes: 30,
      enabled: false,
      next_due_at: null,
    });
    const form = formValuesFromReminder(reminder);
    expect(form.scheduleKind).toBe("monthly");
    expect(form.monthDays).toBe("1, 15, 30");
    expect(form.times).toEqual(["20:00"]);
    expect(form.enabled).toBe(false);
  });
});

describe("defaultTimezone", () => {
  it("prefers the profile time zone", () => {
    expect(defaultTimezone("Europe/Moscow")).toBe("Europe/Moscow");
  });

  it("falls back to the browser time zone", () => {
    const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;
    expect(defaultTimezone("")).toBe(browser || "UTC");
  });
});
