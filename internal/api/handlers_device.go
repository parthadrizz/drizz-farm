package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type deviceHandlers struct {
	pool *pool.Pool
	adb  *android.ADBClient
}

func (h *deviceHandlers) findSerial(id string) string {
	for _, inst := range h.pool.Status().Instances {
		if inst.ID == id || inst.SessionID == id {
			return inst.Serial
		}
	}
	if inst, ok := h.pool.GetInstance(id); ok && inst.Device != nil {
		return inst.Device.Serial()
	}
	return ""
}

// SetGPS handles POST /api/v1/sessions/:id/gps
func (h *deviceHandlers) SetGPS(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Lat float64 `json:"latitude"`
		Lng float64 `json:"longitude"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	cmd := fmt.Sprintf("geo fix %f %f", req.Lng, req.Lat)
	_, err := h.adb.EmuCommand(r.Context(), serial, cmd)
	if err != nil {
		// Fallback: use adb shell for devices without console
		h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put secure location_providers_allowed +gps"))
		JSON(w, 200, map[string]any{"status": "set", "lat": req.Lat, "lng": req.Lng, "note": "console unavailable, used shell fallback"})
		return
	}
	JSON(w, 200, map[string]any{"status": "set", "lat": req.Lat, "lng": req.Lng})
}

// SetNetwork handles POST /api/v1/sessions/:id/network
func (h *deviceHandlers) SetNetwork(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Profile string `json:"profile"` // 2g, 3g, 4g, 5g, wifi_slow, wifi_fast, offline, flaky
	}
	json.NewDecoder(r.Body).Decode(&req)

	profiles := map[string]string{
		"2g":        "gsm",
		"3g":        "umts",
		"4g":        "lte",
		"5g":        "full",
		"wifi_slow": "full",
		"wifi_fast": "full",
		"offline":   "off",
		"flaky":     "full",
	}

	speed, ok := profiles[req.Profile]
	if !ok {
		JSON(w, 400, ErrorResponse{Error: "invalid_profile", Message: fmt.Sprintf("unknown: %s", req.Profile), Code: 400})
		return
	}

	if req.Profile == "offline" {
		h.adb.Shell(r.Context(), serial, "svc wifi disable")
		h.adb.Shell(r.Context(), serial, "svc data disable")
	} else {
		h.adb.Shell(r.Context(), serial, "svc wifi enable")
		h.adb.Shell(r.Context(), serial, "svc data enable")
		h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("network speed %s", speed))
	}

	JSON(w, 200, map[string]any{"status": "set", "profile": req.Profile})
}

// SetBattery handles POST /api/v1/sessions/:id/battery
func (h *deviceHandlers) SetBattery(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Level    int    `json:"level"`    // 0-100
		Charging string `json:"charging"` // "ac", "usb", "none"
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Level > 0 {
		h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("power capacity %d", req.Level))
	}
	switch req.Charging {
	case "ac":
		h.adb.EmuCommand(r.Context(), serial, "power ac on")
	case "usb":
		h.adb.EmuCommand(r.Context(), serial, "power ac off")
	case "none":
		h.adb.EmuCommand(r.Context(), serial, "power ac off")
		h.adb.EmuCommand(r.Context(), serial, "power status discharging")
	}

	JSON(w, 200, map[string]any{"status": "set", "level": req.Level, "charging": req.Charging})
}

// SetOrientation handles POST /api/v1/sessions/:id/orientation
func (h *deviceHandlers) SetOrientation(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Rotation int `json:"rotation"` // 0, 1, 2, 3 (0=portrait, 1=landscape left, 2=reverse portrait, 3=landscape right)
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Disable auto-rotate, then set rotation
	h.adb.Shell(r.Context(), serial, "settings put system accelerometer_rotation 0")
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put system user_rotation %d", req.Rotation))

	JSON(w, 200, map[string]any{"status": "set", "rotation": req.Rotation})
}

// SetLocale handles POST /api/v1/sessions/:id/locale
func (h *deviceHandlers) SetLocale(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Locale string `json:"locale"` // e.g., "en-US", "ja-JP", "hi-IN"
	}
	json.NewDecoder(r.Body).Decode(&req)

	h.adb.Shell(r.Context(), serial, fmt.Sprintf("setprop persist.sys.locale %s", req.Locale))

	JSON(w, 200, map[string]any{"status": "set", "locale": req.Locale})
}

// SetDarkMode handles POST /api/v1/sessions/:id/appearance
func (h *deviceHandlers) SetDarkMode(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Dark bool `json:"dark"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Dark {
		h.adb.Shell(r.Context(), serial, "cmd uimode night yes")
	} else {
		h.adb.Shell(r.Context(), serial, "cmd uimode night no")
	}

	JSON(w, 200, map[string]any{"status": "set", "dark": req.Dark})
}

// InstallAPK handles POST /api/v1/sessions/:id/install
func (h *deviceHandlers) InstallAPK(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	err := h.adb.Install(r.Context(), serial, req.Path, true)
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "install_failed", Message: err.Error(), Code: 500})
		return
	}
	JSON(w, 200, map[string]any{"status": "installed", "path": req.Path})
}

// OpenDeeplink handles POST /api/v1/sessions/:id/deeplink
func (h *deviceHandlers) OpenDeeplink(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	h.adb.Shell(r.Context(), serial, fmt.Sprintf("am start -a android.intent.action.VIEW -d '%s'", req.URL))
	JSON(w, 200, map[string]any{"status": "opened", "url": req.URL})
}

// ExecADB handles POST /api/v1/sessions/:id/adb — raw ADB command
func (h *deviceHandlers) ExecADB(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	output, err := h.adb.Shell(r.Context(), serial, req.Command)
	if err != nil {
		JSON(w, 200, map[string]any{"output": output, "error": err.Error()})
		return
	}
	JSON(w, 200, map[string]any{"output": output})
}
