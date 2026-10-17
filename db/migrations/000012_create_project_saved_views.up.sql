-- Per-project saved views: a named filter/layout combination that appears
-- as a tab on the project. Shared with everyone who can access the project.

CREATE TABLE project_saved_views (
    id UUID PRIMARY KEY,

    tenant_id  TEXT NOT NULL,
    project_id UUID NOT NULL,

    name TEXT NOT NULL,

    view_type TEXT NOT NULL
        CHECK (view_type IN ('list', 'board', 'table')),

    -- Raw filter array: [{f, op, v}]
    filters JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_by_user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, project_id, name),

    FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects (tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX project_saved_views_project_idx
    ON project_saved_views (tenant_id, project_id, created_at);
