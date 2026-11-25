-- name: ListCraftCategories :many
SELECT * FROM craft_categories
ORDER BY sort_order, name;

-- name: GetCraftCategoryBySlug :one
SELECT * FROM craft_categories
WHERE slug = $1;

-- name: ListCraftManufacturers :many
SELECT * FROM craft_manufacturers
ORDER BY name;

-- name: UpsertCraftManufacturer :one
INSERT INTO craft_manufacturers (name, website, notes)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE
SET website = CASE WHEN EXCLUDED.website <> '' THEN EXCLUDED.website ELSE craft_manufacturers.website END,
    notes   = CASE WHEN EXCLUDED.notes <> '' THEN EXCLUDED.notes ELSE craft_manufacturers.notes END
RETURNING *;

-- name: CreateCraftCatalogItem :one
INSERT INTO craft_catalog_items (
    workspace_id, category_id, manufacturer_id, article, series, name,
    color_name, color_hex, color_source, size, finish, unit, pack_size, attrs,
    source, source_url, image_url, external_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18
)
RETURNING *;

-- name: GetCraftCatalogItem :one
-- An item is visible when it is global or belongs to the workspace.
SELECT * FROM craft_catalog_items
WHERE id = $1
  AND (workspace_id IS NULL OR workspace_id = $2);

-- name: UpdateCraftCatalogItem :one
-- Only workspace-private items are editable through this query; global items
-- are changed by scrapers and the instance admin.
UPDATE craft_catalog_items
SET category_id     = $3,
    manufacturer_id = $4,
    article         = $5,
    series          = $6,
    name            = $7,
    color_name      = $8,
    color_hex       = $9,
    color_source    = $10,
    size            = $11,
    finish          = $12,
    unit            = $13,
    pack_size       = $14,
    attrs           = $15,
    updated_at      = now()
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: DeleteCraftCatalogItem :execrows
DELETE FROM craft_catalog_items
WHERE id = $1
  AND workspace_id = $2;

-- name: SearchCraftCatalog :many
-- Catalogue search over the global catalogue plus the workspace's own items.
-- Empty text filters mean "any". only_in_stock limits the result to items that are in stock.
SELECT i.*,
       c.slug  AS category_slug,
       c.name  AS category_name,
       m.name  AS manufacturer_name,
       COALESCE(inv.qty, 0)::numeric(12,3) AS stock_qty,
       (inv.catalog_item_id IS NOT NULL)::boolean AS in_inventory
FROM craft_catalog_items i
JOIN craft_categories c ON c.id = i.category_id
LEFT JOIN craft_manufacturers m ON m.id = i.manufacturer_id
LEFT JOIN craft_inventory inv
       ON inv.catalog_item_id = i.id AND inv.workspace_id = sqlc.arg(workspace_id)::uuid
WHERE (i.workspace_id IS NULL OR i.workspace_id = sqlc.arg(workspace_id)::uuid)
  AND i.deprecated_at IS NULL
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category))
  AND (sqlc.arg(manufacturer)::text = '' OR m.name ILIKE sqlc.arg(manufacturer))
  AND (sqlc.arg(query)::text = '' OR i.article ILIKE sqlc.arg(query) || '%' OR i.name ILIKE '%' || sqlc.arg(query) || '%' OR i.color_name ILIKE '%' || sqlc.arg(query) || '%')
  AND (sqlc.arg(only_in_stock)::boolean = false OR inv.catalog_item_id IS NOT NULL)
ORDER BY c.sort_order, m.name NULLS LAST, i.article, i.name
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ListCraftInventory :many
SELECT inv.workspace_id, inv.catalog_item_id, inv.qty, inv.location, inv.notes, inv.updated_at,
       i.name, i.article, i.series, i.color_name, i.color_hex, i.color_source,
       i.size, i.finish, i.unit, i.pack_size, i.workspace_id AS item_workspace_id,
       c.slug AS category_slug, c.name AS category_name,
       m.name AS manufacturer_name
