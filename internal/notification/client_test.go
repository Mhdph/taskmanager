package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSendWithIDWaitsForAcknowledgementAndKeepsKey(t *testing.T) {
	attempts := 0
	client := NewClient("https://notifications.test", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if got := r.Header.Get("Idempotency-Key"); got != "stable-reminder-key" {
			t.Fatalf("key changed: %q", got)
		}
		var payload SendInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.RecipientUserID != "alice" || payload.Type != EventTaskReminder {
			t.Fatalf("invalid payload: %+v", payload)
		}
		if attempts == 1 {
			return response(503), nil
		}
		return response(201), nil
	})})
	input := SendInput{RecipientUserID: "alice", Type: EventTaskReminder}
	if err := client.SendWithID(context.Background(), input, "stable-reminder-key"); err == nil {
		t.Fatal("failure acknowledged as success")
	}
	if err := client.SendWithID(context.Background(), input, "stable-reminder-key"); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}
