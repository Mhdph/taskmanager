package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhdph/taskmanager/internal/hosted"
)

type fakeBridge struct {
	err    error
	called bool
}

func (f *fakeBridge) ConfirmBridgeToken(_ context.Context, token string) (hosted.BridgeSession, error) {
	f.called = true
	return hosted.BridgeSession{Token: "session-token", AppID: "tenant", UserID: "alice", FromSupportMenu: "no"}, f.err
}
func TestBridgeHandler(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		err        error
		status     int
		called     bool
	}{
		{"success", `{"bridgeToken":"single-use"}`, nil, 200, true},
		{"invalid", `{"bridgeToken":"bad"}`, hosted.ErrInvalidBridgeToken, 401, true},
		{"unavailable", `{"bridgeToken":"bridge"}`, errors.New("secret provider error"), 502, true},
		{"missing", `{"bridgeToken":""}`, nil, 400, false},
		{"malformed", `{"bridgeToken":1}`, nil, 400, false},
		{"oversized", `{"bridgeToken":"` + strings.Repeat("x", 9000) + `"}`, nil, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeBridge{err: tc.err}
			w := httptest.NewRecorder()
			BridgeHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/bridge", strings.NewReader(tc.body)))
			if w.Code != tc.status || client.called != tc.called || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("provider details leaked")
			}
		})
	}
}
