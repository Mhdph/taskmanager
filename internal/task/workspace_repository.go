package task

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/mhdph/taskmanager/internal/database/generated"
)

// WorkspaceSort selects the ORDER BY key for the tenant-wide task list.
type WorkspaceSort string

const (
	WorkspaceSortCreated  WorkspaceSort = "created"
	WorkspaceSortUpdated  WorkspaceSort = "updated"
	WorkspaceSortDue      WorkspaceSort = "due"
	WorkspaceSortPriority WorkspaceSort = "priority"
	WorkspaceSortTitle    WorkspaceSort = "title"
	WorkspaceSortPosition WorkspaceSort = "position"
)

func (s WorkspaceSort) Valid() bool {
	switch s {
	case WorkspaceSortCreated,
		WorkspaceSortUpdated,
		WorkspaceSortDue,
		WorkspaceSortPriority,
		WorkspaceSortTitle,
		WorkspaceSortPosition:

		return true
	}

	return false
}

// WorkspaceCursor is a keyset cursor for the tenant-wide task list. It
// carries the sort key and direction it was produced with, so a page always
// continues with the ordering it started with.
type WorkspaceCursor struct {
	Sort      WorkspaceSort
	Desc      bool
	ID        string
	CreatedAt *time.Time
	UpdatedAt *time.Time
	DueAt     *time.Time
	DueIsNull bool
	Priority  *int32
	Title     *string
	Position  *float64
}

// WorkspaceListOptions holds every filter for the tenant-wide task list.
type WorkspaceListOptions struct {
	IsAdmin bool

	ProjectID *string

	// AssigneeUserID filters by assignee; UnassignedOnly selects tasks
	// without an assignee. Neither means "any assignee".
	AssigneeUserID *string
	UnassignedOnly bool

	Priority       *Priority
	StatusName     *string
	StatusCategory *string
	ParentTaskID   *string
	Search         *string

	Archived bool

	DueFrom   *time.Time
	DueTo     *time.Time
	StartFrom *time.Time
	StartTo   *time.Time

	OverdueOnly   bool
	CompletedOnly bool

	Sort   WorkspaceSort
	Desc   bool
	Cursor *WorkspaceCursor

	Limit int32
}

// priorityCaseExpr ranks priorities numerically: urgent > high > medium > low.
const priorityCaseExpr = "CASE t.priority WHEN 'urgent' THEN 3 WHEN 'high' THEN 2 WHEN 'medium' THEN 1 ELSE 0 END"

// WorkspaceTask is a tenant-scoped task enriched with the project and
// status context the workspace views need.
type WorkspaceTask struct {
	Task

	StatusName     string
	StatusCategory string
	ProjectName    string
}

