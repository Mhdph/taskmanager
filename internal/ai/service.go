package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/comment"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

type Service struct {
	client Client

	taskRepository *task.Repository

	commentRepository *comment.Repository

	projectRepository *project.Repository

	projectAuthorizer *project.Authorizer
}

const providerTimeout = 30 * time.Second

func providerContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerTimeout)
}

func NewService(
	client Client,
	taskRepository *task.Repository,
	commentRepository *comment.Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
) *Service {
	return &Service{
		client: client,

		taskRepository: taskRepository,

		commentRepository: commentRepository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,
	}
}

type TaskFromTextInput struct {
	Identity auth.Identity

	Text string
}

func (s *Service) TaskFromText(
	ctx context.Context,
	input TaskFromTextInput,
) (TaskDraft, error) {
	text := strings.TrimSpace(
		input.Text,
	)

	if text == "" ||
		len(text) > 20000 {

		return TaskDraft{},
			ErrInvalidInput
	}

	providerCtx, cancel := providerContext(ctx)
	defer cancel()
	result, err :=
		s.client.TaskFromText(
			providerCtx,
			text,
		)

	if err != nil {
		return TaskDraft{}, fmt.Errorf(
			"generate task draft: %w",
			err,
		)
	}

	return result, nil
}

type TaskActionInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string
}

func (s *Service) buildTaskContext(
	ctx context.Context,
	input TaskActionInput,
) (TaskContext, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewTasks,
	)

	if err != nil {
		return TaskContext{}, err
	}

	item, err :=
		s.taskRepository.GetForProject(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			input.TaskID,
		)

	if err != nil {
		return TaskContext{}, err
	}

	status, err :=
		s.projectRepository.GetStatus(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			item.StatusID,
		)

	if err != nil {
		return TaskContext{}, err
	}

	comments, err :=
		s.commentRepository.ListForTask(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			input.TaskID,
			comment.ListOptions{
				Limit: 100,
			},
		)

	if err != nil {
		return TaskContext{}, err
	}

	commentContext := make(
		[]CommentContext,
		0,
		len(comments),
	)

	for _, item := range comments {

		commentContext = append(
			commentContext,
			CommentContext{
				AuthorUserID: item.AuthorUserID,

				Body: item.Body,
			},
		)
	}
	var startAt *string

	if item.StartAt != nil {
		value :=
			item.StartAt.Format(
				time.RFC3339,
			)

		startAt = &value
	}

	var dueAt *string

	if item.DueAt != nil {
		value :=
			item.DueAt.Format(
				time.RFC3339,
			)

		dueAt = &value
	}

	return TaskContext{
		Title: item.Title,

		Description: item.Description,

		Priority: string(item.Priority),

		StatusName: status.Name,

		StatusCategory: string(status.Category),

		StartAt: startAt,

		DueAt: dueAt,

		Comments: commentContext,
	}, nil
}
func (s *Service) SummarizeTask(
	ctx context.Context,
	input TaskActionInput,
) (TaskSummary, error) {
	taskContext, err :=
		s.buildTaskContext(
			ctx,
			input,
		)

	if err != nil {
		return TaskSummary{}, fmt.Errorf(
			"build task context: %w",
			err,
		)
	}

	providerCtx, cancel := providerContext(ctx)
	defer cancel()
	result, err :=
		s.client.SummarizeTask(
			providerCtx,
			taskContext,
		)

	if err != nil {
		return TaskSummary{}, fmt.Errorf(
			"summarize task: %w",
			err,
		)
	}

	return result, nil
}
func (s *Service) SuggestTask(
	ctx context.Context,
	input TaskActionInput,
) (TaskSuggestion, error) {
	taskContext, err :=
		s.buildTaskContext(
			ctx,
			input,
		)

	if err != nil {
		return TaskSuggestion{}, fmt.Errorf(
			"build task context: %w",
			err,
		)
	}

	providerCtx, cancel := providerContext(ctx)
	defer cancel()
	result, err :=
		s.client.SuggestTask(
			providerCtx,
			taskContext,
		)

	if err != nil {
		return TaskSuggestion{}, fmt.Errorf(
			"suggest task: %w",
			err,
		)
	}

	return result, nil
}

type ProjectSummaryInput struct {
	Identity auth.Identity

	ProjectID string
}

func (s *Service) SummarizeProject(
	ctx context.Context,
	input ProjectSummaryInput,
) (ProjectSummary, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewDashboard,
	)

	if err != nil {
		return ProjectSummary{}, err
	}

	projectItem, err :=
		s.projectRepository.GetForTenant(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
		)

	if err != nil {
		return ProjectSummary{}, err
	}

	rows, err :=
		s.taskRepository.ListProjectTasksForAI(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
		)

	if err != nil {
		return ProjectSummary{}, err
	}
	tasks := make(
		[]ProjectTaskContext,
		0,
		len(rows),
	)

	for _, item := range rows {

		var dueAt *string

		if item.DueAt != nil {
			value :=
				item.DueAt.Format(
					time.RFC3339,
				)

			dueAt = &value
		}

		tasks = append(
			tasks,
			ProjectTaskContext{
				ID: item.ID,

				Title: item.Title,

				Priority: string(item.Priority),

				StatusName: item.StatusName,

				StatusCategory: item.StatusCategory,

				DueAt: dueAt,

				Completed: item.CompletedAt != nil,
			},
		)
	}

	providerCtx, cancel := providerContext(ctx)
	defer cancel()
	result, err :=
		s.client.SummarizeProject(
			providerCtx,
			ProjectContext{
				Name: projectItem.Name,

				Description: projectItem.Description,

				Tasks: tasks,
			},
		)

	if err != nil {
		return ProjectSummary{}, fmt.Errorf(
			"summarize project: %w",
			err,
		)
	}

	return result, nil
}
