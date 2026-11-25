-- name: CreateWorkspace :one
INSERT INTO workspaces (name, created_by)
VALUES ($1, $2)
RETURNING *;

-- name: AddWorkspaceMember :exec
-- Idempotent: adding an existing member changes nothing.
INSERT INTO workspace_members (workspace_id, user_id, added_by)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id) DO NOTHING;

-- name: RemoveWorkspaceMember :execrows
DELETE FROM workspace_members
WHERE workspace_id = $1
  AND user_id = $2;

-- name: IsWorkspaceMember :one
SELECT EXISTS (
    SELECT 1 FROM workspace_members
    WHERE workspace_id = $1
      AND user_id = $2
);

-- name: ListWorkspacesForUser :many
-- Membership is part of the lookup: a foreign workspace is indistinguishable
-- from an unknown one.
SELECT w.*
FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = $1
ORDER BY w.name, w.id;

-- name: GetWorkspaceForUser :one
SELECT w.*
FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
WHERE w.id = $1
  AND m.user_id = $2;

-- name: RenameWorkspace :one
UPDATE workspaces
SET name       = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces
WHERE id = $1;

-- name: ListWorkspaceMembers :many
SELECT u.id, u.email, u.display_name, m.created_at AS joined_at
FROM workspace_members m
JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at, u.id;

-- name: CountWorkspaceMembers :one
SELECT count(*) FROM workspace_members
WHERE workspace_id = $1;

-- name: ListUsersNotInWorkspace :many
-- Candidates for an invitation: every user who has signed in at least once (a
-- user row only appears at sign-in) and is not yet a member. $2 is an optional
-- case-insensitive substring of the name or e-mail.
SELECT u.id, u.email, u.display_name
FROM users u
WHERE NOT EXISTS (
        SELECT 1 FROM workspace_members m
        WHERE m.workspace_id = $1
          AND m.user_id = u.id
      )
  AND (
        $2::text = ''
        OR u.display_name ILIKE '%' || $2 || '%'
        OR u.email ILIKE '%' || $2 || '%'
      )
ORDER BY u.display_name NULLS LAST, u.email NULLS LAST, u.id
LIMIT 50;
