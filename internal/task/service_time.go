package task

import (
	"context"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

type TimeInput struct {
	Identity  auth.Identity
	ProjectID string
	TaskID    string
}

type TimeEntryPage struct {
	Items      []TimeEntry `json:"items"`
	NextCursor *string     `json:"nextCursor"`
}

func (s *Service) authorizeTime(ctx context.Context, input TimeInput, permission project.Permission) (timeScope, error) {
	scope, err := parseTimeScope(input.Identity.TenantID, input.ProjectID, input.TaskID, input.Identity.UserID)
	if err != nil {
		return timeScope{}, err
	}
	if err := s.projectAuthorizer.Require(ctx, input.Identity, input.ProjectID, permission); err != nil {
		return timeScope{}, err
	}
	if _, err := s.projectRepository.GetForTenant(ctx, input.Identity.TenantID, input.ProjectID); err != nil {
		return timeScope{}, err
	}
	if _, err := s.repository.GetForProject(ctx, input.Identity.TenantID, input.ProjectID, input.TaskID); err != nil {
		return timeScope{}, err
	}
	return scope, nil
}

func (s *Service) StartTimer(ctx context.Context, input TimeInput) (TimeEntry, error) {
	scope, err := s.authorizeTime(ctx, input, project.PermissionTrackTaskTime)
	if err != nil {
		return TimeEntry{}, err
	}
	return s.repository.startTimer(ctx, scope)
}

// An authenticated owner can stop their own timer even after membership removal
// or archival, so an inaccessible project cannot leave their timer stuck active.
func (s *Service) StopTimer(ctx context.Context, input TimeInput) (TimeEntry, error) {
	scope, err := parseTimeScope(input.Identity.TenantID, input.ProjectID, input.TaskID, input.Identity.UserID)
	if err != nil {
		return TimeEntry{}, err
	}
	return s.repository.stopTimer(ctx, scope)
}

func (s *Service) DeleteTimeEntry(ctx context.Context, input TimeInput, entryID string) error {
	scope, err := s.authorizeTime(ctx, input, project.PermissionTrackTaskTime)
	if err != nil {
		return err
	}
	return s.repository.deleteTimeEntry(ctx, scope, entryID)
}

func (s *Service) ListTimeEntries(ctx context.Context, input TimeInput, limit int, encodedCursor string) (TimeEntryPage, error) {
	scope, err := s.authorizeTime(ctx, input, project.PermissionViewTaskTime)
	if err != nil {
		return TimeEntryPage{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var cursor *Cursor
	if encodedCursor != "" {
		parsed, err := DecodeCursor(encodedCursor)
		if err != nil {
			return TimeEntryPage{}, project.ErrInvalidCursor
		}
		cursor = &parsed
	}
	entries, err := s.repository.listTimeEntries(ctx, scope, limit+1, cursor)
	if err != nil {
		return TimeEntryPage{}, err
	}
	page := TimeEntryPage{Items: entries}
	if len(entries) > limit {
		page.Items = entries[:limit]
		last := page.Items[limit-1]
		next := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = &next
	}
	return page, nil
}
