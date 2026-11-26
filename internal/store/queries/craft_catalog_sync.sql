-- name: CreateCraftCatalogSyncRun :one
-- The row is created before the fetch: a crashed run stays "running" and is
-- visible as such in the audit trail.
INSERT INTO craft_catalog_sync_runs (source)
VALUES ($1)
RETURNING *;

-- name: FinishCraftCatalogSyncRun :one
UPDATE craft_catalog_sync_runs
SET finished_at = now(),
    status      = $2,
    created     = $3,
    updated     = $4,
    unchanged   = $5,
    error       = $6
WHERE id = $1
RETURNING *;

-- name: GetGlobalCraftCatalogItemByArticle :one
-- Identity of a global catalogue item written by a scraper: manufacturer,
-- category and article. Workspace items are never touched by a sync.
SELECT * FROM craft_catalog_items
WHERE workspace_id IS NULL
  AND manufacturer_id = $1
  AND category_id = $2
  AND article = $3;

-- name: UpdateGlobalCraftCatalogItem :one
-- Replaces the scraper-owned fields of a global item. The caller has already
-- applied manual_override, so this query writes the final values. A row that
-- reappears in a fetch stops being deprecated.
UPDATE craft_catalog_items
SET series         = $2,
    name           = $3,
    color_name     = $4,
    color_hex      = $5,
    color_source   = $6,
    size           = $7,
    finish         = $8,
    unit           = $9,
    source         = $10,
    source_url     = $11,
    image_url      = $12,
    external_id    = $13,
    fetched_at     = $14,
    deprecated_at  = NULL,
    updated_at     = now()
WHERE id = $1
  AND workspace_id IS NULL
RETURNING *;

-- name: TouchGlobalCraftCatalogItem :execrows
-- Records a successful fetch without changing any field: bytes equal but
-- fetched_at fresh, so a later partial fetch does not deprecate the item.
UPDATE craft_catalog_items
SET fetched_at    = $2,
    deprecated_at = NULL
WHERE id = $1
  AND workspace_id IS NULL;

-- name: CountActiveGlobalCraftCatalogItems :one
-- How many items of one source were live before a run. Used to decide whether
-- a fetch that lost rows is a partial failure.
SELECT count(*) FROM craft_catalog_items
WHERE workspace_id IS NULL
  AND manufacturer_id = $1
  AND category_id = $2
  AND source = $3
  AND deprecated_at IS NULL;

-- name: DeprecateMissingGlobalCraftCatalogItems :execrows
-- Marks items of one source that the fetch did not return. Only called when the
-- fetch kept more than half of the previous catalogue; fetched_at < run start
-- protects rows the same run has just touched.
UPDATE craft_catalog_items
SET deprecated_at = now(),
    updated_at    = now()
WHERE workspace_id IS NULL
  AND manufacturer_id = $1
  AND category_id = $2
  AND source = $3
  AND deprecated_at IS NULL
  AND (fetched_at IS NULL OR fetched_at < $4);

-- name: GetLatestCraftCatalogSyncRun :one
SELECT * FROM craft_catalog_sync_runs
WHERE source = $1
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: GetLatestSuccessfulCraftCatalogSyncRun :one
SELECT * FROM craft_catalog_sync_runs
WHERE source = $1
  AND status = 'ok'
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: ListCraftPhotoColorCandidates :many
-- Global items of one source without a published colour whose photo has not
-- been read yet. A photo that gave no colour is tried again after retry_before.
SELECT id, image_url FROM craft_catalog_items
WHERE workspace_id IS NULL
  AND source = sqlc.arg(source)
  AND deprecated_at IS NULL
  AND color_hex = ''
  AND image_url <> ''
  AND (photo_color_url <> image_url
       OR (photo_color_hex = '' AND photo_color_at < sqlc.arg(retry_before)::timestamptz))
ORDER BY article, id;

-- name: SetCraftPhotoColor :exec
UPDATE craft_catalog_items
SET photo_color_hex = sqlc.arg(hex),
    photo_color_url = sqlc.arg(url),
    photo_color_at  = now()
WHERE id = sqlc.arg(id)
  AND workspace_id IS NULL;
