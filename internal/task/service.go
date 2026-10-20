package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/audit"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/storage"
)

type AuditRecorder interface {
	Record(
		ctx context.Context,
		input audit.RecordInput,
	) error
}

type Service struct {
	repository *Repository

	projectRepository *project.Repository
	projectAuthorizer *project.Authorizer

	notificationSender NotificationSender

	fileStorage FileStorage

	auditRecorder AuditRecorder

	maxUploadSize int64
}

func (s *Service) UploadRequestLimit() int64 { return s.maxUploadSize + (1 << 20) }

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	notificationSender NotificationSender,
	fileStorage FileStorage,
	auditRecorder AuditRecorder,
	maxUploadSize int64,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		notificationSender: notificationSender,

		fileStorage: fileStorage,

		auditRecorder: auditRecorder,

		maxUploadSize: maxUploadSize,
	}
}

type NotificationSender interface {
	Send(
		ctx context.Context,
		input notification.SendInput,
	) error
}

type CreateInput struct {
	Identity auth.Identity

	ProjectID string

	StatusID string

	ParentTaskID *string

	Title       string
	Description *string

	Priority Priority

	AssigneeUserID *string

	StartAt *time.Time
	DueAt   *time.Time

	// Optional estimate (seconds) and recurrence rule.
	EstimateSeconds *int64

	Recurrence *string
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
		utf8.RuneCountInString(title) > 300 {

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

	if input.StartAt != nil &&
		input.DueAt != nil &&
		input.StartAt.After(*input.DueAt) {
		return Task{}, ErrInvalidTaskDateRange
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

	completedAt := completedAtForCreate(status.Category, time.Now().UTC())

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

	// Estimate and recurrence carry the same rules as update.
	if input.EstimateSeconds != nil &&
		(*input.EstimateSeconds <= 0 ||
			*input.EstimateSeconds > maxEstimateSeconds) {
		return Task{},
			ErrInvalidEstimate
	}

	if input.Recurrence != nil && *input.Recurrence != "" {

		switch *input.Recurrence {
		case "daily", "weekly", "biweekly", "monthly":

		default:
			return Task{},
				ErrInvalidRecurrence
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

			StartAt:     input.StartAt,
			DueAt:       input.DueAt,
			CompletedAt: completedAt,

			EstimateSeconds: input.EstimateSeconds,

			Recurrence: input.Recurrence,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"create task: %w",
			err,
		)
	}

	projectID := task.ProjectID
	taskID := task.ID
	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionTaskCreated,
		EntityType:  audit.EntityTask,
		EntityID:    taskID,
		ProjectID:   auditID(projectID),
		TaskID:      auditID(taskID),
		Metadata: map[string]any{
			"status_id":        task.StatusID,
			"priority":         task.Priority,
			"assignee_user_id": task.AssigneeUserID,
			"parent_task_id":   task.ParentTaskID,
		},
	})
	if task.AssigneeUserID != nil {
		s.recordAudit(ctx, audit.RecordInput{
			TenantID:    input.Identity.TenantID,
			ActorUserID: input.Identity.UserID,
			Action:      audit.ActionTaskAssigned,
			EntityType:  audit.EntityTask,
			EntityID:    taskID,
			ProjectID:   auditID(projectID),
			TaskID:      auditID(taskID),
			Metadata: map[string]any{
				"assignee_user_id": task.AssigneeUserID,
			},
		})
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
	Archived  bool
	Identity  auth.Identity
	ProjectID string

	Limit  int
	Cursor string

	StatusID       string
	AssigneeUserID string
	Priority       string
	ParentTaskID   string
	Search         string

	// SprintID selects the tasks of one sprint.
	SprintID string

	// BacklogOnly selects the unfinished tasks without a sprint.
	BacklogOnly bool
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
	if input.Archived {
		if _, err := s.projectRepository.GetForTenant(ctx, input.Identity.TenantID, input.ProjectID); err != nil {
			return TaskPage{}, err
		}
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

	var sprintID *string

	if value := strings.TrimSpace(input.SprintID); value != "" {
		sprintID = &value
	}

	options := ListOptions{
		Archived: input.Archived,
		Limit: int32(
			limit + 1,
		),

		Cursor: cursor,

		StatusID:       statusID,
		AssigneeUserID: assigneeUserID,
		Priority:       priority,
		ParentTaskID:   parentTaskID,
		Search:         search,

		SprintID:    sprintID,
		BacklogOnly: input.BacklogOnly,
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

	labels, err := s.repository.ListTaskLabels(
		ctx,
		identity.TenantID,
		item.ID,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"get task labels: %w",
			err,
		)
	}

	item.Labels = labels

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

	// ClaimOnly prevents replacing another assignee, checked under a row lock.
	ClaimOnly      bool
	UpdateAssignee bool
	AssigneeUserID *string

	UpdateParentTask bool
	ParentTaskID     *string
	UpdateStartAt    bool
	StartAt          *time.Time

	UpdateDueAt bool
	DueAt       *time.Time

	// Estimate in seconds; UpdateEstimate enables clearing via nil.
	UpdateEstimate  bool
	EstimateSeconds *int64

	UpdateRecurrence bool
	Recurrence       *string
}

// taskPositionGap is the default spacing between board positions, leaving
// cheap room for fractional insertions between existing tasks.
const taskPositionGap = 1024

// maxEstimateSeconds caps estimates at ten years of work.
const maxEstimateSeconds = 315_360_000

func completedAtForStatusChange(current *time.Time, statusChanged bool, category project.StatusCategory, now time.Time) (bool, *time.Time) {
	if !statusChanged {
		return false, nil
	}
	if category == project.StatusCategoryDone {
		if current == nil {
			return true, &now
		}
		return false, nil
	}
	if current != nil {
		return true, nil
	}
	return false, nil
}

func completedAtForCreate(category project.StatusCategory, now time.Time) *time.Time {
	if category == project.StatusCategoryDone {
		return &now
	}
	return nil
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

	if input.ClaimOnly {
		input.UpdateAssignee = true
		input.AssigneeUserID = &input.Identity.UserID
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
	effectiveStartAt := current.StartAt
	effectiveDueAt := current.DueAt

	if input.UpdateStartAt {
		effectiveStartAt = input.StartAt
	}
	if input.UpdateDueAt {
		effectiveDueAt = input.DueAt
	}

	if effectiveStartAt != nil &&
		effectiveDueAt != nil &&
		effectiveStartAt.After(*effectiveDueAt) {
		return Task{}, ErrInvalidTaskDateRange
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
			utf8.RuneCountInString(value) > 300 {
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

	if input.UpdateEstimate {
		if input.EstimateSeconds != nil &&
			(*input.EstimateSeconds < 0 ||
				*input.EstimateSeconds > maxEstimateSeconds) {

			return Task{},
				ErrInvalidEstimate
		}
	}

	if input.UpdateRecurrence &&
		input.Recurrence != nil {

		switch *input.Recurrence {
		case "daily", "weekly", "biweekly", "monthly":

		default:
			return Task{},
				ErrInvalidRecurrence
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

		updateCompletedAt, completedAt = completedAtForStatusChange(
			current.CompletedAt,
			statusChanged,
			status.Category,
			time.Now().UTC(),
		)
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

			ClaimOnly:      input.ClaimOnly,
			UpdateAssignee: input.UpdateAssignee,
			AssigneeUserID: input.AssigneeUserID,

			UpdateParentTask: input.UpdateParentTask,
			ParentTaskID:     input.ParentTaskID,

			UpdateStartAt: input.UpdateStartAt,
			StartAt:       input.StartAt,

			UpdateDueAt: input.UpdateDueAt,
			DueAt:       input.DueAt,

			UpdateEstimate:  input.UpdateEstimate,
			EstimateSeconds: input.EstimateSeconds,

			UpdateRecurrence: input.UpdateRecurrence,
			Recurrence:       input.Recurrence,

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

	changedFields := make([]string, 0, 10)
	if title != nil {
		changedFields = append(changedFields, "title")
	}
	if input.UpdateDescription {
		changedFields = append(changedFields, "description")
	}
	if statusChanged {
		changedFields = append(changedFields, "status_id")
	}
	if input.Priority != nil {
		changedFields = append(changedFields, "priority")
	}
	if assigneeChanged {
		changedFields = append(changedFields, "assignee_user_id")
	}
	if input.UpdateParentTask {
		changedFields = append(changedFields, "parent_task_id")
	}
	if input.UpdateStartAt {
		changedFields = append(changedFields, "start_at")
	}
	if input.UpdateDueAt {
		changedFields = append(changedFields, "due_at")
	}
	if input.UpdateEstimate {
		changedFields = append(changedFields, "estimate_seconds")
	}
	if input.UpdateRecurrence {
		changedFields = append(changedFields, "recurrence")
	}

	if len(changedFields) > 0 {
		projectID := updated.ProjectID
		taskID := updated.ID
		s.recordAudit(ctx, audit.RecordInput{
			TenantID:    input.Identity.TenantID,
			ActorUserID: input.Identity.UserID,
			Action:      audit.ActionTaskUpdated,
			EntityType:  audit.EntityTask,
			EntityID:    taskID,
			ProjectID:   auditID(projectID),
			TaskID:      auditID(taskID),
			Metadata: map[string]any{
				"changed_fields": changedFields,
			},
		})
	}

	if statusChanged && previousStatus != nil && nextStatus != nil {
		projectID := updated.ProjectID
		taskID := updated.ID
		s.recordAudit(ctx, audit.RecordInput{
			TenantID:    input.Identity.TenantID,
			ActorUserID: input.Identity.UserID,
			Action:      audit.ActionTaskStatusChanged,
			EntityType:  audit.EntityTask,
			EntityID:    taskID,
			ProjectID:   auditID(projectID),
			TaskID:      auditID(taskID),
			Metadata: map[string]any{
				"from_status_id":   previousStatus.ID,
				"from_status_name": previousStatus.Name,
				"to_status_id":     nextStatus.ID,
				"to_status_name":   nextStatus.Name,
			},
		})
	}

	if assigneeChanged {
		projectID := updated.ProjectID
		taskID := updated.ID
		s.recordAudit(ctx, audit.RecordInput{
			TenantID:    input.Identity.TenantID,
			ActorUserID: input.Identity.UserID,
			Action:      audit.ActionTaskAssigned,
			EntityType:  audit.EntityTask,
			EntityID:    taskID,
			ProjectID:   auditID(projectID),
			TaskID:      auditID(taskID),
			Metadata: map[string]any{
				"from_assignee_user_id": current.AssigneeUserID,
				"to_assignee_user_id":   updated.AssigneeUserID,
			},
		})
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

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.UserID,
		Action:      audit.ActionTaskArchived,
		EntityType:  audit.EntityTask,
		EntityID:    taskID,
		ProjectID:   auditID(projectID),
		TaskID:      auditID(taskID),
	})

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

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionDependencyAdded,
		EntityType:  audit.EntityDependency,
		EntityID:    input.TaskID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(input.TaskID),
		Metadata: map[string]any{
			"depends_on_task_id": input.DependsOnTaskID,
		},
	})

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

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionDependencyRemoved,
		EntityType:  audit.EntityDependency,
		EntityID:    input.TaskID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(input.TaskID),
		Metadata: map[string]any{
			"depends_on_task_id": input.DependsOnTaskID,
		},
	})

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

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionTaskWatcherAdded,
		EntityType:  audit.EntityTask,
		EntityID:    input.TaskID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(input.TaskID),
		Metadata: map[string]any{
			"watcher_user_id": watcher.UserID,
		},
	})

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

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionTaskWatcherRemoved,
		EntityType:  audit.EntityTask,
		EntityID:    input.TaskID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(input.TaskID),
		Metadata: map[string]any{
			"watcher_user_id": input.UserID,
		},
	})

	return nil
}

