package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

type Service struct {
	repository *Repository

	projectRepository *project.Repository
	projectAuthorizer *project.Authorizer
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,
		projectAuthorizer: projectAuthorizer,
	}
}

type CreateInput struct {
	Identity  auth.Identity
	ProjectID string

	StatusID string

	ParentTaskID *string

	Title       string
	Description *string

	Priority Priority

	AssigneeUserID *string

	DueAt *time.Time
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Task, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionCreateTask,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"authorize task creation: %w",
			err,
		)
	}

	title := strings.TrimSpace(
		input.Title,
	)

	if title == "" ||
		len(title) > 300 {

		return Task{}, ErrInvalidTitle
	}

	priority := input.Priority

	if priority == "" {
		priority = PriorityMedium
	}

	switch priority {
	case PriorityLow,
		PriorityMedium,
		PriorityHigh,
		PriorityUrgent:

	default:
		return Task{}, ErrInvalidPriority
	}

	// Verify status belongs to project.
	_, err = s.projectRepository.GetStatus(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.StatusID,
	)
	if err != nil {
		if errors.Is(
			err,
			project.ErrStatusNotFound,
		) {
			return Task{}, ErrStatusNotFound
		}

		return Task{}, fmt.Errorf(
			"validate task status: %w",
			err,
		)
	}

	// Verify assignee is a project member.
	if input.AssigneeUserID != nil {
		_, err := s.projectRepository.GetMemberRole(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			*input.AssigneeUserID,
		)
		if err != nil {
			if errors.Is(
				err,
				project.ErrNotFound,
			) {
				return Task{},
					ErrAssigneeNotMember
			}

			return Task{}, fmt.Errorf(
				"validate task assignee: %w",
				err,
			)
		}
	}

	// Verify parent belongs to same project.
	if input.ParentTaskID != nil {
		_, err := s.repository.GetForProject(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			*input.ParentTaskID,
		)
		if err != nil {
			if errors.Is(
				err,
				ErrTaskNotFound,
			) {
				return Task{},
					ErrParentTaskNotFound
			}

			return Task{}, fmt.Errorf(
				"validate parent task: %w",
				err,
			)
		}
	}

	task, err := s.repository.Create(
		ctx,
		CreateParams{
			ID: uuid.NewString(),

			TenantID: input.Identity.TenantID,

			ProjectID: input.ProjectID,
			StatusID:  input.StatusID,

			ParentTaskID: input.ParentTaskID,

			Title:       title,
			Description: input.Description,

			Priority: priority,

			AssigneeUserID: input.AssigneeUserID,

			CreatedByUserID: input.Identity.UserID,

			DueAt: input.DueAt,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"create task: %w",
			err,
		)
	}

	return task, nil
}

type ListInput struct {
	Identity  auth.Identity
	ProjectID string

	Limit  int
	Cursor string

	StatusID       string
	AssigneeUserID string
	Priority       string
	ParentTaskID   string
	Search         string
}

func (s *Service) List(
	ctx context.Context,
	input ListInput,
) (TaskPage, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewTasks,
	)
	if err != nil {
		return TaskPage{}, fmt.Errorf(
			"authorize task list: %w",
			err,
		)
	}

	limit := input.Limit

	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	var cursor *Cursor

	if input.Cursor != "" {
		decoded, err := DecodeCursor(
			input.Cursor,
		)
		if err != nil {
			return TaskPage{}, fmt.Errorf(
				"invalid cursor: %w",
				err,
			)
		}

		cursor = &decoded
	}

	var statusID *string

	if input.StatusID != "" {
		statusID = &input.StatusID
	}

	var assigneeUserID *string

	if input.AssigneeUserID != "" {
		assigneeUserID =
			&input.AssigneeUserID
	}

	var priority *Priority

	if input.Priority != "" {
		value := Priority(
			input.Priority,
		)

		switch value {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh,
			PriorityUrgent:

		default:
			return TaskPage{},
				ErrInvalidPriority
		}

		priority = &value
	}

	var parentTaskID *string

	if input.ParentTaskID != "" {
		parentTaskID =
			&input.ParentTaskID
	}

	var search *string

	if value := strings.TrimSpace(
		input.Search,
	); value != "" {
		search = &value
	}

	options := ListOptions{
		Limit: int32(
			limit + 1,
		),

		Cursor: cursor,

		StatusID:       statusID,
		AssigneeUserID: assigneeUserID,
		Priority:       priority,
		ParentTaskID:   parentTaskID,
		Search:         search,
	}

	tasks, err := s.repository.ListForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		options,
	)
	if err != nil {
		return TaskPage{}, fmt.Errorf(
			"list tasks: %w",
			err,
		)
	}

	hasNext := len(tasks) > limit

	if hasNext {
		tasks = tasks[:limit]
	}

	var nextCursor *string

	if hasNext &&
		len(tasks) > 0 {

		last := tasks[len(tasks)-1]

		value := EncodeCursor(
			Cursor{
				CreatedAt: last.CreatedAt,
				ID:        last.ID,
			},
		)

		nextCursor = &value
	}

	return TaskPage{
		Items:      tasks,
		NextCursor: nextCursor,
	}, nil
}
