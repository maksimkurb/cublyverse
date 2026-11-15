-- name: CreateSession :one
INSERT INTO sessions (
    token_hash,
    user_id,
    kind,
    expires_at
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- Expiry and revocation are enforced by the database so a stale pool or a
-- second replica cannot resurrect a dead session.
SELECT * FROM sessions
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: TouchSession :exec
UPDATE sessions
SET last_used_at = now()
WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE id = $1
  AND revoked_at IS NULL;

-- name: DeleteStaleSessions :execrows
-- Revoked sessions are useless (lookups reject them) and expired ones are dead
-- weight, so both can go.
DELETE FROM sessions
WHERE revoked_at IS NOT NULL
   OR expires_at <= now();
