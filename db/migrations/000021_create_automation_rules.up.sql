-- Project automation rules.
--
-- A rule binds one trigger (a task event or a due-date window) to an ordered
-- list of actions. Rules are evaluated per project by the automation service;
-- the JSONB columns are validated by the service before they are stored.
--
-- The due-date trigger is driven by a background sweep. Each (rule, task,
-- due date) reminder is claimed once in automation_due_deliveries, so a
-- recurring sweep cannot notify twice for the same deadline. A changed due
-- date creates a new delivery row and therefore a new reminder.

CREATE TABLE automation_rules (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    name VARCHAR(120) NOT NULL
        CHECK (char_length(btrim(name)) > 0),

    trigger_type TEXT NOT NULL
        CHECK (
            trigger_type IN (
                'task_status_changed',
                'task_created',
                'task_assigned',
                'task_due'
            )
        ),

    trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(trigger_config) = 'object'),

    actions JSONB NOT NULL
        CHECK (
            jsonb_typeof(actions) = 'array'
            AND jsonb_array_length(actions) BETWEEN 1 AND 5
        ),

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    created_by_user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, project_id, id),

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE
);

CREATE INDEX automation_rules_project_idx
    ON automation_rules (
        tenant_id,
        project_id,
        created_at ASC,
        id ASC
    );

CREATE INDEX automation_rules_enabled_trigger_idx
    ON automation_rules (trigger_type)
    WHERE enabled;


CREATE TABLE automation_due_deliveries (
    rule_id UUID NOT NULL
        REFERENCES automation_rules (id)
        ON DELETE CASCADE,

    task_id UUID NOT NULL
        REFERENCES tasks (id)
        ON DELETE CASCADE,

    due_at TIMESTAMPTZ NOT NULL,

    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (rule_id, task_id, due_at)
);

CREATE INDEX automation_due_deliveries_task_idx
    ON automation_due_deliveries (task_id);
