package task

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
)

const MaxTaskReminders = 10

var (
	ErrInvalidReminder       = errors.New("provide exactly one future remindAt or minutesBeforeDue (0–43200) with an existing due date; note must be at most 1000 characters")
	ErrReminderNotFound      = errors.New("reminder not found")
	ErrReminderCancelled     = errors.New("cancelled reminder cannot be snoozed")
	ErrReminderLimit         = errors.New("at most 10 scheduled reminders per user per task")
	ErrReminderCompletedTask = errors.New("cannot schedule a reminder for a completed task")
)

type ReminderInput struct {
	Identity  auth.Identity
	ProjectID string
	TaskID    string
}

type CreateReminderInput struct {
	ReminderInput
	RemindAt         *time.Time
	MinutesBeforeDue *int32
	Note             string
}

type Reminder struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"projectId"`
	TaskID           string     `json:"taskId"`
	RemindAt         *time.Time `json:"remindAt"`
	MinutesBeforeDue *int32     `json:"minutesBeforeDue"`
	ScheduledFor     *time.Time `json:"scheduledFor"`
	Note             string     `json:"note"`
	Status           string     `json:"status"`
	SentAt           *time.Time `json:"sentAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type ReminderPage struct {
	Items      []Reminder `json:"items"`
	NextCursor *string    `json:"nextCursor"`
}

func parseReminderInput(input ReminderInput) (timeScope, error) {
	if input.Identity.TenantID == "" || input.Identity.UserID == "" {
		return timeScope{}, ErrInvalidID
	}
	return parseTimeScope(input.Identity.TenantID, input.ProjectID, input.TaskID, input.Identity.UserID)
}

func reminderSchedule(at *time.Time, minutes *int32, due *time.Time) *time.Time {
	if at != nil {
		value := at.UTC()
		return &value
	}
	if minutes == nil || due == nil {
		return nil
	}
	value := due.UTC().Add(-time.Duration(*minutes) * time.Minute)
	return &value
}

func normalizeReminder(input CreateReminderInput, now time.Time) (CreateReminderInput, error) {
	input.Note = strings.TrimSpace(input.Note)
	if !utf8.ValidString(input.Note) || strings.ContainsRune(input.Note, 0) || utf8.RuneCountInString(input.Note) > 1000 {
		return input, ErrInvalidReminder
	}
	if (input.RemindAt == nil) == (input.MinutesBeforeDue == nil) {
		return input, ErrInvalidReminder
	}
	if input.MinutesBeforeDue != nil && (*input.MinutesBeforeDue < 0 || *input.MinutesBeforeDue > 43200) {
		return input, ErrInvalidReminder
	}
	if input.RemindAt != nil && (!input.RemindAt.After(now) || input.RemindAt.Year() > 9999) {
		return input, ErrInvalidReminder
	}
	return input, nil
}

func validateReminder(input CreateReminderInput, item Task, now time.Time) (CreateReminderInput, error) {
	input, err := normalizeReminder(input, now)
	if err != nil {
		return input, err
	}
	if item.CompletedAt != nil {
		return input, ErrReminderCompletedTask
	}
	at := reminderSchedule(input.RemindAt, input.MinutesBeforeDue, item.DueAt)
	if at == nil || !at.After(now) || at.Year() > 9999 {
		return input, ErrInvalidReminder
	}
	return input, nil
}

func (s *Service) authorizeReminder(ctx context.Context, input ReminderInput) (Task, error) {
	if _, err := parseReminderInput(input); err != nil {
		return Task{}, err
	}
	// Reminders are personal to a project member, including read-only viewers.
	if _, err := s.projectRepository.GetMemberRole(ctx, input.Identity.TenantID, input.ProjectID, input.Identity.UserID); err != nil {
		return Task{}, err
	}
	if _, err := s.projectRepository.GetForTenant(ctx, input.Identity.TenantID, input.ProjectID); err != nil {
		return Task{}, err
	}
	return s.repository.GetForProject(ctx, input.Identity.TenantID, input.ProjectID, input.TaskID)
}

func (s *Service) CreateReminder(ctx context.Context, input CreateReminderInput) (Reminder, error) {
	if _, err := parseReminderInput(input.ReminderInput); err != nil {
		return Reminder{}, err
	}
	input, err := normalizeReminder(input, time.Now())
	if err != nil {
		return Reminder{}, err
	}
	if _, err := s.authorizeReminder(ctx, input.ReminderInput); err != nil {
		return Reminder{}, err
	}
	return s.repository.createReminder(ctx, input)
}

func (s *Service) SnoozeReminder(ctx context.Context, input ReminderInput, id string, at time.Time) (Reminder, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Reminder{}, ErrInvalidID
	}
	if !at.After(time.Now()) || at.Year() > 9999 {
		return Reminder{}, ErrInvalidReminder
	}
	if _, err := s.authorizeReminder(ctx, input); err != nil {
		return Reminder{}, err
	}
	return s.repository.snoozeReminder(ctx, input, id, at)
}

func (s *Service) CancelReminder(ctx context.Context, input ReminderInput, id string) error {
	scope, err := parseReminderInput(input)
	if err != nil {
		return err
	}
	idValue, err := parseUUIDParam(id)
	if err != nil {
		return err
	}
	// Owners can cancel even after leaving a project or after archival.
	return s.repository.cancelReminder(ctx, scope, idValue)
}

func (s *Service) ListReminders(ctx context.Context, input ReminderInput, limit int, encoded string) (ReminderPage, error) {
	if limit < 1 || limit > 100 {
		return ReminderPage{}, ErrInvalidID
	}
	var cursor *Cursor
	if encoded != "" {
		decoded, err := DecodeCursor(encoded)
		if err != nil {
			return ReminderPage{}, ErrInvalidID
		}
		if _, err := uuid.Parse(decoded.ID); err != nil {
			return ReminderPage{}, ErrInvalidID
		}
		cursor = &decoded
	}
	item, err := s.authorizeReminder(ctx, input)
	if err != nil {
		return ReminderPage{}, err
	}
	items, err := s.repository.listReminders(ctx, input, item.DueAt, limit+1, cursor)
	if err != nil {
		return ReminderPage{}, err
	}
	page := ReminderPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		next := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = &next
	}
	return page, nil
}

func reminderRetryDelay(attempt int32) time.Duration {
	if attempt > 6 {
		attempt = 6
	}
	return min(time.Minute*time.Duration(1<<uint(attempt)), time.Hour)
}

func reminderBody(title, note string) string {
	if note == "" {
		return title
	}
	return title + "\n" + note
}
