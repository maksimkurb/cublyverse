import { useState } from "react";

import type { WorkspaceCandidate, WorkspaceMember } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useAuth } from "../../auth/AuthProvider";
import { useHost } from "../../host/HostContext";
import { usePageTitle } from "../../router/router";
import { Button, EmptyState, Notice, TextField } from "../../ui/controls";
import { IconPlus } from "../../ui/icons";
import {
  useAddWorkspaceMember,
  useDeleteWorkspace,
  useRemoveWorkspaceMember,
  useWorkspaceCandidates,
  useWorkspaceMembers,
} from "./data";
import { CreateWorkspaceDialog, RenameWorkspaceDialog } from "./WorkspaceDialogs";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { useCurrentWorkspace, writeStoredWorkspaceID } from "./selection";

/** Name to show for a user who has neither a display name nor an e-mail. */
function personName(person: { display_name: string; email: string }): string {
  return person.display_name || person.email || "Без имени";
}

/** Avatar of a member or candidate; the picture is decorative. */
function Avatar({ person }: { person: { avatar_url: string; display_name: string } }) {
  return <img className="avatar" src={person.avatar_url} alt="" loading="lazy" aria-hidden="true" />;
}

/** Members of the current workspace, with removal in two taps. */
function MembersList({ workspaceId, currentUserId }: { workspaceId: string; currentUserId: string }) {
  const host = useHost();
  const query = useWorkspaceMembers(workspaceId);
  const remove = useRemoveWorkspaceMember();
  const [armed, setArmed] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  async function handleRemove(member: WorkspaceMember) {
    if (armed !== member.user_id) {
      host.haptic("selection");
      setArmed(member.user_id);
      return;
    }
    setBusy(member.user_id);
    try {
      await remove.mutateAsync({ params: { path: { id: workspaceId, user_id: member.user_id } } });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось убрать участника: ${describeApiError(cause)}`);
    } finally {
      setArmed(null);
      setBusy(null);
    }
  }

  if (query.isPending) {
    return (
      <p className="page__state" role="status">
        Загрузка участников…
      </p>
    );
  }
  if (query.isError) {
    return (
      <Notice tone="error">
        <span>Не удалось загрузить участников: {query.error?.message ?? "неизвестная ошибка"}</span>
        <Button size="s" onClick={() => void query.refetch()}>
          Повторить
        </Button>
      </Notice>
    );
  }

  const members = query.data ?? [];
  return (
    <ul className="card-list" aria-label="Участники">
      {members.map((member) => {
        const self = member.user_id === currentUserId;
        return (
          <li className="card person" key={member.user_id}>
            <Avatar person={member} />
            <div className="person__text">
              <span className="person__name">
                {personName(member)}
                {self && <span className="badge person__badge">Вы</span>}
              </span>
              {member.email && <span className="person__meta">{member.email}</span>}
            </div>
            <Button
              size="s"
              variant="danger"
              loading={busy === member.user_id}
              onClick={() => void handleRemove(member)}
            >
              {armed === member.user_id ? "Подтвердить" : self ? "Выйти" : "Удалить"}
            </Button>
          </li>
        );
      })}
    </ul>
  );
}

/** Users who can still be added to the current workspace. */
function CandidatesSection({ workspaceId }: { workspaceId: string }) {
  const host = useHost();
  const [search, setSearch] = useState("");
  const query = useWorkspaceCandidates(workspaceId, search);
  const add = useAddWorkspaceMember();
  const [busy, setBusy] = useState<string | null>(null);

  async function handleAdd(candidate: WorkspaceCandidate) {
    setBusy(candidate.user_id);
    try {
      await add.mutateAsync({ params: { path: { id: workspaceId } }, body: { user_id: candidate.user_id } });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось добавить участника: ${describeApiError(cause)}`);
    } finally {
      setBusy(null);
    }
  }

  const candidates = query.data ?? [];

  return (
    <section className="page__section" aria-label="Добавить участника">
      <h2 className="page__subtitle">Добавить участника</h2>
      <TextField
        label="Найти пользователя"
        hint="Показаны те, кто уже входил в Cublyverse."
        placeholder="Имя или e-mail"
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      {query.isPending && (
        <p className="page__state" role="status">
          Поиск…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить список: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}
      {query.isSuccess && candidates.length === 0 && (
        <p className="page__state">
          {search.trim() ? "Никого не найдено по этому запросу." : "Некого добавить: все, кто входил, уже здесь."}
        </p>
      )}
      {candidates.length > 0 && (
        <ul className="card-list" aria-label="Кого можно добавить">
          {candidates.map((candidate) => (
            <li className="card person" key={candidate.user_id}>
              <Avatar person={candidate} />
              <div className="person__text">
                <span className="person__name">{personName(candidate)}</span>
                {candidate.email && <span className="person__meta">{candidate.email}</span>}
              </div>
              <Button
                size="s"
                variant="solid"
                aria-label={`Добавить: ${personName(candidate)}`}
                loading={busy === candidate.user_id}
                onClick={() => void handleAdd(candidate)}
              >
                Добавить
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * «Участники»: the workspace switcher, member list, candidate search and the
 * workspace itself — create, rename and delete. Leaving is the same operation
 * as removing yourself.
 */
export default function MembersScreen() {
  const host = useHost();
  const { user } = useAuth();
  usePageTitle("Участники");

  const { query, workspaces, current, select } = useCurrentWorkspace();
  const removeWorkspace = useDeleteWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteArmed, setDeleteArmed] = useState(false);
  const [deleting, setDeleting] = useState(false);

  async function handleDelete() {
    if (!current) {
      return;
    }
    if (!deleteArmed) {
      host.haptic("selection");
      setDeleteArmed(true);
      return;
    }
    setDeleting(true);
    try {
      await removeWorkspace.mutateAsync({ params: { path: { id: current.id } } });
      writeStoredWorkspaceID(null);
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось удалить пространство: ${describeApiError(cause)}`);
    } finally {
      setDeleteArmed(false);
      setDeleting(false);
    }
  }

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Участники</h1>
        <Button variant="primary" aria-label="Новое пространство" onClick={() => setCreateOpen(true)}>
          <IconPlus />
          Новое
        </Button>
      </header>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}

      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить пространства: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}

      {query.isSuccess && workspaces.length === 0 && (
        <EmptyState title="Нет пространств">
          Создайте пространство — например, «Дом» или «Мастерская» — и пригласите тех, с кем ведёте запасы.
        </EmptyState>
      )}

      {current && (
        <>
          <WorkspaceSwitcher workspaces={workspaces} currentId={current.id} onChange={select} />

          <section className="card ws-current" aria-label={`Пространство ${current.name}`}>
            <div className="ws-current__head">
              <div>
                <span className="cv-eyebrow">Пространство</span>
                <h2 className="ws-current__name">{current.name}</h2>
              </div>
              <div className="ws-current__actions">
                <Button size="s" onClick={() => setRenameOpen(true)}>
                  Переименовать
                </Button>
                <Button size="s" variant="danger" loading={deleting} onClick={() => void handleDelete()}>
                  {deleteArmed ? "Подтвердить удаление" : "Удалить"}
                </Button>
              </div>
            </div>
            <p className="page__state">Все участники равны: любой может менять каталог, склад и состав пространства.</p>
          </section>

          <section className="page__section" aria-label="Состав пространства">
            <h2 className="page__subtitle">Состав</h2>
            <MembersList workspaceId={current.id} currentUserId={user.id} />
          </section>

          <CandidatesSection workspaceId={current.id} />
        </>
      )}

      {createOpen && (
        <CreateWorkspaceDialog
          onClose={() => setCreateOpen(false)}
          onCreated={(workspace) => {
            select(workspace.id);
            setCreateOpen(false);
          }}
        />
      )}

      {renameOpen && current && (
        <RenameWorkspaceDialog workspace={current} onClose={() => setRenameOpen(false)} />
      )}
    </div>
  );
}
