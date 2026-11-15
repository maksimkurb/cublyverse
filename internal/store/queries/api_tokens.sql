-- name: CreateAPIToken :one
INSERT INTO api_tokens (
    user_id,
    name,
    token_prefix,
    token_hash,
    scopes,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListAPITokens :many
-- Every token of one user, newest first. Revoked and expired rows stay visible
-- so the WebApp can show them crossed out.
SELECT * FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC, id;

-- name: GetAPIToken :one
-- Ownership is part of the lookup: a foreign id is indistinguishable from an
-- unknown one.
SELECT * FROM api_tokens
WHERE id = $1
  AND user_id = $2;

-- name: GetAPITokenByHash :one
-- Authentication lookup. Revocation, expiry and the user's deletion are
-- enforced by the database, so a stale pool or a second replica cannot
-- resurrect a dead token.
SELECT * FROM api_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: RevokeAPIToken :execrows
-- Revoking is idempotent: a second call keeps the original revoked_at.
UPDATE api_tokens
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = $1
  AND user_id = $2;

-- name: TouchAPIToken :exec
UPDATE api_tokens
SET last_used_at = now()
WHERE id = $1;

-- name: DeleteRevokedAPITokens :execrows
-- Revoked tokens are useless (lookups reject them), so the cleanup job removes
-- them. Revoked rows are kept for a while so the WebApp can show the state.
DELETE FROM api_tokens
WHERE revoked_at IS NOT NULL
  AND revoked_at < now() - interval '30 days';
