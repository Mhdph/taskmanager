-- Chat message attachments.
--
-- Uploads are two-phase: a file is uploaded first and stored as a pending
-- row (message_id IS NULL), then the message send binds one or more pending
-- rows inside the same transaction that inserts the message. This lets
-- clients show upload progress before the message exists, and it keeps a
-- sent message and its attachments atomic.

CREATE TABLE message_attachments (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    conversation_id UUID NOT NULL
        REFERENCES conversations (id)
        ON DELETE CASCADE,

    -- NULL until the attachment is bound to a sent message.
    message_id UUID
        REFERENCES messages (id)
        ON DELETE CASCADE,

    uploaded_by_user_id TEXT NOT NULL,

    filename VARCHAR(500) NOT NULL,

    content_type VARCHAR(255) NOT NULL,

    size_bytes BIGINT NOT NULL
        CHECK (size_bytes > 0),

    object_key TEXT NOT NULL UNIQUE,

    created_at TIMESTAMPTZ NOT NULL
        DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);

CREATE INDEX message_attachments_message_idx
    ON message_attachments (message_id)
    WHERE message_id IS NOT NULL AND deleted_at IS NULL;

-- Pending uploads are looked up by conversation for binding and listing.
CREATE INDEX message_attachments_pending_idx
    ON message_attachments (
        tenant_id,
        conversation_id,
        uploaded_by_user_id,
        created_at
    )
    WHERE message_id IS NULL AND deleted_at IS NULL;

-- Orphan uploads that were never sent are swept on a schedule.
CREATE INDEX message_attachments_stale_idx
    ON message_attachments (created_at)
    WHERE message_id IS NULL AND deleted_at IS NULL;
