-- name: SyncTenantMember :exec
INSERT INTO tenant_members (
    tenant_id,
    user_id,
    email,
    first_name,
    last_name
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
ON CONFLICT (tenant_id, user_id) DO UPDATE
SET
    email = EXCLUDED.email,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    updated_at = NOW()
WHERE
    tenant_members.email <> EXCLUDED.email
    OR tenant_members.first_name <> EXCLUDED.first_name
    OR tenant_members.last_name <> EXCLUDED.last_name;


-- name: GetTenantMember :one
SELECT *
FROM tenant_members
WHERE
    tenant_id = $1
    AND user_id = $2;


-- name: ListTenantMembers :many
SELECT *
FROM tenant_members
WHERE
    tenant_id = $1
    AND (
        sqlc.narg(role)::text IS NULL
        OR role = sqlc.narg(role)::text
    )
    AND (
        sqlc.narg(status)::text IS NULL
        OR status = sqlc.narg(status)::text
    )
    AND (
        sqlc.narg(search)::text IS NULL
        OR (
            email ILIKE '%' || sqlc.narg(search)::text || '%'
            OR first_name ILIKE '%' || sqlc.narg(search)::text || '%'
            OR last_name ILIKE '%' || sqlc.narg(search)::text || '%'
        )
    )
ORDER BY created_at ASC, user_id ASC
LIMIT $2;


-- name: LockTenantForMembershipMutation :exec
SELECT pg_advisory_xact_lock(hashtext(sqlc.arg(tenant_id)::text)::bigint);


-- name: CountTenantOwners :one
SELECT COUNT(*)
FROM tenant_members
WHERE
    tenant_id = $1
    AND role = 'owner';


-- name: UpdateTenantMemberRole :execrows
UPDATE tenant_members
SET
    role = $3,
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND user_id = $2;


-- name: UpdateTenantMemberTitle :execrows
UPDATE tenant_members
SET
    title = $3,
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND user_id = $2;


-- name: DeleteTenantMember :execrows
DELETE FROM tenant_members
WHERE
    tenant_id = $1
    AND user_id = $2;
