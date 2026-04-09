package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// Error writes a JSON error response, mapping known errors to HTTP status codes.
func Error(w http.ResponseWriter, err error) {
	status, code, message := mapError(err)
	JSON(w, status, ErrorResponse{
		Error:   code,
		Message: message,
		Code:    status,
	})
}

func mapError(err error) (httpStatus int, code string, message string) {
	switch {
	case errors.Is(err, pool.ErrPoolExhausted):
		return http.StatusServiceUnavailable, "pool_exhausted", "No emulators available. Try again shortly."
	case errors.Is(err, pool.ErrProfileNotFound):
		return http.StatusBadRequest, "profile_not_found", err.Error()
	case errors.Is(err, pool.ErrInstanceNotFound):
		return http.StatusNotFound, "instance_not_found", err.Error()
	case errors.Is(err, session.ErrSessionNotFound):
		return http.StatusNotFound, "session_not_found", err.Error()
	case errors.Is(err, session.ErrSessionNotActive):
		return http.StatusConflict, "session_not_active", err.Error()
	case errors.Is(err, session.ErrQueueFull):
		return http.StatusServiceUnavailable, "queue_full", "Session queue is full. Try again later."
	case errors.Is(err, session.ErrQueueTimeout):
		return http.StatusGatewayTimeout, "queue_timeout", "Timed out waiting for an available emulator."
	default:
		return http.StatusInternalServerError, "internal_error", "An internal error occurred."
	}
}
