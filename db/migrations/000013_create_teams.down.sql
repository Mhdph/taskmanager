DROP INDEX IF EXISTS projects_team_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS tenant_members_team_idx;
ALTER TABLE tenant_members DROP COLUMN IF EXISTS team_id;

DROP TABLE IF EXISTS teams;
