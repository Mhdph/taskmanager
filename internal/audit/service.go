package audit

import (
	"context"
	"fmt"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

// Service exposes read access to the activity feed. Writes go through the
// domain services, which record entries alongside their mutations.
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

const (
	defaultLimit = 50
	maxLimit     = 100
)

// validEntityTypes whitelists the entity_type filter accepted from the
// query string.
func validEntityType(value EntityType) bool {
	switch value {
	case EntityProject,
		EntityTask,
		EntityComment,
		EntityAttachment,
		EntityMember,
		EntityDependency:

		return true
	}

	return false
}

// ViewOptions describes one page of the activity feed. nil filters mean
// "no filter".
type ViewOptions struct {
	Identity auth.Identity

	ProjectID   *string
	TaskID      *string
	ActorUserID *string
	EntityType  *EntityType

	Cursor *AuditCursor

	Limit int32
}

// View returns one page of the activity feed. When ProjectID is set the
// feed is scoped to that project (with an explicit access check), otherwise
// the feed is tenant-wide scoped by project membership.
func (s *Service) View(
	ctx context.Context,
	options ViewOptions,
) ([]Entry, *AuditCursor, error) {
	limit := options.Limit

	if limit <= 0 {
		limit = defaultLimit
	}

	if limit > maxLimit {
		limit = maxLimit
	}

	if options.EntityType != nil && !validEntityType(*options.EntityType) {
		return nil, nil, fmt.Errorf(
			"%w: entity type",
			ErrInvalidFilter,
		)
	}

	if options.ProjectID != nil {
		// Resolves the project and enforces visibility before listing.
		if err := s.authorizer.Require(
			ctx,
			options.Identity,
			*options.ProjectID,
			project.PermissionViewProject,
		); err != nil {
			return nil, nil, fmt.Errorf(
				"authorize project activity: %w",
				err,
			)
		}
	}

	repoOptions := ListOptions{
		TenantID: options.Identity.TenantID,
		IsAdmin:  options.Identity.IsAdmin(),
		UserID:   options.Identity.UserID,

		ProjectID:   options.ProjectID,
		TaskID:      options.TaskID,
		ActorUserID: options.ActorUserID,
		EntityType:  options.EntityType,

		Cursor: options.Cursor,

		Limit: limit + 1,
	}

	entries, err := s.repository.ListForTenant(
		ctx,
		repoOptions,
	)
	if err != nil {
		return nil, nil, err
	}

	hasMore := len(entries) > int(limit)

	if hasMore {
		entries = entries[:limit]
	}

	var next *AuditCursor

	if hasMore && len(entries) > 0 {
		last := entries[len(entries)-1]

		next = &AuditCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}
	}

	return entries, next, nil
}
