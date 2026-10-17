-- Teams: workspace-level groups. Members belong to one primary team
-- (tenant_members.team_id); projects may be owned by a team
-- (projects.team_id).

CREATE TABLE teams (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    name TEXT NOT NULL,

    icon  TEXT NOT NULL DEFAULT 'users',
    color TEXT NOT NULL DEFAULT '',

    description TEXT NOT NULL DEFAULT '',

    created_by_user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (tenant_id, name)
);

CREATE INDEX teams_tenant_idx
    ON teams (tenant_id);

ALTER TABLE tenant_members
    ADD COLUMN team_id UUID REFERENCES teams (id) ON DELETE SET NULL;

CREATE INDEX tenant_members_team_idx
    ON tenant_members (tenant_id, team_id);

ALTER TABLE projects
    ADD COLUMN team_id UUID REFERENCES teams (id) ON DELETE SET NULL;

CREATE INDEX projects_team_idx
    ON projects (tenant_id, team_id);
