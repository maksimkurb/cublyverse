import { useState, type FormEvent } from "react";

import type { CraftCategory, CraftField, CraftReference } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Checkbox, Notice, Segmented, SelectField, Sheet, TextField } from "../../ui/controls";
import { IconPlus } from "../../ui/icons";
import { useCraftReference, useReferenceMutations } from "./data";

type Section = "categories" | "manufacturers" | "units" | "catalogs";

const SECTIONS: ReadonlyArray<{ value: Section; label: string }> = [
  { value: "categories", label: "Категории" },
  { value: "manufacturers", label: "Бренды" },
  { value: "units", label: "Единицы" },
  { value: "catalogs", label: "Каталоги" },
];

const FIELD_TYPES: ReadonlyArray<{ value: CraftField["type"]; label: string }> = [
  { value: "text", label: "Текст" },
  { value: "number", label: "Число" },
  { value: "bool", label: "Да / нет" },
  { value: "select", label: "Список" },
];

/** What the editor of one entry is working on; an absent entry means a new one. */
type Editing =
  | { kind: "category"; category?: CraftCategory }
  | { kind: "manufacturer"; id?: string; name: string; website: string }
  | { kind: "unit"; id?: string; name: string }
  | null;

/** The button and sheet that configure a warehouse. */
export function WarehouseSettings({ workspaceId }: { workspaceId: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="space-bar">
      <Button size="s" onClick={() => setOpen(true)}>
        Настройки склада
      </Button>
      {open && <SettingsSheet workspaceId={workspaceId} onClose={() => setOpen(false)} />}
    </div>
  );
}

function SettingsSheet({ workspaceId, onClose }: { workspaceId: string; onClose: () => void }) {
  useHostBackButton(onClose);
  const [section, setSection] = useState<Section>("categories");
  const [editing, setEditing] = useState<Editing>(null);
  const query = useCraftReference(workspaceId);

  // One sheet at a time: an editor replaces the settings while it is open.
  if (editing?.kind === "category") {
    return (
      <CategoryEditor
        workspaceId={workspaceId}
        category={editing.category}
        units={query.data?.units.map((unit) => unit.name) ?? []}
        onClose={() => setEditing(null)}
      />
    );
  }
  if (editing) {
    return <NameEditor workspaceId={workspaceId} editing={editing} onClose={() => setEditing(null)} />;
  }

  return (
    <Sheet title="Настройки склада" onClose={onClose}>
      <div className="cv-form">
        <Segmented label="Что настроить" value={section} options={SECTIONS} onChange={setSection} />
        {query.isPending && (
          <p className="page__state" role="status">
            Загрузка…
          </p>
        )}
        {query.isError && <Notice tone="error">Не удалось загрузить настройки: {describeApiError(query.error)}</Notice>}
        {query.data && <SectionList workspaceId={workspaceId} section={section} reference={query.data} onEdit={setEditing} />}
      </div>
    </Sheet>
  );
}

/** One row of a settings list. */
function Row({ title, meta, onEdit, onDelete }: { title: string; meta: string; onEdit?: () => void; onDelete?: () => void }) {
  return (
    <li className="card settings-row">
      <div className="craft-row__text">
        <span className="craft-row__title">{title}</span>
        {meta && <span className="craft-row__meta">{meta}</span>}
      </div>
      <div className="wish-actions">
        {onEdit && (
          <Button size="s" aria-label={`Изменить: ${title}`} onClick={onEdit}>
            Изменить
          </Button>
        )}
        {onDelete && (
          <Button size="s" variant="danger" aria-label={`Удалить: ${title}`} onClick={onDelete}>
            Удалить
          </Button>
        )}
      </div>
    </li>
  );
}

