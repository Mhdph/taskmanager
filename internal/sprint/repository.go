package sprint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

// Repository owns sprint persistence. Every operation that changes more
// than one row (assignment, start, completion, deletion) runs in a single
// transaction with row locks, so concurrent callers cannot produce
// duplicate carryover or inconsistent snapshots.
type Repository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewRepository(
	pool *pgxpool.Pool,
	queries *db.Queries,
) *Repository {
	return &Repository{
		pool:    pool,
		queries: queries,
	}
}

// -------------------------------------------------------------------------
// Mapping helpers
// -------------------------------------------------------------------------

func parseUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func uuidValue(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}

	return uuid.UUID(value.Bytes).String()
}

func sprintFromDB(row db.Sprint) Sprint {
	sprint := Sprint{
		ID:              uuidValue(row.ID),
		TenantID:        row.TenantID,
		ProjectID:       uuidValue(row.ProjectID),
		Name:            row.Name,
		State:           State(row.State),
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}

	if row.Goal.Valid {
		value := row.Goal.String
		sprint.Goal = &value
	}

	if row.PlannedStartAt.Valid {
		value := row.PlannedStartAt.Time
		sprint.PlannedStartAt = &value
	}

	sprint.PlannedEndAt = row.PlannedEndAt.Time

	if row.StartedAt.Valid {
		value := row.StartedAt.Time
		sprint.StartedAt = &value
	}

	if row.CompletedAt.Valid {
		value := row.CompletedAt.Time
		sprint.CompletedAt = &value
	}

	return sprint
}

func sprintTaskFromDB(row db.Task) SprintTask {
	task := SprintTask{
		ID:        uuidValue(row.ID),
		TenantID:  row.TenantID,
		ProjectID: uuidValue(row.ProjectID),
		Title:     row.Title,
		Archived:  row.ArchivedAt.Valid,
		Completed: row.CompletedAt.Valid,
		CreatedAt: row.CreatedAt.Time,
	}

	if row.EstimateSeconds.Valid {
		value := row.EstimateSeconds.Int64
		task.EstimateSeconds = &value
	}

	if row.AssigneeUserID.Valid {
		value := row.AssigneeUserID.String
		task.AssigneeUserID = &value
	}

	if row.SprintID.Valid {
		value := uuidValue(row.SprintID)
		task.SprintID = &value
	}

	return task
}

// -------------------------------------------------------------------------
// Sprints
// -------------------------------------------------------------------------

type CreateParams struct {
	ID        string
	TenantID  string
	ProjectID string

	Name string
	Goal *string

	PlannedStartAt *time.Time
	PlannedEndAt   time.Time

	CreatedByUserID string
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Sprint, error) {
	id, err := parseUUID(params.ID)
	if err != nil {
		return Sprint{}, err
	}

	projectID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Sprint{}, err
	}

	row, err := r.queries.CreateSprint(
		ctx,
		db.CreateSprintParams{
			ID:              id,
			TenantID:        params.TenantID,
			ProjectID:       projectID,
			Name:            params.Name,
			Goal:            textValue(params.Goal),
			PlannedStartAt:  timestampValue(params.PlannedStartAt),
			PlannedEndAt:    pgtype.Timestamptz{Time: params.PlannedEndAt, Valid: true},
			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Sprint{}, fmt.Errorf("create sprint: %w", err)
	}

	return sprintFromDB(row), nil
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	projectID string,
	sprintID string,
) (Sprint, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return Sprint{}, err
	}

	id, err := parseUUID(sprintID)
	if err != nil {
		return Sprint{}, err
	}

	row, err := r.queries.GetSprint(
		ctx,
		db.GetSprintParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sprint{}, ErrSprintNotFound
	}

	if err != nil {
		return Sprint{}, fmt.Errorf("get sprint: %w", err)
	}

	return sprintFromDB(row), nil
}

func (r *Repository) List(
	ctx context.Context,
	tenantID string,
	projectID string,
	options ListOptions,
) ([]Sprint, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}

	var state pgtype.Text
	if options.State != nil {
		state = pgtype.Text{
			String: string(*options.State),
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

		value, err := parseUUID(options.Cursor.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid cursor id",
				ErrInvalidCursor,
			)
		}

		cursorID = value
	}

	rows, err := r.queries.ListSprintsForProject(
		ctx,
		db.ListSprintsForProjectParams{
			TenantID:        tenantID,
			ProjectID:       projectUUID,
			State:           state,
			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,
			PageLimit:       options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list sprints: %w", err)
	}

	items := make([]Sprint, 0, len(rows))
	for _, row := range rows {
		items = append(items, sprintFromDB(row))
	}

	return items, nil
}

