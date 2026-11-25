import { useState, type FormEvent } from "react";

import type { Workspace } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useAuth } from "../../auth/AuthProvider";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Notice, Sheet, TextField } from "../../ui/controls";
import { IconPlus, IconUsers } from "../../ui/icons";
import { SPACE_APPS, type SpaceApp } from "./apps";
import { useCreateWorkspace, useDeleteWorkspace } from "./data";
import type { CurrentWorkspace } from "./selection";
import { CandidatesSection, MembersList } from "./SpaceMembers";
import { TemplatePicker, type WarehouseTemplate } from "./TemplatePicker";
import { CreateWorkspaceDialog, RenameWorkspaceDialog } from "./WorkspaceDialogs";

/** Props of the space control of a mini-app. */
export interface SpaceBarProps {
  app: SpaceApp;
  space: CurrentWorkspace;
}

/**
 * The button with the current space, made to sit next to the title of a
 * mini-app. It opens switching and sharing; without a space it renders nothing.
 */
export function SpaceButton({ app, space }: SpaceBarProps) {
  const [open, setOpen] = useState(false);
  const { current } = space;
  if (!current) {
    return null;
  }
  return (
    <>
      <Button
        size="s"
        className="space-button"
        aria-label={`Пространство: ${current.name}. Переключить и настроить`}
        onClick={() => setOpen(true)}
      >
        <IconUsers />
        <span className="space-button__name">{current.name}</span>
        <span aria-hidden="true">▾</span>
      </Button>
      {open && <SpaceSheet app={app} space={space} current={current} onClose={() => setOpen(false)} />}
    </>
  );
}

/**
 * What stands in place of a mini-app that has no space yet: loading, the error,
 * or the onboarding. With spaces it renders nothing.
 */
export function SpaceState({ app, space }: SpaceBarProps) {
  const { query, workspaces } = space;

  if (query.isPending) {
    return (
      <p className="page__state" role="status">
        Загрузка…
      </p>
    );
  }
  if (query.isError) {
    return (
      <Notice tone="error">
        <span>Не удалось загрузить пространства: {query.error?.message ?? "неизвестная ошибка"}</span>
        <Button size="s" onClick={() => void query.refetch()}>
          Повторить
        </Button>
      </Notice>
    );
  }
  return workspaces.length === 0 ? <Onboarding app={app} space={space} /> : null;
}

/** First visit of a mini-app: create the first space. */
function Onboarding({ app, space }: SpaceBarProps) {
  const host = useHost();
  const create = useCreateWorkspace();
  const [name, setName] = useState("Личное");
  const [template, setTemplate] = useState<WarehouseTemplate>("craft");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Название не может быть пустым.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const created = await create.mutateAsync({ body: { app, name: trimmed, ...(app === "warehouse" ? { template } : {}) } });
      space.select(created.id);
      host.haptic("success");
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card cv-form space-onboarding" onSubmit={handleSubmit} noValidate aria-label="Создание пространства">
      <h2 className="page__subtitle">Создайте пространство</h2>
      <p className="page__state">{SPACE_APPS[app].onboarding}</p>
      {error && <Notice tone="error">{error}</Notice>}
      <TextField label="Название" maxLength={80} value={name} onChange={(event) => setName(event.target.value)} />
      {app === "warehouse" && <TemplatePicker value={template} onChange={setTemplate} />}
      <Button type="submit" variant="primary" block loading={busy}>
        Начать
      </Button>
    </form>
  );
}

/** Switching, members and the settings of the current space. */
function SpaceSheet({ app, space, current, onClose }: SpaceBarProps & { current: Workspace; onClose: () => void }) {
  const host = useHost();
  const { user } = useAuth();
  const remove = useDeleteWorkspace();
  const [dialog, setDialog] = useState<"create" | "rename" | null>(null);
  const [armed, setArmed] = useState(false);
  const [deleting, setDeleting] = useState(false);
  useHostBackButton(onClose);

  async function handleDelete() {
    if (!armed) {
      host.haptic("selection");
      setArmed(true);
      return;
    }
    setDeleting(true);
    try {
      await remove.mutateAsync({ params: { path: { id: current.id } } });
      host.haptic("success");
      onClose();
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось удалить пространство: ${describeApiError(cause)}`);
    } finally {
      setArmed(false);
      setDeleting(false);
    }
  }

  // One sheet at a time: the dialog replaces the space sheet while it is open.
  if (dialog === "create") {
    return (
      <CreateWorkspaceDialog
        app={app}
        onClose={() => setDialog(null)}
        onCreated={(created) => {
          space.select(created.id);
          setDialog(null);
        }}
      />
    );
  }
  if (dialog === "rename") {
    return <RenameWorkspaceDialog workspace={current} onClose={() => setDialog(null)} />;
  }

  return (
    <Sheet title="Пространство" onClose={onClose}>
      <div className="cv-form">
        <ul className="card-list" aria-label="Мои пространства">
          {space.workspaces.map((item) => (
            <li key={item.id}>
              <Button
                block
                variant={item.id === current.id ? "solid" : "outline"}
                aria-pressed={item.id === current.id}
                onClick={() => {
                  host.haptic("selection");
                  setArmed(false);
                  space.select(item.id);
                }}
              >
                {item.name}
              </Button>
            </li>
          ))}
        </ul>
        <Button block onClick={() => setDialog("create")}>
          <IconPlus />
          Новое пространство
        </Button>

        <section className="page__section" aria-label={`Настройки: ${current.name}`}>
          <h2 className="page__subtitle">Участники «{current.name}»</h2>
          <p className="page__state">{SPACE_APPS[app].sharing}</p>
          <MembersList key={current.id} workspaceId={current.id} currentUserId={user.id} />
          <CandidatesSection key={`c-${current.id}`} workspaceId={current.id} />
          <div className="craft-actions">
            <Button size="s" onClick={() => setDialog("rename")}>
              Переименовать
            </Button>
            <Button size="s" variant="danger" loading={deleting} onClick={() => void handleDelete()}>
              {armed ? "Подтвердить удаление" : "Удалить пространство"}
            </Button>
          </div>
        </section>
      </div>
    </Sheet>
  );
}
