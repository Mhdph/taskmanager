package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/notification"
)

func TestReminderSchedulesAndValidation(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	due := now.Add(48 * time.Hour)
	minutes := int32(1440)
	input := CreateReminderInput{MinutesBeforeDue: &minutes, Note: "  پیگیری خروجی 👋  "}
	got, err := validateReminder(input, Task{DueAt: &due}, now)
	if err != nil || got.Note != "پیگیری خروجی 👋" {
		t.Fatalf("valid relative reminder: %+v %v", got, err)
	}
	if scheduled := reminderSchedule(nil, &minutes, &due); scheduled == nil || !scheduled.Equal(due.Add(-24*time.Hour)) {
		t.Fatal("relative offset incorrect")
	}
	laterDue := due.Add(24 * time.Hour)
	if scheduled := reminderSchedule(nil, &minutes, &laterDue); !scheduled.Equal(due) {
		t.Fatal("deadline change not followed")
	}
	if reminderSchedule(nil, &minutes, nil) != nil {
		t.Fatal("missing deadline should pause relative reminder")
	}
	when := now.Add(time.Hour).In(time.FixedZone("Tehran", 12600))
	if at := reminderSchedule(&when, nil, &due); !at.Equal(now.Add(time.Hour)) || at.Location() != time.UTC {
		t.Fatal("absolute time conversion incorrect")
	}
	for _, invalid := range []CreateReminderInput{
		{}, {RemindAt: &when, MinutesBeforeDue: &minutes}, {RemindAt: &now},
		{RemindAt: &when, Note: strings.Repeat("س", 1001)}, {RemindAt: &when, Note: "bad\x00note"},
	} {
		if _, err := validateReminder(invalid, Task{DueAt: &due}, now); !errors.Is(err, ErrInvalidReminder) {
			t.Fatalf("invalid reminder accepted: %+v %v", invalid, err)
		}
	}
	for _, offset := range []int32{-1, 43201} {
		if _, err := validateReminder(CreateReminderInput{MinutesBeforeDue: &offset}, Task{DueAt: &due}, now); !errors.Is(err, ErrInvalidReminder) {
			t.Fatal("invalid offset accepted")
		}
	}
	if _, err := validateReminder(input, Task{}, now); !errors.Is(err, ErrInvalidReminder) {
		t.Fatal("relative reminder without deadline accepted")
	}
	pastDue := now.Add(time.Hour)
	if _, err := validateReminder(input, Task{DueAt: &pastDue}, now); !errors.Is(err, ErrInvalidReminder) {
		t.Fatal("past relative schedule accepted")
	}
	if _, err := validateReminder(CreateReminderInput{RemindAt: &when}, Task{CompletedAt: &now}, now); !errors.Is(err, ErrReminderCompletedTask) {
		t.Fatal("completed task accepted reminder")
	}
}

func TestReminderRetryAndPayload(t *testing.T) {
	for attempt, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		if got := reminderRetryDelay(int32(attempt)); got != want {
			t.Fatalf("retry %d = %s, want %s", attempt, got, want)
		}
	}
	id := uuid.New()
	row := db.LockDueTaskReminderRow{ID: pgtype.UUID{Bytes: id, Valid: true}, ProjectID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TaskID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TenantID: "tenant-a", UserID: "alice", Title: "Review output", Note: "check screenshots", ScheduledFor: pgtype.Timestamptz{Time: time.Now(), Valid: true}}
	payload := reminderNotification(row)
	if payload.Type != notification.EventTaskReminder || payload.RecipientUserID != "alice" || payload.ActorUserID != nil || payload.Data["reminderId"] != id.String() || payload.Body == nil || *payload.Body != "Review output\ncheck screenshots" {
		t.Fatalf("incorrect payload: %+v", payload)
	}
}

func TestSentReminderKeepsOriginalScheduledTime(t *testing.T) {
	original := time.Now().UTC()
	newDue := original.Add(48 * time.Hour)
	row := db.TaskReminder{Status: "sent", MinutesBeforeDue: pgtype.Int4{Int32: 1440, Valid: true}, SentFor: pgtype.Timestamptz{Time: original, Valid: true}}
	item := reminderFromDB(row, &newDue)
	if item.ScheduledFor == nil || !item.ScheduledFor.Equal(original) {
		t.Fatal("sent reminder changed after deadline edit")
	}
}
