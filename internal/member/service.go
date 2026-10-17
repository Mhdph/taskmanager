package member

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/mhdph/taskmanager/internal/auth"
)

type Service struct {
	repository *Repository
}

func NewService(
	repository *Repository,
) *Service {
	return &Service{
		repository: repository,
	}
}

// canManageMembers reports whether the caller is allowed to change member
// roles or remove members. Hosted admin/support users always can; otherwise
// the caller must hold the owner or admin directory role.
func canManageMembers(
	identity auth.Identity,
	member Member,
	hasMember bool,
) bool {
	if identity.IsSupport {
		return true
	}

	if identity.IsAdmin() {
		return true
	}

	return hasMember &&
		(member.Role == RoleOwner || member.Role == RoleAdmin)
}

func (s *Service) List(
	ctx context.Context,
	identity auth.Identity,
	options ListOptions,
) ([]Member, error) {
	members, err := s.repository.List(
		ctx,
		identity.TenantID,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list members: %w",
			err,
		)
	}

	return members, nil
}

func (s *Service) Get(
	ctx context.Context,
	identity auth.Identity,
	userID string,
) (Member, error) {
	member, err := s.repository.Get(
		ctx,
		identity.TenantID,
		userID,
	)
	if err != nil {
		return Member{}, fmt.Errorf(
			"get member: %w",
			err,
		)
	}

	return member, nil
}

type UpdateInput struct {
	Identity auth.Identity
	UserID   string

	Role      *Role
	Title     *string
	ClearTitle bool
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Member, error) {
	if input.Role != nil && !input.Role.Valid() {
		return Member{}, ErrInvalidRole
	}

	caller, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.Identity.UserID,
	)
	hasCaller := err == nil

	if err != nil && !errors.Is(err, ErrNotFound) {
		return Member{}, fmt.Errorf(
			"get caller membership: %w",
			err,
		)
	}

	if !canManageMembers(input.Identity, caller, hasCaller) {
		return Member{}, ErrForbidden
	}

	target, err := s.repository.Get(
		ctx,
		input.Identity.TenantID,
		input.UserID,
	)
	if err != nil {
		return Member{}, fmt.Errorf(
			"get member: %w",
			err,
		)
	}

	updated := target

	if input.Role != nil {
		updated, err = s.repository.UpdateRole(
			ctx,
			input.Identity.TenantID,
			input.UserID,
			*input.Role,
		)
		if err != nil {
			return Member{}, fmt.Errorf(
				"update member role: %w",
				err,
			)
		}
	}

	if input.Title != nil || input.ClearTitle {
		title := ""

		if input.Title != nil {
			title = strings.TrimSpace(*input.Title)
		}

		updated, err = s.repository.UpdateTitle(
			ctx,
			input.Identity.TenantID,
			input.UserID,
			title,
		)
		if err != nil {
			return Member{}, fmt.Errorf(
				"update member title: %w",
				err,
			)
		}
	}

	return updated, nil
}

func (s *Service) Remove(
	ctx context.Context,
	identity auth.Identity,
	userID string,
) error {
	caller, err := s.repository.Get(
		ctx,
		identity.TenantID,
		identity.UserID,
	)
	hasCaller := err == nil

	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf(
			"get caller membership: %w",
			err,
		)
	}

	if !canManageMembers(identity, caller, hasCaller) {
		return ErrForbidden
	}

	err = s.repository.Delete(
		ctx,
		identity.TenantID,
		userID,
	)
	if err != nil {
		return fmt.Errorf(
			"remove member: %w",
			err,
		)
	}

	return nil
}

// Sync upserts an identity into the tenant directory. It is called by the
// auth middleware on hosted identity resolution; failures are logged but
// never block the request.
func (s *Service) Sync(
	ctx context.Context,
	identity auth.Identity,
) {
	err := s.repository.Sync(
		ctx,
		SyncParams{
			TenantID:  identity.TenantID,
			UserID:    identity.UserID,
			Email:     identity.Email,
			FirstName: identity.FirstName,
			LastName:  identity.LastName,
		},
	)
	if err != nil {
		log.Printf(
			"sync tenant member: %v",
			err,
		)
	}
}
