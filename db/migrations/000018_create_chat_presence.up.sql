-- Shared WebSocket presence.
--
-- Every API instance writes one lease row per live WebSocket connection.
-- Presence is "online" while at least one lease for the user has not
-- expired. Heartbeats extend a lease; a crashed instance stops refreshing
-- its rows and the sweepers on surviving instances turn the user offline.
--
-- The state row records the last published transition. A per-user advisory
-- lock serializes connect/release/sweep decisions, so multiple instances can
-- race safely without publishing duplicate online/offline events.

CREATE TABLE chat_presence_leases (
    connection_id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    user_id TEXT NOT NULL,

    -- Identifies the API instance for operational visibility and debugging.
    instance_id TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX chat_presence_leases_user_idx
    ON chat_presence_leases (tenant_id, user_id, expires_at);

CREATE INDEX chat_presence_leases_expiry_idx
    ON chat_presence_leases (expires_at);

CREATE INDEX chat_presence_leases_instance_idx
    ON chat_presence_leases (instance_id, expires_at);


CREATE TABLE chat_presence (
    tenant_id TEXT NOT NULL,

    user_id TEXT NOT NULL,

    online BOOLEAN NOT NULL DEFAULT FALSE,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, user_id)
);

-- Used by operational queries; the hot path checks the lease index instead.
CREATE INDEX chat_presence_online_idx
    ON chat_presence (tenant_id)
    WHERE online;
