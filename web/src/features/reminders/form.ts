import type { CreateReminderRequest, Reminder, UpdateReminderRequest } from "../../api/client";
import { formatMonthDays, parseMonthDays } from "./monthDays";
import { WEEKDAY_LABELS } from "./format";

/** Recurrence kinds accepted by the API. */
export type ReminderScheduleKind = "once" | "daily" | "weekly" | "monthly" | "yearly";

/** Stop condition of a repeating reminder. */
export type ReminderEndKind = "none" | "until" | "count";

/** How a monthly rule selects its days. */
export type MonthMode = "days" | "weekday";

/** Editable state of the reminder form. */
export interface ReminderFormValues {
  /** Reminder text, untrimmed. */
  text: string;
  /** Wall clock times, one reminder per time. At least one. */
  times: string[];
  /** Recurrence kind: the unit of `interval`. */
  scheduleKind: ReminderScheduleKind;
  /** Every N days/weeks/months/years, as typed. */
  interval: string;
  /** ISO weekdays 1..7, used by the weekly schedule. */
  weekdays: number[];
  /** Monthly rule: days of month or an nth weekday. */
  monthMode: MonthMode;
  /** Raw «Дни месяца» input, used by the day-of-month mode. */
  monthDays: string;
  /** 1..4, or -1 for the last weekday of the month. */
  monthWeekPosition: number;
  /** ISO weekday 1..7 of the nth-weekday mode. */
  monthWeekday: number;
  /** First possible day as `YYYY-MM-DD`; the date of a one-time reminder. */
  startDate: string;
  /** End condition of a repeating reminder. */
  endKind: ReminderEndKind;
  /** Last allowed day as `YYYY-MM-DD` for `until`. */
  endDate: string;
  /** Number of occurrences as typed for `count`. */
  endCount: string;
  /** Repeat interval as typed; converted to a number on save. */
  repeatMinutes: string;
  enabled: boolean;
}

/** Presets of the «Повтор» select. */
export type ReminderPreset =
  | "once"
  | "daily"
  | "weekly"
  | "monthly-day"
  | "monthly-nth"
  | "yearly"
  | "weekdays"
  | "custom";

/** Repeat kinds in the custom editor, labelled for the unit select. */
export const CUSTOM_UNITS: ReadonlyArray<{ value: ReminderScheduleKind; label: string }> = [
  { value: "daily", label: "день" },
  { value: "weekly", label: "неделя" },
  { value: "monthly", label: "месяц" },
  { value: "yearly", label: "год" },
];

/** Limits shared with the backend (`internal/features/reminders`). */
export const MAX_MESSAGE_LENGTH = 1500;
export const MIN_REPEAT_MINUTES = 1;
export const MAX_REPEAT_MINUTES = 1440;
export const MIN_INTERVAL = 1;
export const MAX_INTERVAL = 1000;
export const MIN_END_COUNT = 1;
export const MAX_END_COUNT = 10000;
export const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;
export const ALL_WEEKDAYS = [1, 2, 3, 4, 5, 6, 7];
export const WORKDAYS = [1, 2, 3, 4, 5];

/** Messages are the same as the backend's, so the user sees one wording. */
export const validationMessages = {
  message: `Сообщение должно быть от 1 до ${MAX_MESSAGE_LENGTH} символов`,
  noTime: "Укажите хотя бы одно время",
  badTime: "Время должно быть в формате ЧЧ:ММ",
  noWeekday: "Укажите хотя бы один день недели",
  badWeekday: "Дни недели должны быть от 1 (Пн) до 7 (Вс)",
  noMonthDay: "Укажите хотя бы один день месяца",
  badMonthDay: "Дни месяца должны быть от 1 до 31",
  interval: `Интервал должен быть от ${MIN_INTERVAL} до ${MAX_INTERVAL}`,
  noDate: "Укажите дату",
  badDate: "Дата должна быть в формате ГГГГ-ММ-ДД",
  endBeforeStart: "Дата окончания не может быть раньше даты начала",
  endCount: `Количество повторов должно быть от ${MIN_END_COUNT} до ${MAX_END_COUNT}`,
  repeat: `Интервал повтора должен быть от ${MIN_REPEAT_MINUTES} до ${MAX_REPEAT_MINUTES} минут`,
} as const;

