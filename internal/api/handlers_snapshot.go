package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type snapshotHandlers struct {
	pool *pool.Pool
	adb  *android.ADBClient
}

// Save handles POST /api/v1/sessions/:id/snapshot/save
func (h *snapshotHandlers) Save(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_request", Message: "name is required", Code: 400})
		return
	}

	serial := h.findSerial(id)
	if serial == "" {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	_, err := h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("avd snapshot save %s", req.Name))
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "snapshot_failed", Message: err.Error(), Code: 500})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "saved", "name": req.Name})
}

// Restore handles POST /api/v1/sessions/:id/snapshot/restore
func (h *snapshotHandlers) Restore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_request", Message: "name is required", Code: 400})
		return
	}

	serial := h.findSerial(id)
	if serial == "" {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	_, err := h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("avd snapshot load %s", req.Name))
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "snapshot_failed", Message: err.Error(), Code: 500})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "restored", "name": req.Name})
}

// List handles GET /api/v1/sessions/:id/snapshots
func (h *snapshotHandlers) List(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	serial := h.findSerial(id)
	if serial == "" {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	out, err := h.adb.EmuCommand(r.Context(), serial, "avd snapshot list")
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "snapshot_failed", Message: err.Error(), Code: 500})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"snapshots": out})
}

// Delete handles DELETE /api/v1/sessions/:id/snapshot/:name
func (h *snapshotHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")

	serial := h.findSerial(id)
	if serial == "" {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	_, err := h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("avd snapshot delete %s", name))
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "snapshot_failed", Message: err.Error(), Code: 500})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

func (h *snapshotHandlers) findSerial(id string) string {
	if h.pool == nil { return "" }
	status := h.pool.Status()
	for _, inst := range status.Instances {
		if inst.ID == id || inst.SessionID == id {
			return inst.Serial
		}
	}
	if inst, ok := h.pool.GetInstance(id); ok && inst.Device != nil {
		return inst.Device.Serial()
	}
	return ""
}
