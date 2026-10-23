package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersOnSuccessAndErrors(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError} {
		w := httptest.NewRecorder()
		SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("unsafe response: %d %v", w.Code, w.Header())
		}
	}
}