type UpdateParams struct {
	TenantID  string
	ProjectID string
	SprintID  string

	Name *string

	// Goal is replaced when GoalIsSet is true, so a client can clear it.
	Goal      *string
	GoalIsSet bool

	PlannedStartAt    *time.Time
	PlannedStartIsSet bool

	PlannedEndAt *time.Time
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (Sprint, error) {
	projectUUID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Sprint{}, err
	}

	id, err := parseUUID(params.SprintID)
	if err != nil {
		return Sprint{}, err
	}

	row, err := r.queries.UpdateSprint(
		ctx,
		db.UpdateSprintParams{
			Name:              textValue(params.Name),
			Goal:              textValue(params.Goal),
			GoalIsSet:         params.GoalIsSet,
			PlannedStartAt:    timestampValue(params.PlannedStartAt),
			PlannedStartIsSet: params.PlannedStartIsSet,
			PlannedEndAt:      timestampValue(params.PlannedEndAt),
			TenantID:          params.TenantID,
			ProjectID:         projectUUID,
			ID:                id,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sprint{}, ErrSprintNotFound
	}

	if err != nil {
		return Sprint{}, fmt.Errorf("update sprint: %w", err)
	}

	return sprintFromDB(row), nil
}

// Delete removes a planned sprint and returns its tasks to the backlog.
// Active and completed sprints are refused.
func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	projectID string,
	sprintID string,
) error {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return err
	}

	id, err := parseUUID(sprintID)
	if err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sprint deletion: %w", err)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.LockSprint(
		ctx,
		db.LockSprintParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSprintNotFound
	}

	if err != nil {
		return fmt.Errorf("lock sprint: %w", err)
	}

	if State(row.State) != StatePlanned {
		return ErrLifecycleConflict
	}

	tasks, err := r.listSprintTasks(ctx, qtx, tenantID, projectUUID, id)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if _, err := r.detachTask(
			ctx,
			qtx,
			tenantID,
			projectUUID,
			task.ID,
			sprintID,
			"",
		); err != nil {
			return err
		}
	}

	if _, err := qtx.DeleteSprint(
		ctx,
		db.DeleteSprintParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	); err != nil {
		return fmt.Errorf("delete sprint: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sprint deletion: %w", err)
	}

	return nil
}

// Start moves a planned sprint to active and snapshots the tasks it
// committed to. Starting an already active sprint is a no-op.
func (r *Repository) Start(
	ctx context.Context,
	tenantID string,
	projectID string,
	sprintID string,
) (Sprint, bool, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return Sprint{}, false, err
	}

	id, err := parseUUID(sprintID)
	if err != nil {
		return Sprint{}, false, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Sprint{}, false, fmt.Errorf(
			"begin sprint start: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.LockSprint(
		ctx,
		db.LockSprintParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sprint{}, false, ErrSprintNotFound
	}

	if err != nil {
		return Sprint{}, false, fmt.Errorf("lock sprint: %w", err)
	}

	switch State(row.State) {
	case StateActive:
		// Repeating the start returns the existing sprint without
		// touching the commitment snapshot.
		if err := tx.Commit(ctx); err != nil {
			return Sprint{}, false, fmt.Errorf(
				"commit sprint start: %w",
				err,
			)
		}

		return sprintFromDB(row), false, nil

	case StateCompleted:
		return Sprint{}, false, ErrLifecycleConflict
	}

	// A project has at most one active sprint.
	active, err := qtx.CountActiveSprintsForProject(
		ctx,
		db.CountActiveSprintsForProjectParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
		},
	)
	if err != nil {
		return Sprint{}, false, fmt.Errorf(
			"count active sprints: %w",
			err,
		)
	}

	if active > 0 {
		return Sprint{}, false, ErrActiveSprintExists
	}

	tasks, err := r.listSprintTasks(ctx, qtx, tenantID, projectUUID, id)
	if err != nil {
		return Sprint{}, false, err
	}

	for _, task := range tasks {
		if err := qtx.InsertSprintCommitment(
			ctx,
			db.InsertSprintCommitmentParams{
				SprintID:        id,
				TenantID:        tenantID,
				TaskID:          mustUUID(task.ID),
				Title:           task.Title,
				EstimateSeconds: int8Value(task.EstimateSeconds),
				Archived:        task.Archived,
			},
		); err != nil {
			return Sprint{}, false, fmt.Errorf(
				"insert sprint commitment: %w",
				err,
			)
		}
	}

	started, err := qtx.StartSprint(
		ctx,
		db.StartSprintParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			return Sprint{}, false, ErrActiveSprintExists
		}

		return Sprint{}, false, fmt.Errorf("start sprint: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Sprint{}, false, fmt.Errorf(
			"commit sprint start: %w",
			err,
		)
	}

	return sprintFromDB(started), true, nil
}

