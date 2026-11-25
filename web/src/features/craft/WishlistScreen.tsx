import { useState } from "react";

import type { Wish } from "../../api/client";
import { usePageTitle } from "../../router/router";
import { Button, EmptyState, Notice } from "../../ui/controls";
import { IconPlus } from "../../ui/icons";
import { useCurrentWorkspace } from "../workspaces/selection";
import { WorkspaceSwitcher } from "../workspaces/WorkspaceSwitcher";
import { useWishes } from "./data";
import { itemMeta } from "./format";
import { BoughtDialog, CreateWishDialog, EditWishDialog } from "./WishlistDialogs";
import { groupWishes, linkName, linkPriceLabel, linkStockLabel, priorityChip, wishQtyLabel, wishStockLabel } from "./wishlist";

/** One wish with its links and actions. */
function WishCard({ wish, onEdit, onBought }: { wish: Wish; onEdit: (wish: Wish) => void; onBought: (wish: Wish) => void }) {
  const chip = priorityChip(wish.priority);
  const item = wish.item;
  const meta = [item ? itemMeta(item) : "", item?.category_name ?? "", wishStockLabel(item)].filter(Boolean).join(" · ");

  return (
    <li className="card wish-card">
      <div className="wish-card__head">
        <span className="wish-card__title">{wish.title}</span>
        {chip && <span className={wish.priority >= 2 ? "badge wish-priority wish-priority--high" : "badge wish-priority"}>{chip}</span>}
      </div>

      <span className="wish-card__meta">{[wishQtyLabel(wish), meta].filter(Boolean).join(" · ") || "Количество не указано"}</span>
      {wish.note && <span className="wish-card__note">{wish.note}</span>}

      {wish.links.length > 0 && (
        <div className="wish-links" aria-label={`Ссылки: ${wish.title}`}>
          {wish.links.map((link) => (
            <a
              key={link.id}
              className="wish-link"
              href={link.url}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={`${linkName(link)}: ${wish.title}`}
            >
              <span>{linkName(link)}</span>
              {linkPriceLabel(link) && <span className="wish-link__price">{linkPriceLabel(link)}</span>}
              {linkStockLabel(link) && (
                <span className={link.in_stock === false ? "wish-link__stock wish-link__stock--off" : "wish-link__stock"}>
                  {linkStockLabel(link)}
                </span>
              )}
            </a>
          ))}
        </div>
      )}

      <div className="wish-actions">
        {wish.status !== "bought" && (
          <Button size="s" variant="solid" aria-label={`Куплено: ${wish.title}`} onClick={() => onBought(wish)}>
            Куплено
          </Button>
        )}
        <Button size="s" aria-label={`Изменить: ${wish.title}`} onClick={() => onEdit(wish)}>
          Изменить
        </Button>
      </div>
    </li>
  );
}

/** The wish list of one workspace, grouped by status. */
function WishlistPanel({ workspaceId }: { workspaceId: string }) {
  const query = useWishes(workspaceId, "all");
  const [editing, setEditing] = useState<Wish | null>(null);
  const [buying, setBuying] = useState<Wish | null>(null);

  const groups = groupWishes(query.data ?? []);

  return (
    <section className="page__section" aria-label="Wishlist">
      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}
      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить wishlist: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}
      {query.isSuccess && (query.data ?? []).length === 0 && (
        <EmptyState title="Wishlist пуст">
          Добавьте то, что хочется купить: позицию каталога или свободный текст, с количеством, приоритетом и ссылками на магазины.
        </EmptyState>
      )}

      {groups.map((group) => (
        <section className="page__section" key={group.status} aria-label={group.label}>
          <h2 className="page__subtitle">
            {group.label} <span className="badge">{group.wishes.length}</span>
          </h2>
          <ul className="card-list" aria-label={group.label}>
            {group.wishes.map((wish) => (
              <WishCard key={wish.id} wish={wish} onEdit={setEditing} onBought={setBuying} />
            ))}
          </ul>
        </section>
      ))}

      {editing && <EditWishDialog workspaceId={workspaceId} wish={editing} onClose={() => setEditing(null)} />}
      {buying && <BoughtDialog workspaceId={workspaceId} wish={buying} onClose={() => setBuying(null)} />}
    </section>
  );
}

/**
 * «Wishlist»: what the workspace wants to buy, grouped by status. The entries
 * live in the selected workspace, like the stock and the projects.
 */
export default function WishlistScreen() {
  usePageTitle("Wishlist");
  const { query, workspaces, current, select } = useCurrentWorkspace();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Wishlist</h1>
        <Button variant="primary" aria-label="Добавить в wishlist" disabled={!current} onClick={() => setCreateOpen(true)}>
          <IconPlus />
          Добавить
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
        <EmptyState title="Нет пространства">
          Сначала создайте пространство в разделе «Участники»: wishlist живёт внутри него.
        </EmptyState>
      )}

      {current && (
        <>
          <WorkspaceSwitcher workspaces={workspaces} currentId={current.id} onChange={select} />
          <WishlistPanel workspaceId={current.id} />
        </>
      )}

      {createOpen && current && (
        <CreateWishDialog workspaceId={current.id} onClose={() => setCreateOpen(false)} />
      )}
    </div>
  );
}
