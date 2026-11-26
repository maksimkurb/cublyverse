-- name: EnsureAppKey :exec
-- Keeps the first value: every instance then reads the same key.
INSERT INTO app_keys (name, value)
VALUES ($1, $2)
ON CONFLICT (name) DO NOTHING;

-- name: GetAppKey :one
SELECT value FROM app_keys
WHERE name = $1;
