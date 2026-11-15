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
