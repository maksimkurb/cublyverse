import { describe, expect, it } from "vitest";

import {
  emptyFormValues,
  formValuesFromReminder,
  buildSavePlan,
  defaultTimezone,
  presetOf,
  applyPreset,
  validateForm,
  validationMessages,
  type ReminderFormValues,
} from "./form";
import { makeReminder } from "../../test/fixtures";

/** A deterministic form: Monday 2026-01-05, so date-derived presets are fixed. */
function values(overrides: Partial<ReminderFormValues> = {}): ReminderFormValues {
  return { ...emptyFormValues(), text: "Выпить таблетку", startDate: "2026-01-05", ...overrides };
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

  it("requires a start date", () => {
    expect(validateForm(values({ startDate: "" }))).toEqual({ ok: false, message: validationMessages.noDate });
    expect(validateForm(values({ startDate: "2026-2-3" }))).toEqual({ ok: false, message: validationMessages.badDate });
  });

  it("checks the interval", () => {
    expect(validateForm(values({ interval: "0" }))).toEqual({ ok: false, message: validationMessages.interval });
    expect(validateForm(values({ interval: "1001" }))).toEqual({ ok: false, message: validationMessages.interval });
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

  it("accepts an nth weekday without month days", () => {
    const result = validateForm(
      values({ scheduleKind: "monthly", monthMode: "weekday", monthWeekPosition: 2, monthWeekday: 2 }),
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.days).toEqual([]);
      expect(result.monthWeekPosition).toBe(2);
      expect(result.monthWeekday).toBe(2);
    }
  });

  it("rejects an end date before the start date", () => {
    expect(validateForm(values({ endKind: "until", endDate: "2026-01-01" }))).toEqual({
      ok: false,
      message: validationMessages.endBeforeStart,
    });
  });

  it("checks the occurrence count of a count end", () => {
    expect(validateForm(values({ endKind: "count", endCount: "0" }))).toEqual({
      ok: false,
      message: validationMessages.endCount,
    });
  });

  it("checks the repeat interval", () => {
    expect(validateForm(values({ repeatMinutes: "0" }))).toEqual({ ok: false, message: validationMessages.repeat });
    expect(validateForm(values({ repeatMinutes: "1441" }))).toEqual({ ok: false, message: validationMessages.repeat });
    expect(validateForm(values({ repeatMinutes: "" }))).toEqual({ ok: false, message: validationMessages.repeat });
  });

  it("normalizes days and the repeat interval", () => {
    const result = validateForm(values({ scheduleKind: "monthly", monthDays: "31 1 15", repeatMinutes: "15" }));
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.days).toEqual([1, 15, 31]);
      expect(result.repeatMinutes).toBe(15);
      expect(result.startDate).toBe("2026-01-05");
      expect(result.endKind).toBe("none");
    }
  });
});

describe("presets", () => {
  it("recognises the date-derived presets", () => {
    expect(presetOf(values({ scheduleKind: "once" }))).toBe("once");
    expect(presetOf(values({ scheduleKind: "daily" }))).toBe("daily");
    expect(presetOf(values({ scheduleKind: "weekly", weekdays: [1] }))).toBe("weekly");
    expect(presetOf(values({ scheduleKind: "weekly", weekdays: [1, 2, 3, 4, 5] }))).toBe("weekdays");
    expect(presetOf(values({ scheduleKind: "monthly", monthDays: "5" }))).toBe("monthly-day");
    expect(
      presetOf(values({ scheduleKind: "monthly", monthMode: "weekday", monthWeekPosition: 1, monthWeekday: 1 })),
    ).toBe("monthly-nth");
    expect(presetOf(values({ scheduleKind: "yearly" }))).toBe("yearly");
  });

  it("falls back to the custom editor for other rules", () => {
    expect(presetOf(values({ scheduleKind: "weekly", weekdays: [2, 4] }))).toBe("custom");
    expect(presetOf(values({ scheduleKind: "daily", interval: "2" }))).toBe("custom");
    expect(presetOf(values({ scheduleKind: "daily", endKind: "count", endCount: "3" }))).toBe("custom");
  });

  it("applies presets relative to the start date", () => {
    const weekly = applyPreset(values(), "weekly");
    expect(weekly.scheduleKind).toBe("weekly");
    expect(weekly.weekdays).toEqual([1]);

    const monthly = applyPreset(values({ startDate: "2026-01-13" }), "monthly-nth");
    expect(monthly.scheduleKind).toBe("monthly");
    expect(monthly.monthMode).toBe("weekday");
    expect(monthly.monthWeekPosition).toBe(2);
    expect(monthly.monthWeekday).toBe(2);

    const workdays = applyPreset(values(), "weekdays");
    expect(workdays.weekdays).toEqual([1, 2, 3, 4, 5]);
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
      interval: 1,
      month_week_position: null,
      month_weekday: null,
      start_date: "2026-01-05",
      end_kind: "none",
      end_date: null,
      end_count: null,
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

  it("builds a one-time rule with the start date and no end", () => {
    const result = buildSavePlan(values({ scheduleKind: "once", startDate: "2027-03-20" }), "Europe/Moscow");
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.plan.primary.schedule_kind).toBe("once");
      expect(result.plan.primary.schedule_days).toEqual([]);
      expect(result.plan.primary.start_date).toBe("2027-03-20");
      expect(result.plan.primary.end_kind).toBe("none");
    }
  });

  it("builds an nth-weekday rule and an until end", () => {
    const result = buildSavePlan(
      values({
        scheduleKind: "monthly",
        monthMode: "weekday",
        monthWeekPosition: -1,
        monthWeekday: 5,
        endKind: "until",
        endDate: "2026-12-31",
      }),
      "Europe/Moscow",
    );
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.plan.primary.month_week_position).toBe(-1);
      expect(result.plan.primary.month_weekday).toBe(5);
      expect(result.plan.primary.schedule_days).toEqual([]);
      expect(result.plan.primary.end_kind).toBe("until");
      expect(result.plan.primary.end_date).toBe("2026-12-31");
    }
  });
});

describe("form values", () => {
  it("maps a stored reminder into the form", () => {
    expect(formValuesFromReminder(makeReminder())).toEqual({
      text: "Выпить таблетку",
      times: ["09:30"],
      scheduleKind: "weekly",
      interval: "1",
      weekdays: [1, 3, 5],
      monthMode: "days",
      monthDays: "1",
      monthWeekPosition: 1,
      monthWeekday: 1,
      startDate: "2026-01-05",
      endKind: "none",
      endDate: "",
      endCount: "1",
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
    expect(form.monthMode).toBe("days");
    expect(form.monthDays).toBe("1, 15, 30");
    expect(form.times).toEqual(["20:00"]);
    expect(form.enabled).toBe(false);
  });

  it("maps an nth-weekday reminder to the weekday mode", () => {
    const reminder = makeReminder({
      schedule_kind: "monthly",
      schedule_days: [],
      month_week_position: 2,
      month_weekday: 2,
    });
    const form = formValuesFromReminder(reminder);
    expect(form.monthMode).toBe("weekday");
    expect(form.monthWeekPosition).toBe(2);
    expect(form.monthWeekday).toBe(2);
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
