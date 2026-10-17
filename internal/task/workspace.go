package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
)

// maximum number of tasks a single bulk request may touch.
const bulkMaxTasks = 100
const defaultWorkspaceLimit = 20
const maxWorkspaceLimit = 100

type WorkspaceListInput struct {
	Identity auth.Identity

	ProjectID string

	// Assignee accepts "me", "none" or a raw user id; empty means any.
	Assignee string

	Priority       string
	Status         string
	StatusCategory string
	ParentTaskID   string
	Search         string

	Archived bool

	DueFrom   *time.Time
	DueTo     *time.Time
	StartFrom *time.Time
	StartTo   *time.Time

	OverdueOnly   bool
	CompletedOnly bool

	Sort   string
	Desc   bool
	Cursor string

	Limit int
}

type WorkspaceListPage struct {
	Items []WorkspaceTask

	NextCursor *string
}

func (s *Service) WorkspaceList(
	ctx context.Context,
	input WorkspaceListInput,
) (WorkspaceListPage, error) {
	limit := input.Limit

	if limit <= 0 {
		limit = defaultWorkspaceLimit
	}

	if limit > maxWorkspaceLimit {
		limit = maxWorkspaceLimit
	}

	sort := WorkspaceSort(input.Sort)

	desc := input.Desc

	if sort == "" {
		sort = WorkspaceSortCreated
		// Newest first, matching the project task list ordering.
		desc = true
	}

	if !sort.Valid() {
		return WorkspaceListPage{}, ErrInvalidSort
	}

	if input.StatusCategory != "" {
		switch input.StatusCategory {
		case "todo", "in_progress", "done":

		default:
			return WorkspaceListPage{}, ErrInvalidStatusCategory
		}
	}

	if input.Priority != "" {
		switch Priority(input.Priority) {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh,
			PriorityUrgent:

		default:
			return WorkspaceListPage{}, ErrInvalidPriority
		}
	}

	if input.DueFrom != nil &&
		input.DueTo != nil &&
		input.DueFrom.After(*input.DueTo) {

		return WorkspaceListPage{},
			ErrInvalidTaskDateRange
	}

	if input.StartFrom != nil &&
		input.StartTo != nil &&
		input.StartFrom.After(*input.StartTo) {

		return WorkspaceListPage{},
			ErrInvalidTaskDateRange
	}

	var cursor *WorkspaceCursor

	if input.Cursor != "" {
		decoded, err := DecodeWorkspaceCursor(
			input.Cursor,
		)
		if err != nil {
			return WorkspaceListPage{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidCursor,
				err,
			)
		}

		// Continue with the ordering the cursor was produced with.
		sort = decoded.Sort
		desc = decoded.Desc
		cursor = &decoded
	}

	options := WorkspaceListOptions{
		IsAdmin: input.Identity.IsAdmin(),

		Archived: input.Archived,

		DueFrom:   input.DueFrom,
		DueTo:     input.DueTo,
		StartFrom: input.StartFrom,
		StartTo:   input.StartTo,

		OverdueOnly:   input.OverdueOnly,
		CompletedOnly: input.CompletedOnly,

		Sort: sort,
		Desc: desc,

		Cursor: cursor,

		Limit: int32(limit + 1),
	}

	if input.ProjectID != "" {
		value := input.ProjectID
		options.ProjectID = &value
	}

	switch strings.TrimSpace(input.Assignee) {
	case "":
		// any assignee

	case "me":
		value := input.Identity.UserID
		options.AssigneeUserID = &value

	case "none":
		options.UnassignedOnly = true

	default:
		value := input.Assignee
		options.AssigneeUserID = &value
	}

	if input.Priority != "" {
		value := Priority(input.Priority)
		options.Priority = &value
	}

	if value := strings.TrimSpace(input.Status); value != "" {
		options.StatusName = &value
	}

	if input.StatusCategory != "" {
		value := input.StatusCategory
		options.StatusCategory = &value
	}

	if input.ParentTaskID != "" {
		value := input.ParentTaskID
		options.ParentTaskID = &value
	}

	if value := strings.TrimSpace(input.Search); value != "" {
		options.Search = &value
	}

	items, err := s.repository.ListForTenant(
		ctx,
		input.Identity.TenantID,
		input.Identity.UserID,
		options,
	)
	if err != nil {
		return WorkspaceListPage{}, err
	}

	hasNext := len(items) > limit

	if hasNext {
		items = items[:limit]
	}

	var nextCursor *string

	if hasNext && len(items) > 0 {
		value, err := workspaceCursorFromLastTask(
			sort,
			desc,
			items[len(items)-1].Task,
		)
		if err != nil {
			return WorkspaceListPage{}, err
		}

		encoded, err := EncodeWorkspaceCursor(value)
		if err != nil {
			return WorkspaceListPage{}, err
		}

		nextCursor = &encoded
	}

	return WorkspaceListPage{
		Items: items,

		NextCursor: nextCursor,
	}, nil
}

