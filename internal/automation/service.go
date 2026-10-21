package automation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

const (
	// eventQueueSize bounds pending task events. A full queue drops new
	// events instead of slowing down the task write that produced them;
	// automation is best-effort by design.
	eventQueueSize = 256

	// workerCount is how many events are evaluated concurrently.
	workerCount = 2

	// defaultMaxRulesPerProject caps stored rules unless the caller
	// configures another limit.
	defaultMaxRulesPerProject = 20
)

type NotificationSender interface {
	Send(
		ctx context.Context,
		input notification.SendInput,
	) error
}

// TaskUpdater applies rule actions to tasks. It is implemented by
// task.Service; defining the interface here keeps task free of an
// automation dependency.
type TaskUpdater interface {
	Update(ctx context.Context, input task.UpdateInput) (task.Task, error)
}

type Service struct {
	repository *Repository

	taskRepository    *task.Repository
	projectRepository *project.Repository
	projectAuthorizer *project.Authorizer

	notificationSender NotificationSender

	taskUpdater TaskUpdater

	maxRulesPerProject int

	events chan task.Event
}

func NewService(
	repository *Repository,
	taskRepository *task.Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	notificationSender NotificationSender,
	maxRulesPerProject int,
) *Service {
	if maxRulesPerProject <= 0 {
		maxRulesPerProject = defaultMaxRulesPerProject
	}

	return &Service{
		repository: repository,

		taskRepository:    taskRepository,
		projectRepository: projectRepository,
		projectAuthorizer: projectAuthorizer,

		notificationSender: notificationSender,

		maxRulesPerProject: maxRulesPerProject,

		events: make(chan task.Event, eventQueueSize),
	}
}

// SetTaskUpdater wires the task service after construction: the task
// service needs the automation notifier, so the two services cannot be
// passed to each other in their constructors.
func (s *Service) SetTaskUpdater(updater TaskUpdater) {
	s.taskUpdater = updater
}

// -------------------------------------------------------------------------
// Rule CRUD
// -------------------------------------------------------------------------

type CreateInput struct {
	Identity auth.Identity

	ProjectID string

	Name string

	TriggerType   TriggerType
	TriggerConfig Trigger

	Actions []Action

	Enabled *bool
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Rule, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageAutomation,
	); err != nil {
		return Rule{}, fmt.Errorf(
			"authorize automation rule creation: %w",
			err,
		)
	}

	name, err := normalizeRuleName(input.Name)
	if err != nil {
		return Rule{}, err
	}

	trigger, actions, err := s.validateRule(
		ctx,
		input.Identity,
		input.ProjectID,
		input.TriggerType,
		input.TriggerConfig,
		input.Actions,
	)
	if err != nil {
		return Rule{}, err
	}

	count, err := s.repository.Count(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
	)
	if err != nil {
		return Rule{}, err
	}

	if count >= int64(s.maxRulesPerProject) {
		return Rule{}, ErrTooManyRules
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	return s.repository.Create(
		ctx,
		CreateParams{
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,

			Name: name,

			TriggerType:   input.TriggerType,
			TriggerConfig: trigger,

			Actions: actions,

			Enabled: enabled,

			CreatedByUserID: input.Identity.UserID,
		},
	)
}

type UpdateInput struct {
	Identity auth.Identity

	ProjectID string
	RuleID    string

	Name *string

	TriggerConfig *Trigger

	Actions *[]Action

	Enabled *bool
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Rule, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionManageAutomation,
	); err != nil {
		return Rule{}, fmt.Errorf(
			"authorize automation rule update: %w",
			err,
		)
	}

	existing, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.RuleID,
	)
	if err != nil {
		return Rule{}, err
	}

	params := UpdateParams{
		TenantID:  input.Identity.TenantID,
		ProjectID: input.ProjectID,
		RuleID:    input.RuleID,

		Enabled: input.Enabled,
	}

	if input.Name != nil {
		name, err := normalizeRuleName(*input.Name)
		if err != nil {
			return Rule{}, err
		}

		params.Name = &name
	}

	if input.TriggerConfig != nil || input.Actions != nil {
		trigger := existing.TriggerConfig
		if input.TriggerConfig != nil {
			trigger = *input.TriggerConfig
		}

		actions := existing.Actions
		if input.Actions != nil {
			actions = *input.Actions
		}

		normalizedTrigger, normalizedActions, err := s.validateRule(
			ctx,
			input.Identity,
			input.ProjectID,
			existing.TriggerType,
			trigger,
			actions,
		)
		if err != nil {
			return Rule{}, err
		}

		params.TriggerConfig = &normalizedTrigger
		params.Actions = &normalizedActions
	}

	return s.repository.Update(ctx, params)
}

