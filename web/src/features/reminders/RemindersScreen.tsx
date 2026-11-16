import { useEffect, useState } from "react";

import type { Reminder } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useAuth } from "../../auth/AuthProvider";
import { useHost } from "../../host/HostContext";
import { usePageTitle } from "../../router/router";
import { Button, EmptyState, Notice, Segmented, Switch } from "../../ui/controls";
import { IconCheck, IconPlus } from "../../ui/icons";
import { ReminderEditor } from "./ReminderEditor";
import { toggleBody, useCompleteReminder, useRemindersQuery, useUpdateReminder } from "./data";
import { defaultTimezone, weekdayOptions } from "./form";
import { reminderScheduleLine, reminderStatusLine } from "./format";

type EditorState = { mode: "create" } | { mode: "edit"; reminder: Reminder } | null;

type Filter = "all" | "weekly" | "monthly";

const FILTERS: ReadonlyArray<{ value: Filter; label: string }> = [
  { value: "all", label: "Все" },
  { value: "weekly", label: "По дням" },
  { value: "monthly", label: "По числам" },
];

/** The days of a reminder as chips: seven weekday squares or the month days. */
function ScheduleChips({ reminder }: { reminder: Reminder }) {
  if (reminder.schedule_kind === "monthly") {
    return (
      <span className="day-chips" aria-hidden="true">
        {reminder.schedule_days.map((day) => (
          <span className="day-chip day-chip--month" key={day}>
            {day}
          </span>
        ))}
        <span className="day-chips__suffix">числа</span>
      </span>
    );
  }
  return (
    <span className="day-chips" aria-hidden="true">
      {weekdayOptions.map(({ day, label }) => (
        <span className={reminder.schedule_days.includes(day) ? "day-chip day-chip--on" : "day-chip"} key={day}>
          {label.slice(0, 1)}
        </span>
      ))}
    </span>
  );
}

/** Reminders screen: what waits for a confirmation, then every reminder. */
export default function RemindersScreen() {
  const host = useHost();
  const { user } = useAuth();
  usePageTitle("Напоминания");

  const query = useRemindersQuery();
  const update = useUpdateReminder();
  const complete = useCompleteReminder();

  const [editor, setEditor] = useState<EditorState>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [optimistic, setOptimistic] = useState<Record<string, boolean>>({});
  const [toggling, setToggling] = useState<Record<string, boolean>>({});
  const [completing, setCompleting] = useState<string | null>(null);

  // Drop an optimistic flag once the refetched list agrees with it.
  useEffect(() => {
    const data = query.data;
    if (!data) {
      return;
    }
    setOptimistic((current) => {
      const next = { ...current };
      let changed = false;
      for (const [id, enabled] of Object.entries(current)) {
        const reminder = data.find((item) => item.id === id);
        if (reminder && reminder.enabled === enabled) {
          delete next[id];
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [query.data]);

  const reminders = (query.data ?? []).map((reminder) =>
    optimistic[reminder.id] === undefined ? reminder : { ...reminder, enabled: optimistic[reminder.id] },
  );
  const pending = reminders.filter((reminder) => reminder.active_occurrence);
  const visible = filter === "all" ? reminders : reminders.filter((reminder) => reminder.schedule_kind === filter);

  async function toggle(reminder: Reminder, enabled: boolean) {
    if (toggling[reminder.id]) {
      return;
    }
    host.haptic("selection");
    setOptimistic((current) => ({ ...current, [reminder.id]: enabled }));
    setToggling((current) => ({ ...current, [reminder.id]: true }));
    try {
      await update.mutateAsync({
        params: { path: { id: reminder.id } },
        body: toggleBody(reminder, enabled),
      });
    } catch (cause) {
      setOptimistic((current) => {
        const next = { ...current };
        delete next[reminder.id];
        return next;
      });
      host.haptic("error");
      await host.showAlert(`Не удалось изменить напоминание: ${describeApiError(cause)}`);
    } finally {
      setToggling((current) => {
        const next = { ...current };
        delete next[reminder.id];
        return next;
      });
    }
  }

  async function markDone(reminder: Reminder) {
    setCompleting(reminder.id);
    try {
      await complete.mutateAsync({ params: { path: { id: reminder.id } } });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось отметить напоминание: ${describeApiError(cause)}`);
    } finally {
      setCompleting(null);
    }
  }

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Напоминания</h1>
        <Button variant="primary" aria-label="Новое напоминание" onClick={() => setEditor({ mode: "create" })}>
          <IconPlus />
          Новое
        </Button>
      </header>

      {pending.map((reminder) => (
        <section className="pending" key={reminder.id} aria-label={`Ждёт отметки: ${reminder.message}`}>
          <div className="pending__meta">
            <span className="cv-eyebrow">Ждёт отметки</span>
            <span className="cv-mono">{reminder.time}</span>
          </div>
          <p className="pending__title">{reminder.message}</p>
          <p className="pending__text">Бот напоминает в чате, пока не отметишь.</p>
          <Button variant="solid" block loading={completing === reminder.id} onClick={() => void markDone(reminder)}>
            <IconCheck className="pending__check" />
            Выполнено
          </Button>
        </section>
      ))}

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}

      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить напоминания: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}

      {query.isSuccess && reminders.length === 0 && (
        <EmptyState title="Нет напоминаний">Добавьте первое напоминание — оно появится здесь как будильник</EmptyState>
      )}

      {reminders.length > 0 && (
        <>
          <Segmented label="Фильтр по расписанию" value={filter} options={FILTERS} onChange={setFilter} />

          {visible.length === 0 && <p className="page__state">С таким расписанием напоминаний нет.</p>}

          <ul className="card-list">
            {visible.map((reminder) => (
              <li className={reminder.enabled ? "card reminder" : "card reminder reminder--off"} key={reminder.id}>
                <button
                  type="button"
                  className="reminder__main"
                  aria-label={`Изменить напоминание: ${reminder.message}`}
                  onClick={() => setEditor({ mode: "edit", reminder })}
                >
                  <span className="reminder__title">{reminder.message}</span>
                  <span className="reminder__row">
                    <ScheduleChips reminder={reminder} />
                    <span className="cv-mono reminder__time">{reminder.time}</span>
                  </span>
                  <span className="cv-sr-only">{reminderScheduleLine(reminder)}</span>
                  <span className={reminder.active_occurrence ? "reminder__status reminder__status--pending" : "reminder__status"}>
                    {reminderStatusLine(reminder)}
                  </span>
                </button>
                <span className="reminder__switch">
                  <Switch
                    aria-label={`Включить напоминание: ${reminder.message}`}
                    checked={reminder.enabled}
                    disabled={Boolean(toggling[reminder.id])}
                    onCheckedChange={(checked) => void toggle(reminder, checked)}
                  />
                </span>
              </li>
            ))}
          </ul>
        </>
      )}

      {editor && (
        <ReminderEditor
          mode={editor.mode}
          reminder={editor.mode === "edit" ? editor.reminder : undefined}
          defaultTimezone={defaultTimezone(user.timezone)}
          onClose={() => setEditor(null)}
          onFinished={() => setEditor(null)}
        />
      )}
    </div>
  );
}
