package ai

import "context"

type Client interface {
	TaskFromText(
		ctx context.Context,
		text string,
	) (TaskDraft, error)

	SummarizeTask(
		ctx context.Context,
		input TaskContext,
	) (TaskSummary, error)

	SuggestTask(
		ctx context.Context,
		input TaskContext,
	) (TaskSuggestion, error)

	SummarizeProject(
		ctx context.Context,
		input ProjectContext,
	) (ProjectSummary, error)
}
type CommentContext struct {
	AuthorUserID string `json:"authorUserId"`

	Body string `json:"body"`
}

type TaskContext struct {
	Title string `json:"title"`

	Description *string `json:"description"`

	Priority string `json:"priority"`

	StatusName string `json:"statusName"`

	StatusCategory string `json:"statusCategory"`

	StartAt *string `json:"startAt"`

	DueAt *string `json:"dueAt"`

	Comments []CommentContext `json:"comments"`
}

type ProjectTaskContext struct {
	ID string `json:"id"`

	Title string `json:"title"`

	Priority string `json:"priority"`

	StatusName string `json:"statusName"`

	StatusCategory string `json:"statusCategory"`

	DueAt *string `json:"dueAt"`

	Completed bool `json:"completed"`
}

type ProjectContext struct {
	Name string `json:"name"`

	Description *string `json:"description"`

	Tasks []ProjectTaskContext `json:"tasks"`
}