type FileStorage interface {
	Upload(
		ctx context.Context,
		input storage.UploadInput,
	) error

	Delete(
		ctx context.Context,
		objectKey string,
	) error

	PresignedDownloadURL(
		ctx context.Context,
		objectKey string,
		filename string,
		expiry time.Duration,
	) (string, error)
}
type UploadAttachmentInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	Filename string

	ContentType string

	Size int64

	Reader io.Reader
}

func (s *Service) UploadAttachment(
	ctx context.Context,
	input UploadAttachmentInput,
) (Attachment, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageTaskAttachments,
	)
	if err != nil {
		return Attachment{}, fmt.Errorf(
			"authorize attachment upload: %w",
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
		return Attachment{}, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	filename :=
		strings.TrimSpace(
			input.Filename,
		)

	if filename == "" {
		return Attachment{},
			ErrInvalidAttachment
	}

	if input.Size <= 0 {
		return Attachment{},
			ErrInvalidAttachment
	}

	if input.Size >
		s.maxUploadSize {

		return Attachment{},
			ErrAttachmentTooLarge
	}

	attachmentID :=
		uuid.NewString()

	objectKey := storage.TaskAttachmentObjectKey(
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		attachmentID,
	)

	contentType :=
		input.ContentType

	if contentType == "" {
		contentType =
			"application/octet-stream"
	}

	err = s.fileStorage.Upload(
		ctx,
		storage.UploadInput{
			ObjectKey: objectKey,

			Reader: input.Reader,

			Size: input.Size,

			ContentType: contentType,
		},
	)

	if err != nil {
		return Attachment{}, fmt.Errorf(
			"upload attachment object: %w",
			err,
		)
	}

	attachment, err :=
		s.repository.CreateAttachment(
			ctx,
			CreateAttachmentParams{
				ID: attachmentID,

				TenantID: input.Identity.TenantID,

				ProjectID: input.ProjectID,

				TaskID: input.TaskID,

				UploadedByUserID: input.Identity.UserID,

				Filename: filename,

				ContentType: contentType,

				SizeBytes: input.Size,

				ObjectKey: objectKey,
			},
		)

	if err != nil {
		// The database write failed but the object was already uploaded.
		// Use a detached context: a canceled request must not leave the
		// object behind, and the storage reconciler is the safety net.
		cleanupCtx, cleanupCancel := attachmentStorageContext()
		defer cleanupCancel()

		if cleanupErr :=
			s.fileStorage.Delete(
				cleanupCtx,
				objectKey,
			); cleanupErr != nil {

			log.Printf(
				"cleanup orphan attachment %s: %v",
				objectKey,
				cleanupErr,
			)
		}

		return Attachment{}, fmt.Errorf(
			"save attachment metadata: %w",
			err,
		)
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionAttachmentUploaded,
		EntityType:  audit.EntityAttachment,
		EntityID:    attachment.ID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(input.TaskID),
		Metadata: map[string]any{
			"filename":     attachment.Filename,
			"content_type": attachment.ContentType,
			"size_bytes":   attachment.SizeBytes,
		},
	})

	return attachment, nil
}
func (s *Service) ListAttachments(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) ([]Attachment, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTaskAttachments,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"authorize attachment view: %w",
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

	return s.repository.ListAttachments(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	)
}

