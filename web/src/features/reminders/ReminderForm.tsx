import { useState, type FormEvent } from "react";
import { Banner, Button, Input, Select, Switch, Textarea } from "@telegram-apps/telegram-ui";

import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { type ReminderFormValues, type SavePlan, buildSavePlan, weekdayOptions } from "./form";

/** Props of the reminder form. The Modal around it is rendered by the editor. */
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
 * The editor body: text, one or more times, schedule days, repeat interval and
 * the enabled switch. Pure client state; saving is delegated to the editor so
 * the modal can close after success.
 */
export function ReminderForm({ mode, initial, timezone, onSave, onDelete }: ReminderFormProps) {
  const host = useHost();
  const [values, setValues] = useState<ReminderFormValues>(initial);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [deleteArmed, setDeleteArmed] = useState(false);

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
    <form className="reminder-form" onSubmit={handleSubmit} noValidate>
      {error && (
        <div className="reminder-form__error" role="alert">
          <Banner type="inline" header="Проверьте данные" description={error} />
        </div>
      )}

      <Textarea
        header="Что напомнить"
        aria-label="Что напомнить"
        placeholder="Например: 💊 Выпить таблетку"
        rows={3}
        maxLength={1500}
        value={values.text}
        onChange={(event) => update("text", event.target.value)}
      />

      <section className="reminder-form__times" aria-label="Время напоминания">
        {values.times.map((time, index) => (
          <div className="reminder-form__time-row" key={index}>
            <Input
              type="time"
              header={index === 0 ? "Время" : "Ещё время"}
              aria-label={index === 0 ? "Время" : "Ещё время"}
              value={time}
              onChange={(event) => changeTime(index, event.target.value)}
            />
            <button
              type="button"
              className="reminder-form__remove-time"
              aria-label={`Удалить время ${index + 1}`}
              disabled={values.times.length <= 1}
              onClick={() => removeTime(index)}
            >
              ×
            </button>
          </div>
        ))}
        <Button type="button" size="s" mode="plain" onClick={addTime}>
          + Добавить время
        </Button>
        <p className="reminder-form__note">Каждое время сохраняется как отдельное напоминание</p>
      </section>

      <Select
        header="Расписание"
        aria-label="Расписание"
        value={values.scheduleKind}
        onChange={(event) => update("scheduleKind", event.target.value === "monthly" ? "monthly" : "weekly")}
      >
        <option value="weekly">По дням недели</option>
        <option value="monthly">По дням месяца</option>
      </Select>

      {values.scheduleKind === "weekly" ? (
        <div className="reminder-form__weekdays" role="group" aria-label="Дни недели">
          {weekdayOptions.map(({ day, label }) => {
            const selected = values.weekdays.includes(day);
            return (
              <button
                type="button"
                key={day}
                className={selected ? "weekday-chip weekday-chip--selected" : "weekday-chip"}
                aria-pressed={selected}
                onClick={() => toggleWeekday(day)}
              >
                {label}
              </button>
            );
          })}
        </div>
      ) : (
        <div className="reminder-form__monthdays">
          <Input
            header="Дни месяца"
            aria-label="Дни месяца"
            inputMode="numeric"
            placeholder="Например: 19 или 1, 15, 30"
            value={values.monthDays}
            onChange={(event) => update("monthDays", event.target.value)}
          />
          <p className="reminder-form__note">
            Если в месяце нет такого дня, напоминание сработает в последний день месяца.
          </p>
        </div>
      )}

      <Input
        type="number"
        min={1}
        max={1440}
        header="Повторять, минут"
        aria-label="Повторять, минут"
        value={values.repeatMinutes}
        onChange={(event) => update("repeatMinutes", event.target.value)}
      />

      <label className="reminder-form__switch">
        <span>Включено</span>
        <Switch
          aria-label="Включено"
          checked={values.enabled}
          onChange={(event) => update("enabled", event.target.checked)}
        />
      </label>

      <Button type="submit" size="l" stretched loading={busy} disabled={busy}>
        Сохранить
      </Button>

      {mode === "edit" && onDelete && (
        <Button
          type="button"
          size="l"
          stretched
          mode="plain"
          className={deleteArmed ? "reminder-form__delete reminder-form__delete--armed" : "reminder-form__delete"}
          disabled={busy}
          onClick={handleDelete}
        >
          {deleteArmed ? "Подтвердить удаление" : "Удалить напоминание"}
        </Button>
      )}
    </form>
  );
}
