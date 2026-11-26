-- +goose Up
-- Beads are counted in grams. Quantities that were counted in packs become
-- grams at 5 g a pack, the weight the owner of this installation chose because
-- the imported data recorded none; every converted stock row says so.
CREATE TEMPORARY TABLE packed_beads ON COMMIT DROP AS
SELECT i.id
FROM craft_catalog_items i
JOIN craft_categories c ON c.id = i.category_id
WHERE i.unit = 'упаковка'
  AND (c.slug IN ('seed-beads', 'bugles', 'cut-beads')
       OR c.catalog_slug IN ('seed-beads', 'bugles', 'cut-beads'));

INSERT INTO craft_inventory_log (workspace_id, catalog_item_id, delta, qty_after, reason)
SELECT workspace_id, catalog_item_id, qty * 4, qty * 5, 'Перевод из упаковок в граммы (5 г в упаковке)'
FROM craft_inventory
WHERE catalog_item_id IN (SELECT id FROM packed_beads)
  AND qty > 0;

UPDATE craft_inventory
SET qty        = qty * 5,
    notes      = concat_ws(E'\n', NULLIF(notes, ''),
                     'Вес принят: ' || trim_scale(qty) || ' уп. × 5 г'),
    updated_at = now()
WHERE catalog_item_id IN (SELECT id FROM packed_beads)
  AND qty > 0;

UPDATE craft_project_materials
SET qty_planned = qty_planned * 5,
    qty_used    = qty_used * 5
WHERE catalog_item_id IN (SELECT id FROM packed_beads);

UPDATE craft_wishlist_items
SET qty_wanted = qty_wanted * 5
WHERE catalog_item_id IN (SELECT id FROM packed_beads);

UPDATE craft_catalog_items
SET unit       = 'г',
    updated_at = now()
WHERE id IN (SELECT id FROM packed_beads);

UPDATE craft_categories
SET default_unit = 'г'
WHERE default_unit = 'упаковка'
  AND (slug IN ('seed-beads', 'bugles', 'cut-beads')
       OR catalog_slug IN ('seed-beads', 'bugles', 'cut-beads'));

-- A warehouse that counts beads must offer grams.
INSERT INTO craft_units (workspace_id, name)
SELECT DISTINCT c.workspace_id, 'г'
FROM craft_categories c
WHERE c.workspace_id IS NOT NULL
  AND c.default_unit = 'г'
ON CONFLICT DO NOTHING;