func (s *Service) List(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]Rule, error) {
	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewAutomation,
	); err != nil {
		return nil, fmt.Errorf(
			"authorize automation rule list: %w",
			err,
		)
	}

	return s.repository.List(
		ctx,
		identity.TenantID,
		projectID,
	)
}

func (s *Service) Delete(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	ruleID string,
) error {
	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionManageAutomation,
	); err != nil {
		return fmt.Errorf(
			"authorize automation rule deletion: %w",
			err,
		)
	}

	return s.repository.Delete(
		ctx,
		identity.TenantID,
		projectID,
		ruleID,
	)
}

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

func normalizeRuleName(name string) (string, error) {
	value := strings.TrimSpace(name)

	if value == "" ||
		utf8.RuneCountInString(value) > maxNameLength {
		return "", ErrInvalidName
	}

	return value, nil
}

func (s *Service) validateRule(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	triggerType TriggerType,
	trigger Trigger,
	actions []Action,
) (Trigger, []Action, error) {
	if !triggerType.Valid() {
		return Trigger{}, nil, ErrInvalidTrigger
	}

	trigger = trigger.normalize(triggerType)

	if err := s.validateTrigger(
		ctx,
		identity.TenantID,
		projectID,
		triggerType,
		trigger,
	); err != nil {
		return Trigger{}, nil, err
	}

	if len(actions) == 0 || len(actions) > MaxActionsPerRule {
		return Trigger{}, nil, fmt.Errorf(
			"%w: expected 1 to %d actions",
			ErrInvalidAction,
			MaxActionsPerRule,
		)
	}

	normalized := make([]Action, 0, len(actions))

	for _, action := range actions {
		value, err := s.validateAction(
			ctx,
			identity.TenantID,
			projectID,
			action,
		)
		if err != nil {
			return Trigger{}, nil, err
		}

		normalized = append(normalized, value)
	}

	return trigger, normalized, nil
}

func (s *Service) validateTrigger(
	ctx context.Context,
	tenantID string,
	projectID string,
	triggerType TriggerType,
	trigger Trigger,
) error {
	switch triggerType {
	case TriggerTaskStatusChanged:
		if trigger.FromStatusID != nil {
			if err := s.requireStatus(
				ctx,
				tenantID,
				projectID,
				*trigger.FromStatusID,
			); err != nil {
				return err
			}
		}

		if trigger.ToStatusID != nil {
			if err := s.requireStatus(
				ctx,
				tenantID,
				projectID,
				*trigger.ToStatusID,
			); err != nil {
				return err
			}
		}

	case TriggerTaskCreated:

	case TriggerTaskAssigned:
		if trigger.ToUserID != nil {
			if err := s.requireMember(
				ctx,
				tenantID,
				projectID,
				*trigger.ToUserID,
				ErrInvalidTriggerConfig,
			); err != nil {
				return err
			}
		}

	case TriggerTaskDue:
		if trigger.DaysBefore != nil {
			days := *trigger.DaysBefore
			if days < 1 || days > MaxDueDaysBefore {
				return fmt.Errorf(
					"%w: daysBefore must be between 1 and %d",
					ErrInvalidTriggerConfig,
					MaxDueDaysBefore,
				)
			}
		}
	}

	return nil
}

func (s *Service) requireStatus(
	ctx context.Context,
	tenantID string,
	projectID string,
	statusID string,
) error {
	_, err := s.projectRepository.GetStatus(
		ctx,
		tenantID,
		projectID,
		statusID,
	)
	if err != nil {
		if errors.Is(err, project.ErrStatusNotFound) {
			return fmt.Errorf(
				"%w: unknown status %q",
				ErrInvalidTriggerConfig,
				statusID,
			)
		}

		return fmt.Errorf(
			"validate automation status: %w",
			err,
		)
	}

	return nil
}

func (s *Service) requireMember(
	ctx context.Context,
	tenantID string,
	projectID string,
	userID string,
	kind error,
) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: empty user id", kind)
	}

	_, err := s.projectRepository.GetMemberRole(
		ctx,
		tenantID,
		projectID,
		userID,
	)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return fmt.Errorf(
				"%w: %q is not a project member",
				kind,
				userID,
			)
		}

		return fmt.Errorf(
			"validate automation member: %w",
			err,
		)
	}

	return nil
}

