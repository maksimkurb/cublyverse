import { useState } from "react";

import type { CraftItem, CraftStockEntry } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { usePageTitle } from "../../router/router";
import { Button, EmptyState, Notice, Segmented, SelectField, Switch, TextField } from "../../ui/controls";
import { IconPlus } from "../../ui/icons";
import { useCurrentWorkspace } from "../workspaces/selection";
import { SpaceBar } from "../workspaces/SpaceBar";
import { AddStockDialog } from "./AddStockDialog";
import { CreateCraftItemDialog, LinkCraftItemDialog, StockEditDialog } from "./CraftDialogs";
import { useAdjustCraftStock, useCraftCatalog, useCraftCategories, useCraftInventory, useCraftManufacturers } from "./data";
import { formatQty, itemColorLabel, itemMeta, qtyLabel, swatchStyle } from "./format";
import ProjectsPanel from "./ProjectsPanel";
import PurchasesPanel from "./PurchasesPanel";

type Tab = "inventory" | "purchases" | "projects" | "catalog";

const TABS: ReadonlyArray<{ value: Tab; label: string }> = [
  { value: "inventory", label: "Запасы" },
  { value: "purchases", label: "Закупки" },
  { value: "projects", label: "Проекты" },
  { value: "catalog", label: "Каталог" },
];

/** Threshold of the low-stock filter. */
const LOW_STOCK_MAX = 5;

/** Category and manufacturer selects of the stock and catalogue tabs. */
function CraftFilters({
  category,
  manufacturer,
  onCategory,
  onManufacturer,
}: {
  category: string;
  manufacturer: string;
  onCategory: (value: string) => void;
  onManufacturer: (value: string) => void;
}) {
  const categories = useCraftCategories();
  const manufacturers = useCraftManufacturers();

  return (
    <>
      <SelectField label="Категория" value={category} onChange={(event) => onCategory(event.target.value)}>
        <option value="">Все категории</option>
        {(categories.data ?? []).map((item) => (
          <option key={item.slug} value={item.slug}>
            {item.name}
          </option>
        ))}
      </SelectField>
      <SelectField label="Производитель" value={manufacturer} onChange={(event) => onManufacturer(event.target.value)}>
        <option value="">Все производители</option>
        {(manufacturers.data ?? []).map((item) => (
          <option key={item.id} value={item.name}>
            {item.name}
          </option>
        ))}
      </SelectField>
    </>
  );
}

/** One row of the stock list with quick ±1 actions. */
function StockRow({
  entry,
  busy,
  onAdjust,
  onEdit,
}: {
  entry: CraftStockEntry;
  busy: boolean;
  onAdjust: (entry: CraftStockEntry, delta: number) => void;
  onEdit: (entry: CraftStockEntry) => void;
}) {
  const name = entry.item.name;
  return (
    <li className="card craft-row">
      <span className="craft-swatch" style={swatchStyle(entry.item)} aria-hidden="true" />
      <div className="craft-row__text">
        <span className="craft-row__title">{name}</span>
        <span className="craft-row__meta">
          {[itemMeta(entry.item), entry.item.category_name, entry.location].filter(Boolean).join(" · ")}
        </span>
        {entry.notes && <span className="craft-row__notes">{entry.notes}</span>}
      </div>
      <div className="craft-row__side">
        <span className="craft-qty">{qtyLabel(entry)}</span>
        <div className="craft-stepper">
          <button
            type="button"
            className="cv-iconbtn"
            aria-label={`Уменьшить на 1: ${name}`}
            disabled={busy}
            onClick={() => onAdjust(entry, -1)}
          >
            −
          </button>
          <button
            type="button"
            className="cv-iconbtn"
            aria-label={`Добавить 1: ${name}`}
            disabled={busy}
            onClick={() => onAdjust(entry, 1)}
          >
            +
          </button>
        </div>
        <Button size="s" aria-label={`Изменить остаток: ${name}`} onClick={() => onEdit(entry)}>
          Изменить
        </Button>
      </div>
    </li>
  );
}

