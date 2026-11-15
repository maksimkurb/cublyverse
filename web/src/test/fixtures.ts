import type { Reminder } from "../api/client";

/** Builds a reminder with sane defaults for tests. */
export function makeReminder(overrides: Partial<Reminder> = {}): Reminder {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    message: "Выпить таблетку",
    timezone: "Europe/Moscow",
    schedule_kind: "weekly",
    schedule_days: [1, 3, 5],
    time: "09:30",
    repeat_minutes: 10,
    enabled: true,
    next_due_at: "2026-01-12T06:30:00Z",
    last_error: null,
    created_at: "2026-01-05T09:00:00Z",
    updated_at: "2026-01-05T09:00:00Z",
    ...overrides,
  };
}