func (s *Service) validateAction(
	ctx context.Context,
	tenantID string,
	projectID string,
	action Action,
) (Action, error) {
	switch action.Type {
	case ActionSetAssignee:
		switch action.Assignee {
		case AssigneeUser:
			if action.UserID == nil {
				return Action{}, fmt.Errorf(
					"%w: set_assignee requires userId",
					ErrInvalidAction,
				)
			}

			if err := s.requireMember(
				ctx,
				tenantID,
				projectID,
				*action.UserID,
				ErrInvalidAction,
			); err != nil {
				return Action{}, err
			}

			userID := *action.UserID

			return Action{
				Type:     ActionSetAssignee,
				Assignee: AssigneeUser,
				UserID:   &userID,
			}, nil

		case AssigneeCreator:
			return Action{
				Type:     ActionSetAssignee,
				Assignee: AssigneeCreator,
			}, nil

		default:
			return Action{}, fmt.Errorf(
				"%w: set_assignee requires assignee user or creator",
				ErrInvalidAction,
			)
		}

	case ActionNotify:
		recipients := make([]Recipient, 0, len(action.Recipients))
		seenRecipients := make(map[Recipient]struct{}, len(action.Recipients))

		for _, recipient := range action.Recipients {
			switch recipient {
			case RecipientAssignee,
				RecipientCreator,
				RecipientWatchers:

			default:
				return Action{}, fmt.Errorf(
					"%w: unknown notification recipient %q",
					ErrInvalidAction,
					recipient,
				)
			}

			if _, ok := seenRecipients[recipient]; ok {
				continue
			}

			seenRecipients[recipient] = struct{}{}
			recipients = append(recipients, recipient)
		}

		userIDs := make([]string, 0, len(action.UserIDs))
		seenUsers := make(map[string]struct{}, len(action.UserIDs))

		for _, userID := range action.UserIDs {
			userID = strings.TrimSpace(userID)
			if userID == "" {
				continue
			}

			if _, ok := seenUsers[userID]; ok {
				continue
			}

			if err := s.requireMember(
				ctx,
				tenantID,
				projectID,
				userID,
				ErrInvalidAction,
			); err != nil {
				return Action{}, err
			}

			seenUsers[userID] = struct{}{}
			userIDs = append(userIDs, userID)
		}

		if len(recipients) == 0 && len(userIDs) == 0 {
			return Action{}, fmt.Errorf(
				"%w: notify needs a recipient",
				ErrInvalidAction,
			)
		}

		title := strings.TrimSpace(action.Title)

		if utf8.RuneCountInString(title) >
			maxNotificationTitleLength {
			return Action{}, fmt.Errorf(
				"%w: notification title is too long",
				ErrInvalidAction,
			)
		}

		var body *string

		if action.Body != nil {
			value := strings.TrimSpace(*action.Body)

			if utf8.RuneCountInString(value) >
				maxNotificationBodyLength {
				return Action{}, fmt.Errorf(
					"%w: notification body is too long",
					ErrInvalidAction,
				)
			}

			if value != "" {
				body = &value
			}
		}

		return Action{
			Type: ActionNotify,

			Recipients: recipients,
			UserIDs:    userIDs,

			Title: title,
			Body:  body,
		}, nil

	default:
		return Action{}, fmt.Errorf(
			"%w: unknown action type %q",
			ErrInvalidAction,
			action.Type,
		)
	}
}

// -------------------------------------------------------------------------
// Event evaluation
// -------------------------------------------------------------------------

// HandleTaskEvent queues a task event for evaluation. It never blocks the
// task write: when the queue is full the event is dropped and logged.
func (s *Service) HandleTaskEvent(
	ctx context.Context,
	event task.Event,
) {
	select {
	case s.events <- event:
	default:
		log.Printf(
			"automation: queue full, dropping %s event for task %s",
			event.Kind,
			event.TaskID,
		)
	}
}

// Run starts the event workers and the due-date sweep. It runs until the
// context is canceled.
func (s *Service) Run(
	ctx context.Context,
	sweepInterval time.Duration,
) {
	if sweepInterval <= 0 {
		sweepInterval = 15 * time.Minute
	}

	var workers sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		workers.Add(1)

		go func() {
			defer workers.Done()
			s.runWorker(ctx)
		}()
	}

	// Run once at startup so a restart catches deadlines that are already
	// inside the next reminder window, then keep sweeping.
	s.sweepDue(ctx)

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return
		case <-ticker.C:
			s.sweepDue(ctx)
		}
	}
}

func (s *Service) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.events:
			s.handleEvent(ctx, event)
		}
	}
}

