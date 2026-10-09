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
