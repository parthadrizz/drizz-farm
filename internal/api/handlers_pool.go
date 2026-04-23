package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/store"
)

type poolHandlers struct {
	pool  *pool.Pool
	store *store.Store // optional — used to persist reservations
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

// Devices handles GET /api/v1/devices — list every instance the pool
// knows about, optionally filtered. Supports multiple query params
// that AND together:
//
//   ?free=true         → only state=warm AND not reserved (for automated clients)
//   ?state=warm        → explicit state filter
//   ?profile=api34_play
//   ?kind=android_emulator | android_usb
//   ?node=mac-mini-1   → we only serve our own instances, so this is
//                        really just a no-op self-check
//   ?reserved=true|false
//
// Useful for: test harnesses that want to pick a device explicitly,
// dashboards that show "what's actually available right now," and
// humans who want to reserve a specific one.
func (h *poolHandlers) Devices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := h.pool.Status()
	out := make([]pool.InstanceSnapshot, 0, len(status.Instances))
	for _, inst := range status.Instances {
		if v := q.Get("free"); v != "" {
			free, _ := strconv.ParseBool(v)
			if free {
				// "free" means actually allocatable by an automated caller:
				// warm AND not reserved.
				if string(inst.State) != "warm" || inst.Reserved {
					continue
				}
			}
		}
		if v := q.Get("state"); v != "" && string(inst.State) != v {
			continue
		}
		if v := q.Get("profile"); v != "" && inst.ProfileName != v {
			continue
		}
		if v := q.Get("kind"); v != "" && string(inst.DeviceKind) != v {
			continue
		}
		if v := q.Get("reserved"); v != "" {
			want, _ := strconv.ParseBool(v)
			if inst.Reserved != want {
				continue
			}
		}
		out = append(out, inst)
	}
	JSON(w, http.StatusOK, map[string]any{
		"devices": out,
		"total":   len(out),
	})
}

// Reserve handles POST /api/v1/devices/{id}/reserve.
// Body: {"label": "optional"}.
// Marks the instance as reserved — automated session requests will
// skip it until it's unreserved. Persisted in SQLite so a restart
// doesn't wipe reservations.
func (h *poolHandlers) Reserve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	inst := h.pool.FindByInstanceID(id)
	if inst == nil {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	if err := h.pool.ReserveInstance(id, req.Label); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "reserve_failed", Message: err.Error(), Code: 500})
		return
	}
	if h.store != nil && inst.Device != nil {
		_ = h.store.SetReservation(inst.Device.DisplayName(), req.Label)
	}
	JSON(w, http.StatusOK, map[string]any{"status": "reserved", "id": id, "label": req.Label})
}

// Unreserve handles DELETE /api/v1/devices/{id}/reserve.
func (h *poolHandlers) Unreserve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inst := h.pool.FindByInstanceID(id)
	if inst == nil {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	if err := h.pool.UnreserveInstance(id); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "unreserve_failed", Message: err.Error(), Code: 500})
		return
	}
	if h.store != nil && inst.Device != nil {
		_ = h.store.ClearReservation(inst.Device.DisplayName())
	}
	JSON(w, http.StatusOK, map[string]any{"status": "unreserved", "id": id})
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
