package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

type CreateInput struct {
	TenantID    string
	UserID      string
	Name        string
	Description *string
}

type UpdateInput struct {
	Identity  auth.Identity
	ProjectID string

	Name *string

	UpdateDescription bool
	Description       *string
}

type ListInput struct {
	Identity auth.Identity
	Limit    int
	Cursor   string
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Project, error) {

	name := strings.TrimSpace(input.Name)

	if name == "" {
		return Project{}, ErrNameRequired
	}

	if len(name) > 200 {
		return Project{}, ErrNameTooLong
	}

	id := uuid.NewString()

	project, err := s.repository.CreateWithDefaultStatuses(
		ctx,
		CreateParams{
			ID:              id,
			TenantID:        input.TenantID,
			Name:            name,
			Description:     input.Description,
			CreatedByUserID: input.UserID,
		},
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"create project: %w",
			err,
		)
	}

	return project, nil
}
func (s *Service) List(
	ctx context.Context,
	input ListInput,
) (ProjectPage, error) {
	limit := input.Limit

	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	var cursor *Cursor

	if input.Cursor != "" {
		decoded, err := DecodeCursor(input.Cursor)
		if err != nil {
			return ProjectPage{}, fmt.Errorf(
				"%w: %v",
				ErrInvalidCursor,
				err,
			)
		}

		cursor = &decoded
	}

	options := ListOptions{
		Limit:  int32(limit + 1),
		Cursor: cursor,
	}

	var projects []Project
	var err error

	if input.Identity.IsAdmin() {
		projects, err = s.repository.ListForTenant(
			ctx,
			input.Identity.TenantID,
			options,
		)
	} else {
		projects, err = s.repository.ListForUser(
			ctx,
			input.Identity.TenantID,
			input.Identity.UserID,
			options,
		)
	}

	if err != nil {
		return ProjectPage{}, fmt.Errorf(
			"list projects: %w",
			err,
		)
	}

	hasNext := len(projects) > limit

	if hasNext {
		projects = projects[:limit]
	}

	var nextCursor *string

	if hasNext && len(projects) > 0 {
		last := projects[len(projects)-1]

		encoded := EncodeCursor(
			Cursor{
				CreatedAt: last.CreatedAt,
				ID:        last.ID,
			},
		)

		nextCursor = &encoded
	}

	return ProjectPage{
		Items:      projects,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Project, error) {
	if identity.IsAdmin() {
		project, err := s.repository.GetForTenant(
			ctx,
			identity.TenantID,
			projectID,
		)
		if err != nil {
			return Project{}, fmt.Errorf(
				"get project as admin: %w",
				err,
			)
		}

		return project, nil
	}

	project, err := s.repository.GetForUser(
		ctx,
		identity.TenantID,
		identity.UserID,
		projectID,
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"get project as member: %w",
			err,
		)
	}

	return project, nil
}
func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Project, error) {
	if !input.Identity.IsAdmin() {
		role, err := s.repository.GetMemberRole(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
			input.Identity.UserID,
		)
		if err != nil {
			return Project{}, fmt.Errorf(
				"get project membership: %w",
				err,
			)
		}

		if role != MemberRoleManager {
			return Project{}, ErrForbidden
		}
	}

	var name *string

	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)

		if value == "" {
			return Project{}, ErrInvalidName
		}

		if len(value) > 200 {
			return Project{}, ErrInvalidName
		}

		name = &value
	}

	if input.Name == nil && !input.UpdateDescription {
		return Project{}, ErrNoChanges
	}

	project, err := s.repository.Update(
		ctx,
		UpdateParams{
			TenantID: input.Identity.TenantID,

			ProjectID: input.ProjectID,

			Name: name,

			UpdateDescription: input.UpdateDescription,
			Description:       input.Description,
		},
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"update project: %w",
			err,
		)
	}

	return project, nil
}
