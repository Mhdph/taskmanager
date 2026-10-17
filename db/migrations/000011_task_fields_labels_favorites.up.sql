-- Task-field extensions: ordering, estimate, recurrence, labels, favorites.

-- Ordering (drag & drop). Fractional positions are set by the reorder
-- endpoint; new tasks get the next slot in their status column.
ALTER TABLE tasks
    ADD COLUMN position DOUBLE PRECISION;

-- Work estimate in seconds.
ALTER TABLE tasks
    ADD COLUMN estimate_seconds BIGINT
    CHECK (estimate_seconds IS NULL OR estimate_seconds >= 0);

-- Recurrence rule.
ALTER TABLE tasks
    ADD COLUMN recurrence TEXT
    CHECK (
        recurrence IS NULL
        OR recurrence IN ('daily', 'weekly', 'biweekly', 'monthly')
    );

-- Workspace label directory. Labels belong to the tenant and can be
-- attached to tasks from any project.
CREATE TABLE labels (
    id         UUID PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE INDEX labels_tenant_idx
    ON labels (tenant_id);

-- Label attachments on tasks. Mirrors the composite tenant-scoped key
-- pattern introduced in migration 000009.
CREATE TABLE task_labels (
    tenant_id  TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id    UUID NOT NULL,
    label_id   UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (task_id, label_id),

    FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects (tenant_id, id)
        ON DELETE CASCADE,

    FOREIGN KEY (tenant_id, project_id, task_id)
        REFERENCES tasks (tenant_id, project_id, id)
        ON DELETE CASCADE,

    FOREIGN KEY (tenant_id, label_id)
        REFERENCES labels (tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX task_labels_label_idx
    ON task_labels (tenant_id, label_id);

CREATE INDEX task_labels_project_idx
    ON task_labels (tenant_id, project_id);

-- Per-user favorites. Two tables instead of one polymorphic table so both
-- keep real composite foreign keys.
CREATE TABLE favorite_tasks (
    tenant_id  TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id    UUID NOT NULL,
    user_id    TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, task_id),

    FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects (tenant_id, id)
        ON DELETE CASCADE,

    FOREIGN KEY (tenant_id, project_id, task_id)
        REFERENCES tasks (tenant_id, project_id, id)
        ON DELETE CASCADE
);

CREATE INDEX favorite_tasks_user_idx
    ON favorite_tasks (tenant_id, user_id);

CREATE TABLE favorite_projects (
    tenant_id  TEXT NOT NULL,
    project_id UUID NOT NULL,
    user_id    TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, project_id),

    FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects (tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX favorite_projects_user_idx
    ON favorite_projects (tenant_id, user_id);
