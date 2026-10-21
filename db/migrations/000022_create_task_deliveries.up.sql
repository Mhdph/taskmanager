CREATE TABLE task_deliveries (
    id UUID PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    submitted_by_user_id TEXT NOT NULL,
    reviewer_user_id TEXT NOT NULL,
    body TEXT NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 10000),
    output_url TEXT CHECK (char_length(output_url) BETWEEN 1 AND 2048),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'changes_requested', 'withdrawn')),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_by_user_id TEXT,
    review_note TEXT CHECK (char_length(review_note) BETWEEN 1 AND 2000),
    reviewed_at TIMESTAMPTZ,
    FOREIGN KEY (tenant_id, project_id, task_id)
        REFERENCES tasks (tenant_id, project_id, id) ON DELETE CASCADE,
    UNIQUE (tenant_id, project_id, task_id, revision),
    CHECK (submitted_by_user_id <> reviewer_user_id),
    CHECK (
        (status = 'pending' AND reviewed_by_user_id IS NULL AND reviewed_at IS NULL AND review_note IS NULL)
        OR (status <> 'pending' AND reviewed_by_user_id IS NOT NULL AND reviewed_at IS NOT NULL)
    ),
    CHECK (status <> 'changes_requested' OR review_note IS NOT NULL),
    CHECK (status NOT IN ('approved', 'changes_requested') OR reviewed_by_user_id = reviewer_user_id)
);

CREATE UNIQUE INDEX task_deliveries_one_pending_idx
    ON task_deliveries (tenant_id, project_id, task_id) WHERE status = 'pending';
