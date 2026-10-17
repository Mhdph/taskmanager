package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

type Repository struct {
	queries *db.Queries
}

func NewRepository(
	queries *db.Queries,
) *Repository {
	return &Repository{
		queries: queries,
	}
}

func (r *Repository) Record(
	ctx context.Context,
	input RecordInput,
) error {
	id := uuid.New()

	var projectID pgtype.UUID

	if input.ProjectID != nil {
		value, err := uuid.Parse(
			*input.ProjectID,
		)
		if err != nil {
			return fmt.Errorf(
				"parse audit project id: %w",
				err,
			)
		}

		projectID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	var taskID pgtype.UUID

	if input.TaskID != nil {
		value, err := uuid.Parse(
			*input.TaskID,
		)
		if err != nil {
			return fmt.Errorf(
				"parse audit task id: %w",
				err,
			)
		}

		taskID = pgtype.UUID{
			Bytes: value,
			Valid: true,
		}
	}

	metadata, err := json.Marshal(
		input.Metadata,
	)
	if err != nil {
		return fmt.Errorf(
			"marshal audit metadata: %w",
			err,
		)
	}

	_, err = r.queries.CreateAuditLog(
		ctx,
		db.CreateAuditLogParams{
			ID: pgtype.UUID{
				Bytes: id,
				Valid: true,
			},

			TenantID: input.TenantID,

			ActorUserID: input.ActorUserID,

			Action: string(input.Action),

			EntityType: string(input.EntityType),

			EntityID: input.EntityID,

			ProjectID: projectID,

			TaskID: taskID,

			Metadata: metadata,
		},
	)

	if err != nil {
		return fmt.Errorf(
			"create audit log: %w",
			err,
		)
	}

	return nil
}

// -------------------------------------------------------------------------
// Listing
// -------------------------------------------------------------------------

func entryFromDB(row db.AuditLog) Entry {
	entry := Entry{
		TenantID: row.TenantID,

		ActorUserID: row.ActorUserID,

		Action: Action(row.Action),

		EntityType: EntityType(row.EntityType),
		EntityID:   row.EntityID,

		Metadata: row.Metadata,

		CreatedAt: row.CreatedAt.Time,
	}

	if row.ID.Valid {
		entry.ID = uuid.UUID(row.ID.Bytes).String()
	}

	if row.ProjectID.Valid {
		value := uuid.UUID(row.ProjectID.Bytes).String()
		entry.ProjectID = &value
	}

	if row.TaskID.Valid {
		value := uuid.UUID(row.TaskID.Bytes).String()
		entry.TaskID = &value
	}

	return entry
}

// ListOptions filters the tenant-wide activity feed. nil filters mean
// "no filter".
type ListOptions struct {
	TenantID string

	IsAdmin bool
	UserID  string

	ProjectID   *string
	TaskID      *string
	ActorUserID *string
	EntityType  *EntityType

	Cursor *AuditCursor

	Limit int32
}

func (r *Repository) ListForTenant(
	ctx context.Context,
	options ListOptions,
) ([]Entry, error) {
	var projectID pgtype.UUID
	if options.ProjectID != nil {
		value, err := parseUUIDParam(*options.ProjectID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: project filter",
				ErrInvalidFilter,
			)
		}

		projectID = value
	}

	var taskID pgtype.UUID
	if options.TaskID != nil {
		value, err := parseUUIDParam(*options.TaskID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: task filter",
				ErrInvalidFilter,
			)
		}

		taskID = value
	}

	var actorUserID pgtype.Text
	if options.ActorUserID != nil {
		actorUserID = pgtype.Text{
			String: *options.ActorUserID,
			Valid:  true,
		}
	}

	var entityType pgtype.Text
	if options.EntityType != nil {
		entityType = pgtype.Text{
			String: string(*options.EntityType),
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

		id, err := parseUUIDParam(options.Cursor.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid cursor id",
				ErrInvalidCursor,
			)
		}

		cursorID = id
	}

	rows, err := r.queries.ListAuditLogsForTenant(
		ctx,
		db.ListAuditLogsForTenantParams{
			TenantID:    options.TenantID,
			IsAdmin:     options.IsAdmin,
			UserID:      options.UserID,
			ProjectID:   projectID,
			TaskID:      taskID,
			ActorUserID: actorUserID,
			EntityType:  entityType,

			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,

			PageLimit: options.Limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list audit logs for tenant: %w",
			err,
		)
	}

	return entriesFromRows(rows), nil
}

