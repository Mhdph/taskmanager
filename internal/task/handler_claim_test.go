package task

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestClaimRejectsInvalidRequestsBeforeMutation(t *testing.T) {
	for _, tt := range []struct {
		name, body    string
		authenticated bool
		status        int
	}{
		{"unauthenticated", `{}`, false, http.StatusUnauthorized},
		{"cannot select another user", `{"assigneeUserId":"someone-else"}`, true, http.StatusBadRequest},
		{"cannot select identity", `{"userId":"someone-else"}`, true, http.StatusBadRequest},
		{"multiple JSON bodies", `{} {}`, true, http.StatusBadRequest},
		{"oversized body", `{"statusId":"` + strings.Repeat("x", 1<<20) + `"}`, true, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/projects/project/tasks/task/claim", strings.NewReader(tt.body))
			if tt.authenticated {
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "alice", TenantID: "tenant-a"}))
			}
			w := httptest.NewRecorder()
			// A nil service intentionally catches any attempt to mutate for invalid input.
			NewHandler(nil).Claim(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}
