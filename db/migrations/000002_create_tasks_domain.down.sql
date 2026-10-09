DROP TABLE task_dependencies;
DROP TABLE tasks;

ALTER TABLE project_statuses
DROP CONSTRAINT project_statuses_tenant_project_id_id_key;

ALTER TABLE projects
DROP CONSTRAINT projects_tenant_id_id_key;
