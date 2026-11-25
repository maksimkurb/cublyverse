-- +goose Up
-- +goose StatementBegin
-- Craft supplies ("Бусинки"): a catalogue of materials, the stock a workspace
-- owns, projects and a wish list.
--
-- The catalogue is split from the stock on purpose. A catalogue item is a fact
-- about the world (PRECIOSA 1152 is a light green rocailles bead); stock is
-- what a workspace happens to hold. Catalogue items are either global
-- (workspace_id IS NULL, written by scrapers and the instance admin, read by
-- everyone) or private to one workspace (manual items without a manufacturer
-- catalogue, such as a thread of Mallorca pearls).

CREATE TABLE craft_manufacturers (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    website    text        NOT NULL DEFAULT '',
    notes      text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT craft_manufacturers_name_unique UNIQUE (name),
    CONSTRAINT craft_manufacturers_name_check CHECK (char_length(name) BETWEEN 1 AND 120)
);

-- A category is a kind of material. attr_schema is a JSON Schema for the
-- category specific attributes kept in craft_catalog_items.attrs; the WebApp
-- builds forms from it and the service validates against it, so a new category
-- needs no migration.
CREATE TABLE craft_categories (
    id           uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         text    NOT NULL,
    name         text    NOT NULL,
    default_unit text    NOT NULL,
    attr_schema  jsonb   NOT NULL DEFAULT '{}'::jsonb,
    sort_order   integer NOT NULL DEFAULT 0,
    CONSTRAINT craft_categories_slug_unique UNIQUE (slug),
    CONSTRAINT craft_categories_unit_check CHECK (
        default_unit IN ('шт', 'г', 'моток', 'м', 'см', 'упаковка', 'набор')
    )
);

INSERT INTO craft_categories (slug, name, default_unit, sort_order) VALUES
    ('floss',          'Мулине',         'моток',    10),
    ('seed-beads',     'Бисер',          'упаковка', 20),
    ('bugles',         'Стеклярус',      'г',        30),
    ('cut-beads',      'Рубка',          'г',        40),
    ('pearls',         'Жемчуг',         'шт',       50),
    ('beads',          'Бусины',         'г',        60),
    ('sequins',        'Пайетки',        'г',        70),
    ('bullion-wire',   'Канитель',       'г',        80),
    ('rhinestones',    'Стразы',         'шт',       90),
    ('settings',       'Оправа',         'шт',      100),
    ('rhinestone-chain', 'Стразовая цепь', 'см',    110),
    ('cord',           'Шнур / трос',    'м',       120),
    ('fabric',         'Ткань',          'шт',      130);

CREATE TABLE craft_catalog_items (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULL: global catalogue item. Otherwise the item is private to the
    -- workspace and deleted with it.
    workspace_id    uuid         REFERENCES workspaces (id) ON DELETE CASCADE,
    category_id     uuid         NOT NULL REFERENCES craft_categories (id),
    manufacturer_id uuid         REFERENCES craft_manufacturers (id),
    article         text         NOT NULL DEFAULT '',
    series          text         NOT NULL DEFAULT '',
    name            text         NOT NULL,
    color_name      text         NOT NULL DEFAULT '',
    -- #RRGGBB or empty. color_source says how trustworthy it is: official
    -- (published by the manufacturer), approx (estimated from a colour name or a
    -- third-party chart) or empty.
    color_hex       text         NOT NULL DEFAULT '',
    color_source    text         NOT NULL DEFAULT '',
    size            text         NOT NULL DEFAULT '',
    finish          text         NOT NULL DEFAULT '',
    -- The unit stock of this item is counted in. It belongs to the item, not to
    -- a stock row, so "+1" always means the same thing.
    unit            text         NOT NULL,
    -- How many base units (usually grams or pieces) one pack holds; NULL when
    -- unknown or not applicable.
    pack_size       numeric(12,3),
    attrs           jsonb        NOT NULL DEFAULT '{}'::jsonb,
    -- Where the row came from: manual, grist-import, or a scraper name such as
    -- dmc or preciosa.
    source          text         NOT NULL DEFAULT 'manual',
    source_url      text         NOT NULL DEFAULT '',
    image_url       text         NOT NULL DEFAULT '',
    external_id     text         NOT NULL DEFAULT '',
    fetched_at      timestamptz,
    -- Fields a human corrected; a scraper never overwrites them.
    manual_override jsonb        NOT NULL DEFAULT '{}'::jsonb,
    deprecated_at   timestamptz,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT craft_catalog_items_name_check CHECK (char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT craft_catalog_items_hex_check CHECK (color_hex = '' OR color_hex ~ '^#[0-9A-Fa-f]{6}$'),
    CONSTRAINT craft_catalog_items_source_check CHECK (color_source IN ('', 'official', 'approx')),
    CONSTRAINT craft_catalog_items_unit_check CHECK (
        unit IN ('шт', 'г', 'моток', 'м', 'см', 'упаковка', 'набор')
    ),
    CONSTRAINT craft_catalog_items_pack_check CHECK (pack_size IS NULL OR pack_size > 0)
);

-- One article per manufacturer and category, separately for the global
-- catalogue and for every workspace. NULLs are folded to the zero uuid so the
-- index also covers items without a manufacturer.
CREATE UNIQUE INDEX craft_catalog_items_article_uidx ON craft_catalog_items (
    COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'),
    COALESCE(manufacturer_id, '00000000-0000-0000-0000-000000000000'),
    category_id,
    article
) WHERE article <> '';

CREATE INDEX craft_catalog_items_workspace_idx ON craft_catalog_items (workspace_id, category_id);
CREATE INDEX craft_catalog_items_manufacturer_idx ON craft_catalog_items (manufacturer_id, article);

-- What a workspace holds. One row per catalogue item; qty is in the unit of the
-- item.
CREATE TABLE craft_inventory (
    workspace_id    uuid          NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    catalog_item_id uuid          NOT NULL REFERENCES craft_catalog_items (id) ON DELETE CASCADE,
    qty             numeric(12,3) NOT NULL DEFAULT 0,
    location        text          NOT NULL DEFAULT '',
    notes           text          NOT NULL DEFAULT '',
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, catalog_item_id),
    CONSTRAINT craft_inventory_qty_check CHECK (qty >= 0)
);

