import { useEffect, useId, useMemo, useState, type FormEvent } from "react";

import type { CraftItem, CraftStockEntry } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Checkbox, Notice, SelectField, Sheet, TextAreaField, TextField } from "../../ui/controls";
import {
  useCraftCatalog,
  useCraftReference,
  useCreateCraftItem,
  useLinkCraftItem,
  useRemoveCraftStock,
  useSetCraftStock,
  useUpdateCraftItem,
  useUpdateCraftStockMeta,
} from "./data";
import { Swatch } from "./Swatch";
import { formatQty, itemMeta, normalizeHex, swatchStyle } from "./format";

/** Props of the stock edit dialog. */
export interface StockEditDialogProps {
  workspaceId: string;
  entry: CraftStockEntry;
  onClose: () => void;
}

/**
 * Exact-value edit of one stock row: quantity, location and notes. Removing the
 * row is the second tap of the danger button.
 */
export function StockEditDialog({ workspaceId, entry, onClose }: StockEditDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const setStock = useSetCraftStock();
  const setMeta = useUpdateCraftStockMeta();
  const remove = useRemoveCraftStock();

  const [qty, setQtyValue] = useState(formatQty(entry.qty).replace(/\s/g, ""));
  const [location, setLocation] = useState(entry.location);
  const [notes, setNotes] = useState(entry.notes);
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = Number(qty.replace(",", "."));
    if (!Number.isFinite(value) || value < 0) {
      setError("Остаток не может быть отрицательным.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await setStock.mutateAsync({
        params: { path: { id: workspaceId, item_id: entry.item.id } },
        body: { qty: value, reason: "Ручная правка остатка" },
      });
      await setMeta.mutateAsync({
        params: { path: { id: workspaceId, item_id: entry.item.id } },
        body: { location: location.trim(), notes: notes.trim() },
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

  async function handleRemove() {
    if (!armed) {
      host.haptic("selection");
      setArmed(true);
      return;
    }
    setBusy(true);
    try {
      await remove.mutateAsync({ params: { path: { id: workspaceId, item_id: entry.item.id } } });
      host.haptic("success");
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setArmed(false);
      setBusy(false);
    }
  }

  return (
    <Sheet title={`Остаток: ${entry.item.name}`} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField
          label={`Количество, ${entry.item.unit}`}
          inputMode="decimal"
          mono
          value={qty}
          onChange={(event) => setQtyValue(event.target.value)}
        />
        <TextField label="Где хранится" placeholder="Например: Ящик 2" value={location} onChange={(event) => setLocation(event.target.value)} />
        <TextAreaField label="Заметки" value={notes} onChange={(event) => setNotes(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить
        </Button>
        <Button variant="danger" block loading={busy} onClick={() => void handleRemove()}>
          {armed ? "Подтвердить удаление со склада" : "Убрать со склада"}
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the own item dialog. */
export interface CraftItemDialogProps {
  workspaceId: string;
  /** The own item to change; absent to create one. */
  item?: CraftItem;
  onClose: () => void;
  /** Receives the saved item. */
  onSaved: (item: CraftItem) => void;
}

/** Text of a stored attribute for an input. */
function attrText(value: unknown): string {
  return value === undefined || value === null ? "" : String(value);
}

/**
 * Creates or changes an own item. The category decides the default unit and
 * which extra fields the form has.
 */
export function CraftItemDialog({ workspaceId, item, onClose, onSaved }: CraftItemDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const reference = useCraftReference(workspaceId);
  const create = useCreateCraftItem();
  const update = useUpdateCraftItem();

  const [category, setCategory] = useState(item?.category ?? "");
  const [name, setName] = useState(item?.name ?? "");
  const [manufacturer, setManufacturer] = useState(item?.manufacturer ?? "");
  const [article, setArticle] = useState(item?.article ?? "");
  const [colorName, setColorName] = useState(item?.color_name ?? "");
  const [colorHex, setColorHex] = useState(item?.color_hex ?? "");
  const [size, setSize] = useState(item?.size ?? "");
  const [imageUrl, setImageUrl] = useState(item?.image_url ?? "");
  const [unit, setUnit] = useState(item?.unit ?? "");
  const [attrs, setAttrs] = useState<Record<string, unknown>>(item?.attrs ?? {});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const manufacturersListId = useId();

  const categories = useMemo(() => reference.data?.categories ?? [], [reference.data]);
  const units = reference.data?.units ?? [];
  const selected = categories.find((entry) => entry.slug === category);

  // The first category is a sensible default; the unit follows the category.
  useEffect(() => {
    if (!category && categories.length > 0) {
      setCategory(categories[0].slug);
      setUnit(categories[0].default_unit);
    }
  }, [categories, category]);

  function handleCategoryChange(slug: string) {
    setCategory(slug);
    const next = categories.find((entry) => entry.slug === slug);
    if (next) {
      setUnit(next.default_unit);
    }
  }

  function setAttr(key: string, value: unknown) {
    setAttrs((current) => ({ ...current, [key]: value }));
  }

  /** The stored attributes with the values of the category's fields typed. */
  function typedAttrs(): Record<string, unknown> | string {
    const result: Record<string, unknown> = { ...attrs };
    for (const field of selected?.fields ?? []) {
      const key = field.key ?? "";
      const value = result[key];
      if (value === undefined || value === "") {
        delete result[key];
      } else if (field.type === "number") {
        const parsed = Number(String(value).replace(",", "."));
        if (!Number.isFinite(parsed)) {
          return `Поле «${field.label}» должно быть числом.`;
        }
        result[key] = parsed;
      }
    }
    return result;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fail = (message: string) => {
      setError(message);
      host.haptic("error");
    };
    if (!category) {
      fail("Выберите категорию.");
      return;
    }
    if (!name.trim() && !article.trim()) {
      fail("Укажите название или артикул.");
      return;
    }
    const hex = normalizeHex(colorHex);
    if (hex === null) {
      fail("Цвет должен быть в формате #RRGGBB.");
      return;
    }
    const values = typedAttrs();
    if (typeof values === "string") {
      fail(values);
      return;
    }
    setError(null);
    setBusy(true);
    const body = {
      category,
      name: name.trim() || undefined,
      manufacturer: manufacturer.trim() || undefined,
      article: article.trim() || undefined,
      color_name: colorName.trim() || undefined,
      color_hex: hex || undefined,
      size: size.trim() || undefined,
      unit,
      image_url: imageUrl.trim() || undefined,
      attrs: values,
    };
    try {
      // A change replaces the item, so fields without an input are sent back.
      const saved = item
        ? await update.mutateAsync({
            params: { path: { id: workspaceId, item_id: item.id } },
            body: {
              ...body,
              series: item.series || undefined,
              finish: item.finish || undefined,
              pack_size: item.pack_size,
              color_source:
                hex && hex === item.color_hex && (item.color_source === "official" || item.color_source === "approx")
                  ? (item.color_source as "official" | "approx")
                  : undefined,
            },
          })
        : await create.mutateAsync({ params: { path: { id: workspaceId } }, body });
      host.haptic("success");
      onSaved(saved);
    } catch (cause) {
      fail(describeApiError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title={item ? "Изменить позицию" : "Новая позиция"} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        {reference.isSuccess && categories.length === 0 && (
          <Notice tone="error">В складе нет категорий. Создайте первую в «Настройках склада».</Notice>
        )}
        <SelectField label="Категория" value={category} onChange={(event) => handleCategoryChange(event.target.value)}>
          {categories.map((entry) => (
            <option key={entry.slug} value={entry.slug}>
              {entry.name}
            </option>
          ))}
        </SelectField>
        <TextField label="Название" placeholder="Например: Жемчуг айвори 2 мм" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        <TextField label="Производитель" list={manufacturersListId} placeholder="Выберите или впишите нового" value={manufacturer} onChange={(event) => setManufacturer(event.target.value)} />
        <datalist id={manufacturersListId}>
          {(reference.data?.manufacturers ?? []).map((entry) => (
            <option key={entry.id} value={entry.name} />
          ))}
        </datalist>
        <TextField label="Артикул" value={article} onChange={(event) => setArticle(event.target.value)} />
        <div className="craft-color">
          <TextField label="Цвет" placeholder="#RRGGBB, необязательно" mono value={colorHex} onChange={(event) => setColorHex(event.target.value)} />
          <span className="craft-swatch craft-swatch--large" style={swatchStyle({ color_hex: normalizeHex(colorHex) ?? "" })} aria-hidden="true" />
        </div>
        <TextField label="Название цвета" placeholder="Например: айвори" value={colorName} onChange={(event) => setColorName(event.target.value)} />
        <TextField label="Размер" placeholder="Необязательно" value={size} onChange={(event) => setSize(event.target.value)} />
        <TextField label="Ссылка на фото" type="url" placeholder="https://… необязательно" value={imageUrl} onChange={(event) => setImageUrl(event.target.value)} />
        <SelectField label="Единица" hint={`По умолчанию для категории: ${selected?.default_unit ?? "—"}`} value={unit} onChange={(event) => setUnit(event.target.value)}>
          {units.map((entry) => (
            <option key={entry.id} value={entry.name}>
              {entry.name}
            </option>
          ))}
        </SelectField>
        {(selected?.fields ?? []).map((field) => {
          const key = field.key ?? "";
          if (field.type === "bool") {
            return (
              <label className="scopes__item" key={key}>
                <Checkbox checked={attrs[key] === true} onCheckedChange={(checked) => setAttr(key, checked)} />
                <span className="scopes__label">{field.label}</span>
              </label>
            );
          }
          if (field.type === "select") {
            return (
              <SelectField key={key} label={field.label} value={attrText(attrs[key])} onChange={(event) => setAttr(key, event.target.value)}>
                <option value="">—</option>
                {(field.options ?? []).map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </SelectField>
            );
          }
          return (
            <TextField
              key={key}
              label={field.label}
              inputMode={field.type === "number" ? "decimal" : undefined}
              value={attrText(attrs[key])}
              onChange={(event) => setAttr(key, event.target.value)}
            />
          );
        })}
        <Button type="submit" variant="primary" block loading={busy} disabled={categories.length === 0}>
          {item ? "Сохранить" : "Создать позицию"}
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the link dialog. */
export interface LinkCraftItemDialogProps {
  workspaceId: string;
  item: CraftItem;
  onClose: () => void;
}

/**
 * Links a private item to a catalogue item: the stock moves to the target and
 * the private entry disappears.
 */
export function LinkCraftItemDialog({ workspaceId, item, onClose }: LinkCraftItemDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const [search, setSearch] = useState("");
  const query = useCraftCatalog(workspaceId, { q: search, category: item.category, limit: 20 });
  const link = useLinkCraftItem();
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const candidates = (query.data ?? []).filter((candidate) => candidate.id !== item.id);

  async function handleLink(target: CraftItem) {
    setBusy(target.id);
    setError(null);
    try {
      await link.mutateAsync({
        params: { path: { id: workspaceId, item_id: item.id } },
        body: { target_item_id: target.id },
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
    <Sheet title="Связать с каталогом" onClose={onClose}>
      <div className="cv-form">
        <p className="page__state">
          Остаток «{item.name}» перейдёт к выбранной позиции, а частная запись исчезнет. Единицы должны совпадать: {item.unit}.
        </p>
        {error && (
          <Notice tone="error" title="Не получилось связать">
            {error}
          </Notice>
        )}
        <TextField label="Поиск в каталоге" placeholder="Название или артикул" value={search} onChange={(event) => setSearch(event.target.value)} />
        {query.isPending && (
          <p className="page__state" role="status">
            Поиск…
          </p>
        )}
        {query.isSuccess && candidates.length === 0 && <p className="page__state">Подходящих позиций нет.</p>}
        {candidates.length > 0 && (
          <ul className="card-list">
            {candidates.map((candidate) => (
              <li className="card craft-row" key={candidate.id}>
                <Swatch item={candidate} />
                <div className="craft-row__text">
                  <span className="craft-row__title">{candidate.name}</span>
                  <span className="craft-row__meta">
                    {[itemMeta(candidate), `${formatQty(candidate.stock_qty)} ${candidate.unit}`].filter(Boolean).join(" · ")}
                  </span>
                </div>
                <Button
                  size="s"
                  variant="solid"
                  aria-label={`Связать: ${candidate.name}`}
                  loading={busy === candidate.id}
                  onClick={() => void handleLink(candidate)}
                >
                  Связать
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  );
}
