import { useState, type FormEvent } from "react";

import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { Button, Notice, Segmented, SelectField, Switch, TextAreaField, TextField } from "../../ui/controls";
import { IconClose } from "../../ui/icons";
import { formatMonthDays, parseMonthDays } from "./monthDays";
import {
  CUSTOM_UNITS,
  applyPreset,
  buildSavePlan,
  dayOfMonthOf,
  nthWeekdayOf,
  positionOptions,
  presetOf,
  presetOptions,
  type ReminderFormValues,
  type ReminderPreset,
  type SavePlan,
  type MonthMode,
  weekdayOf,
  weekdayOptions,
} from "./form";

const MONTH_MODES = [
  { value: "days", label: "Числа месяца" },
  { value: "weekday", label: "День недели" },
] as const;

const END_KINDS = [
  { value: "none", label: "Никогда" },
  { value: "until", label: "До даты" },
  { value: "count", label: "После N раз" },
] as const;

const MONTH_DAYS = Array.from({ length: 31 }, (_, index) => index + 1);

/** Props of the reminder form. The sheet around it is rendered by the editor. */
export interface ReminderFormProps {
  mode: "create" | "edit";
  initial: ReminderFormValues;
  /** IANA time zone: profile default for new reminders, reminder's own on edit. */
  timezone: string;
  /** Sends the plan: the reminder itself, then one create per extra time. */
  onSave: (plan: SavePlan) => Promise<void> | void;
  /** Present on existing reminders only. */
  onDelete?: () => Promise<void> | void;
}

/**
 * The editor body, modelled on a calendar recurrence dialog: the message, one or
 * more times, a start date, the «Повтор» preset and, behind «Своё…», the custom
 * editor with the interval, weekdays, monthly day and the end condition. Pure
 * client state; saving is delegated to the editor so the sheet can close after
 * success.
 */
