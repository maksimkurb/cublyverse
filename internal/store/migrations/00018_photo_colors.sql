-- +goose Up
-- An approximate colour computed from the product photo of a catalogue that
-- publishes no colour values. It has its own column so that it never competes
-- with a published colour or with one a warehouse remembered itself.
ALTER TABLE craft_catalog_items
    ADD COLUMN photo_color_hex text NOT NULL DEFAULT ''
        CHECK (photo_color_hex = '' OR photo_color_hex ~ '^#[0-9A-F]{6}$'),
    -- The photo the last attempt was made for; a new photo is read again.
    ADD COLUMN photo_color_url text NOT NULL DEFAULT '',
    ADD COLUMN photo_color_at timestamptz;
