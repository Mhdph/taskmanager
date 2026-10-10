package notification

type EventType string

const (
	EventCommentMentioned  EventType = "comment.mentioned"
	EventTaskAssigned      EventType = "task.assigned"
	EventTaskStatusChanged EventType = "task.status_changed"
)

type SendInput struct {
	TenantID string `json:"tenantId"`

	RecipientUserID string `json:"recipientUserId"`

	Type EventType `json:"type"`

	ActorUserID *string `json:"actorUserId,omitempty"`

	ProjectID *string `json:"projectId,omitempty"`
	TaskID    *string `json:"taskId,omitempty"`
	CommentID *string `json:"commentId,omitempty"`

	Title string  `json:"title"`
	Body  *string `json:"body,omitempty"`

	Data map[string]any `json:"data,omitempty"`
}