export function ReminderForm({ mode, initial, timezone, onSave, onDelete }: ReminderFormProps) {
  const host = useHost();
  const [values, setValues] = useState<ReminderFormValues>(initial);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [deleteArmed, setDeleteArmed] = useState(false);
  // The select follows the values, but once the user opens «Своё…» the form
  // stays there: switching month modes must not collapse the editor back into a
  // preset that happens to describe the same values.
  const [customMode, setCustomMode] = useState(() => presetOf(initial) === "custom");

  const preset = customMode ? "custom" : presetOf(values);
  const custom = customMode;

  function update<K extends keyof ReminderFormValues>(key: K, value: ReminderFormValues[K]) {
    setValues((current) => ({ ...current, [key]: value }));
  }

  function changeTime(index: number, value: string) {
    setValues((current) => ({
      ...current,
      times: current.times.map((time, position) => (position === index ? value : time)),
    }));
  }

  function removeTime(index: number) {
    if (values.times.length <= 1) {
      return;
    }
    host.haptic("selection");
    setValues((current) => ({
      ...current,
      times: current.times.filter((_, position) => position !== index),
    }));
  }

  function addTime() {
    host.haptic("selection");
    setValues((current) => ({ ...current, times: [...current.times, "09:00"] }));
  }

  function toggleWeekday(day: number) {
    host.haptic("selection");
    setValues((current) => ({
      ...current,
      weekdays: current.weekdays.includes(day)
        ? current.weekdays.filter((value) => value !== day)
        : [...current.weekdays, day].sort((a, b) => a - b),
    }));
  }

  /** Choosing a preset resets the date-derived parts to the current start date. */
  function choosePreset(next: ReminderPreset) {
    host.haptic("selection");
    if (next === "custom") {
      setCustomMode(true);
      setValues((current) => applyPreset(current, next));
      return;
    }
    setCustomMode(false);
    setValues((current) => applyPreset(current, next));
  }

  /** A new start date moves a weekly or monthly preset to the new weekday/day. */
  function changeStartDate(value: string) {
    setValues((current) => {
      if (customMode) {
        return { ...current, startDate: value };
      }
      const currentPreset = presetOf(current);
      const next: ReminderFormValues = { ...current, startDate: value };
      if (currentPreset === "weekly") {
        next.weekdays = [weekdayOf(value)];
      } else if (currentPreset === "monthly-day") {
        next.monthMode = "days";
        next.monthDays = String(dayOfMonthOf(value));
      } else if (currentPreset === "monthly-nth") {
        const nth = nthWeekdayOf(value);
        next.monthMode = "weekday";
        next.monthWeekPosition = nth.position;
        next.monthWeekday = nth.weekday;
      }
      return next;
    });
  }

  function changeMonthMode(next: MonthMode) {
    host.haptic("selection");
    setValues((current) => ({ ...current, monthMode: next }));
  }

  /** The day grid and the text field edit the same list of days. */
  function toggleMonthDay(day: number) {
    host.haptic("selection");
    setValues((current) => {
      const selected = parseMonthDays(current.monthDays).days;
      const next = selected.includes(day)
        ? selected.filter((value) => value !== day)
        : [...selected, day].sort((a, b) => a - b);
      return { ...current, monthDays: formatMonthDays(next) };
    });
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = buildSavePlan(values, timezone);
    if (!result.ok) {
      setError(result.message);
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await onSave(result.plan);
      host.haptic("success");
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  async function handleDelete() {
    if (!onDelete) {
      return;
    }
    if (!deleteArmed) {
      setDeleteArmed(true);
      host.haptic("selection");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await onDelete();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
      setBusy(false);
    }
  }

  return (
    <form className="cv-form" onSubmit={handleSubmit} noValidate>
      {error && (
        <Notice tone="error" title="Проверьте данные">
          {error}
        </Notice>
      )}

      <TextAreaField
        label="Что напомнить"
        placeholder="Например: 💊 Выпить таблетку"
        rows={3}
        maxLength={1500}
        value={values.text}
        onChange={(event) => update("text", event.target.value)}
      />

      <section className="reminder-form__times" aria-label="Время напоминания">
        {values.times.map((time, index) => (
          <div className="reminder-form__time-row" key={index}>
            <TextField
              type="time"
              mono
              label={index === 0 ? "Время" : "Ещё время"}
              value={time}
              onChange={(event) => changeTime(index, event.target.value)}
            />
            <button
              type="button"
              className="cv-iconbtn"
              aria-label={`Удалить время ${index + 1}`}
              disabled={values.times.length <= 1}
              onClick={() => removeTime(index)}
            >
              <IconClose />
            </button>
          </div>
        ))}
        <Button size="s" variant="ghost" onClick={addTime}>
          + Добавить время
        </Button>
        <p className="cv-field__hint">Каждое время сохраняется как отдельное напоминание</p>
      </section>

      <TextField
        type="date"
        mono
        label={values.scheduleKind === "once" ? "Дата" : "Начать с"}
        value={values.startDate}
        onChange={(event) => changeStartDate(event.target.value)}
      />

      <SelectField label="Повтор" value={preset} onChange={(event) => choosePreset(event.target.value as ReminderPreset)}>
        {presetOptions(values).map((option) => (
          <option value={option.value} key={option.value}>
            {option.label}
          </option>
        ))}
      </SelectField>

      {custom && (
        <section className="reminder-form__custom" aria-label="Своё расписание">
          <div className="reminder-form__interval">
            <TextField
              type="number"
              mono
              min={1}
              max={1000}
              label="Каждые"
              value={values.interval}
              onChange={(event) => update("interval", event.target.value)}
            />
            <SelectField
              label="Единица"
              value={values.scheduleKind}
              onChange={(event) => update("scheduleKind", event.target.value as ReminderFormValues["scheduleKind"])}
            >
              {CUSTOM_UNITS.map((unit) => (
                <option value={unit.value} key={unit.value}>
                  {unit.label}
                </option>
              ))}
            </SelectField>
          </div>

          {values.scheduleKind === "weekly" && (
            <div className="weekdays" role="group" aria-label="Дни недели">
              {weekdayOptions.map(({ day, label }) => {
                const selected = values.weekdays.includes(day);
                return (
                  <button
                    type="button"
                    key={day}
                    className="weekdays__day"
                    aria-pressed={selected}
                    onClick={() => toggleWeekday(day)}
                  >
                    {label}
                  </button>
                );
              })}
            </div>
          )}

          {values.scheduleKind === "monthly" && (
            <>
              <Segmented
                label="День месяца"
                value={values.monthMode}
                options={MONTH_MODES}
                onChange={changeMonthMode}
              />
              {values.monthMode === "days" ? (
                <>
                  <div className="month-days" role="group" aria-label="Сетка дней">
                    {MONTH_DAYS.map((day) => {
                      const selected = parseMonthDays(values.monthDays).days.includes(day);
                      return (
                        <button
                          type="button"
                          key={day}
                          className="weekdays__day"
                          aria-pressed={selected}
                          aria-label={`День ${day}`}
                          onClick={() => toggleMonthDay(day)}
                        >
                          {day}
                        </button>
                      );
                    })}
                  </div>
                  <TextField
                    label="Дни месяца"
                    hint="Если в месяце нет такого дня, напоминание сработает в последний день месяца."
                    mono
                    inputMode="numeric"
                    placeholder="Например: 19 или 1, 15, 30"
                    value={values.monthDays}
                    onChange={(event) => update("monthDays", event.target.value)}
                  />
                </>
              ) : (
                <div className="reminder-form__nth">
                  <SelectField
                    label="Какой по счёту"
                    value={String(values.monthWeekPosition)}
                    onChange={(event) => update("monthWeekPosition", Number(event.target.value))}
                  >
                    {positionOptions.map((option) => (
                      <option value={String(option.value)} key={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </SelectField>
                  <SelectField
                    label="День недели"
                    value={String(values.monthWeekday)}
                    onChange={(event) => update("monthWeekday", Number(event.target.value))}
                  >
                    {weekdayOptions.map(({ day, label }) => (
                      <option value={String(day)} key={day}>
                        {label}
                      </option>
                    ))}
                  </SelectField>
                </div>
              )}
            </>
          )}

          <SelectField
            label="Окончание"
            value={values.endKind}
            onChange={(event) => update("endKind", event.target.value as ReminderFormValues["endKind"])}
          >
            {END_KINDS.map((option) => (
              <option value={option.value} key={option.value}>
                {option.label}
              </option>
            ))}
          </SelectField>
          {values.endKind === "until" && (
            <TextField
              type="date"
              mono
              label="Повторять до"
              value={values.endDate}
              onChange={(event) => update("endDate", event.target.value)}
            />
          )}
          {values.endKind === "count" && (
            <TextField
              type="number"
              mono
              min={1}
              max={10000}
              label="Количество повторов"
              value={values.endCount}
              onChange={(event) => update("endCount", event.target.value)}
            />
          )}
        </section>
      )}

      <TextField
        type="number"
        mono
        min={1}
        max={1440}
        label="Повторять, минут"
        hint="Бот повторяет сообщение с этим интервалом, пока не нажмёшь «Выполнено»."
        value={values.repeatMinutes}
        onChange={(event) => update("repeatMinutes", event.target.value)}
      />

      <div className="switch-row">
        <span aria-hidden="true">Включено</span>
        <Switch aria-label="Включено" checked={values.enabled} onCheckedChange={(checked) => update("enabled", checked)} />
      </div>

      <Button type="submit" variant="primary" block loading={busy}>
        Сохранить
      </Button>

      {mode === "edit" && onDelete && (
        <Button variant={deleteArmed ? "danger" : "ghost"} block disabled={busy} onClick={handleDelete}>
          {deleteArmed ? "Подтвердить удаление" : "Удалить напоминание"}
        </Button>
      )}
    </form>
  );
}
