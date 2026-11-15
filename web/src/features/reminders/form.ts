import type { CreateReminderRequest, Reminder, UpdateReminderRequest } from "../../api/client";
import { formatMonthDays, parseMonthDays } from "./monthDays";
import { WEEKDAY_LABELS } from "./format";

/** Schedule kinds accepted by the API. */
export type ReminderScheduleKind = "weekly" | "monthly";

/** Editable state of the reminder form. */
export interface ReminderFormValues {
  /** Reminder text, untrimmed. */
  text: string;
  /** Wall clock times, one reminder per time. At least one. */
  times: string[];
  scheduleKind: ReminderScheduleKind;
  /** ISO weekdays 1..7, used by the weekly schedule. */
  weekdays: number[];
  /** Raw «Дни месяца» input, used by the monthly schedule. */
  monthDays: string;
  /** Repeat interval as typed; converted to a number on save. */
  repeatMinutes: string;
  enabled: boolean;
}

/** Limits shared with the backend (`internal/features/reminders`). */
export const MAX_MESSAGE_LENGTH = 1500;
export const MIN_REPEAT_MINUTES = 1;
export const MAX_REPEAT_MINUTES = 1440;
export const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;
export const ALL_WEEKDAYS = [1, 2, 3, 4, 5, 6, 7];

/** Messages are the same as the backend's, so the user sees one wording. */
export const validationMessages = {
  message: `Сообщение должно быть от 1 до ${MAX_MESSAGE_LENGTH} символов`,
  noTime: "Укажите хотя бы одно время",
  badTime: "Время должно быть в формате ЧЧ:ММ",
  noWeekday: "Укажите хотя бы один день недели",
  badWeekday: "Дни недели должны быть от 1 (Пн) до 7 (Вс)",
  noMonthDay: "Укажите хотя бы один день месяца",
  badMonthDay: "Дни месяца должны быть от 1 до 31",
  repeat: `Интервал повтора должен быть от ${MIN_REPEAT_MINUTES} до ${MAX_REPEAT_MINUTES} минут`,
} as const;

/** Initial state of the «new reminder» form. */
export function emptyFormValues(): ReminderFormValues {
  return {
    text: "",
    times: ["09:00"],
    scheduleKind: "weekly",
    weekdays: [...ALL_WEEKDAYS],
    monthDays: "1",
    repeatMinutes: "10",
    enabled: true,
  };
}

/** Form state of an existing reminder. */
export function formValuesFromReminder(reminder: Reminder): ReminderFormValues {
  const weekly = reminder.schedule_kind === "weekly";
  return {
    text: reminder.message,
    times: [reminder.time],
    scheduleKind: reminder.schedule_kind,
    weekdays: weekly ? [...reminder.schedule_days] : [...ALL_WEEKDAYS],
    monthDays: weekly ? "1" : formatMonthDays(reminder.schedule_days),
    repeatMinutes: String(reminder.repeat_minutes),
    enabled: reminder.enabled,
  };
}

/** Result of checking the form. */
export type ValidationResult =
  | { ok: true; days: number[]; repeatMinutes: number }
  | { ok: false; message: string };

/** Checks the form and returns its canonical days and repeat interval. */
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

  let days: number[];
  if (values.scheduleKind === "weekly") {
    const unique = [...new Set(values.weekdays)].sort((a, b) => a - b);
    if (unique.length === 0) {
      return { ok: false, message: validationMessages.noWeekday };
    }
    if (unique.some((day) => day < 1 || day > 7 || !Number.isInteger(day))) {
      return { ok: false, message: validationMessages.badWeekday };
    }
    days = unique;
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

  return { ok: true, days, repeatMinutes };
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
    schedule_kind: values.scheduleKind,
    schedule_days: [...validation.days],
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
