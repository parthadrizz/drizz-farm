package api

import (
	"encoding/json"
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

// Boot handles POST /api/v1/pool/boot — boot a specific AVD on-demand
func (h *poolHandlers) Boot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AVDName string `json:"avd_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AVDName == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "avd_name is required", Code: 400,
		})
		return
	}

	inst, err := h.pool.BootAVD(r.Context(), req.AVDName)
	if err != nil {
		Error(w, err)
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"status":   "booting",
		"instance": inst.Snapshot(),
	})
}

// Shutdown handles POST /api/v1/pool/shutdown — shut down a specific instance
func (h *poolHandlers) Shutdown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstanceID == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "instance_id is required", Code: 400,
		})
		return
	}

	if err := h.pool.ShutdownInstance(r.Context(), req.InstanceID); err != nil {
		Error(w, err)
		return
	}

	JSON(w, http.StatusOK, map[string]any{"status": "shutdown", "instance_id": req.InstanceID})
}
