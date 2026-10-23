-- name: LockTaskForReminder :one
SELECT t.* FROM tasks t
JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id
JOIN project_members pm ON pm.tenant_id = t.tenant_id AND pm.project_id = t.project_id AND pm.user_id = sqlc.arg(user_id)
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.project_id = sqlc.arg(project_id) AND t.id = sqlc.arg(task_id)
    AND t.archived_at IS NULL AND p.archived_at IS NULL
FOR UPDATE OF t;

-- name: CountScheduledTaskReminders :one
SELECT count(*) FROM task_reminders
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND user_id = $4 AND status = 'scheduled';

-- name: CreateTaskReminder :one
INSERT INTO task_reminders (id, tenant_id, project_id, task_id, user_id, remind_at, minutes_before_due, note, dispatch_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetTaskReminder :one
SELECT * FROM task_reminders
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND user_id = $4 AND id = $5
FOR UPDATE;

-- name: ListTaskReminders :many
SELECT * FROM task_reminders
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
    AND task_id = sqlc.arg(task_id) AND user_id = sqlc.arg(user_id)
    AND (sqlc.narg(cursor_created_at)::timestamptz IS NULL OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);

-- name: SnoozeTaskReminder :one
UPDATE task_reminders
SET remind_at = sqlc.arg(remind_at), minutes_before_due = NULL, status = 'scheduled',
    dispatch_id = sqlc.arg(dispatch_id), attempt_count = 0, next_attempt_at = NOW(), sent_at = NULL, sent_for = NULL, updated_at = NOW()
WHERE tenant_id = sqlc.arg(tenant_id) AND project_id = sqlc.arg(project_id)
    AND task_id = sqlc.arg(task_id) AND user_id = sqlc.arg(user_id) AND id = sqlc.arg(id) AND status <> 'cancelled'
RETURNING *;

-- name: CancelTaskReminder :execrows
UPDATE task_reminders SET status = 'cancelled', sent_at = NULL, sent_for = NULL, updated_at = NOW()
WHERE tenant_id = $1 AND project_id = $2 AND task_id = $3 AND user_id = $4 AND id = $5;

-- name: LockDueTaskReminder :one
SELECT r.*, t.title, COALESCE(r.remind_at, t.due_at - r.minutes_before_due * INTERVAL '1 minute')::timestamptz AS scheduled_for
FROM task_reminders r
JOIN tasks t ON t.tenant_id = r.tenant_id AND t.project_id = r.project_id AND t.id = r.task_id
JOIN projects p ON p.tenant_id = r.tenant_id AND p.id = r.project_id
JOIN project_members pm ON pm.tenant_id = r.tenant_id AND pm.project_id = r.project_id AND pm.user_id = r.user_id
WHERE r.status = 'scheduled' AND r.next_attempt_at <= NOW()
    AND t.archived_at IS NULL AND t.completed_at IS NULL AND p.archived_at IS NULL
    AND COALESCE(r.remind_at, t.due_at - r.minutes_before_due * INTERVAL '1 minute') <= NOW()
ORDER BY r.next_attempt_at, r.created_at, r.id
LIMIT 1
FOR UPDATE OF r SKIP LOCKED;

-- name: MarkTaskReminderSent :exec
UPDATE task_reminders SET status = 'sent', sent_at = NOW(), sent_for = $2, updated_at = NOW(), attempt_count = attempt_count + 1
WHERE id = $1;

-- name: RetryTaskReminder :exec
UPDATE task_reminders SET attempt_count = attempt_count + 1, next_attempt_at = $2, updated_at = NOW()
WHERE id = $1;
