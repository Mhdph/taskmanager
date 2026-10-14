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
    start_at,
    due_at,
    completed_at
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
    sqlc.narg(start_at),
    sqlc.narg(due_at),
    sqlc.narg(completed_at)
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

-- name: LockTaskForUpdate :one
SELECT * FROM tasks
WHERE tenant_id = $1 AND project_id = $2 AND id = $3
    AND archived_at IS NULL
FOR UPDATE;

-- name: ListTasksForProject :many
SELECT *
FROM tasks
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND ((archived_at IS NOT NULL) = sqlc.arg(archived)::boolean)

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



-- name: UpdateTask :one
    UPDATE tasks
    SET
        title = COALESCE(
            sqlc.narg(title)::varchar,
            title
        ),

        description = CASE
            WHEN sqlc.arg(update_description)::boolean
            THEN sqlc.narg(description)::text
            ELSE description
        END,

        status_id = COALESCE(
            sqlc.narg(status_id)::uuid,
            status_id
        ),

        priority = COALESCE(
            sqlc.narg(priority)::varchar,
            priority
        ),

        assignee_user_id = CASE
            WHEN sqlc.arg(update_assignee)::boolean
            THEN sqlc.narg(assignee_user_id)::text
            ELSE assignee_user_id
        END,

        parent_task_id = CASE
            WHEN sqlc.arg(update_parent_task)::boolean
            THEN sqlc.narg(parent_task_id)::uuid
            ELSE parent_task_id
        END,

        due_at = CASE
            WHEN sqlc.arg(update_due_at)::boolean
            THEN sqlc.narg(due_at)::timestamptz
            ELSE due_at
        END,

        completed_at = CASE
            WHEN sqlc.arg(update_completed_at)::boolean
            THEN sqlc.narg(completed_at)::timestamptz
            ELSE completed_at
        END,

        start_at = CASE
            WHEN sqlc.arg(update_start_at)::boolean
            THEN sqlc.narg(start_at)::timestamptz
            ELSE start_at
        END,

        updated_at = NOW()

    WHERE
        tenant_id = sqlc.arg(tenant_id)
        AND project_id = sqlc.arg(project_id)
        AND id = sqlc.arg(id)
        AND archived_at IS NULL

    RETURNING *;

-- name: LockTaskHierarchy :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));

-- name: TaskParentWouldCycle :one
WITH RECURSIVE ancestors AS (
    SELECT t.id AS ancestor_id
    FROM tasks t
    WHERE t.tenant_id = sqlc.arg(tenant_id)
      AND t.project_id = sqlc.arg(project_id)
      AND t.id = sqlc.arg(parent_task_id)

    UNION

    SELECT t.parent_task_id AS ancestor_id
    FROM tasks t
    JOIN ancestors a ON t.id = a.ancestor_id
    WHERE t.tenant_id = sqlc.arg(tenant_id)
      AND t.project_id = sqlc.arg(project_id)
      AND t.parent_task_id IS NOT NULL
)
SELECT (sqlc.arg(task_id)::uuid IN (
    SELECT * FROM ancestors
))::boolean AS would_cycle;

-- name: ArchiveTask :execrows
UPDATE tasks
SET
    archived_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3
    AND archived_at IS NULL;


-- name: AddTaskDependency :one
INSERT INTO task_dependencies (
        tenant_id,
        project_id,
        task_id,
        depends_on_task_id
    )
VALUES (
        $1,
        $2,
        $3,
        $4
    )
RETURNING *;

-- name: LockDependencyGraph :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key)::text, 0));


-- name: DeleteTaskDependency :execrows
DELETE FROM task_dependencies
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND depends_on_task_id = $4;


-- name: ListTaskDependencies :many
SELECT *
FROM task_dependencies
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
ORDER BY created_at ASC;


