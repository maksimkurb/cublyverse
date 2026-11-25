import type { APIToken, Connection, Reminder } from "../api/client";

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

/** Builds a personal access token with sane defaults for tests. */
export function makeToken(overrides: Partial<APIToken> = {}): APIToken {
  return {
    id: "22222222-2222-4222-8222-222222222222",
    name: "Claude Code",
    prefix: "9xQ2mB7k",
    scopes: ["profile:read", "reminders:read"],
    expires_at: "2027-04-15T09:30:00Z",
    last_used_at: "2026-01-16T08:00:00Z",
    revoked_at: null,
    created_at: "2026-01-15T09:30:00Z",
    ...overrides,
  };
}

/** Builds a connected OAuth application for tests. */
export function makeConnection(overrides: Partial<Connection> = {}): Connection {
  return {
    id: "33333333-3333-4333-8333-333333333333",
    client_name: "Claude",
    workspace_id: "44444444-4444-4444-8444-444444444444",
    workspace_name: "Дом",
    scopes: ["craft:read", "craft:write"],
    created_at: "2026-01-15T09:30:00Z",
    last_used_at: "2026-01-16T08:00:00Z",
    ...overrides,
  };
}
