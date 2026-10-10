package comment

import "time"

type Comment struct {
	ID        string
	TenantID  string
	ProjectID string
	TaskID    string

	AuthorUserID string

	Body string

	MentionedUserIDs []string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type CommentPage struct {
	Items      []Comment
	NextCursor *string
}
