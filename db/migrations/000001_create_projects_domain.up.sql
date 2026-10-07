CREATE TABLE projects (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    name VARCHAR(200) NOT NULL,
    description TEXT,

    created_by_user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE INDEX projects_tenant_id_idx
    ON projects (tenant_id);


CREATE TABLE project_members (
    project_id UUID NOT NULL
        REFERENCES projects(id)
        ON DELETE CASCADE,

    tenant_id TEXT NOT NULL,

    user_id TEXT NOT NULL,

    role VARCHAR(32) NOT NULL
        CHECK (
            role IN ('manager', 'member', 'viewer')
        ),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX project_members_tenant_user_idx
    ON project_members (tenant_id, user_id);


CREATE TABLE project_statuses (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL
        REFERENCES projects(id)
        ON DELETE CASCADE,

    name VARCHAR(80) NOT NULL,

    position INTEGER NOT NULL
        CHECK (position >= 0),

    category VARCHAR(32) NOT NULL
        CHECK (
            category IN ('todo', 'in_progress', 'done')
        ),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (project_id, name),
    UNIQUE (project_id, position)
);

CREATE INDEX project_statuses_tenant_project_idx
    ON project_statuses (tenant_id, project_id);
