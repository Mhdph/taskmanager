package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const DefaultJSONLimit int64 = 1 << 20

var ErrBodyTooLarge = errors.New("request body too large")

// DecodeJSON bounds mutation payloads, rejects unknown fields, and requires
// exactly one JSON value. The caller maps ErrBodyTooLarge to HTTP 413.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, DefaultJSONLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrBodyTooLarge
		}
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrBodyTooLarge
		}
		return err
	}
	return nil
}

func DecodeErrorStatus(err error) int {
	if errors.Is(err, ErrBodyTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func DecodeErrorMessage(err error) string {
	if errors.Is(err, ErrBodyTooLarge) {
		return "request body too large"
	}
	return "invalid request body"
}
