package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfirmBridgeToken(t *testing.T) {
	for _, tc := range []struct {
		payload string
		valid   bool
	}{
		{`{"data":{"nupConfirmBridgeToken":{"token":"session-token","appId":"tenant","userId":"alice","fromSupportMenu":"yes"}}}`, true},
		{`{"errors":[{"message":"expired"}]}`, false},
		{`{"data":{"nupConfirmBridgeToken":{"token":"partial"}}}`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body graphQLRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Variables["bridgeToken"] != "single-use" {
				t.Error("bridge exchange does not match NUP contract")
			}
			if r.Header.Get("X-APIKEY") != "" {
				t.Error("previous session sent during bridge exchange")
			}
			_, _ = w.Write([]byte(tc.payload))
		}))
		session, err := NewClient(server.URL, server.Client()).ConfirmBridgeToken(context.Background(), "single-use")
		server.Close()
		if tc.valid {
			if err != nil || session.UserID != "alice" || session.FromSupportMenu != "yes" {
				t.Fatalf("invalid session: %+v %v", session, err)
			}
		} else if !errors.Is(err, ErrInvalidBridgeToken) {
			t.Fatalf("incomplete/error response accepted: %v", err)
		}
	}
}