func (s *Service) handleEvent(
	ctx context.Context,
	event task.Event,
) {
	triggerType := triggerForEvent(event.Kind)
	if triggerType == "" {
		return
	}

	rules, err := s.repository.ListEnabledByTrigger(
		ctx,
		event.TenantID,
		event.ProjectID,
		triggerType,
	)
	if err != nil {
		log.Printf(
			"automation: list rules for task %s: %v",
			event.TaskID,
			err,
		)
		return
	}

	for _, rule := range rules {
		if !rule.matches(event) {
			continue
		}

		s.applyActions(ctx, rule, event)
	}
}

func triggerForEvent(kind task.EventKind) TriggerType {
	switch kind {
	case task.EventTaskCreated:
		return TriggerTaskCreated
	case task.EventTaskStatusChanged:
		return TriggerTaskStatusChanged
	case task.EventTaskAssigned:
		return TriggerTaskAssigned
	case task.EventTaskDue:
		return TriggerTaskDue
	default:
		return ""
	}
}

func (r Rule) matches(event task.Event) bool {
	switch r.TriggerType {
	case TriggerTaskStatusChanged:
		if r.TriggerConfig.FromStatusID != nil &&
			(event.FromStatusID == nil ||
				*r.TriggerConfig.FromStatusID != *event.FromStatusID) {
			return false
		}

		if r.TriggerConfig.ToStatusID != nil &&
			(event.ToStatusID == nil ||
				*r.TriggerConfig.ToStatusID != *event.ToStatusID) {
			return false
		}

		return true

	case TriggerTaskCreated:
		if r.TriggerConfig.HasAssignee != nil {
			return *r.TriggerConfig.HasAssignee ==
				(event.AssigneeUserID != nil)
		}

		return true

	case TriggerTaskAssigned:
		if r.TriggerConfig.ToUserID != nil {
			return event.AssigneeUserID != nil &&
				*r.TriggerConfig.ToUserID == *event.AssigneeUserID
		}

		return true

	case TriggerTaskDue:
		return true

	default:
		return false
	}
}

func (s *Service) applyActions(
	ctx context.Context,
	rule Rule,
	event task.Event,
) {
	for _, action := range rule.Actions {
		switch action.Type {
		case ActionSetAssignee:
			event = s.applyAssignment(ctx, rule, action, event)

		case ActionNotify:
			s.notify(ctx, rule, action, event)

		default:
			log.Printf(
				"automation: rule %s has unknown action %q",
				rule.ID,
				action.Type,
			)
		}
	}
}

func (s *Service) applyAssignment(
	ctx context.Context,
	rule Rule,
	action Action,
	event task.Event,
) task.Event {
	if s.taskUpdater == nil {
		log.Printf(
			"automation: rule %s cannot assign, task updater is not wired",
			rule.ID,
		)
		return event
	}

	var target string

	switch action.Assignee {
	case AssigneeUser:
		if action.UserID == nil {
			return event
		}

		target = *action.UserID

	case AssigneeCreator:
		target = event.CreatedByUserID

	default:
		return event
	}

	if target == "" {
		return event
	}

	if event.AssigneeUserID != nil &&
		*event.AssigneeUserID == target {
		return event
	}

	actorUserID := event.ActorUserID

	updated, err := s.taskUpdater.Update(
		ctx,
		task.UpdateInput{
			Identity: auth.Identity{
				UserID:   actorUserID,
				TenantID: event.TenantID,

				// The rule is trusted configuration: it applies even
				// when the acting user could not edit the task.
				Role: auth.RoleAdmin,
			},

			ProjectID: event.ProjectID,
			TaskID:    event.TaskID,

			UpdateAssignee: true,
			AssigneeUserID: &target,

			// The assignment must not start another automation pass.
			SuppressAutomation: true,
		},
	)
	if err != nil {
		log.Printf(
			"automation: rule %s assign task %s to %s: %v",
			rule.ID,
			event.TaskID,
			target,
			err,
		)

		return event
	}

	event.AssigneeUserID = updated.AssigneeUserID

	return event
}

