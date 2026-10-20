-- Storage reconciliation.
--
-- An attachment object is live while a non-deleted attachment row still
-- references its key. Soft-deleted rows no longer reference the object: the
-- download endpoints filter them out, so the object is garbage.

-- name: ListLiveAttachmentObjectKeys :many
SELECT object_key
FROM task_attachments
WHERE
    object_key = ANY(sqlc.arg(object_keys)::text[])
    AND deleted_at IS NULL
UNION
SELECT object_key
FROM message_attachments
WHERE
    object_key = ANY(sqlc.arg(object_keys)::text[])
    AND deleted_at IS NULL;