type CompleteParams struct {
	TenantID  string
	ProjectID string
	SprintID  string

	// CarryoverSprintID is the destination of the unfinished work. Nil
	// means the backlog.
	CarryoverSprintID *string

	CompletedByUserID string
}

// Complete freezes the report and moves the unfinished work to the
// carryover destination. Completing an already completed sprint returns
// the frozen report without carrying anything over twice.
func (r *Repository) Complete(
	ctx context.Context,
	params CompleteParams,
) (Sprint, Report, bool, error) {
	projectUUID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Sprint{}, Report{}, false, err
	}

	id, err := parseUUID(params.SprintID)
	if err != nil {
		return Sprint{}, Report{}, false, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf(
			"begin sprint completion: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	row, err := qtx.LockSprint(
		ctx,
		db.LockSprintParams{
			TenantID:  params.TenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sprint{}, Report{}, false, ErrSprintNotFound
	}

	if err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf("lock sprint: %w", err)
	}

	if State(row.State) == StateCompleted {
		report, err := r.loadFrozenReport(
			ctx,
			qtx,
			params.TenantID,
			id,
		)
		if err != nil {
			return Sprint{}, Report{}, false, err
		}

		if err := tx.Commit(ctx); err != nil {
			return Sprint{}, Report{}, false, fmt.Errorf(
				"commit sprint completion: %w",
				err,
			)
		}

		return sprintFromDB(row), report, false, nil
	}

	if State(row.State) != StateActive {
		return Sprint{}, Report{}, false, ErrLifecycleConflict
	}

	// The carryover destination must be a planned sprint of the same
	// project.
	var carryoverID *pgtype.UUID

	if params.CarryoverSprintID != nil {
		parsed, err := parseUUID(*params.CarryoverSprintID)
		if err != nil {
			return Sprint{}, Report{}, false, ErrCarryoverTargetInvalid
		}

		if parsed.Bytes == id.Bytes {
			return Sprint{}, Report{}, false, ErrCarryoverTargetInvalid
		}

		target, err := qtx.LockSprint(
			ctx,
			db.LockSprintParams{
				TenantID:  params.TenantID,
				ProjectID: projectUUID,
				ID:        parsed,
			},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return Sprint{}, Report{}, false, ErrCarryoverTargetInvalid
		}

		if err != nil {
			return Sprint{}, Report{}, false, fmt.Errorf(
				"lock carryover sprint: %w",
				err,
			)
		}

		if State(target.State) != StatePlanned {
			return Sprint{}, Report{}, false, ErrCarryoverTargetInvalid
		}

		carryoverID = &parsed
	}

	members, err := r.listSprintTasks(
		ctx,
		qtx,
		params.TenantID,
		projectUUID,
		id,
	)
	if err != nil {
		return Sprint{}, Report{}, false, err
	}

	commitments, err := r.listCommitments(ctx, qtx, params.TenantID, id)
	if err != nil {
		return Sprint{}, Report{}, false, err
	}

	report := buildReport(
		sprintFromDB(row),
		members,
		commitments,
		row.StartedAt.Valid,
		carryoverID,
	)

	// The report is frozen from this moment on: later edits to the tasks
	// cannot change it.
	report.State = StateCompleted
	report.Frozen = true

	payload, err := json.Marshal(reportTasksPayload(report.Tasks))
	if err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf(
			"encode sprint report: %w",
			err,
		)
	}

	if _, err := qtx.InsertSprintReport(
		ctx,
		db.InsertSprintReportParams{
			SprintID:                   id,
			TenantID:                   params.TenantID,
			ProjectID:                  projectUUID,
			CompletedByUserID:          params.CompletedByUserID,
			CommitmentTaskCount:        int32(report.CommitmentTaskCount),
			CommitmentEstimatedSeconds: report.CommitmentEstimatedSeconds,
			CommitmentUnestimatedCount: int32(report.CommitmentUnestimatedCount),
			AddedTaskCount:             int32(report.AddedTaskCount),
			AddedEstimatedSeconds:      report.AddedEstimatedSeconds,
			RemovedTaskCount:           int32(report.RemovedTaskCount),
			RemovedEstimatedSeconds:    report.RemovedEstimatedSeconds,
			CompletedTaskCount:         int32(report.CompletedTaskCount),
			CompletedEstimatedSeconds:  report.CompletedEstimatedSeconds,
			CarryoverTaskCount:         int32(report.CarryoverTaskCount),
			CarryoverEstimatedSeconds:  report.CarryoverEstimatedSeconds,
			ArchivedTaskCount:          int32(report.ArchivedTaskCount),
			UnestimatedTaskCount:       int32(report.UnestimatedTaskCount),
			Tasks:                      payload,
		},
	); err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf(
			"insert sprint report: %w",
			err,
		)
	}

	completed, err := qtx.CompleteSprint(
		ctx,
		db.CompleteSprintParams{
			TenantID:  params.TenantID,
			ProjectID: projectUUID,
			ID:        id,
		},
	)
	if err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf(
			"complete sprint: %w",
			err,
		)
	}

	// The carryover destination as a plain id: nil means the backlog.
	var carryoverSprint *string
	if carryoverID != nil {
		carryoverSprint = ptrString(uuidValue(*carryoverID))
	}

	// Unfinished, non-archived work moves on. Finished and archived tasks
	// keep the completed sprint for traceability.
	for _, task := range members {
		if task.Completed || task.Archived {
			continue
		}

		// The move is conditional: a task that was moved to another
		// sprint while the completion ran stays where it is.
		moved, err := qtx.MoveTaskOutOfSprint(
			ctx,
			db.MoveTaskOutOfSprintParams{
				SprintID:     uuidValuePtr(carryoverSprint),
				TenantID:     params.TenantID,
				ProjectID:    projectUUID,
				ID:           mustUUID(task.ID),
				FromSprintID: id,
			},
		)
		if err != nil {
			return Sprint{}, Report{}, false, fmt.Errorf(
				"move task out of sprint: %w",
				err,
			)
		}

		if moved == 0 {
			continue
		}

		if err := r.recordMembership(
			ctx,
			qtx,
			params.TenantID,
			projectUUID,
			params.SprintID,
			task.ID,
			params.CompletedByUserID,
			"removed",
		); err != nil {
			return Sprint{}, Report{}, false, err
		}

		if carryoverID != nil {
			if err := r.recordMembership(
				ctx,
				qtx,
				params.TenantID,
				projectUUID,
				uuidValue(*carryoverID),
				task.ID,
				params.CompletedByUserID,
				"added",
			); err != nil {
				return Sprint{}, Report{}, false, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Sprint{}, Report{}, false, fmt.Errorf(
			"commit sprint completion: %w",
			err,
		)
	}

	return sprintFromDB(completed), report, true, nil
}

// GetReport returns the frozen report of a completed sprint, or the live
// summary of a planned or active sprint.
func (r *Repository) GetReport(
	ctx context.Context,
	tenantID string,
	projectID string,
	sprintID string,
) (Report, error) {
	sprint, err := r.Get(ctx, tenantID, projectID, sprintID)
	if err != nil {
		return Report{}, err
	}

	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return Report{}, err
	}

	id, err := parseUUID(sprintID)
	if err != nil {
		return Report{}, err
	}

	if sprint.State == StateCompleted {
		return r.loadFrozenReport(ctx, r.queries, tenantID, id)
	}

	members, err := r.listSprintTasks(
		ctx,
		r.queries,
		tenantID,
		projectUUID,
		id,
	)
	if err != nil {
		return Report{}, err
	}

	commitments, err := r.listCommitments(
		ctx,
		r.queries,
		tenantID,
		id,
	)
	if err != nil {
		return Report{}, err
	}

	return buildReport(
		sprint,
		members,
		commitments,
		sprint.StartedAt != nil,
		nil,
	), nil
}

