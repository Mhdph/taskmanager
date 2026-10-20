package view

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

// Service manages saved views: named, shared task views attached to a
// project and rendered as its tabs.
type Service struct {
	repository *Repository
	authorizer *project.Authorizer
}

func NewService(
	repository *Repository,
	authorizer *project.Authorizer,
) *Service {
	return &Service{
		repository: repository,
		authorizer: authorizer,
	}
}

// backlogFilterValue is the saved-view value that selects the tasks
// without a sprint.
const backlogFilterValue = "backlog"

const (
	// maxFilters caps the size of one saved view's filter list.
	maxFilters = 10

	// maxFilterValues caps one condition's value list; ids (user ids,
	// status ids...) are opaque strings here.
	maxFilterValues = 30

	maxFilterValueLength = 128

	maxNameLength = 64
)

// filterFields mirrors the filter engine's field list (view-engine
// FIELDS): the fields saved views can persist.
var filterFields = map[string]struct{}{
	"status":   {},
	"assignee": {},
	"priority": {},
	"due":      {},
	"project":  {},
	"labels":   {},
	"created":  {},
	"updated":  {},
	"sprint":   {},
}

var viewTypes = map[string]struct{}{
	"list":  {},
	"board": {},
	"table": {},
}

type ListInput struct {
	Identity auth.Identity

	ProjectID string
}

func (s *Service) List(
	ctx context.Context,
	input ListInput,
) ([]SavedView, error) {
	if err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewProject,
	); err != nil {
		return nil, fmt.Errorf(
			"authorize list views: %w",
			err,
		)
	}

	views, err := s.repository.List(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list views: %w",
			err,
		)
	}

	return views, nil
}

type GetInput struct {
	Identity auth.Identity

	ProjectID string
	ViewID    string
}

func (s *Service) Get(
	ctx context.Context,
	input GetInput,
) (SavedView, error) {
	if err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewProject,
	); err != nil {
		return SavedView{}, fmt.Errorf(
			"authorize view read: %w",
			err,
		)
	}

	item, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.ViewID,
	)
	if err != nil {
		return SavedView{}, fmt.Errorf(
			"get view: %w",
			err,
		)
	}

	return item, nil
}

type CreateInput struct {
	Identity auth.Identity

	ProjectID string

	Name     string
	ViewType string

	Filters []Filter
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (SavedView, error) {
	// Saved views shape the workspace for everyone; creating needs task
	// edit access rather than read-only visibility.
	if err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	); err != nil {
		return SavedView{}, fmt.Errorf(
			"authorize view create: %w",
			err,
		)
	}

	name, err := normalizeName(input.Name)
	if err != nil {
		return SavedView{}, err
	}

	viewType, err := normalizeViewType(input.ViewType)
	if err != nil {
		return SavedView{}, err
	}

	filters, err := validateFilters(input.Filters)
	if err != nil {
		return SavedView{}, err
	}

	item, err := s.repository.Create(
		ctx,
		CreateParams{
			ID:        uuid.NewString(),
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,

			Name:     name,
			ViewType: viewType,

			Filters: filters,

			CreatedByUserID: input.Identity.UserID,
		},
	)
	if err != nil {
		if isViewConflict(err) {
			return SavedView{}, ErrViewConflict
		}

		return SavedView{}, fmt.Errorf(
			"create view: %w",
			err,
		)
	}

	return item, nil
}

type UpdateInput struct {
	Identity auth.Identity

	ProjectID string
	ViewID    string

	Name     *string
	ViewType *string
	Filters  *[]Filter
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (SavedView, error) {
	if err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	); err != nil {
		return SavedView{}, fmt.Errorf(
			"authorize view update: %w",
			err,
		)
	}

	params := UpdateParams{
		TenantID:  input.Identity.TenantID,
		ProjectID: input.ProjectID,
		ID:        input.ViewID,
	}

	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return SavedView{}, err
		}

		params.Name = &name
	}

	if input.ViewType != nil {
		viewType, err := normalizeViewType(*input.ViewType)
		if err != nil {
			return SavedView{}, err
		}

		params.ViewType = &viewType
	}

	if input.Filters != nil {
		filters, err := validateFilters(*input.Filters)
		if err != nil {
			return SavedView{}, err
		}

		params.Filters = &filters
	}

	if params.Name == nil &&
		params.ViewType == nil &&
		params.Filters == nil {

		return SavedView{}, ErrNothingToUpdate
	}

	item, err := s.repository.Update(
		ctx,
		params,
	)
	if err != nil {
		if isViewConflict(err) {
			return SavedView{}, ErrViewConflict
		}

		return SavedView{}, fmt.Errorf(
			"update view: %w",
			err,
		)
	}

	return item, nil
}

type DeleteInput struct {
	Identity auth.Identity

	ProjectID string
	ViewID    string
}

func (s *Service) Delete(
	ctx context.Context,
	input DeleteInput,
) error {
	if err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionEditTask,
	); err != nil {
		return fmt.Errorf(
			"authorize view delete: %w",
			err,
		)
	}

	return s.repository.Delete(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.ViewID,
	)
}

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

func normalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)

	if name == "" {
		return "", ErrViewNameRequired
	}

	if len([]rune(name)) > maxNameLength {
		return "", ErrViewNameRequired
	}

	return name, nil
}

func normalizeViewType(value string) (string, error) {
	viewType := strings.TrimSpace(value)

	if viewType == "" {
		viewType = "list"
	}

	if _, ok := viewTypes[viewType]; !ok {
		return "", ErrViewInvalidType
	}

	return viewType, nil
}

func validateFilters(
	filters []Filter,
) ([]Filter, error) {
	if len(filters) > maxFilters {
		return nil, ErrViewFiltersTooMany
	}

	validated := make(
		[]Filter,
		0,
		len(filters),
	)

	for _, filter := range filters {
		if _, ok := filterFields[filter.Field]; !ok {
			return nil, fmt.Errorf(
				"%w: unknown field %q",
				ErrViewInvalidFilter,
				filter.Field,
			)
		}

		if filter.Operator != "" &&
			filter.Operator != "is" &&
			filter.Operator != "not" {

			return nil, fmt.Errorf(
				"%w: unknown operator %q",
				ErrViewInvalidFilter,
				filter.Operator,
			)
		}

		if len(filter.Values) > maxFilterValues {
			return nil, fmt.Errorf(
				"%w: too many values",
				ErrViewInvalidFilter,
			)
		}

		values := make(
			[]string,
			0,
			len(filter.Values),
		)

		for _, value := range filter.Values {
			if len([]rune(value)) > maxFilterValueLength {
				return nil, fmt.Errorf(
					"%w: value too long",
					ErrViewInvalidFilter,
				)
			}

			// A sprint filter points at one sprint or at the backlog.
			if filter.Field == "sprint" &&
				value != backlogFilterValue {

				if _, err := uuid.Parse(value); err != nil {
					return nil, fmt.Errorf(
						"%w: invalid sprint %q",
						ErrViewInvalidFilter,
						value,
					)
				}
			}

			values = append(
				values,
				value,
			)
		}

		operator := filter.Operator

		if operator == "" {
			operator = "is"
		}

		validated = append(
			validated,
			Filter{
				Field:    filter.Field,
				Operator: operator,
				Values:   values,
			},
		)
	}

	return validated, nil
}
