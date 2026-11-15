-- name: UpsertBinding :one
-- A messenger account belongs to exactly one user. Re-binding the same
-- messenger account to another user therefore moves the row instead of
-- creating a duplicate.
INSERT INTO messenger_bindings (
    user_id,
    platform,
    remote_user_id,
    conversation_id,
    verified_at,
    metadata
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (platform, remote_user_id) DO UPDATE
SET user_id         = EXCLUDED.user_id,
    conversation_id = EXCLUDED.conversation_id,
    verified_at     = EXCLUDED.verified_at,
    metadata        = EXCLUDED.metadata,
    updated_at      = now()
RETURNING *;

-- name: GetBinding :one
SELECT * FROM messenger_bindings
WHERE platform = $1 AND remote_user_id = $2;

-- name: ListBindingsByUser :many
SELECT * FROM messenger_bindings
WHERE user_id = $1
ORDER BY platform, remote_user_id;
