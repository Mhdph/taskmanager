-- Workspace member directory.
--
-- Auth is delegated to the hosted platform, so this table is the only place
-- the API keeps its own record of the users belonging to a tenant. It is
-- synced (upserted) every time an identity is resolved from hosted and not
-- found in the identity cache.
--
-- The "role" column is the workspace-level role shown in the members UI
-- (owner / admin / member / guest). It is independent of the hosted role,
-- which stays the source of truth for authentication.

CREATE TABLE tenant_members (
    tenant_id  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    last_name  TEXT NOT NULL DEFAULT '',
    role       TEXT NOT NULL DEFAULT 'member'
               CHECK (role IN ('owner', 'admin', 'member', 'guest')),
    title      TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active'
               CHECK (status IN ('active', 'invited')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, user_id)
);

CREATE INDEX tenant_members_role_idx
    ON tenant_members (tenant_id, role);

CREATE INDEX tenant_members_search_idx
    ON tenant_members USING gin (
        (
            email
            || ' '
            || first_name
            || ' '
            || last_name
        ) gin_trgm_ops
    );
