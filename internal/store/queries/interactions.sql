-- name: CreateInteractionByToken :one
-- The token itself is never stored: only its sha256, because the token is a
-- bearer secret that travels through the messenger.
INSERT INTO interactions (
    token_hash,
    action_id,
    choice_id,
    option_id,
    expected_user_id,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpsertInteractionByMessageKey :one
INSERT INTO interactions (
    platform,
    conversation_id,
    message_id,
    action_key,
    action_id,
    choice_id,
    option_id,
    expected_user_id,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (platform, conversation_id, message_id, action_key) DO UPDATE
SET action_id       = EXCLUDED.action_id,
    choice_id       = EXCLUDED.choice_id,
    option_id       = EXCLUDED.option_id,
    expected_user_id = EXCLUDED.expected_user_id,
    expires_at      = EXCLUDED.expires_at
RETURNING *;

-- name: GetInteractionByTokenHash :one
SELECT * FROM interactions
WHERE token_hash = $1
  AND expires_at > now();

-- name: GetInteractionByMessageKey :one
SELECT * FROM interactions
WHERE platform = $1
  AND conversation_id = $2
  AND message_id = $3
  AND action_key = $4
  AND expires_at > now();

-- name: DeleteExpiredInteractions :execrows
DELETE FROM interactions
WHERE expires_at <= now();
