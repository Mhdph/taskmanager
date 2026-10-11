package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type Repository struct {
	queries *db.Queries
}

func NewRepository(
	queries *db.Queries,
) *Repository {
	return &Repository{
		queries: queries,
	}
}

type CreateParams struct {
	ID        string
	TenantID  string
	ProjectID string
	StatusID  string

	ParentTaskID *string

	Title       string
	Description *string

	Priority Priority

	AssigneeUserID *string

	CreatedByUserID string

	StartAt *time.Time

	DueAt *time.Time

	CompletedAt *time.Time
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Task, error) {
	taskID, err := uuid.Parse(params.ID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"parse generated task id: %w",
			err,
		)
	}

	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	statusID, err := uuid.Parse(params.StatusID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	var parentTaskID pgtype.UUID

	if params.ParentTaskID != nil {
		value, err := uuid.Parse(
			*params.ParentTaskID,
		)
		if err != nil {
			return Task{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		parentTaskID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var description pgtype.Text

	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	var assigneeUserID pgtype.Text

	if params.AssigneeUserID != nil {
		assigneeUserID = pgtype.Text{
			String: *params.AssigneeUserID,
			Valid:  true,
		}
	}

	var dueAt pgtype.Timestamptz

	if params.DueAt != nil {
		dueAt = pgtype.Timestamptz{
			Time:  *params.DueAt,
			Valid: true,
		}
	}

	var startAt pgtype.Timestamptz

	if params.StartAt != nil {
		startAt = pgtype.Timestamptz{
			Time:  *params.StartAt,
			Valid: true,
		}
	}

	var completedAt pgtype.Timestamptz

	if params.CompletedAt != nil {
		completedAt = pgtype.Timestamptz{
			Time:  *params.CompletedAt,
			Valid: true,
		}
	}

	row, err := r.queries.CreateTask(
		ctx,
		db.CreateTaskParams{
			ID: pgtype.UUID{
				Bytes: taskID,
				Valid: true,
			},

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			StatusID: pgtype.UUID{
				Bytes: statusID,
				Valid: true,
			},

			ParentTaskID: parentTaskID,

			Title:       params.Title,
			Description: description,

			Priority: string(params.Priority),

			AssigneeUserID: assigneeUserID,

			CreatedByUserID: params.CreatedByUserID,

			DueAt:   dueAt,
			StartAt: startAt,

			CompletedAt: completedAt,
		},
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"create task: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}

func taskFromDB(
	row db.Task,
) Task {
	task := Task{
		TenantID:        row.TenantID,
		Title:           row.Title,
		Priority:        Priority(row.Priority),
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		task.ID = uuid.UUID(
			row.ID.Bytes,
		).String()
	}

	if row.ProjectID.Valid {
		task.ProjectID = uuid.UUID(
			row.ProjectID.Bytes,
		).String()
	}

	if row.StatusID.Valid {
		task.StatusID = uuid.UUID(
			row.StatusID.Bytes,
		).String()
	}

	if row.ParentTaskID.Valid {
		value := uuid.UUID(
			row.ParentTaskID.Bytes,
		).String()

		task.ParentTaskID = &value
	}

	if row.Description.Valid {
		value := row.Description.String
		task.Description = &value
	}

	if row.AssigneeUserID.Valid {
		value := row.AssigneeUserID.String
		task.AssigneeUserID = &value
	}

	if row.StartAt.Valid {
		value := row.StartAt.Time
		task.StartAt = &value
	}

	if row.DueAt.Valid {
		value := row.DueAt.Time
		task.DueAt = &value
	}

	if row.CompletedAt.Valid {
		value := row.CompletedAt.Time
		task.CompletedAt = &value
	}

	if row.ArchivedAt.Valid {
		value := row.ArchivedAt.Time
		task.ArchivedAt = &value
	}

	return task
}

func (r *Repository) GetForProject(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) (Task, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetTaskForProject(
		ctx,
		db.GetTaskForProjectParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrTaskNotFound
		}

		return Task{}, fmt.Errorf(
			"get task for project: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}
func (r *Repository) ListForProject(
	ctx context.Context,
	tenantID string,
	projectID string,
	options ListOptions,
) ([]Task, error) {
	projectUUID, err := uuid.Parse(
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	var statusID pgtype.UUID

	if options.StatusID != nil {
		value, err := uuid.Parse(
			*options.StatusID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		statusID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var assigneeUserID pgtype.Text

	if options.AssigneeUserID != nil {
		assigneeUserID = pgtype.Text{
			String: *options.AssigneeUserID,
			Valid:  true,
		}
	}

	var priority pgtype.Text

	if options.Priority != nil {
		priority = pgtype.Text{
			String: string(
				*options.Priority,
			),
			Valid: true,
		}
	}

	var parentTaskID pgtype.UUID

	if options.ParentTaskID != nil {
		value, err := uuid.Parse(
			*options.ParentTaskID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		parentTaskID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var search pgtype.Text

	if options.Search != nil {
		search = pgtype.Text{
			String: *options.Search,
			Valid:  true,
		}
	}

	var cursorCreatedAt pgtype.Timestamptz
	var cursorID pgtype.UUID

	if options.Cursor != nil {
		cursorCreatedAt = pgtype.Timestamptz{
			Time:  options.Cursor.CreatedAt,
			Valid: true,
		}

		value, err := uuid.Parse(
			options.Cursor.ID,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid cursor id: %v",
				ErrInvalidID,
				err,
			)
		}

		cursorID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	rows, err := r.queries.ListTasksForProject(
		ctx,
		db.ListTasksForProjectParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			StatusID:       statusID,
			AssigneeUserID: assigneeUserID,
			Priority:       priority,
			ParentTaskID:   parentTaskID,
			Search:         search,

			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,

			PageLimit: options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list tasks for project: %w",
			err,
		)
	}

	tasks := make(
		[]Task,
		0,
		len(rows),
	)

	for _, row := range rows {
		tasks = append(
			tasks,
			taskFromDB(row),
		)
	}

	return tasks, nil
}

type UpdateParams struct {
	TenantID  string
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

	UpdateStartAt bool
	StartAt       *time.Time

	UpdateDueAt bool
	DueAt       *time.Time

	UpdateCompletedAt bool
	CompletedAt       *time.Time
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (Task, error) {
	projectID, err := uuid.Parse(
		params.ProjectID,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	taskID, err := uuid.Parse(
		params.TaskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	var title pgtype.Text
	if params.Title != nil {
		title = pgtype.Text{
			String: *params.Title,
			Valid:  true,
		}
	}

	var description pgtype.Text
	if params.Description != nil {
		description = pgtype.Text{
			String: *params.Description,
			Valid:  true,
		}
	}

	var statusID pgtype.UUID
	if params.StatusID != nil {
		value, err := uuid.Parse(
			*params.StatusID,
		)
		if err != nil {
			return Task{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		statusID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var priority pgtype.Text
	if params.Priority != nil {
		priority = pgtype.Text{
			String: string(
				*params.Priority,
			),
			Valid: true,
		}
	}

	var assignee pgtype.Text
	if params.AssigneeUserID != nil {
		assignee = pgtype.Text{
			String: *params.AssigneeUserID,
			Valid:  true,
		}
	}

	var parentTaskID pgtype.UUID
	if params.ParentTaskID != nil {
		value, err := uuid.Parse(
			*params.ParentTaskID,
		)
		if err != nil {
			return Task{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		parentTaskID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var startAt pgtype.Timestamptz
	if params.StartAt != nil {
		startAt = pgtype.Timestamptz{
			Time:  *params.StartAt,
			Valid: true,
		}
	}

	var dueAt pgtype.Timestamptz
	if params.DueAt != nil {
		dueAt = pgtype.Timestamptz{
			Time:  *params.DueAt,
			Valid: true,
		}
	}

	var completedAt pgtype.Timestamptz
	if params.CompletedAt != nil {
		completedAt = pgtype.Timestamptz{
			Time:  *params.CompletedAt,
			Valid: true,
		}
	}

	row, err := r.queries.UpdateTask(
		ctx,
		db.UpdateTaskParams{
			Title: title,

			UpdateDescription: params.UpdateDescription,
			Description:       description,

			StatusID: statusID,

			Priority: priority,

			UpdateAssignee: params.UpdateAssignee,
			AssigneeUserID: assignee,

			UpdateParentTask: params.UpdateParentTask,
			ParentTaskID:     parentTaskID,

			UpdateStartAt: params.UpdateStartAt,
			StartAt:       startAt,

			UpdateDueAt: params.UpdateDueAt,
			DueAt:       dueAt,

			UpdateCompletedAt: params.UpdateCompletedAt,
			CompletedAt:       completedAt,

			TenantID: params.TenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: taskID,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return Task{},
				ErrTaskNotFound
		}

		return Task{}, fmt.Errorf(
			"update task: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}
func (r *Repository) Archive(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.ArchiveTask(
		ctx,
		db.ArchiveTaskParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			ID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"archive task: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrTaskNotFound
	}

	return nil
}
func dependencyFromDB(
	row db.TaskDependency,
) Dependency {
	dependency := Dependency{
		CreatedAt: row.CreatedAt.Time,
	}

	if row.TaskID.Valid {
		dependency.TaskID = uuid.UUID(
			row.TaskID.Bytes,
		).String()
	}

	if row.DependsOnTaskID.Valid {
		dependency.DependsOnTaskID = uuid.UUID(
			row.DependsOnTaskID.Bytes,
		).String()
	}

	return dependency
}

func (r *Repository) AddDependency(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	dependsOnTaskID string,
) (Dependency, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	dependsOnUUID, err := uuid.Parse(
		dependsOnTaskID,
	)
	if err != nil {
		return Dependency{}, fmt.Errorf(
			"%w: invalid dependency task id: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.AddTaskDependency(
		ctx,
		db.AddTaskDependencyParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},

			DependsOnTaskID: pgtype.UUID{
				Bytes: dependsOnUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			return Dependency{},
				ErrDependencyExists
		}

		return Dependency{}, fmt.Errorf(
			"add task dependency: %w",
			err,
		)
	}

	return dependencyFromDB(row), nil
}

func (r *Repository) ListDependencies(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) ([]Dependency, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	rows, err := r.queries.ListTaskDependencies(
		ctx,
		db.ListTaskDependenciesParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list task dependencies: %w",
			err,
		)
	}

	result := make(
		[]Dependency,
		0,
		len(rows),
	)

	for _, row := range rows {
		result = append(
			result,
			dependencyFromDB(row),
		)
	}

	return result, nil
}
func (r *Repository) DeleteDependency(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	dependsOnTaskID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	dependsOnUUID, err := uuid.Parse(
		dependsOnTaskID,
	)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid dependency id: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.DeleteTaskDependency(
		ctx,
		db.DeleteTaskDependencyParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},

			DependsOnTaskID: pgtype.UUID{
				Bytes: dependsOnUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete task dependency: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrDependencyNotFound
	}

	return nil
}
func (r *Repository) DependsTransitivelyOn(
	ctx context.Context,
	tenantID string,
	projectID string,
	startTaskID string,
	targetTaskID string,
) (bool, error) {
	projectUUID, err := uuid.Parse(
		projectID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	startUUID, err := uuid.Parse(
		startTaskID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	targetUUID, err := uuid.Parse(
		targetTaskID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"%w: invalid target task id: %v",
			ErrInvalidID,
			err,
		)
	}

	exists, err := r.queries.TaskDependsTransitivelyOn(
		ctx,
		db.TaskDependsTransitivelyOnParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			StartTaskID: pgtype.UUID{
				Bytes: startUUID,
				Valid: true,
			},

			TargetTaskID: pgtype.UUID{
				Bytes: targetUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return false, fmt.Errorf(
			"check task dependency graph: %w",
			err,
		)
	}

	return exists, nil
}

func watcherFromDB(
	row db.TaskWatcher,
) Watcher {
	item := Watcher{
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt.Time,
	}

	if row.TaskID.Valid {
		item.TaskID = uuid.UUID(
			row.TaskID.Bytes,
		).String()
	}

	return item
}

func (r *Repository) AddWatcher(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	userID string,
) (Watcher, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return Watcher{}, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return Watcher{}, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.AddTaskWatcher(
		ctx,
		db.AddTaskWatcherParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},

			UserID: userID,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			return Watcher{},
				ErrWatcherExists
		}

		return Watcher{}, fmt.Errorf(
			"add task watcher: %w",
			err,
		)
	}

	return watcherFromDB(row), nil
}

func (r *Repository) ListWatchers(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) ([]Watcher, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	rows, err := r.queries.ListTaskWatchers(
		ctx,
		db.ListTaskWatchersParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list task watchers: %w",
			err,
		)
	}

	result := make(
		[]Watcher,
		0,
		len(rows),
	)

	for _, row := range rows {
		result = append(
			result,
			watcherFromDB(row),
		)
	}

	return result, nil
}
func (r *Repository) RemoveWatcher(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	userID string,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid project id: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf(
			"%w: invalid task id: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.DeleteTaskWatcher(
		ctx,
		db.DeleteTaskWatcherParams{
			TenantID: tenantID,

			ProjectID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},

			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},

			UserID: userID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"remove task watcher: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrWatcherNotFound
	}

	return nil
}
func attachmentFromDB(
	row db.TaskAttachment,
) Attachment {
	item := Attachment{
		Filename: row.Filename,

		ContentType: row.ContentType,

		SizeBytes: row.SizeBytes,

		ObjectKey: row.ObjectKey,

		UploadedByUserID: row.UploadedByUserID,

		CreatedAt: row.CreatedAt.Time,
	}

	if row.ID.Valid {
		item.ID = uuid.UUID(
			row.ID.Bytes,
		).String()
	}

	if row.TaskID.Valid {
		item.TaskID = uuid.UUID(
			row.TaskID.Bytes,
		).String()
	}

	return item
}

type CreateAttachmentParams struct {
	ID string

	TenantID string

	ProjectID string
	TaskID    string

	UploadedByUserID string

	Filename string

	ContentType string

	SizeBytes int64

	ObjectKey string
}

func (r *Repository) CreateAttachment(
	ctx context.Context,
	params CreateAttachmentParams,
) (Attachment, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return Attachment{}, fmt.Errorf(
			"%w: invalid attachment id",
			ErrInvalidID,
		)
	}

	projectID, err :=
		uuid.Parse(params.ProjectID)

	if err != nil {
		return Attachment{}, fmt.Errorf(
			"%w: invalid project id",
			ErrInvalidID,
		)
	}

	taskID, err :=
		uuid.Parse(params.TaskID)

	if err != nil {
		return Attachment{}, fmt.Errorf(
			"%w: invalid task id",
			ErrInvalidID,
		)
	}

	row, err :=
		r.queries.CreateTaskAttachment(
			ctx,
			db.CreateTaskAttachmentParams{
				ID: pgtype.UUID{
					Bytes: id,
					Valid: true,
				},

				TenantID: params.TenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectID,
					Valid: true,
				},

				TaskID: pgtype.UUID{
					Bytes: taskID,
					Valid: true,
				},

				UploadedByUserID: params.UploadedByUserID,

				Filename: params.Filename,

				ContentType: params.ContentType,

				SizeBytes: params.SizeBytes,

				ObjectKey: params.ObjectKey,
			},
		)

	if err != nil {
		return Attachment{}, fmt.Errorf(
			"create attachment metadata: %w",
			err,
		)
	}

	return attachmentFromDB(row), nil
}
func (r *Repository) ListAttachments(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
) ([]Attachment, error) {
	projectUUID, err :=
		uuid.Parse(projectID)

	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid project id",
			ErrInvalidID,
		)
	}

	taskUUID, err :=
		uuid.Parse(taskID)

	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid task id",
			ErrInvalidID,
		)
	}

	rows, err :=
		r.queries.ListTaskAttachments(
			ctx,
			db.ListTaskAttachmentsParams{
				TenantID: tenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectUUID,
					Valid: true,
				},

				TaskID: pgtype.UUID{
					Bytes: taskUUID,
					Valid: true,
				},
			},
		)

	if err != nil {
		return nil, fmt.Errorf(
			"list task attachments: %w",
			err,
		)
	}

	result := make(
		[]Attachment,
		0,
		len(rows),
	)

	for _, row := range rows {
		result = append(
			result,
			attachmentFromDB(row),
		)
	}

	return result, nil
}
func (r *Repository) GetAttachment(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	attachmentID string,
) (Attachment, error) {
	projectUUID, err :=
		uuid.Parse(projectID)

	if err != nil {
		return Attachment{}, ErrInvalidID
	}

	taskUUID, err :=
		uuid.Parse(taskID)

	if err != nil {
		return Attachment{}, ErrInvalidID
	}

	attachmentUUID, err :=
		uuid.Parse(attachmentID)

	if err != nil {
		return Attachment{}, ErrInvalidID
	}

	row, err :=
		r.queries.GetTaskAttachment(
			ctx,
			db.GetTaskAttachmentParams{
				TenantID: tenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectUUID,
					Valid: true,
				},

				TaskID: pgtype.UUID{
					Bytes: taskUUID,
					Valid: true,
				},

				ID: pgtype.UUID{
					Bytes: attachmentUUID,
					Valid: true,
				},
			},
		)

	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return Attachment{},
				ErrAttachmentNotFound
		}

		return Attachment{}, fmt.Errorf(
			"get attachment: %w",
			err,
		)
	}

	return attachmentFromDB(row), nil
}
func (r *Repository) DeleteAttachment(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	attachmentID string,
) error {
	projectUUID, err :=
		uuid.Parse(projectID)

	if err != nil {
		return ErrInvalidID
	}

	taskUUID, err :=
		uuid.Parse(taskID)

	if err != nil {
		return ErrInvalidID
	}

	attachmentUUID, err :=
		uuid.Parse(attachmentID)

	if err != nil {
		return ErrInvalidID
	}

	affected, err :=
		r.queries.DeleteTaskAttachment(
			ctx,
			db.DeleteTaskAttachmentParams{
				TenantID: tenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectUUID,
					Valid: true,
				},

				TaskID: pgtype.UUID{
					Bytes: taskUUID,
					Valid: true,
				},

				ID: pgtype.UUID{
					Bytes: attachmentUUID,
					Valid: true,
				},
			},
		)

	if err != nil {
		return fmt.Errorf(
			"delete attachment metadata: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrAttachmentNotFound
	}

	return nil
}
