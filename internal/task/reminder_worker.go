package task

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
)

// Unlike the in-memory dispatcher, this sender acknowledges provider delivery.
type ReminderSender interface {
	SendWithID(context.Context, notification.SendInput, string) error
}

func (s *Service) RunReminders(ctx context.Context, sender ReminderSender) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 100 && ctx.Err() == nil; i++ {
			found, err := s.repository.deliverReminder(ctx, sender)
			if err != nil {
				log.Printf("task reminder sweep: %v", err)
				break
			}
			if !found {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Repository) deliverReminder(ctx context.Context, sender ReminderSender) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	row, err := queries.LockDueTaskReminder(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Hold only this reminder's lock through the bounded provider call. Other
	// instances skip it; snooze/cancel wait for an already in-flight send.
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = sender.SendWithID(sendCtx, reminderNotification(row), uuid.UUID(row.DispatchID.Bytes).String())
	cancel()
	if err != nil {
		log.Printf("send task reminder %s: %v", uuid.UUID(row.ID.Bytes), err)
		retry := time.Now().UTC().Add(reminderRetryDelay(row.AttemptCount))
		err = queries.RetryTaskReminder(ctx, db.RetryTaskReminderParams{ID: row.ID, NextAttemptAt: pgtype.Timestamptz{Time: retry, Valid: true}})
	} else {
		err = queries.MarkTaskReminderSent(ctx, db.MarkTaskReminderSentParams{ID: row.ID, SentFor: row.ScheduledFor})
	}
	if err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func reminderNotification(row db.LockDueTaskReminderRow) notification.SendInput {
	projectID, taskID := uuid.UUID(row.ProjectID.Bytes).String(), uuid.UUID(row.TaskID.Bytes).String()
	body := reminderBody(row.Title, row.Note)
	return notification.SendInput{
		TenantID: row.TenantID, RecipientUserID: row.UserID, Type: notification.EventTaskReminder,
		ProjectID: &projectID, TaskID: &taskID, Title: "Task reminder", Body: &body,
		Data: map[string]any{"reminderId": uuid.UUID(row.ID.Bytes).String(), "scheduledFor": row.ScheduledFor.Time.UTC().Format(time.RFC3339Nano)},
	}
}
