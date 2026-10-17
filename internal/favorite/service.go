package favorite

import (
	"context"
	"fmt"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

// Service owns favorite toggling. Favorites are personal: any task or
// project a user can view can be favorited.
type Service struct {
	repository        *Repository
	taskRepository    *task.Repository
	projectRepository *project.Repository
	projectAuthorizer *project.Authorizer
}

func NewService(
	repository *Repository,
	taskRepository *task.Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
) *Service {
	return &Service{
		repository:        repository,
		taskRepository:    taskRepository,
		projectRepository: projectRepository,
		projectAuthorizer: projectAuthorizer,
	}
}

func (s *Service) AddTask(
	ctx context.Context,
	identity auth.Identity,
	taskID string,
) error {
	item, err := s.taskRepository.GetForTenant(
		ctx,
		identity.TenantID,
		taskID,
	)
	if err != nil {
		return fmt.Errorf(
			"resolve favorite task: %w",
			err,
		)
	}

	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		item.ProjectID,
		project.PermissionViewTasks,
	); err != nil {
		return fmt.Errorf(
			"authorize favorite task: %w",
			err,
		)
	}

	return s.repository.AddTask(
		ctx,
		identity.TenantID,
		identity.UserID,
		item.ProjectID,
		item.ID,
	)
}

func (s *Service) RemoveTask(
	ctx context.Context,
	identity auth.Identity,
	taskID string,
) error {
	return s.repository.RemoveTask(
		ctx,
		identity.TenantID,
		identity.UserID,
		taskID,
	)
}

func (s *Service) AddProject(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) error {
	// Confirms the project exists in this tenant.
	if _, err := s.projectRepository.GetForTenant(
		ctx,
		identity.TenantID,
		projectID,
	); err != nil {
		return fmt.Errorf(
			"resolve favorite project: %w",
			err,
		)
	}

	if err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionViewProject,
	); err != nil {
		return fmt.Errorf(
			"authorize favorite project: %w",
			err,
		)
	}

	return s.repository.AddProject(
		ctx,
		identity.TenantID,
		identity.UserID,
		projectID,
	)
}

func (s *Service) RemoveProject(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) error {
	return s.repository.RemoveProject(
		ctx,
		identity.TenantID,
		identity.UserID,
		projectID,
	)
}

type ListOutput struct {
	TaskIDs    []string
	ProjectIDs []string
}

func (s *Service) List(
	ctx context.Context,
	identity auth.Identity,
) (ListOutput, error) {
	taskIDs, err := s.repository.ListTaskIDs(
		ctx,
		identity.TenantID,
		identity.UserID,
	)
	if err != nil {
		return ListOutput{}, err
	}

	projectIDs, err := s.repository.ListProjectIDs(
		ctx,
		identity.TenantID,
		identity.UserID,
	)
	if err != nil {
		return ListOutput{}, err
	}

	return ListOutput{
		TaskIDs:    taskIDs,
		ProjectIDs: projectIDs,
	}, nil
}
