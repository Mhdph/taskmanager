package ai

type TaskDraft struct {
	Title string `json:"title"`

	Description *string `json:"description"`

	Priority string `json:"priority"`

	DueAt *string `json:"dueAt"`

	ActionItems []string `json:"actionItems"`
}

type TaskSummary struct {
	Summary string `json:"summary"`

	ActionItems []string `json:"actionItems"`

	Risks []string `json:"risks"`

	NextSteps []string `json:"nextSteps"`
}

type TaskSuggestion struct {
	Priority *string `json:"priority"`

	StatusCategory *string `json:"statusCategory"`

	DueAt *string `json:"dueAt"`

	Reasoning string `json:"reasoning"`
}

type ProjectSummary struct {
	Summary string `json:"summary"`

	Highlights []string `json:"highlights"`

	Risks []string `json:"risks"`

	BlockedTasks []string `json:"blockedTasks"`

	NextActions []string `json:"nextActions"`
}
