package task

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

type Service struct {
	repository *Repository

	projectRepository  *project.Repository
	projectAuthorizer  *project.Authorizer
	notificationSender NotificationSender
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	notificationSender NotificationSender,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		notificationSender: notificationSender,
	}
}

type NotificationSender interface {
	Send(
		ctx context.Context,
		input notification.SendInput,
	) error
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
	status, err := s.projectRepository.GetStatus(
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

	var completedAt *time.Time

	if status.Category == project.StatusCategoryDone {
		now := time.Now().UTC()
		completedAt = &now
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

			DueAt:       input.DueAt,
			CompletedAt: completedAt,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"create task: %w",
			err,
		)
	}

	if task.AssigneeUserID != nil {
		s.sendAssignedNotification(
			ctx,
			input.Identity,
			task,
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
func (s *Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) (Task, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTasks,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"authorize task view: %w",
			err,
		)
	}

	item, err := s.repository.GetForProject(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	return item, nil
}

type UpdateInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	Title *string

	UpdateDescription bool
	Description       *string

	StatusID *string

	Priority *Priority

	UpdateAssignee bool
	AssigneeUserID *string

	UpdateParentTask bool
	ParentTaskID     *string

	UpdateDueAt bool
	DueAt       *time.Time
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Task, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"authorize task update: %w",
			err,
		)
	}

	current, err := s.repository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"get current task: %w",
			err,
		)
	}
	assigneeChanged := false

	statusChanged := false

	if input.StatusID != nil {
		statusChanged =
			current.StatusID != *input.StatusID
	}

	if input.UpdateAssignee {
		assigneeChanged =
			!sameOptionalString(
				current.AssigneeUserID,
				input.AssigneeUserID,
			)
	}

	var title *string

	if input.Title != nil {
		value := strings.TrimSpace(
			*input.Title,
		)

		if value == "" ||
			len(value) > 300 {
			return Task{},
				ErrInvalidTitle
		}

		title = &value
	}

	if input.Priority != nil {
		switch *input.Priority {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh,
			PriorityUrgent:

		default:
			return Task{},
				ErrInvalidPriority
		}
	}
	var updateCompletedAt bool
	var completedAt *time.Time
	var previousStatus *project.ProjectStatus
	var nextStatus *project.ProjectStatus

	if input.StatusID != nil {
		status, err := s.projectRepository.GetStatus(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			*input.StatusID,
		)
		if err != nil {
			if errors.Is(
				err,
				project.ErrStatusNotFound,
			) {
				return Task{},
					ErrStatusNotFound
			}

			return Task{}, fmt.Errorf(
				"validate task status: %w",
				err,
			)
		}

		nextStatus = &status

		if statusChanged {
			oldStatus, err :=
				s.projectRepository.GetStatus(
					ctx,
					input.Identity.TenantID,
					input.ProjectID,
					current.StatusID,
				)

			if err != nil {
				return Task{}, fmt.Errorf(
					"get current task status: %w",
					err,
				)
			}

			previousStatus = &oldStatus
		}

		if statusChanged {
			if status.Category ==
				project.StatusCategoryDone {

				if current.CompletedAt == nil {
					now := time.Now().UTC()

					updateCompletedAt = true
					completedAt = &now
				}

			} else if current.CompletedAt != nil {
				updateCompletedAt = true
				completedAt = nil
			}
		}
	}
	if input.UpdateAssignee &&
		input.AssigneeUserID != nil {

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
	updated, err := s.repository.Update(
		ctx,
		UpdateParams{
			TenantID: input.Identity.TenantID,

			ProjectID: input.ProjectID,
			TaskID:    input.TaskID,

			Title: title,

			UpdateDescription: input.UpdateDescription,
			Description:       input.Description,

			StatusID: input.StatusID,

			Priority: input.Priority,

			UpdateAssignee: input.UpdateAssignee,
			AssigneeUserID: input.AssigneeUserID,

			UpdateParentTask: input.UpdateParentTask,
			ParentTaskID:     input.ParentTaskID,

			UpdateDueAt: input.UpdateDueAt,
			DueAt:       input.DueAt,

			UpdateCompletedAt: updateCompletedAt,
			CompletedAt:       completedAt,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"update task: %w",
			err,
		)
	}

	if assigneeChanged {
		s.sendAssignedNotification(
			ctx,
			input.Identity,
			updated,
		)
	}

	if statusChanged &&
		previousStatus != nil &&
		nextStatus != nil {

		s.sendStatusChangedNotifications(
			ctx,
			input.Identity,
			updated,
			*previousStatus,
			*nextStatus,
		)
	}

	return updated, nil
}

