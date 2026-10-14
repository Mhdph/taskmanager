package ai

import "context"

// UnavailableClient reports that no AI provider has been connected.
type UnavailableClient struct{}

var _ Client = UnavailableClient{}

func (UnavailableClient) TaskFromText(context.Context, string) (TaskDraft, error) {
	return TaskDraft{}, ErrProviderUnavailable
}

func (UnavailableClient) SummarizeTask(context.Context, TaskContext) (TaskSummary, error) {
	return TaskSummary{}, ErrProviderUnavailable
}

func (UnavailableClient) SuggestTask(context.Context, TaskContext) (TaskSuggestion, error) {
	return TaskSuggestion{}, ErrProviderUnavailable
}

func (UnavailableClient) SummarizeProject(context.Context, ProjectContext) (ProjectSummary, error) {
	return ProjectSummary{}, ErrProviderUnavailable
}