// Totals returns the live numbers of a sprint.
func (r *Repository) Totals(
	ctx context.Context,
	tenantID string,
	projectID string,
	sprintID string,
) (Totals, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return Totals{}, err
	}

	id, err := parseUUID(sprintID)
	if err != nil {
		return Totals{}, err
	}

	tasks, err := r.listSprintTasks(
		ctx,
		r.queries,
		tenantID,
		projectUUID,
		id,
	)
	if err != nil {
		return Totals{}, err
	}

	totals := Totals{}

	for _, task := range tasks {
		if task.Archived {
			continue
		}

		totals.TaskCount++

		if task.EstimateSeconds != nil {
			totals.EstimatedSeconds += *task.EstimateSeconds
		} else {
			totals.UnestimatedTaskCount++
		}
	}

	return totals, nil
}

// -------------------------------------------------------------------------
// Task assignment
// -------------------------------------------------------------------------

type AssignParams struct {
	TenantID  string
	ProjectID string
	TaskID    string

	// SprintID is the destination. Nil returns the task to the backlog.
	SprintID *string

	ActorUserID string
}

// AssignTask moves a task into a sprint, into another sprint, or back to
// the backlog. The task and the destination sprint are locked, so a move
// cannot race a sprint completion.
func (r *Repository) AssignTask(
	ctx context.Context,
	params AssignParams,
) (SprintTask, error) {
	projectUUID, err := parseUUID(params.ProjectID)
	if err != nil {
		return SprintTask{}, err
	}

	taskID, err := parseUUID(params.TaskID)
	if err != nil {
		return SprintTask{}, err
	}

	// The current assignment is read without a lock: the sprints have to
	// be locked before the task, in the same order the lifecycle
	// transitions use, so a move cannot deadlock a completion.
	current, err := r.getSprintTask(
		ctx,
		r.queries,
		params.TenantID,
		projectUUID,
		taskID,
	)
	if err != nil {
		return SprintTask{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SprintTask{}, fmt.Errorf(
			"begin task assignment: %w",
			err,
		)
	}

	defer tx.Rollback(ctx)

	qtx := r.queries.WithTx(tx)

	if params.SprintID != nil {
		target, err := parseUUID(*params.SprintID)
		if err != nil {
			return SprintTask{}, ErrInvalidID
		}

		// The destination must be a planned or active sprint of the same
		// project.
		sprintRow, err := qtx.LockSprint(
			ctx,
			db.LockSprintParams{
				TenantID:  params.TenantID,
				ProjectID: projectUUID,
				ID:        target,
			},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return SprintTask{}, ErrSprintNotFound
		}

		if err != nil {
			return SprintTask{}, fmt.Errorf(
				"lock target sprint: %w",
				err,
			)
		}

		state := State(sprintRow.State)
		if state != StatePlanned && state != StateActive {
			return SprintTask{}, ErrLifecycleConflict
		}
	}

	// Lock the sprint the task is leaving, if it is a different one.
	if current.SprintID != nil &&
		(params.SprintID == nil ||
			*current.SprintID != *params.SprintID) {

		source, err := parseUUID(*current.SprintID)
		if err == nil {
			// A sprint that disappeared in the meantime cannot hold
			// the task any more.
			_, _ = qtx.LockSprint(
				ctx,
				db.LockSprintParams{
					TenantID:  params.TenantID,
					ProjectID: projectUUID,
					ID:        source,
				},
			)
		}
	}

	row, err := qtx.LockSprintTask(
		ctx,
		db.LockSprintTaskParams{
			TenantID:  params.TenantID,
			ProjectID: projectUUID,
			ID:        taskID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SprintTask{}, ErrTaskNotFound
	}

	if err != nil {
		return SprintTask{}, fmt.Errorf("lock task: %w", err)
	}

	task := sprintTaskFromDB(row)

	if task.Archived {
		return SprintTask{}, ErrTaskNotEligible
	}

	if task.Completed {
		return SprintTask{}, ErrTaskNotEligible
	}

	previousSprintID := task.SprintID

	// Same sprint: nothing changes.
	if sameSprint(previousSprintID, params.SprintID) {
		if err := tx.Commit(ctx); err != nil {
			return SprintTask{}, fmt.Errorf(
				"commit task assignment: %w",
				err,
			)
		}

		return task, nil
	}

	updated := row

	if previousSprintID != nil {
		detached, err := r.detachTask(
			ctx,
			qtx,
			params.TenantID,
			projectUUID,
			task.ID,
			*previousSprintID,
			params.ActorUserID,
		)
		if err != nil {
			return SprintTask{}, err
		}

		updated = detached
	}

	if params.SprintID != nil {
		target, err := parseUUID(*params.SprintID)
		if err != nil {
			return SprintTask{}, ErrInvalidID
		}

		attached, err := r.attachTask(
			ctx,
			qtx,
			params.TenantID,
			projectUUID,
			task.ID,
			target,
			params.ActorUserID,
		)
		if err != nil {
			return SprintTask{}, err
		}

		updated = attached
	}

	if err := tx.Commit(ctx); err != nil {
		return SprintTask{}, fmt.Errorf(
			"commit task assignment: %w",
			err,
		)
	}

	return sprintTaskFromDB(updated), nil
}

func sameSprint(a *string, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}

// -------------------------------------------------------------------------
// Internal helpers
// -------------------------------------------------------------------------

type queryable interface {
	ListSprintTasks(
		ctx context.Context,
		arg db.ListSprintTasksParams,
	) ([]db.Task, error)

	LockSprintTask(
		ctx context.Context,
		arg db.LockSprintTaskParams,
	) (db.Task, error)

	SetTaskSprint(
		ctx context.Context,
		arg db.SetTaskSprintParams,
	) (db.Task, error)

	RecordSprintMembership(
		ctx context.Context,
		arg db.RecordSprintMembershipParams,
	) error

	InsertSprintCommitment(
		ctx context.Context,
		arg db.InsertSprintCommitmentParams,
	) error

	ListSprintCommitments(
		ctx context.Context,
		arg db.ListSprintCommitmentsParams,
	) ([]db.SprintCommitment, error)

	InsertSprintReport(
		ctx context.Context,
		arg db.InsertSprintReportParams,
	) (db.SprintReport, error)

	GetSprintReport(
		ctx context.Context,
		arg db.GetSprintReportParams,
	) (db.SprintReport, error)
}

// getSprintTask reads a task without locking it.
func (r *Repository) getSprintTask(
	ctx context.Context,
	q *db.Queries,
	tenantID string,
	projectID pgtype.UUID,
	taskID pgtype.UUID,
) (SprintTask, error) {
	row, err := q.GetSprintTask(
		ctx,
		db.GetSprintTaskParams{
			TenantID:  tenantID,
			ProjectID: projectID,
			ID:        taskID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SprintTask{}, ErrTaskNotFound
	}

	if err != nil {
		return SprintTask{}, fmt.Errorf("get task: %w", err)
	}

	return sprintTaskFromDB(row), nil
}

func (r *Repository) listSprintTasks(
	ctx context.Context,
	q queryable,
	tenantID string,
	projectID pgtype.UUID,
	sprintID pgtype.UUID,
) ([]SprintTask, error) {
	rows, err := q.ListSprintTasks(
		ctx,
		db.ListSprintTasksParams{
			TenantID:  tenantID,
			ProjectID: projectID,
			SprintID:  sprintID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list sprint tasks: %w", err)
	}

	tasks := make([]SprintTask, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, sprintTaskFromDB(row))
	}

	return tasks, nil
}

func (r *Repository) listCommitments(
	ctx context.Context,
	q queryable,
	tenantID string,
	sprintID pgtype.UUID,
) ([]db.SprintCommitment, error) {
	rows, err := q.ListSprintCommitments(
		ctx,
		db.ListSprintCommitmentsParams{
			TenantID: tenantID,
			SprintID: sprintID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list sprint commitments: %w", err)
	}

	return rows, nil
}

// detachTask returns a task to the backlog and records the removal from
// the sprint it leaves.
func (r *Repository) detachTask(
	ctx context.Context,
	q queryable,
	tenantID string,
	projectID pgtype.UUID,
	taskID string,
	fromSprintID string,
	actorUserID string,
) (db.Task, error) {
	id, err := parseUUID(taskID)
	if err != nil {
		return db.Task{}, err
	}

	updated, err := q.SetTaskSprint(
		ctx,
		db.SetTaskSprintParams{
			SprintID:  pgtype.UUID{},
			TenantID:  tenantID,
			ProjectID: projectID,
			ID:        id,
		},
	)
	if err != nil {
		return db.Task{}, fmt.Errorf("clear task sprint: %w", err)
	}

	if err := r.recordMembership(
		ctx,
		q,
		tenantID,
		projectID,
		fromSprintID,
		taskID,
		actorUserID,
		"removed",
	); err != nil {
		return db.Task{}, err
	}

	return updated, nil
}

func (r *Repository) attachTask(
	ctx context.Context,
	q queryable,
	tenantID string,
	projectID pgtype.UUID,
	taskID string,
	sprintID pgtype.UUID,
	actorUserID string,
) (db.Task, error) {
	id, err := parseUUID(taskID)
	if err != nil {
		return db.Task{}, err
	}

	updated, err := q.SetTaskSprint(
		ctx,
		db.SetTaskSprintParams{
			SprintID:  sprintID,
			TenantID:  tenantID,
			ProjectID: projectID,
			ID:        id,
		},
	)
	if err != nil {
		return db.Task{}, fmt.Errorf("assign task sprint: %w", err)
	}

	if err := r.recordMembership(
		ctx,
		q,
		tenantID,
		projectID,
		uuidValue(sprintID),
		taskID,
		actorUserID,
		"added",
	); err != nil {
		return db.Task{}, err
	}

	return updated, nil
}

func (r *Repository) recordMembership(
	ctx context.Context,
	q queryable,
	tenantID string,
	projectID pgtype.UUID,
	sprintID string,
	taskID string,
	actorUserID string,
	action string,
) error {
	if sprintID == "" {
		// A task that was never in a sprint has no membership history.
		return nil
	}

	if err := q.RecordSprintMembership(
		ctx,
		db.RecordSprintMembershipParams{
			ID:          mustUUID(uuid.NewString()),
			TenantID:    tenantID,
			ProjectID:   projectID,
			SprintID:    mustUUID(sprintID),
			TaskID:      mustUUID(taskID),
			ActorUserID: actorUserID,
			Action:      action,
		},
	); err != nil {
		return fmt.Errorf("record sprint membership: %w", err)
	}

	return nil
}

func (r *Repository) loadFrozenReport(
	ctx context.Context,
	q queryable,
	tenantID string,
	sprintID pgtype.UUID,
) (Report, error) {
	row, err := q.GetSprintReport(
		ctx,
		db.GetSprintReportParams{
			TenantID: tenantID,
			SprintID: sprintID,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrSprintNotFound
	}

	if err != nil {
		return Report{}, fmt.Errorf("get sprint report: %w", err)
	}

	report := Report{
		SprintID:                   uuidValue(row.SprintID),
		State:                      StateCompleted,
		Frozen:                     true,
		CommitmentTaskCount:        int(row.CommitmentTaskCount),
		CommitmentEstimatedSeconds: row.CommitmentEstimatedSeconds,
		CommitmentUnestimatedCount: int(row.CommitmentUnestimatedCount),
		AddedTaskCount:             int(row.AddedTaskCount),
		AddedEstimatedSeconds:      row.AddedEstimatedSeconds,
		RemovedTaskCount:           int(row.RemovedTaskCount),
		RemovedEstimatedSeconds:    row.RemovedEstimatedSeconds,
		CompletedTaskCount:         int(row.CompletedTaskCount),
		CompletedEstimatedSeconds:  row.CompletedEstimatedSeconds,
		CarryoverTaskCount:         int(row.CarryoverTaskCount),
		CarryoverEstimatedSeconds:  row.CarryoverEstimatedSeconds,
		ArchivedTaskCount:          int(row.ArchivedTaskCount),
		UnestimatedTaskCount:       int(row.UnestimatedTaskCount),
		Tasks:                      []ReportTask{},
	}

	completedAt := row.CompletedAt.Time
	report.CompletedAt = &completedAt

	if len(row.Tasks) > 0 {
		var tasks []ReportTask
		if err := json.Unmarshal(row.Tasks, &tasks); err != nil {
			return Report{}, fmt.Errorf(
				"decode sprint report tasks: %w",
				err,
			)
		}

		report.Tasks = tasks
	}

	return report, nil
}

// reportTaskPayload is the frozen JSON shape of a report task.
type reportTaskPayload struct {
	TaskID string `json:"taskId"`

	Title string `json:"title"`

	EstimateSeconds *int64 `json:"estimateSeconds"`

	Archived  bool `json:"archived"`
	Completed bool `json:"completed"`

	CarriedOver bool `json:"carriedOver"`

	CarriedOverToSprintID *string `json:"carriedOverToSprintId,omitempty"`
}

func reportTasksPayload(tasks []ReportTask) []reportTaskPayload {
	payload := make([]reportTaskPayload, 0, len(tasks))

	for _, task := range tasks {
		payload = append(payload, reportTaskPayload{
			TaskID:                task.TaskID,
			Title:                 task.Title,
			EstimateSeconds:       task.EstimateSeconds,
			Archived:              task.Archived,
			Completed:             task.Completed,
			CarriedOver:           task.CarriedOver,
			CarriedOverToSprintID: task.CarriedOverToSprintID,
		})
	}

	return payload
}

// buildReport aggregates the sprint numbers. Every task counts its own
// estimate once; child estimates are never rolled up.
func buildReport(
	sprint Sprint,
	members []SprintTask,
	commitments []db.SprintCommitment,
	started bool,
	carryoverID *pgtype.UUID,
) Report {
	report := Report{
		SprintID: sprint.ID,
		State:    sprint.State,
		Tasks:    []ReportTask{},
	}

	// The commitment is the frozen snapshot for a started sprint. A sprint
	// that has not started treats its current tasks as the plan.
	commitmentIDs := make(map[string]struct{}, len(commitments))
	commitmentEstimates := make(map[string]int64, len(commitments))

	for _, commitment := range commitments {
		id := uuidValue(commitment.TaskID)
		commitmentIDs[id] = struct{}{}

		if commitment.EstimateSeconds.Valid {
			commitmentEstimates[id] = commitment.EstimateSeconds.Int64
		}
	}

	memberIDs := make(map[string]struct{}, len(members))

	for _, member := range members {
		memberIDs[member.ID] = struct{}{}
	}

	if started {
		for _, commitment := range commitments {
			id := uuidValue(commitment.TaskID)

			report.CommitmentTaskCount++

			if commitment.EstimateSeconds.Valid {
				report.CommitmentEstimatedSeconds +=
					commitment.EstimateSeconds.Int64
			} else {
				report.CommitmentUnestimatedCount++
			}

			if _, ok := memberIDs[id]; !ok {
				report.RemovedTaskCount++

				if commitment.EstimateSeconds.Valid {
					report.RemovedEstimatedSeconds +=
						commitment.EstimateSeconds.Int64
				}
			}
		}
	} else {
		// Not started yet: the current tasks are the plan.
		for _, member := range members {
			report.CommitmentTaskCount++

			if member.EstimateSeconds != nil {
				report.CommitmentEstimatedSeconds +=
					*member.EstimateSeconds
			} else {
				report.CommitmentUnestimatedCount++
			}
		}
	}

	for _, member := range members {
		estimate := int64(0)
		if member.EstimateSeconds != nil {
			estimate = *member.EstimateSeconds
		}

		switch {
		case member.Archived:
			report.ArchivedTaskCount++

		case member.Completed:
			report.CompletedTaskCount++
			report.CompletedEstimatedSeconds += estimate

		default:
			report.CarryoverTaskCount++
			report.CarryoverEstimatedSeconds += estimate
		}

		if member.EstimateSeconds == nil {
			report.UnestimatedTaskCount++
		}

		if started {
			if _, ok := commitmentIDs[member.ID]; !ok {
				report.AddedTaskCount++
				report.AddedEstimatedSeconds += estimate
			}
		}

		entry := ReportTask{
			TaskID:          member.ID,
			Title:           member.Title,
			EstimateSeconds: member.EstimateSeconds,
			Archived:        member.Archived,
			Completed:       member.Completed,
		}

		if carryoverID != nil &&
			!member.Completed &&
			!member.Archived {

			entry.CarriedOver = true

			if *carryoverID != (pgtype.UUID{}) {
				entry.CarriedOverToSprintID = ptrString(
					uuidValue(*carryoverID),
				)
			}
		}

		report.Tasks = append(report.Tasks, entry)
	}

	return report
}

func ptrString(value string) *string {
	return &value
}

func textValue(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}

	return pgtype.Text{String: *value, Valid: true}
}

func timestampValue(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func int8Value(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}

	return pgtype.Int8{Int64: *value, Valid: true}
}

func uuidValuePtr(value *string) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}

	id, err := uuid.Parse(*value)
	if err != nil {
		return pgtype.UUID{}
	}

	return pgtype.UUID{Bytes: id, Valid: true}
}

func mustUUID(value string) pgtype.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}

	return pgtype.UUID{Bytes: id, Valid: true}
}
