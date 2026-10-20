package chat

import "time"

// EventKind identifies a durable or ephemeral chat event.
type EventKind string

const (
	EventMessageCreated EventKind = "message.created"
	EventMessageEdited  EventKind = "message.edited"
	EventMessageDeleted EventKind = "message.deleted"

	EventConversationRead EventKind = "conversation.read"

	// EventTyping is ephemeral and never stored; it rides the broker so
	// typing indicators also work across API instances.
	EventTyping EventKind = "typing"

	// Presence transitions travel through the broker so every instance can
	// fan them out to its local connections. The event carries the user in
	// ActorUserID; the hub re-checks the shared lease before broadcasting.
	EventPresenceOnline  EventKind = "presence.online"
	EventPresenceOffline EventKind = "presence.offline"

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

	Typing bool `json:"typing,omitempty"`
}

// WireAttachment is attachment metadata sent to clients. The storage object
// key stays server-side; downloads go through the presigned URL endpoint.
type WireAttachment struct {
	ID string `json:"id"`

	Filename string `json:"filename"`

	ContentType string `json:"contentType"`

	SizeBytes int64 `json:"sizeBytes"`

	UploadedByUserID string `json:"uploadedByUserId"`

	CreatedAt time.Time `json:"createdAt"`
}

func NewWireAttachment(item Attachment) WireAttachment {
	return WireAttachment{
		ID: item.ID,

		Filename: item.Filename,

		ContentType: item.ContentType,

		SizeBytes: item.SizeBytes,

		UploadedByUserID: item.UploadedByUserID,

		CreatedAt: item.CreatedAt,
	}
}

func newWireAttachments(items []Attachment) []WireAttachment {
	attachments := make(
		[]WireAttachment,
		0,
		len(items),
	)

	for _, item := range items {
		attachments = append(
			attachments,
			NewWireAttachment(item),
		)
	}

	return attachments
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

	// MentionedUserIDs are the members mentioned in the body.
	MentionedUserIDs []string `json:"mentionedUserIds,omitempty"`

	Attachments []WireAttachment `json:"attachments"`

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

		MentionedUserIDs: item.Mentions,

		Attachments: newWireAttachments(item.Attachments),

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

	// Typing is a pointer so a false value ("stopped typing") is still
	// serialized instead of being dropped by omitempty.
	Typing *bool `json:"typing,omitempty"`

	ServerTime time.Time `json:"serverTime"`
}

// clientFrame is a frame sent by the browser over the WebSocket. Only
// typing is accepted in this phase; anything else is ignored.
type clientFrame struct {
	Type string `json:"type"`

	ConversationID string `json:"conversationId"`

	Typing bool `json:"typing"`
}
