import { useState, type FormEvent } from "react";

import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { Button, Notice, Segmented, Switch, TextAreaField, TextField } from "../../ui/controls";
import { IconClose } from "../../ui/icons";
import { type ReminderFormValues, type SavePlan, buildSavePlan, weekdayOptions } from "./form";

const SCHEDULE_KINDS = [
  { value: "weekly", label: "Дни недели" },
  { value: "monthly", label: "Числа месяца" },
] as const;

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
 * The editor body: text, one or more times, schedule days, repeat interval and
 * the enabled switch. Pure client state; saving is delegated to the editor so
 * the sheet can close after success.
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

      <Segmented
        label="Расписание"
        value={values.scheduleKind}
        options={SCHEDULE_KINDS}
        onChange={(kind) => update("scheduleKind", kind)}
      />

      {values.scheduleKind === "weekly" ? (
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
      ) : (
        <TextField
          label="Дни месяца"
          hint="Если в месяце нет такого дня, напоминание сработает в последний день месяца."
          mono
          inputMode="numeric"
          placeholder="Например: 19 или 1, 15, 30"
          value={values.monthDays}
          onChange={(event) => update("monthDays", event.target.value)}
        />
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
