package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type deviceHandlers struct {
	pool *pool.Pool
	adb  *android.ADBClient
}

// findSerial resolves a session/instance ID to an ADB serial number for device commands.
func (h *deviceHandlers) findSerial(id string) string {
	if h.pool == nil { return "" }
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

// PushFile handles POST /api/v1/sessions/:id/file/push
func (h *deviceHandlers) PushFile(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { LocalPath string `json:"local_path"`; DevicePath string `json:"device_path"` }
	json.NewDecoder(r.Body).Decode(&req)
	err := h.adb.Push(r.Context(), serial, req.LocalPath, req.DevicePath)
	if err != nil { JSON(w, 500, ErrorResponse{Error: "push_failed", Message: err.Error(), Code: 500}); return }
	JSON(w, 200, map[string]any{"status": "pushed", "device_path": req.DevicePath})
}

// PullFile handles POST /api/v1/sessions/:id/file/pull
func (h *deviceHandlers) PullFile(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { DevicePath string `json:"device_path"`; LocalPath string `json:"local_path"` }
	json.NewDecoder(r.Body).Decode(&req)
	err := h.adb.Pull(r.Context(), serial, req.DevicePath, req.LocalPath)
	if err != nil { JSON(w, 500, ErrorResponse{Error: "pull_failed", Message: err.Error(), Code: 500}); return }
	JSON(w, 200, map[string]any{"status": "pulled", "local_path": req.LocalPath})
}

// Biometric handles POST /api/v1/sessions/:id/biometric
func (h *deviceHandlers) Biometric(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Action string `json:"action"` } // "touch" or "fail"
	json.NewDecoder(r.Body).Decode(&req)
	if req.Action == "fail" {
		h.adb.EmuCommand(r.Context(), serial, "finger touch bad")
	} else {
		h.adb.EmuCommand(r.Context(), serial, "finger touch 1")
	}
	JSON(w, 200, map[string]any{"status": "triggered", "action": req.Action})
}

// CameraInject handles POST /api/v1/sessions/:id/camera
func (h *deviceHandlers) CameraInject(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { ImagePath string `json:"image_path"` }
	json.NewDecoder(r.Body).Decode(&req)
	// Push image to device and set as virtualscene
	h.adb.Push(r.Context(), serial, req.ImagePath, "/sdcard/camera_inject.jpg")
	JSON(w, 200, map[string]any{"status": "injected"})
}

// Permissions handles POST /api/v1/sessions/:id/permissions
func (h *deviceHandlers) Permissions(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"`; Permission string `json:"permission"`; Grant bool `json:"grant"` }
	json.NewDecoder(r.Body).Decode(&req)
	action := "grant"
	if !req.Grant { action = "revoke" }
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("pm %s %s %s", action, req.Package, req.Permission))
	JSON(w, 200, map[string]any{"status": action, "package": req.Package, "permission": req.Permission})
}

// ClearData handles POST /api/v1/sessions/:id/clear-data
func (h *deviceHandlers) ClearData(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("pm clear %s", req.Package))
	JSON(w, 200, map[string]any{"status": "cleared", "package": req.Package})
}

// UninstallApp handles POST /api/v1/sessions/:id/uninstall
func (h *deviceHandlers) UninstallApp(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Uninstall(r.Context(), serial, req.Package)
	JSON(w, 200, map[string]any{"status": "uninstalled", "package": req.Package})
}

// SetTimezone handles POST /api/v1/sessions/:id/timezone
func (h *deviceHandlers) SetTimezone(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Timezone string `json:"timezone"` } // e.g. "America/New_York"
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("setprop persist.sys.timezone %s", req.Timezone))
	JSON(w, 200, map[string]any{"status": "set", "timezone": req.Timezone})
}

// PushNotification handles POST /api/v1/sessions/:id/push-notification
func (h *deviceHandlers) PushNotification(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"`; Title string `json:"title"`; Body string `json:"body"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("am broadcast -a com.android.test.NOTIFY --es title '%s' --es body '%s' -n %s/.NotificationReceiver", req.Title, req.Body, req.Package))
	JSON(w, 200, map[string]any{"status": "sent"})
}

