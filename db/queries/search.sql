-- name: SearchProjects :many
SELECT
    p.id,
    p.name,
    p.description
FROM projects p
WHERE
    p.tenant_id = sqlc.arg(tenant_id)
    AND p.archived_at IS NULL

    AND (
        sqlc.arg(is_admin)::boolean = true

        OR EXISTS (
            SELECT 1
            FROM project_members pm
            WHERE
                pm.tenant_id = p.tenant_id
                AND pm.project_id = p.id
                AND pm.user_id = sqlc.arg(user_id)
        )
    )

    AND (
        p.name ILIKE '%' || sqlc.arg(search_query)::text || '%'
        OR COALESCE(
            p.description,
            ''
        ) ILIKE '%' || sqlc.arg(search_query)::text || '%'
    )

ORDER BY
    p.updated_at DESC,
    p.id DESC

LIMIT sqlc.arg(page_limit);
-- name: SearchTasks :many
SELECT
    t.id,
    t.project_id,
    t.title,
    t.description
FROM tasks t
JOIN projects p
    ON p.tenant_id = t.tenant_id
    AND p.id = t.project_id

WHERE
    t.tenant_id = sqlc.arg(tenant_id)

    AND t.archived_at IS NULL
    AND p.archived_at IS NULL

    AND (
        sqlc.arg(is_admin)::boolean = true

        OR EXISTS (
            SELECT 1
            FROM project_members pm
            WHERE
                pm.tenant_id = t.tenant_id
                AND pm.project_id = t.project_id
                AND pm.user_id = sqlc.arg(user_id)
        )
    )

    AND (
        t.title ILIKE '%' || sqlc.arg(search_query)::text || '%'
        OR COALESCE(
            t.description,
            ''
        ) ILIKE '%' || sqlc.arg(search_query)::text || '%'
    )

ORDER BY
    t.updated_at DESC,
    t.id DESC

LIMIT sqlc.arg(page_limit);


-- name: SearchMembers :many
SELECT
    user_id,
    email,
    first_name,
    last_name,
    title
FROM tenant_members
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND status = 'active'

    AND (
        (
            COALESCE(first_name, '')
            || ' '
            || COALESCE(last_name, '')
        ) ILIKE '%' || sqlc.arg(search_query)::text || '%'
        OR email ILIKE '%' || sqlc.arg(search_query)::text || '%'
        OR title ILIKE '%' || sqlc.arg(search_query)::text || '%'
    )

ORDER BY
    first_name ASC,
    last_name ASC,
    user_id ASC

LIMIT sqlc.arg(page_limit);


-- name: SearchComments :many
SELECT
    c.id,
    c.project_id,
    c.task_id,
    c.body
FROM task_comments c
JOIN projects p
    ON p.tenant_id = c.tenant_id
    AND p.id = c.project_id
WHERE
    c.tenant_id = sqlc.arg(tenant_id)

    AND c.deleted_at IS NULL
    AND p.archived_at IS NULL

    AND (
        sqlc.arg(is_admin)::boolean = true

        OR EXISTS (
            SELECT 1
            FROM project_members pm
            WHERE
                pm.tenant_id = c.tenant_id
                AND pm.project_id = c.project_id
                AND pm.user_id = sqlc.arg(user_id)
        )
    )

    AND c.body ILIKE '%' || sqlc.arg(search_query)::text || '%'

ORDER BY
    c.updated_at DESC,
    c.id DESC

LIMIT sqlc.arg(page_limit);


-- name: SearchAttachments :many
SELECT
    a.id,
    a.project_id,
    a.task_id,
    a.filename,
    a.content_type,
    a.size_bytes
FROM task_attachments a
JOIN projects p
    ON p.tenant_id = a.tenant_id
    AND p.id = a.project_id
WHERE
    a.tenant_id = sqlc.arg(tenant_id)

    AND a.deleted_at IS NULL
    AND p.archived_at IS NULL

    AND (
        sqlc.arg(is_admin)::boolean = true

        OR EXISTS (
            SELECT 1
            FROM project_members pm
            WHERE
                pm.tenant_id = a.tenant_id
                AND pm.project_id = a.project_id
                AND pm.user_id = sqlc.arg(user_id)
        )
    )

    AND a.filename ILIKE '%' || sqlc.arg(search_query)::text || '%'

ORDER BY
    a.created_at DESC,
    a.id DESC

LIMIT sqlc.arg(page_limit);
