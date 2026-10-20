-- Sprints

-- name: CreateSprint :one
INSERT INTO sprints (
    id,
    tenant_id,
    project_id,
    name,
    goal,
    planned_start_at,
    planned_end_at,
    state,
    created_by_user_id
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(tenant_id),
    sqlc.arg(project_id),
    sqlc.arg(name),
    sqlc.narg(goal),
    sqlc.narg(planned_start_at),
    sqlc.arg(planned_end_at),
    'planned',
    sqlc.arg(created_by_user_id)
)
RETURNING *;


-- name: GetSprint :one
SELECT *
FROM sprints
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id);


-- The row lock serializes lifecycle transitions and assignment moves.
-- name: LockSprint :one
SELECT *
FROM sprints
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
FOR UPDATE;


-- name: ListSprintsForProject :many
SELECT *
FROM sprints
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)

    AND (
        sqlc.narg(state)::text IS NULL
        OR state = sqlc.narg(state)::text
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


-- name: UpdateSprint :one
UPDATE sprints
SET
    name = COALESCE(sqlc.narg(name)::varchar, name),
    goal = CASE
        WHEN sqlc.arg(goal_is_set)::boolean THEN sqlc.narg(goal)::text
        ELSE goal
    END,
    planned_start_at = CASE
        WHEN sqlc.arg(planned_start_is_set)::boolean
            THEN sqlc.narg(planned_start_at)::timestamptz
        ELSE planned_start_at
    END,
    planned_end_at = COALESCE(
        sqlc.narg(planned_end_at)::timestamptz,
        planned_end_at
    ),
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
RETURNING *;


-- name: DeleteSprint :execrows
DELETE FROM sprints
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id);


-- name: StartSprint :one
UPDATE sprints
SET
    state = 'active',
    started_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
    AND state = 'planned'
RETURNING *;


-- name: CompleteSprint :one
UPDATE sprints
SET
    state = 'completed',
    completed_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
    AND state = 'active'
RETURNING *;


-- name: CountActiveSprintsForProject :one
SELECT COUNT(*)
FROM sprints
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND state = 'active';


-- Task membership

-- name: ListSprintTasks :many
SELECT *
FROM tasks
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND sprint_id = sqlc.arg(sprint_id)
ORDER BY created_at ASC, id ASC;


-- name: GetSprintTask :one
SELECT *
FROM tasks
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id);


-- The task lock ignores the archived flag: an archived task still has to
-- be readable to explain why it cannot be reassigned.
-- name: LockSprintTask :one
SELECT *
FROM tasks
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
FOR UPDATE;


-- name: SetTaskSprint :one
UPDATE tasks
SET
    sprint_id = sqlc.narg(sprint_id)::uuid,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
RETURNING *;


-- The conditional move keeps a completion from overwriting a task that
-- was moved to another sprint while the completion was running.
-- name: MoveTaskOutOfSprint :execrows
UPDATE tasks
SET
    sprint_id = sqlc.narg(sprint_id)::uuid,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND id = sqlc.arg(id)
    AND sprint_id = sqlc.arg(from_sprint_id)::uuid;


-- name: RecordSprintMembership :exec
INSERT INTO sprint_task_membership (
    id,
    tenant_id,
    project_id,
    sprint_id,
    task_id,
    actor_user_id,
    action
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(tenant_id),
    sqlc.arg(project_id),
    sqlc.arg(sprint_id),
    sqlc.arg(task_id),
    sqlc.arg(actor_user_id),
    sqlc.arg(action)
);


-- Commitment snapshot

-- name: InsertSprintCommitment :exec
INSERT INTO sprint_commitments (
    sprint_id,
    tenant_id,
    task_id,
    title,
    estimate_seconds,
    archived
)
VALUES (
    sqlc.arg(sprint_id),
    sqlc.arg(tenant_id),
    sqlc.arg(task_id),
    sqlc.arg(title),
    sqlc.narg(estimate_seconds),
    sqlc.arg(archived)::boolean
)
ON CONFLICT (sprint_id, task_id) DO NOTHING;


-- name: ListSprintCommitments :many
SELECT *
FROM sprint_commitments
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND sprint_id = sqlc.arg(sprint_id)
ORDER BY task_id ASC;


-- Completion report

-- name: InsertSprintReport :one
INSERT INTO sprint_reports (
    sprint_id,
    tenant_id,
    project_id,
    completed_by_user_id,
    commitment_task_count,
    commitment_estimated_seconds,
    commitment_unestimated_count,
    added_task_count,
    added_estimated_seconds,
    removed_task_count,
    removed_estimated_seconds,
    completed_task_count,
    completed_estimated_seconds,
    carryover_task_count,
    carryover_estimated_seconds,
    archived_task_count,
    unestimated_task_count,
    tasks
)
VALUES (
    sqlc.arg(sprint_id),
    sqlc.arg(tenant_id),
    sqlc.arg(project_id),
    sqlc.arg(completed_by_user_id),
    sqlc.arg(commitment_task_count),
    sqlc.arg(commitment_estimated_seconds),
    sqlc.arg(commitment_unestimated_count),
    sqlc.arg(added_task_count),
    sqlc.arg(added_estimated_seconds),
    sqlc.arg(removed_task_count),
    sqlc.arg(removed_estimated_seconds),
    sqlc.arg(completed_task_count),
    sqlc.arg(completed_estimated_seconds),
    sqlc.arg(carryover_task_count),
    sqlc.arg(carryover_estimated_seconds),
    sqlc.arg(archived_task_count),
    sqlc.arg(unestimated_task_count),
    sqlc.arg(tasks)::jsonb
)
RETURNING *;


-- name: GetSprintReport :one
SELECT *
FROM sprint_reports
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND sprint_id = sqlc.arg(sprint_id);