// ListForTenant returns tasks across all (or one) projects of a tenant,
// scoped by project membership, sorted by the requested key with keyset
// pagination.
//
// The statement is assembled dynamically because the sort key, sort
// direction and keyset predicate must match exactly. Every sort key and
// direction is validated against a whitelist and every value is passed as a
// bound parameter before it reaches the query text.
func (r *Repository) ListForTenant(
	ctx context.Context,
	tenantID string,
	userID string,
	options WorkspaceListOptions,
) ([]WorkspaceTask, error) {
	if !options.Sort.Valid() {
		return nil, ErrInvalidSort
	}

	var args []any

	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	where := []string{
		// Tenant
		fmt.Sprintf("t.tenant_id = %s", add(tenantID)),
	}

	// Project visibility: hosted admins see everything, everyone else only
	// sees tasks from projects they belong to (same scoping as search).
	where = append(
		where,
		fmt.Sprintf(
			"(%s::boolean OR EXISTS (SELECT 1 FROM project_members pm WHERE pm.tenant_id = t.tenant_id AND pm.project_id = t.project_id AND pm.user_id = %s))",
			add(options.IsAdmin),
			add(userID),
		),
	)

	if options.ProjectID != nil {
		projectID, err := parseUUIDParam(*options.ProjectID)
		if err != nil {
			return nil, err
		}

		where = append(
			where,
			fmt.Sprintf(
				"t.project_id = %s",
				add(projectID),
			),
		)
	}

	// Archived tasks of active projects only make sense as a separate page —
	// active listings also exclude archived projects.
	archivedParam := add(options.Archived)
	where = append(
		where,
		fmt.Sprintf("(t.archived_at IS NOT NULL) = %s", archivedParam),
	)

	if !options.Archived {
		where = append(where, "p.archived_at IS NULL")
	}

	if options.UnassignedOnly {
		where = append(where, "t.assignee_user_id IS NULL")
	} else if options.AssigneeUserID != nil {
		where = append(
			where,
			fmt.Sprintf(
				"t.assignee_user_id = %s",
				add(*options.AssigneeUserID),
			),
		)
	}

	if options.Priority != nil {
		switch *options.Priority {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh,
			PriorityUrgent:

		default:
			return nil, ErrInvalidPriority
		}

		where = append(
			where,
			fmt.Sprintf(
				"t.priority = %s",
				add(string(*options.Priority)),
			),
		)
	}

	if options.StatusName != nil && *options.StatusName != "" {
		where = append(
			where,
			fmt.Sprintf(
				"lower(s.name) = lower(%s)",
				add(*options.StatusName),
			),
		)
	}

	if options.StatusCategory != nil {
		switch *options.StatusCategory {
		case "todo", "in_progress", "done":

		default:
			return nil, ErrInvalidStatusCategory
		}

		where = append(
			where,
			fmt.Sprintf(
				"s.category = %s",
				add(*options.StatusCategory),
			),
		)
	}

	if options.ParentTaskID != nil {
		parentID, err := parseUUIDParam(*options.ParentTaskID)
		if err != nil {
			return nil, err
		}

		where = append(
			where,
			fmt.Sprintf(
				"t.parent_task_id = %s",
				add(parentID),
			),
		)
	}

	if options.Search != nil && *options.Search != "" {
		where = append(
			where,
			fmt.Sprintf(
				"t.title ILIKE '%%' || %s || '%%'",
				add(*options.Search),
			),
		)
	}

	if options.DueFrom != nil {
		where = append(
			where,
			fmt.Sprintf(
				"t.due_at >= %s",
				add(*options.DueFrom),
			),
		)
	}

	if options.DueTo != nil {
		where = append(
			where,
			fmt.Sprintf(
				"t.due_at <= %s",
				add(*options.DueTo),
			),
		)
	}

	if options.StartFrom != nil {
		where = append(
			where,
			fmt.Sprintf(
				"t.start_at >= %s",
				add(*options.StartFrom),
			),
		)
	}

	if options.StartTo != nil {
		where = append(
			where,
			fmt.Sprintf(
				"t.start_at <= %s",
				add(*options.StartTo),
			),
		)
	}

	if options.OverdueOnly {
		where = append(
			where,
			"(t.due_at IS NOT NULL AND t.due_at < NOW() AND t.completed_at IS NULL)",
		)
	}

	if options.CompletedOnly {
		where = append(where, "t.completed_at IS NOT NULL")
	}

	cursorPredicate, err := buildWorkspaceCursorPredicate(
		options,
		add,
	)
	if err != nil {
		return nil, err
	}

	if cursorPredicate != "" {
		where = append(
			where,
			cursorPredicate,
		)
	}

	query := "SELECT " + workspaceColumns() +
		" FROM tasks t" +
		" JOIN project_statuses s ON s.tenant_id = t.tenant_id AND s.id = t.status_id" +
		" JOIN projects p ON p.tenant_id = t.tenant_id AND p.id = t.project_id" +
		" WHERE " + strings.Join(where, " AND ") +
		buildWorkspaceOrder(options) +
		fmt.Sprintf(" LIMIT %s", add(options.Limit))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"list tasks for tenant: %w",
			err,
		)
	}

	defer rows.Close()

	tasks, err := scanWorkspaceTasks(rows)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func workspaceColumns() string {
	return `
    t.id,
    t.tenant_id,
    t.project_id,
    t.status_id,
    t.parent_task_id,
    t.title,
    t.description,
    t.priority,
    t.assignee_user_id,
    t.created_by_user_id,
    t.due_at,
    t.completed_at,
    t.created_at,
    t.updated_at,
    t.archived_at,
    t.start_at,
    t.position,
    t.estimate_seconds,
    t.recurrence,
    s.name AS status_name,
    s.category AS status_category,
    p.name AS project_name,
    COALESCE((
        SELECT jsonb_agg(
            jsonb_build_object(
                'id', l.id,
                'name', l.name,
                'color', l.color
            ) ORDER BY l.name, l.id
        )
        FROM task_labels tl
        JOIN labels l
            ON l.tenant_id = tl.tenant_id
            AND l.id = tl.label_id
        WHERE tl.task_id = t.id
    ), '[]'::jsonb) AS labels
`
}

