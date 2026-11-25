-- Categories, manufacturers, units and official catalogues of one warehouse.

-- name: ListCraftCategories :many
SELECT c.*,
       (SELECT count(*) FROM craft_catalog_items i WHERE i.category_id = c.id)::bigint AS own_items
FROM craft_categories c
WHERE c.workspace_id = sqlc.arg(workspace_id)::uuid
ORDER BY c.sort_order, lower(c.name);

-- name: GetCraftCategory :one
SELECT * FROM craft_categories
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid;

-- name: FindCraftCategory :one
-- By slug or, for people and models, by name.
SELECT * FROM craft_categories
WHERE workspace_id = sqlc.arg(workspace_id)::uuid
  AND (slug = sqlc.arg(ref)::text OR lower(name) = lower(sqlc.arg(ref)::text))
ORDER BY (slug = sqlc.arg(ref)::text) DESC
LIMIT 1;

-- name: CreateCraftCategory :one
INSERT INTO craft_categories (workspace_id, slug, name, default_unit, fields, sort_order)
VALUES (
    sqlc.arg(workspace_id)::uuid, sqlc.arg(slug), sqlc.arg(name), sqlc.arg(default_unit), sqlc.arg(fields),
    (SELECT COALESCE(max(sort_order), 0) + 10 FROM craft_categories WHERE workspace_id = sqlc.arg(workspace_id)::uuid)
)
RETURNING *;

-- name: UpdateCraftCategory :one
UPDATE craft_categories
SET name         = sqlc.arg(name),
    default_unit = sqlc.arg(default_unit),
    fields       = sqlc.arg(fields)
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid
RETURNING *;

-- name: DeleteCraftCategory :execrows
DELETE FROM craft_categories
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid;

-- name: CountCraftItemsOfCategory :one
SELECT count(*) FROM craft_catalog_items
WHERE category_id = $1;

-- name: SeedCraftCategories :exec
-- The «Рукоделие» template: a copy of the reference categories.
INSERT INTO craft_categories (workspace_id, slug, name, default_unit, sort_order, catalog_slug)
SELECT sqlc.arg(workspace_id)::uuid, slug, name, default_unit, sort_order, slug
FROM craft_categories
WHERE workspace_id IS NULL
ON CONFLICT DO NOTHING;

-- name: LinkCraftCategoryToCatalog :exec
-- Makes sure the official items of one reference category have a warehouse
-- category: an own category with the same name is linked, else a copy is made.
WITH reference AS (
    SELECT * FROM craft_categories r
    WHERE r.workspace_id IS NULL AND r.slug = sqlc.arg(slug)::text
), linked AS (
    UPDATE craft_categories c
    SET catalog_slug = reference.slug
    FROM reference
    WHERE c.workspace_id = sqlc.arg(workspace_id)::uuid
      AND c.catalog_slug = ''
      AND (c.slug = reference.slug OR lower(c.name) = lower(reference.name))
      AND NOT EXISTS (
          SELECT 1 FROM craft_categories other
          WHERE other.workspace_id = sqlc.arg(workspace_id)::uuid AND other.catalog_slug = reference.slug
      )
    RETURNING c.id
)
INSERT INTO craft_categories (workspace_id, slug, name, default_unit, sort_order, catalog_slug)
SELECT sqlc.arg(workspace_id)::uuid, slug, name, default_unit, sort_order, slug
FROM reference
WHERE NOT EXISTS (SELECT 1 FROM linked)
ON CONFLICT DO NOTHING;

-- name: ListCraftManufacturers :many
-- Own manufacturers plus those of the connected official catalogues.
SELECT m.*,
       (SELECT count(*) FROM craft_catalog_items i
        WHERE i.manufacturer_id = m.id AND i.workspace_id = sqlc.arg(workspace_id)::uuid)::bigint AS own_items
FROM craft_manufacturers m
WHERE m.workspace_id = sqlc.arg(workspace_id)::uuid
   OR (m.workspace_id IS NULL AND EXISTS (
          SELECT 1 FROM craft_catalog_items g
          JOIN craft_space_catalogs sources
            ON sources.source = g.source AND sources.workspace_id = sqlc.arg(workspace_id)::uuid
          WHERE g.workspace_id IS NULL AND g.manufacturer_id = m.id
      ))