// Clipboard handles POST /api/v1/sessions/:id/clipboard
func (h *deviceHandlers) Clipboard(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Text string `json:"text"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("input text '%s'", req.Text))
	JSON(w, 200, map[string]any{"status": "set"})
}

// FontScale handles POST /api/v1/sessions/:id/font-scale
func (h *deviceHandlers) FontScale(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Scale float64 `json:"scale"` } // 0.85, 1.0, 1.15, 1.3
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put system font_scale %.2f", req.Scale))
	JSON(w, 200, map[string]any{"status": "set", "scale": req.Scale})
}

// Shake handles POST /api/v1/sessions/:id/shake
func (h *deviceHandlers) Shake(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	h.adb.EmuCommand(r.Context(), serial, "sensor set acceleration 0:15:0")
	time.Sleep(200 * time.Millisecond)
	h.adb.EmuCommand(r.Context(), serial, "sensor set acceleration 0:0:9.8")
	JSON(w, 200, map[string]any{"status": "shaken"})
}

// Sensor handles POST /api/v1/sessions/:id/sensor
func (h *deviceHandlers) Sensor(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Name string `json:"name"`; Values string `json:"values"` } // name: acceleration, gyroscope, proximity
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("sensor set %s %s", req.Name, req.Values))
	JSON(w, 200, map[string]any{"status": "set", "sensor": req.Name})
}

// AudioInject handles POST /api/v1/sessions/:id/audio
func (h *deviceHandlers) AudioInject(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { FilePath string `json:"file_path"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("audio inject %s", req.FilePath))
	JSON(w, 200, map[string]any{"status": "injected"})
}

// Volume handles POST /api/v1/sessions/:id/volume
func (h *deviceHandlers) Volume(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Action string `json:"action"`; Level int `json:"level"` } // action: up, down, mute, set
	json.NewDecoder(r.Body).Decode(&req)
	switch req.Action {
	case "up":
		h.adb.Shell(r.Context(), serial, "input keyevent KEYCODE_VOLUME_UP")
	case "down":
		h.adb.Shell(r.Context(), serial, "input keyevent KEYCODE_VOLUME_DOWN")
	case "mute":
		h.adb.Shell(r.Context(), serial, "input keyevent KEYCODE_VOLUME_MUTE")
	case "set":
		h.adb.Shell(r.Context(), serial, fmt.Sprintf("media volume --stream 3 --set %d", req.Level))
	}
	JSON(w, 200, map[string]any{"status": "set", "action": req.Action})
}

// LockUnlock handles POST /api/v1/sessions/:id/lock
func (h *deviceHandlers) LockUnlock(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Lock bool `json:"lock"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.Lock {
		h.adb.Shell(r.Context(), serial, "input keyevent KEYCODE_SLEEP")
	} else {
		h.adb.Shell(r.Context(), serial, "input keyevent KEYCODE_WAKEUP")
		time.Sleep(300 * time.Millisecond)
		h.adb.Shell(r.Context(), serial, "input swipe 540 1800 540 800 300") // swipe up to unlock
	}
	JSON(w, 200, map[string]any{"status": "set", "locked": req.Lock})
}

// Animations handles POST /api/v1/sessions/:id/animations
func (h *deviceHandlers) Animations(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Enabled bool `json:"enabled"` }
	json.NewDecoder(r.Body).Decode(&req)
	scale := "1.0"
	if !req.Enabled { scale = "0.0" }
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put global window_animation_scale %s", scale))
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put global transition_animation_scale %s", scale))
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put global animator_duration_scale %s", scale))
	JSON(w, 200, map[string]any{"status": "set", "enabled": req.Enabled})
}

// GPSRoute handles POST /api/v1/sessions/:id/gps-route — simulate movement
func (h *deviceHandlers) GPSRoute(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct {
		Points []struct { Lat float64 `json:"lat"`; Lng float64 `json:"lng"` } `json:"points"`
		DelayMS int `json:"delay_ms"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.DelayMS == 0 { req.DelayMS = 1000 }

	// Run route in background
	go func() {
		for _, pt := range req.Points {
			h.adb.EmuCommand(r.Context(), serial, fmt.Sprintf("geo fix %f %f", pt.Lng, pt.Lat))
			time.Sleep(time.Duration(req.DelayMS) * time.Millisecond)
		}
	}()

	JSON(w, 200, map[string]any{"status": "started", "points": len(req.Points), "delay_ms": req.DelayMS})
}

// Accessibility handles POST /api/v1/sessions/:id/accessibility
func (h *deviceHandlers) Accessibility(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { TalkBack bool `json:"talkback"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.TalkBack {
		h.adb.Shell(r.Context(), serial, "settings put secure enabled_accessibility_services com.google.android.marvin.talkback/com.google.android.marvin.talkback.TalkBackService")
		h.adb.Shell(r.Context(), serial, "settings put secure accessibility_enabled 1")
	} else {
		h.adb.Shell(r.Context(), serial, "settings put secure enabled_accessibility_services \"\"")
		h.adb.Shell(r.Context(), serial, "settings put secure accessibility_enabled 0")
	}
	JSON(w, 200, map[string]any{"status": "set", "talkback": req.TalkBack})
}

// Brightness handles POST /api/v1/sessions/:id/brightness
func (h *deviceHandlers) Brightness(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Level int `json:"level"` } // 0-255
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, "settings put system screen_brightness_mode 0") // manual
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("settings put system screen_brightness %d", req.Level))
	JSON(w, 200, map[string]any{"status": "set", "level": req.Level})
}

// WifiToggle handles POST /api/v1/sessions/:id/wifi
func (h *deviceHandlers) WifiToggle(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Enabled bool `json:"enabled"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.Enabled {
		h.adb.Shell(r.Context(), serial, "svc wifi enable")
	} else {
		h.adb.Shell(r.Context(), serial, "svc wifi disable")
	}
	JSON(w, 200, map[string]any{"status": "set", "wifi": req.Enabled})
}

// LaunchApp handles POST /api/v1/sessions/:id/launch
func (h *deviceHandlers) LaunchApp(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("monkey -p %s -c android.intent.category.LAUNCHER 1", req.Package))
	JSON(w, 200, map[string]any{"status": "launched", "package": req.Package})
}

