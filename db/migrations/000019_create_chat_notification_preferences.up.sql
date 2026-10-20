-- Notification preferences and mention targets.
--
-- Each member can decide how chat notifications behave for one conversation:
--   all      : notify for every message (default)
--   mentions : notify only when the member is mentioned
--   mute     : never notify
--
-- Preferences are per conversation. Muting only silences push
-- notifications; the WebSocket stream still delivers every message.

CREATE TABLE conversation_notification_preferences (
    conversation_id UUID NOT NULL
        REFERENCES conversations (id)
        ON DELETE CASCADE,

    tenant_id TEXT NOT NULL,

    user_id TEXT NOT NULL,

    mode TEXT NOT NULL DEFAULT 'all'
        CHECK (mode IN ('all', 'mentions', 'mute')),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX conversation_notification_preferences_user_idx
    ON conversation_notification_preferences (tenant_id, user_id);


-- Mention targets are stored with the message so notification routing and
-- clients read exactly the same list. Mentions are resolved once, when the
-- message is written, against the conversation members at that moment.
ALTER TABLE messages
    ADD COLUMN mentioned_user_ids TEXT[] NOT NULL DEFAULT '{}';