func (s *Service) Archive(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) error {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionArchiveTask,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize task archive: %w",
			err,
		)
	}

	if err := s.repository.Archive(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	); err != nil {
		return fmt.Errorf(
			"archive task: %w",
			err,
		)
	}

	return nil
}

type AddDependencyInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	DependsOnTaskID string
}

func (s *Service) AddDependency(
	ctx context.Context,
	input AddDependencyInput,
) (Dependency, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageTaskDependencies,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"authorize dependency management: %w",
			err,
		)
	}

	if input.TaskID == input.DependsOnTaskID {
		return Dependency{},
			ErrSelfDependency
	}

	// Source task must belong to project.
	_, err = s.repository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	// Dependency target must belong to same project.
	_, err = s.repository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.DependsOnTaskID,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"get dependency task: %w",
			err,
		)
	}

	// If target already depends transitively on source,
	// adding source -> target creates a cycle.
	wouldCycle, err := s.repository.DependsTransitivelyOn(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.DependsOnTaskID,
		input.TaskID,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"check dependency cycle: %w",
			err,
		)
	}

	if wouldCycle {
		return Dependency{},
			ErrDependencyCycle
	}

	dependency, err := s.repository.AddDependency(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		input.DependsOnTaskID,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"add dependency: %w",
			err,
		)
	}

	return dependency, nil
}

func (s *Service) ListDependencies(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) ([]Dependency, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTaskDependencies,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"authorize dependency view: %w",
			err,
		)
	}

	_, err = s.repository.GetForProject(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	items, err := s.repository.ListDependencies(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list dependencies: %w",
			err,
		)
	}

	return items, nil
}

type RemoveDependencyInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	DependsOnTaskID string
}

func (s *Service) RemoveDependency(
	ctx context.Context,
	input RemoveDependencyInput,
) error {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageTaskDependencies,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize dependency management: %w",
			err,
		)
	}

	if err := s.repository.DeleteDependency(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		input.DependsOnTaskID,
	); err != nil {
		return fmt.Errorf(
			"remove dependency: %w",
			err,
		)
	}

	return nil
}

