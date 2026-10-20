-- Scope message idempotency to the conversation.
--
-- The original index was unique per (tenant, author, client_message_id),
-- which meant a client reusing an id in another conversation received the
-- other conversation's message instead of creating a new one. Client
-- message ids are only meaningful within the conversation they were
-- generated for.

DROP INDEX IF EXISTS messages_client_message_idx;

CREATE UNIQUE INDEX messages_client_message_idx
    ON messages (
        tenant_id,
        conversation_id,
        author_user_id,
        client_message_id
    )
    WHERE kind = 'user' AND client_message_id IS NOT NULL;
