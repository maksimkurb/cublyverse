-- +goose Up
-- A colour from a shop's comparison table. Like the photo colour it has its own
-- column: it never competes with a published colour or with one a warehouse
-- remembered, and it is preferred to the photo.
ALTER TABLE craft_catalog_items
    ADD COLUMN shop_color_hex text NOT NULL DEFAULT ''
        CHECK (shop_color_hex = '' OR shop_color_hex ~ '^#[0-9A-F]{6}$');

-- Secrets the server generates for itself, so that they survive restarts.
CREATE TABLE app_keys (
    name       text        PRIMARY KEY,
    value      bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
