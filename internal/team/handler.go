package team

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/member"
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

type teamResponse struct {
	ID string `json:"id"`

	Name string `json:"name"`

	Icon  string `json:"icon"`
	Color string `json:"color"`

	Description string `json:"description"`

	CreatedByUserID string `json:"createdByUserId"`

	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

func toTeamResponse(item Team) teamResponse {
	return teamResponse{
		ID: item.ID,

		Name: item.Name,

		Icon:  item.Icon,
		Color: item.Color,

		Description: item.Description,

		CreatedByUserID: item.CreatedByUserID,

		CreatedAt: item.CreatedAt.Unix(),
		UpdatedAt: item.UpdatedAt.Unix(),
	}
}

type listTeamsResponse struct {
	Items []teamResponse `json:"items"`
}

func (h *Handler) identity(
	w http.ResponseWriter,
	r *http.Request,
) (auth.Identity, bool) {
	identity, ok := auth.IdentityFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)

		return auth.Identity{}, false
	}

	return identity, true
}

func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teams, err := h.service.List(
		r.Context(),
		identity,
	)
	if err != nil {
		h.writeError(w, "list teams", err)
		return
	}

	items := make(
		[]teamResponse,
		0,
		len(teams),
	)

	for _, item := range teams {
		items = append(
			items,
			toTeamResponse(item),
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		listTeamsResponse{Items: items},
	); err != nil {
		log.Printf(
			"encode team list: %v",
			err,
		)
	}
}

type teamMembersResponse struct {
	// UserIds is the full member list of the team, sorted.
	UserIds []string `json:"userIds"`
}

func (h *Handler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teamID := chi.URLParam(
		r,
		"teamID",
	)

	item, err := h.service.Get(
		r.Context(),
		identity,
		teamID,
	)
	if err != nil {
		h.writeError(w, "get team", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toTeamResponse(item),
	); err != nil {
		log.Printf(
			"encode team response: %v",
			err,
		)
	}
}

func (h *Handler) ListMembers(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teamID := chi.URLParam(
		r,
		"teamID",
	)

	ids, err := h.service.ListMembers(
		r.Context(),
		ListMembersInput{
			Identity: identity,

			TeamID: teamID,
		},
	)
	if err != nil {
		h.writeError(w, "list team members", err)
		return
	}

	if ids == nil {
		ids = make([]string, 0)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		teamMembersResponse{UserIds: ids},
	); err != nil {
		log.Printf(
			"encode team member list: %v",
			err,
		)
	}
}

type setTeamMembersRequest struct {
	UserIds []string `json:"userIds"`
}

func (h *Handler) SetMembers(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teamID := chi.URLParam(
		r,
		"teamID",
	)

	var req setTeamMembersRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	if err := h.service.SetMembers(
		r.Context(),
		SetMembersInput{
			Identity: identity,

			TeamID: teamID,

			UserIDs: req.UserIds,
		},
	); err != nil {
		h.writeError(w, "set team members", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type teamRequest struct {
	Name string `json:"name"`

	// Icon is a material-ish icon id from the UI's PICONS; empty accepts
	// the default ('users').
	Icon string `json:"icon"`

	Color string `json:"color"`

	Description string `json:"description"`
}

func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	var req teamRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	item, err := h.service.Create(
		r.Context(),
		CreateInput{
			Identity: identity,

			Name: req.Name,

			Icon:  req.Icon,
			Color: req.Color,

			Description: req.Description,
		},
	)
	if err != nil {
		h.writeError(w, "create team", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		toTeamResponse(item),
	); err != nil {
		log.Printf(
			"encode team response: %v",
			err,
		)
	}
}

func (h *Handler) Update(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teamID := chi.URLParam(
		r,
		"teamID",
	)

	var req teamRequest

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

		TeamID: teamID,
	}

	// The rename prompt omits the other fields; omitted means keep.
	if req.Name != "" {
		input.Name = &req.Name
	}

	if req.Icon != "" {
		input.Icon = &req.Icon
	}

	if req.Color != "" {
		input.Color = &req.Color
	}

	if req.Description != "" {
		input.Description = &req.Description
	}

	item, err := h.service.Update(
		r.Context(),
		input,
	)
	if err != nil {
		h.writeError(w, "update team", err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		toTeamResponse(item),
	); err != nil {
		log.Printf(
			"encode team response: %v",
			err,
		)
	}
}

func (h *Handler) Delete(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}

	teamID := chi.URLParam(
		r,
		"teamID",
	)

	err := h.service.Delete(
		r.Context(),
		identity,
		teamID,
	)
	if err != nil {
		h.writeError(w, "delete team", err)
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
	case errors.Is(err, ErrTeamNotFound):
		http.Error(
			w,
			"team not found",
			http.StatusNotFound,
		)

	case errors.Is(err, ErrTeamConflict):
		http.Error(
			w,
			"a team with this name already exists",
			http.StatusConflict,
		)

	case errors.Is(err, ErrTeamNameRequired):
		http.Error(
			w,
			"team name is required",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrTeamNameTooLong):
		http.Error(
			w,
			"team name is too long",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrTeamDescriptionTooLong):
		http.Error(
			w,
			"team description is too long",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrTeamMembersInvalid):
		http.Error(
			w,
			"team members invalid: every userId must be a workspace member",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrNothingToUpdate):
		http.Error(
			w,
			"no changes provided",
			http.StatusBadRequest,
		)

	case errors.Is(err, ErrForbidden),
		errors.Is(err, member.ErrForbidden):

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
			"failed to manage teams",
			http.StatusInternalServerError,
		)
	}
}
