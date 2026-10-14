package audit

import (
	"context"
	"encoding/json"
	"fmt"

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
