package sprint

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/audit"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

const (
	// maxSprintNameLength caps the sprint name.
	maxSprintNameLength = 120

	// maxGoalLength caps the sprint goal.
	maxGoalLength = 2000

	defaultPageSize = 50
	maxPageSize     = 100
)

// AuditRecorder records sprint events in the project activity feed.
type AuditRecorder interface {
	Record(
		ctx context.Context,
		input audit.RecordInput,
	) error
}

// Service owns the sprint rules: who may do what, which transitions are
// legal and what the public responses look like.
type Service struct {
	repository *Repository

	projectRepository *project.Repository
	projectAuthorizer *project.Authorizer

	auditRecorder AuditRecorder
}

func NewService(
	repository *Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	auditRecorder AuditRecorder,
) *Service {
	return &Service{
		repository: repository,

		projectRepository: projectRepository,
		projectAuthorizer: projectAuthorizer,

		auditRecorder: auditRecorder,
	}
}

// -------------------------------------------------------------------------
// Inputs
// -------------------------------------------------------------------------

type ListInput struct {
	Identity auth.Identity

	ProjectID string

	// State filters by lifecycle state. Nil lists every sprint.
	State *State

	Limit int

	Cursor string
}

type CreateInput struct {
	Identity auth.Identity

	ProjectID string

	Name string
	Goal *string

	PlannedStartAt *time.Time
	PlannedEndAt   *time.Time
}

type GetInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string
}

// SprintDetail is a sprint with its live totals.
type SprintDetail struct {
	Sprint

	Totals Totals
}

type UpdateInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string

	Name *string

	// Goal is replaced when GoalIsSet is true, so a client can clear it.
	Goal      *string
	GoalIsSet bool

	PlannedStartAt    *time.Time
	PlannedStartIsSet bool

	PlannedEndAt *time.Time
}

type DeleteInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string
}

type StartInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string
}

type CompleteInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string

	// CarryoverSprintID is where the unfinished work goes. Nil means the
	// backlog.
	CarryoverSprintID *string
}

type ReportInput struct {
	Identity auth.Identity

	ProjectID string
	SprintID  string
}

type SetTaskSprintInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	// SprintID is the destination. Nil returns the task to the backlog.
	SprintID *string
}

// -------------------------------------------------------------------------
// Commands
// -------------------------------------------------------------------------

func (s *Service) List(
	ctx context.Context,
	input ListInput,
) (SprintPage, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewSprints,
	); err != nil {
		return SprintPage{}, err
	}

	if input.State != nil {
		switch *input.State {
		case StatePlanned, StateActive, StateCompleted:
		default:
			return SprintPage{}, ErrInvalidState
		}
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	options := ListOptions{
		State: input.State,
		// One extra row tells the service whether another page exists.
		Limit: int32(limit) + 1,
	}

	if input.Cursor != "" {
		cursor, err := DecodeCursor(input.Cursor)
		if err != nil {
			return SprintPage{}, ErrInvalidCursor
		}

		options.Cursor = &cursor
	}

	items, err := s.repository.List(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		options,
	)
	if err != nil {
		return SprintPage{}, err
	}

	page := SprintPage{Items: items}

	if len(items) > limit {
		page.Items = items[:limit]

		last := page.Items[len(page.Items)-1]

		cursor := EncodeCursor(Cursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})

		page.NextCursor = &cursor
	}

	return page, nil
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Sprint, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageSprints,
	); err != nil {
		return Sprint{}, err
	}

	name, err := normalizeName(input.Name)
	if err != nil {
		return Sprint{}, err
	}

	goal, err := normalizeGoal(input.Goal)
	if err != nil {
		return Sprint{}, err
	}

	if input.PlannedEndAt == nil {
		return Sprint{}, ErrInvalidDateRange
	}

	if input.PlannedStartAt != nil &&
		!input.PlannedEndAt.After(*input.PlannedStartAt) {

		return Sprint{}, ErrInvalidDateRange
	}

	sprint, err := s.repository.Create(
		ctx,
		CreateParams{
			ID:              uuid.NewString(),
			TenantID:        input.Identity.TenantID,
			ProjectID:       input.ProjectID,
			Name:            name,
			Goal:            goal,
			PlannedStartAt:  input.PlannedStartAt,
			PlannedEndAt:    *input.PlannedEndAt,
			CreatedByUserID: input.Identity.UserID,
		},
	)
	if err != nil {
		return Sprint{}, err
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionSprintCreated,
		EntityType:  audit.EntitySprint,
		EntityID:    sprint.ID,
		ProjectID:   auditID(input.ProjectID),
		Metadata: map[string]any{
			"name": sprint.Name,
		},
	})

	return sprint, nil
}

