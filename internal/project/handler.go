package project

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mhdph/taskmanager/internal/auth"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type createProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string         `json:"name"`
	Description json.RawMessage `json:"description"`
}

type listProjectsResponse struct {
	Items      []projectResponse `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type projectMemberResponse struct {
	UserID    string `json:"userId"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type addProjectMemberRequest struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

type projectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	var req createProjectRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	project, err := h.service.Create(
		r.Context(),
		CreateInput{
			TenantID:    identity.TenantID,
			UserID:      identity.UserID,
			Name:        req.Name,
			Description: req.Description,
		},
	)
	if err != nil {
		log.Printf(
			"create project: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to create project",
			err,
		)

		return
	}

	response := projectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		CreatedAt: project.CreatedAt.
			UTC().
			Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: project.UpdatedAt.
			UTC().
			Format("2006-01-02T15:04:05Z07:00"),
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf(
			"encode project response: %v",
			err,
		)
	}
}

func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return
	}

	limit := 20

	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(
				w,
				"invalid limit",
				http.StatusBadRequest,
			)
			return
		}

		limit = parsed
	}

	cursor := r.URL.Query().Get("cursor")

	page, err := h.service.List(
		r.Context(),
		ListInput{
			Identity: identity,
			Limit:    limit,
			Cursor:   cursor,
		},
	)
	if err != nil {
		log.Printf(
			"list projects: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to list projects",
			err,
		)

		return
	}

	items := make(
		[]projectResponse,
		0,
		len(page.Items),
	)

	for _, item := range page.Items {
		items = append(
			items,
			toProjectResponse(item),
		)
	}

	response := listProjectsResponse{
		Items:      items,
		NextCursor: page.NextCursor,
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode projects response: %v",
			err,
		)
	}
}

func (h *Handler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
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

	if projectID == "" {
		http.Error(
			w,
			"project id is required",
			http.StatusBadRequest,
		)
		return
	}

	project, err := h.service.Get(
		r.Context(),
		identity,
		projectID,
	)
	if err != nil {
		log.Printf(
			"get project: %v",
			err,
		)

		handleProjectError(
			w,
			"failed to get project",
			err,
		)

		return
	}

	response := toProjectResponse(project)

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode project response: %v",
			err,
		)
	}
}

func handleProjectError(
	w http.ResponseWriter,
	message string,
	err error,
) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(
			w,
			ErrNotFound.Error(),
			http.StatusNotFound,
		)

	case errors.Is(err, ErrInvalidID),
		errors.Is(err, ErrNameRequired),
		errors.Is(err, ErrNameTooLong),
		errors.Is(err, ErrInvalidCursor):
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

	default:
		http.Error(
			w,
			message,
			http.StatusInternalServerError,
		)
	}
}

func toProjectResponse(project Project) projectResponse {
	return projectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		CreatedAt: project.CreatedAt.
			UTC().
			Format(time.RFC3339),
		UpdatedAt: project.UpdatedAt.
			UTC().
			Format(time.RFC3339),
	}
}

func (h *Handler) Update(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
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

	var req updateProjectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	var description *string
	updateDescription := false

	if req.Description != nil {
		updateDescription = true

		if string(req.Description) != "null" {
			var value string

			if err := json.Unmarshal(
				req.Description,
				&value,
			); err != nil {
				http.Error(
					w,
					"description must be string or null",
					http.StatusBadRequest,
				)
				return
			}

			description = &value
		}
	}

	updated, err := h.service.Update(
		r.Context(),
		UpdateInput{
			Identity:  identity,
			ProjectID: projectID,

			Name: req.Name,

			UpdateDescription: updateDescription,
			Description:       description,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid project id",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrInvalidName):
			http.Error(
				w,
				"invalid project name",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrNoChanges):
			http.Error(
				w,
				"no changes provided",
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
				"update project: %v",
				err,
			)

			http.Error(
				w,
				"failed to update project",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toProjectResponse(updated),
	); err != nil {
		log.Printf(
			"encode project response: %v",
			err,
		)
	}
}

func (h *Handler) Archive(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
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

	err := h.service.Archive(
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
				"archive project: %v",
				err,
			)

			http.Error(
				w,
				"failed to archive project",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddMember(
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

	var req addProjectMemberRequest

	if err := json.NewDecoder(r.Body).Decode(
		&req,
	); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	member, err := h.service.AddMember(
		r.Context(),
		AddMemberInput{
			Identity:  identity,
			ProjectID: projectID,
			UserID:    req.UserID,
			Role:      MemberRole(req.Role),
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrInvalidID,
		):
			http.Error(
				w,
				"invalid project id",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrInvalidMember,
		):
			http.Error(
				w,
				"invalid member",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrInvalidMemberRole,
		):
			http.Error(
				w,
				"invalid member role",
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			ErrNotFound,
		):
			http.Error(
				w,
				"project not found",
				http.StatusNotFound,
			)

		case errors.Is(
			err,
			ErrForbidden,
		):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"add project member: %v",
				err,
			)

			http.Error(
				w,
				"failed to add project member",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := projectMemberResponse{
		UserID: member.UserID,
		Role:   string(member.Role),

		CreatedAt: member.CreatedAt.
			UTC().
			Format(time.RFC3339),
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		http.StatusCreated,
	)

	if err := json.NewEncoder(w).Encode(
		response,
	); err != nil {
		log.Printf(
			"encode project member: %v",
			err,
		)
	}
}
func (h *Handler) UpdateMemberRole(
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

	userID := chi.URLParam(
		r,
		"userID",
	)

	var req updateProjectMemberRequest

	if err := json.NewDecoder(r.Body).Decode(
		&req,
	); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	member, err := h.service.UpdateMemberRole(
		r.Context(),
		UpdateMemberRoleInput{
			Identity:  identity,
			ProjectID: projectID,
			UserID:    userID,
			Role:      MemberRole(req.Role),
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidMemberRole):
			http.Error(
				w,
				"invalid member role",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrLastManager):
			http.Error(
				w,
				"project must have at least one manager",
				http.StatusConflict,
			)

		case errors.Is(err, ErrMemberNotFound):
			http.Error(
				w,
				"member not found",
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
				"update project member: %v",
				err,
			)

			http.Error(
				w,
				"failed to update member",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := projectMemberResponse{
		UserID: member.UserID,
		Role:   string(member.Role),

		CreatedAt: member.CreatedAt.
			UTC().
			Format(time.RFC3339),
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode project member: %v",
			err,
		)
	}
}

func (h *Handler) RemoveMember(
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

	userID := chi.URLParam(
		r,
		"userID",
	)

	err := h.service.RemoveMember(
		r.Context(),
		RemoveMemberInput{
			Identity:  identity,
			ProjectID: projectID,
			UserID:    userID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrLastManager):
			http.Error(
				w,
				"project must have at least one manager",
				http.StatusConflict,
			)

		case errors.Is(err, ErrMemberNotFound):
			http.Error(
				w,
				"member not found",
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
				"remove project member: %v",
				err,
			)

			http.Error(
				w,
				"failed to remove member",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
