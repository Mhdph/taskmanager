package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
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
