-- name: CreateTeam :one
INSERT INTO teams (
    id,
    tenant_id,
    name,
    icon,
    color,
    description,
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


-- name: GetTeam :one
SELECT *
FROM teams
WHERE
    tenant_id = $1
    AND id = $2;


-- name: ListTeams :many
SELECT *
FROM teams
WHERE tenant_id = $1
ORDER BY
    name ASC,
    id ASC;


-- name: UpdateTeam :one
UPDATE teams
SET
    name = COALESCE(
        sqlc.narg(name)::text,
        name
    ),
    icon = COALESCE(
        sqlc.narg(icon)::text,
        icon
    ),
    color = COALESCE(
        sqlc.narg(color)::text,
        color
    ),
    description = COALESCE(
        sqlc.narg(description)::text,
        description
    ),
    updated_at = NOW()
WHERE
    tenant_id = $1
    AND id = $2
RETURNING *;


-- name: DeleteTeam :many
DELETE FROM teams
WHERE
    tenant_id = $1
    AND id = $2
RETURNING id;


-- name: ListTeamMemberIDs :many
SELECT user_id
FROM tenant_members
WHERE
    tenant_id = $1
    AND team_id = $2
ORDER BY user_id ASC;


-- name: SetTeamMembership :exec
UPDATE tenant_members
SET
    team_id = sqlc.arg(team_id)::uuid,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND user_id = ANY(sqlc.arg(user_ids)::text[]);


-- name: ClearTeamMembership :exec
UPDATE tenant_members
SET
    team_id = NULL,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)
    AND team_id = sqlc.arg(team_id)::uuid;


-- name: ListTenantMemberIDsIn :many
SELECT user_id
FROM tenant_members
WHERE
    tenant_id = $1
    AND user_id = ANY(sqlc.arg(user_ids)::text[])
ORDER BY user_id ASC;
