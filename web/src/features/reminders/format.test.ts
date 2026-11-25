import { describe, expect, it } from "vitest";

import {
  describeSchedule,
  formatSchedule,
  formatDateTime,
  reminderScheduleLine,
  reminderStatusLine,
  scheduleChipLabel,
} from "./format";
import { makeReminder } from "../../test/fixtures";

describe("formatSchedule", () => {
  it("says every day for all seven weekdays", () => {
    expect(formatSchedule("weekly", [7, 1, 2, 3, 4, 5, 6])).toBe("Каждый день");
  });

  it("lists short weekday names in order", () => {
    expect(formatSchedule("weekly", [5, 1, 3])).toBe("Пн, Ср, Пт");
  });

  it("formats a single day of month", () => {
    expect(formatSchedule("monthly", [19])).toBe("19-е число каждого месяца");
  });

  it("formats several days of month", () => {
    expect(formatSchedule("monthly", [30, 1, 15])).toBe("1, 15, 30 числа месяца");
  });
});

describe("describeSchedule", () => {
  it("describes a weekly rule", () => {
    expect(describeSchedule(makeReminder())).toBe("каждую неделю по пн, ср, пт в 09:30");
  });

  it("describes a weekly rule every N weeks", () => {
    expect(describeSchedule(makeReminder({ interval: 2 }))).toBe("каждые 2 недели по пн, ср, пт в 09:30");
  });

  it("describes a monthly day-of-month rule", () => {
    expect(
      describeSchedule(makeReminder({ schedule_kind: "monthly", schedule_days: [15] })),
    ).toBe("каждое 15-е число месяца в 09:30");
  });

  it("describes an nth-weekday rule", () => {
    expect(
      describeSchedule(
        makeReminder({
          schedule_kind: "monthly",
          schedule_days: [],
          month_week_position: 2,
          month_weekday: 2,
        }),
      ),
    ).toBe("каждый второй вторник месяца в 09:30");
  });

  it("describes the last weekday of the month", () => {
    expect(
      describeSchedule(
        makeReminder({
          schedule_kind: "monthly",
          schedule_days: [],
          month_week_position: -1,
          month_weekday: 5,
        }),
      ),
    ).toBe("каждую последнюю пятницу месяца в 09:30");
  });

  it("describes a one-time rule and a yearly rule", () => {
    expect(
      describeSchedule(makeReminder({ schedule_kind: "once", schedule_days: [], start_date: "2027-03-20" })),
    ).toBe("20.03.2027 в 09:30 (один раз)");
    expect(
      describeSchedule(makeReminder({ schedule_kind: "yearly", schedule_days: [], start_date: "2026-03-20" })),
    ).toBe("каждый год 20 марта в 09:30");
  });

  it("adds the end condition", () => {
    expect(
      describeSchedule(makeReminder({ end_kind: "until", end_date: "2026-12-31" })),
    ).toBe("каждую неделю по пн, ср, пт в 09:30 до 31.12.2026");
    expect(
      describeSchedule(makeReminder({ end_kind: "count", end_count: 3 })),
    ).toBe("каждую неделю по пн, ср, пт в 09:30 (всего 3 раза)");
  });
});

describe("scheduleChipLabel", () => {
  it("labels the kinds without a day grid", () => {
    expect(scheduleChipLabel(makeReminder({ schedule_kind: "once", schedule_days: [] }))).toBe("Один раз");
    expect(scheduleChipLabel(makeReminder({ schedule_kind: "daily", schedule_days: [] }))).toBe("Каждый день");
    expect(scheduleChipLabel(makeReminder({ schedule_kind: "yearly", schedule_days: [] }))).toBe("Каждый год");
  });
});

describe("formatDateTime", () => {
  it("renders dd.mm hh:mm in the reminder time zone", () => {
    expect(formatDateTime("2026-01-12T06:30:00Z", "Europe/Moscow")).toBe("12.01 09:30");
  });

  it("returns an empty string for an invalid timestamp", () => {
    expect(formatDateTime("not-a-date", "Europe/Moscow")).toBe("");
  });
});

describe("reminder lines", () => {
  it("builds the schedule line with the repeat interval", () => {
    expect(reminderScheduleLine(makeReminder())).toBe("каждую неделю по пн, ср, пт в 09:30 · повтор каждые 10 мин");
  });

  it("shows the next firing", () => {
    expect(reminderStatusLine(makeReminder())).toBe("Следующее: 12.01 09:30");
  });

  it("shows a pending confirmation with the next repeat", () => {
    const reminder = makeReminder({
      active_occurrence: {
        id: "22222222-2222-4222-8222-222222222222",
        reminder_id: "11111111-1111-4111-8111-111111111111",
        scheduled_at: "2026-01-12T06:30:00Z",
        first_sent_at: "2026-01-12T06:30:01Z",
        repeat_count: 2,
        next_repeat_at: "2026-01-12T06:40:01Z",
        status: "active",
        completed_at: null,
      },
    });
    expect(reminderStatusLine(reminder)).toBe("Ждёт подтверждения · следующий повтор 12.01 09:40");
  });

  it("says the reminder is finished", () => {
    expect(
      reminderStatusLine(
        makeReminder({
          enabled: false,
          next_due_at: null,
          finished: true,
          finished_at: "2026-01-12T06:30:00Z",
        }),
      ),
    ).toBe("Завершено");
  });

  it("says the reminder is off when nothing is scheduled", () => {
    expect(reminderStatusLine(makeReminder({ enabled: false, next_due_at: null }))).toBe("Выключено");
  });
});
