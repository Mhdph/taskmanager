CREATE TABLE task_reminders (
    id UUID PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id UUID NOT NULL,
    user_id TEXT NOT NULL,
    remind_at TIMESTAMPTZ,
    minutes_before_due INTEGER CHECK (minutes_before_due BETWEEN 0 AND 43200),
    note TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    status TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'sent', 'cancelled')),
    dispatch_id UUID NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    sent_for TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((remind_at IS NOT NULL) <> (minutes_before_due IS NOT NULL)),
    CHECK ((status = 'sent') = (sent_at IS NOT NULL)),
    CHECK ((status = 'sent') = (sent_for IS NOT NULL)),
    FOREIGN KEY (tenant_id, project_id, task_id)
        REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE
);

CREATE INDEX task_reminders_owner_idx ON task_reminders (tenant_id, project_id, task_id, user_id, created_at DESC, id DESC);
CREATE INDEX task_reminders_scheduled_idx ON task_reminders (next_attempt_at, id) WHERE status = 'scheduled';