func sameOptionalString(
	a *string,
	b *string,
) bool {
	if a == nil && b == nil {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	return *a == *b
}
func (s *Service) sendAssignedNotification(
	ctx context.Context,
	identity auth.Identity,
	item Task,
) {
	if item.AssigneeUserID == nil {
		return
	}

	if *item.AssigneeUserID ==
		identity.UserID {

		return
	}

	actorUserID :=
		identity.UserID

	projectID :=
		item.ProjectID

	taskID :=
		item.ID

	recipientUserID :=
		*item.AssigneeUserID

	body :=
		item.Title

	err := s.notificationSender.Send(
		ctx,
		notification.SendInput{
			TenantID: identity.TenantID,

			RecipientUserID: recipientUserID,

			Type: notification.EventTaskAssigned,

			ActorUserID: &actorUserID,

			ProjectID: &projectID,

			TaskID: &taskID,

			Title: "You were assigned a task",

			Body: &body,

			Data: map[string]any{
				"taskId": item.ID,

				"projectId": item.ProjectID,

				"assigneeUserId": recipientUserID,
			},
		},
	)

	if err != nil {
		log.Printf(
			"send task assignment notification: %v",
			err,
		)
	}
}
func (s *Service) sendStatusChangedNotifications(
	ctx context.Context,
	identity auth.Identity,
	item Task,
	fromStatus project.ProjectStatus,
	toStatus project.ProjectStatus,
) {
	recipients := make(
		map[string]struct{},
	)

	if item.AssigneeUserID != nil {
		recipients[*item.AssigneeUserID] = struct{}{}
	}

	if item.CreatedByUserID != "" {
		recipients[item.CreatedByUserID] = struct{}{}
	}

	watchers, err :=
		s.repository.ListWatchers(
			ctx,
			identity.TenantID,
			item.ProjectID,
			item.ID,
		)

	if err != nil {
		log.Printf(
			"list watchers for notification: %v",
			err,
		)
	} else {
		for _, watcher := range watchers {
			recipients[watcher.UserID] = struct{}{}
		}
	}

	delete(
		recipients,
		identity.UserID,
	)

	for recipientUserID := range recipients {

		actorUserID :=
			identity.UserID

		projectID :=
			item.ProjectID

		taskID :=
			item.ID

		body := fmt.Sprintf(
			"%s: %s → %s",
			item.Title,
			fromStatus.Name,
			toStatus.Name,
		)

		err := s.notificationSender.Send(
			ctx,
			notification.SendInput{
				TenantID: identity.TenantID,

				RecipientUserID: recipientUserID,

				Type: notification.EventTaskStatusChanged,

				ActorUserID: &actorUserID,

				ProjectID: &projectID,

				TaskID: &taskID,

				Title: "Task status changed",

				Body: &body,

				Data: map[string]any{
					"taskId": item.ID,

					"projectId": item.ProjectID,

					"fromStatusId": fromStatus.ID,

					"fromStatusName": fromStatus.Name,

					"fromStatusCategory": string(
						fromStatus.Category,
					),

					"toStatusId": toStatus.ID,

					"toStatusName": toStatus.Name,

					"toStatusCategory": string(
						toStatus.Category,
					),
				},
			},
		)

		if err != nil {
			log.Printf(
				"send task status notification to %s: %v",
				recipientUserID,
				err,
			)
		}
	}
}

type AddWatcherInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	UserID string
}

func (s *Service) AddWatcher(
	ctx context.Context,
	input AddWatcherInput,
) (Watcher, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageTaskWatchers,
	)
	if err != nil {
		return Watcher{}, fmt.Errorf(
			"authorize watcher management: %w",
			err,
		)
	}

	_, err = s.repository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
	)
	if err != nil {
		return Watcher{}, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	userID := strings.TrimSpace(
		input.UserID,
	)

	if userID == "" {
		return Watcher{},
			ErrWatcherNotMember
	}

	_, err =
		s.projectRepository.GetMemberRole(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			userID,
		)

	if err != nil {
		if errors.Is(
			err,
			project.ErrNotFound,
		) {
			return Watcher{},
				ErrWatcherNotMember
		}

		return Watcher{}, fmt.Errorf(
			"validate watcher membership: %w",
			err,
		)
	}

	watcher, err :=
		s.repository.AddWatcher(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			input.TaskID,
			userID,
		)

	if err != nil {
		return Watcher{}, fmt.Errorf(
			"add watcher: %w",
			err,
		)
	}

	return watcher, nil
}

func (s *Service) ListWatchers(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) ([]Watcher, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTaskWatchers,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"authorize watcher view: %w",
			err,
		)
	}

	_, err = s.repository.GetForProject(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	items, err :=
		s.repository.ListWatchers(
			ctx,
			identity.TenantID,
			projectID,
			taskID,
		)

	if err != nil {
		return nil, fmt.Errorf(
			"list watchers: %w",
			err,
		)
	}

	return items, nil

}

type RemoveWatcherInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	UserID string
}

func (s *Service) RemoveWatcher(
	ctx context.Context,
	input RemoveWatcherInput,
) error {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageTaskWatchers,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize watcher management: %w",
			err,
		)
	}

	if err := s.repository.RemoveWatcher(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		input.UserID,
	); err != nil {
		return fmt.Errorf(
			"remove watcher: %w",
			err,
		)
	}

	return nil
}
