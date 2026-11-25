import { useState, type FormEvent } from "react";

import type { CraftItem, CraftStockEntry } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Notice, Sheet, TextField } from "../../ui/controls";
import { CreateCraftItemDialog } from "./CraftDialogs";
import { useAdjustCraftStock, useCraftCatalog, useUpdateCraftStockMeta } from "./data";
import { formatQty, itemMeta, swatchStyle } from "./format";
import { parsePositiveQty } from "./wishlist";

/** Props of the «add to stock» dialog. */
export interface AddStockDialogProps {
  workspaceId: string;
  /** Preselected catalogue item; without it the dialog starts with a search. */
  item?: CraftItem;
  onClose: () => void;
}

/**
 * Puts material into stock: pick a catalogue item (or create an own one), then
 * say how much and where it lives. The quantity is added to what is already
 * there.
 */
export function AddStockDialog({ workspaceId, item, onClose }: AddStockDialogProps) {
  const [selected, setSelected] = useState<CraftItem | null>(item ?? null);
  const [creating, setCreating] = useState(false);

  if (creating) {
    return (
      <CreateCraftItemDialog
        workspaceId={workspaceId}
        onClose={() => setCreating(false)}
        onCreated={(created) => {
          setSelected(created);
          setCreating(false);
        }}
      />
    );
  }
  if (selected) {
    return <QuantityStep workspaceId={workspaceId} item={selected} onClose={onClose} />;
  }
  return <PickStep workspaceId={workspaceId} onPick={setSelected} onCreate={() => setCreating(true)} onClose={onClose} />;
}

/** First step: find the item in the catalogue. */
function PickStep({
  workspaceId,
  onPick,
  onCreate,
  onClose,
}: {
  workspaceId: string;
  onPick: (item: CraftItem) => void;
  onCreate: () => void;
  onClose: () => void;
}) {
  useHostBackButton(onClose);
  const [search, setSearch] = useState("");
  const query = useCraftCatalog(workspaceId, { q: search, limit: 20 });
  const items = query.data ?? [];

  return (
    <Sheet title="Добавить в запасы" onClose={onClose}>
      <div className="cv-form">
        <TextField label="Поиск в каталоге" placeholder="Название, цвет или артикул" value={search} onChange={(event) => setSearch(event.target.value)} />
        <Button block onClick={onCreate}>
          Нет в каталоге — создать свою позицию
        </Button>
        {query.isPending && (
          <p className="page__state" role="status">
            Поиск…
          </p>
        )}
        {query.isError && <Notice tone="error">Не удалось загрузить каталог: {describeApiError(query.error)}</Notice>}
        {query.isSuccess && items.length === 0 && <p className="page__state">Ничего не найдено.</p>}
        {items.length > 0 && (
          <ul className="card-list" aria-label="Позиции каталога">
            {items.map((candidate) => (
              <li className="card craft-row" key={candidate.id}>
                <span className="craft-swatch" style={swatchStyle(candidate)} aria-hidden="true" />
                <div className="craft-row__text">
                  <span className="craft-row__title">{candidate.name}</span>
                  <span className="craft-row__meta">{[itemMeta(candidate), candidate.category_name].filter(Boolean).join(" · ")}</span>
                </div>
                <Button size="s" aria-label={`Выбрать: ${candidate.name}`} onClick={() => onPick(candidate)}>
                  Выбрать
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Sheet>
  );
}

/** Second step: how much and where. */
function QuantityStep({ workspaceId, item, onClose }: { workspaceId: string; item: CraftItem; onClose: () => void }) {
  useHostBackButton(onClose);
  const host = useHost();
  const adjust = useAdjustCraftStock();
  const setMeta = useUpdateCraftStockMeta();
  const [qty, setQty] = useState("1");
  const [location, setLocation] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The quantity must not be added twice when only saving the location failed.
  const [added, setAdded] = useState<CraftStockEntry | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = parsePositiveQty(qty);
    if (value === null) {
      setError("Количество должно быть положительным.");
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    const path = { id: workspaceId, item_id: item.id };
    try {
      let entry = added;
      if (!entry) {
        entry = await adjust.mutateAsync({ params: { path }, body: { delta: value, reason: "Добавлено в запасы" } });
        setAdded(entry);
      }
      if (location.trim()) {
        // Both fields are replaced, so the existing notes are sent back.
        await setMeta.mutateAsync({ params: { path }, body: { location: location.trim(), notes: entry.notes } });
      }
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
    <Sheet title="Добавить в запасы" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Не получилось">
            {error}
          </Notice>
        )}
        <p className="page__state">
          {item.name}
          {item.in_inventory ? ` — уже есть ${formatQty(item.stock_qty)} ${item.unit}` : ""}
        </p>
        <TextField label={`Сколько добавить, ${item.unit}`} inputMode="decimal" mono disabled={added !== null} value={qty} onChange={(event) => setQty(event.target.value)} />
        <TextField label="Где хранится" placeholder="Необязательно" value={location} onChange={(event) => setLocation(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Добавить
        </Button>
      </form>
    </Sheet>
  );
}
