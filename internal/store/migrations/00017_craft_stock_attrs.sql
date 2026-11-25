-- +goose Up
-- +goose StatementBegin
-- What a warehouse knows about a stocked item beyond the catalogue: the values
-- it had before it was linked to an official item (key "legacy"), and
-- color_name / color_hex used while the official item has none.
ALTER TABLE craft_inventory
    ADD COLUMN attrs jsonb NOT NULL DEFAULT '{}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE craft_inventory DROP COLUMN attrs;
-- +goose StatementEnd
