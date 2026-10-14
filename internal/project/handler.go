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
	"github.com/mhdph/taskmanager/internal/httpx"
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

	err := httpx.DecodeJSON(w, r, &req)
	if err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
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

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
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

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
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

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
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

type createProjectStatusRequest struct {
	Name     string `json:"name"`
	Position int32  `json:"position"`
	Category string `json:"category"`
}

type updateProjectStatusRequest struct {
	Name     *string `json:"name"`
	Position *int32  `json:"position"`
	Category *string `json:"category"`
}

type reorderProjectStatusesRequest struct {
	StatusIDs []string `json:"statusIds"`
}

type projectStatusResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Position  int32  `json:"position"`
	Category  string `json:"category"`
	CreatedAt string `json:"createdAt"`
}

func toProjectStatusResponse(
	status ProjectStatus,
) projectStatusResponse {
	return projectStatusResponse{
		ID:       status.ID,
		Name:     status.Name,
		Position: status.Position,
		Category: string(status.Category),

		CreatedAt: status.CreatedAt.
			UTC().
			Format(time.RFC3339),
	}
}

func (h *Handler) ListStatuses(
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

	statuses, err := h.service.ListStatuses(
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
				"list project statuses: %v",
				err,
			)

			http.Error(
				w,
				"failed to list statuses",
				http.StatusInternalServerError,
			)
		}

		return
	}

	response := make(
		[]projectStatusResponse,
		0,
		len(statuses),
	)

	for _, status := range statuses {
		response = append(
			response,
			toProjectStatusResponse(status),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode project statuses: %v",
			err,
		)
	}
}

func (h *Handler) CreateStatus(
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

	var req createProjectStatusRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	status, err := h.service.CreateStatus(
		r.Context(),
		CreateStatusInput{
			Identity:  identity,
			ProjectID: projectID,
			Name:      req.Name,
			Position:  req.Position,
			Category: StatusCategory(
				req.Category,
			),
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidStatus):
			http.Error(
				w,
				"invalid status",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrStatusAlreadyExists),
			errors.Is(err, ErrStatusPositionConflict):

			http.Error(
				w,
				"status conflict",
				http.StatusConflict,
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
				"create project status: %v",
				err,
			)

			http.Error(
				w,
				"failed to create status",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		http.StatusCreated,
	)

	if err := json.NewEncoder(w).Encode(
		toProjectStatusResponse(status),
	); err != nil {
		log.Printf(
			"encode project status: %v",
			err,
		)
	}
}

func (h *Handler) UpdateStatus(
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

	statusID := chi.URLParam(
		r,
		"statusID",
	)

	var req updateProjectStatusRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	var category *StatusCategory

	if req.Category != nil {
		value := StatusCategory(
			*req.Category,
		)

		category = &value
	}

	status, err := h.service.UpdateStatus(
		r.Context(),
		UpdateStatusInput{
			Identity:  identity,
			ProjectID: projectID,
			StatusID:  statusID,

			Name:     req.Name,
			Position: req.Position,
			Category: category,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidStatus),
			errors.Is(err, ErrNoChanges),
			errors.Is(err, ErrInvalidID):

			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrStatusNotFound):
			http.Error(
				w,
				"status not found",
				http.StatusNotFound,
			)

		case errors.Is(err, ErrStatusInUse):
			http.Error(w, "status is in use by tasks", http.StatusConflict)

		case errors.Is(err, ErrStatusAlreadyExists),
			errors.Is(err, ErrStatusPositionConflict):

			http.Error(
				w,
				"status conflict",
				http.StatusConflict,
			)

		case errors.Is(err, ErrForbidden):
			http.Error(
				w,
				"forbidden",
				http.StatusForbidden,
			)

		default:
			log.Printf(
				"update project status: %v",
				err,
			)

			http.Error(
				w,
				"failed to update status",
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
		toProjectStatusResponse(status),
	); err != nil {
		log.Printf(
			"encode project status: %v",
			err,
		)
	}
}

func (h *Handler) DeleteStatus(
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

	statusID := chi.URLParam(
		r,
		"statusID",
	)

	err := h.service.DeleteStatus(
		r.Context(),
		identity,
		projectID,
		statusID,
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrStatusInUse):
			http.Error(w, "status is in use by tasks", http.StatusConflict)

		case errors.Is(err, ErrInvalidID):
			http.Error(
				w,
				"invalid id",
				http.StatusBadRequest,
			)

		case errors.Is(err, ErrStatusNotFound):
			http.Error(
				w,
				"status not found",
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
				"delete project status: %v",
				err,
			)

			http.Error(
				w,
				"failed to delete status",
				http.StatusInternalServerError,
			)
		}

		return
	}

	w.WriteHeader(
		http.StatusNoContent,
	)
}

func (h *Handler) ReorderStatuses(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}
	var req reorderProjectStatusesRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
		return
	}
	err := h.service.ReorderStatuses(r.Context(), identity, chi.URLParam(r, "projectID"), req.StatusIDs)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidID), errors.Is(err, ErrInvalidStatusOrder):
			http.Error(w, "invalid status ordering", http.StatusBadRequest)
		case errors.Is(err, ErrNotFound):
			http.Error(w, "project not found", http.StatusNotFound)
		case errors.Is(err, ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			log.Printf("reorder project statuses: %v", err)
			http.Error(w, "failed to reorder statuses", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
