-- name: CreateSavedView :one
INSERT INTO project_saved_views (
    id,
    tenant_id,
    project_id,
    name,
    view_type,
    filters,
    created_by_user_id
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
)
RETURNING *;


-- name: GetSavedView :one
SELECT *
FROM project_saved_views
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;


-- name: ListSavedViewsForProject :many
SELECT *
FROM project_saved_views
WHERE
    tenant_id = $1
    AND project_id = $2
ORDER BY
    created_at ASC,
    id ASC;


-- name: UpdateSavedView :one
UPDATE project_saved_views
SET
    name = COALESCE(
        sqlc.narg(name)::text,
        name
    ),
    view_type = COALESCE(
        sqlc.narg(view_type)::varchar,
        view_type
    ),
    filters = COALESCE(
        sqlc.narg(filters)::jsonb,
        filters
    ),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3
RETURNING *;


-- name: DeleteSavedView :execrows
DELETE FROM project_saved_views
WHERE
    tenant_id = $1
    AND project_id = $2
    AND id = $3;
