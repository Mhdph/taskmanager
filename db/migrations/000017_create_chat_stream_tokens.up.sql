-- One-time WebSocket stream tickets.
--
-- Browsers cannot set custom headers on a WebSocket handshake, so a client
-- first calls the authenticated REST API to mint a short-lived ticket and then
-- opens the socket with "?token=...". Only the SHA-256 hash is stored: a leaked
-- database row cannot be replayed. Consuming the ticket deletes it atomically,
-- so it works once across any API instance without shared in-memory state or a
-- signing secret.

CREATE TABLE chat_stream_tokens (
    token_hash BYTEA PRIMARY KEY
        CHECK (octet_length(token_hash) = 32),

    tenant_id TEXT NOT NULL,

    user_id TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL
        DEFAULT NOW(),

    CHECK (expires_at > created_at)
);

-- Expired, unused tickets are swept periodically.
CREATE INDEX chat_stream_tokens_expiry_idx
    ON chat_stream_tokens (expires_at);
