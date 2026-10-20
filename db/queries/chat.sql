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

-- name: ListChatConversationIDsForUser :many
SELECT conversation_id::text AS conversation_id
FROM conversation_members
WHERE
    tenant_id = $1
    AND user_id = $2
ORDER BY conversation_id;

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
ON CONFLICT (
    tenant_id,
    conversation_id,
    author_user_id,
    client_message_id
)
    WHERE kind = 'user' AND client_message_id IS NOT NULL
DO NOTHING
RETURNING *;


-- name: GetChatMessageByClientID :one
SELECT *
FROM messages
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND author_user_id = $3
    AND client_message_id = $4;


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


-- Forward replay after a known sequence. Unlike the history query this reads
-- oldest-first, so a reconnecting client can page forward from its last seen
-- sequence without gaps.
-- name: ListChatMessagesAfter :many
SELECT *
FROM messages
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND seq > sqlc.arg(after_seq)::bigint
ORDER BY seq ASC
LIMIT sqlc.arg(page_limit);


-- Serialize message inserts per conversation. The identity sequence is
-- assigned at INSERT time; without this lock a transaction holding a lower
-- seq can commit after a higher one, making "seq > lastSeen" replay skip it.
-- The lock is transaction-scoped and released on commit or rollback.
-- name: LockChatConversation :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(lock_key)::text, 0)
);


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


-- Attachments

-- name: CreateChatAttachment :one
INSERT INTO message_attachments (
    id,
    tenant_id,
    conversation_id,
    uploaded_by_user_id,
    filename,
    content_type,
    size_bytes,
    object_key
)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8
)
RETURNING *;


-- name: GetChatAttachment :one
SELECT *
FROM message_attachments
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND id = $3
    AND deleted_at IS NULL;


-- name: ListChatAttachmentsByMessages :many
SELECT *
FROM message_attachments
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND message_id = ANY(sqlc.arg(message_ids)::uuid[])
    AND deleted_at IS NULL
ORDER BY created_at ASC, id ASC;


-- name: AttachChatAttachmentsToMessage :execrows
UPDATE message_attachments
SET message_id = sqlc.arg(message_id)::uuid
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND uploaded_by_user_id = sqlc.arg(uploaded_by_user_id)::text
    AND message_id IS NULL
    AND deleted_at IS NULL
    AND id = ANY(sqlc.arg(attachment_ids)::uuid[]);


-- name: SoftDeleteChatMessageAttachments :many
UPDATE message_attachments
SET deleted_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND conversation_id = sqlc.arg(conversation_id)::uuid
    AND message_id = sqlc.arg(message_id)::uuid
    AND deleted_at IS NULL
RETURNING *;


-- name: DeletePendingChatAttachment :execrows
DELETE FROM message_attachments
WHERE
    tenant_id = $1
    AND conversation_id = $2
    AND id = $3
    AND uploaded_by_user_id = $4
    AND message_id IS NULL;


-- name: ListStalePendingChatAttachments :many
SELECT *
FROM message_attachments
WHERE
    message_id IS NULL
    AND deleted_at IS NULL
    AND created_at < sqlc.arg(created_before)::timestamptz
ORDER BY created_at ASC
LIMIT sqlc.arg(page_limit);


-- Stream tokens

-- name: CreateChatStreamToken :one
INSERT INTO chat_stream_tokens (
    token_hash,
    tenant_id,
    user_id,
    expires_at
)
VALUES (
    $1,
    $2,
    $3,
    NOW() + sqlc.arg(ttl_milliseconds)::int * INTERVAL '1 millisecond'
)
RETURNING expires_at;


-- name: ConsumeChatStreamToken :one
DELETE FROM chat_stream_tokens
WHERE
    token_hash = $1
    AND expires_at > NOW()
RETURNING
    tenant_id,
    user_id;


-- name: DeleteExpiredChatStreamTokens :execrows
DELETE FROM chat_stream_tokens
WHERE expires_at < NOW();


-- Presence

-- name: LockChatPresence :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg(lock_key)::text, 0)
);


-- name: UpsertChatPresenceLease :exec
INSERT INTO chat_presence_leases (
    connection_id,
    tenant_id,
    user_id,
    instance_id,
    expires_at
)
VALUES (
    sqlc.arg(connection_id)::uuid,
    sqlc.arg(tenant_id)::text,
    sqlc.arg(user_id)::text,
    sqlc.arg(instance_id)::text,
    NOW() + sqlc.arg(ttl_milliseconds)::bigint * INTERVAL '1 millisecond'
)
ON CONFLICT (connection_id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    user_id = EXCLUDED.user_id,
    instance_id = EXCLUDED.instance_id,
    expires_at = EXCLUDED.expires_at,
    updated_at = NOW();


-- Fast heartbeat path: only extends a lease that is still live. If this
-- updates zero rows the caller falls back to the full acquire transaction,
-- which can republish presence.online after a lease was swept.
-- name: RefreshChatPresenceLease :execrows
UPDATE chat_presence_leases
SET
    expires_at =
        NOW() + sqlc.arg(ttl_milliseconds)::bigint * INTERVAL '1 millisecond',
    updated_at = NOW()
WHERE
    connection_id = sqlc.arg(connection_id)::uuid
    AND instance_id = sqlc.arg(instance_id)::text
    AND expires_at > NOW();


-- name: DeleteChatPresenceLease :exec
DELETE FROM chat_presence_leases
WHERE connection_id = sqlc.arg(connection_id)::uuid;


-- name: DeleteExpiredChatPresenceLeasesForUser :exec
DELETE FROM chat_presence_leases
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND user_id = sqlc.arg(user_id)::text
    AND expires_at <= NOW();


-- name: CountLiveChatPresenceLeases :one
SELECT COUNT(*)
FROM chat_presence_leases
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND user_id = sqlc.arg(user_id)::text
    AND expires_at > NOW();


-- Insert-on-first-online, update-on-offline. The WHERE clause makes the
-- command return a row only when the state actually changed, so callers can
-- publish exactly one transition under the per-user advisory lock.
-- name: MarkChatPresenceOnline :execrows
INSERT INTO chat_presence (
    tenant_id,
    user_id,
    online,
    updated_at
)
VALUES (
    sqlc.arg(tenant_id)::text,
    sqlc.arg(user_id)::text,
    TRUE,
    NOW()
)
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
    online = TRUE,
    updated_at = NOW()
WHERE chat_presence.online = FALSE;


-- name: MarkChatPresenceOffline :execrows
UPDATE chat_presence
SET
    online = FALSE,
    updated_at = NOW()
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND user_id = sqlc.arg(user_id)::text
    AND online = TRUE;


-- name: IsChatUserOnline :one
SELECT EXISTS (
    SELECT 1
    FROM chat_presence_leases
    WHERE
        tenant_id = sqlc.arg(tenant_id)::text
        AND user_id = sqlc.arg(user_id)::text
        AND expires_at > NOW()
);


-- name: ListOnlineChatUserIDs :many
SELECT DISTINCT user_id
FROM chat_presence_leases
WHERE
    tenant_id = sqlc.arg(tenant_id)::text
    AND user_id = ANY(sqlc.arg(user_ids)::text[])
    AND expires_at > NOW()
ORDER BY user_id;


-- Sweepers claim users with expired leases in bounded batches, then delete
-- and reconcile each user under the same advisory lock as connect/release.
-- name: ListChatPresenceUsersWithExpiredLeases :many
SELECT DISTINCT tenant_id, user_id
FROM chat_presence_leases
WHERE expires_at <= NOW()
ORDER BY tenant_id, user_id
LIMIT sqlc.arg(page_limit);
