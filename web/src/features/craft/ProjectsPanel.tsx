import { useState } from "react";

import type { ProjectMaterial } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, EmptyState, Notice } from "../../ui/controls";
import { IconPlus } from "../../ui/icons";
import { useProjects, useProject, useRemoveProjectMaterial, useShortageToWishlist } from "./data";
import { itemMeta, swatchStyle } from "./format";
import { AddMaterialDialog, CreateProjectDialog, EditProjectDialog, SetMaterialDialog, UseMaterialDialog } from "./ProjectDialogs";
import { materialsCountLabel, plannedLabel, shortageLabel, stockLabel, usedLabel } from "./project";
import { addedWishesLabel } from "./wishlist";

/** One material of a project with its plan, usage, stock and shortage. */
function MaterialRow({
  material,
  busy,
  onUse,
  onPlan,
  onRemove,
}: {
  material: ProjectMaterial;
  busy: boolean;
  onUse: (material: ProjectMaterial) => void;
  onPlan: (material: ProjectMaterial) => void;
  onRemove: (material: ProjectMaterial) => void;
}) {
  const name = material.item.name;
  return (
    <li className="card material">
      <div className="material__head">
        <span className="craft-swatch" style={swatchStyle(material.item)} aria-hidden="true" />
        <div className="craft-row__text">
          <span className="craft-row__title">{name}</span>
          <span className="craft-row__meta">{[itemMeta(material.item), material.item.category_name].filter(Boolean).join(" · ")}</span>
        </div>
      </div>

      <div className="material__figures">
        <span>
          План: <strong>{plannedLabel(material)}</strong>
        </span>
        <span>
          Использовано: <strong>{usedLabel(material)}</strong>
        </span>
        <span>
          На складе: <strong>{stockLabel(material)}</strong>
        </span>
      </div>

      {material.shortage > 0 && <span className="material__shortage">Нужно купить: {shortageLabel(material)}</span>}

      <div className="wish-actions">
        <Button size="s" variant="solid" aria-label={`Использовать: ${name}`} onClick={() => onUse(material)}>
          Использовать
        </Button>
        <Button size="s" aria-label={`Изменить план: ${name}`} onClick={() => onPlan(material)}>
          План
        </Button>
        <Button size="s" variant="danger" aria-label={`Убрать из проекта: ${name}`} loading={busy} onClick={() => onRemove(material)}>
          Убрать
        </Button>
      </div>
    </li>
  );
}

/** One project page: the plan, its materials and the ways to change them. */
function ProjectDetail({ workspaceId, projectId, onBack }: { workspaceId: string; projectId: string; onBack: () => void }) {
  useHostBackButton(onBack);
  const host = useHost();
  const query = useProject(workspaceId, projectId);
  const shortage = useShortageToWishlist();
  const removeMaterial = useRemoveProjectMaterial();

  const [addOpen, setAddOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [using, setUsing] = useState<ProjectMaterial | null>(null);
  const [planning, setPlanning] = useState<ProjectMaterial | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const project = query.data;

  async function handleShortage() {
    setNotice(null);
    try {
      const added = await shortage.mutateAsync({ params: { path: { id: workspaceId, project_id: projectId } } });
      setNotice(addedWishesLabel(added.length));
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось добавить в закупки: ${describeApiError(cause)}`);
    }
  }

  async function handleRemove(material: ProjectMaterial) {
    const confirmed = await host.showConfirm(`Убрать «${material.item.name}» из проекта? Остаток не изменится.`);
    if (!confirmed) {
      return;
    }
    setRemoving(material.item.id);
    try {
      await removeMaterial.mutateAsync({
        params: { path: { id: workspaceId, project_id: projectId, item_id: material.item.id } },
      });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось убрать материал: ${describeApiError(cause)}`);
    } finally {
      setRemoving(null);
    }
  }

  return (
    <section className="page__section" aria-label="Проект">
      <div className="project-detail__head">
        <Button size="s" aria-label="К списку проектов" onClick={onBack}>
          ← Проекты
        </Button>
      </div>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить проект: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}

      {project && (
        <>
          <header className="project-detail__title">
            <h2 className="page__title">{project.name}</h2>
            <span className="wish-card__meta">{materialsCountLabel(project.material_count)}</span>
          </header>
          {project.notes && <p className="page__state">{project.notes}</p>}

          <div className="craft-actions">
            <Button variant="primary" onClick={() => setAddOpen(true)}>
              <IconPlus />
              Добавить материал
            </Button>
            <Button onClick={() => void handleShortage()} loading={shortage.isPending}>
              Недостающее в закупки
            </Button>
            <Button aria-label="Изменить проект" onClick={() => setEditOpen(true)}>
              Изменить
            </Button>
          </div>

          {notice && (
            <p className="page__state" role="status">
              {notice}
            </p>
          )}

          {project.materials.length === 0 ? (
            <EmptyState title="Материалов пока нет">
              Добавьте позиции каталога в план: для каждой будут видны план, использование, остаток и дефицит.
            </EmptyState>
          ) : (
            <ul className="card-list" aria-label="Материалы проекта">
              {project.materials.map((material) => (
                <MaterialRow
                  key={material.item.id}
                  material={material}
                  busy={removing === material.item.id}
                  onUse={setUsing}
                  onPlan={setPlanning}
                  onRemove={(value) => void handleRemove(value)}
                />
              ))}
            </ul>
          )}

          {addOpen && (
            <AddMaterialDialog
              workspaceId={workspaceId}
              projectId={projectId}
              existingItemIds={project.materials.map((material) => material.item.id)}
              onClose={() => setAddOpen(false)}
            />
          )}
          {using && <UseMaterialDialog workspaceId={workspaceId} projectId={projectId} material={using} onClose={() => setUsing(null)} />}
          {planning && <SetMaterialDialog workspaceId={workspaceId} projectId={projectId} material={planning} onClose={() => setPlanning(null)} />}
          {editOpen && (
            <EditProjectDialog
              workspaceId={workspaceId}
              project={project}
              onClose={() => setEditOpen(false)}
              onDeleted={onBack}
            />
          )}
        </>
      )}
    </section>
  );
}

/** The project list of one workspace; opening an entry shows its page. */
export default function ProjectsPanel({ workspaceId }: { workspaceId: string }) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const query = useProjects(workspaceId);

  if (selectedId) {
    return <ProjectDetail workspaceId={workspaceId} projectId={selectedId} onBack={() => setSelectedId(null)} />;
  }

  const projects = query.data ?? [];

  return (
    <section className="page__section" aria-label="Проекты">
      <div className="craft-actions">
        <Button variant="primary" onClick={() => setCreateOpen(true)}>
          <IconPlus />
          Новый проект
        </Button>
      </div>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить проекты: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}
      {query.isSuccess && projects.length === 0 && (
        <EmptyState title="Проектов пока нет">
          Создайте проект — например, «Серьги» — и запланируйте материалы: приложение посчитает, чего не хватает.
        </EmptyState>
      )}

      {projects.length > 0 && (
        <ul className="card-list" aria-label="Проекты">
          {projects.map((project) => (
            <li className="card project-card" key={project.id}>
              <button type="button" className="project-card__open" onClick={() => setSelectedId(project.id)}>
                <span className="project-card__title">{project.name}</span>
                <span className="project-card__meta">{materialsCountLabel(project.material_count)}</span>
                {project.notes && <span className="project-card__notes">{project.notes}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}

      {createOpen && <CreateProjectDialog workspaceId={workspaceId} onClose={() => setCreateOpen(false)} />}
    </section>
  );
}