/** Local date of today as `YYYY-MM-DD`. */
export function todayISO(): string {
  const now = new Date();
  return isoDate(now.getFullYear(), now.getMonth() + 1, now.getDate());
}

function isoDate(year: number, month: number, day: number): string {
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

interface CivilDate {
  year: number;
  month: number;
  day: number;
}

/** Parses a canonical `YYYY-MM-DD` date; `null` for anything else. */
export function parseISODate(value: string): CivilDate | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  if (!match) {
    return null;
  }
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(year, month - 1, day, 12, 0, 0, 0);
  if (date.getFullYear() !== year || date.getMonth() + 1 !== month || date.getDate() !== day) {
    return null;
  }
  return { year, month, day };
}

/** ISO weekday (Mon = 1, Sun = 7) of a date, or 1 when the date is invalid. */
export function weekdayOf(value: string): number {
  const parsed = parseISODate(value);
  if (!parsed) {
    return 1;
  }
  const weekday = new Date(parsed.year, parsed.month - 1, parsed.day, 12).getDay();
  return weekday === 0 ? 7 : weekday;
}

/** Day of month (1..31) of a date, or 1 when the date is invalid. */
export function dayOfMonthOf(value: string): number {
  return parseISODate(value)?.day ?? 1;
}

function daysInMonth(year: number, month: number): number {
  return new Date(year, month, 0, 12).getDate();
}

/** The nth-weekday form of a date: position 1..4, or -1 for the last one. */
export function nthWeekdayOf(value: string): { position: number; weekday: number } {
  const parsed = parseISODate(value);
  if (!parsed) {
    return { position: 1, weekday: 1 };
  }
  const weekday = weekdayOf(value);
  const position = Math.floor((parsed.day - 1) / 7) + 1;
  const isLast = parsed.day + 7 > daysInMonth(parsed.year, parsed.month);
  return { position: isLast ? -1 : position, weekday };
}

/** Genders of the short weekday names, for the Russian ordinal agreement. */
const WEEKDAY_GENDERS: ReadonlyArray<"m" | "f" | "n"> = ["m", "m", "f", "m", "f", "f", "n"];

/** Full weekday names in the accusative case: «во второй вторник». */
const WEEKDAY_FULL_ACCUSATIVE = [
  "понедельник",
  "вторник",
  "среду",
  "четверг",
  "пятницу",
  "субботу",
  "воскресенье",
];

const ORDINAL_ACCUSATIVE: Record<"m" | "f" | "n", string[]> = {
  m: ["первый", "второй", "третий", "четвёртый"],
  f: ["первую", "вторую", "третью", "четвёртую"],
  n: ["первое", "второе", "третье", "четвёртое"],
};

/** «во второй вторник», «в последнюю пятницу»: a date as an nth weekday. */
export function nthWeekdayPhrase(value: string): string {
  const { position, weekday } = nthWeekdayOf(value);
  const gender = WEEKDAY_GENDERS[weekday - 1];
  const ordinal =
    position === -1
      ? { m: "последний", f: "последнюю", n: "последнее" }[gender]
      : ORDINAL_ACCUSATIVE[gender][position - 1];
  const preposition = ordinal.startsWith("втор") ? "во" : "в";
  return `${preposition} ${ordinal} ${WEEKDAY_FULL_ACCUSATIVE[weekday - 1]}`;
}

/** Initial state of the «new reminder» form. */
export function emptyFormValues(): ReminderFormValues {
  return {
    text: "",
    times: ["09:00"],
    scheduleKind: "weekly",
    interval: "1",
    weekdays: [...ALL_WEEKDAYS],
    monthMode: "days",
    monthDays: "1",
    monthWeekPosition: 1,
    monthWeekday: 1,
    startDate: todayISO(),
    endKind: "none",
    endDate: "",
    endCount: "1",
    repeatMinutes: "10",
    enabled: true,
  };
}

