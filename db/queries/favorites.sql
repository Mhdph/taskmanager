-- name: AddFavoriteTask :exec
INSERT INTO favorite_tasks (
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
ON CONFLICT (user_id, task_id) DO NOTHING;


-- name: RemoveFavoriteTask :execrows
DELETE FROM favorite_tasks
WHERE
    tenant_id = $1
    AND user_id = $2
    AND task_id = $3;


-- name: ListFavoriteTaskIDs :many
SELECT task_id
FROM favorite_tasks
WHERE
    tenant_id = $1
    AND user_id = $2
ORDER BY created_at DESC, task_id ASC;


-- name: AddFavoriteProject :exec
INSERT INTO favorite_projects (
    tenant_id,
    project_id,
    user_id
)
VALUES (
    $1,
    $2,
    $3
)
ON CONFLICT (user_id, project_id) DO NOTHING;


-- name: RemoveFavoriteProject :execrows
DELETE FROM favorite_projects
WHERE
    tenant_id = $1
    AND user_id = $2
    AND project_id = $3;


-- name: ListFavoriteProjectIDs :many
SELECT project_id
FROM favorite_projects
WHERE
    tenant_id = $1
    AND user_id = $2
ORDER BY created_at DESC, project_id ASC;