// workspaceCursorFromTask builds the continuation cursor for the last task
// of a page. The cursor must carry the sort value of the used sort key.
func workspaceCursorFromLastTask(
	sort WorkspaceSort,
	desc bool,
	last Task,
) (WorkspaceCursor, error) {
	id := last.ID

	cursor := WorkspaceCursor{
		Sort: sort,
		Desc: desc,
		ID:   id,
	}

	switch sort {
	case WorkspaceSortCreated:
		value := last.CreatedAt
		cursor.CreatedAt = &value

	case WorkspaceSortUpdated:
		value := last.UpdatedAt
		cursor.UpdatedAt = &value

	case WorkspaceSortDue:
		if last.DueAt == nil {
			cursor.DueIsNull = true
		} else {
			value := *last.DueAt
			cursor.DueAt = &value
		}

	case WorkspaceSortPosition:
		if last.Position != nil {
			value := *last.Position
			cursor.Position = &value
		}

	case WorkspaceSortPriority:
		value := priorityRank(last.Priority)
		cursor.Priority = &value

	case WorkspaceSortTitle:
		value := last.Title
		cursor.Title = &value
	}

	return cursor, nil
}

func priorityRank(priority Priority) int32 {
	switch priority {
	case PriorityUrgent:
		return 3
	case PriorityHigh:
		return 2
	case PriorityMedium:
		return 1
	default:
		return 0
	}
}

// -------------------------------------------------------------------------
// Bulk operations
// -------------------------------------------------------------------------

type BulkUpdateInput struct {
	Identity auth.Identity

	TaskIDs []string

	// Status accepts a status id or a status name (resolved per-project).
	Status *string

	Priority *Priority

	// Assignee accepts "none" (unassign) or a user id.
	Assignee *string

	Archive bool
}

type BulkResultItem struct {
	TaskID string

	OK  bool
	Err string
}

type BulkResult struct {
	Results []BulkResultItem

	Updated int
}

func (s *Service) BulkUpdate(
	ctx context.Context,
	input BulkUpdateInput,
) (BulkResult, error) {
	if len(input.TaskIDs) == 0 {
		return BulkResult{}, ErrInvalidID
	}

	if len(input.TaskIDs) > bulkMaxTasks {
		return BulkResult{}, ErrBulkTooLarge
	}

	hasAction := input.Archive ||
		input.Status != nil ||
		input.Priority != nil ||
		input.Assignee != nil

	if !hasAction {
		return BulkResult{}, ErrNoChanges
	}

	if input.Priority != nil {
		switch *input.Priority {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh,
			PriorityUrgent:

		default:
			return BulkResult{}, ErrInvalidPriority
		}
	}

	// Deduplicate while keeping the original order.
	seen := make(map[string]struct{}, len(input.TaskIDs))

	ids := make([]string, 0, len(input.TaskIDs))

	for _, id := range input.TaskIDs {
		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	// Cache resolved status ids per project, so a status name is looked up
	// once per affected project instead of once per task.
	statusIDsByProject := make(map[string]string)

	statusIDFor := func(
		ctx context.Context,
		projectID string,
	) (string, error) {
		key := input.Identity.TenantID + ":" + projectID

		if cached, ok := statusIDsByProject[key]; ok {
			return cached, nil
		}

		statusID, err := s.resolveStatusInput(
			ctx,
			input.Identity.TenantID,
			projectID,
			*input.Status,
		)
		if err != nil {
			return "", err
		}

		statusIDsByProject[key] = statusID

		return statusID, nil
	}

	results := make(
		[]BulkResultItem,
		0,
		len(ids),
	)

	updated := 0

	for _, taskID := range ids {
		item := BulkResultItem{
			TaskID: taskID,
		}

		err := s.applyBulkTask(
			ctx,
			input,
			taskID,
			statusIDFor,
		)
		if err != nil {
			item.Err = err.Error()
		} else {
			item.OK = true
			updated++
		}

		results = append(
			results,
			item,
		)
	}

	return BulkResult{
		Results: results,
		Updated: updated,
	}, nil
}

// resolveStatusInput accepts either a status UUID or a status name. Names
// are matched case-insensitively within the task's project.
func (s *Service) resolveStatusInput(
	ctx context.Context,
	tenantID string,
	projectID string,
	value string,
) (string, error) {
	if _, err := uuid.Parse(value); err == nil {
		return value, nil
	}

	statuses, err := s.projectRepository.ListStatuses(
		ctx,
		tenantID,
		projectID,
	)
	if err != nil {
		return "", fmt.Errorf(
			"list project statuses: %w",
			err,
		)
	}

	for _, status := range statuses {
		if strings.EqualFold(
			status.Name,
			value,
		) {
			return status.ID, nil
		}
	}

	return "", ErrStatusNotFound
}

func (s *Service) applyBulkTask(
	ctx context.Context,
	input BulkUpdateInput,
	taskID string,
	statusIDFor func(context.Context, string) (string, error),
) error {
	current, err := s.repository.GetForTenant(
		ctx,
		input.Identity.TenantID,
		taskID,
	)
	if err != nil {
		return err
	}

	if input.Archive {
		return s.Archive(
			ctx,
			input.Identity,
			current.ProjectID,
			taskID,
		)
	}

	update := UpdateInput{
		Identity: input.Identity,

		ProjectID: current.ProjectID,
		TaskID:    taskID,
	}

	if input.Status != nil {
		statusID, err := statusIDFor(
			ctx,
			current.ProjectID,
		)
		if err != nil {
			if errors.Is(err, ErrStatusNotFound) {
				return ErrStatusNotFound
			}

			return err
		}

		update.StatusID = &statusID
	}

	if input.Priority != nil {
		update.Priority = input.Priority
	}

	if input.Assignee != nil {
		update.UpdateAssignee = true

		if strings.EqualFold(
			*input.Assignee,
			"none",
		) {
			update.AssigneeUserID = nil
		} else {
			value := strings.TrimSpace(*input.Assignee)
			update.AssigneeUserID = &value
		}
	}

	_, err = s.Update(
		ctx,
		update,
	)

	return err
}
