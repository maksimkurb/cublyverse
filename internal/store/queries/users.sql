-- name: CreateUser :one
INSERT INTO users (
    oidc_issuer,
    oidc_subject,
    email,
    display_name
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: GetUserByOIDC :one
SELECT * FROM users
WHERE oidc_issuer = $1 AND oidc_subject = $2;

-- name: UpsertUserFromOIDC :one
-- Sign-in is the only way a user row appears or changes: the SSO account is the
-- source of truth for the e-mail and the display name. Missing claims never
-- erase data that is already stored.
INSERT INTO users (
    oidc_issuer,
    oidc_subject,
    email,
    display_name
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (oidc_issuer, oidc_subject) DO UPDATE
SET email        = COALESCE(EXCLUDED.email, users.email),
    display_name = COALESCE(EXCLUDED.display_name, users.display_name),
    updated_at   = now()
RETURNING *;

-- name: UpdateUserTimezone :one
UPDATE users
SET timezone   = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;
