package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
)

type Service struct {
	repository *Repository
	authorizer *Authorizer
}

func NewService(
	repository *Repository,
	authorizer *Authorizer,
) *Service {
	return &Service{
		repository: repository,
		authorizer: authorizer,
	}
}

type CreateInput struct {
	TenantID    string
	UserID      string
	Name        string
	Description *string
}
type UpdateMemberRoleInput struct {
	Identity  auth.Identity
	ProjectID string
	UserID    string
	Role      MemberRole
}

type AddMemberInput struct {
	Identity  auth.Identity
	ProjectID string

	UserID string
	Role   MemberRole
}

type UpdateInput struct {
	Identity  auth.Identity
	ProjectID string

	Name *string

	UpdateDescription bool
	Description       *string

	// UpdateTeam replaces the owning team (nil clears the link).
	UpdateTeam bool
	TeamID     *string
}

type ListInput struct {
	Archived bool
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

	if utf8.RuneCountInString(name) > 200 {
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
		Archived: input.Archived,
		Limit:    int32(limit + 1),
		Cursor:   cursor,
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
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionEditProject,
	)
	if err != nil {
		return Project{}, fmt.Errorf(
			"authorize project update: %w",
			err,
		)
	}

	var name *string

	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)

		if value == "" {
			return Project{}, ErrInvalidName
		}

		if utf8.RuneCountInString(value) > 200 {
			return Project{}, ErrInvalidName
		}

		name = &value
	}

	if input.Name == nil && !input.UpdateDescription && !input.UpdateTeam {
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

			UpdateTeam: input.UpdateTeam,
			TeamID:     input.TeamID,
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

func (s *Service) Archive(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) error {
	err := s.authorizer.Require(
		ctx,
		identity,
		projectID,
		PermissionArchiveProject,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize project archive: %w",
			err,
		)
	}

	if err := s.repository.Archive(
		ctx,
		identity.TenantID,
		projectID,
	); err != nil {
		return fmt.Errorf(
			"archive project: %w",
			err,
		)
	}

	return nil
}
func (s *Service) AddMember(
	ctx context.Context,
	input AddMemberInput,
) (ProjectMember, error) {
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionManageMembers,
	)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"authorize member management: %w",
			err,
		)
	}

	if input.UserID == "" {
		return ProjectMember{}, ErrInvalidMember
	}

	switch input.Role {
	case MemberRoleManager,
		MemberRoleMember,
		MemberRoleViewer:

	default:
		return ProjectMember{}, ErrInvalidMemberRole
	}

	member, err := s.repository.AddMember(
		ctx,
		AddMemberParams{
			TenantID: input.Identity.TenantID,

			ProjectID: input.ProjectID,

			UserID: input.UserID,

			Role: input.Role,
		},
	)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"add project member: %w",
			err,
		)
	}

	return member, nil
}
func (s *Service) ListMembers(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]ProjectMember, error) {
	err := s.authorizer.Require(
		ctx,
		identity,
		projectID,
		PermissionViewMembers,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"authorize view members: %w",
			err,
		)
	}

	members, err := s.repository.ListMembers(
		ctx,
		identity.TenantID,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list members: %w",
			err,
		)
	}

	return members, nil
}
func (s *Service) UpdateMemberRole(
	ctx context.Context,
	input UpdateMemberRoleInput,
) (ProjectMember, error) {
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionManageMembers,
	)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"authorize member management: %w",
			err,
		)
	}

	switch input.Role {
	case MemberRoleManager,
		MemberRoleMember,
		MemberRoleViewer:

	default:
		return ProjectMember{}, ErrInvalidMemberRole
	}

	member, err := s.repository.UpdateMemberRole(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.UserID,
		input.Role,
	)
	if err != nil {
		return ProjectMember{}, fmt.Errorf(
			"update member role: %w",
			err,
		)
	}

	return member, nil
}

type RemoveMemberInput struct {
	Identity  auth.Identity
	ProjectID string
	UserID    string
}

func (s *Service) RemoveMember(
	ctx context.Context,
	input RemoveMemberInput,
) error {
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionManageMembers,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize member management: %w",
			err,
		)
	}

	if err := s.repository.DeleteMember(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.UserID,
	); err != nil {
		return fmt.Errorf(
			"delete project member: %w",
			err,
		)
	}

	return nil
}

type updateProjectMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) ListMembers(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	members, err := h.service.ListMembers(
		r.Context(),
		identity,
		projectID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid project id",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrNotFound):
			http.Error(
				w,
				"project not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"list project members: %v",
				err,
			)

			http.Error(
				w,
				"failed to list members",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := make(
		[]projectMemberResponse,
		0,
		len(members),
	)

	for _, member := range members {
		response = append(
			response,
			projectMemberResponse{
				UserID: member.UserID,
				Role:   string(member.Role),

				CreatedAt: member.CreatedAt.
					UTC().
					Format(time.RFC3339),
			},
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode project members: %v",
			err,
		)
	}
}

type CreateStatusInput struct {
	Identity  auth.Identity
	ProjectID string

	Name     string
	Position int32
	Category StatusCategory
}

func (s *Service) CreateStatus(
	ctx context.Context,
	input CreateStatusInput,
) (ProjectStatus, error) {
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionManageStatuses,
	)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"authorize status management: %w",
			err,
		)
	}

	name := strings.TrimSpace(input.Name)

	if name == "" || utf8.RuneCountInString(name) > 80 {
		return ProjectStatus{}, ErrInvalidStatus
	}

	if input.Position < 0 {
		return ProjectStatus{}, ErrInvalidStatus
	}

	switch input.Category {
	case StatusCategoryTodo,
		StatusCategoryInProgress,
		StatusCategoryDone:

	default:
		return ProjectStatus{}, ErrInvalidStatus
	}

	status, err := s.repository.CreateStatus(
		ctx,
		CreateStatusParams{
			ID:        uuid.NewString(),
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,
			Name:      name,
			Position:  input.Position,
			Category:  input.Category,
		},
	)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"create project status: %w",
			err,
		)
	}

	return status, nil
}

func (s *Service) ListStatuses(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]ProjectStatus, error) {
	err := s.authorizer.Require(
		ctx,
		identity,
		projectID,
		PermissionViewStatuses,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"authorize view statuses: %w",
			err,
		)
	}

	statuses, err := s.repository.ListStatuses(
		ctx,
		identity.TenantID,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list project statuses: %w",
			err,
		)
	}

	return statuses, nil
}

type UpdateStatusInput struct {
	Identity  auth.Identity
	ProjectID string
	StatusID  string

	Name     *string
	Position *int32
	Category *StatusCategory
}

func (s *Service) UpdateStatus(
	ctx context.Context,
	input UpdateStatusInput,
) (ProjectStatus, error) {
	err := s.authorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		PermissionManageStatuses,
	)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"authorize status management: %w",
			err,
		)
	}

	if input.Name == nil &&
		input.Position == nil &&
		input.Category == nil {
		return ProjectStatus{}, ErrNoChanges
	}

	var name *string

	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)

		if value == "" || utf8.RuneCountInString(value) > 80 {
			return ProjectStatus{}, ErrInvalidStatus
		}

		name = &value
	}

	if input.Position != nil &&
		*input.Position < 0 {
		return ProjectStatus{}, ErrInvalidStatus
	}

	if input.Category != nil {
		switch *input.Category {
		case StatusCategoryTodo,
			StatusCategoryInProgress,
			StatusCategoryDone:

		default:
			return ProjectStatus{}, ErrInvalidStatus
		}
	}

	status, err := s.repository.UpdateStatus(
		ctx,
		UpdateStatusParams{
			TenantID:  input.Identity.TenantID,
			ProjectID: input.ProjectID,
			StatusID:  input.StatusID,

			Name:     name,
			Position: input.Position,
			Category: input.Category,
		},
	)
	if err != nil {
		return ProjectStatus{}, fmt.Errorf(
			"update project status: %w",
			err,
		)
	}

	return status, nil
}

func (s *Service) DeleteStatus(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	statusID string,
) error {
	err := s.authorizer.Require(
		ctx,
		identity,
		projectID,
		PermissionManageStatuses,
	)
	if err != nil {
		return fmt.Errorf(
			"authorize status management: %w",
			err,
		)
	}

	if err := s.repository.DeleteStatus(
		ctx,
		identity.TenantID,
		projectID,
		statusID,
	); err != nil {
		return fmt.Errorf(
			"delete project status: %w",
			err,
		)
	}

	return nil
}

func (s *Service) ReorderStatuses(ctx context.Context, identity auth.Identity, projectID string, statusIDs []string) error {
	if err := s.authorizer.Require(ctx, identity, projectID, PermissionManageStatuses); err != nil {
		return fmt.Errorf("authorize status management: %w", err)
	}
	return s.repository.ReorderStatuses(ctx, identity.TenantID, projectID, statusIDs)
}
