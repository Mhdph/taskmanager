package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mhdph/taskmanager/internal/auth"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

// Label is a workspace-level label that can be attached to tasks from any
// project of its tenant.
type Label struct {
	ID    string
	Name  string
	Color string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func labelFromDB(row db.Label) Label {
	label := Label{
		Name:      row.Name,
		Color:     row.Color,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}

	if row.ID.Valid {
		label.ID = uuid.UUID(row.ID.Bytes).String()
	}

	return label
}

// -------------------------------------------------------------------------
// Repository: label directory + task attachments
// -------------------------------------------------------------------------

type CreateLabelParams struct {
	ID       string
	TenantID string
	Name     string
	Color    string
}

func (r *Repository) CreateLabel(
	ctx context.Context,
	params CreateLabelParams,
) (Label, error) {
	id, err := uuid.Parse(params.ID)
	if err != nil {
		return Label{}, fmt.Errorf(
			"parse label id: %w",
			err,
		)
	}

	row, err := r.queries.CreateLabel(
		ctx,
		db.CreateLabelParams{
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
			TenantID: params.TenantID,
			Name:     params.Name,
			Color:    params.Color,
		},
	)
	if err != nil {
		if isLabelConflict(err) {
			return Label{}, err
		}

		return Label{}, fmt.Errorf(
			"create label: %w",
			err,
		)
	}

	return labelFromDB(row), nil
}

func (r *Repository) GetLabel(
	ctx context.Context,
	tenantID string,
	labelID string,
) (Label, error) {
	id, err := uuid.Parse(labelID)
	if err != nil {
		return Label{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetLabel(
		ctx,
		db.GetLabelParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Label{}, ErrLabelNotFound
		}

		return Label{}, fmt.Errorf(
			"get label: %w",
			err,
		)
	}

	return labelFromDB(row), nil
}

func (r *Repository) ListLabels(
	ctx context.Context,
	tenantID string,
) ([]Label, error) {
	rows, err := r.queries.ListLabels(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf(
			"list labels: %w",
			err,
		)
	}

	labels := make(
		[]Label,
		0,
		len(rows),
	)

	for _, row := range rows {
		labels = append(
			labels,
			labelFromDB(row),
		)
	}

	return labels, nil
}

type UpdateLabelParams struct {
	TenantID string
	LabelID  string

	Name  *string
	Color *string
}

func (r *Repository) UpdateLabel(
	ctx context.Context,
	params UpdateLabelParams,
) (Label, error) {
	id, err := uuid.Parse(params.LabelID)
	if err != nil {
		return Label{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	var name pgtype.Text

	if params.Name != nil {
		name = pgtype.Text{
			String: *params.Name,
			Valid:  true,
		}
	}

	var color pgtype.Text

	if params.Color != nil {
		color = pgtype.Text{
			String: *params.Color,
			Valid:  true,
		}
	}

	row, err := r.queries.UpdateLabel(
		ctx,
		db.UpdateLabelParams{
			Name:  name,
			Color: color,

			TenantID: params.TenantID,

			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Label{}, ErrLabelNotFound
		}

		if isLabelConflict(err) {
			return Label{}, err
		}

		return Label{}, fmt.Errorf(
			"update label: %w",
			err,
		)
	}

	return labelFromDB(row), nil
}

func (r *Repository) DeleteLabel(
	ctx context.Context,
	tenantID string,
	labelID string,
) error {
	id, err := uuid.Parse(labelID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	affected, err := r.queries.DeleteLabel(
		ctx,
		db.DeleteLabelParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete label: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrLabelNotFound
	}

	return nil
}

func (r *Repository) ListTaskLabels(
	ctx context.Context,
	tenantID string,
	taskID string,
) ([]Label, error) {
	id, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	rows, err := r.queries.ListTaskLabels(
		ctx,
		db.ListTaskLabelsParams{
			TenantID: tenantID,
			TaskID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list task labels: %w",
			err,
		)
	}

	labels := make(
		[]Label,
		0,
		len(rows),
	)

	for _, row := range rows {
		labels = append(
			labels,
			labelFromDB(row),
		)
	}

	return labels, nil
}

// SetTaskLabels atomically replaces the label attachments of a task.
// Every label id must exist in the tenant; the task must exist in the
// given project.
func (r *Repository) SetTaskLabels(
	ctx context.Context,
	tenantID string,
	projectID string,
	taskID string,
	labelIDs []string,
) ([]Label, error) {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"begin task labels transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	// Confirm the task exists in this project and is not archived.
	if _, err := qtx.GetTaskForProject(
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
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}

		return nil, fmt.Errorf(
			"get task for labels: %w",
			err,
		)
	}

	if err := qtx.DeleteTaskLabels(
		ctx,
		db.DeleteTaskLabelsParams{
			TenantID: tenantID,
			TaskID: pgtype.UUID{
				Bytes: taskUUID,
				Valid: true,
			},
		},
	); err != nil {
		return nil, fmt.Errorf(
			"clear task labels: %w",
			err,
		)
	}

	attached := make([]Label, 0, len(labelIDs))

	for _, labelID := range labelIDs {
		id, err := uuid.Parse(labelID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		row, err := qtx.GetLabel(
			ctx,
			db.GetLabelParams{
				TenantID: tenantID,
				ID: pgtype.UUID{
					Bytes: id,
					Valid: true,
				},
			},
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrLabelNotFound
			}

			return nil, fmt.Errorf(
				"get label for task: %w",
				err,
			)
		}

		if _, err := qtx.AddTaskLabel(
			ctx,
			db.AddTaskLabelParams{
				TenantID: tenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectUUID,
					Valid: true,
				},

				TaskID: pgtype.UUID{
					Bytes: taskUUID,
					Valid: true,
				},

				LabelID: pgtype.UUID{
					Bytes: id,
					Valid: true,
				},
			},
		); err != nil {
			return nil, fmt.Errorf(
				"add task label: %w",
				err,
			)
		}

		attached = append(
			attached,
			labelFromDB(row),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit task labels transaction: %w",
			err,
		)
	}

	return attached, nil
}

// -------------------------------------------------------------------------
// Service
// -------------------------------------------------------------------------

const labelMaxTasksPerSet = 50
const labelMaxNameLength = 64
const labelMaxColorLength = 16

func normalizeLabelName(value string) (string, error) {
	name := strings.TrimSpace(value)

	if name == "" {
		return "", ErrLabelNameRequired
	}

	if len([]rune(name)) > labelMaxNameLength {
		return "", ErrLabelNameRequired
	}

	return name, nil
}

func normalizeLabelColor(value string) (string, error) {
	color := strings.TrimSpace(value)

	if color == "" {
		return "", nil
	}

	if len(color) > labelMaxColorLength {
		return "", ErrLabelInvalidColor
	}

	return color, nil
}

func (s *Service) ListLabels(
	ctx context.Context,
	identity auth.Identity,
) ([]Label, error) {
	return s.repository.ListLabels(
		ctx,
		identity.TenantID,
	)
}

type CreateLabelInput struct {
	Identity auth.Identity

	Name  string
	Color string
}

func (s *Service) CreateLabel(
	ctx context.Context,
	input CreateLabelInput,
) (Label, error) {
	if !s.canManageLabels(input.Identity) {
		return Label{}, ErrForbidden
	}

	name, err := normalizeLabelName(input.Name)
	if err != nil {
		return Label{}, err
	}

	color, err := normalizeLabelColor(input.Color)
	if err != nil {
		return Label{}, err
	}

	label, err := s.repository.CreateLabel(
		ctx,
		CreateLabelParams{
			ID:       uuid.NewString(),
			TenantID: input.Identity.TenantID,
			Name:     name,
			Color:    color,
		},
	)
	if err != nil {
		return Label{}, fmt.Errorf(
			"create label: %w",
			err,
		)
	}

	return label, nil
}

type UpdateLabelInput struct {
	Identity auth.Identity
	LabelID  string

	Name  *string
	Color *string
}

func (s *Service) UpdateLabel(
	ctx context.Context,
	input UpdateLabelInput,
) (Label, error) {
	if !s.canManageLabels(input.Identity) {
		return Label{}, ErrForbidden
	}

	params := UpdateLabelParams{
		TenantID: input.Identity.TenantID,
		LabelID:  input.LabelID,
	}

	if input.Name != nil {
		name, err := normalizeLabelName(*input.Name)
		if err != nil {
			return Label{}, err
		}

		params.Name = &name
	}

	if input.Color != nil {
		color, err := normalizeLabelColor(*input.Color)
		if err != nil {
			return Label{}, err
		}

		params.Color = &color
	}

	if params.Name == nil && params.Color == nil {
		return Label{}, ErrNoChanges
	}

	label, err := s.repository.UpdateLabel(
		ctx,
		params,
	)
	if err != nil {
		return Label{}, fmt.Errorf(
			"update label: %w",
			err,
		)
	}

	return label, nil
}

func (s *Service) DeleteLabel(
	ctx context.Context,
	identity auth.Identity,
	labelID string,
) error {
	if !s.canManageLabels(identity) {
		return ErrForbidden
	}

	return s.repository.DeleteLabel(
		ctx,
		identity.TenantID,
		labelID,
	)
}

// canManageLabels: hosted admins/support and workspace owners can manage the
// label directory. Everybody can list labels and read attachments.
func (s *Service) canManageLabels(identity auth.Identity) bool {
	if identity.IsSupport {
		return true
	}

	return identity.IsAdmin()
}

func (s *Service) GetTaskLabels(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
) ([]Label, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewTasks,
	); err != nil {
		return nil, fmt.Errorf(
			"authorize task labels: %w",
			err,
		)
	}

	labels, err := s.repository.ListTaskLabels(
		ctx,
		identity.TenantID,
		taskID,
	)
	if err != nil {
		return nil, err
	}

	return labels, nil
}

type SetTaskLabelsInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	LabelIDs []string
}

func (s *Service) SetTaskLabels(
	ctx context.Context,
	input SetTaskLabelsInput,
) ([]Label, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	); err != nil {
		return nil, fmt.Errorf(
			"authorize task labels: %w",
			err,
		)
	}

	if len(input.LabelIDs) > labelMaxTasksPerSet {
		return nil, fmt.Errorf(
			"a task cannot carry more than %d labels",
			labelMaxTasksPerSet,
		)
	}

	labels, err := s.repository.SetTaskLabels(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		input.LabelIDs,
	)
	if err != nil {
		return nil, err
	}

	return labels, nil
}

