CREATE TABLE task_attachments (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    task_id UUID NOT NULL,

    uploaded_by_user_id TEXT NOT NULL,

    filename VARCHAR(500) NOT NULL,

    content_type VARCHAR(255) NOT NULL,

    size_bytes BIGINT NOT NULL
        CHECK (size_bytes >= 0),

    object_key TEXT NOT NULL UNIQUE,

    created_at TIMESTAMPTZ NOT NULL
        DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

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

CREATE INDEX task_attachments_task_idx
    ON task_attachments (
        tenant_id,
        project_id,
        task_id,
        created_at DESC
    )
    WHERE deleted_at IS NULL;
