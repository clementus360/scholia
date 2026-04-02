package http

import (
	"encoding/json"
	"net/http"
)

type ResponseEnvelope struct {
	Success bool           `json:"success"`
	Data    any            `json:"data,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type ResponseError struct {
	Message string `json:"message"`
}

// Success sends a successful JSON response.
func Success(w http.ResponseWriter, data any, statusCode int, meta ...map[string]any) {
	resp := ResponseEnvelope{Success: true, Data: data}
	if len(meta) > 0 && meta[0] != nil {
		resp.Meta = meta[0]
	}
	writeJSON(w, resp, statusCode)
}

// Error sends an error JSON response.
func Error(w http.ResponseWriter, message string, statusCode int) {
	writeJSON(w, ResponseEnvelope{Success: false, Error: &ResponseError{Message: message}}, statusCode)
}

func writeJSON(w http.ResponseWriter, payload any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if statusCode == http.StatusNoContent {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}