ORDER BY lower(m.name);

-- name: FindCraftManufacturer :one
-- The own manufacturer of a name, else the official one.
SELECT * FROM craft_manufacturers
WHERE lower(name) = lower(sqlc.arg(name)::text)
  AND (workspace_id = sqlc.arg(workspace_id)::uuid OR workspace_id IS NULL)
ORDER BY workspace_id NULLS LAST
LIMIT 1;

-- name: CreateCraftManufacturer :one
INSERT INTO craft_manufacturers (workspace_id, name, website)
VALUES (sqlc.arg(workspace_id)::uuid, sqlc.arg(name), sqlc.arg(website))
RETURNING *;

-- name: UpdateCraftManufacturer :one
UPDATE craft_manufacturers
SET name    = sqlc.arg(name),
    website = sqlc.arg(website)
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid
RETURNING *;

-- name: DeleteCraftManufacturer :execrows
DELETE FROM craft_manufacturers
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid;

-- name: CountCraftItemsOfManufacturer :one
SELECT count(*) FROM craft_catalog_items
WHERE manufacturer_id = $1;

-- name: ListCraftUnits :many
SELECT * FROM craft_units
WHERE workspace_id = $1
ORDER BY sort_order, lower(name);

-- name: GetCraftUnit :one
SELECT * FROM craft_units
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid;

-- name: FindCraftUnit :one
SELECT * FROM craft_units
WHERE workspace_id = sqlc.arg(workspace_id)::uuid
  AND lower(name) = lower(sqlc.arg(name)::text);

-- name: CreateCraftUnit :one
INSERT INTO craft_units (workspace_id, name, sort_order)
VALUES (
    sqlc.arg(workspace_id)::uuid, sqlc.arg(name),
    (SELECT COALESCE(max(sort_order), 0) + 10 FROM craft_units WHERE workspace_id = sqlc.arg(workspace_id)::uuid)
)
RETURNING *;

-- name: EnsureCraftUnit :exec
INSERT INTO craft_units (workspace_id, name, sort_order)
VALUES (
    sqlc.arg(workspace_id)::uuid, sqlc.arg(name),
    (SELECT COALESCE(max(sort_order), 0) + 10 FROM craft_units WHERE workspace_id = sqlc.arg(workspace_id)::uuid)
)
ON CONFLICT DO NOTHING;

-- name: RenameCraftUnit :one
UPDATE craft_units
SET name = sqlc.arg(name)
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid
RETURNING *;

-- name: RenameCraftItemUnit :exec
UPDATE craft_catalog_items
SET unit = sqlc.arg(new_name), updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id)::uuid
  AND unit = sqlc.arg(old_name);

-- name: RenameCraftCategoryUnit :exec
UPDATE craft_categories
SET default_unit = sqlc.arg(new_name)
WHERE workspace_id = sqlc.arg(workspace_id)::uuid
  AND default_unit = sqlc.arg(old_name);

-- name: CountCraftUnitUses :one
SELECT (SELECT count(*) FROM craft_catalog_items i
        WHERE i.workspace_id = sqlc.arg(workspace_id)::uuid AND i.unit = sqlc.arg(name))::bigint
     + (SELECT count(*) FROM craft_categories c
        WHERE c.workspace_id = sqlc.arg(workspace_id)::uuid AND c.default_unit = sqlc.arg(name))::bigint AS uses;

-- name: DeleteCraftUnit :execrows
DELETE FROM craft_units
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::uuid;

-- name: ListCraftSpaceCatalogs :many
SELECT source FROM craft_space_catalogs
WHERE workspace_id = $1
ORDER BY source;

-- name: ClearCraftSpaceCatalogs :exec
DELETE FROM craft_space_catalogs
WHERE workspace_id = $1;

-- name: AddCraftSpaceCatalog :exec
INSERT INTO craft_space_catalogs (workspace_id, source)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: CountGlobalCraftItemsBySource :many
SELECT source, count(*)::bigint AS items
FROM craft_catalog_items
WHERE workspace_id IS NULL
  AND deprecated_at IS NULL
GROUP BY source;