func (s *Service) notify(
	ctx context.Context,
	rule Rule,
	action Action,
	event task.Event,
) {
	recipients := make(map[string]struct{})

	for _, recipient := range action.Recipients {
		switch recipient {
		case RecipientAssignee:
			if event.AssigneeUserID != nil &&
				*event.AssigneeUserID != "" {
				recipients[*event.AssigneeUserID] = struct{}{}
			}

		case RecipientCreator:
			if event.CreatedByUserID != "" {
				recipients[event.CreatedByUserID] = struct{}{}
			}

		case RecipientWatchers:
			watchers, err := s.taskRepository.ListWatchers(
				ctx,
				event.TenantID,
				event.ProjectID,
				event.TaskID,
			)
			if err != nil {
				log.Printf(
					"automation: rule %s list watchers: %v",
					rule.ID,
					err,
				)
				continue
			}

			for _, watcher := range watchers {
				recipients[watcher.UserID] = struct{}{}
			}
		}
	}

	for _, userID := range action.UserIDs {
		recipients[userID] = struct{}{}
	}

	if event.ActorUserID != "" {
		delete(recipients, event.ActorUserID)
	}

	title := action.Title
	if title == "" {
		title = "Automation: " + rule.Name
	}

	body := action.Body

	if body == nil {
		value := notificationBody(rule, event)
		body = &value
	}

	var actorUserID *string

	if event.ActorUserID != "" {
		value := event.ActorUserID
		actorUserID = &value
	}

	projectID := event.ProjectID
	taskID := event.TaskID

	for recipient := range recipients {
		data := map[string]any{
			"automationRuleId":   rule.ID,
			"automationRuleName": rule.Name,
			"triggerType":        string(rule.TriggerType),

			"taskId":    event.TaskID,
			"projectId": event.ProjectID,
		}

		if event.FromStatusID != nil {
			data["fromStatusId"] = *event.FromStatusID
		}

		if event.ToStatusID != nil {
			data["toStatusId"] = *event.ToStatusID
		}

		if event.DueAt != nil {
			data["dueAt"] = event.DueAt.UTC().Format(time.RFC3339)
		}

		err := s.notificationSender.Send(
			ctx,
			notification.SendInput{
				TenantID: event.TenantID,

				RecipientUserID: recipient,

				Type: notification.EventAutomation,

				ActorUserID: actorUserID,

				ProjectID: &projectID,
				TaskID:    &taskID,

				Title: title,
				Body:  body,

				Data: data,
			},
		)
		if err != nil {
			log.Printf(
				"automation: rule %s notify %s: %v",
				rule.ID,
				recipient,
				err,
			)
		}
	}
}

func notificationBody(rule Rule, event task.Event) string {
	switch rule.TriggerType {
	case TriggerTaskStatusChanged:
		if event.FromStatusName != "" && event.ToStatusName != "" {
			return fmt.Sprintf(
				"%s: %s → %s",
				event.Title,
				event.FromStatusName,
				event.ToStatusName,
			)
		}

	case TriggerTaskDue:
		if event.DueAt != nil {
			return fmt.Sprintf(
				"%s is due on %s",
				event.Title,
				event.DueAt.Format("2006-01-02"),
			)
		}
	}

	return event.Title
}

// -------------------------------------------------------------------------
// Due-date sweep
// -------------------------------------------------------------------------

func (s *Service) sweepDue(ctx context.Context) {
	rules, err := s.repository.ListEnabledDue(ctx)
	if err != nil {
		log.Printf("automation: list due rules: %v", err)
		return
	}

	now := time.Now().UTC()

	for _, rule := range rules {
		days := DefaultDueDaysBefore

		if rule.TriggerConfig.DaysBefore != nil {
			days = *rule.TriggerConfig.DaysBefore
		}

		until := now.Add(
			time.Duration(days) * 24 * time.Hour,
		)

		tasks, err := s.repository.ListTasksDueSoon(
			ctx,
			rule.TenantID,
			rule.ProjectID,
			until,
		)
		if err != nil {
			log.Printf(
				"automation: rule %s list due tasks: %v",
				rule.ID,
				err,
			)
			continue
		}

		for _, item := range tasks {
			claimed, err := s.repository.ClaimDueDelivery(
				ctx,
				rule.ID,
				item.ID,
				item.DueAt,
			)
			if err != nil {
				log.Printf(
					"automation: rule %s claim reminder for task %s: %v",
					rule.ID,
					item.ID,
					err,
				)
				continue
			}

			if !claimed {
				continue
			}

			dueAt := item.DueAt
			assignee := item.AssigneeUserID

			event := task.Event{
				Kind: task.EventTaskDue,

				TenantID:  rule.TenantID,
				ProjectID: rule.ProjectID,
				TaskID:    item.ID,

				// The rule author is the actor of a sweep action, so
				// audits and notifications have a stable identity.
				ActorUserID: rule.CreatedByUserID,

				Title:           item.Title,
				AssigneeUserID:  assignee,
				CreatedByUserID: item.CreatedByUserID,

				DueAt: &dueAt,
			}

			s.applyActions(ctx, rule, event)
		}
	}
}
