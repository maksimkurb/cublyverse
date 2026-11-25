import { useState } from "react";

import type { Reminder } from "../../api/client";
import { useHostBackButton } from "../../host/HostContext";
import { Checkbox, Sheet } from "../../ui/controls";
import { useWorkspaceMembers } from "../workspaces/data";
import { personName } from "../workspaces/SpaceMembers";
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
  /** Reminders space a new reminder goes to. */
  spaceId: string;
  /** The signed-in user, the default recipient. */
  currentUserId: string;
  onClose: () => void;
  /** Called after a successful save or delete. */
  onFinished: () => void;
}

/**
 * Sheet editor. It owns the network side of a save: `PUT` for the reminder
 * being edited and one `POST` per extra time; a create is `POST`s all the way.
 */
export function ReminderEditor({ mode, reminder, defaultTimezone, spaceId, currentUserId, onClose, onFinished }: ReminderEditorProps) {
  useHostBackButton(onClose);

  const members = useWorkspaceMembers(reminder?.space_id ?? spaceId).data ?? [];
  const [recipients, setRecipients] = useState<string[]>(reminder?.recipient_ids ?? [currentUserId]);
  // With one member there is nobody to choose between.
  const shared = members.length > 1;

  const create = useCreateReminder();
  const update = useUpdateReminder();
  const remove = useDeleteReminder();

  const title = mode === "edit" ? "Изменить напоминание" : "Новое напоминание";
  const timezone = mode === "edit" && reminder ? reminder.timezone : defaultTimezone;
  const initial = mode === "edit" && reminder ? formValuesFromReminder(reminder) : emptyFormValues();

  async function save(plan: SavePlan) {
    if (shared && recipients.length === 0) {
      throw new Error("Выберите хотя бы одного получателя.");
    }
    // Without a choice the server keeps the recipients, or picks the author.
    const chosen = shared ? { recipient_ids: recipients } : {};
    if (mode === "edit" && reminder) {
      await update.mutateAsync({ params: { path: { id: reminder.id } }, body: { ...plan.primary, ...chosen } });
    } else {
      await create.mutateAsync({ body: { ...plan.primary, ...chosen, space_id: spaceId } });
    }

    for (const body of plan.extra) {
      await create.mutateAsync({ body: { ...body, ...chosen, space_id: reminder?.space_id ?? spaceId } });
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
      {shared && (
        <fieldset className="recipients">
          <legend className="cv-eyebrow">Кому присылать</legend>
          {members.map((member) => (
            <label className="scopes__item" key={member.user_id}>
              <Checkbox
                checked={recipients.includes(member.user_id)}
                onCheckedChange={(checked) =>
                  setRecipients((current) =>
                    checked ? [...current, member.user_id] : current.filter((id) => id !== member.user_id),
                  )
                }
              />
              <span className="scopes__label">
                {personName(member)}
                {member.user_id === currentUserId ? " (вы)" : ""}
              </span>
            </label>
          ))}
        </fieldset>
      )}
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
