package task

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
)

type DeliveryStatus string

const (
	DeliveryPending          DeliveryStatus = "pending"
	DeliveryApproved         DeliveryStatus = "approved"
	DeliveryChangesRequested DeliveryStatus = "changes_requested"
	DeliveryWithdrawn        DeliveryStatus = "withdrawn"
)

var (
	ErrInvalidDelivery         = errors.New("invalid delivery: provide a reviewer, 1–10000 character body and optional HTTP(S) output URL")
	ErrInvalidDeliveryReview   = errors.New("invalid review: decision must be approved or changes_requested; changes_requested requires a reason (max 2000 characters)")
	ErrInvalidDeliveryReviewer = errors.New("reviewer must be another active project member with review permission")
	ErrDeliveryPending         = errors.New("task already has a pending delivery; review or withdraw it first")
	ErrDeliveryResolved        = errors.New("delivery has already been reviewed or withdrawn")
	ErrDeliveryNotFound        = errors.New("delivery not found")
)

type Delivery struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"projectId"`
	TaskID            string         `json:"taskId"`
	Revision          int32          `json:"revision"`
	SubmittedByUserID string         `json:"submittedByUserId"`
	ReviewerUserID    string         `json:"reviewerUserId"`
	Body              string         `json:"body"`
	OutputURL         *string        `json:"outputUrl"`
	Status            DeliveryStatus `json:"status"`
	SubmittedAt       time.Time      `json:"submittedAt"`
	ReviewedByUserID  *string        `json:"reviewedByUserId"`
	ReviewNote        *string        `json:"reviewNote"`
	ReviewedAt        *time.Time     `json:"reviewedAt"`
}

type DeliveryInput struct {
	Identity  auth.Identity
	ProjectID string
	TaskID    string
}

type SubmitDeliveryInput struct {
	DeliveryInput
	ReviewerUserID string
	Body           string
	OutputURL      *string
}

type ReviewDeliveryInput struct {
	DeliveryInput
	DeliveryID string
	Decision   DeliveryStatus
	Note       string
}

type DeliveryPage struct {
	Items      []Delivery `json:"items"`
	NextCursor *string    `json:"nextCursor"`
}

func validateDeliveryScope(input DeliveryInput) error {
	if input.Identity.TenantID == "" || input.Identity.UserID == "" {
		return project.ErrForbidden
	}
	if _, err := uuid.Parse(input.ProjectID); err != nil {
		return ErrInvalidID
	}
	if _, err := uuid.Parse(input.TaskID); err != nil {
		return ErrInvalidID
	}
	return nil
}

func normalizeDelivery(input SubmitDeliveryInput) (SubmitDeliveryInput, error) {
	input.Body = strings.TrimSpace(input.Body)
	input.ReviewerUserID = strings.TrimSpace(input.ReviewerUserID)
	if input.ReviewerUserID == "" || strings.ContainsRune(input.ReviewerUserID, 0) || input.ReviewerUserID == input.Identity.UserID {
		return input, ErrInvalidDeliveryReviewer
	}
	if !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, 0) || input.Body == "" || utf8.RuneCountInString(input.Body) > 10000 {
		return input, ErrInvalidDelivery
	}
	if input.OutputURL != nil {
		value := strings.TrimSpace(*input.OutputURL)
		input.OutputURL = nil
		if value != "" {
			parsed, err := url.Parse(value)
			if err != nil || len(value) > 2048 || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
				return input, ErrInvalidDelivery
			}
			input.OutputURL = &value
		}
	}
	return input, nil
}

func normalizeDeliveryReview(input ReviewDeliveryInput, withdraw bool) (ReviewDeliveryInput, error) {
	if _, err := uuid.Parse(input.DeliveryID); err != nil {
		return input, ErrInvalidID
	}
	input.Note = strings.TrimSpace(input.Note)
	if !utf8.ValidString(input.Note) || strings.ContainsRune(input.Note, 0) || utf8.RuneCountInString(input.Note) > 2000 {
		return input, ErrInvalidDeliveryReview
	}
	if withdraw {
		input.Decision = DeliveryWithdrawn
		return input, nil
	}
	if input.Decision != DeliveryApproved && input.Decision != DeliveryChangesRequested {
		return input, ErrInvalidDeliveryReview
	}
	if input.Decision == DeliveryChangesRequested && input.Note == "" {
		return input, ErrInvalidDeliveryReview
	}
	return input, nil
}

func canSubmitDelivery(identity auth.Identity, role project.MemberRole, item Task) bool {
	if identity.IsAdmin() || role == project.MemberRoleManager {
		return true
	}
	if role != project.MemberRoleMember {
		return false
	}
	return deliveryOwner(item) == identity.UserID
}

func deliveryOwner(item Task) string {
	if item.AssigneeUserID != nil {
		return *item.AssigneeUserID
	}
	return item.CreatedByUserID
}

func canResolveDelivery(identity auth.Identity, role project.MemberRole, delivery Delivery, withdraw bool) bool {
	if withdraw {
		return identity.IsAdmin() || role == project.MemberRoleManager || ((role == project.MemberRoleMember) && delivery.SubmittedByUserID == identity.UserID)
	}
	return (identity.IsAdmin() || role == project.MemberRoleManager || role == project.MemberRoleMember) &&
		identity.UserID == delivery.ReviewerUserID && identity.UserID != delivery.SubmittedByUserID
}

func (s *Service) SubmitDelivery(ctx context.Context, input SubmitDeliveryInput) (Delivery, error) {
	if err := validateDeliveryScope(input.DeliveryInput); err != nil {
		return Delivery{}, err
	}
	input, err := normalizeDelivery(input)
	if err != nil {
		return Delivery{}, err
	}
	if err := s.projectAuthorizer.Require(ctx, input.Identity, input.ProjectID, project.PermissionSubmitTaskDelivery); err != nil {
		return Delivery{}, err
	}
	delivery, err := s.repository.submitDelivery(ctx, input)
	if err != nil {
		return Delivery{}, err
	}
	s.notifyDelivery(ctx, input.DeliveryInput, delivery, delivery.ReviewerUserID, notification.EventTaskDeliverySubmitted)
	return delivery, nil
}

func (s *Service) ReviewDelivery(ctx context.Context, input ReviewDeliveryInput) (Delivery, error) {
	return s.resolveDelivery(ctx, input, false)
}

func (s *Service) WithdrawDelivery(ctx context.Context, input ReviewDeliveryInput) (Delivery, error) {
	return s.resolveDelivery(ctx, input, true)
}

func (s *Service) resolveDelivery(ctx context.Context, input ReviewDeliveryInput, withdraw bool) (Delivery, error) {
	if err := validateDeliveryScope(input.DeliveryInput); err != nil {
		return Delivery{}, err
	}
	input, err := normalizeDeliveryReview(input, withdraw)
	if err != nil {
		return Delivery{}, err
	}
	permission := project.PermissionReviewTaskDelivery
	if withdraw {
		permission = project.PermissionSubmitTaskDelivery
	}
	if err := s.projectAuthorizer.Require(ctx, input.Identity, input.ProjectID, permission); err != nil {
		return Delivery{}, err
	}
	delivery, err := s.repository.resolveDelivery(ctx, input, withdraw)
	if err != nil {
		return Delivery{}, err
	}
	recipient, event := delivery.SubmittedByUserID, notification.EventTaskDeliveryReviewed
	if withdraw {
		recipient, event = delivery.ReviewerUserID, notification.EventTaskDeliveryWithdrawn
	}
	s.notifyDelivery(ctx, input.DeliveryInput, delivery, recipient, event)
	return delivery, nil
}

func (s *Service) authorizeDeliveryRead(ctx context.Context, input DeliveryInput) error {
	if err := validateDeliveryScope(input); err != nil {
		return err
	}
	if err := s.projectAuthorizer.Require(ctx, input.Identity, input.ProjectID, project.PermissionViewTaskDeliveries); err != nil {
		return err
	}
	if _, err := s.projectRepository.GetForTenant(ctx, input.Identity.TenantID, input.ProjectID); err != nil {
		return err
	}
	_, err := s.repository.GetForProject(ctx, input.Identity.TenantID, input.ProjectID, input.TaskID)
	return err
}

func (s *Service) GetDelivery(ctx context.Context, input DeliveryInput, id string) (Delivery, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Delivery{}, ErrInvalidID
	}
	if err := s.authorizeDeliveryRead(ctx, input); err != nil {
		return Delivery{}, err
	}
	return s.repository.getDelivery(ctx, input, id)
}

func (s *Service) ListDeliveries(ctx context.Context, input DeliveryInput, limit int, cursor string) (DeliveryPage, error) {
	if limit < 1 || limit > 100 {
		return DeliveryPage{}, ErrInvalidID
	}
	var before int64
	if cursor != "" {
		parsed, err := strconv.ParseInt(cursor, 10, 32)
		if err != nil || parsed < 1 {
			return DeliveryPage{}, ErrInvalidID
		}
		before = parsed
	}
	if err := s.authorizeDeliveryRead(ctx, input); err != nil {
		return DeliveryPage{}, err
	}
	items, err := s.repository.listDeliveries(ctx, input, limit+1, int32(before))
	if err != nil {
		return DeliveryPage{}, err
	}
	page := DeliveryPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next := strconv.FormatInt(int64(page.Items[limit-1].Revision), 10)
		page.NextCursor = &next
	}
	return page, nil
}

func (s *Service) notifyDelivery(ctx context.Context, input DeliveryInput, delivery Delivery, recipient string, event notification.EventType) {
	if s.notificationSender == nil || recipient == input.Identity.UserID {
		return
	}
	// A saved reviewer/submitter may have left the project since submission.
	if _, err := s.projectRepository.GetMemberRole(ctx, input.Identity.TenantID, input.ProjectID, recipient); err != nil {
		return
	}
	title := "Task delivery submitted for review"
	if event == notification.EventTaskDeliveryReviewed {
		title = "Task delivery " + string(delivery.Status)
	}
	if event == notification.EventTaskDeliveryWithdrawn {
		title = "Task delivery withdrawn"
	}
	if err := s.notificationSender.Send(ctx, notification.SendInput{
		TenantID: input.Identity.TenantID, RecipientUserID: recipient, Type: event,
		ActorUserID: auditID(input.Identity.UserID), ProjectID: auditID(input.ProjectID), TaskID: auditID(input.TaskID),
		Title: title, Data: map[string]any{"deliveryId": delivery.ID, "revision": delivery.Revision, "status": delivery.Status},
	}); err != nil {
		log.Printf("notify delivery %s: %v", delivery.ID, err)
	}
}
