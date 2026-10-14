package main

import (
	"context"
	"net/http"
	"time"
)

// Readiness checks critical local dependencies. Hosted authentication, optional
// AI and notification providers do not block readiness of the whole API.
func readinessHandler(databaseCheck, storageCheck func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{"database": "ok", "storage": "ok"}
		status := http.StatusOK
		if err := databaseCheck(ctx); err != nil {
			checks["database"] = "unavailable"
			status = http.StatusServiceUnavailable
		}
		if err := storageCheck(ctx); err != nil {
			checks["storage"] = "unavailable"
			status = http.StatusServiceUnavailable
		}
		state := "ok"
		if status != http.StatusOK {
			state = "unavailable"
		}
		writeJSON(w, status, map[string]any{"status": state, "checks": checks})
	}
}
