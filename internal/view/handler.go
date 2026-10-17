package view

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

type Handler struct {
	service *Service
}

func NewHandler(
	service *Service,
) *Handler {
	return &Handler{
		service: service,
	}
}

type viewResponse struct {
	ID string `json:"id"`

	ProjectID string `json:"projectId"`

	Name string `json:"name"`

	// Layout: list | board | table.
	Type string `json:"type"`

	Filters []Filter `json:"filters"`

	CreatedByUserID string `json:"createdByUserId"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

func toViewResponse(item SavedView) viewResponse {
	filters := item.Filters

	if filters == nil {
		filters = make([]Filter, 0)
	}

	return viewResponse{
		ID: item.ID,

		ProjectID: item.ProjectID,

		Name: item.Name,

		Type: item.ViewType,

		Filters: filters,

		CreatedByUserID: item.CreatedByUserID,

		CreatedAt: item.CreatedAt.Unix(),
		UpdatedAt: item.UpdatedAt.Unix(),
	}
}

type listResponse struct {
	Items []viewResponse `json:"items"`
}

func (h *Handler) List(
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

	views, err := h.service.List(
		r.Context(),
		ListInput{
			Identity: identity,

			ProjectID: projectID,
		},
	)
	if err != nil {
		h.writeError(w, "list views", err)
		return
	}

	items := make(
		[]viewResponse,
		0,
		len(views),
	)

	for _, item := range views {
		q := toViewResponse(item)
		items = append(items, q)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		listResponse{Items: items},
	); err != nil {
		log.Printf(
			"encode view list: %v",
			err,
		)
	}
}

type createRequest struct {
	Name string `json:"name"`

	// Layout: list | board | table; empty defaults to list.
	Type string `json:"type"`

	// Pointer so an omitted filters field differs from an empty one
	// (omitted = keep, [] = clear).
	Filters *[]Filter `json:"filters"`
}

func (h *Handler) Create(
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

	var req createRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	filters := make([]Filter, 0)

	if req.Filters != nil {
		filters = *req.Filters
	}

	item, err := h.service.Create(
		r.Context(),
		CreateInput{
			Identity: identity,

			ProjectID: projectID,

			Name:     req.Name,
			ViewType: req.Type,

			Filters: filters,
		},
	)
	if err != nil {
		h.writeError(w, "create view", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		toViewResponse(item),
	); err != nil {
		log.Printf(
			"encode view response: %v",
			err,
		)
	}
}

func (h *Handler) Update(
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

	viewID := chi.URLParam(
		r,
		"viewID",
	)

	var req createRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	input := UpdateInput{
		Identity: identity,

		ProjectID: projectID,
		ViewID:    viewID,

		Filters: req.Filters,
	}
	if req.Name != "" {
		input.Name = &req.Name
	}

	if req.Type != "" {
		input.ViewType = &req.Type
	}

	item, err := h.service.Update(
		r.Context(),
		input,
	)
	if err != nil {
		h.writeError(w, "update view", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toViewResponse(item),
	); err != nil {
		log.Printf(
			"encode view response: %v",
			err,
		)
	}
}

func (h *Handler) Delete(
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

	viewID := chi.URLParam(
		r,
		"viewID",
	)

	err := h.service.Delete(
		r.Context(),
		DeleteInput{
			Identity: identity,

			ProjectID: projectID,
			ViewID:    viewID,
		},
	)
	if err != nil {
		h.writeError(w, "delete view", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrViewNotFound),
		errors.Is(err, project.ErrNotFound):

		http.Error(
			w,
			"view not found",
			http.StatusNotFound,
		)

	case errors.Is(err, ErrViewConflict):
		http.Error(
			w,
			"a view with this name already exists",
			http.StatusConflict,
		)

	case errors.Is(err, ErrViewNameRequired):
		http.Error(
			w,
			"view name is required",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrViewInvalidType):
		http.Error(
			w,
			"type must be list, board or table",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrViewFiltersTooMany):
		http.Error(
			w,
			"a view cannot carry more than 10 filters",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrViewInvalidFilter):
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrNothingToUpdate):
		http.Error(
			w,
			"invalid request",
			http.StatusBadRequest,
		)

	case errors.Is(err, project.ErrForbidden):
		http.Error(
			w,
			"forbidden",
			http.StatusForbidden,
		)

	default:
		log.Printf(
			"%s: %v",
			action,
			err,
		)

		http.Error(
			w,
			"failed to manage saved views",
			http.StatusInternalServerError,
		)
	}
}
