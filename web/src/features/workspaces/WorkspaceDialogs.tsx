import { useState, type FormEvent } from "react";

import type { Workspace } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Notice, Sheet, TextField } from "../../ui/controls";
import { SPACE_APPS, type SpaceApp } from "./apps";
import { useCreateWorkspace, useRenameWorkspace } from "./data";

/** Props of the create dialog. */
export interface CreateWorkspaceDialogProps {
  app: SpaceApp;
  onClose: () => void;
  onCreated: (workspace: Workspace) => void;
}

/** Creates a space; the caller becomes its first member. */
export function CreateWorkspaceDialog({ app, onClose, onCreated }: CreateWorkspaceDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const create = useCreateWorkspace();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed.length < 1 || trimmed.length > 80) {
      setError("Название должно быть от 1 до 80 символов");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      const workspace = await create.mutateAsync({ body: { app, name: trimmed } });
      host.haptic("success");
      onCreated(workspace);
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title="Новое пространство" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField
          label="Название"
          placeholder="Например: Дом"
          maxLength={80}
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        <p className="page__state">{SPACE_APPS[app].sharing}</p>
        <Button type="submit" variant="primary" block loading={busy}>
          Создать пространство
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the rename dialog. */
export interface RenameWorkspaceDialogProps {
  workspace: Workspace;
  onClose: () => void;
}

/** Renames the current workspace. */
export function RenameWorkspaceDialog({ workspace, onClose }: RenameWorkspaceDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const rename = useRenameWorkspace();
  const [name, setName] = useState(workspace.name);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed.length < 1 || trimmed.length > 80) {
      setError("Название должно быть от 1 до 80 символов");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await rename.mutateAsync({ params: { path: { id: workspace.id } }, body: { name: trimmed } });
      host.haptic("success");
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title="Переименовать пространство" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField
          label="Название"
          maxLength={80}
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить
        </Button>
      </form>
    </Sheet>
  );
}
