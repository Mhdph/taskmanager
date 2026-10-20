package chat

import "time"

type ConversationType string

const (
	ConversationTypeProject ConversationType = "project"
	ConversationTypeDM      ConversationType = "dm"
	ConversationTypeGroup   ConversationType = "group"
)

type MemberRole string

const (
	MemberRoleOwner  MemberRole = "owner"
	MemberRoleMember MemberRole = "member"
)

// MemberSource distinguishes membership managed by the project sync from
// explicit membership (DM/group members, admins opening a project chat).
type MemberSource string

const (
	MemberSourceProject MemberSource = "project"
	MemberSourceManual  MemberSource = "manual"
)

type MessageKind string

const (
	MessageKindUser   MessageKind = "user"
	MessageKindSystem MessageKind = "system"
)

type Conversation struct {
	ID       string
	TenantID string

	Type ConversationType

	// ProjectID is set for project conversations only.
	ProjectID *string

	Title string

	CreatedByUserID string

	LastMessageSeq int64
	LastMessageAt  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Member struct {
	UserID string

	Role   MemberRole
	Source MemberSource

	LastReadSeq int64

	JoinedAt time.Time
}

type MessagePreview struct {
	Kind MessageKind

	Body string

	AuthorUserID *string

	CreatedAt time.Time
}

// ConversationSummary is the conversation-list projection: the
// conversation plus the requesting member's read state and the tail of the
// message stream.
type ConversationSummary struct {
	Conversation

	MemberRole MemberRole

	LastReadSeq int64
	UnreadCount int64

	LastMessage *MessagePreview

	MemberIDs []string
}

type Attachment struct {
	ID string

	TenantID string

	ConversationID string

	// MessageID is nil while the upload is still pending.
	MessageID *string

	UploadedByUserID string

	Filename string

	ContentType string

	SizeBytes int64

	// ObjectKey is the storage location; never expose it to clients.
	ObjectKey string

	CreatedAt time.Time

	DeletedAt *time.Time
}

// StreamToken is a one-time ticket for the WebSocket handshake. The raw
// value is returned to the client once; only its hash is stored.
type StreamToken struct {
	Token string

	ExpiresAt time.Time
}

// StreamTokenIdentity is the identity recovered by consuming a ticket.
type StreamTokenIdentity struct {
	TenantID string

	UserID string
}

type Message struct {
	ID       string
	TenantID string

	ConversationID string

	// Seq is the monotonic ordering key assigned by the database.
	Seq int64

	Kind MessageKind

	// AuthorUserID is nil for system messages.
	AuthorUserID *string

	Body string

	// ClientMessageID is the sender-provided idempotency key.
	ClientMessageID *string

	Attachments []Attachment

	CreatedAt time.Time

	EditedAt  *time.Time
	DeletedAt *time.Time
}

type MessagePage struct {
	Items []Message

	// NextCursor points at the oldest item in the page; pass it as
	// "before" to load the previous page.
	NextCursor *string
}

type ConversationDetail struct {
	Conversation

	Members []Member
}
