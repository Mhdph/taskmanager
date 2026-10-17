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


-- name: ArchiveProject :execrows
UPDATE projects
SET
    archived_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND id = $2;

-- name: AddProjectMember :one
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
    )
RETURNING *;

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
    AND ((archived_at IS NOT NULL) = sqlc.arg(archived)::boolean)
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
    AND ((p.archived_at IS NOT NULL) = sqlc.arg(archived)::boolean)
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

-- name: GetProjectForTenant :one
SELECT *
FROM projects
WHERE
    tenant_id = $1
    AND id = $2
    AND archived_at IS NULL;


-- name: GetProjectForUser :one
SELECT p.*
FROM projects p
JOIN project_members pm
    ON pm.project_id = p.id
WHERE
    p.tenant_id = $1
    AND pm.tenant_id = $1
    AND p.id = $2
    AND pm.user_id = $3
    AND p.archived_at IS NULL;

-- name: GetProjectMember :one
SELECT *
FROM project_members
WHERE
    tenant_id = $1
    AND project_id = $2
    AND user_id = $3;

-- name: LockProjectForMembershipMutation :one
SELECT id
FROM projects
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(project_id)
FOR UPDATE;

-- name: UpdateProject :one
UPDATE projects
    SET
        name = COALESCE(
            sqlc.narg(name)::varchar,
            name
        ),
        description = CASE
            WHEN sqlc.arg(update_description)::boolean
            THEN sqlc.narg(description)::text
            ELSE description
        END,
        updated_at = NOW()
    WHERE
        tenant_id = sqlc.arg(tenant_id)
        AND id = sqlc.arg(id)
        AND archived_at IS NULL
    RETURNING *;


-- name: UpdateProjectMemberRole :one
UPDATE project_members
SET
    role = $4
WHERE
    tenant_id = $1
    AND project_id = $2
    AND user_id = $3
RETURNING *;


-- name: DeleteProjectMember :execrows
DELETE FROM project_members
WHERE
    tenant_id = $1
    AND project_id = $2
    AND user_id = $3;

-- name: CountProjectManagers :one
SELECT COUNT(*)
FROM project_members
WHERE
    tenant_id = $1
    AND project_id = $2
    AND role = 'manager';

-- name: GetProjectStatus :one
SELECT *
FROM project_statuses
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;

-- name: LockProjectStatusForUpdate :one
SELECT * FROM project_statuses
WHERE tenant_id = $1 AND project_id = $2 AND id = $3
FOR UPDATE;

-- name: LockProjectStatusForTask :one
SELECT * FROM project_statuses
WHERE tenant_id = $1 AND project_id = $2 AND id = $3
FOR SHARE;

-- name: ProjectStatusHasTasks :one
SELECT EXISTS (
    SELECT 1 FROM tasks
    WHERE tenant_id = $1 AND project_id = $2 AND status_id = $3
) AS has_tasks;

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
-- name: UpdateProjectStatus :one
UPDATE project_statuses
SET
    name = COALESCE(
            sqlc.narg(name)::varchar,
            name
        ),
    position = COALESCE(
            sqlc.narg(position)::integer,
            position
        ),
    category = COALESCE(
            sqlc.narg(category)::varchar,
            category
        )
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
RETURNING *;


-- name: DeleteProjectStatus :execrows
DELETE FROM project_statuses
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;

-- name: ListProjectTasksForAI :many
    SELECT
        t.id,
        t.title,
        t.priority,
        t.due_at,
        t.completed_at,

        ps.name AS status_name,
        ps.category AS status_category

    FROM tasks t

    JOIN project_statuses ps
        ON ps.tenant_id = t.tenant_id
        AND ps.project_id = t.project_id
        AND ps.id = t.status_id

    WHERE
        t.tenant_id = $1
        AND t.project_id = $2
        AND t.archived_at IS NULL

    ORDER BY
        t.updated_at DESC

    LIMIT 500;

-- name: RestoreProject :one
UPDATE projects SET archived_at = NULL, updated_at = NOW()
WHERE tenant_id = $1 AND id = $2 AND archived_at IS NOT NULL
RETURNING *;
