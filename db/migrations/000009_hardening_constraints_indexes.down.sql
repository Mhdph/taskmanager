DROP INDEX IF EXISTS tasks_my_work_created_idx;
DROP INDEX IF EXISTS task_time_entries_one_active_per_user_idx;
DROP INDEX IF EXISTS tasks_description_trgm_idx;
DROP INDEX IF EXISTS tasks_title_trgm_idx;
DROP INDEX IF EXISTS projects_description_trgm_idx;
DROP INDEX IF EXISTS projects_name_trgm_idx;

ALTER TABLE project_members DROP CONSTRAINT project_members_tenant_project_fkey;
ALTER TABLE project_members ADD CONSTRAINT project_members_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE project_statuses DROP CONSTRAINT project_statuses_project_id_position_key;
ALTER TABLE project_statuses ADD CONSTRAINT project_statuses_project_id_position_key UNIQUE (project_id, position);

ALTER TABLE task_time_entries DROP CONSTRAINT task_time_entries_task_same_project_fkey;
ALTER TABLE task_time_entries ADD CONSTRAINT task_time_entries_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE task_attachments DROP CONSTRAINT task_attachments_task_same_project_fkey;
ALTER TABLE task_attachments ADD CONSTRAINT task_attachments_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE task_watchers DROP CONSTRAINT task_watchers_task_same_project_fkey;
ALTER TABLE task_watchers ADD CONSTRAINT task_watchers_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE task_comments DROP CONSTRAINT task_comments_task_same_project_fkey;
ALTER TABLE task_comments ADD CONSTRAINT task_comments_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE task_dependencies DROP CONSTRAINT task_dependencies_task_same_project_fkey;
ALTER TABLE task_dependencies ADD CONSTRAINT task_dependencies_task_id_fkey
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE task_dependencies DROP CONSTRAINT task_dependencies_target_same_project_fkey;
ALTER TABLE task_dependencies ADD CONSTRAINT task_dependencies_depends_on_task_id_fkey
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE;
ALTER TABLE tasks DROP CONSTRAINT tasks_parent_same_project_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_parent_task_id_fkey
    FOREIGN KEY (parent_task_id) REFERENCES tasks(id) ON DELETE SET NULL;
ALTER TABLE tasks DROP CONSTRAINT tasks_tenant_project_id_key;
