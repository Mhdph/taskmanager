DROP INDEX IF EXISTS messages_client_message_idx;

CREATE UNIQUE INDEX messages_client_message_idx
    ON messages (tenant_id, author_user_id, client_message_id)
    WHERE kind = 'user' AND client_message_id IS NOT NULL;
