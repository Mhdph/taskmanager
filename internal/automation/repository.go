package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/mhdph/taskmanager/internal/database/generated"
)

// Repository owns automation rule persistence.
type Repository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewRepository(
	pool *pgxpool.Pool,
	queries *db.Queries,
) *Repository {
	return &Repository{
		pool:    pool,
		queries: queries,
	}
}

func parseUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidID,
			err,
		)
	}

	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func uuidValue(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}

	return uuid.UUID(value.Bytes).String()
}

func ruleFromDB(row db.AutomationRule) (Rule, error) {
	trigger, err := decodeTrigger(row.TriggerConfig)
	if err != nil {
		return Rule{}, fmt.Errorf(
			"decode automation rule trigger: %w",
			err,
		)
	}

	actions, err := decodeActions(row.Actions)
	if err != nil {
		return Rule{}, fmt.Errorf(
			"decode automation rule actions: %w",
			err,
		)
	}

	return Rule{
		ID:        uuidValue(row.ID),
		TenantID:  row.TenantID,
		ProjectID: uuidValue(row.ProjectID),
		Name:      row.Name,

		TriggerType:   TriggerType(row.TriggerType),
		TriggerConfig: trigger,

		Actions: actions,

		Enabled: row.Enabled,

		CreatedByUserID: row.CreatedByUserID,

		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

type CreateParams struct {
	TenantID  string
	ProjectID string

	Name string

	TriggerType   TriggerType
	TriggerConfig Trigger

	Actions []Action

	Enabled bool

	CreatedByUserID string
}

func (r *Repository) Create(
	ctx context.Context,
	params CreateParams,
) (Rule, error) {
	projectID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Rule{}, err
	}

	triggerConfig, err := params.TriggerConfig.encode()
	if err != nil {
		return Rule{}, fmt.Errorf(
			"encode automation rule trigger: %w",
			err,
		)
	}

	actions, err := encodeActions(params.Actions)
	if err != nil {
		return Rule{}, fmt.Errorf(
			"encode automation rule actions: %w",
			err,
		)
	}

	row, err := r.queries.CreateAutomationRule(
		ctx,
		db.CreateAutomationRuleParams{
			ID: pgtype.UUID{
				Bytes: uuid.New(),
				Valid: true,
			},

			TenantID:  params.TenantID,
			ProjectID: projectID,

			Name: params.Name,

			TriggerType:   string(params.TriggerType),
			TriggerConfig: triggerConfig,

			Actions: actions,

			Enabled: params.Enabled,

			CreatedByUserID: params.CreatedByUserID,
		},
	)
	if err != nil {
		return Rule{}, fmt.Errorf(
			"create automation rule: %w",
			err,
		)
	}

	return ruleFromDB(row)
}

func (r *Repository) Get(
	ctx context.Context,
	tenantID string,
	projectID string,
	ruleID string,
) (Rule, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return Rule{}, err
	}

	ruleUUID, err := parseUUID(ruleID)
	if err != nil {
		return Rule{}, err
	}

	row, err := r.queries.GetAutomationRule(
		ctx,
		db.GetAutomationRuleParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        ruleUUID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rule{}, ErrRuleNotFound
		}

		return Rule{}, fmt.Errorf(
			"get automation rule: %w",
			err,
		)
	}

	return ruleFromDB(row)
}

func (r *Repository) List(
	ctx context.Context,
	tenantID string,
	projectID string,
) ([]Rule, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}

	rows, err := r.queries.ListAutomationRulesForProject(
		ctx,
		db.ListAutomationRulesForProjectParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list automation rules: %w",
			err,
		)
	}

	rules := make([]Rule, 0, len(rows))

	for _, row := range rows {
		rule, err := ruleFromDB(row)
		if err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

type UpdateParams struct {
	TenantID  string
	ProjectID string
	RuleID    string

	Name *string

	TriggerConfig *Trigger

	Actions *[]Action

	Enabled *bool
}

func (r *Repository) Update(
	ctx context.Context,
	params UpdateParams,
) (Rule, error) {
	projectUUID, err := parseUUID(params.ProjectID)
	if err != nil {
		return Rule{}, err
	}

	ruleUUID, err := parseUUID(params.RuleID)
	if err != nil {
		return Rule{}, err
	}

	var name pgtype.Text
	if params.Name != nil {
		name = pgtype.Text{
			String: *params.Name,
			Valid:  true,
		}
	}

	var triggerConfig []byte
	if params.TriggerConfig != nil {
		triggerConfig, err = params.TriggerConfig.encode()
		if err != nil {
			return Rule{}, fmt.Errorf(
				"encode automation rule trigger: %w",
				err,
			)
		}
	}

	var actions []byte
	if params.Actions != nil {
		actions, err = encodeActions(*params.Actions)
		if err != nil {
			return Rule{}, fmt.Errorf(
				"encode automation rule actions: %w",
				err,
			)
		}
	}

	var enabled pgtype.Bool
	if params.Enabled != nil {
		enabled = pgtype.Bool{
			Bool:  *params.Enabled,
			Valid: true,
		}
	}

	row, err := r.queries.UpdateAutomationRule(
		ctx,
		db.UpdateAutomationRuleParams{
			TenantID:  params.TenantID,
			ProjectID: projectUUID,
			ID:        ruleUUID,

			Name: name,

			TriggerConfig: triggerConfig,
			Actions:       actions,
			Enabled:       enabled,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rule{}, ErrRuleNotFound
		}

		return Rule{}, fmt.Errorf(
			"update automation rule: %w",
			err,
		)
	}

	return ruleFromDB(row)
}

func (r *Repository) Delete(
	ctx context.Context,
	tenantID string,
	projectID string,
	ruleID string,
) error {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return err
	}

	ruleUUID, err := parseUUID(ruleID)
	if err != nil {
		return err
	}

	deleted, err := r.queries.DeleteAutomationRule(
		ctx,
		db.DeleteAutomationRuleParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			ID:        ruleUUID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"delete automation rule: %w",
			err,
		)
	}

	if deleted == 0 {
		return ErrRuleNotFound
	}

	return nil
}