// buildWorkspaceOrder assembles the ORDER BY clause from the whitelisted
// sort key and direction. For the due sort, tasks without a due date always
// sort last regardless of direction, so cursor pages remain stable.
func buildWorkspaceOrder(
	options WorkspaceListOptions,
) string {
	direction := "ASC"

	if options.Desc {
		direction = "DESC"
	}

	switch options.Sort {
	case WorkspaceSortDue:
		return " ORDER BY (t.due_at IS NULL) ASC, t.due_at " +
			direction +
			", t.id " + direction

	case WorkspaceSortPosition:
		// Board ordering: tasks without a position keep sorting last in
		// both directions, mirroring the due-date treatment.
		return " ORDER BY (t.position IS NULL) ASC, t.position " +
			direction +
			", t.id " + direction

	case WorkspaceSortPriority:
		return " ORDER BY " + priorityCaseExpr +
			" " + direction +
			", t.id " + direction

	case WorkspaceSortUpdated:
		return " ORDER BY t.updated_at " +
			direction +
			", t.id " + direction

	case WorkspaceSortTitle:
		return " ORDER BY lower(t.title) " +
			direction +
			", t.id " + direction

	default:
		return " ORDER BY t.created_at " +
			direction +
			", t.id " + direction
	}
}

// buildWorkspaceCursorPredicate assembles the keyset predicate for the
// cursor position. The sort expression, the comparison operators and the
// ORDER BY clause always stay in lockstep.
func buildWorkspaceCursorPredicate(
	options WorkspaceListOptions,
	add func(any) string,
) (string, error) {
	cursor := options.Cursor
	if cursor == nil {
		return "", nil
	}

	comparison := ">"

	if options.Desc {
		comparison = "<"
	}

	id, err := uuid.Parse(cursor.ID)
	if err != nil {
		return "", fmt.Errorf(
			"%w: invalid cursor id: %v",
			ErrInvalidID,
			err,
		)
	}

	idParam := add(pgtype.UUID{
		Bytes: id,
		Valid: true,
	})

	switch options.Sort {
	case WorkspaceSortDue:
		// due_at is nullable and its NULL block always sorts last, so the
		// cursor carries an explicit null marker. The ORDER BY structure
		// is: all non-null rows [direction], then NULL rows [id direction].
		if cursor.DueAt != nil {
			valueParam := add(*cursor.DueAt)

			return fmt.Sprintf(
				"((t.due_at IS NOT NULL AND (t.due_at %s %s OR (t.due_at = %s AND t.id %s %s))) OR t.due_at IS NULL)",
				comparison,
				valueParam,
				valueParam,
				comparison,
				idParam,
			), nil
		}

		return fmt.Sprintf(
			"t.due_at IS NULL AND t.id %s %s",
			comparison,
			idParam,
		), nil

	case WorkspaceSortPosition:
		// Same nulls-last structure as the due sort, with a float value.
		if cursor.Position != nil {
			valueParam := add(*cursor.Position)

			return fmt.Sprintf(
				"((t.position IS NOT NULL AND (t.position %s %s OR (t.position = %s AND t.id %s %s))) OR t.position IS NULL)",
				comparison,
				valueParam,
				valueParam,
				comparison,
				idParam,
			), nil
		}

		return fmt.Sprintf(
			"t.position IS NULL AND t.id %s %s",
			comparison,
			idParam,
		), nil

	case WorkspaceSortCreated:
		if cursor.CreatedAt == nil {
			return "", fmt.Errorf(
				"%w: cursor is missing created timestamp",
				ErrInvalidCursor,
			)
		}

		return fmt.Sprintf(
			"((t.created_at %s %s) OR (t.created_at = %s AND t.id %s %s))",
			comparison,
			add(*cursor.CreatedAt),
			add(*cursor.CreatedAt),
			comparison,
			idParam,
		), nil

	case WorkspaceSortUpdated:
		if cursor.UpdatedAt == nil {
			return "", fmt.Errorf(
				"%w: cursor is missing updated timestamp",
				ErrInvalidCursor,
			)
		}

		return fmt.Sprintf(
			"((t.updated_at %s %s) OR (t.updated_at = %s AND t.id %s %s))",
			comparison,
			add(*cursor.UpdatedAt),
			add(*cursor.UpdatedAt),
			comparison,
			idParam,
		), nil

	case WorkspaceSortPriority:
		if cursor.Priority == nil {
			return "", fmt.Errorf(
				"%w: cursor is missing priority",
				ErrInvalidCursor,
			)
		}

		valueParam := add(*cursor.Priority)

		return fmt.Sprintf(
			"((%s %s %s) OR (%s = %s AND t.id %s %s))",
			priorityCaseExpr,
			comparison,
			valueParam,
			priorityCaseExpr,
			valueParam,
			comparison,
			idParam,
		), nil

	case WorkspaceSortTitle:
		if cursor.Title == nil {
			return "", fmt.Errorf(
				"%w: cursor is missing title",
				ErrInvalidCursor,
			)
		}

		valueParam := add(strings.ToLower(*cursor.Title))

		return fmt.Sprintf(
			"((lower(t.title) %s %s) OR (lower(t.title) = %s AND t.id %s %s))",
			comparison,
			valueParam,
			valueParam,
			comparison,
			idParam,
		), nil
	}

	return "", ErrInvalidSort
}

