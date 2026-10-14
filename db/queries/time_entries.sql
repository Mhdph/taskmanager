-- name: StartTaskTimer :one
INSERT INTO task_time_entries (id, tenant_id, project_id, task_id, user_id, started_at)
VALUES ($1, $2, $3, $4, $5, NOW())
RETURNING *;

-- name: StopTaskTimer :one
UPDATE task_time_entries
SET ended_at = GREATEST(NOW(), started_at),
    duration_seconds = GREATEST(0, FLOOR(EXTRACT(EPOCH FROM NOW() - started_at)))::bigint,
    updated_at = NOW()
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND user_id = $4
    AND ended_at IS NULL AND deleted_at IS NULL
RETURNING *;

-- name: DeleteTaskTimeEntry :execrows
UPDATE task_time_entries SET deleted_at = NOW(), updated_at = NOW()
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND id = $4 AND user_id = $5
    AND deleted_at IS NULL AND ended_at IS NOT NULL;

-- name: ListTaskTimeEntries :many
SELECT * FROM task_time_entries
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
    AND task_id = sqlc.arg(task_id) AND deleted_at IS NULL
    AND (sqlc.narg(cursor_created_at)::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);