func (r *Repository) Count(
	ctx context.Context,
	tenantID string,
	projectID string,
) (int64, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return 0, err
	}

	count, err := r.queries.CountAutomationRulesForProject(
		ctx,
		db.CountAutomationRulesForProjectParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"count automation rules: %w",
			err,
		)
	}

	return count, nil
}

func (r *Repository) ListEnabledByTrigger(
	ctx context.Context,
	tenantID string,
	projectID string,
	triggerType TriggerType,
) ([]Rule, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}

	rows, err :=
		r.queries.ListEnabledAutomationRulesForProjectAndTrigger(
			ctx,
			db.ListEnabledAutomationRulesForProjectAndTriggerParams{
				TenantID:    tenantID,
				ProjectID:   projectUUID,
				TriggerType: string(triggerType),
			},
		)
	if err != nil {
		return nil, fmt.Errorf(
			"list automation rules for trigger: %w",
			err,
		)
	}

	rules := make([]Rule, 0, len(rows))

	for _, row := range rows {
		rule, err := ruleFromDB(row)
		if err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

func (r *Repository) ListEnabledDue(
	ctx context.Context,
) ([]Rule, error) {
	rows, err := r.queries.ListEnabledDueAutomationRules(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"list due automation rules: %w",
			err,
		)
	}

	rules := make([]Rule, 0, len(rows))

	for _, row := range rows {
		rule, err := ruleFromDB(row)
		if err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// DueTask is the slice of a task the due-date sweep needs.
type DueTask struct {
	ID    string
	Title string
	DueAt time.Time

	AssigneeUserID  *string
	CreatedByUserID string
}

func (r *Repository) ListTasksDueSoon(
	ctx context.Context,
	tenantID string,
	projectID string,
	until time.Time,
) ([]DueTask, error) {
	projectUUID, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}

	rows, err := r.queries.ListTasksDueSoon(
		ctx,
		db.ListTasksDueSoonParams{
			TenantID:  tenantID,
			ProjectID: projectUUID,
			DueAt: pgtype.Timestamptz{
				Time:  until,
				Valid: true,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list tasks due soon: %w",
			err,
		)
	}

	tasks := make([]DueTask, 0, len(rows))

	for _, row := range rows {
		task := DueTask{
			ID:              uuidValue(row.ID),
			Title:           row.Title,
			CreatedByUserID: row.CreatedByUserID,
		}

		if row.AssigneeUserID.Valid {
			value := row.AssigneeUserID.String
			task.AssigneeUserID = &value
		}

		if row.DueAt.Valid {
			task.DueAt = row.DueAt.Time
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// ClaimDueDelivery records that a rule reminded about one task deadline.
// It returns true only for the caller that created the row, so a repeated
// sweep never sends the same reminder twice. A changed due date is a new
// delivery and is reminded again.
func (r *Repository) ClaimDueDelivery(
	ctx context.Context,
	ruleID string,
	taskID string,
	dueAt time.Time,
) (bool, error) {
	ruleUUID, err := parseUUID(ruleID)
	if err != nil {
		return false, err
	}

	taskUUID, err := parseUUID(taskID)
	if err != nil {
		return false, err
	}

	inserted, err := r.queries.ClaimAutomationDueDelivery(
		ctx,
		db.ClaimAutomationDueDeliveryParams{
			RuleID: ruleUUID,
			TaskID: taskUUID,
			DueAt: pgtype.Timestamptz{
				Time:  dueAt,
				Valid: true,
			},
		},
	)
	if err != nil {
		return false, fmt.Errorf(
			"claim automation due delivery: %w",
			err,
		)
	}

	return inserted == 1, nil
}
