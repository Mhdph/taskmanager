DROP TABLE IF EXISTS favorite_projects;
DROP TABLE IF EXISTS favorite_tasks;
DROP TABLE IF EXISTS task_labels;
DROP TABLE IF EXISTS labels;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS position,
    DROP COLUMN IF EXISTS estimate_seconds,
    DROP COLUMN IF EXISTS recurrence;
