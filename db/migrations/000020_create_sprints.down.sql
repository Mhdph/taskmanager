DROP TABLE IF EXISTS sprint_reports;
DROP TABLE IF EXISTS sprint_commitments;
DROP TABLE IF EXISTS sprint_task_membership;

ALTER TABLE tasks
    DROP CONSTRAINT IF EXISTS tasks_sprint_same_project_fkey;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS sprint_id;

DROP TABLE IF EXISTS sprints;
