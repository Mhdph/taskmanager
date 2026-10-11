ALTER TABLE tasks
ADD COLUMN start_at TIMESTAMPTZ;

CREATE INDEX tasks_project_timeline_idx
ON tasks (
    tenant_id,
    project_id,
    start_at,
    due_at
)
WHERE
    archived_at IS NULL
    AND (
        start_at IS NOT NULL
        OR due_at IS NOT NULL
    );