func parseUUIDParam(
	value string,
) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	return pgtype.UUID{
		Bytes: id,
		Valid: true,
	}, nil
}

func scanWorkspaceTasks(
	rows pgx.Rows,
) ([]WorkspaceTask, error) {
	tasks := make([]WorkspaceTask, 0)

	for rows.Next() {
		var dbTask db.Task

		var statusName string
		var statusCategory string
		var projectName string

		var labelsJSON []byte

		err := rows.Scan(
			&dbTask.ID,
			&dbTask.TenantID,
			&dbTask.ProjectID,
			&dbTask.StatusID,
			&dbTask.ParentTaskID,
			&dbTask.Title,
			&dbTask.Description,
			&dbTask.Priority,
			&dbTask.AssigneeUserID,
			&dbTask.CreatedByUserID,
			&dbTask.DueAt,
			&dbTask.CompletedAt,
			&dbTask.CreatedAt,
			&dbTask.UpdatedAt,
			&dbTask.ArchivedAt,
			&dbTask.StartAt,
			&dbTask.Position,
			&dbTask.EstimateSeconds,
			&dbTask.Recurrence,
			&statusName,
			&statusCategory,
			&projectName,
			&labelsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"scan workspace task: %w",
				err,
			)
		}

		task := taskFromDB(dbTask)

		var labels []Label

		if err := json.Unmarshal(
			labelsJSON,
			&labels,
		); err != nil {
			return nil, fmt.Errorf(
				"decode task labels: %w",
				err,
			)
		}

		task.Labels = labels

		tasks = append(
			tasks,
			WorkspaceTask{
				Task: task,

				StatusName:     statusName,
				StatusCategory: statusCategory,
				ProjectName:    projectName,
			},
		)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate workspace tasks: %w",
			err,
		)
	}

	return tasks, nil
}

