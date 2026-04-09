package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/session"
)

type sessionHandlers struct {
	broker *session.Broker
}

// CreateSession handles POST /api/v1/sessions
func (h *sessionHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req session.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: "Invalid JSON body: " + err.Error(),
			Code:    400,
		})
		return
	}

	if req.Source == "" {
		req.Source = "api"
	}

	sess, err := h.broker.Create(r.Context(), req)
	if err != nil {
		Error(w, err)
		return
	}

	JSON(w, http.StatusCreated, sess)
}

// GetSession handles GET /api/v1/sessions/:id
func (h *sessionHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := h.broker.Get(id)
	if err != nil {
		Error(w, err)
		return
	}
	JSON(w, http.StatusOK, sess)
}

// ReleaseSession handles DELETE /api/v1/sessions/:id
func (h *sessionHandlers) Release(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.broker.Release(r.Context(), id); err != nil {
		Error(w, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{
		"status":  "released",
		"session": id,
	})
}

// ListSessions handles GET /api/v1/sessions
func (h *sessionHandlers) List(w http.ResponseWriter, r *http.Request) {
	sessions := h.broker.List()
	JSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"total":    len(sessions),
		"active":   h.broker.ActiveCount(),
		"queued":   h.broker.QueueDepth(),
	})
}
