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
