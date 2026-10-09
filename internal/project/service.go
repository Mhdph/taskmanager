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
		return Project{}, fmt.Errorf(
			"project name is required",
		)
	}

	if len(name) > 200 {
		return Project{}, fmt.Errorf(
			"project name is too long",
		)
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
				"invalid cursor: %w",
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