func (s *Service) Get(
	ctx context.Context,
	input GetInput,
) (SprintDetail, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewSprints,
	); err != nil {
		return SprintDetail{}, err
	}

	sprint, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	)
	if err != nil {
		return SprintDetail{}, err
	}

	totals, err := s.repository.Totals(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	)
	if err != nil {
		return SprintDetail{}, err
	}

	return SprintDetail{
		Sprint: sprint,
		Totals: totals,
	}, nil
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Sprint, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageSprints,
	); err != nil {
		return Sprint{}, err
	}

	current, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	)
	if err != nil {
		return Sprint{}, err
	}

	// A completed sprint is immutable.
	if current.State == StateCompleted {
		return Sprint{}, ErrLifecycleConflict
	}

	// An active sprint keeps its planned start: only the name, the goal
	// and the planned end may change.
	if current.State == StateActive &&
		input.PlannedStartIsSet {

		return Sprint{}, ErrLifecycleConflict
	}

	params := UpdateParams{
		TenantID:  input.Identity.TenantID,
		ProjectID: input.ProjectID,
		SprintID:  input.SprintID,

		Goal:      input.Goal,
		GoalIsSet: input.GoalIsSet,

		PlannedStartAt:    input.PlannedStartAt,
		PlannedStartIsSet: input.PlannedStartIsSet,

		PlannedEndAt: input.PlannedEndAt,
	}

	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return Sprint{}, err
		}

		params.Name = &name
	}

	if input.GoalIsSet {
		goal, err := normalizeGoal(input.Goal)
		if err != nil {
			return Sprint{}, err
		}

		params.Goal = goal
	}

	// Validate the resulting window.
	plannedStart := current.PlannedStartAt
	if params.PlannedStartIsSet {
		plannedStart = params.PlannedStartAt
	}

	plannedEnd := current.PlannedEndAt
	if params.PlannedEndAt != nil {
		plannedEnd = *params.PlannedEndAt
	}

	if plannedStart != nil && !plannedEnd.After(*plannedStart) {
		return Sprint{}, ErrInvalidDateRange
	}

	sprint, err := s.repository.Update(ctx, params)
	if err != nil {
		return Sprint{}, err
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionSprintUpdated,
		EntityType:  audit.EntitySprint,
		EntityID:    sprint.ID,
		ProjectID:   auditID(input.ProjectID),
		Metadata: map[string]any{
			"name": sprint.Name,
		},
	})

	return sprint, nil
}

func (s *Service) Delete(
	ctx context.Context,
	input DeleteInput,
) error {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageSprints,
	); err != nil {
		return err
	}

	if err := s.repository.Delete(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	); err != nil {
		return err
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionSprintDeleted,
		EntityType:  audit.EntitySprint,
		EntityID:    input.SprintID,
		ProjectID:   auditID(input.ProjectID),
	})

	return nil
}

func (s *Service) Start(
	ctx context.Context,
	input StartInput,
) (Sprint, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageSprints,
	); err != nil {
		return Sprint{}, err
	}

	sprint, started, err := s.repository.Start(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	)
	if err != nil {
		return Sprint{}, err
	}

	// Repeating the start returns the sprint without a second snapshot.
	if !started {
		return sprint, nil
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionSprintStarted,
		EntityType:  audit.EntitySprint,
		EntityID:    sprint.ID,
		ProjectID:   auditID(input.ProjectID),
	})

	return sprint, nil
}