// GetForTenant resolves a task across all projects of a tenant. Used by the
// bulk operations, which receive bare task IDs.
func (r *Repository) GetForTenant(
	ctx context.Context,
	tenantID string,
	taskID string,
) (Task, error) {
	id, err := uuid.Parse(taskID)
	if err != nil {
		return Task{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	row, err := r.queries.GetTaskForTenant(
		ctx,
		db.GetTaskForTenantParams{
			TenantID: tenantID,
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrTaskNotFound
		}

		return Task{}, fmt.Errorf(
			"get task for tenant: %w",
			err,
		)
	}

	return taskFromDB(row), nil
}

// workspaceCursorJSON is the wire format of WorkspaceCursor.
type workspaceCursorJSON struct {
	Sort      string   `json:"sort"`
	Desc      bool     `json:"desc"`
	ID        string   `json:"id"`
	Title     *string  `json:"title"`
	Due       *string  `json:"due"`
	DueIsNull bool     `json:"dueIsNull"`
	Created   *string  `json:"created"`
	Updated   *string  `json:"updated"`
	Priority  *int32   `json:"priority"`
	Position  *float64 `json:"position"`
}

func formatCursorTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// EncodeWorkspaceCursor serializes a cursor to a URL-safe token.
func EncodeWorkspaceCursor(
	cursor WorkspaceCursor,
) (string, error) {
	data := workspaceCursorJSON{
		Sort:  string(cursor.Sort),
		Desc:  cursor.Desc,
		ID:    cursor.ID,
		Title: cursor.Title,
	}

	if cursor.DueAt != nil {
		value := formatCursorTime(*cursor.DueAt)
		data.Due = &value
	}

	data.DueIsNull = cursor.DueIsNull

	if cursor.CreatedAt != nil {
		value := formatCursorTime(*cursor.CreatedAt)
		data.Created = &value
	}

	if cursor.UpdatedAt != nil {
		value := formatCursorTime(*cursor.UpdatedAt)
		data.Updated = &value
	}

	data.Priority = cursor.Priority

	data.Position = cursor.Position

	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf(
			"marshal workspace cursor: %w",
			err,
		)
	}

	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// DecodeWorkspaceCursor parses a cursor token produced by
// EncodeWorkspaceCursor.
func DecodeWorkspaceCursor(
	value string,
) (WorkspaceCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return WorkspaceCursor{}, fmt.Errorf(
			"decode workspace cursor: %w",
			err,
		)
	}

	var data workspaceCursorJSON

	if err := json.Unmarshal(decoded, &data); err != nil {
		return WorkspaceCursor{}, fmt.Errorf(
			"invalid workspace cursor: %w",
			err,
		)
	}

	cursor := WorkspaceCursor{
		Sort:      WorkspaceSort(data.Sort),
		Desc:      data.Desc,
		ID:        data.ID,
		DueIsNull: data.DueIsNull,
		Priority:  data.Priority,
		Title:     data.Title,
		Position:  data.Position,
	}

	if !cursor.Sort.Valid() {
		return WorkspaceCursor{}, fmt.Errorf(
			"invalid workspace cursor sort: %s",
			data.Sort,
		)
	}

	if data.ID == "" {
		return WorkspaceCursor{}, fmt.Errorf(
			"invalid workspace cursor: id is empty",
		)
	}

	parse := func(raw *string, name string) (*time.Time, error) {
		if raw == nil {
			return nil, nil
		}

		value, err := time.Parse(
			time.RFC3339Nano,
			*raw,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid workspace cursor %s: %w",
				name,
				err,
			)
		}

		return &value, nil
	}

	if cursor.CreatedAt, err = parse(data.Created, "createdAt"); err != nil {
		return WorkspaceCursor{}, err
	}

	if cursor.UpdatedAt, err = parse(data.Updated, "updatedAt"); err != nil {
		return WorkspaceCursor{}, err
	}

	if cursor.DueAt, err = parse(data.Due, "dueAt"); err != nil {
		return WorkspaceCursor{}, err
	}

	return cursor, nil
}