// ListForProject returns the activity of a single project, newest first.
func (r *Repository) ListForProject(
	ctx context.Context,
	tenantID string,
	projectID string,
	cursor *AuditCursor,
	limit int32,
) ([]Entry, error) {
	id, err := parseUUIDParam(projectID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: project id",
			ErrInvalidFilter,
		)
	}

	cursorCreatedAt := pgtype.Timestamptz{}
	cursorID := pgtype.UUID{}

	if cursor != nil {
		cursorCreatedAt = pgtype.Timestamptz{
			Time:  cursor.CreatedAt,
			Valid: true,
		}

		cursorIDParam, err := parseUUIDParam(cursor.ID)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid cursor id",
				ErrInvalidCursor,
			)
		}

		cursorID = cursorIDParam
	}

	rows, err := r.queries.ListProjectAuditLogs(
		ctx,
		db.ListProjectAuditLogsParams{
			TenantID:        tenantID,
			ProjectID:       id,
			CursorCreatedAt: cursorCreatedAt,
			CursorID:        cursorID,
			PageLimit:       limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list project audit logs: %w",
			err,
		)
	}

	return entriesFromRows(rows), nil
}

func entriesFromRows(
	rows []db.AuditLog,
) []Entry {
	entries := make(
		[]Entry,
		0,
		len(rows),
	)

	for _, row := range rows {
		entries = append(
			entries,
			entryFromDB(row),
		)
	}

	return entries
}

func parseUUIDParam(
	value string,
) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf(
			"invalid uuid %q: %w",
			value,
			err,
		)
	}

	return pgtype.UUID{
		Bytes: id,
		Valid: true,
	}, nil
}

// -------------------------------------------------------------------------
// Cursor
// -------------------------------------------------------------------------

// AuditCursor is the keyset cursor for the activity feed: strictly
// (created_at, id) descending, already validated when decoded.
type AuditCursor struct {
	CreatedAt time.Time
	ID        string
}

type auditCursorJSON struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

// EncodeAuditCursor serializes a cursor to a URL-safe token.
func EncodeAuditCursor(
	cursor AuditCursor,
) (string, error) {
	encoded, err := json.Marshal(auditCursorJSON{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID,
	})
	if err != nil {
		return "", fmt.Errorf(
			"marshal audit cursor: %w",
			err,
		)
	}

	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// DecodeAuditCursor parses a token produced by EncodeAuditCursor.
func DecodeAuditCursor(
	value string,
) (AuditCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return AuditCursor{}, fmt.Errorf(
			"decode audit cursor: %w",
			err,
		)
	}

	var data auditCursorJSON

	if err := json.Unmarshal(decoded, &data); err != nil {
		return AuditCursor{}, fmt.Errorf(
			"invalid audit cursor: %w",
			err,
		)
	}

	if data.ID == "" {
		return AuditCursor{}, fmt.Errorf(
			"invalid audit cursor: id is empty",
		)
	}

	parsed, err := time.Parse(
		time.RFC3339Nano,
		data.CreatedAt,
	)
	if err != nil {
		return AuditCursor{}, fmt.Errorf(
			"invalid audit cursor createdAt: %w",
			err,
		)
	}

	return AuditCursor{
		CreatedAt: parsed,
		ID:        data.ID,
	}, nil
}
