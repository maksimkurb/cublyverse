-- name: CreatePendingAuth :one
INSERT INTO auth_pending (
    state_hash,
    status,
    flow,
    nonce,
    verifier,
    platform,
    remote_user_id,
    conversation_id,
    return_to,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetPendingAuthByStateHash :one
SELECT * FROM auth_pending
WHERE state_hash = $1;

-- name: VerifyPendingAuthInitData :one
-- Marks the pending Telegram binding as confirmed by the Mini App: the caller
-- passes the Telegram user id taken from a signature-checked initData payload.
UPDATE auth_pending
SET init_data_verified_at = now(),
    updated_at            = now()
WHERE state_hash = $1
  AND status = 'pending_oidc'
  AND flow = 'bind'
  AND platform = 'telegram'
  AND remote_user_id = $2
  AND expires_at > now()
RETURNING *;

-- name: BeginPendingAuthConfirmation :one
-- Atomically moves a successful OIDC callback into the confirmation step. The
-- state can only be claimed once, so a replayed callback cannot issue a second
-- confirmation token.
UPDATE auth_pending
SET status             = 'awaiting_confirmation',
    user_id            = $2,
    confirm_token_hash = $3,
    expires_at         = $4,
    updated_at         = now()
WHERE state_hash = $1
  AND status = 'pending_oidc'
  AND expires_at > now()
  AND (platform <> 'telegram' OR init_data_verified_at IS NOT NULL)
RETURNING *;

-- name: CompletePendingAuth :one
-- Consumes the confirmation token's flow. Only the first POST wins.
UPDATE auth_pending
SET status     = 'completed',
    updated_at = now()
WHERE state_hash = $1
  AND status = 'awaiting_confirmation'
  AND expires_at > now()
RETURNING *;

-- name: DeletePendingAuth :exec
DELETE FROM auth_pending
WHERE state_hash = $1;

-- name: DeleteExpiredPendingAuth :execrows
DELETE FROM auth_pending
WHERE expires_at <= now();
