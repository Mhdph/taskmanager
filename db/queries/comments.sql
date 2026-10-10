-- name: CreateTaskComment :one
INSERT INTO task_comments (
    id,
    tenant_id,
    project_id,
    task_id,
    author_user_id,
    body
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
RETURNING *;

-- name: AddTaskCommentMention :exec
INSERT INTO task_comment_mentions (
    comment_id,
    mentioned_user_id
)
VALUES (
    $1,
    $2
)
ON CONFLICT DO NOTHING;


-- name: ListTaskComments :many
SELECT *
FROM task_comments
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND project_id = sqlc.arg(project_id)
    AND task_id = sqlc.arg(task_id)
    AND deleted_at IS NULL

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



-- name: ListTaskCommentMentions :many
SELECT mentioned_user_id
FROM task_comment_mentions
WHERE comment_id = $1
ORDER BY created_at ASC;


-- name: DeleteTaskComment :execrows
UPDATE task_comments
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND id = $4
    AND deleted_at IS NULL;


-- name: GetTaskComment :one
SELECT *
FROM task_comments
WHERE
    tenant_id = $1
    AND project_id = $2
    AND task_id = $3
    AND id = $4
    AND deleted_at IS NULL;
