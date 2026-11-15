import { describe, expect, it } from "vitest";

import { formatSchedule, formatDateTime, reminderScheduleLine, reminderStatusLine } from "./format";
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
    expect(reminderScheduleLine(makeReminder())).toBe("Пн, Ср, Пт · повтор каждые 10 мин");
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

  it("says the reminder is off when nothing is scheduled", () => {
    expect(reminderStatusLine(makeReminder({ enabled: false, next_due_at: null }))).toBe("Выключено");
  });
});