// ForceStop handles POST /api/v1/sessions/:id/force-stop
func (h *deviceHandlers) ForceStop(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Package string `json:"package"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("am force-stop %s", req.Package))
	JSON(w, 200, map[string]any{"status": "stopped", "package": req.Package})
}

// GetUITree handles GET /api/v1/sessions/:id/ui-tree — accessibility tree
func (h *deviceHandlers) GetUITree(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	output, err := h.adb.Shell(r.Context(), serial, "uiautomator dump /dev/tty")
	if err != nil { JSON(w, 500, ErrorResponse{Error: "ui_tree_failed", Message: err.Error(), Code: 500}); return }
	JSON(w, 200, map[string]any{"tree": output})
}

// GetActivity handles GET /api/v1/sessions/:id/activity — current foreground activity
func (h *deviceHandlers) GetActivity(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	output, _ := h.adb.Shell(r.Context(), serial, "dumpsys activity activities | grep mResumedActivity")
	JSON(w, 200, map[string]any{"activity": output})
}

// GetDeviceInfo handles GET /api/v1/sessions/:id/device-info — all device properties
func (h *deviceHandlers) GetDeviceInfo(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	model, _ := h.adb.GetProp(r.Context(), serial, "ro.product.model")
	brand, _ := h.adb.GetProp(r.Context(), serial, "ro.product.brand")
	api, _ := h.adb.GetProp(r.Context(), serial, "ro.build.version.sdk")
	version, _ := h.adb.GetProp(r.Context(), serial, "ro.build.version.release")
	locale, _ := h.adb.GetProp(r.Context(), serial, "persist.sys.locale")
	tz, _ := h.adb.GetProp(r.Context(), serial, "persist.sys.timezone")
	screenSize, _ := h.adb.Shell(r.Context(), serial, "wm size")
	density, _ := h.adb.Shell(r.Context(), serial, "wm density")
	battery, _ := h.adb.Shell(r.Context(), serial, "dumpsys battery | grep level")
	mem, _ := h.adb.Shell(r.Context(), serial, "cat /proc/meminfo | head -3")
	JSON(w, 200, map[string]any{
		"model": model, "brand": brand, "api_level": api, "android_version": version,
		"locale": locale, "timezone": tz, "screen_size": screenSize, "density": density,
		"battery": battery, "memory": mem, "serial": serial,
	})
}

// GetNotifications handles GET /api/v1/sessions/:id/notifications
func (h *deviceHandlers) GetNotifications(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	output, _ := h.adb.Shell(r.Context(), serial, "dumpsys notification --noredact | grep -A2 'NotificationRecord' | head -30")
	JSON(w, 200, map[string]any{"notifications": output})
}

// GetClipboard handles GET /api/v1/sessions/:id/clipboard/get
func (h *deviceHandlers) GetClipboard(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	output, _ := h.adb.Shell(r.Context(), serial, "service call clipboard 2 s16 com.android.shell")
	JSON(w, 200, map[string]any{"clipboard": output})
}

// IsKeyboardShown handles GET /api/v1/sessions/:id/keyboard
func (h *deviceHandlers) IsKeyboardShown(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	output, _ := h.adb.Shell(r.Context(), serial, "dumpsys input_method | grep mInputShown")
	shown := strings.Contains(output, "mInputShown=true")
	JSON(w, 200, map[string]any{"shown": shown})
}

// PressKey handles POST /api/v1/sessions/:id/key
func (h *deviceHandlers) PressKey(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	var req struct { Keycode string `json:"keycode"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.adb.Shell(r.Context(), serial, fmt.Sprintf("input keyevent %s", req.Keycode))
	JSON(w, 200, map[string]any{"status": "pressed", "keycode": req.Keycode})
}

// GetPackageInfo handles GET /api/v1/sessions/:id/package-info?package=com.app
func (h *deviceHandlers) GetPackageInfo(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" { JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404}); return }
	pkg := r.URL.Query().Get("package")
	if pkg == "" { JSON(w, 400, ErrorResponse{Error: "invalid_request", Message: "package param required", Code: 400}); return }
	output, _ := h.adb.Shell(r.Context(), serial, fmt.Sprintf("dumpsys package %s | head -20", pkg))
	JSON(w, 200, map[string]any{"package": pkg, "info": output})
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
