CREATE TABLE task_watchers (
    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    task_id UUID NOT NULL,

    user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (
        task_id,
        user_id
    ),

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

CREATE INDEX task_watchers_project_idx
    ON task_watchers (
        tenant_id,
        project_id
    );

CREATE INDEX task_watchers_user_idx
    ON task_watchers (
        tenant_id,
        user_id
    );
