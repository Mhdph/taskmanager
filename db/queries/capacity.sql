-- name: GetMemberCapacity :one
SELECT * FROM member_capacities WHERE tenant_id = $1 AND user_id = $2;

-- name: SetMemberCapacity :one
INSERT INTO member_capacities (tenant_id, user_id, daily_capacity_seconds, working_days, timezone)
SELECT m.tenant_id, m.user_id, sqlc.arg(daily_capacity_seconds), sqlc.arg(working_days)::smallint[], sqlc.arg(timezone)
FROM tenant_members m WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.user_id = sqlc.arg(user_id)
ON CONFLICT (tenant_id, user_id) DO UPDATE
SET daily_capacity_seconds = EXCLUDED.daily_capacity_seconds, working_days = EXCLUDED.working_days,
    timezone = EXCLUDED.timezone, updated_at = NOW()
RETURNING *;

-- name: ClearMemberCapacity :exec
DELETE FROM member_capacities WHERE tenant_id = $1 AND user_id = $2;

-- name: ListWorkloadMembers :many
SELECT m.user_id, m.first_name, m.last_name, c.daily_capacity_seconds, c.working_days, c.timezone
FROM tenant_members m
LEFT JOIN member_capacities c ON c.tenant_id = m.tenant_id AND c.user_id = m.user_id
WHERE m.tenant_id = sqlc.arg(tenant_id) AND m.status = 'active'
    AND (sqlc.narg(user_id)::text IS NULL OR m.user_id = sqlc.narg(user_id)::text)
    AND m.user_id > sqlc.arg(after_user_id)::text
ORDER BY m.user_id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListWorkloadTasks :many
SELECT t.id, t.assignee_user_id, t.start_at, t.due_at, t.estimate_seconds,
    GREATEST(COALESCE(t.estimate_seconds, 0) - COALESCE(tracked.seconds, 0), 0)::bigint AS remaining_seconds
FROM tasks t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
JOIN project_statuses s ON s.tenant_id = t.tenant_id AND s.project_id = t.project_id AND s.id = t.status_id
LEFT JOIN member_capacities c ON c.tenant_id = t.tenant_id AND c.user_id = t.assignee_user_id
LEFT JOIN LATERAL (
    SELECT SUM(CASE WHEN e.ended_at IS NULL
        THEN GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (NOW() - e.started_at))))::bigint
        ELSE e.duration_seconds END)::bigint AS seconds
    FROM task_time_entries e
    WHERE e.tenant_id = t.tenant_id AND e.project_id = t.project_id AND e.task_id = t.id AND e.deleted_at IS NULL
) tracked ON TRUE
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.assignee_user_id = ANY(sqlc.arg(user_ids)::text[])
    AND t.archived_at IS NULL AND p.archived_at IS NULL AND t.completed_at IS NULL AND s.category <> 'done'
    AND (sqlc.arg(full_access)::boolean OR EXISTS (
        SELECT 1 FROM project_members pm WHERE pm.tenant_id = t.tenant_id AND pm.project_id = t.project_id AND pm.user_id = sqlc.arg(caller_user_id)
    ))
    AND (COALESCE(t.start_at, t.due_at) IS NULL OR
        (COALESCE(t.start_at, t.due_at) AT TIME ZONE COALESCE(c.timezone, sqlc.arg(timezone)::text))::date <= sqlc.arg(to_date)::date);
