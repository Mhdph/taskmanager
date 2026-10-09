ALTER TABLE projects
ADD CONSTRAINT projects_tenant_id_id_key
UNIQUE (
    tenant_id,
    id
);


ALTER TABLE project_statuses
ADD CONSTRAINT project_statuses_tenant_project_id_id_key
UNIQUE (
    tenant_id,
    project_id,
    id
);


CREATE TABLE tasks (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    status_id UUID NOT NULL,

    parent_task_id UUID,

    title VARCHAR(300) NOT NULL,

    description TEXT,

    priority VARCHAR(32) NOT NULL DEFAULT 'medium'
        CHECK (
            priority IN (
                'low',
                'medium',
                'high',
                'urgent'
            )
        ),

    assignee_user_id TEXT,

    created_by_user_id TEXT NOT NULL,

    due_at TIMESTAMPTZ,

    completed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    archived_at TIMESTAMPTZ,

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
        tenant_id,
        project_id,
        status_id
    )
    REFERENCES project_statuses (
        tenant_id,
        project_id,
        id
    )
    ON DELETE RESTRICT,

    FOREIGN KEY (
        parent_task_id
    )
    REFERENCES tasks (
        id
    )
    ON DELETE SET NULL
);


CREATE INDEX tasks_tenant_project_idx
    ON tasks (
        tenant_id,
        project_id
    );


CREATE INDEX tasks_project_status_idx
    ON tasks (
        tenant_id,
        project_id,
        status_id
    );


CREATE INDEX tasks_tenant_assignee_idx
    ON tasks (
        tenant_id,
        assignee_user_id
    )
    WHERE assignee_user_id IS NOT NULL;


CREATE INDEX tasks_parent_task_idx
    ON tasks (
        parent_task_id
    )
    WHERE parent_task_id IS NOT NULL;


CREATE INDEX tasks_project_created_idx
    ON tasks (
        tenant_id,
        project_id,
        created_at DESC,
        id DESC
    );


CREATE TABLE task_dependencies (
    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    task_id UUID NOT NULL,

    depends_on_task_id UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (
        task_id,
        depends_on_task_id
    ),

    CHECK (
        task_id <> depends_on_task_id
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
    ON DELETE CASCADE,

    FOREIGN KEY (
        depends_on_task_id
    )
    REFERENCES tasks (
        id
    )
    ON DELETE CASCADE
);


CREATE INDEX task_dependencies_project_idx
    ON task_dependencies (
        tenant_id,
        project_id
    );


CREATE INDEX task_dependencies_depends_on_idx
    ON task_dependencies (
        depends_on_task_id
    );
