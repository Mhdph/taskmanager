-- name: GetProjectDashboardSummary :one
SELECT
    COUNT(*)::bigint AS total_tasks,

    COUNT(*) FILTER (
        WHERE completed_at IS NOT NULL
    )::bigint AS completed_tasks,

    COUNT(*) FILTER (
        WHERE
            completed_at IS NULL
            AND due_at IS NOT NULL
            AND due_at < NOW()
    )::bigint AS overdue_tasks,

    COUNT(*) FILTER (
        WHERE assignee_user_id IS NULL
    )::bigint AS unassigned_tasks

FROM tasks
WHERE
    tenant_id = $1
    AND project_id = $2
    AND archived_at IS NULL;
-- name: GetProjectDashboardByStatus :many
    SELECT
        ps.id AS status_id,
        ps.name AS status_name,
        ps.category AS status_category,

        COUNT(t.id)::bigint AS task_count

    FROM project_statuses ps

    LEFT JOIN tasks t
        ON t.tenant_id = ps.tenant_id
        AND t.project_id = ps.project_id
        AND t.status_id = ps.id
        AND t.archived_at IS NULL

    WHERE
        ps.tenant_id = $1
        AND ps.project_id = $2

    GROUP BY
        ps.id,
        ps.name,
        ps.category,
        ps.position

    ORDER BY
        ps.position ASC;


-- name: GetProjectDashboardByPriority :many
        SELECT
            priority,
            COUNT(*)::bigint AS task_count

        FROM tasks

        WHERE
            tenant_id = $1
            AND project_id = $2
            AND archived_at IS NULL

        GROUP BY priority

        ORDER BY
            CASE priority
                WHEN 'urgent' THEN 1
                WHEN 'high' THEN 2
                WHEN 'medium' THEN 3
                WHEN 'low' THEN 4
                ELSE 5
            END;
-- name: GetProjectDashboardByAssignee :many
            SELECT
                assignee_user_id,
                COUNT(*)::bigint AS task_count

            FROM tasks

            WHERE
                tenant_id = $1
                AND project_id = $2
                AND archived_at IS NULL

            GROUP BY assignee_user_id

            ORDER BY task_count DESC;
-- name: GetProjectTrackedSeconds :one
            SELECT
                COALESCE(
                    SUM(duration_seconds),
                    0
                )::bigint AS tracked_seconds

        FROM task_time_entries

            WHERE
                tenant_id = $1
                AND project_id = $2
                AND deleted_at IS NULL
                AND ended_at IS NOT NULL;
