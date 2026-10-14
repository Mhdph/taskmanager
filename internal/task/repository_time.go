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

var (
	ErrTimerActive       = errors.New("user already has an active timer")
	ErrTimerNotFound     = errors.New("active timer not found")
	ErrTimeEntryNotFound = errors.New("completed time entry not found")
)

type TimeEntry struct {
	ID              string     `json:"id"`
	TaskID          string     `json:"taskId"`
	UserID          string     `json:"userId"`
	StartedAt       time.Time  `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	DurationSeconds int64      `json:"durationSeconds"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type timeScope struct {
	tenantID, userID  string
	projectID, taskID pgtype.UUID
}

func parseTimeScope(tenantID, projectID, taskID, userID string) (timeScope, error) {
	p, err := uuid.Parse(projectID)
	if err != nil {
		return timeScope{}, ErrInvalidID
	}
	t, err := uuid.Parse(taskID)
	if err != nil {
		return timeScope{}, ErrInvalidID
	}
	return timeScope{tenantID, userID, pgtype.UUID{Bytes: p, Valid: true}, pgtype.UUID{Bytes: t, Valid: true}}, nil
}

func timeEntryFromDB(row db.TaskTimeEntry) TimeEntry {
	entry := TimeEntry{ID: uuid.UUID(row.ID.Bytes).String(), TaskID: uuid.UUID(row.TaskID.Bytes).String(), UserID: row.UserID, StartedAt: row.StartedAt.Time, DurationSeconds: row.DurationSeconds, CreatedAt: row.CreatedAt.Time}
	if row.EndedAt.Valid {
		entry.EndedAt = &row.EndedAt.Time
	}
	return entry
}

func (r *Repository) startTimer(ctx context.Context, scope timeScope) (TimeEntry, error) {
	row, err := r.queries.StartTaskTimer(ctx, db.StartTaskTimerParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "task_time_entries_one_active_per_user_idx" {
			return TimeEntry{}, ErrTimerActive
		}
		return TimeEntry{}, fmt.Errorf("start task timer: %w", err)
	}
	return timeEntryFromDB(row), nil
}

func (r *Repository) stopTimer(ctx context.Context, scope timeScope) (TimeEntry, error) {
	row, err := r.queries.StopTaskTimer(ctx, db.StopTaskTimerParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return TimeEntry{}, ErrTimerNotFound
	}
	if err != nil {
		return TimeEntry{}, fmt.Errorf("stop task timer: %w", err)
	}
	return timeEntryFromDB(row), nil
}

func (r *Repository) deleteTimeEntry(ctx context.Context, scope timeScope, entryID string) error {
	id, err := uuid.Parse(entryID)
	if err != nil {
		return ErrInvalidID
	}
	count, err := r.queries.DeleteTaskTimeEntry(ctx, db.DeleteTaskTimeEntryParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID, ID: pgtype.UUID{Bytes: id, Valid: true}})
	if err != nil {
		return fmt.Errorf("delete task time entry: %w", err)
	}
	if count == 0 {
		return ErrTimeEntryNotFound
	}
	return nil
}

func (r *Repository) listTimeEntries(ctx context.Context, scope timeScope, limit int, cursor *Cursor) ([]TimeEntry, error) {
	args := db.ListTaskTimeEntriesParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, PageLimit: int32(limit)}
	if cursor != nil {
		id, err := uuid.Parse(cursor.ID)
		if err != nil {
			return nil, ErrInvalidID
		}
		args.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, Valid: true}
		args.CursorID = pgtype.UUID{Bytes: id, Valid: true}
	}
	rows, err := r.queries.ListTaskTimeEntries(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("list task time entries: %w", err)
	}
	result := make([]TimeEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, timeEntryFromDB(row))
	}
	return result, nil
}