/** Form state of an existing reminder. */
export function formValuesFromReminder(reminder: Reminder): ReminderFormValues {
  const monthlyNth = reminder.month_week_position !== null && reminder.month_week_position !== undefined;
  const weekly = reminder.schedule_kind === "weekly";
  return {
    text: reminder.message,
    times: [reminder.time],
    scheduleKind: reminder.schedule_kind as ReminderScheduleKind,
    interval: String(reminder.interval ?? 1),
    weekdays: weekly ? [...reminder.schedule_days] : [...ALL_WEEKDAYS],
    monthMode: monthlyNth ? "weekday" : "days",
    monthDays: reminder.schedule_kind === "monthly" && !monthlyNth ? formatMonthDays(reminder.schedule_days) : "1",
    monthWeekPosition: monthlyNth ? (reminder.month_week_position as number) : 1,
    monthWeekday: monthlyNth ? (reminder.month_weekday as number) : 1,
    startDate: reminder.start_date,
    endKind: reminder.end_kind as ReminderEndKind,
    endDate: reminder.end_date ?? "",
    endCount: reminder.end_count ? String(reminder.end_count) : "1",
    repeatMinutes: String(reminder.repeat_minutes),
    enabled: reminder.enabled,
  };
}

/** The preset that describes the current values, or `custom`. */
export function presetOf(values: ReminderFormValues): ReminderPreset {
  if (values.scheduleKind === "once") {
    return "once";
  }
  if (values.endKind !== "none" || Number(values.interval) !== 1) {
    return "custom";
  }
  if (values.scheduleKind === "daily") {
    return "daily";
  }
  if (values.scheduleKind === "yearly") {
    return "yearly";
  }
  if (values.scheduleKind === "weekly") {
    const days = [...new Set(values.weekdays)].sort((a, b) => a - b);
    if (sameDays(days, WORKDAYS)) {
      return "weekdays";
    }
    if (days.length === 1 && days[0] === weekdayOf(values.startDate)) {
      return "weekly";
    }
    return "custom";
  }
  const nth = nthWeekdayOf(values.startDate);
  if (values.monthMode === "weekday") {
    if (values.monthWeekPosition === nth.position && values.monthWeekday === nth.weekday) {
      return "monthly-nth";
    }
    return "custom";
  }
  const days = parseMonthDays(values.monthDays).days;
  if (days.length === 1 && days[0] === dayOfMonthOf(values.startDate)) {
    return "monthly-day";
  }
  return "custom";
}

/** Options of the «Повтор» select. Labels follow the start date. */
export function presetOptions(values: ReminderFormValues): ReadonlyArray<{ value: ReminderPreset; label: string }> {
  const start = values.startDate || todayISO();
  const weekday = WEEKDAY_LABELS[weekdayOf(start) - 1];
  const day = dayOfMonthOf(start);
  return [
    { value: "once", label: "Не повторять" },
    { value: "daily", label: "Каждый день" },
    { value: "weekly", label: `Каждую неделю в ${weekday.toLowerCase()}` },
    { value: "monthly-day", label: `Каждый месяц ${day}-го числа` },
    { value: "monthly-nth", label: `Каждый месяц ${nthWeekdayPhrase(start)}` },
    { value: "yearly", label: "Каждый год" },
    { value: "weekdays", label: "Рабочие дни пн–пт" },
    { value: "custom", label: "Своё…" },
  ];
}

