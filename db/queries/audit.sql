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


-- name: ListAuditLogsForTenant :many
SELECT a.*
FROM audit_logs a
WHERE
    a.tenant_id = sqlc.arg(tenant_id)

    AND (
        sqlc.arg(is_admin)::boolean
        OR EXISTS (
            SELECT 1
            FROM project_members pm
            WHERE
                pm.tenant_id = a.tenant_id
                AND pm.project_id = a.project_id
                AND pm.user_id = sqlc.arg(user_id)
        )
    )

    AND (
        sqlc.narg(project_id)::uuid IS NULL
        OR a.project_id = sqlc.narg(project_id)::uuid
    )

    AND (
        sqlc.narg(task_id)::uuid IS NULL
        OR a.task_id = sqlc.narg(task_id)::uuid
    )

    AND (
        sqlc.narg(actor_user_id)::text IS NULL
        OR a.actor_user_id = sqlc.narg(actor_user_id)::text
    )

    AND (
        sqlc.narg(entity_type)::text IS NULL
        OR a.entity_type = sqlc.narg(entity_type)::text
    )

    AND (
        sqlc.narg(cursor_created_at)::timestamptz IS NULL
        OR (
            a.created_at,
            a.id
        ) < (
            sqlc.narg(cursor_created_at)::timestamptz,
            sqlc.narg(cursor_id)::uuid
        )
    )

ORDER BY
    a.created_at DESC,
    a.id DESC

LIMIT sqlc.arg(page_limit);
