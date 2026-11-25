-- +goose Up
-- +goose StatementBegin
-- Every warehouse manages its own categories, manufacturers and units. Rows of
-- craft_categories and craft_manufacturers with workspace_id IS NULL are the
-- reference data of the official catalogues: global catalogue items point at
-- them and no warehouse can change them. A warehouse category with a
-- catalog_slug is the one the official items of that reference category are
-- shown under.
ALTER TABLE craft_categories
    ADD COLUMN workspace_id uuid  REFERENCES workspaces (id) ON DELETE CASCADE,
    ADD COLUMN catalog_slug text  NOT NULL DEFAULT '',
    -- Extra fields of the items of the category:
    -- [{"key": "...", "label": "...", "type": "text|number|bool|select", "options": []}]
    ADD COLUMN fields       jsonb NOT NULL DEFAULT '[]'::jsonb,
    DROP COLUMN attr_schema,
    DROP CONSTRAINT craft_categories_slug_unique,
    DROP CONSTRAINT craft_categories_unit_check,
    ADD CONSTRAINT craft_categories_name_check CHECK (char_length(name) BETWEEN 1 AND 80);
CREATE UNIQUE INDEX craft_categories_slug_uidx
    ON craft_categories (COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'), slug);
CREATE UNIQUE INDEX craft_categories_name_uidx
    ON craft_categories (COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'), lower(name));
CREATE UNIQUE INDEX craft_categories_link_uidx
    ON craft_categories (workspace_id, catalog_slug) WHERE catalog_slug <> '';

ALTER TABLE craft_manufacturers
    ADD COLUMN workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE,
    DROP CONSTRAINT craft_manufacturers_name_unique;
CREATE UNIQUE INDEX craft_manufacturers_name_uidx
    ON craft_manufacturers (COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'), lower(name));

-- Units are whatever the warehouse defines.
ALTER TABLE craft_catalog_items DROP CONSTRAINT craft_catalog_items_unit_check;

CREATE TABLE craft_units (
    id           uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text    NOT NULL,
    sort_order   integer NOT NULL DEFAULT 0,
    CONSTRAINT craft_units_name_check CHECK (char_length(name) BETWEEN 1 AND 24)
);
CREATE UNIQUE INDEX craft_units_name_uidx ON craft_units (workspace_id, lower(name));

-- The official catalogues a warehouse sees.
CREATE TABLE craft_space_catalogs (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    source       text NOT NULL,
    PRIMARY KEY (workspace_id, source),
    CONSTRAINT craft_space_catalogs_source_check CHECK (source IN ('dmc', 'preciosa'))
);

-- Existing warehouses get what they had: every category, every unit and both
-- official catalogues.
INSERT INTO craft_categories (workspace_id, slug, name, default_unit, sort_order, catalog_slug)
SELECT w.id, c.slug, c.name, c.default_unit, c.sort_order, c.slug
FROM workspaces w
CROSS JOIN craft_categories c
WHERE w.app = 'warehouse'
  AND c.workspace_id IS NULL;

INSERT INTO craft_units (workspace_id, name, sort_order)
SELECT w.id, u.name, u.sort_order
FROM workspaces w
CROSS JOIN (VALUES ('шт', 10), ('г', 20), ('моток', 30), ('м', 40), ('см', 50), ('упаковка', 60), ('набор', 70))
    AS u (name, sort_order)
WHERE w.app = 'warehouse';

INSERT INTO craft_space_catalogs (workspace_id, source)
SELECT w.id, s.source
FROM workspaces w
CROSS JOIN (VALUES ('dmc'), ('preciosa')) AS s (source)
WHERE w.app = 'warehouse';

-- Private items move to the warehouse copies of their categories.
UPDATE craft_catalog_items i
SET category_id = mine.id
FROM craft_categories reference
JOIN craft_categories mine ON mine.catalog_slug = reference.slug
WHERE i.workspace_id IS NOT NULL
  AND reference.id = i.category_id
  AND reference.workspace_id IS NULL
  AND mine.workspace_id = i.workspace_id;

-- Manufacturers that only private items use belong to those warehouses.
INSERT INTO craft_manufacturers (workspace_id, name, website, notes)
SELECT DISTINCT i.workspace_id, m.name, m.website, m.notes
FROM craft_catalog_items i
JOIN craft_manufacturers m ON m.id = i.manufacturer_id
WHERE i.workspace_id IS NOT NULL
  AND m.workspace_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM craft_catalog_items g
      WHERE g.workspace_id IS NULL AND g.manufacturer_id = m.id
  );

UPDATE craft_catalog_items i
SET manufacturer_id = mine.id
FROM craft_manufacturers shared
JOIN craft_manufacturers mine ON lower(mine.name) = lower(shared.name)
WHERE i.workspace_id IS NOT NULL
  AND shared.id = i.manufacturer_id
  AND shared.workspace_id IS NULL
  AND mine.workspace_id = i.workspace_id;

DELETE FROM craft_manufacturers m
WHERE m.workspace_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM craft_catalog_items i WHERE i.manufacturer_id = m.id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Warehouse reference data cannot be represented in the old schema; a rollback
-- in production is a database restore.
SELECT 1;
-- +goose StatementEnd