/** Applies a preset, keeping the text, time and enabled state. */
export function applyPreset(values: ReminderFormValues, preset: ReminderPreset): ReminderFormValues {
  const start = values.startDate || todayISO();
  const nth = nthWeekdayOf(start);
  switch (preset) {
    case "once":
      return { ...values, scheduleKind: "once", endKind: "none" };
    case "daily":
      return { ...values, scheduleKind: "daily", interval: "1", endKind: "none" };
    case "weekly":
      return { ...values, scheduleKind: "weekly", interval: "1", weekdays: [weekdayOf(start)], endKind: "none" };
    case "monthly-day":
      return {
        ...values,
        scheduleKind: "monthly",
        interval: "1",
        monthMode: "days",
        monthDays: String(dayOfMonthOf(start)),
        endKind: "none",
      };
    case "monthly-nth":
      return {
        ...values,
        scheduleKind: "monthly",
        interval: "1",
        monthMode: "weekday",
        monthWeekPosition: nth.position,
        monthWeekday: nth.weekday,
        endKind: "none",
      };
    case "yearly":
      return { ...values, scheduleKind: "yearly", interval: "1", endKind: "none" };
    case "weekdays":
      return { ...values, scheduleKind: "weekly", interval: "1", weekdays: [...WORKDAYS], endKind: "none" };
    case "custom":
      return {
        ...values,
        scheduleKind: values.scheduleKind === "once" ? "daily" : values.scheduleKind,
        endKind: values.endKind,
      };
  }
}

function sameDays(left: number[], right: number[]): boolean {
  return left.length === right.length && left.every((day, index) => day === right[index]);
}

/** Result of checking the form: the canonical rule, or the message to show. */
export type ValidationResult =
  | {
      ok: true;
      kind: ReminderScheduleKind;
      days: number[];
      interval: number;
      monthWeekPosition: number | null;
      monthWeekday: number | null;
      startDate: string;
      endKind: ReminderEndKind;
      endDate: string | null;
      endCount: number | null;
      repeatMinutes: number;
    }
  | { ok: false; message: string };

/** Checks the form and returns its canonical rule. */
export function validateForm(values: ReminderFormValues): ValidationResult {
  const text = values.text.trim();
  if (text.length < 1 || [...text].length > MAX_MESSAGE_LENGTH) {
    return { ok: false, message: validationMessages.message };
  }

  if (values.times.length === 0) {
    return { ok: false, message: validationMessages.noTime };
  }
  for (const time of values.times) {
    if (!TIME_PATTERN.test(time.trim())) {
      return { ok: false, message: validationMessages.badTime };
    }
  }

  const rawInterval = values.interval.trim();
  const interval = rawInterval === "" ? 1 : Number(rawInterval);
  if (!Number.isInteger(interval) || interval < MIN_INTERVAL || interval > MAX_INTERVAL) {
    return { ok: false, message: validationMessages.interval };
  }

  const startDate = values.startDate.trim();
  if (!startDate) {
    return { ok: false, message: validationMessages.noDate };
  }
  if (!parseISODate(startDate)) {
    return { ok: false, message: validationMessages.badDate };
  }

  let days: number[] = [];
  let monthWeekPosition: number | null = null;
  let monthWeekday: number | null = null;
  switch (values.scheduleKind) {
    case "once":
    case "daily":
    case "yearly":
      break;
    case "weekly": {
      const unique = [...new Set(values.weekdays)].sort((a, b) => a - b);
      if (unique.length === 0) {
        return { ok: false, message: validationMessages.noWeekday };
      }
      if (unique.some((day) => day < 1 || day > 7 || !Number.isInteger(day))) {
        return { ok: false, message: validationMessages.badWeekday };
      }
      days = unique;
      break;
    }
    case "monthly":
      if (values.monthMode === "weekday") {
        if (![1, 2, 3, 4, -1].includes(values.monthWeekPosition)) {
          return { ok: false, message: "Позиция дня недели должна быть от 1 до 4 или последняя" };
        }
        if (values.monthWeekday < 1 || values.monthWeekday > 7) {
          return { ok: false, message: validationMessages.badWeekday };
        }
        monthWeekPosition = values.monthWeekPosition;
        monthWeekday = values.monthWeekday;
      } else {
        const parsed = parseMonthDays(values.monthDays);
        if (parsed.invalid.length > 0) {
          return { ok: false, message: validationMessages.badMonthDay };
        }
        if (parsed.days.length === 0) {
          return { ok: false, message: validationMessages.noMonthDay };
        }
        days = parsed.days;
      }
      break;
  }

  let endKind = values.endKind;
  let endDate: string | null = null;
  let endCount: number | null = null;
  if (values.scheduleKind === "once") {
    endKind = "none";
  }
  switch (endKind) {
    case "until": {
      const raw = values.endDate.trim();
      if (!raw) {
        return { ok: false, message: validationMessages.noDate };
      }
      if (!parseISODate(raw)) {
        return { ok: false, message: validationMessages.badDate };
      }
      if (raw < startDate) {
        return { ok: false, message: validationMessages.endBeforeStart };
      }
      endDate = raw;
      break;
    }
    case "count": {
      const raw = values.endCount.trim();
      const count = Number(raw);
      if (!Number.isInteger(count) || count < MIN_END_COUNT || count > MAX_END_COUNT) {
        return { ok: false, message: validationMessages.endCount };
      }
      endCount = count;
      break;
    }
    case "none":
      break;
  }

  const rawRepeat = values.repeatMinutes.trim();
  const repeatMinutes = Number(rawRepeat);
  if (
    rawRepeat === "" ||
    !Number.isInteger(repeatMinutes) ||
    repeatMinutes < MIN_REPEAT_MINUTES ||
    repeatMinutes > MAX_REPEAT_MINUTES
  ) {
    return { ok: false, message: validationMessages.repeat };
  }

  return {
    ok: true,
    kind: values.scheduleKind,
    days,
    interval,
    monthWeekPosition,
    monthWeekday,
    startDate,
    endKind,
    endDate,
    endCount,
    repeatMinutes,
  };
}

