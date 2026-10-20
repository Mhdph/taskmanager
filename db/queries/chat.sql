-- Conversations

-- name: CreateProjectConversation :one
INSERT INTO conversations (
    id,
    tenant_id,
    type,
    project_id,
    created_by_user_id
)
VALUES (
    $1,
    $2,
    'project',
    $3,
    $4
)
ON CONFLICT (tenant_id, project_id) WHERE type = 'project'
DO UPDATE SET updated_at = conversations.updated_at
RETURNING *;


-- name: CreateGroupConversation :one
INSERT INTO conversations (
    id,
    tenant_id,
    type,
    title,
    created_by_user_id
)
VALUES (
    $1,
    $2,
    'group',
    $3,
    $4
)
RETURNING *;


-- name: CreateDMConversation :one
INSERT INTO conversations (
    id,
    tenant_id,
    type,
    dm_key,
    created_by_user_id
)
VALUES (
    $1,
    $2,
    'dm',
    $3,
    $4
)
ON CONFLICT (tenant_id, dm_key) WHERE type = 'dm'
DO UPDATE SET tenant_id = conversations.tenant_id
RETURNING *;


-- name: GetConversation :one
SELECT *
FROM conversations
WHERE
    tenant_id = $1
    AND id = $2;


-- name: GetProjectConversation :one
SELECT *
FROM conversations
WHERE
    tenant_id = $1
    AND project_id = $2
    AND type = 'project';


-- name: ListConversationsForUser :many
SELECT
    c.id,
    c.tenant_id,
    c.type,
    c.project_id,
    c.title,
    c.created_by_user_id,
    c.last_message_seq,
    c.last_message_at,
    c.created_at,
    c.updated_at,

    COALESCE(cm.role, '')::text AS member_role,
    COALESCE(cm.last_read_seq, 0)::bigint AS last_read_seq,

    COALESCE((
        SELECT COUNT(*)
        FROM messages m
        WHERE
            m.conversation_id = c.id
            AND m.seq > COALESCE(cm.last_read_seq, 0)
            AND m.deleted_at IS NULL
            AND (
                m.author_user_id IS NULL
                OR m.author_user_id <> sqlc.arg(user_id)::text
            )
    ), 0)::bigint AS unread_count,

    COALESCE(lm.kind, '')::text AS last_message_kind,
    COALESCE(lm.body, '')::text AS last_message_body,
    lm.author_user_id AS last_message_author_user_id,
    lm.created_at AS last_message_created_at
FROM conversations c
LEFT JOIN conversation_members cm ON
    cm.conversation_id = c.id
    AND cm.tenant_id = c.tenant_id
    AND cm.user_id = sqlc.arg(user_id)::text
LEFT JOIN LATERAL (
    SELECT
        m.kind,
        m.body,
        m.author_user_id,
        m.created_at
    FROM messages m
    WHERE m.conversation_id = c.id
    ORDER BY m.seq DESC
    LIMIT 1
) lm ON TRUE
WHERE
    c.tenant_id = sqlc.arg(tenant_id)::text
    AND (
        cm.user_id IS NOT NULL
        OR (c.type = 'project' AND sqlc.arg(is_admin)::bool)
    )
ORDER BY
    c.last_message_at DESC NULLS LAST,
    c.created_at DESC
LIMIT sqlc.arg(page_limit);


-- Members

-- name: ListConversationMembers :many
SELECT *
FROM conversation_members
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = ANY(sqlc.arg(conversation_ids)::uuid[])
ORDER BY
    conversation_id ASC,
    user_id ASC;


-- name: GetConversationMember :one
SELECT *
FROM conversation_members
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND user_id = $3;


-- name: AddConversationMember :exec
INSERT INTO conversation_members (
    conversation_id,
    tenant_id,
    user_id,
    role,
    source
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    'manual'
)
ON CONFLICT (conversation_id, user_id) DO NOTHING;


-- name: RemoveConversationMember :execrows
DELETE FROM conversation_members
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND user_id = $3
    AND source = 'manual';


