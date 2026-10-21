package notification

type EventType string

const (
	EventCommentMentioned      EventType = "comment.mentioned"
	EventTaskAssigned          EventType = "task.assigned"
	EventTaskStatusChanged     EventType = "task.status_changed"
	EventTaskDeliverySubmitted EventType = "task.delivery_submitted"
	EventTaskDeliveryReviewed  EventType = "task.delivery_reviewed"
	EventTaskDeliveryWithdrawn EventType = "task.delivery_withdrawn"
	EventChatMessage           EventType = "chat.message"

	// EventAutomation is a notification produced by an automation rule
	// (for example a due-date reminder).
	EventAutomation EventType = "automation.triggered"
)

type SendInput struct {
	TenantID string `json:"tenantId"`

	RecipientUserID string `json:"recipientUserId"`

	Type EventType `json:"type"`

	ActorUserID *string `json:"actorUserId,omitempty"`

	ProjectID *string `json:"projectId,omitempty"`
	TaskID    *string `json:"taskId,omitempty"`
	CommentID *string `json:"commentId,omitempty"`

	ConversationID *string `json:"conversationId,omitempty"`
	MessageID      *string `json:"messageId,omitempty"`

	Title string  `json:"title"`
	Body  *string `json:"body,omitempty"`

	Data map[string]any `json:"data,omitempty"`
}
