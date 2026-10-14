-- Refuse to install constraints over inconsistent historical references. This
-- preserves all rows and makes any required repair explicit to operators.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM project_members pm
        JOIN projects p ON p.id = pm.project_id
        WHERE p.tenant_id <> pm.tenant_id
    ) THEN
        RAISE EXCEPTION 'project member tenant mismatches prevent hardening migration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM tasks child
        JOIN tasks parent ON parent.id = child.parent_task_id
        WHERE child.parent_task_id IS NOT NULL
          AND (child.tenant_id <> parent.tenant_id OR child.project_id <> parent.project_id)
    ) THEN
        RAISE EXCEPTION 'cross-tenant or cross-project task parent references prevent hardening migration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM task_dependencies d
        LEFT JOIN tasks source ON source.id = d.task_id
        LEFT JOIN tasks target ON target.id = d.depends_on_task_id
        WHERE source.id IS NULL OR target.id IS NULL
           OR source.tenant_id <> d.tenant_id OR source.project_id <> d.project_id
           OR target.tenant_id <> d.tenant_id OR target.project_id <> d.project_id
    ) THEN
        RAISE EXCEPTION 'cross-tenant or cross-project task dependencies prevent hardening migration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM task_comments c
        LEFT JOIN tasks t ON t.id = c.task_id
        WHERE t.id IS NULL OR t.tenant_id <> c.tenant_id OR t.project_id <> c.project_id
    ) OR EXISTS (
        SELECT 1 FROM task_watchers w
        LEFT JOIN tasks t ON t.id = w.task_id
        WHERE t.id IS NULL OR t.tenant_id <> w.tenant_id OR t.project_id <> w.project_id
    ) OR EXISTS (
        SELECT 1 FROM task_attachments a
        LEFT JOIN tasks t ON t.id = a.task_id
        WHERE t.id IS NULL OR t.tenant_id <> a.tenant_id OR t.project_id <> a.project_id
    ) OR EXISTS (
        SELECT 1 FROM task_time_entries e
        LEFT JOIN tasks t ON t.id = e.task_id
        WHERE t.id IS NULL OR t.tenant_id <> e.tenant_id OR t.project_id <> e.project_id
    ) THEN
        RAISE EXCEPTION 'cross-tenant or cross-project task child references prevent hardening migration';
    END IF;
    IF EXISTS (
        SELECT 1 FROM task_time_entries
        WHERE ended_at IS NULL AND deleted_at IS NULL
        GROUP BY tenant_id, user_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'multiple active time entries per tenant user prevent hardening migration';
    END IF;
END $$;

ALTER TABLE tasks ADD CONSTRAINT tasks_tenant_project_id_key
    UNIQUE (tenant_id, project_id, id);

ALTER TABLE project_statuses DROP CONSTRAINT project_statuses_project_id_position_key;
ALTER TABLE project_statuses ADD CONSTRAINT project_statuses_project_id_position_key
    UNIQUE (project_id, position) DEFERRABLE INITIALLY IMMEDIATE;

ALTER TABLE project_members DROP CONSTRAINT project_members_project_id_fkey;
ALTER TABLE project_members ADD CONSTRAINT project_members_tenant_project_fkey
    FOREIGN KEY (tenant_id, project_id) REFERENCES projects (tenant_id, id) ON DELETE CASCADE;

ALTER TABLE tasks DROP CONSTRAINT tasks_parent_task_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_parent_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, parent_task_id)
    REFERENCES tasks (tenant_id, project_id, id)
    ON DELETE SET NULL (parent_task_id);

ALTER TABLE task_dependencies DROP CONSTRAINT task_dependencies_task_id_fkey;
ALTER TABLE task_dependencies DROP CONSTRAINT task_dependencies_depends_on_task_id_fkey;
ALTER TABLE task_dependencies ADD CONSTRAINT task_dependencies_task_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;
ALTER TABLE task_dependencies ADD CONSTRAINT task_dependencies_target_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, depends_on_task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;

ALTER TABLE task_comments DROP CONSTRAINT task_comments_task_id_fkey;
ALTER TABLE task_comments ADD CONSTRAINT task_comments_task_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;

ALTER TABLE task_watchers DROP CONSTRAINT task_watchers_task_id_fkey;
ALTER TABLE task_watchers ADD CONSTRAINT task_watchers_task_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;

ALTER TABLE task_attachments DROP CONSTRAINT task_attachments_task_id_fkey;
ALTER TABLE task_attachments ADD CONSTRAINT task_attachments_task_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;

ALTER TABLE task_time_entries DROP CONSTRAINT task_time_entries_task_id_fkey;
ALTER TABLE task_time_entries ADD CONSTRAINT task_time_entries_task_same_project_fkey
    FOREIGN KEY (tenant_id, project_id, task_id)
    REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
CREATE INDEX projects_name_trgm_idx ON projects USING GIN (name gin_trgm_ops);
CREATE INDEX projects_description_trgm_idx ON projects USING GIN (description gin_trgm_ops);
CREATE INDEX tasks_title_trgm_idx ON tasks USING GIN (title gin_trgm_ops);
CREATE INDEX tasks_description_trgm_idx ON tasks USING GIN (description gin_trgm_ops);
CREATE INDEX tasks_my_work_created_idx
    ON tasks (tenant_id, assignee_user_id, created_at DESC, id DESC)
    WHERE archived_at IS NULL AND assignee_user_id IS NOT NULL;
CREATE UNIQUE INDEX task_time_entries_one_active_per_user_idx
    ON task_time_entries (tenant_id, user_id)
    WHERE ended_at IS NULL AND deleted_at IS NULL;

-- Audit rows intentionally remain append-only and independent of entity FKs.