/** Body shared by create and update: `PUT` requires every field as well. */
export type ReminderBody = CreateReminderRequest & UpdateReminderRequest;

/** What saving the form does: one request for the reminder itself and, for
 * extra times, one create request each. */
export interface SavePlan {
  primary: ReminderBody;
  extra: ReminderBody[];
}

/** Either the requests to send or the validation message to show. */
export type SavePlanResult = { ok: true; plan: SavePlan } | { ok: false; message: string };

/** Time zone of a new reminder: profile, then browser, then UTC. */
export function defaultTimezone(profileTimezone?: string | null): string {
  const fromProfile = profileTimezone?.trim();
  if (fromProfile) {
    return fromProfile;
  }
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/**
 * Builds the API bodies of one save.
 *
 * The first time is the reminder being edited (or created); every additional
 * time is a separate reminder, exactly like the original Mini App. The caller
 * sends `primary` first and then one create request per `extra` entry.
 */
export function buildSavePlan(values: ReminderFormValues, timezone: string): SavePlanResult {
  const validation = validateForm(values);
  if (!validation.ok) {
    return { ok: false, message: validation.message };
  }

  const times = values.times.map((time) => time.trim());
  const bodies = times.map<ReminderBody>((time) => ({
    message: values.text.trim(),
    timezone,
    schedule_kind: validation.kind,
    schedule_days: [...validation.days],
    interval: validation.interval,
    month_week_position: validation.monthWeekPosition,
    month_weekday: validation.monthWeekday,
    start_date: validation.startDate,
    end_kind: validation.endKind,
    end_date: validation.endDate,
    end_count: validation.endCount,
    time,
    repeat_minutes: validation.repeatMinutes,
    enabled: values.enabled,
  }));

  return {
    ok: true,
    plan: { primary: bodies[0], extra: bodies.slice(1) },
  };
}

/** Weekday chip labels with their ISO day. */
export const weekdayOptions = WEEKDAY_LABELS.map((label, index) => ({ day: index + 1, label }));

/** Nth-weekday positions for the custom editor. */
export const positionOptions: ReadonlyArray<{ value: number; label: string }> = [
  { value: 1, label: "первый" },
  { value: 2, label: "второй" },
  { value: 3, label: "третий" },
  { value: 4, label: "четвёртый" },
  { value: -1, label: "последний" },
];
