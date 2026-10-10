CREATE TABLE task_comments (
    id UUID PRIMARY KEY,

    tenant_id TEXT NOT NULL,
    project_id UUID NOT NULL,
    task_id UUID NOT NULL,

    author_user_id TEXT NOT NULL,

    body TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    FOREIGN KEY (
        tenant_id,
        project_id
    )
    REFERENCES projects (
        tenant_id,
        id
    )
    ON DELETE CASCADE,

    FOREIGN KEY (
        task_id
    )
    REFERENCES tasks (
        id
    )
    ON DELETE CASCADE
);


CREATE INDEX task_comments_task_created_idx
    ON task_comments (
        tenant_id,
        project_id,
        task_id,
        created_at DESC,
        id DESC
    )
    WHERE deleted_at IS NULL;


CREATE TABLE task_comment_mentions (
    comment_id UUID NOT NULL
        REFERENCES task_comments(id)
        ON DELETE CASCADE,

    mentioned_user_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (
        comment_id,
        mentioned_user_id
    )
);


CREATE INDEX task_comment_mentions_user_idx
    ON task_comment_mentions (
        mentioned_user_id
    );
