package automation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/project"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// -------------------------------------------------------------------------
// DTOs
// -------------------------------------------------------------------------

type createRuleRequest struct {
	Name string `json:"name"`

	TriggerType   TriggerType `json:"triggerType"`
	TriggerConfig Trigger     `json:"triggerConfig"`

	Actions []Action `json:"actions"`

	Enabled *bool `json:"enabled"`
}

type updateRuleRequest struct {
	Name *string `json:"name"`

	TriggerConfig json.RawMessage `json:"triggerConfig"`

	Actions json.RawMessage `json:"actions"`

	Enabled *bool `json:"enabled"`
}

type listRulesResponse struct {
	Items []ruleResponse `json:"items"`
}

type ruleResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`

	Name string `json:"name"`

	TriggerType   string  `json:"triggerType"`
	TriggerConfig Trigger `json:"triggerConfig"`

	Actions []Action `json:"actions"`

	Enabled bool `json:"enabled"`

	CreatedByUserID string `json:"createdByUserId"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toRuleResponse(rule Rule) ruleResponse {
	actions := rule.Actions
	if actions == nil {
		actions = []Action{}
	}

	return ruleResponse{
		ID:        rule.ID,
		ProjectID: rule.ProjectID,

		Name: rule.Name,

		TriggerType:   string(rule.TriggerType),
		TriggerConfig: rule.TriggerConfig,

		Actions: actions,

		Enabled: rule.Enabled,

		CreatedByUserID: rule.CreatedByUserID,

		CreatedAt: rule.CreatedAt,
		UpdatedAt: rule.UpdatedAt,
	}
}

// decodeStrictRaw is httpx.DecodeJSON's strictness applied to a raw JSON
// fragment of the update request.
func decodeStrictRaw(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}

		return err
	}

	return nil
}

// -------------------------------------------------------------------------
// Handlers
// -------------------------------------------------------------------------

func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	rules, err := h.service.List(
		r.Context(),
		identity,
		chi.URLParam(r, "projectID"),
	)
	if err != nil {
		h.writeError(w, "list automation rules", err)
		return
	}

	items := make([]ruleResponse, 0, len(rules))
	for _, rule := range rules {
		items = append(items, toRuleResponse(rule))
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		listRulesResponse{Items: items},
	); err != nil {
		log.Printf("encode automation rule list: %v", err)
	}
}

func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req createRuleRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	rule, err := h.service.Create(
		r.Context(),
		CreateInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),

			Name: req.Name,

			TriggerType:   req.TriggerType,
			TriggerConfig: req.TriggerConfig,

			Actions: req.Actions,

			Enabled: req.Enabled,
		},
	)
	if err != nil {
		h.writeError(w, "create automation rule", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(
		toRuleResponse(rule),
	); err != nil {
		log.Printf("encode automation rule: %v", err)
	}
}

func (h *Handler) Update(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	var req updateRuleRequest

	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		http.Error(
			w,
			httpx.DecodeErrorMessage(err),
			httpx.DecodeErrorStatus(err),
		)
		return
	}

	var triggerConfig *Trigger

	if len(req.TriggerConfig) > 0 {
		var value Trigger

		if err := decodeStrictRaw(
			req.TriggerConfig,
			&value,
		); err != nil {
			http.Error(
				w,
				"invalid triggerConfig",
				http.StatusBadRequest,
			)
			return
		}

		triggerConfig = &value
	}

	var actions *[]Action

	if len(req.Actions) > 0 {
		var value []Action

		if err := decodeStrictRaw(
			req.Actions,
			&value,
		); err != nil {
			http.Error(
				w,
				"invalid actions",
				http.StatusBadRequest,
			)
			return
		}

		actions = &value
	}

	rule, err := h.service.Update(
		r.Context(),
		UpdateInput{
			Identity: identity,

			ProjectID: chi.URLParam(r, "projectID"),
			RuleID:    chi.URLParam(r, "ruleID"),

			Name: req.Name,

			TriggerConfig: triggerConfig,
			Actions:       actions,

			Enabled: req.Enabled,
		},
	)
	if err != nil {
		h.writeError(w, "update automation rule", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(
		toRuleResponse(rule),
	); err != nil {
		log.Printf("encode automation rule: %v", err)
	}
}

func (h *Handler) Delete(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "identity not found", http.StatusUnauthorized)
		return
	}

	err := h.service.Delete(
		r.Context(),
		identity,
		chi.URLParam(r, "projectID"),
		chi.URLParam(r, "ruleID"),
	)
	if err != nil {
		h.writeError(w, "delete automation rule", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// -------------------------------------------------------------------------
// Errors
// -------------------------------------------------------------------------

func (h *Handler) writeError(
	w http.ResponseWriter,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, ErrInvalidID),
		errors.Is(err, ErrInvalidName),
		errors.Is(err, ErrInvalidTrigger),
		errors.Is(err, ErrInvalidTriggerConfig),
		errors.Is(err, ErrInvalidAction),
		errors.Is(err, project.ErrInvalidID):

		http.Error(w, err.Error(), http.StatusBadRequest)

	case errors.Is(err, ErrRuleNotFound),
		errors.Is(err, project.ErrNotFound):

		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, ErrForbidden),
		errors.Is(err, project.ErrForbidden):

		http.Error(w, err.Error(), http.StatusForbidden)

	case errors.Is(err, ErrTooManyRules):

		http.Error(w, err.Error(), http.StatusConflict)

	default:
		log.Printf("%s: %v", action, err)
		http.Error(
			w,
			"failed to "+action,
			http.StatusInternalServerError,
		)
	}
}
