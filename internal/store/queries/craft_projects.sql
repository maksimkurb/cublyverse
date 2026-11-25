-- name: CreateCraftProject :one
INSERT INTO craft_projects (workspace_id, name, notes)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListCraftProjects :many
-- Projects of a workspace with the number of materials planned in each.
SELECT p.*, count(pm.catalog_item_id)::bigint AS material_count
FROM craft_projects p
LEFT JOIN craft_project_materials pm ON pm.project_id = p.id
WHERE p.workspace_id = $1
GROUP BY p.id
ORDER BY p.name;

-- name: GetCraftProject :one
SELECT * FROM craft_projects
WHERE id = $1
  AND workspace_id = $2;

-- name: UpdateCraftProject :one
UPDATE craft_projects
SET name  = $3,
    notes = $4
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: DeleteCraftProject :execrows
DELETE FROM craft_projects
WHERE id = $1
  AND workspace_id = $2;

-- name: GetCraftProjectMaterial :one
SELECT * FROM craft_project_materials
WHERE project_id = $1
  AND catalog_item_id = $2;

-- name: ListCraftProjectMaterials :many
-- Materials of a project with the catalogue item and the current stock, which
-- together give the shortage of the project.
SELECT pm.project_id, pm.catalog_item_id, pm.qty_planned, pm.qty_used,
       i.*,
       COALESCE(sc.slug, rc.slug)::text AS category_slug,
       COALESCE(sc.name, rc.name)::text AS category_name,
       m.name  AS manufacturer_name,
       COALESCE(inv.qty, 0)::numeric(12,3) AS stock_qty,
       (inv.catalog_item_id IS NOT NULL)::boolean AS in_inventory,
       inv.attrs AS stock_attrs
FROM craft_project_materials pm
JOIN craft_catalog_items i ON i.id = pm.catalog_item_id
JOIN craft_categories rc ON rc.id = i.category_id
LEFT JOIN craft_categories sc
       ON sc.workspace_id = sqlc.arg(workspace_id)::uuid AND i.workspace_id IS NULL AND sc.catalog_slug = rc.slug
LEFT JOIN craft_manufacturers m ON m.id = i.manufacturer_id
LEFT JOIN craft_inventory inv ON inv.catalog_item_id = i.id AND inv.workspace_id = sqlc.arg(workspace_id)::uuid
WHERE pm.project_id = sqlc.arg(project_id)
ORDER BY COALESCE(sc.sort_order, rc.sort_order), m.name NULLS LAST, i.article, i.name;

-- name: UpsertCraftProjectMaterial :one
-- Sets the planned quantity of a material; a NULL plan clears it.
INSERT INTO craft_project_materials (project_id, catalog_item_id, qty_planned)
VALUES (sqlc.arg(project_id), sqlc.arg(catalog_item_id), sqlc.arg(qty_planned))
ON CONFLICT (project_id, catalog_item_id) DO UPDATE
SET qty_planned = EXCLUDED.qty_planned
RETURNING *;

-- name: DeleteCraftProjectMaterial :execrows
DELETE FROM craft_project_materials
WHERE project_id = $1
  AND catalog_item_id = $2;

-- name: IncrementCraftProjectMaterialUsed :exec
UPDATE craft_project_materials
SET qty_used = qty_used + $3
WHERE project_id = $1
  AND catalog_item_id = $2;
