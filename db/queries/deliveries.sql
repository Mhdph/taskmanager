-- name: LockDeliveryProject :one
SELECT id FROM projects
WHERE tenant_id = $1 AND id = $2 AND archived_at IS NULL
FOR SHARE;

-- name: CreateTaskDelivery :one
INSERT INTO task_deliveries (
    id, tenant_id, project_id, task_id, revision,
    submitted_by_user_id, reviewer_user_id, body, output_url
)
VALUES (
    sqlc.arg(id), sqlc.arg(tenant_id), sqlc.arg(project_id), sqlc.arg(task_id),
    (SELECT COALESCE(MAX(d.revision), 0) + 1 FROM task_deliveries d
        WHERE d.tenant_id = sqlc.arg(tenant_id)
            AND d.project_id = sqlc.arg(project_id) AND d.task_id = sqlc.arg(task_id)),
    sqlc.arg(submitted_by_user_id), sqlc.arg(reviewer_user_id), sqlc.arg(body), sqlc.narg(output_url)
)
RETURNING *;

-- name: GetTaskDelivery :one
SELECT * FROM task_deliveries
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND id = $4;

-- name: ListTaskDeliveries :many
SELECT * FROM task_deliveries
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id) AND task_id = sqlc.arg(task_id)
    AND (sqlc.narg(before_revision)::integer IS NULL OR revision < sqlc.narg(before_revision)::integer)
ORDER BY revision DESC
LIMIT sqlc.arg(page_limit);

-- name: ResolveTaskDelivery :one
UPDATE task_deliveries
SET status = sqlc.arg(status), reviewed_by_user_id = sqlc.arg(reviewed_by_user_id),
    review_note = sqlc.narg(review_note), reviewed_at = NOW()
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
    AND task_id = sqlc.arg(task_id) AND id = sqlc.arg(id) AND status = 'pending'
RETURNING *;
