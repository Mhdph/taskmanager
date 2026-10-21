-- name: CreateAutomationRule :one
INSERT INTO automation_rules (
    id,
    tenant_id,
    project_id,
    name,
    trigger_type,
    trigger_config,
    actions,
    enabled,
    created_by_user_id
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


-- name: GetAutomationRule :one
SELECT *
FROM automation_rules
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;


-- name: ListAutomationRulesForProject :many
SELECT *
FROM automation_rules
WHERE
    tenant_id = $1
    AND project_id = $2
ORDER BY
    created_at ASC,
    id ASC;


-- name: ListEnabledAutomationRulesForProjectAndTrigger :many
SELECT *
FROM automation_rules
WHERE
    tenant_id = $1
    AND project_id = $2
    AND trigger_type = $3
    AND enabled
ORDER BY
    created_at ASC,
    id ASC;


-- name: UpdateAutomationRule :one
UPDATE automation_rules
SET
    name = COALESCE(
        sqlc.narg(name)::varchar,
        name
    ),
    trigger_config = COALESCE(
        sqlc.narg(trigger_config)::jsonb,
        trigger_config
    ),
    actions = COALESCE(
        sqlc.narg(actions)::jsonb,
        actions
    ),
    enabled = COALESCE(
        sqlc.narg(enabled)::boolean,
        enabled
    ),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3
RETURNING *;


-- name: DeleteAutomationRule :execrows
DELETE FROM automation_rules
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;


-- name: CountAutomationRulesForProject :one
SELECT COUNT(*)
FROM automation_rules
WHERE
    tenant_id = $1
    AND project_id = $2;


-- name: ListEnabledDueAutomationRules :many
SELECT *
FROM automation_rules
WHERE
    trigger_type = 'task_due'
    AND enabled
ORDER BY
    created_at ASC,
    id ASC;


-- name: ListTasksDueSoon :many
SELECT
    id,
    title,
    assignee_user_id,
    created_by_user_id,
    due_at
FROM tasks
WHERE
    tenant_id = $1
    AND project_id = $2
    AND archived_at IS NULL
    AND completed_at IS NULL
    AND due_at IS NOT NULL
    AND due_at >= NOW()
    AND due_at <= $3
ORDER BY
    due_at ASC,
    id ASC
LIMIT 1000;


-- name: ClaimAutomationDueDelivery :execrows
INSERT INTO automation_due_deliveries (
    rule_id,
    task_id,
    due_at
)
VALUES (
    $1,
    $2,
    $3
)
ON CONFLICT DO NOTHING;
