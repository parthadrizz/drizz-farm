package api

import (
	"net/http"

	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type poolHandlers struct {
	pool *pool.Pool
}

// Status handles GET /api/v1/pool
func (h *poolHandlers) Status(w http.ResponseWriter, r *http.Request) {
	status := h.pool.Status()
	JSON(w, http.StatusOK, status)
}

// Available handles GET /api/v1/pool/available
func (h *poolHandlers) Available(w http.ResponseWriter, r *http.Request) {
	profile := r.URL.Query().Get("profile")
	count := h.pool.Available(profile)
	JSON(w, http.StatusOK, map[string]any{
		"available": count,
		"profile":   profile,
	})
}