CREATE TABLE craft_projects (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    notes        text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT craft_projects_name_check CHECK (char_length(name) BETWEEN 1 AND 200)
);
CREATE INDEX craft_projects_workspace_idx ON craft_projects (workspace_id, name);

CREATE TABLE craft_project_materials (
    project_id      uuid          NOT NULL REFERENCES craft_projects (id) ON DELETE CASCADE,
    catalog_item_id uuid          NOT NULL REFERENCES craft_catalog_items (id) ON DELETE CASCADE,
    qty_planned     numeric(12,3),
    qty_used        numeric(12,3) NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, catalog_item_id),
    CONSTRAINT craft_project_materials_qty_check CHECK (
        qty_used >= 0 AND (qty_planned IS NULL OR qty_planned >= 0)
    )
);

-- Every stock change, for "who used this and when". The row stays when its
-- project or user disappears.
CREATE TABLE craft_inventory_log (
    id              bigserial     PRIMARY KEY,
    workspace_id    uuid          NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    catalog_item_id uuid          NOT NULL REFERENCES craft_catalog_items (id) ON DELETE CASCADE,
    user_id         uuid          REFERENCES users (id) ON DELETE SET NULL,
    project_id      uuid          REFERENCES craft_projects (id) ON DELETE SET NULL,
    delta           numeric(12,3) NOT NULL,
    qty_after       numeric(12,3) NOT NULL,
    reason          text          NOT NULL DEFAULT '',
    created_at      timestamptz   NOT NULL DEFAULT now()
);
CREATE INDEX craft_inventory_log_item_idx ON craft_inventory_log (workspace_id, catalog_item_id, created_at DESC);

-- Wish list. An entry may point at a catalogue item or be free text for
-- something that is not in any catalogue yet.
CREATE TABLE craft_wishlist_items (
    id              uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid          NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    catalog_item_id uuid          REFERENCES craft_catalog_items (id) ON DELETE CASCADE,
    title           text          NOT NULL,
    qty_wanted      numeric(12,3),
    priority        smallint      NOT NULL DEFAULT 0,
    note            text          NOT NULL DEFAULT '',
    status          text          NOT NULL DEFAULT 'wanted',
    created_by      uuid          REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT craft_wishlist_items_title_check CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT craft_wishlist_items_status_check CHECK (status IN ('wanted', 'ordered', 'bought')),
    CONSTRAINT craft_wishlist_items_qty_check CHECK (qty_wanted IS NULL OR qty_wanted > 0)
);
CREATE INDEX craft_wishlist_items_workspace_idx ON craft_wishlist_items (workspace_id, status, priority DESC);
-- A catalogue item is wanted at most once at a time.
CREATE UNIQUE INDEX craft_wishlist_items_wanted_uidx
    ON craft_wishlist_items (workspace_id, catalog_item_id)
    WHERE catalog_item_id IS NOT NULL AND status = 'wanted';

CREATE TABLE craft_wishlist_links (
    id               uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    wishlist_item_id uuid          NOT NULL REFERENCES craft_wishlist_items (id) ON DELETE CASCADE,
    url              text          NOT NULL,
    shop_name        text          NOT NULL DEFAULT '',
    price            numeric(12,2),
    currency         text          NOT NULL DEFAULT '',
    in_stock         boolean,
    checked_at       timestamptz,
    created_at       timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT craft_wishlist_links_url_check CHECK (url ~ '^https?://'),
    CONSTRAINT craft_wishlist_links_price_check CHECK (price IS NULL OR price >= 0)
);
CREATE INDEX craft_wishlist_links_item_idx ON craft_wishlist_links (wishlist_item_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE craft_wishlist_links;
DROP TABLE craft_wishlist_items;
DROP TABLE craft_inventory_log;
DROP TABLE craft_project_materials;
DROP TABLE craft_projects;
DROP TABLE craft_inventory;
DROP TABLE craft_catalog_items;
DROP TABLE craft_categories;
DROP TABLE craft_manufacturers;
-- +goose StatementEnd