type AttachmentDownload struct {
	URL string

	ExpiresAt time.Time
}

// attachmentStorageContext detaches storage cleanup from the request
// context, so a canceled request cannot leave an object behind.
func attachmentStorageContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *Service) GetAttachmentDownload(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
	attachmentID string,
) (AttachmentDownload, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTaskAttachments,
	)
	if err != nil {
		return AttachmentDownload{},
			fmt.Errorf(
				"authorize attachment download: %w",
				err,
			)
	}

	attachment, err :=
		s.repository.GetAttachment(
			ctx,
			identity.TenantID,
			projectID,
			taskID,
			attachmentID,
		)

	if err != nil {
		return AttachmentDownload{},
			err
	}

	expiry :=
		5 * time.Minute

	downloadURL, err :=
		s.fileStorage.
			PresignedDownloadURL(
				ctx,
				attachment.ObjectKey,
				attachment.Filename,
				expiry,
			)

	if err != nil {
		return AttachmentDownload{},
			fmt.Errorf(
				"create download url: %w",
				err,
			)
	}

	return AttachmentDownload{
		URL: downloadURL,

		ExpiresAt: time.Now().
			UTC().
			Add(expiry),
	}, nil
}
func (s *Service) DeleteAttachment(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
	attachmentID string,
) error {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionManageTaskAttachments,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize attachment delete: %w",
			err,
		)
	}

	attachment, err :=
		s.repository.GetAttachment(
			ctx,
			identity.TenantID,
			projectID,
			taskID,
			attachmentID,
		)

	if err != nil {
		return err
	}

	err = s.repository.DeleteAttachment(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
		attachmentID,
	)

	if err != nil {
		return fmt.Errorf(
			"delete attachment metadata: %w",
			err,
		)
	}
	// The metadata is already committed. Delete the object with a detached
	// context so a client disconnect cannot leave the object behind; the
	// storage reconciler removes it later if this fails.
	cleanupCtx, cleanupCancel := attachmentStorageContext()
	defer cleanupCancel()

	if err := s.fileStorage.Delete(cleanupCtx, attachment.ObjectKey); err != nil {
		log.Printf("attachment storage cleanup failed attachment_id=%s: %v", attachment.ID, err)
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.UserID,
		Action:      audit.ActionAttachmentDeleted,
		EntityType:  audit.EntityAttachment,
		EntityID:    attachment.ID,
		ProjectID:   auditID(projectID),
		TaskID:      auditID(taskID),
		Metadata: map[string]any{
			"filename": attachment.Filename,
		},
	})

	return nil
}

