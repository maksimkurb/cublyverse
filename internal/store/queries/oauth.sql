-- name: UpsertOAuthClient :one
INSERT INTO oauth_clients (client_id, kind, name, redirect_uris, fetched_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (client_id) DO UPDATE
SET name          = EXCLUDED.name,
    redirect_uris = EXCLUDED.redirect_uris,
    fetched_at    = EXCLUDED.fetched_at
RETURNING *;

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients
WHERE client_id = $1;

-- name: CreateOAuthCode :exec
INSERT INTO oauth_codes (
    code_hash, family_id, client_id, user_id, scopes, resource,
    redirect_uri, code_challenge, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
);

-- name: ConsumeOAuthCode :one
-- Marks the code used and returns it; a second call finds nothing. The caller
-- distinguishes "replayed" with GetOAuthCode.
UPDATE oauth_codes
SET used_at = now()
WHERE code_hash = $1
  AND used_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: GetOAuthCode :one
SELECT * FROM oauth_codes
WHERE code_hash = $1;

-- name: CreateOAuthToken :one
INSERT INTO oauth_tokens (
    family_id, client_id, user_id, scopes, resource,
    access_hash, access_expires_at, refresh_hash, refresh_expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetOAuthTokenByAccess :one
SELECT * FROM oauth_tokens
WHERE access_hash = $1;

-- name: GetOAuthTokenByRefresh :one
SELECT * FROM oauth_tokens
WHERE refresh_hash = $1;

-- name: UseOAuthRefresh :one
-- Atomically spends a refresh token; a concurrent second use finds no row.
UPDATE oauth_tokens
SET refresh_used_at = now()
WHERE id = $1
  AND refresh_used_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeOAuthFamily :exec
UPDATE oauth_tokens
SET revoked_at = now()
WHERE family_id = $1
  AND revoked_at IS NULL;

-- name: TouchOAuthToken :exec
UPDATE oauth_tokens
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ListOAuthGrantsForUser :many
-- One row per live family: the most recent token of each, with the client name.
SELECT DISTINCT ON (t.family_id)
       t.family_id, t.client_id, c.name AS client_name,
       t.scopes, t.created_at, t.last_used_at, t.refresh_expires_at
FROM oauth_tokens t
JOIN oauth_clients c ON c.client_id = t.client_id
WHERE t.user_id = $1
  AND t.revoked_at IS NULL
  AND t.refresh_expires_at > now()
ORDER BY t.family_id, t.created_at DESC;

-- name: RevokeOAuthFamilyForUser :execrows
UPDATE oauth_tokens
SET revoked_at = now()
WHERE family_id = $1
  AND user_id = $2
  AND revoked_at IS NULL;

-- name: DeleteExpiredOAuth :exec
WITH c AS (
    DELETE FROM oauth_codes WHERE expires_at < now() - interval '1 day'
)
DELETE FROM oauth_tokens
WHERE refresh_expires_at < now() - interval '1 day';

-- name: PruneOAuthClients :execrows
-- Dynamically registered clients that never completed an authorization within a
-- week are noise from the open registration endpoint.
DELETE FROM oauth_clients c
WHERE c.kind = 'dcr'
  AND c.created_at < now() - interval '7 days'
  AND NOT EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.client_id = c.client_id)
  AND NOT EXISTS (SELECT 1 FROM oauth_codes k WHERE k.client_id = c.client_id);