function SectionList({
  workspaceId,
  section,
  reference,
  onEdit,
}: {
  workspaceId: string;
  section: Section;
  reference: CraftReference;
  onEdit: (editing: Editing) => void;
}) {
  const host = useHost();
  const mutations = useReferenceMutations();

  async function remove(name: string, run: () => Promise<unknown>) {
    if (!(await host.showConfirm(`Удалить «${name}»?`))) {
      return;
    }
    try {
      await run();
      host.haptic("success");
    } catch (cause) {
      // The server says why an entry in use cannot be deleted.
      host.haptic("error");
      await host.showAlert(describeApiError(cause));
    }
  }

  const path = (refId: string) => ({ params: { path: { id: workspaceId, ref_id: refId } } });

  if (section === "catalogs") {
    const enabled = reference.catalogs.filter((entry) => entry.enabled).map((entry) => entry.source);
    return (
      <>
        <p className="page__state">Официальные каталоги только читаются. Отключение не убирает то, что уже есть в запасах.</p>
        {reference.catalogs.map((entry) => (
          <label className="scopes__item" key={entry.source}>
            <Checkbox
              checked={entry.enabled}
              onCheckedChange={(checked) =>
                void mutations.setCatalogs
                  .mutateAsync({
                    params: { path: { id: workspaceId } },
                    body: { sources: checked ? [...enabled, entry.source] : enabled.filter((source) => source !== entry.source) },
                  })
                  .catch((cause: unknown) => host.showAlert(describeApiError(cause)))
              }
            />
            <span className="scopes__label">
              {entry.name} · позиций: {entry.items}
            </span>
          </label>
        ))}
      </>
    );
  }

  const add: Record<Exclude<Section, "catalogs">, { label: string; editing: Editing }> = {
    categories: { label: "Новая категория", editing: { kind: "category" } },
    manufacturers: { label: "Новый производитель", editing: { kind: "manufacturer", name: "", website: "" } },
    units: { label: "Новая единица", editing: { kind: "unit", name: "" } },
  };

  return (
    <>
      <Button block onClick={() => onEdit(add[section].editing)}>
        <IconPlus />
        {add[section].label}
      </Button>
      <ul className="card-list" aria-label={SECTIONS.find((entry) => entry.value === section)?.label}>
        {section === "categories" &&
          reference.categories.map((category) => (
            <Row
              key={category.id}
              title={category.name}
              meta={[
                category.default_unit,
                category.fields.length > 0 ? `полей: ${category.fields.length}` : "",
                category.catalog_slug ? "официальный каталог" : "",
              ]
                .filter(Boolean)
                .join(" · ")}
              onEdit={() => onEdit({ kind: "category", category })}
              onDelete={() => void remove(category.name, () => mutations.deleteCategory.mutateAsync(path(category.id)))}
            />
          ))}
        {section === "manufacturers" &&
          reference.manufacturers.map((maker) => (
            <Row
              key={maker.id}
              title={maker.name}
              meta={maker.official ? "из официального каталога" : maker.website}
              onEdit={
                maker.official
                  ? undefined
                  : () => onEdit({ kind: "manufacturer", id: maker.id, name: maker.name, website: maker.website })
              }
              onDelete={
                maker.official ? undefined : () => void remove(maker.name, () => mutations.deleteManufacturer.mutateAsync(path(maker.id)))
              }
            />
          ))}
        {section === "units" &&
          reference.units.map((unit) => (
            <Row
              key={unit.id}
              title={unit.name}
              meta=""
              onEdit={() => onEdit({ kind: "unit", id: unit.id, name: unit.name })}
              onDelete={() => void remove(unit.name, () => mutations.deleteUnit.mutateAsync(path(unit.id)))}
            />
          ))}
      </ul>
    </>
  );
}

