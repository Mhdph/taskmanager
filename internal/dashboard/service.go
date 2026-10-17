package dashboard

import (
	"context"
	"fmt"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

type Service struct {
	repository *Repository

	projectAuthorizer *project.Authorizer
}

func NewService(
	repository *Repository,
	projectAuthorizer *project.Authorizer,
) *Service {
	return &Service{
		repository: repository,

		projectAuthorizer: projectAuthorizer,
	}
}

type GetInput struct {
	Identity auth.Identity

	ProjectID string
}

func (s *Service) Get(
	ctx context.Context,
	input GetInput,
) (Dashboard, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewDashboard,
	)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"authorize dashboard: %w",
			err,
		)
	}

	result, err :=
		s.repository.GetProjectDashboard(
			ctx,
			input.Identity.TenantID,
			input.ProjectID,
		)

	if err != nil {
		return Dashboard{}, fmt.Errorf(
			"get project dashboard: %w",
			err,
		)
	}

	return result, nil
}
