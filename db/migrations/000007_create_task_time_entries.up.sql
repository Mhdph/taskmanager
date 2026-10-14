CREATE TABLE task_time_entries (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id UUID NOT NULL,

    user_id TEXT NOT NULL,

    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,

    duration_seconds BIGINT NOT NULL DEFAULT 0
        CHECK (duration_seconds >= 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    CHECK (ended_at IS NULL OR ended_at >= started_at),

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE,

    FOREIGN KEY (
        task_id
    )
    REFERENCES tasks (
        id
    )
    ON DELETE CASCADE
);

CREATE INDEX task_time_entries_project_completed_idx
    ON task_time_entries (
        tenant_id,
        project_id
    )
    WHERE deleted_at IS NULL AND ended_at IS NOT NULL;