/** Editor of a manufacturer or a unit. */
function NameEditor({
  workspaceId,
  editing,
  onClose,
}: {
  workspaceId: string;
  editing: Exclude<Editing, null | { kind: "category" }>;
  onClose: () => void;
}) {
  useHostBackButton(onClose);
  const mutations = useReferenceMutations();
  const [name, setName] = useState(editing.name);
  const [website, setWebsite] = useState(editing.kind === "manufacturer" ? editing.website : "");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const noun = editing.kind === "unit" ? "единица" : "производитель";

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const create = { path: { id: workspaceId } };
    try {
      if (editing.kind === "unit") {
        const body = { name: name.trim() };
        await (editing.id
          ? mutations.updateUnit.mutateAsync({ params: { path: { id: workspaceId, ref_id: editing.id } }, body })
          : mutations.createUnit.mutateAsync({ params: create, body }));
      } else {
        const body = { name: name.trim(), website: website.trim() };
        await (editing.id
          ? mutations.updateManufacturer.mutateAsync({ params: { path: { id: workspaceId, ref_id: editing.id } }, body })
          : mutations.createManufacturer.mutateAsync({ params: create, body }));
      }
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title={editing.id ? `Изменить: ${noun}` : `Добавить: ${noun}`} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && <Notice tone="error">{error}</Notice>}
        <TextField label="Название" value={name} onChange={(event) => setName(event.target.value)} />
        {editing.kind === "manufacturer" && (
          <TextField label="Сайт" placeholder="https://… необязательно" value={website} onChange={(event) => setWebsite(event.target.value)} />
        )}
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить
        </Button>
      </form>
    </Sheet>
  );
}

/** One extra field while it is edited; options are typed comma separated. */
interface FieldDraft {
  id: number;
  key?: string;
  label: string;
  type: CraftField["type"];
  options: string;
}

/** Editor of a category with its extra fields. */
function CategoryEditor({
  workspaceId,
  category,
  units,
  onClose,
}: {
  workspaceId: string;
  category?: CraftCategory;
  units: string[];
  onClose: () => void;
}) {
  useHostBackButton(onClose);
  const mutations = useReferenceMutations();
  const [name, setName] = useState(category?.name ?? "");
  const [unit, setUnit] = useState(category?.default_unit ?? units[0] ?? "");
  const [fields, setFields] = useState<FieldDraft[]>(() =>
    (category?.fields ?? []).map((field, index) => ({
      id: index,
      key: field.key,
      label: field.label,
      type: field.type,
      options: (field.options ?? []).join(", "),
    })),
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function change(id: number, patch: Partial<FieldDraft>) {
    setFields((current) => current.map((field) => (field.id === id ? { ...field, ...patch } : field)));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const body = {
      name: name.trim(),
      default_unit: unit,
      fields: fields.map((field) => ({
        key: field.key,
        label: field.label.trim(),
        type: field.type,
        options: field.type === "select" ? field.options.split(",").map((option) => option.trim()).filter(Boolean) : undefined,
      })),
    };
    try {
      await (category
        ? mutations.updateCategory.mutateAsync({ params: { path: { id: workspaceId, ref_id: category.id } }, body })
        : mutations.createCategory.mutateAsync({ params: { path: { id: workspaceId } }, body }));
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title={category ? "Изменить категорию" : "Новая категория"} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && <Notice tone="error">{error}</Notice>}
        <TextField label="Название категории" maxLength={80} value={name} onChange={(event) => setName(event.target.value)} />
        <SelectField label="Единица по умолчанию" value={unit} onChange={(event) => setUnit(event.target.value)}>
          {units.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </SelectField>

        <h2 className="page__subtitle">Свои поля</h2>
        {fields.map((field, index) => (
          <fieldset className="card settings-field" key={field.id} aria-label={`Поле ${index + 1}`}>
            <TextField label="Название поля" value={field.label} onChange={(event) => change(field.id, { label: event.target.value })} />
            <SelectField label="Тип" value={field.type} onChange={(event) => change(field.id, { type: event.target.value as CraftField["type"] })}>
              {FIELD_TYPES.map((entry) => (
                <option key={entry.value} value={entry.value}>
                  {entry.label}
                </option>
              ))}
            </SelectField>
            {field.type === "select" && (
              <TextField label="Варианты через запятую" value={field.options} onChange={(event) => change(field.id, { options: event.target.value })} />
            )}
            <Button size="s" variant="danger" onClick={() => setFields((current) => current.filter((entry) => entry.id !== field.id))}>
              Убрать поле
            </Button>
          </fieldset>
        ))}
        <Button
          block
          onClick={() =>
            setFields((current) => [
              ...current,
              { id: Math.max(0, ...current.map((entry) => entry.id)) + 1, label: "", type: "text", options: "" },
            ])
          }
        >
          <IconPlus />
          Добавить поле
        </Button>
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить категорию
        </Button>
      </form>
    </Sheet>
  );
}
