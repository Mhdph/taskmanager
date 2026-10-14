-- name: CreateAuditLog :one
INSERT INTO audit_logs (
    id,
    tenant_id,
    actor_user_id,
    action,
    entity_type,
    entity_id,
    project_id,
    task_id,
    metadata
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9
)
RETURNING *;

-- name: ListProjectAuditLogs :many
SELECT *
FROM audit_logs
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)

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