-- SyncProjectConversationMembers mirrors the current project members into
-- the conversation. Existing rows keep their read cursor; only the role is
-- refreshed.
-- name: SyncProjectConversationMembers :exec
INSERT INTO conversation_members (
    conversation_id,
    tenant_id,
    user_id,
    role,
    source
)
SELECT
    sqlc.arg(conversation_id)::uuid,
    sqlc.arg(tenant_id)::text,
    pm.user_id,
    CASE
        WHEN pm.role = 'manager' THEN 'owner'
        ELSE 'member'
    END,
    'project'
FROM project_members pm
WHERE
    pm.tenant_id = sqlc.arg(tenant_id)::text
    AND pm.project_id = sqlc.arg(project_id)::uuid
ON CONFLICT (conversation_id, user_id) DO UPDATE
SET
    role = EXCLUDED.role,
    tenant_id = EXCLUDED.tenant_id;


-- PruneProjectConversationMembers removes project-sourced members that are
-- no longer on the project. Manually added rows (workspace admins) survive.
-- name: PruneProjectConversationMembers :exec
DELETE FROM conversation_members cm
WHERE
    cm.tenant_id = sqlc.arg(tenant_id)::text
    AND cm.conversation_id = sqlc.arg(conversation_id)::uuid
    AND cm.source = 'project'
    AND NOT EXISTS (
        SELECT 1
        FROM project_members pm
        WHERE
            pm.tenant_id = sqlc.arg(tenant_id)::text
            AND pm.project_id = sqlc.arg(project_id)::uuid
            AND pm.user_id = cm.user_id
    );


-- name: ListChatTenantMemberIDsIn :many
SELECT user_id
FROM tenant_members
WHERE
    tenant_id = $1
    AND user_id = ANY(sqlc.arg(user_ids)::text[])
ORDER BY user_id ASC;


-- name: UpdateConversationReadCursor :execrows
UPDATE conversation_members
SET last_read_seq = GREATEST(
    last_read_seq,
    sqlc.arg(last_read_seq)::bigint
)
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND user_id = sqlc.arg(user_id)::text;


-- Messages

-- name: CreateChatMessage :one
INSERT INTO messages (
    id,
    tenant_id,
    conversation_id,
    kind,
    author_user_id,
    body,
    client_message_id
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
ON CONFLICT (tenant_id, author_user_id, client_message_id)
    WHERE kind = 'user' AND client_message_id IS NOT NULL
DO NOTHING
RETURNING *;


-- name: GetChatMessageByClientID :one
SELECT *
FROM messages
WHERE
    tenant_id = $1
    AND author_user_id = $2
    AND client_message_id = $3;


-- name: GetChatMessage :one
SELECT *
FROM messages
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND id = $3;


-- name: ListChatMessages :many
SELECT *
FROM messages
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND (
        sqlc.narg(before_seq)::bigint IS NULL
        OR seq < sqlc.narg(before_seq)::bigint
    )
ORDER BY seq DESC
LIMIT sqlc.arg(page_limit);


-- name: BumpConversationLastMessage :exec
UPDATE conversations
SET
    last_message_seq = sqlc.arg(last_message_seq)::bigint,
    last_message_at = sqlc.arg(last_message_at)::timestamptz,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND id = sqlc.arg(id)::uuid
    -- Concurrent inserts may commit out of sequence order; never let an
    -- older message overwrite a newer conversation tail.
    AND last_message_seq < sqlc.arg(last_message_seq)::bigint;


-- name: EditChatMessage :one
UPDATE messages
SET
    body = sqlc.arg(body)::text,
    edited_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND id = sqlc.arg(id)::uuid
    AND kind = 'user'
    AND deleted_at IS NULL
RETURNING *;


-- name: DeleteChatMessage :one
UPDATE messages
SET
    body = '',
    deleted_at = NOW(),
    edited_at = NULL
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND id = sqlc.arg(id)::uuid
    AND kind = 'user'
    AND deleted_at IS NULL
RETURNING *;
