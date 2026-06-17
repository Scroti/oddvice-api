// Package httpx holds small HTTP helpers shared across feature packages.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteJSON serializes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// ErrorBody is the standard error envelope returned by the API.
type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// WriteError responds with a JSON error envelope.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorBody{
		Error:   http.StatusText(status),
		Message: message,
	})
}
