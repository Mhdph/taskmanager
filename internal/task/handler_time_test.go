package task

import (
	"errors"
	"github.com/mhdph/taskmanager/internal/project"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimeEntryErrorResponses(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
	}{
		{ErrTimerActive, http.StatusConflict},
		{ErrTimerNotFound, http.StatusNotFound},
		{ErrTimeEntryNotFound, http.StatusNotFound},
		{ErrInvalidID, http.StatusBadRequest},
		{project.ErrInvalidCursor, http.StatusBadRequest},
		{project.ErrForbidden, http.StatusForbidden},
		{errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		w := httptest.NewRecorder()
		writeTimeResult(w, http.StatusOK, nil, tt.err)
		if w.Code != tt.status {
			t.Fatalf("%v returned %d, want %d", tt.err, w.Code, tt.status)
		}
	}
}

func TestTimeEntryEndpointsRequireIdentity(t *testing.T) {
	h := NewHandler(nil)
	for _, handler := range []http.HandlerFunc{h.StartTimer, h.StopTimer, h.ListTimeEntries, h.DeleteTimeEntry} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated request returned %d", w.Code)
		}
	}
}
