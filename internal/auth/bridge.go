package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mhdph/taskmanager/internal/hosted"
	"github.com/mhdph/taskmanager/internal/httpx"
)

type bridgeExchanger interface {
	ConfirmBridgeToken(context.Context, string) (hosted.BridgeSession, error)
}

func BridgeHandler(client bridgeExchanger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var input struct {
			BridgeToken string `json:"bridgeToken"`
		}
		if err := httpx.DecodeJSON(w, r, &input); err != nil {
			http.Error(w, httpx.DecodeErrorMessage(err), httpx.DecodeErrorStatus(err))
			return
		}
		if strings.TrimSpace(input.BridgeToken) == "" || len(input.BridgeToken) > 8192 {
			http.Error(w, "invalid bridge token", http.StatusBadRequest)
			return
		}
		session, err := client.ConfirmBridgeToken(r.Context(), input.BridgeToken)
		if err != nil {
			if errors.Is(err, hosted.ErrInvalidBridgeToken) {
				http.Error(w, "invalid bridge token", http.StatusUnauthorized)
			} else {
				http.Error(w, "identity provider unavailable", http.StatusBadGateway)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session)
	}
}
