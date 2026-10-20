-- Project sprints.
--
-- A sprint is a planning container inside one project. It has an explicit
-- lifecycle: planned -> active -> completed. Starting and completing are
-- explicit actions; the planned end date never closes a sprint by itself.
--
-- A project has at most one active sprint (enforced by a partial unique
-- index) and any number of planned or completed sprints.
--
-- Tasks point at their sprint through tasks.sprint_id. The composite
-- foreign key carries tenant and project, so a task can never be assigned
-- to a sprint of another project.

CREATE TABLE sprints (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    name VARCHAR(120) NOT NULL
        CHECK (char_length(btrim(name)) > 0),

    goal TEXT,

    planned_start_at TIMESTAMPTZ,

    planned_end_at TIMESTAMPTZ NOT NULL,

    state TEXT NOT NULL DEFAULT 'planned'
        CHECK (state IN ('planned', 'active', 'completed')),

    created_by_user_id TEXT NOT NULL,

    started_at TIMESTAMPTZ,

    completed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- The planned window must be a real range.
    CHECK (
        planned_start_at IS NULL
        OR planned_end_at > planned_start_at
    ),

    -- A sprint is completed through an explicit action, so the completion
    -- timestamp only exists in the completed state.
    CHECK (
        (state = 'completed') = (completed_at IS NOT NULL)
    ),

    UNIQUE (tenant_id, id),

    -- The task assignment foreign key is composite, so the referenced
    -- columns need their own unique key.
    UNIQUE (tenant_id, project_id, id),

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE
);

-- One active sprint per project.
CREATE UNIQUE INDEX sprints_single_active_idx
    ON sprints (tenant_id, project_id)
    WHERE state = 'active';

CREATE INDEX sprints_project_idx
    ON sprints (tenant_id, project_id, created_at DESC, id DESC);


-- Task assignment.
--
-- The column is nullable: a task without a sprint is backlog work. The
-- composite foreign key prevents cross-project assignment.
ALTER TABLE tasks
    ADD COLUMN sprint_id UUID;

ALTER TABLE tasks
    ADD CONSTRAINT tasks_sprint_same_project_fkey
    FOREIGN KEY (
        tenant_id,
        project_id,
        sprint_id
    )
    REFERENCES sprints (
        tenant_id,
        project_id,
        id
    )
    ON DELETE NO ACTION;

CREATE INDEX tasks_sprint_idx
    ON tasks (tenant_id, project_id, sprint_id)
    WHERE sprint_id IS NOT NULL;


-- Membership history.
--
-- Every assignment change is recorded with the actor and the time, so the
-- sprint history does not depend on the best-effort audit feed.
CREATE TABLE sprint_task_membership (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    sprint_id UUID NOT NULL
        REFERENCES sprints (id)
        ON DELETE CASCADE,

    task_id UUID NOT NULL
        REFERENCES tasks (id)
        ON DELETE CASCADE,

    actor_user_id TEXT NOT NULL,

    action TEXT NOT NULL
        CHECK (action IN ('added', 'removed')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE
);

CREATE INDEX sprint_task_membership_sprint_idx
    ON sprint_task_membership (tenant_id, sprint_id, created_at DESC);

CREATE INDEX sprint_task_membership_task_idx
    ON sprint_task_membership (tenant_id, task_id, created_at DESC);


-- Commitment snapshot.
--
-- Starting a sprint freezes the tasks it committed to, with their titles
-- and estimates. Later edits cannot rewrite that commitment.
CREATE TABLE sprint_commitments (
    sprint_id UUID NOT NULL
        REFERENCES sprints (id)
        ON DELETE CASCADE,

    tenant_id TEXT NOT NULL,

    task_id UUID NOT NULL
        REFERENCES tasks (id)
        ON DELETE CASCADE,

    title VARCHAR(300) NOT NULL,

    estimate_seconds BIGINT,

    archived BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (sprint_id, task_id)
);


-- Completion report.
--
-- The report is written once, when the sprint completes, and never
-- recomputed: task edits, status changes and later sprint participation
-- cannot change a finished sprint's numbers.
CREATE TABLE sprint_reports (
    sprint_id UUID PRIMARY KEY
        REFERENCES sprints (id)
        ON DELETE CASCADE,

    tenant_id TEXT NOT NULL,

    project_id UUID NOT NULL,

    completed_by_user_id TEXT NOT NULL,

    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    commitment_task_count INTEGER NOT NULL DEFAULT 0,

    commitment_estimated_seconds BIGINT NOT NULL DEFAULT 0,

    commitment_unestimated_count INTEGER NOT NULL DEFAULT 0,

    added_task_count INTEGER NOT NULL DEFAULT 0,

    added_estimated_seconds BIGINT NOT NULL DEFAULT 0,

    removed_task_count INTEGER NOT NULL DEFAULT 0,

    removed_estimated_seconds BIGINT NOT NULL DEFAULT 0,

    completed_task_count INTEGER NOT NULL DEFAULT 0,

    completed_estimated_seconds BIGINT NOT NULL DEFAULT 0,

    carryover_task_count INTEGER NOT NULL DEFAULT 0,

    carryover_estimated_seconds BIGINT NOT NULL DEFAULT 0,

    archived_task_count INTEGER NOT NULL DEFAULT 0,

    unestimated_task_count INTEGER NOT NULL DEFAULT 0,

    -- Frozen per-task snapshot: title, estimate, archived flag, completion
    -- outcome and carryover destination.
    tasks JSONB NOT NULL DEFAULT '[]'::jsonb,

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE
);