func (s *Service) Complete(
	ctx context.Context,
	input CompleteInput,
) (Sprint, Report, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageSprints,
	); err != nil {
		return Sprint{}, Report{}, err
	}

	sprint, report, completed, err := s.repository.Complete(
		ctx,
		CompleteParams{
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,
			SprintID:  input.SprintID,

			CarryoverSprintID: input.CarryoverSprintID,

			CompletedByUserID: input.Identity.UserID,
		},
	)
	if err != nil {
		return Sprint{}, Report{}, err
	}

	// Repeating the completion returns the frozen report; the carryover
	// only happens once.
	if !completed {
		return sprint, report, nil
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionSprintCompleted,
		EntityType:  audit.EntitySprint,
		EntityID:    sprint.ID,
		ProjectID:   auditID(input.ProjectID),
		Metadata: map[string]any{
			"carryoverTaskCount": report.CarryoverTaskCount,
		},
	})

	return sprint, report, nil
}

func (s *Service) Report(
	ctx context.Context,
	input ReportInput,
) (Report, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewSprints,
	); err != nil {
		return Report{}, err
	}

	return s.repository.GetReport(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.SprintID,
	)
}

func (s *Service) SetTaskSprint(
	ctx context.Context,
	input SetTaskSprintInput,
) (SprintTask, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionPlanTasks,
	); err != nil {
		return SprintTask{}, err
	}

	task, err := s.repository.AssignTask(
		ctx,
		AssignParams{
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,
			TaskID:    input.TaskID,
			SprintID:  input.SprintID,

			ActorUserID: input.Identity.UserID,
		},
	)
	if err != nil {
		return SprintTask{}, err
	}

	s.recordAudit(ctx, audit.RecordInput{
		TenantID:    input.Identity.TenantID,
		ActorUserID: input.Identity.UserID,
		Action:      audit.ActionTaskSprintChanged,
		EntityType:  audit.EntityTask,
		EntityID:    task.ID,
		ProjectID:   auditID(input.ProjectID),
		TaskID:      auditID(task.ID),
		Metadata: map[string]any{
			"sprintId": task.SprintID,
		},
	})

	return task, nil
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func normalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)

	if name == "" ||
		utf8.RuneCountInString(name) > maxSprintNameLength {

		return "", ErrInvalidName
	}

	return name, nil
}

func normalizeGoal(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	goal := strings.TrimSpace(*value)

	if goal == "" {
		return nil, nil
	}

	if utf8.RuneCountInString(goal) > maxGoalLength {
		return nil, ErrInvalidGoal
	}

	return &goal, nil
}

func (s *Service) recordAudit(
	ctx context.Context,
	input audit.RecordInput,
) {
	if s.auditRecorder == nil {
		return
	}

	if err := s.auditRecorder.Record(ctx, input); err != nil {
		// Audit recording stays best-effort: the sprint report and the
		// membership history are stored transactionally, so the activity
		// feed never becomes the source of truth.
		log.Printf(
			"record audit event %s for %s: %v",
			input.Action,
			input.EntityID,
			err,
		)
	}
}

func auditID(value string) *string {
	return &value
}

// -------------------------------------------------------------------------
// Cursor
// -------------------------------------------------------------------------

// EncodeCursor encodes the keyset cursor of the sprint list.
func EncodeCursor(cursor Cursor) string {
	value := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) +
		"|" +
		cursor.ID

	return base64.RawURLEncoding.EncodeToString(
		[]byte(value),
	)
}

// DecodeCursor decodes the keyset cursor of the sprint list.
func DecodeCursor(value string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidCursor,
			err,
		)
	}

	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return Cursor{}, ErrInvalidCursor
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}

	return Cursor{
		CreatedAt: createdAt,
		ID:        parts[1],
	}, nil
}
