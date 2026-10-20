package storage

import (
	"fmt"
	"strings"
)

// Attachment object layout.
//
// Every attachment object lives under a tenant prefix:
//
//	tenants/{tenant}/chat/{conversation}/{attachment}
//	tenants/{tenant}/projects/{project}/tasks/{task}/{attachment}
//
// The layout lives here so the upload paths and the storage reconciler agree
// on which objects the application owns.
const (
	// AttachmentObjectPrefix is the prefix that holds every attachment
	// object.
	AttachmentObjectPrefix = "tenants/"
)

// ChatAttachmentObjectKey returns the object key of a chat attachment.
func ChatAttachmentObjectKey(
	tenantID string,
	conversationID string,
	attachmentID string,
) string {
	return fmt.Sprintf(
		"tenants/%s/chat/%s/%s",
		tenantID,
		conversationID,
		attachmentID,
	)
}

// TaskAttachmentObjectKey returns the object key of a task attachment.
func TaskAttachmentObjectKey(
	tenantID string,
	projectID string,
	taskID string,
	attachmentID string,
) string {
	return fmt.Sprintf(
		"tenants/%s/projects/%s/tasks/%s/%s",
		tenantID,
		projectID,
		taskID,
		attachmentID,
	)
}

// IsAttachmentObjectKey reports whether a key belongs to an attachment
// object. The reconciler only ever deletes keys that match, so objects
// stored under other prefixes are never touched.
func IsAttachmentObjectKey(key string) bool {
	parts := strings.Split(key, "/")

	switch {
	case len(parts) == 5 &&
		parts[0] == "tenants" &&
		parts[2] == "chat":

		return true

	case len(parts) == 7 &&
		parts[0] == "tenants" &&
		parts[2] == "projects" &&
		parts[4] == "tasks":

		return true

	default:
		return false
	}
}
