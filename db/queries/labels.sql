-- name: CreateLabel :one
INSERT INTO labels (
    id,
    tenant_id,
    name,
    color
)
VALUES (
    $1,
    $2,
    $3,
    $4
)
RETURNING *;


-- name: GetLabel :one
SELECT *
FROM labels
WHERE
    tenant_id = $1
    AND id = $2;


-- name: ListLabels :many
SELECT *
FROM labels
WHERE
    tenant_id = $1
ORDER BY name ASC, id ASC;


-- name: UpdateLabel :one
UPDATE labels
SET
    name = COALESCE(
        sqlc.narg(name)::text,
        name
    ),
    color = COALESCE(
        sqlc.narg(color)::text,
        color
    ),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND id = $2
RETURNING *;


-- name: DeleteLabel :execrows
DELETE FROM labels
WHERE
    tenant_id = $1
    AND id = $2;


-- name: ListTaskLabels :many
SELECT l.*
FROM task_labels tl
JOIN labels l
    ON l.tenant_id = tl.tenant_id
    AND l.id = tl.label_id
WHERE
    tl.tenant_id = $1
    AND tl.task_id = $2
ORDER BY l.name ASC, l.id ASC;


-- name: DeleteTaskLabels :exec
DELETE FROM task_labels
WHERE
    tenant_id = $1
    AND task_id = $2;


-- name: AddTaskLabel :execrows
INSERT INTO task_labels (
    tenant_id,
    project_id,
    task_id,
    label_id
)
VALUES (
    $1,
    $2,
    $3,
    $4
)
ON CONFLICT (task_id, label_id) DO NOTHING;


-- name: RemoveTaskLabel :execrows
DELETE FROM task_labels
WHERE
    tenant_id = $1
    AND task_id = $2
    AND label_id = $3;