/** «Запасы»: what the workspace holds, with filters and quick changes. */
function InventoryPanel({ workspaceId }: { workspaceId: string }) {
  const host = useHost();
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");
  const [manufacturer, setManufacturer] = useState("");
  const [lowOnly, setLowOnly] = useState(false);
  const [editing, setEditing] = useState<CraftStockEntry | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);

  const query = useCraftInventory(workspaceId, {
    q,
    category,
    manufacturer,
    maxQty: lowOnly ? LOW_STOCK_MAX : undefined,
    limit: 200,
  });
  const adjust = useAdjustCraftStock();

  async function handleAdjust(entry: CraftStockEntry, delta: number) {
    setBusyId(entry.item.id);
    try {
      await adjust.mutateAsync({
        params: { path: { id: workspaceId, item_id: entry.item.id } },
        body: { delta, reason: delta > 0 ? "Быстрое добавление" : "Быстрое списание" },
      });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось изменить остаток: ${describeApiError(cause)}`);
    } finally {
      setBusyId(null);
    }
  }

  const rows = query.data ?? [];
  const filtered = q.trim() !== "" || category !== "" || manufacturer !== "" || lowOnly;

  return (
    <section className="page__section" aria-label="Запасы">
      <div className="craft-actions">
        <Button variant="primary" onClick={() => setAddOpen(true)}>
          <IconPlus />
          Добавить в запасы
        </Button>
      </div>

      <div className="craft-filters">
        <TextField label="Поиск" placeholder="Название, цвет или артикул" value={q} onChange={(event) => setQ(event.target.value)} />
        <CraftFilters category={category} manufacturer={manufacturer} onCategory={setCategory} onManufacturer={setManufacturer} />
      </div>

      <div className="switch-row">
        <span aria-hidden="true">Мало осталось (≤ {LOW_STOCK_MAX})</span>
        <Switch
          aria-label={`Только малое количество, не больше ${LOW_STOCK_MAX}`}
          checked={lowOnly}
          onCheckedChange={(checked) => {
            host.haptic("selection");
            setLowOnly(checked);
          }}
        />
      </div>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить запасы: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}
      {query.isSuccess && rows.length === 0 && (
        <EmptyState title={filtered ? "Ничего не найдено" : "Склад пуст"}>
          {filtered
            ? "Измените фильтры или строку поиска."
            : "Нажмите «Добавить в запасы»: найдите позицию в каталоге или создайте свою."}
        </EmptyState>
      )}

      {rows.length > 0 && (
        <ul className="card-list" aria-label="Запасы">
          {rows.map((entry) => (
            <StockRow
              key={entry.item.id}
              entry={entry}
              busy={busyId === entry.item.id}
              onAdjust={(item, delta) => void handleAdjust(item, delta)}
              onEdit={setEditing}
            />
          ))}
        </ul>
      )}

      {addOpen && <AddStockDialog workspaceId={workspaceId} onClose={() => setAddOpen(false)} />}
      {editing && <StockEditDialog workspaceId={workspaceId} entry={editing} onClose={() => setEditing(null)} />}
    </section>
  );
}

/** One catalogue result with its stock and actions. */
function CatalogRow({
  item,
  onAdd,
  onLink,
}: {
  item: CraftItem;
  onAdd: (item: CraftItem) => void;
  onLink: (item: CraftItem) => void;
}) {
  return (
    <li className="card craft-row">
      <span className="craft-swatch" style={swatchStyle(item)} aria-hidden="true" />
      <div className="craft-row__text">
        <span className="craft-row__title">
          {item.name}
          {item.private && <span className="badge craft-row__badge">Частная</span>}
        </span>
        <span className="craft-row__meta">
          {[itemMeta(item), item.category_name, itemColorLabel(item)].filter(Boolean).join(" · ")}
        </span>
      </div>
      <div className="craft-row__side">
        <span className="craft-qty">{`${formatQty(item.stock_qty)} ${item.unit}`}</span>
        <Button size="s" variant="solid" aria-label={`Добавить на склад: ${item.name}`} onClick={() => onAdd(item)}>
          На склад
        </Button>
        {item.private && (
          <Button size="s" aria-label={`Связать с каталогом: ${item.name}`} onClick={() => onLink(item)}>
            Связать
          </Button>
        )}
      </div>
    </li>
  );
}

/** «Каталог»: search the shared catalogue, create private items and stock them. */
function CatalogPanel({ workspaceId }: { workspaceId: string }) {
  const [q, setQ] = useState("");
  const [category, setCategory] = useState("");
  const [manufacturer, setManufacturer] = useState("");
  const [onlyInStock, setOnlyInStock] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [linking, setLinking] = useState<CraftItem | null>(null);
  const [adding, setAdding] = useState<CraftItem | null>(null);

  const query = useCraftCatalog(workspaceId, {
    q,
    category,
    manufacturer,
    onlyInStock,
    limit: 100,
  });
  const items = query.data ?? [];
  const filtered = q.trim() !== "" || category !== "" || manufacturer !== "" || onlyInStock;

  return (
    <section className="page__section" aria-label="Каталог">
      <div className="craft-filters">
        <TextField label="Поиск" placeholder="Название, цвет или артикул" value={q} onChange={(event) => setQ(event.target.value)} />
        <CraftFilters category={category} manufacturer={manufacturer} onCategory={setCategory} onManufacturer={setManufacturer} />
      </div>

      <div className="switch-row">
        <span aria-hidden="true">Только то, что есть в запасах</span>
        <Switch aria-label="Только то, что есть в запасах" checked={onlyInStock} onCheckedChange={setOnlyInStock} />
      </div>

      <div className="craft-actions">
        <Button variant="primary" onClick={() => setCreateOpen(true)}>
          <IconPlus />
          Новая позиция
        </Button>
      </div>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить каталог: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}
      {query.isSuccess && items.length === 0 && (
        <EmptyState title={filtered ? "Ничего не найдено" : "Каталог пуст"}>
          {filtered ? "Измените фильтры или строку поиска." : "Создайте частную позицию: она видна только участникам этого пространства."}
        </EmptyState>
      )}

      {items.length > 0 && (
        <ul className="card-list" aria-label="Каталог">
          {items.map((item) => (
            <CatalogRow
              key={item.id}
              item={item}
              onAdd={setAdding}
              onLink={setLinking}
            />
          ))}
        </ul>
      )}

      {createOpen && (
        <CreateCraftItemDialog workspaceId={workspaceId} onClose={() => setCreateOpen(false)} onCreated={() => setCreateOpen(false)} />
      )}
      {adding && <AddStockDialog workspaceId={workspaceId} item={adding} onClose={() => setAdding(null)} />}
      {linking && <LinkCraftItemDialog workspaceId={workspaceId} item={linking} onClose={() => setLinking(null)} />}
    </section>
  );
}

/**
 * «Склад»: stock, purchases, projects and catalogue of the selected warehouse
 * space.
 */
export default function CraftScreen() {
  const [tab, setTab] = useState<Tab>("inventory");
  usePageTitle("Склад");

  const space = useCurrentWorkspace("warehouse");
  const current = space.current;

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Склад</h1>
      </header>

      <SpaceBar app="warehouse" space={space} />

      {current && (
        <>
          <Segmented label="Раздел" value={tab} options={TABS} onChange={setTab} />
          {tab === "inventory" && <InventoryPanel key={current.id} workspaceId={current.id} />}
          {tab === "purchases" && <PurchasesPanel workspaceId={current.id} />}
          {tab === "projects" && <ProjectsPanel key={current.id} workspaceId={current.id} />}
          {tab === "catalog" && <CatalogPanel workspaceId={current.id} />}
        </>
      )}
    </div>
  );
}
