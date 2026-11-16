import type { Reminder } from "../../api/client";
import { useHostBackButton } from "../../host/HostContext";
import { Sheet } from "../../ui/controls";
import { ReminderForm } from "./ReminderForm";
import { useCreateReminder, useDeleteReminder, useUpdateReminder } from "./data";
import { emptyFormValues, formValuesFromReminder, type SavePlan } from "./form";

/** Props of the modal editor. */
export interface ReminderEditorProps {
  mode: "create" | "edit";
  /** Reminder being edited; absent when creating. */
  reminder?: Reminder;
  /** Time zone of a new reminder (the profile one, with a browser fallback). */
  defaultTimezone: string;
  onClose: () => void;
  /** Called after a successful save or delete. */
  onFinished: () => void;
}

/**
 * Sheet editor. It owns the network side of a save: `PUT` for the reminder
 * being edited and one `POST` per extra time; a create is `POST`s all the way.
 */
export function ReminderEditor({ mode, reminder, defaultTimezone, onClose, onFinished }: ReminderEditorProps) {
  useHostBackButton(onClose);

  const create = useCreateReminder();
  const update = useUpdateReminder();
  const remove = useDeleteReminder();

  const title = mode === "edit" ? "Изменить напоминание" : "Новое напоминание";
  const timezone = mode === "edit" && reminder ? reminder.timezone : defaultTimezone;
  const initial = mode === "edit" && reminder ? formValuesFromReminder(reminder) : emptyFormValues();

  async function save(plan: SavePlan) {
    if (mode === "edit" && reminder) {
      await update.mutateAsync({ params: { path: { id: reminder.id } }, body: plan.primary });
    } else {
      await create.mutateAsync({ body: plan.primary });
    }

    for (const body of plan.extra) {
      await create.mutateAsync({ body });
    }
    onFinished();
  }

  async function deleteReminder() {
    if (!reminder) {
      return;
    }
    await remove.mutateAsync({ params: { path: { id: reminder.id } } });
    onFinished();
  }

  return (
    <Sheet title={title} onClose={onClose}>
      <ReminderForm
        mode={mode}
        initial={initial}
        timezone={timezone}
        onSave={save}
        onDelete={mode === "edit" ? deleteReminder : undefined}
      />
    </Sheet>
  );
}
