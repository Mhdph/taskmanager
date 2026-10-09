-- name: CreateProject :one
INSERT INTO projects (
    id,
    tenant_id,
    name,
    description,
    created_by_user_id
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
RETURNING *;


-- name: GetProject :one
SELECT *
FROM projects
WHERE
    tenant_id = $1
    AND id = $2
    AND archived_at IS NULL;


-- name: ListProjects :many
SELECT *
FROM projects
WHERE
    tenant_id = $1
    AND archived_at IS NULL
ORDER BY created_at DESC;


-- name: ArchiveProject :exec
UPDATE projects
SET
    archived_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND id = $2;


-- name: AddProjectMember :exec
INSERT INTO project_members (
    project_id,
    tenant_id,
    user_id,
    role
)
VALUES (
    $1,
    $2,
    $3,
    $4
);


-- name: ListProjectMembers :many
SELECT *
FROM project_members
WHERE
    tenant_id = $1
    AND project_id = $2
ORDER BY created_at ASC;


-- name: CreateProjectStatus :one
INSERT INTO project_statuses (
    id,
    tenant_id,
    project_id,
    name,
    position,
    category
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
RETURNING *;


-- name: ListProjectStatuses :many
SELECT *
FROM project_statuses
WHERE
    tenant_id = $1
    AND project_id = $2
ORDER BY position ASC;


-- name: ListProjectsForTenant :many
SELECT *
FROM projects
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND archived_at IS NULL
    AND (
        sqlc.narg(cursor_created_at)::timestamptz IS NULL
        OR
        (created_at, id) <
        (
            sqlc.narg(cursor_created_at)::timestamptz,
            sqlc.narg(cursor_id)::uuid
        )
    )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListProjectsForUser :many
SELECT p.*
FROM projects p
JOIN project_members pm
    ON pm.project_id = p.id
WHERE
    p.tenant_id = sqlc.arg(tenant_id)
    AND pm.tenant_id = sqlc.arg(tenant_id)
    AND pm.user_id = sqlc.arg(user_id)
    AND p.archived_at IS NULL
    AND (
        sqlc.narg(cursor_created_at)::timestamptz IS NULL
        OR
        (p.created_at, p.id) <
        (
            sqlc.narg(cursor_created_at)::timestamptz,
            sqlc.narg(cursor_id)::uuid
        )
    )
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg(page_limit);
