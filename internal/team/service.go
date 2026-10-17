package team

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/member"
)

// Service manages the workspace team directory and the primary-team
// assignment of workspace members.
type Service struct {
	repository *Repository

	memberRepository *member.Repository
}

func NewService(
	repository *Repository,
	memberRepository *member.Repository,
) *Service {
	return &Service{
		repository: repository,

		memberRepository: memberRepository,
	}
}

const (
	maxNameLength = 64

	maxDescriptionLength = 200

	maxMembers = 100
)

// canManageTeams: hosted admins/support and workspace owners can manage
// the team directory. Everybody can list teams and read membership.
func (s *Service) canManageTeams(identity auth.Identity) bool {
	if identity.IsSupport {
		return true
	}

	return identity.IsAdmin()
}

func (s *Service) List(
	ctx context.Context,
	identity auth.Identity,
) ([]Team, error) {
	teams, err := s.repository.List(
		ctx,
		identity.TenantID,
	)
	if err != nil {
		return nil, err
	}

	return teams, nil
}

func (s *Service) Get(
	ctx context.Context,
	identity auth.Identity,
	teamID string,
) (Team, error) {
	item, err := s.repository.Get(
		ctx,
		identity.TenantID,
		teamID,
	)
	if err != nil {
		return Team{}, err
	}

	return item, nil
}

type CreateInput struct {
	Identity auth.Identity

	Name string

	Icon  string
	Color string

	Description string
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Team, error) {
	if !s.canManageTeams(input.Identity) {
		return Team{}, ErrForbidden
	}

	name, err := normalizeName(input.Name)
	if err != nil {
		return Team{}, err
	}

	if utf8.RuneCountInString(input.Description) > maxDescriptionLength {
		return Team{}, ErrTeamDescriptionTooLong
	}

	item, err := s.repository.Create(
		ctx,
		CreateParams{
			ID:       uuid.NewString(),
			TenantID: input.Identity.TenantID,

			Name: name,

			Icon:  strings.TrimSpace(input.Icon),
			Color: strings.TrimSpace(input.Color),

			Description: input.Description,

			CreatedByUserID: input.Identity.UserID,
		},
	)
	if err != nil {
		if isTeamConflict(err) {
			return Team{}, ErrTeamConflict
		}

		return Team{}, err
	}

	if item.Icon == "" {
		item.Icon = "users"
	}

	return item, nil
}

type UpdateInput struct {
	Identity auth.Identity

	TeamID string

	Name        *string
	Icon        *string
	Color       *string
	Description *string
}

func (s *Service) Update(
	ctx context.Context,
	input UpdateInput,
) (Team, error) {
	if !s.canManageTeams(input.Identity) {
		return Team{}, ErrForbidden
	}

	params := UpdateParams{
		TenantID: input.Identity.TenantID,
		ID:       input.TeamID,
	}

	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return Team{}, err
		}

		params.Name = &name
	}

	if input.Icon != nil {
		params.Icon = input.Icon
	}

	if input.Color != nil {
		params.Color = input.Color
	}

	if input.Description != nil {
		if utf8.RuneCountInString(*input.Description) > maxDescriptionLength {
			return Team{}, ErrTeamDescriptionTooLong
		}

		params.Description = input.Description
	}

	if params.Name == nil &&
		params.Icon == nil &&
		params.Color == nil &&
		params.Description == nil {

		return Team{}, ErrNothingToUpdate
	}

	item, err := s.repository.Update(
		ctx,
		params,
	)
	if err != nil {
		if isTeamConflict(err) {
			return Team{}, ErrTeamConflict
		}

		return Team{}, err
	}

	return item, nil
}

func (s *Service) Delete(
	ctx context.Context,
	identity auth.Identity,
	teamID string,
) error {
	if !s.canManageTeams(identity) {
		return ErrForbidden
	}

	// Dropping a team only unassigns members and projects (ON DELETE SET
	// NULL); people and projects are never deleted with it.
	return s.repository.Delete(
		ctx,
		identity.TenantID,
		teamID,
	)
}

type ListMembersInput struct {
	Identity auth.Identity

	TeamID string
}

func (s *Service) ListMembers(
	ctx context.Context,
	input ListMembersInput,
) ([]string, error) {
	return s.repository.ListMemberIDs(
		ctx,
		input.Identity.TenantID,
		input.TeamID,
	)
}

type SetMembersInput struct {
	Identity auth.Identity

	TeamID string

	// UserIDs is the complete member list for the team. A member absent
	// from the list leaves the team.
	UserIDs []string
}

func (s *Service) SetMembers(
	ctx context.Context,
	input SetMembersInput,
) error {
	if !s.canManageTeams(input.Identity) {
		return ErrForbidden
	}

	userIDs := dedupe(input.UserIDs)

	if len(userIDs) > maxMembers {
		return fmt.Errorf(
			"%w: a team cannot carry more than %d members",
			ErrTeamMembersInvalid,
			maxMembers,
		)
	}

	// Every requested user id must exist in the workspace directory.
	existing, err := s.repository.ListMemberIDsIn(
		ctx,
		input.Identity.TenantID,
		userIDs,
	)
	if err != nil {
		return fmt.Errorf(
			"resolve team membership: %w",
			err,
		)
	}

	if len(existing) != len(userIDs) {
		return ErrTeamMembersInvalid
	}

	return s.repository.SetMembers(
		ctx,
		input.Identity.TenantID,
		input.TeamID,
		userIDs,
	)
}

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

func normalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)

	if name == "" {
		return "", ErrTeamNameRequired
	}

	if utf8.RuneCountInString(name) > maxNameLength {
		return "", ErrTeamNameTooLong
	}

	return name, nil
}

func dedupe(values []string) []string {
	deduped := make([]string, 0, len(values))

	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}

		deduped = append(deduped, value)
	}

	sort.Strings(deduped)

	return deduped
}
