import { useState, type FormEvent } from "react";

import type { CraftItem, Project, ProjectMaterial } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Notice, Sheet, TextAreaField, TextField } from "../../ui/controls";
import {
  useCraftCatalog,
  useCreateProject,
  useDeleteProject,
  useSetProjectMaterial,
  useUpdateProject,
  useUseProjectMaterial,
} from "./data";
import { Swatch } from "./Swatch";
import { formatQty, itemMeta } from "./format";
import { plannedLabel } from "./project";

/** Props of the create dialog. */
export interface CreateProjectDialogProps {
  workspaceId: string;
  onClose: () => void;
}

/** Creates an empty project. */
export function CreateProjectDialog({ workspaceId, onClose }: CreateProjectDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const create = useCreateProject();
  const [name, setName] = useState("");
  const [notes, setNotes] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) {
      setError("Укажите название проекта.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await create.mutateAsync({ params: { path: { id: workspaceId } }, body: { name: name.trim(), notes: notes.trim() } });
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
    <Sheet title="Новый проект" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField label="Название" placeholder="Например: Серьги «Майорка»" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        <TextAreaField label="Заметки" placeholder="Что это за проект" value={notes} onChange={(event) => setNotes(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Создать проект
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the edit dialog. */
export interface EditProjectDialogProps {
  workspaceId: string;
  project: Project;
  onClose: () => void;
  /** Called instead of `onClose` after the project was deleted. */
  onDeleted?: () => void;
}

/** Changes the name and notes of a project; deletion is two taps. */
export function EditProjectDialog({ workspaceId, project, onClose, onDeleted }: EditProjectDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const update = useUpdateProject();
  const remove = useDeleteProject();
  const [name, setName] = useState(project.name);
  const [notes, setNotes] = useState(project.notes);
  const [armed, setArmed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) {
      setError("Укажите название проекта.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await update.mutateAsync({
        params: { path: { id: workspaceId, project_id: project.id } },
        body: { name: name.trim(), notes: notes.trim() },
      });
      host.haptic("success");
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  async function handleDelete() {
    if (!armed) {
      host.haptic("selection");
      setArmed(true);
      return;
    }
    setBusy(true);
    try {
      await remove.mutateAsync({ params: { path: { id: workspaceId, project_id: project.id } } });
      host.haptic("success");
      if (onDeleted) {
        onDeleted();
      } else {
        onClose();
      }
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setArmed(false);
      setBusy(false);
    }
  }

  return (
    <Sheet title="Изменить проект" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField label="Название" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        <TextAreaField label="Заметки" value={notes} onChange={(event) => setNotes(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить
        </Button>
      </form>
      <section className="page__section">
        <Button variant="danger" block loading={busy} onClick={() => void handleDelete()}>
          {armed ? "Подтвердить удаление" : "Удалить проект"}
        </Button>
      </section>
    </Sheet>
  );
}

/** Props of the add-material dialog. */
export interface AddMaterialDialogProps {
  workspaceId: string;
  projectId: string;
  /** Items already planned in the project; they are not offered again. */
  existingItemIds: string[];
  onClose: () => void;
}

/** Picks a catalogue item and plans a quantity of it. */
export function AddMaterialDialog({ workspaceId, projectId, existingItemIds, onClose }: AddMaterialDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const setMaterial = useSetProjectMaterial();
  const [search, setSearch] = useState("");
  const [qty, setQty] = useState("1");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const catalog = useCraftCatalog(workspaceId, { q: search, limit: 20 });
  const candidates = (catalog.data ?? []).filter((item) => !existingItemIds.includes(item.id));

  async function handleAdd(item: CraftItem) {
    const planned = Number(qty.trim().replace(",", "."));
    if (!Number.isFinite(planned) || planned < 0) {
      setError("Плановое количество не может быть отрицательным.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(item.id);
    try {
      await setMaterial.mutateAsync({
        params: { path: { id: workspaceId, project_id: projectId, item_id: item.id } },
        body: { qty_planned: planned },
      });
      host.haptic("success");
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(null);
    }
  }

  return (
    <Sheet title="Добавить материал" onClose={onClose}>
      <div className="cv-form">
        {error && (
          <Notice tone="error" title="Не получилось">
            {error}
          </Notice>
        )}
        <TextField
          label="Сколько запланировать"
          hint="Количество в единице позиции. Ноль тоже допустим."
          inputMode="decimal"
          mono
          value={qty}
          onChange={(event) => setQty(event.target.value)}
        />
        <TextField label="Поиск в каталоге" placeholder="Название, цвет или артикул" value={search} onChange={(event) => setSearch(event.target.value)} />
        {catalog.isPending && (
          <p className="page__state" role="status">
            Поиск…
          </p>
        )}
        {catalog.isSuccess && candidates.length === 0 && <p className="page__state">Подходящих позиций нет.</p>}
        {candidates.length > 0 && (
          <ul className="card-list" aria-label="Позиции каталога">
            {candidates.map((item) => (
              <li className="card craft-row" key={item.id}>
                <Swatch item={item} />
                <div className="craft-row__text">
                  <span className="craft-row__title">{item.name}</span>
                  <span className="craft-row__meta">
                    {[itemMeta(item), `${formatQty(item.stock_qty)} ${item.unit}`].filter(Boolean).join(" · ")}
                  </span>
                </div>
                <Button size="s" variant="solid" aria-label={`Добавить материал: ${item.name}`} loading={busy === item.id} onClick={() => void handleAdd(item)}>
                  Добавить
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  );
}

/** Props of the planned quantity dialog. */
export interface SetMaterialDialogProps {
  workspaceId: string;
  projectId: string;
  material: ProjectMaterial;
  onClose: () => void;
}

/** Changes or clears the planned quantity of one material. */
export function SetMaterialDialog({ workspaceId, projectId, material, onClose }: SetMaterialDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const setMaterial = useSetProjectMaterial();
  const [qty, setQty] = useState(material.qty_planned != null ? String(material.qty_planned) : "");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    let planned: number | null = null;
    if (qty.trim()) {
      const parsed = Number(qty.trim().replace(",", "."));
      if (!Number.isFinite(parsed) || parsed < 0) {
        setError("Плановое количество не может быть отрицательным.");
        host.haptic("error");
        return;
      }
      planned = parsed;
    }
    setError(null);
    setBusy(true);
    try {
      await setMaterial.mutateAsync({
        params: { path: { id: workspaceId, project_id: projectId, item_id: material.item.id } },
        body: { qty_planned: planned },
      });
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
    <Sheet title={`План: ${material.item.name}`} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <p className="page__state">
          {`Сейчас: ${plannedLabel(material)}. Пустое поле очищает план.`}
        </p>
        <TextField label={`Плановое количество, ${material.item.unit}`} inputMode="decimal" mono value={qty} onChange={(event) => setQty(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить план
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the use-material dialog. */
export interface UseMaterialDialogProps {
  workspaceId: string;
  projectId: string;
  material: ProjectMaterial;
  onClose: () => void;
}

/** Withdraws material from the stock for the project. */
export function UseMaterialDialog({ workspaceId, projectId, material, onClose }: UseMaterialDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const useMaterial = useUseProjectMaterial();
  const [qty, setQty] = useState("1");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parsed = Number(qty.trim().replace(",", "."));
    if (!Number.isFinite(parsed) || parsed <= 0) {
      setError("Количество должно быть положительным.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await useMaterial.mutateAsync({
        params: { path: { id: workspaceId, project_id: projectId, item_id: material.item.id } },
        body: { qty: parsed },
      });
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
    <Sheet title={`Использовать: ${material.item.name}`} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Не получилось">
            {error}
          </Notice>
        )}
        <p className="page__state">
          {`На складе: ${formatQty(material.item.stock_qty)} ${material.item.unit}. Остаток уменьшится, а использованное количество вырастет.`}
        </p>
        <TextField label={`Сколько использовать, ${material.item.unit}`} inputMode="decimal" mono value={qty} onChange={(event) => setQty(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Использовать
        </Button>
      </form>
    </Sheet>
  );
}
