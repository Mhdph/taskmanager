package audit

import (
	"encoding/json"
	"time"
)

type Action string

const (
	ActionProjectCreated  Action = "project.created"
	ActionProjectUpdated  Action = "project.updated"
	ActionProjectArchived Action = "project.archived"

	ActionProjectMemberAdded       Action = "project.member.added"
	ActionProjectMemberRoleChanged Action = "project.member.role_changed"
	ActionProjectMemberRemoved     Action = "project.member.removed"

	ActionTaskCreated       Action = "task.created"
	ActionTaskUpdated       Action = "task.updated"
	ActionTaskArchived      Action = "task.archived"
	ActionTaskRestored      Action = "task.restored"
	ActionTaskAssigned      Action = "task.assigned"
	ActionTaskStatusChanged Action = "task.status_changed"

	ActionCommentCreated Action = "task.comment.created"
	ActionCommentDeleted Action = "task.comment.deleted"

	ActionAttachmentUploaded Action = "task.attachment.uploaded"
	ActionAttachmentDeleted  Action = "task.attachment.deleted"

	ActionDependencyAdded   Action = "task.dependency.added"
	ActionDependencyRemoved Action = "task.dependency.removed"

	ActionTaskWatcherAdded   Action = "task.watcher.added"
	ActionTaskWatcherRemoved Action = "task.watcher.removed"
)

type EntityType string

const (
	EntityProject    EntityType = "project"
	EntityTask       EntityType = "task"
	EntityComment    EntityType = "comment"
	EntityAttachment EntityType = "attachment"
	EntityMember     EntityType = "project_member"
	EntityDependency EntityType = "task_dependency"
)

type Entry struct {
	ID string

	TenantID string

	ActorUserID string

	Action Action

	EntityType EntityType
	EntityID   string

	ProjectID *string
	TaskID    *string

	Metadata json.RawMessage

	CreatedAt time.Time
}

type RecordInput struct {
	TenantID string

	ActorUserID string

	Action Action

	EntityType EntityType
	EntityID   string

	ProjectID *string
	TaskID    *string

	Metadata any
}
