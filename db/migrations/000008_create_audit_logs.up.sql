CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    actor_user_id TEXT NOT NULL,

    action VARCHAR(128) NOT NULL,

    entity_type VARCHAR(64) NOT NULL,

    entity_id TEXT NOT NULL,

    project_id UUID,
    task_id UUID,

    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX audit_logs_tenant_created_idx
ON audit_logs (
    tenant_id,
    created_at DESC,
    id DESC
);

CREATE INDEX audit_logs_project_created_idx
ON audit_logs (
    tenant_id,
    project_id,
    created_at DESC
)
WHERE project_id IS NOT NULL;

CREATE INDEX audit_logs_task_created_idx
ON audit_logs (
    tenant_id,
    task_id,
    created_at DESC
)
WHERE task_id IS NOT NULL;

CREATE INDEX audit_logs_actor_created_idx
ON audit_logs (
    tenant_id,
    actor_user_id,
    created_at DESC
);