// -------------------------------------------------------------------------
// Reorder
// -------------------------------------------------------------------------

type ReorderTask struct {
	TaskID   string
	Position float64
}

type ReorderInput struct {
	Identity auth.Identity

	ProjectID string

	Tasks []ReorderTask
}

const reorderMaxTasks = 200

func (s *Service) Reorder(
	ctx context.Context,
	input ReorderInput,
) error {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	); err != nil {
		return fmt.Errorf(
			"authorize task reorder: %w",
			err,
		)
	}

	if len(input.Tasks) == 0 {
		return ErrNoChanges
	}

	if len(input.Tasks) > reorderMaxTasks {
		return fmt.Errorf(
			"cannot reorder more than %d tasks at once",
			reorderMaxTasks,
		)
	}

	return s.repository.Reorder(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.Tasks,
	)
}

// Reorder applies explicit fractional positions to a set of tasks inside a
// transaction guarded by a project lock.
func (r *Repository) Reorder(
	ctx context.Context,
	tenantID string,
	projectID string,
	tasks []ReorderTask,
) error {
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin task reorder transaction: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if _, err := qtx.LockProjectForTaskReorder(
		ctx,
		db.LockProjectForTaskReorderParams{
			TenantID: tenantID,

			ID: pgtype.UUID{
				Bytes: projectUUID,
				Valid: true,
			},
		},
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}

		return fmt.Errorf(
			"lock project for task reorder: %w",
			err,
		)
	}

	for _, item := range tasks {
		taskUUID, err := uuid.Parse(item.TaskID)
		if err != nil {
			return fmt.Errorf(
				"%w: %v",
				ErrInvalidID,
				err,
			)
		}

		affected, err := qtx.UpdateTaskPosition(
			ctx,
			db.UpdateTaskPositionParams{
				TenantID: tenantID,

				ProjectID: pgtype.UUID{
					Bytes: projectUUID,
					Valid: true,
				},

				ID: pgtype.UUID{
					Bytes: taskUUID,
					Valid: true,
				},

				Position: pgtype.Float8{
					Float64: item.Position,
					Valid:   true,
				},
			},
		)
		if err != nil {
			return fmt.Errorf(
				"update task position: %w",
				err,
			)
		}

		if affected == 0 {
			return ErrTaskNotFound
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit task reorder transaction: %w",
			err,
		)
	}

	return nil
}