-- name: TaskDependsTransitivelyOn :one
WITH RECURSIVE dependency_tree AS (
    SELECT
        td.depends_on_task_id
    FROM task_dependencies td
    WHERE
        td.tenant_id = sqlc.arg(tenant_id)
        AND td.project_id = sqlc.arg(project_id)
        AND td.task_id = sqlc.arg(start_task_id)

    UNION

    SELECT
        td.depends_on_task_id
    FROM task_dependencies td
    JOIN dependency_tree tree
        ON td.task_id = tree.depends_on_task_id
    WHERE
        td.tenant_id = sqlc.arg(tenant_id)
        AND td.project_id = sqlc.arg(project_id)
)
SELECT (sqlc.arg(target_task_id)::uuid IN (
    SELECT *
    FROM dependency_tree
))::boolean AS exists;


-- name: AddTaskWatcher :one
INSERT INTO task_watchers (
    tenant_id,
    project_id,
    task_id,
    user_id
)
VALUES (
    $1,
    $2,
    $3,
    $4
)
RETURNING *;


-- name: ListTaskWatchers :many
SELECT *
FROM task_watchers
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
ORDER BY created_at ASC;


-- name: DeleteTaskWatcher :execrows
DELETE FROM task_watchers
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND user_id = $4;


-- name: TaskWatcherExists :one
SELECT EXISTS (
    SELECT 1
    FROM task_watchers
    WHERE
        tenant_id = $1
        AND project_id = $2
        AND task_id = $3
        AND user_id = $4
);

-- name: CreateTaskAttachment :one
INSERT INTO task_attachments (
    id,
    tenant_id,
    project_id,
    task_id,
    uploaded_by_user_id,
    filename,
    content_type,
    size_bytes,
    object_key
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


-- name: ListTaskAttachments :many
SELECT *
FROM task_attachments
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND deleted_at IS NULL
ORDER BY created_at DESC;


-- name: GetTaskAttachment :one
SELECT *
FROM task_attachments
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND id = $4
    AND deleted_at IS NULL;


-- name: DeleteTaskAttachment :execrows
UPDATE task_attachments
SET
    deleted_at = NOW()
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND id = $4
    AND deleted_at IS NULL;
-- name: ListMyWorkTasks :many
SELECT t.*
FROM tasks t
WHERE
    t.tenant_id = sqlc.arg(tenant_id)
    AND t.assignee_user_id = sqlc.arg(user_id)
    AND t.archived_at IS NULL

    AND EXISTS (
        SELECT 1
        FROM projects p
        WHERE p.tenant_id = t.tenant_id
          AND p.id = t.project_id
          AND p.archived_at IS NULL
          AND (
              sqlc.arg(is_admin)::boolean = true
              OR EXISTS (
                  SELECT 1
                  FROM project_members pm
                  WHERE pm.tenant_id = p.tenant_id
                    AND pm.project_id = p.id
                    AND pm.user_id = sqlc.arg(user_id)
              )
          )
    )

    AND (
        sqlc.narg(status_id)::uuid IS NULL
        OR status_id = sqlc.narg(status_id)::uuid
    )

    AND (
        sqlc.narg(priority)::varchar IS NULL
        OR priority = sqlc.narg(priority)::varchar
    )

    AND (
        sqlc.narg(due_from)::timestamptz IS NULL
        OR due_at >= sqlc.narg(due_from)::timestamptz
    )

    AND (
        sqlc.narg(due_to)::timestamptz IS NULL
        OR due_at <= sqlc.narg(due_to)::timestamptz
    )

    AND (
        sqlc.narg(overdue_only)::boolean IS NULL
        OR sqlc.narg(overdue_only)::boolean = false
        OR (
            due_at IS NOT NULL
            AND due_at < NOW()
            AND completed_at IS NULL
        )
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

-- name: RestoreTask :one
UPDATE tasks t SET archived_at = NULL, updated_at = NOW()
WHERE t.tenant_id = $1 AND t.project_id = $2 AND t.id = $3 AND t.archived_at IS NOT NULL
    AND EXISTS (SELECT 1 FROM projects p WHERE p.tenant_id = t.tenant_id AND p.id = t.project_id AND p.archived_at IS NULL)
RETURNING t.*;