type MyWorkInput struct {
	Identity auth.Identity

	Limit  int
	Cursor string

	StatusID string
	Priority string

	DueFrom *time.Time
	DueTo   *time.Time

	OverdueOnly bool

	// SprintID selects the tasks of one sprint.
	SprintID string

	// BacklogOnly selects the unfinished tasks without a sprint.
	BacklogOnly bool
}

func (s *Service) MyWork(
	ctx context.Context,
	input MyWorkInput,
) (MyWorkPage, error) {
	limit := input.Limit

	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	var cursor *Cursor

	if input.Cursor != "" {
		value, err := DecodeCursor(
			input.Cursor,
		)
		if err != nil {
			return MyWorkPage{}, fmt.Errorf(
				"invalid cursor: %w",
				err,
			)
		}

		cursor = &value
	}

	var statusID *string

	if input.StatusID != "" {
		statusID = &input.StatusID
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
			return MyWorkPage{},
				ErrInvalidPriority
		}

		priority = &value
	}

	if input.DueFrom != nil &&
		input.DueTo != nil &&
		input.DueFrom.After(
			*input.DueTo,
		) {

		return MyWorkPage{},
			ErrInvalidTaskDateRange
	}

	var myWorkSprintID *string

	if value := strings.TrimSpace(input.SprintID); value != "" {
		myWorkSprintID = &value
	}

	items, err := s.repository.ListMyWork(
		ctx,
		input.Identity.TenantID,
		input.Identity.UserID,
		MyWorkOptions{
			IsAdmin: input.Identity.IsAdmin(),

			Limit: int32(limit + 1),

			Cursor: cursor,

			StatusID: statusID,

			Priority: priority,

			DueFrom: input.DueFrom,

			DueTo: input.DueTo,

			OverdueOnly: input.OverdueOnly,

			SprintID:    myWorkSprintID,
			BacklogOnly: input.BacklogOnly,
		},
	)

	if err != nil {
		return MyWorkPage{}, fmt.Errorf(
			"list my work: %w",
			err,
		)
	}

	hasNext :=
		len(items) > limit

	if hasNext {
		items =
			items[:limit]
	}

	var nextCursor *string

	if hasNext &&
		len(items) > 0 {

		last :=
			items[len(items)-1]

		value := EncodeCursor(
			Cursor{
				CreatedAt: last.CreatedAt,

				ID: last.ID,
			},
		)

		nextCursor = &value
	}

	return MyWorkPage{
		Items: items,

		NextCursor: nextCursor,
	}, nil
}

type myWorkResponse struct {
	Items []taskResponse `json:"items"`

	NextCursor *string `json:"nextCursor"`
}
