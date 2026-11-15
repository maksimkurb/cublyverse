-- +goose Up
-- +goose StatementBegin
-- An action can be addressed to an SSO account in addition to the messenger
-- account: expected_user_id is the messenger id, principal_id is users.id. The
-- messenger id survives until the messenger is relinked, the principal id is
-- stable. An action with principal_id set is dropped by the runtime for every
-- other account.
--
-- payload carries opaque feature data (for example a reminder and occurrence
-- id) that must come back when the button is pressed. Telegram callback_data is
-- limited to 64 bytes and cannot hold it, so it lives here.
ALTER TABLE interactions
    ADD COLUMN principal_id uuid REFERENCES users (id) ON DELETE CASCADE,
    ADD COLUMN payload text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE interactions
    DROP COLUMN payload,
    DROP COLUMN principal_id;
-- +goose StatementEnd
