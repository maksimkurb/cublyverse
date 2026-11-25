import { useState, type FormEvent } from "react";

import type { CraftItem, Wish, WishLink } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Checkbox, Notice, Segmented, SelectField, Sheet, TextAreaField, TextField } from "../../ui/controls";
import {
  useAddWishLink,
  useCraftCatalog,
  useCreateWish,
  useDeleteWish,
  useMarkWishBought,
  useRemoveWishLink,
  useUpdateWish,
} from "./data";
import { Swatch } from "./Swatch";
import { formatQty, itemMeta } from "./format";
import { PRIORITY_OPTIONS, WISH_STATUS_LABELS, WISH_STATUS_ORDER, isHttpURL, linkName, linkPriceLabel } from "./wishlist";

type WishSource = "catalog" | "text";

const SOURCES: ReadonlyArray<{ value: WishSource; label: string }> = [
  { value: "catalog", label: "Из каталога" },
  { value: "text", label: "Свободный текст" },
];

/** Props of the create dialog. */
export interface CreateWishDialogProps {
  workspaceId: string;
  onClose: () => void;
}

/** Adds a wish from the catalogue or as free text. */
export function CreateWishDialog({ workspaceId, onClose }: CreateWishDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const create = useCreateWish();

  const [source, setSource] = useState<WishSource>("catalog");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<CraftItem | null>(null);
  const [title, setTitle] = useState("");
  const [qty, setQty] = useState("");
  const [priority, setPriority] = useState("0");
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const catalog = useCraftCatalog(workspaceId, { q: search, limit: 20 });
  const candidates = catalog.data ?? [];

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (source === "catalog" && !selected) {
      setError("Выберите позицию каталога.");
      host.haptic("error");
      return;
    }
    if (source === "text" && !title.trim()) {
      setError("Укажите название позиции.");
      host.haptic("error");
      return;
    }
    let wanted: number | undefined;
    if (qty.trim()) {
      const parsed = Number(qty.trim().replace(",", "."));
      if (!Number.isFinite(parsed) || parsed <= 0) {
        setError("Количество должно быть положительным.");
        host.haptic("error");
        return;
      }
      wanted = parsed;
    }
    setError(null);
    setBusy(true);
    try {
      await create.mutateAsync({
        params: { path: { id: workspaceId } },
        body: {
          catalog_item_id: source === "catalog" ? selected?.id : undefined,
          title: source === "text" ? title.trim() : undefined,
          qty_wanted: wanted,
          priority: Number(priority),
          note: note.trim(),
        },
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
    <Sheet title="Новая запись" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <Segmented label="Откуда взять позицию" value={source} options={SOURCES} onChange={setSource} />

        {source === "catalog" ? (
          <>
            <TextField label="Поиск в каталоге" placeholder="Название, цвет или артикул" value={search} onChange={(event) => setSearch(event.target.value)} />
            {catalog.isPending && (
              <p className="page__state" role="status">
                Поиск…
              </p>
            )}
            {catalog.isSuccess && candidates.length === 0 && <p className="page__state">Ничего не найдено.</p>}
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
                    <Button
                      size="s"
                      variant={selected?.id === item.id ? "solid" : "outline"}
                      aria-label={`Выбрать: ${item.name}`}
                      onClick={() => {
                        host.haptic("selection");
                        setSelected(item);
                      }}
                    >
                      {selected?.id === item.id ? "Выбрано" : "Выбрать"}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </>
        ) : (
          <TextField
            label="Название"
            placeholder="Например: Лента атласная, 1 см"
            maxLength={200}
            value={title}
            onChange={(event) => setTitle(event.target.value)}
          />
        )}

        <TextField label="Сколько нужно" inputMode="decimal" mono placeholder="Необязательно" value={qty} onChange={(event) => setQty(event.target.value)} />
        <SelectField label="Приоритет" value={priority} onChange={(event) => setPriority(event.target.value)}>
          {PRIORITY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </SelectField>
        <TextAreaField label="Заметка" placeholder="Например: на подарок" value={note} onChange={(event) => setNote(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Добавить
        </Button>
      </form>
    </Sheet>
  );
}

/** Props of the edit dialog. */
export interface EditWishDialogProps {
  workspaceId: string;
  wish: Wish;
  onClose: () => void;
}

/** Changes an entry and manages its shop links. */
export function EditWishDialog({ workspaceId, wish, onClose }: EditWishDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const update = useUpdateWish();
  const remove = useDeleteWish();
  const addLink = useAddWishLink();
  const removeLink = useRemoveWishLink();

  const [qty, setQty] = useState(wish.qty_wanted != null ? String(wish.qty_wanted) : "");
  const [priority, setPriority] = useState(String(wish.priority));
  const [note, setNote] = useState(wish.note);
  const [status, setStatus] = useState(wish.status);
  const [links, setLinks] = useState<WishLink[]>(wish.links);
  const [linkFormOpen, setLinkFormOpen] = useState(false);
  const [linkURL, setLinkURL] = useState("");
  const [linkShop, setLinkShop] = useState("");
  const [linkPrice, setLinkPrice] = useState("");
  const [linkCurrency, setLinkCurrency] = useState("RUB");
  const [linkInStock, setLinkInStock] = useState(false);
  const [armed, setArmed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    let wanted: number | null = null;
    if (qty.trim()) {
      const parsed = Number(qty.trim().replace(",", "."));
      if (!Number.isFinite(parsed) || parsed <= 0) {
        setError("Количество должно быть положительным.");
        host.haptic("error");
        return;
      }
      wanted = parsed;
    }
    setError(null);
    setBusy(true);
    try {
      await update.mutateAsync({
        params: { path: { id: workspaceId, wish_id: wish.id } },
        body: { qty_wanted: wanted, priority: Number(priority), note: note.trim(), status },
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

  async function handleAddLink() {
    if (!isHttpURL(linkURL)) {
      setError("Ссылка должна начинаться с http:// или https://.");
      host.haptic("error");
      return;
    }
    let price: number | undefined;
    if (linkPrice.trim()) {
      const parsed = Number(linkPrice.trim().replace(",", "."));
      if (!Number.isFinite(parsed) || parsed < 0) {
        setError("Цена не может быть отрицательной.");
        host.haptic("error");
        return;
      }
      price = parsed;
    }
    setError(null);
    setBusy(true);
    try {
      const created = await addLink.mutateAsync({
        params: { path: { id: workspaceId, wish_id: wish.id } },
        body: {
          url: linkURL.trim(),
          shop_name: linkShop.trim() || undefined,
          price,
          currency: linkCurrency.trim() || undefined,
          in_stock: linkInStock,
        },
      });
      setLinks((previous) => [...previous, created]);
      setLinkURL("");
      setLinkShop("");
      setLinkPrice("");
      setLinkFormOpen(false);
      host.haptic("success");
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  async function handleRemoveLink(link: WishLink) {
    setBusy(true);
    try {
      await removeLink.mutateAsync({ params: { path: { id: workspaceId, wish_id: wish.id, link_id: link.id } } });
      setLinks((previous) => previous.filter((item) => item.id !== link.id));
      host.haptic("success");
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
      await remove.mutateAsync({ params: { path: { id: workspaceId, wish_id: wish.id } } });
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
    <Sheet title={wish.title} onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}
        <TextField label="Сколько нужно" inputMode="decimal" mono placeholder="Не указано" value={qty} onChange={(event) => setQty(event.target.value)} />
        <SelectField label="Приоритет" value={priority} onChange={(event) => setPriority(event.target.value)}>
          {PRIORITY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </SelectField>
        <SelectField label="Статус" value={status} onChange={(event) => setStatus(event.target.value as Wish["status"])}>
          {WISH_STATUS_ORDER.map((value) => (
            <option key={value} value={value}>
              {WISH_STATUS_LABELS[value]}
            </option>
          ))}
        </SelectField>
        <TextAreaField label="Заметка" value={note} onChange={(event) => setNote(event.target.value)} />
        <Button type="submit" variant="primary" block loading={busy}>
          Сохранить
        </Button>
      </form>

      <section className="page__section" aria-label="Ссылки на магазины">
        <h2 className="page__subtitle">Ссылки</h2>
        {links.length === 0 && <p className="page__state">Ссылок пока нет.</p>}
        {links.length > 0 && (
          <ul className="wish-links" aria-label="Ссылки на магазины">
            {links.map((link) => (
              <li key={link.id} className="wish-link">
                <span>{linkName(link)}</span>
                {linkPriceLabel(link) && <span className="wish-link__price">{linkPriceLabel(link)}</span>}
                <button
                  type="button"
                  className="wish-link__remove"
                  aria-label={`Удалить ссылку: ${linkName(link)}`}
                  disabled={busy}
                  onClick={() => void handleRemoveLink(link)}
                >
                  ×
                </button>
              </li>
            ))}
          </ul>
        )}

        {linkFormOpen ? (
          <div className="wish-add-link">
            <TextField label="Ссылка" placeholder="https://" value={linkURL} onChange={(event) => setLinkURL(event.target.value)} />
            <TextField label="Магазин" placeholder="Название магазина" value={linkShop} onChange={(event) => setLinkShop(event.target.value)} />
            <TextField label="Цена" inputMode="decimal" mono placeholder="Необязательно" value={linkPrice} onChange={(event) => setLinkPrice(event.target.value)} />
            <TextField label="Валюта" mono value={linkCurrency} onChange={(event) => setLinkCurrency(event.target.value)} />
            <label className="scopes__item">
              <Checkbox checked={linkInStock} onCheckedChange={setLinkInStock} />
              <span className="scopes__label">Есть в наличии</span>
            </label>
            <Button variant="solid" block loading={busy} onClick={() => void handleAddLink()}>
              Добавить ссылку
            </Button>
          </div>
        ) : (
          <Button block onClick={() => setLinkFormOpen(true)}>
            Добавить ссылку
          </Button>
        )}
      </section>

      <section className="page__section">
        <Button variant="danger" block loading={busy} onClick={() => void handleDelete()}>
          {armed ? "Подтвердить удаление" : "Удалить запись"}
        </Button>
      </section>
    </Sheet>
  );
}

/** Props of the bought dialog. */
export interface BoughtDialogProps {
  workspaceId: string;
  wish: Wish;
  onClose: () => void;
}

/** Marks an entry bought and asks whether the purchase goes to the stock. */
export function BoughtDialog({ workspaceId, wish, onClose }: BoughtDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const bought = useMarkWishBought();
  const canStock = wish.item !== undefined && wish.item !== null;
  const [addToStock, setAddToStock] = useState(canStock);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await bought.mutateAsync({
        params: { path: { id: workspaceId, wish_id: wish.id } },
        body: { add_to_stock: canStock && addToStock },
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
    <Sheet title="Куплено" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Не получилось">
            {error}
          </Notice>
        )}
        <p className="page__state">
          «{wish.title}» перейдёт в статус «Куплено».
          {canStock && wish.qty_wanted != null && ` Количество: ${formatQty(wish.qty_wanted)} ${wish.item?.unit ?? ""}.`}
        </p>
        {canStock ? (
          <label className="scopes__item">
            <Checkbox checked={addToStock} onCheckedChange={setAddToStock} />
            <span className="scopes__label">Добавить в запасы</span>
          </label>
        ) : (
          <p className="page__state">Свободный текст нельзя добавить в запасы: у него нет позиции каталога.</p>
        )}
        <Button type="submit" variant="primary" block loading={busy}>
          Отметить купленным
        </Button>
      </form>
    </Sheet>
  );
}
