import { useEffect, useMemo, useState, type FormEvent } from "react";

import type { CraftItem, CraftStockEntry } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Notice, SelectField, Sheet, TextAreaField, TextField } from "../../ui/controls";
import {
  useCraftCatalog,
  useCraftCategories,
  useCreateCraftItem,
  useLinkCraftItem,
  useRemoveCraftStock,
  useSetCraftStock,
  useUpdateCraftStockMeta,
} from "./data";
import { UNITS, formatQty, itemMeta, normalizeHex, swatchStyle } from "./format";

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

/** Props of the private item dialog. */
export interface CreateCraftItemDialogProps {
  workspaceId: string;
  onClose: () => void;
  onCreated: () => void;
}

/** Creates a private catalogue item; the form is shaped by the category. */
export function CreateCraftItemDialog({ workspaceId, onClose, onCreated }: CreateCraftItemDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const categoriesQuery = useCraftCategories();
  const create = useCreateCraftItem();

  const [category, setCategory] = useState("");
  const [name, setName] = useState("");
  const [manufacturer, setManufacturer] = useState("");
  const [article, setArticle] = useState("");
  const [colorName, setColorName] = useState("");
  const [colorHex, setColorHex] = useState("");
  const [unit, setUnit] = useState<string>(UNITS[0]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const categories = useMemo(() => categoriesQuery.data ?? [], [categoriesQuery.data]);
  const selected = categories.find((item) => item.slug === category);

  // The first category is a sensible default; the unit follows the category.
  useEffect(() => {
    if (!category && categories.length > 0) {
      setCategory(categories[0].slug);
      setUnit(categories[0].default_unit);
    }
  }, [categories, category]);

  function handleCategoryChange(slug: string) {
    setCategory(slug);
    const next = categories.find((item) => item.slug === slug);
    if (next) {
      setUnit(next.default_unit);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!category) {
      setError("Выберите категорию.");
      host.haptic("error");
      return;
    }
    if (!name.trim() && !article.trim()) {
      setError("Укажите название или артикул.");
      host.haptic("error");
      return;
    }
    const hex = normalizeHex(colorHex);
    if (hex === null) {
      setError("Цвет должен быть в формате #RRGGBB.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await create.mutateAsync({
        params: { path: { id: workspaceId } },
        body: {
          category,
          name: name.trim() || undefined,
          manufacturer: manufacturer.trim() || undefined,
          article: article.trim() || undefined,
          color_name: colorName.trim() || undefined,
          color_hex: hex || undefined,
          unit,
        },
      });
      host.haptic("success");
      onCreated();
      onClose();
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Sheet title="Новая позиция" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <SelectField label="Категория" value={category} onChange={(event) => handleCategoryChange(event.target.value)}>
          {categories.map((item) => (
            <option key={item.slug} value={item.slug}>
              {item.name}
            </option>
          ))}
        </SelectField>
        <TextField label="Название" placeholder="Например: Жемчуг айвори 2 мм" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        <TextField label="Производитель" placeholder="Например: PRECIOSA" value={manufacturer} onChange={(event) => setManufacturer(event.target.value)} />
        <TextField label="Артикул" value={article} onChange={(event) => setArticle(event.target.value)} />
        <div className="craft-color">
          <TextField label="Цвет" placeholder="#RRGGBB" mono value={colorHex} onChange={(event) => setColorHex(event.target.value)} />
          <span className="craft-swatch craft-swatch--large" style={swatchStyle({ color_hex: normalizeHex(colorHex) ?? "" })} aria-hidden="true" />
        </div>
        <TextField label="Название цвета" placeholder="Например: айвори" value={colorName} onChange={(event) => setColorName(event.target.value)} />
        <SelectField label="Единица" hint={`По умолчанию для категории: ${selected?.default_unit ?? "—"}`} value={unit} onChange={(event) => setUnit(event.target.value)}>
          {UNITS.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </SelectField>
        <Button type="submit" variant="primary" block loading={busy} disabled={categories.length === 0}>
          Создать позицию
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
                <span className="craft-swatch" style={swatchStyle(candidate)} aria-hidden="true" />
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
