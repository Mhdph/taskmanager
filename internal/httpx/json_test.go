package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	for _, body := range []string{`{"name":"ok","typo":true}`, `{"name":"ok"} {}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := DecodeJSON(httptest.NewRecorder(), r, &payload{}); err == nil {
			t.Fatalf("DecodeJSON(%q) unexpectedly succeeded", body)
		}
	}
}

func TestDecodeJSONRejectsOversizedBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"`+strings.Repeat("x", int(DefaultJSONLimit))+`"}`))
	err := DecodeJSON(httptest.NewRecorder(), r, &struct {
		Name string `json:"name"`
	}{})
	if err == nil || DecodeErrorStatus(err) != 413 {
		t.Fatalf("expected oversized body error/status 413, got %v / %d", err, DecodeErrorStatus(err))
	}
}
