-- name: CreateTask :one
INSERT INTO tasks (
    id,
    tenant_id,
    project_id,
    status_id,
    parent_task_id,
    title,
    description,
    priority,
    assignee_user_id,
    created_by_user_id,
    due_at
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(tenant_id),
    sqlc.arg(project_id),
    sqlc.arg(status_id),
    sqlc.narg(parent_task_id),
    sqlc.arg(title),
    sqlc.narg(description),
    sqlc.arg(priority),
    sqlc.narg(assignee_user_id),
    sqlc.arg(created_by_user_id),
    sqlc.narg(due_at)
)
RETURNING *;
-- name: GetTaskForProject :one
SELECT *
FROM tasks
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3
    AND archived_at IS NULL;

-- name: ListTasksForProject :many
SELECT *
FROM tasks
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND archived_at IS NULL

    AND (
            sqlc.narg(status_id)::uuid IS NULL
            OR status_id = sqlc.narg(status_id)::uuid
        )

    AND (
            sqlc.narg(assignee_user_id)::text IS NULL
            OR assignee_user_id = sqlc.narg(assignee_user_id)::text
        )

    AND (
            sqlc.narg(priority)::varchar IS NULL
            OR priority = sqlc.narg(priority)::varchar
        )

    AND (
            sqlc.narg(parent_task_id)::uuid IS NULL
            OR parent_task_id = sqlc.narg(parent_task_id)::uuid
        )

    AND (
            sqlc.narg(search)::text IS NULL
            OR title ILIKE '%' || sqlc.narg(search)::text || '%'
        )

    AND (
            sqlc.narg(cursor_created_at)::timestamptz IS NULL
            OR (
                created_at,
                id
            ) < (
                sqlc.narg(cursor_created_at)::timestamptz,
                sqlc.narg(cursor_id)::uuid
            )
        )

    ORDER BY
        created_at DESC,
        id DESC

    LIMIT sqlc.arg(page_limit);
