import type { Reminder } from "../../api/client";

/** Short Russian weekday names, indexed by ISO weekday (Mon = 1). */
export const WEEKDAY_LABELS = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"] as const;

/** Formats the day list of a schedule as the subtitle asks for it. */
export function formatSchedule(kind: "weekly" | "monthly", days: number[]): string {
  const sorted = [...new Set(days)].sort((a, b) => a - b);
  if (kind === "weekly") {
    if (sorted.length === 7) {
      return "Каждый день";
    }
    return sorted
      .map((day) => WEEKDAY_LABELS[day - 1])
      .filter(Boolean)
      .join(", ");
  }
  if (sorted.length === 1) {
    return `${sorted[0]}-е число каждого месяца`;
  }
  return `${sorted.join(", ")} числа месяца`;
}

function formatParts(date: Date, timezone: string): string | null {
  try {
    const formatter = new Intl.DateTimeFormat("ru-RU", {
      timeZone: timezone,
      day: "2-digit",
      month: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    });
    const parts = new Map(formatter.formatToParts(date).map((part) => [part.type, part.value]));
    const day = parts.get("day");
    const month = parts.get("month");
    const hour = parts.get("hour");
    const minute = parts.get("minute");
    if (!day || !month || !hour || !minute) {
      return null;
    }
    return `${day}.${month} ${hour}:${minute}`;
  } catch {
    return null;
  }
}

/** Formats a UTC timestamp as `дд.мм чч:мм` in the reminder's time zone. */
export function formatDateTime(value: string, timezone: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return formatParts(date, timezone) ?? "";
}

/** Subtitle of a reminder: schedule plus the repeat interval. */
export function reminderScheduleLine(reminder: Reminder): string {
  return `${formatSchedule(reminder.schedule_kind, reminder.schedule_days)} · повтор каждые ${reminder.repeat_minutes} мин`;
}

/** Second line of a reminder: pending confirmation, next firing, or disabled. */
export function reminderStatusLine(reminder: Reminder): string {
  const occurrence = reminder.active_occurrence;
  if (occurrence) {
    const at = occurrence.next_repeat_at ?? occurrence.scheduled_at;
    const formatted = formatDateTime(at, reminder.timezone);
    return formatted ? `Ждёт подтверждения · следующий повтор ${formatted}` : "Ждёт подтверждения";
  }
  if (reminder.next_due_at) {
    const formatted = formatDateTime(reminder.next_due_at, reminder.timezone);
    return formatted ? `Следующее: ${formatted}` : "Следующее срабатывание запланировано";
  }
  return "Выключено";
}
