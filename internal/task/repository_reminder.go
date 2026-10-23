package task

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

func reminderFromDB(row db.TaskReminder, due *time.Time) Reminder {
	item := Reminder{ID: uuid.UUID(row.ID.Bytes).String(), ProjectID: uuid.UUID(row.ProjectID.Bytes).String(), TaskID: uuid.UUID(row.TaskID.Bytes).String(),
		Note: row.Note, Status: row.Status, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	if row.RemindAt.Valid {
		item.RemindAt = &row.RemindAt.Time
	}
	if row.MinutesBeforeDue.Valid {
		item.MinutesBeforeDue = &row.MinutesBeforeDue.Int32
	}
	if row.SentAt.Valid {
		item.SentAt = &row.SentAt.Time
	}
	item.ScheduledFor = reminderSchedule(item.RemindAt, item.MinutesBeforeDue, due)
	if row.SentFor.Valid {
		item.ScheduledFor = &row.SentFor.Time
	}
	return item
}

func lockReminderTask(ctx context.Context, queries *db.Queries, scope timeScope) (Task, error) {
	row, err := queries.LockTaskForReminder(ctx, db.LockTaskForReminderParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, err
	}
	return taskFromDB(row), nil
}

func checkReminderLimit(ctx context.Context, queries *db.Queries, scope timeScope) error {
	count, err := queries.CountScheduledTaskReminders(ctx, db.CountScheduledTaskRemindersParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID})
	if err != nil {
		return err
	}
	if count >= MaxTaskReminders {
		return ErrReminderLimit
	}
	return nil
}

func (r *Repository) createReminder(ctx context.Context, input CreateReminderInput) (Reminder, error) {
	scope, err := parseReminderInput(input.ReminderInput)
	if err != nil {
		return Reminder{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	item, err := lockReminderTask(ctx, queries, scope)
	if err != nil {
		return Reminder{}, err
	}
	input, err = validateReminder(input, item, time.Now())
	if err != nil {
		return Reminder{}, err
	}
	if err := checkReminderLimit(ctx, queries, scope); err != nil {
		return Reminder{}, err
	}
	params := db.CreateTaskReminderParams{ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID,
		Note: input.Note, DispatchID: pgtype.UUID{Bytes: uuid.New(), Valid: true}}
	if input.RemindAt != nil {
		params.RemindAt = pgtype.Timestamptz{Time: input.RemindAt.UTC(), Valid: true}
	}
	if input.MinutesBeforeDue != nil {
		params.MinutesBeforeDue = pgtype.Int4{Int32: *input.MinutesBeforeDue, Valid: true}
	}
	row, err := queries.CreateTaskReminder(ctx, params)
	if err != nil {
		return Reminder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reminder{}, err
	}
	return reminderFromDB(row, item.DueAt), nil
}

func (r *Repository) snoozeReminder(ctx context.Context, input ReminderInput, id string, at time.Time) (Reminder, error) {
	scope, err := parseReminderInput(input)
	if err != nil {
		return Reminder{}, err
	}
	idValue, err := parseUUIDParam(id)
	if err != nil {
		return Reminder{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	item, err := lockReminderTask(ctx, queries, scope)
	if err != nil {
		return Reminder{}, err
	}
	if _, err := validateReminder(CreateReminderInput{ReminderInput: input, RemindAt: &at}, item, time.Now()); err != nil {
		return Reminder{}, err
	}
	current, err := queries.GetTaskReminder(ctx, db.GetTaskReminderParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID, ID: idValue})
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrReminderNotFound
	}
	if err != nil {
		return Reminder{}, err
	}
	if current.Status == "cancelled" {
		return Reminder{}, ErrReminderCancelled
	}
	if current.Status != "scheduled" {
		if err := checkReminderLimit(ctx, queries, scope); err != nil {
			return Reminder{}, err
		}
	}
	row, err := queries.SnoozeTaskReminder(ctx, db.SnoozeTaskReminderParams{
		TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID, ID: idValue,
		RemindAt: pgtype.Timestamptz{Time: at.UTC(), Valid: true}, DispatchID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
	})
	if err != nil {
		return Reminder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reminder{}, err
	}
	return reminderFromDB(row, item.DueAt), nil
}

func (r *Repository) cancelReminder(ctx context.Context, scope timeScope, id pgtype.UUID) error {
	affected, err := r.queries.CancelTaskReminder(ctx, db.CancelTaskReminderParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID, ID: id})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrReminderNotFound
	}
	return nil
}

func (r *Repository) listReminders(ctx context.Context, input ReminderInput, due *time.Time, limit int, cursor *Cursor) ([]Reminder, error) {
	scope, err := parseReminderInput(input)
	if err != nil {
		return nil, err
	}
	params := db.ListTaskRemindersParams{TenantID: scope.tenantID, ProjectID: scope.projectID, TaskID: scope.taskID, UserID: scope.userID, PageLimit: int32(limit)}
	if cursor != nil {
		id, err := parseUUIDParam(cursor.ID)
		if err != nil {
			return nil, err
		}
		params.CursorID = id
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, Valid: true}
	}
	rows, err := r.queries.ListTaskReminders(ctx, params)
	if err != nil {
		return nil, err
	}
	items := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		items = append(items, reminderFromDB(row, due))
	}
	return items, nil
}
