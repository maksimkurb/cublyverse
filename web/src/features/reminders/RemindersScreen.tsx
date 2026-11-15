import { useEffect, useState } from "react";
import { Button, Cell, List, Placeholder, Switch } from "@telegram-apps/telegram-ui";

import type { Reminder } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useAuth } from "../../auth/AuthProvider";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { navigate, usePageTitle } from "../../router/router";
import { ReminderEditor } from "./ReminderEditor";
import { toggleBody, useRemindersQuery, useUpdateReminder } from "./data";
import { defaultTimezone } from "./form";
import { reminderScheduleLine, reminderStatusLine } from "./format";

type EditorState = { mode: "create" } | { mode: "edit"; reminder: Reminder } | null;

/** Reminders screen: the WebApp counterpart of the n8n Mini App. */
export default function RemindersScreen() {
  const host = useHost();
  const { user } = useAuth();
  usePageTitle("Напоминания");
  useHostBackButton(() => navigate("/"));

  const query = useRemindersQuery();
  const update = useUpdateReminder();

  const [editor, setEditor] = useState<EditorState>(null);
  const [optimistic, setOptimistic] = useState<Record<string, boolean>>({});
  const [toggling, setToggling] = useState<Record<string, boolean>>({});

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

  return (
    <main className="screen">
      <button type="button" className="screen__back" onClick={() => navigate("/")}>
        ← Главная
      </button>
      <h1 className="screen__title">Напоминания</h1>

      {query.isPending && (
        <p className="screen__state" role="status">
          Загрузка…
        </p>
      )}

      {query.isError && (
        <div className="screen__error" role="alert">
          <p>Не удалось загрузить напоминания: {query.error?.message ?? "неизвестная ошибка"}</p>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </div>
      )}

      {query.isSuccess && reminders.length === 0 && (
        <Placeholder
          header="Нет напоминаний"
          description="Добавьте первое напоминание — оно появится здесь как будильник"
        />
      )}

      {reminders.length > 0 && (
        <List className="reminder-list">
          {reminders.map((reminder) => (
            <Cell
              key={reminder.id}
              className={reminder.enabled ? "reminder-row" : "reminder-row reminder-row--disabled"}
              multiline
              role="button"
              tabIndex={0}
              aria-label={`Изменить напоминание: ${reminder.message}`}
              onClick={() => setEditor({ mode: "edit", reminder })}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  setEditor({ mode: "edit", reminder });
                }
              }}
              before={<span className="reminder-row__time">{reminder.time}</span>}
              after={
                <span
                  className="reminder-row__switch"
                  onClick={(event) => event.stopPropagation()}
                  onKeyDown={(event) => event.stopPropagation()}
                >
                  <Switch
                    role="switch"
                    aria-label={`Включить напоминание: ${reminder.message}`}
                    checked={reminder.enabled}
                    disabled={Boolean(toggling[reminder.id])}
                    onChange={(event) => void toggle(reminder, event.target.checked)}
                  />
                </span>
              }
            >
              <div
                className={
                  reminder.enabled
                    ? "reminder-row__content"
                    : "reminder-row__content reminder-row__content--dim"
                }
              >
                <span className="reminder-row__message">{reminder.message}</span>
                <span className="reminder-row__subtitle">{reminderScheduleLine(reminder)}</span>
                <span className="reminder-row__status">{reminderStatusLine(reminder)}</span>
              </div>
            </Cell>
          ))}
        </List>
      )}

      <Button className="screen__primary-action" size="l" stretched onClick={() => setEditor({ mode: "create" })}>
        ＋ Добавить напоминание
      </Button>

      {editor && (
        <ReminderEditor
          mode={editor.mode}
          reminder={editor.mode === "edit" ? editor.reminder : undefined}
          defaultTimezone={defaultTimezone(user.timezone)}
          onClose={() => setEditor(null)}
          onFinished={() => setEditor(null)}
        />
      )}
    </main>
  );
}
