package sprint

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
)

func sprintRouter(handler *Handler) chi.Router {
	router := chi.NewRouter()

	router.Route("/projects/{projectID}", func(r chi.Router) {
		r.Get("/sprints", handler.List)
		r.Post("/sprints", handler.Create)
		r.Get("/sprints/{sprintID}", handler.Get)
		r.Patch("/sprints/{sprintID}", handler.Update)
		r.Delete("/sprints/{sprintID}", handler.Delete)
		r.Post("/sprints/{sprintID}/start", handler.Start)
		r.Post("/sprints/{sprintID}/complete", handler.Complete)
		r.Get("/sprints/{sprintID}/report", handler.Report)
		r.Put("/tasks/{taskID}/sprint", handler.SetTaskSprint)
	})

	return router
}

func TestSprintHandlerRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name           string
		method         string
		path           string
		body           string
		authenticated  bool
		expectedStatus int
	}{
		{
			name:           "unauthenticated list",
			method:         http.MethodGet,
			path:           "/projects/p1/sprints",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid json",
			method:         http.MethodPost,
			path:           "/projects/p1/sprints",
			body:           "{",
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid state filter",
			method:         http.MethodGet,
			path:           "/projects/p1/sprints?state=broken",
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid limit",
			method:         http.MethodGet,
			path:           "/projects/p1/sprints?limit=abc",
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid cursor",
			method:         http.MethodGet,
			path:           "/projects/p1/sprints?cursor=not-a-cursor",
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid goal type",
			method:         http.MethodPatch,
			path:           "/projects/p1/sprints/s1",
			body:           `{"goal":42}`,
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid planned start",
			method:         http.MethodPatch,
			path:           "/projects/p1/sprints/s1",
			body:           `{"plannedStartAt":"not-a-date"}`,
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid sprint id on assignment",
			method:         http.MethodPut,
			path:           "/projects/p1/tasks/t1/sprint",
			body:           `{"sprintId":"not-a-uuid"}`,
			authenticated:  true,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(
				testCase.method,
				testCase.path,
				strings.NewReader(testCase.body),
			)

			if testCase.authenticated {
				request = request.WithContext(
					auth.WithIdentity(
						request.Context(),
						auth.Identity{
							UserID:   "alice",
							TenantID: sprintTestTenant,
						},
					),
				)
			}

			recorder := httptest.NewRecorder()

			// A nil service catches any attempt to reach the database
			// with an invalid request.
			sprintRouter(NewHandler(nil)).ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != testCase.expectedStatus {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					testCase.expectedStatus,
					recorder.Body.String(),
				)
			}
		})
	}
}

func TestSprintHandlerPermissionsAndErrors(t *testing.T) {
	service, pool, ctx := integrationSprintService(t)

	projectID := uuid.NewString()
	insertSprintProject(t, pool, ctx, projectID)

	insertSprintMember(t, pool, ctx, projectID, "alice", "manager")
	insertSprintMember(t, pool, ctx, projectID, "carol", "viewer")

	handler := NewHandler(service)
	router := sprintRouter(handler)

	request := func(
		method string,
		path string,
		body string,
		userID string,
	) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest(
			method,
			path,
			strings.NewReader(body),
		)

		req = req.WithContext(
			auth.WithIdentity(
				req.Context(),
				auth.Identity{
					UserID:   userID,
					TenantID: sprintTestTenant,
				},
			),
		)

		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, req)

		return recorder
	}

	// A viewer cannot create a sprint.
	recorder := request(
		http.MethodPost,
		"/projects/"+projectID+"/sprints",
		`{"name":"Sprint","plannedEndAt":"2030-01-02T00:00:00Z"}`,
		"carol",
	)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"viewer create status = %d, want 403; body = %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	// A manager can.
	recorder = request(
		http.MethodPost,
		"/projects/"+projectID+"/sprints",
		`{"name":"Sprint","plannedEndAt":"2030-01-02T00:00:00Z"}`,
		"alice",
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf(
			"manager create status = %d, want 201; body = %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	// An unknown sprint is a 404.
	recorder = request(
		http.MethodGet,
		"/projects/"+projectID+"/sprints/"+uuid.NewString(),
		"",
		"alice",
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"missing sprint status = %d, want 404; body = %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	// Starting a sprint that does not exist is a 404, and completing a
	// planned sprint is a conflict.
	sprint, err := service.Create(ctx, CreateInput{
		Identity: sprintIdentity("alice"),

		ProjectID: projectID,

		Name: "Planned",

		PlannedEndAt: timePtr(time.Now().Add(24 * time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder = request(
		http.MethodPost,
		"/projects/"+projectID+"/sprints/"+sprint.ID+"/complete",
		`{}`,
		"alice",
	)
	if recorder.Code != http.StatusConflict {
		t.Fatalf(
			"complete planned status = %d, want 409; body = %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	// The list is paginated and returns the created sprint.
	recorder = request(
		http.MethodGet,
		"/projects/"+projectID+"/sprints?limit=1",
		"",
		"alice",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"list status = %d, want 200; body = %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	if !strings.Contains(recorder.Body.String(), sprint.ID) {
		t.Fatalf("list body = %s, want the sprint", recorder.Body.String())
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
