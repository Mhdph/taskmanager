DROP INDEX tasks_project_timeline_idx;

ALTER TABLE tasks
DROP COLUMN start_at;
