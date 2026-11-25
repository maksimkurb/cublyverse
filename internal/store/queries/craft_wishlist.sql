-- name: CreateCraftWishlistItem :one
INSERT INTO craft_wishlist_items (
    workspace_id, catalog_item_id, title, qty_wanted, priority, note, created_by
) VALUES (
    sqlc.arg(workspace_id), sqlc.arg(catalog_item_id), sqlc.arg(title),
    sqlc.arg(qty_wanted), sqlc.arg(priority), sqlc.arg(note), sqlc.arg(created_by)
)
RETURNING *;

-- name: ListCraftWishlistItems :many
-- Wish list entries of one workspace with the catalogue item and the current
-- stock, if the entry points at an item. An empty status means "any"; a set
-- wish_id narrows the result to one entry (both used by the service).
SELECT w.*,
       i.id            AS item_id,
       i.workspace_id  AS item_workspace_id,
       i.article       AS item_article,
       i.series        AS item_series,
       i.name          AS item_name,
       i.color_name    AS item_color_name,
       i.color_hex     AS item_color_hex,
       i.color_source  AS item_color_source,
       i.size          AS item_size,
       i.finish        AS item_finish,
       i.unit          AS item_unit,
       i.pack_size     AS item_pack_size,
       i.attrs         AS item_attrs,
       i.source        AS item_source,
       i.source_url    AS item_source_url,
       i.image_url     AS item_image_url,
       COALESCE(sc.slug, rc.slug, '')::text AS category_slug,
       COALESCE(sc.name, rc.name, '')::text AS category_name,
       m.name          AS manufacturer_name,
       COALESCE(inv.qty, 0)::numeric(12,3) AS stock_qty,
       (inv.catalog_item_id IS NOT NULL)::boolean AS in_inventory,
       inv.attrs AS stock_attrs
FROM craft_wishlist_items w
LEFT JOIN craft_catalog_items i ON i.id = w.catalog_item_id
LEFT JOIN craft_categories rc ON rc.id = i.category_id
LEFT JOIN craft_categories sc
       ON sc.workspace_id = w.workspace_id AND i.workspace_id IS NULL AND sc.catalog_slug = rc.slug
LEFT JOIN craft_manufacturers m ON m.id = i.manufacturer_id
LEFT JOIN craft_inventory inv ON inv.catalog_item_id = i.id AND inv.workspace_id = w.workspace_id
WHERE w.workspace_id = sqlc.arg(workspace_id)::uuid
  AND (sqlc.arg(status)::text = '' OR w.status = sqlc.arg(status))
  AND (sqlc.narg(wish_id)::uuid IS NULL OR w.id = sqlc.narg(wish_id))
ORDER BY w.priority DESC, w.created_at DESC;

-- name: GetWantedCraftWishlistItem :one
-- The wanted entry of a catalogue item, used to skip duplicates when the
-- shortage of a project is turned into wishes.
SELECT * FROM craft_wishlist_items
WHERE workspace_id = $1
  AND catalog_item_id = $2
  AND status = 'wanted';

-- name: UpdateCraftWishlistItem :one
UPDATE craft_wishlist_items
SET qty_wanted = sqlc.arg(qty_wanted),
    priority   = sqlc.arg(priority),
    note       = sqlc.arg(note),
    status     = sqlc.arg(status),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: SetCraftWishlistStatus :one
UPDATE craft_wishlist_items
SET status     = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: DeleteCraftWishlistItem :execrows
DELETE FROM craft_wishlist_items
WHERE id = $1
  AND workspace_id = $2;

-- name: CreateCraftWishlistLink :one
INSERT INTO craft_wishlist_links (
    wishlist_item_id, url, shop_name, price, currency, in_stock
) VALUES (
    sqlc.arg(wishlist_item_id), sqlc.arg(url), sqlc.arg(shop_name),
    sqlc.arg(price), sqlc.arg(currency), sqlc.arg(in_stock)
)
RETURNING *;

-- name: ListCraftWishlistLinks :many
-- Every link of one entry.
SELECT * FROM craft_wishlist_links
WHERE wishlist_item_id = $1
ORDER BY created_at;

-- name: ListCraftWishlistLinksForWorkspace :many
-- Links of every entry of a workspace with the given status; used to decorate
-- the list without one query per entry.
SELECT l.* FROM craft_wishlist_links l
JOIN craft_wishlist_items w ON w.id = l.wishlist_item_id
WHERE w.workspace_id = sqlc.arg(workspace_id)::uuid
  AND (sqlc.arg(status)::text = '' OR w.status = sqlc.arg(status))
ORDER BY l.created_at;

-- name: DeleteCraftWishlistLink :execrows
DELETE FROM craft_wishlist_links l
USING craft_wishlist_items w
WHERE l.id = $1
  AND l.wishlist_item_id = w.id
  AND w.workspace_id = $2
  AND l.wishlist_item_id = $3;
