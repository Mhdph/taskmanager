package chat

import "time"

// EventKind identifies a durable or ephemeral chat event.
type EventKind string

const (
	EventMessageCreated EventKind = "message.created"
	EventMessageEdited  EventKind = "message.edited"
	EventMessageDeleted EventKind = "message.deleted"

	EventConversationRead EventKind = "conversation.read"

	// EventHello is generated locally for every new WebSocket connection;
	// it never travels through the broker.
	EventHello EventKind = "hello"
)

// Event is the small broker payload published through Postgres NOTIFY.
// Message events carry identifiers only: the listener hydrates the message
// from the database, so the payload always fits the 8 KB NOTIFY limit and
// the pushed state is the committed state (not a stale in-memory copy).
type Event struct {
	Kind EventKind `json:"kind"`

	TenantID       string `json:"tenantId"`
	ConversationID string `json:"conversationId,omitempty"`

	MessageID string `json:"messageId,omitempty"`

	Seq int64 `json:"seq,omitempty"`

	ActorUserID string `json:"actorUserId,omitempty"`

	LastReadSeq int64 `json:"lastReadSeq,omitempty"`
}

// WireMessage is the message representation shared by the REST API and
// WebSocket events.
type WireMessage struct {
	ID string `json:"id"`

	ConversationID string `json:"conversationId"`

	Seq int64 `json:"seq"`

	Kind string `json:"kind"`

	AuthorUserID *string `json:"authorUserId"`

	Body string `json:"body"`

	ClientMessageID *string `json:"clientMessageId"`

	CreatedAt time.Time `json:"createdAt"`

	EditedAt *time.Time `json:"editedAt"`

	DeletedAt *time.Time `json:"deletedAt"`
}

func NewWireMessage(item Message) WireMessage {
	return WireMessage{
		ID: item.ID,

		ConversationID: item.ConversationID,

		Seq: item.Seq,

		Kind: string(item.Kind),

		AuthorUserID: item.AuthorUserID,

		Body: item.Body,

		ClientMessageID: item.ClientMessageID,

		CreatedAt: item.CreatedAt,

		EditedAt: item.EditedAt,

		DeletedAt: item.DeletedAt,
	}
}

// ClientEvent is the JSON frame pushed to WebSocket clients.
type ClientEvent struct {
	Type string `json:"type"`

	ConversationID string `json:"conversationId,omitempty"`

	Seq int64 `json:"seq,omitempty"`

	Message *WireMessage `json:"message,omitempty"`

	UserID string `json:"userId,omitempty"`

	TenantID string `json:"tenantId,omitempty"`

	LastReadSeq int64 `json:"lastReadSeq,omitempty"`

	ServerTime time.Time `json:"serverTime"`
}