FROM craft_inventory inv
JOIN craft_catalog_items i ON i.id = inv.catalog_item_id
JOIN craft_categories c ON c.id = i.category_id
LEFT JOIN craft_manufacturers m ON m.id = i.manufacturer_id
WHERE inv.workspace_id = sqlc.arg(workspace_id)::uuid
  AND (sqlc.arg(category)::text = '' OR c.slug = sqlc.arg(category))
  AND (sqlc.arg(manufacturer)::text = '' OR m.name ILIKE sqlc.arg(manufacturer))
  AND (sqlc.arg(query)::text = '' OR i.article ILIKE sqlc.arg(query) || '%' OR i.name ILIKE '%' || sqlc.arg(query) || '%' OR i.color_name ILIKE '%' || sqlc.arg(query) || '%')
  AND (sqlc.narg(max_qty)::numeric IS NULL OR inv.qty <= sqlc.narg(max_qty))
ORDER BY c.sort_order, m.name NULLS LAST, i.article, i.name
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: LockCraftInventoryRow :one
-- Serialises concurrent adjustments of one stock row.
SELECT * FROM craft_inventory
WHERE workspace_id = $1
  AND catalog_item_id = $2
FOR UPDATE;

-- name: InsertCraftInventory :one
INSERT INTO craft_inventory (workspace_id, catalog_item_id, qty, location, notes)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, catalog_item_id) DO NOTHING
RETURNING *;

-- name: SetCraftInventoryQty :one
UPDATE craft_inventory
SET qty        = $3,
    updated_at = now()
WHERE workspace_id = $1
  AND catalog_item_id = $2
RETURNING *;

-- name: UpdateCraftInventoryMeta :one
UPDATE craft_inventory
SET location   = $3,
    notes      = $4,
    updated_at = now()
WHERE workspace_id = $1
  AND catalog_item_id = $2
RETURNING *;

-- name: DeleteCraftInventory :execrows
DELETE FROM craft_inventory
WHERE workspace_id = $1
  AND catalog_item_id = $2;

-- name: InsertCraftInventoryLog :exec
INSERT INTO craft_inventory_log (
    workspace_id, catalog_item_id, user_id, project_id, delta, qty_after, reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
);

-- name: ListCraftInventoryLog :many
SELECT * FROM craft_inventory_log
WHERE workspace_id = $1
  AND catalog_item_id = $2
ORDER BY created_at DESC, id DESC
LIMIT $3;

-- name: RelinkCraftInventory :exec
-- Moves stock from a private item to another catalogue item (typically an
-- official one found later). The target row is created or the quantities are
-- summed.
INSERT INTO craft_inventory (workspace_id, catalog_item_id, qty, location, notes)
SELECT src.workspace_id, $3::uuid, src.qty, src.location, src.notes
FROM craft_inventory src
WHERE src.workspace_id = $1
  AND src.catalog_item_id = $2
ON CONFLICT (workspace_id, catalog_item_id) DO UPDATE
SET qty        = craft_inventory.qty + EXCLUDED.qty,
    updated_at = now();

-- name: AdjustCraftInventoryQty :one
-- Atomic relative change; the CHECK (qty >= 0) rejects a withdrawal larger than
-- the stock, so concurrent adjustments can never drive a quantity negative.
UPDATE craft_inventory
SET qty        = qty + $3,
    updated_at = now()
WHERE workspace_id = $1
  AND catalog_item_id = $2
RETURNING *;

-- name: GetCraftInventory :one
SELECT * FROM craft_inventory
WHERE workspace_id = $1
  AND catalog_item_id = $2;

-- name: GetCraftCategory :one
SELECT * FROM craft_categories
WHERE id = $1;

-- name: GetCraftManufacturerByName :one
SELECT * FROM craft_manufacturers
WHERE lower(name) = lower($1);
