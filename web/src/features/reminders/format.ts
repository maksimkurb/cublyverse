import type { Reminder } from "../../api/client";

/** Short Russian weekday names, indexed by ISO weekday (Mon = 1). */
export const WEEKDAY_LABELS = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"] as const;

/** Genders of the short weekday names, for the Russian ordinal agreement. */
const WEEKDAY_GENDERS: ReadonlyArray<"m" | "f" | "n"> = ["m", "m", "f", "m", "f", "f", "n"];

const WEEKDAY_FULL = ["понедельник", "вторник", "среду", "четверг", "пятницу", "субботу", "воскресенье"];

const ORDINAL_ACCUSATIVE: Record<"m" | "f" | "n", string[]> = {
  m: ["первый", "второй", "третий", "четвёртый"],
  f: ["первую", "вторую", "третью", "четвёртую"],
  n: ["первое", "второе", "третье", "четвёртое"],
};

const ORDINAL_NOMINATIVE = ["первый", "второй", "третий", "четвёртый"];

const MONTH_GENITIVE = [
  "января",
  "февраля",
  "марта",
  "апреля",
  "мая",
  "июня",
  "июля",
  "августа",
  "сентября",
  "октября",
  "ноября",
  "декабря",
];

/** Formats the day list of a weekly or monthly schedule. */
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

function plural(number: number, one: string, few: string, many: string): string {
  const mod100 = number % 100;
  if (mod100 >= 11 && mod100 <= 14) {
    return many;
  }
  switch (number % 10) {
    case 1:
      return one;
    case 2:
    case 3:
    case 4:
      return few;
    default:
      return many;
  }
}

function everyDays(interval: number): string {
  return interval === 1 ? "каждый день" : `каждые ${interval} ${plural(interval, "день", "дня", "дней")}`;
}

function everyWeeks(interval: number): string {
  return interval === 1 ? "каждую неделю" : `каждые ${interval} ${plural(interval, "неделю", "недели", "недель")}`;
}

function everyMonths(interval: number): string {
  return interval === 1 ? "каждый месяц" : `каждые ${interval} ${plural(interval, "месяц", "месяца", "месяцев")}`;
}

function everyYears(interval: number): string {
  return interval === 1 ? "каждый год" : `каждые ${interval} ${plural(interval, "год", "года", "лет")}`;
}

function joinRussian(items: string[]): string {
  switch (items.length) {
    case 0:
      return "";
    case 1:
      return items[0];
    case 2:
      return `${items[0]} и ${items[1]}`;
    default:
      return `${items.slice(0, -1).join(", ")} и ${items[items.length - 1]}`;
  }
}

function formatCivilDate(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) {
    return value;
  }
  return `${match[3]}.${match[2]}.${match[1]}`;
}

function monthName(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) {
    return "";
  }
  const month = Number(match[2]);
  return MONTH_GENITIVE[month - 1] ?? "";
}

function nthWeekdayDescription(reminder: Reminder, interval: number, time: string): string {
  const position = reminder.month_week_position ?? 1;
  const weekday = reminder.month_weekday ?? 1;
  if (weekday < 1 || weekday > 7) {
    return `каждый месяц в ${time}`;
  }
  const gender = WEEKDAY_GENDERS[weekday - 1];
  const name = WEEKDAY_FULL[weekday - 1];
  if (interval === 1) {
    const ordinal =
      position === -1
        ? { m: "последний", f: "последнюю", n: "последнее" }[gender]
        : ORDINAL_ACCUSATIVE[gender][position - 1];
    const adverb = { m: "каждый", f: "каждую", n: "каждое" }[gender];
    return `${adverb} ${ordinal} ${name} месяца в ${time}`;
  }
  const ordinal = position === -1 ? "последний" : ORDINAL_NOMINATIVE[position - 1];
  return `${everyMonths(interval)}, ${ordinal} ${name} в ${time}`;
}

function endDescription(reminder: Reminder): string {
  if (reminder.end_kind === "until" && reminder.end_date) {
    return ` до ${formatCivilDate(reminder.end_date)}`;
  }
  if (reminder.end_kind === "count" && reminder.end_count) {
    return ` (всего ${reminder.end_count} ${plural(reminder.end_count, "раз", "раза", "раз")})`;
  }
  return "";
}

/**
 * Human-readable Russian description of a schedule, mirroring the backend's
 * `reminders.ScheduleDescription`: the chat and the app describe the same rule
 * with the same words.
 */
export function describeSchedule(reminder: Reminder): string {
  const time = reminder.time;
  const interval = Math.max(1, reminder.interval || 1);
  let description: string;
  switch (reminder.schedule_kind) {
    case "once":
      return `${formatCivilDate(reminder.start_date)} в ${time} (один раз)`;
    case "daily":
      description = `${everyDays(interval)} в ${time}`;
      break;
    case "weekly": {
      const days = reminder.schedule_days
        .filter((day) => day >= 1 && day <= 7)
        .map((day) => WEEKDAY_LABELS[day - 1].toLowerCase());
      if (interval === 1 && days.length === 7) {
        description = `каждый день в ${time}`;
        break;
      }
      description = `${everyWeeks(interval)} по ${days.join(", ")} в ${time}`;
      break;
    }
    case "monthly":
      if (reminder.month_week_position !== null && reminder.month_week_position !== undefined) {
        description = nthWeekdayDescription(reminder, interval, time);
        break;
      }
      if (reminder.schedule_days.length === 1) {
        description =
          interval === 1
            ? `каждое ${reminder.schedule_days[0]}-е число месяца в ${time}`
            : `${everyMonths(interval)} ${reminder.schedule_days[0]} числа в ${time}`;
        break;
      }
      description = `${everyMonths(interval)} ${joinRussian(reminder.schedule_days.map(String))} числа в ${time}`;
      break;
    case "yearly": {
      const day = Number(reminder.start_date.slice(8, 10));
      description = `${everyYears(interval)} ${day} ${monthName(reminder.start_date)} в ${time}`;
      break;
    }
    default:
      description = `${time}`;
  }
  return description + endDescription(reminder);
}

/** Short label used by the list chips for kinds without a day grid. */
export function scheduleChipLabel(reminder: Reminder): string {
  switch (reminder.schedule_kind) {
    case "once":
      return "Один раз";
    case "daily":
      return reminder.interval === 1 ? "Каждый день" : `Каждые ${reminder.interval} дн.`;
    case "yearly":
      return "Каждый год";
    case "monthly":
      if (reminder.month_week_position !== null && reminder.month_week_position !== undefined) {
        const position = reminder.month_week_position === -1 ? "Последний" : `${reminder.month_week_position}-й`;
        return `${position} ${WEEKDAY_LABELS[(reminder.month_weekday ?? 1) - 1]}`;
      }
      return "";
    default:
      return "";
  }
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
  return `${describeSchedule(reminder)} · повтор каждые ${reminder.repeat_minutes} мин`;
}

/** Second line of a reminder: pending confirmation, next firing, finished or off. */
export function reminderStatusLine(reminder: Reminder): string {
  if (reminder.finished) {
    return "Завершено";
  }
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
