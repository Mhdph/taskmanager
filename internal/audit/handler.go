package audit

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
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

type entryResponse struct {
	ID string `json:"id"`

	Action string `json:"action"`

	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`

	ProjectID *string `json:"projectId"`
	TaskID    *string `json:"taskId"`

	ActorUserID string `json:"actorUserId"`

	Metadata json.RawMessage `json:"metadata"`

	CreatedAt time.Time `json:"createdAt"`
}

type listResponse struct {
	Items []entryResponse `json:"items"`

	NextCursor *string `json:"nextCursor"`
}

func toEntryResponse(
	entry Entry,
) entryResponse {
	return entryResponse{
		ID: entry.ID,

		Action: string(entry.Action),

		EntityType: string(entry.EntityType),
		EntityID:   entry.EntityID,

		ProjectID: entry.ProjectID,
		TaskID:    entry.TaskID,

		ActorUserID: entry.ActorUserID,

		Metadata: entry.Metadata,

		CreatedAt: entry.CreatedAt,
	}
}

type activityQueryParams struct {
	ProjectID   *string
	TaskID      *string
	ActorUserID *string
	EntityType  *EntityType

	Cursor string

	Limit int32
}

func parseActivityQuery(
	r *http.Request,
) (activityQueryParams, bool) {
	query := r.URL.Query()

	var params activityQueryParams

	if raw := query.Get("project"); raw != "" {
		params.ProjectID = &raw
	}

	if raw := query.Get("task"); raw != "" {
		params.TaskID = &raw
	}

	if raw := query.Get("actor"); raw != "" {
		params.ActorUserID = &raw
	}

	if raw := query.Get("type"); raw != "" {
		value := EntityType(raw)
		params.EntityType = &value
	}

	params.Cursor = query.Get("cursor")

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || limit < 0 {
			return params, false
		}

		params.Limit = int32(limit)
	}

	return params, true
}

func (h *Handler) authorizeIdentity(
	w http.ResponseWriter,
	r *http.Request,
) (*auth.Identity, bool) {
	identity, ok := auth.IdentityFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusUnauthorized,
		)
		return nil, false
	}

	return &identity, true
}

func (h *Handler) writeError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrInvalidFilter),
		errors.Is(err, ErrInvalidCursor):

		http.Error(
			w,
			"invalid request",
			http.StatusBadRequest,
		)

	case errors.Is(err, project.ErrNotFound):
		http.Error(
			w,
			"project not found",
			http.StatusNotFound,
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
			"failed to list activity",
			http.StatusInternalServerError,
		)
	}
}

func (h *Handler) writeList(
	w http.ResponseWriter,
	entries []Entry,
	next *AuditCursor,
) {
	items := make(
		[]entryResponse,
		0,
		len(entries),
	)

	for _, entry := range entries {
		items = append(
			items,
			toEntryResponse(entry),
		)
	}

	response := listResponse{
		Items: items,
	}

	if next != nil {
		token, err := EncodeAuditCursor(*next)
		if err != nil {
			log.Printf(
				"encode activity cursor: %v",
				err,
			)

			http.Error(
				w,
				"failed to list activity",
				http.StatusInternalServerError,
			)
			return
		}

		response.NextCursor = &token
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf(
			"encode activity response: %v",
			err,
		)
	}
}

// Activity serves GET /activity — the tenant-wide feed, scoped to the
// projects the caller belongs to.
func (h *Handler) Activity(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.authorizeIdentity(w, r)
	if !ok {
		return
	}

	params, ok := parseActivityQuery(r)
	if !ok {
		http.Error(
			w,
			"invalid request",
			http.StatusBadRequest,
		)
		return
	}

	options := ViewOptions{
		Identity: *identity,

		ProjectID:   params.ProjectID,
		TaskID:      params.TaskID,
		ActorUserID: params.ActorUserID,
		EntityType:  params.EntityType,

		Limit: params.Limit,
	}

	if params.Cursor != "" {
		cursor, err := DecodeAuditCursor(params.Cursor)
		if err != nil {
			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)
			return
		}

		options.Cursor = &cursor
	}

	entries, next, err := h.service.View(
		r.Context(),
		options,
	)
	if err != nil {
		h.writeError(
			w,
			"list activity",
			err,
		)
		return
	}

	h.writeList(w, entries, next)
}

// ProjectActivity serves GET /projects/{projectID}/activity.
func (h *Handler) ProjectActivity(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := h.authorizeIdentity(w, r)
	if !ok {
		return
	}

	projectID := chi.URLParam(
		r,
		"projectID",
	)

	params, ok := parseActivityQuery(r)
	if !ok {
		http.Error(
			w,
			"invalid request",
			http.StatusBadRequest,
		)
		return
	}

	options := ViewOptions{
		Identity: *identity,

		ProjectID:   &projectID,
		TaskID:      params.TaskID,
		ActorUserID: params.ActorUserID,
		EntityType:  params.EntityType,

		Limit: params.Limit,
	}

	if params.Cursor != "" {
		cursor, err := DecodeAuditCursor(params.Cursor)
		if err != nil {
			http.Error(
				w,
				"invalid request",
				http.StatusBadRequest,
			)
			return
		}

		options.Cursor = &cursor
	}

	entries, next, err := h.service.View(
		r.Context(),
		options,
	)
	if err != nil {
		h.writeError(
			w,
			"list project activity",
			err,
		)
		return
	}

	h.writeList(w, entries, next)
}
