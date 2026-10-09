package project

import (
	"context"
	"fmt"

	"github.com/mhdph/taskmanager/internal/auth"
)

type Authorizer struct {
	repository *Repository
}

func NewAuthorizer(
	repository *Repository,
) *Authorizer {
	return &Authorizer{
		repository: repository,
	}
}

func (a *Authorizer) Require(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	permission Permission,
) error {
	if identity.IsAdmin() {
		return nil
	}

	role, err := a.repository.GetMemberRole(
		ctx,
		identity.TenantID,
		projectID,
		identity.UserID,
	)
	if err != nil {
		return fmt.Errorf(
			"get project membership: %w",
			err,
		)
	}

	if !roleHasPermission(role, permission) {
		return ErrForbidden
	}

	return nil
}

func roleHasPermission(
	role MemberRole,
	permission Permission,
) bool {
	switch role {
	case MemberRoleManager:
		return managerHasPermission(permission)

	case MemberRoleMember:
		return memberHasPermission(permission)

	case MemberRoleViewer:
		return viewerHasPermission(permission)

	default:
		return false
	}
}

func managerHasPermission(
	permission Permission,
) bool {
	switch permission {
	case PermissionViewProject,
		PermissionEditProject,
		PermissionViewMembers,
		PermissionManageMembers,
		PermissionViewStatuses,
		PermissionManageStatuses,
		PermissionArchiveProject,
		PermissionCreateTask,
		PermissionViewTasks,
		PermissionEditTask,
		PermissionArchiveTask:
		return true

	default:
		return false
	}
}
func memberHasPermission(
	permission Permission,
) bool {
	switch permission {
	case PermissionViewProject,
		PermissionViewMembers,
		PermissionViewStatuses,
		PermissionCreateTask,
		PermissionViewTasks,
		PermissionEditTask,
		PermissionArchiveTask:
		return true

	default:
		return false
	}
}
func viewerHasPermission(
	permission Permission,
) bool {
	switch permission {
	case PermissionViewProject,
		PermissionViewMembers,
		PermissionViewStatuses,
		PermissionViewTasks:
		return true

	default:
		return false
	}
}
