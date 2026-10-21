package task

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mhdph/taskmanager/internal/audit"
	db "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/project"
)

func deliveryUUID(value string) pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.MustParse(value), Valid: true}
}

func deliveryFromDB(row db.TaskDelivery) Delivery {
	item := Delivery{
		ID: uuid.UUID(row.ID.Bytes).String(), ProjectID: uuid.UUID(row.ProjectID.Bytes).String(), TaskID: uuid.UUID(row.TaskID.Bytes).String(),
		Revision: row.Revision, SubmittedByUserID: row.SubmittedByUserID, ReviewerUserID: row.ReviewerUserID,
		Body: row.Body, Status: DeliveryStatus(row.Status), SubmittedAt: row.SubmittedAt.Time,
	}
	if row.OutputUrl.Valid {
		item.OutputURL = &row.OutputUrl.String
	}
	if row.ReviewedByUserID.Valid {
		item.ReviewedByUserID = &row.ReviewedByUserID.String
	}
	if row.ReviewNote.Valid {
		item.ReviewNote = &row.ReviewNote.String
	}
	if row.ReviewedAt.Valid {
		item.ReviewedAt = &row.ReviewedAt.Time
	}
	return item
}

// Lock the project before the task, matching membership mutations. FOR SHARE
// keeps archive/member removal from racing with authorization inside the write.
func lockDeliveryTask(ctx context.Context, queries *db.Queries, input DeliveryInput) (Task, project.MemberRole, error) {
	if _, err := queries.LockDeliveryProject(ctx, db.LockDeliveryProjectParams{
		TenantID: input.Identity.TenantID, ID: deliveryUUID(input.ProjectID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, "", project.ErrNotFound
		}
		return Task{}, "", err
	}
	var role project.MemberRole
	if !input.Identity.IsAdmin() {
		member, err := queries.GetProjectMember(ctx, db.GetProjectMemberParams{
			TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), UserID: input.Identity.UserID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, "", project.ErrForbidden
		}
		if err != nil {
			return Task{}, "", err
		}
		role = project.MemberRole(member.Role)
		if role != project.MemberRoleManager && role != project.MemberRoleMember {
			return Task{}, "", project.ErrForbidden
		}
	}
	row, err := queries.LockTaskForUpdate(ctx, db.LockTaskForUpdateParams{
		TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), ID: deliveryUUID(input.TaskID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, "", ErrTaskNotFound
	}
	if err != nil {
		return Task{}, "", err
	}
	return taskFromDB(row), role, nil
}

func (r *Repository) submitDelivery(ctx context.Context, input SubmitDeliveryInput) (Delivery, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Delivery{}, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	item, role, err := lockDeliveryTask(ctx, queries, input.DeliveryInput)
	if err != nil {
		return Delivery{}, err
	}
	if !canSubmitDelivery(input.Identity, role, item) {
		return Delivery{}, project.ErrForbidden
	}
	// A manager submitting on behalf of an assignee must not appoint that
	// same assignee to approve their own output.
	if input.ReviewerUserID == deliveryOwner(item) {
		return Delivery{}, ErrInvalidDeliveryReviewer
	}
	reviewer, err := queries.GetProjectMember(ctx, db.GetProjectMemberParams{
		TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), UserID: input.ReviewerUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrInvalidDeliveryReviewer
	}
	if err != nil {
		return Delivery{}, err
	}
	if reviewer.Role != string(project.MemberRoleMember) && reviewer.Role != string(project.MemberRoleManager) {
		return Delivery{}, ErrInvalidDeliveryReviewer
	}
	params := db.CreateTaskDeliveryParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, TenantID: input.Identity.TenantID,
		ProjectID: deliveryUUID(input.ProjectID), TaskID: deliveryUUID(input.TaskID),
		SubmittedByUserID: input.Identity.UserID, ReviewerUserID: input.ReviewerUserID, Body: input.Body,
	}
	if input.OutputURL != nil {
		params.OutputUrl = pgtype.Text{String: *input.OutputURL, Valid: true}
	}
	row, err := queries.CreateTaskDelivery(ctx, params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "task_deliveries_one_pending_idx" {
			return Delivery{}, ErrDeliveryPending
		}
		return Delivery{}, err
	}
	delivery := deliveryFromDB(row)
	if err := recordDeliveryAudit(ctx, queries, input.DeliveryInput, delivery); err != nil {
		return Delivery{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, err
	}
	return delivery, nil
}

func (r *Repository) resolveDelivery(ctx context.Context, input ReviewDeliveryInput, withdraw bool) (Delivery, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Delivery{}, err
	}
	defer tx.Rollback(ctx)
	queries := r.queries.WithTx(tx)
	_, role, err := lockDeliveryTask(ctx, queries, input.DeliveryInput)
	if err != nil {
		return Delivery{}, err
	}
	row, err := queries.GetTaskDelivery(ctx, deliveryGetParams(input.DeliveryInput, input.DeliveryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrDeliveryNotFound
	}
	if err != nil {
		return Delivery{}, err
	}
	delivery := deliveryFromDB(row)
	if !canResolveDelivery(input.Identity, role, delivery, withdraw) {
		return Delivery{}, project.ErrForbidden
	}
	if delivery.Status != DeliveryPending {
		return Delivery{}, ErrDeliveryResolved
	}
	params := db.ResolveTaskDeliveryParams{
		TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), TaskID: deliveryUUID(input.TaskID), ID: deliveryUUID(input.DeliveryID),
		Status: string(input.Decision), ReviewedByUserID: pgtype.Text{String: input.Identity.UserID, Valid: true},
	}
	if input.Note != "" {
		params.ReviewNote = pgtype.Text{String: input.Note, Valid: true}
	}
	row, err = queries.ResolveTaskDelivery(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrDeliveryResolved
	}
	if err != nil {
		return Delivery{}, err
	}
	delivery = deliveryFromDB(row)
	if err := recordDeliveryAudit(ctx, queries, input.DeliveryInput, delivery); err != nil {
		return Delivery{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, err
	}
	return delivery, nil
}

func recordDeliveryAudit(ctx context.Context, queries *db.Queries, input DeliveryInput, delivery Delivery) error {
	actions := map[DeliveryStatus]audit.Action{
		DeliveryPending: audit.ActionTaskDeliverySubmitted, DeliveryApproved: audit.ActionTaskDeliveryApproved,
		DeliveryChangesRequested: audit.ActionTaskDeliveryChangesRequested, DeliveryWithdrawn: audit.ActionTaskDeliveryWithdrawn,
	}
	return audit.NewRepository(queries).Record(ctx, audit.RecordInput{
		TenantID: input.Identity.TenantID, ActorUserID: input.Identity.UserID, Action: actions[delivery.Status],
		EntityType: audit.EntityTask, EntityID: input.TaskID, ProjectID: auditID(input.ProjectID), TaskID: auditID(input.TaskID),
		Metadata: map[string]any{"deliveryId": delivery.ID, "revision": delivery.Revision, "status": delivery.Status, "reviewerUserId": delivery.ReviewerUserID},
	})
}

func deliveryGetParams(input DeliveryInput, id string) db.GetTaskDeliveryParams {
	return db.GetTaskDeliveryParams{TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), TaskID: deliveryUUID(input.TaskID), ID: deliveryUUID(id)}
}

func (r *Repository) getDelivery(ctx context.Context, input DeliveryInput, id string) (Delivery, error) {
	row, err := r.queries.GetTaskDelivery(ctx, deliveryGetParams(input, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, ErrDeliveryNotFound
	}
	if err != nil {
		return Delivery{}, err
	}
	return deliveryFromDB(row), nil
}

func (r *Repository) listDeliveries(ctx context.Context, input DeliveryInput, limit int, before int32) ([]Delivery, error) {
	rows, err := r.queries.ListTaskDeliveries(ctx, db.ListTaskDeliveriesParams{
		TenantID: input.Identity.TenantID, ProjectID: deliveryUUID(input.ProjectID), TaskID: deliveryUUID(input.TaskID), PageLimit: int32(limit),
		BeforeRevision: pgtype.Int4{Int32: before, Valid: before > 0},
	})
	if err != nil {
		return nil, err
	}
	items := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		items = append(items, deliveryFromDB(row))
	}
	return items, nil
}
