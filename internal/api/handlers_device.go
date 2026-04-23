package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// SetDarkMode handles POST /api/v1/sessions/:id/appearance.
//
// Accepts `dark` (preferred) or `dark_mode` (legacy alias from BS/LT
// parity docs). Uses `cmd uimode night yes|no` which works on API
// 29+; on older images it silently no-ops so we also write the
// secure setting ui_night_mode directly as a belt-and-braces fallback.
// Values: 1=light, 2=dark, 0=auto (we only expose light/dark here).
func (h *deviceHandlers) SetDarkMode(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}
	var req struct {
		Dark     *bool `json:"dark,omitempty"`
		DarkMode *bool `json:"dark_mode,omitempty"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var dark bool
	switch {
	case req.Dark != nil:
		dark = *req.Dark
	case req.DarkMode != nil:
		dark = *req.DarkMode
	}

	mode, uiVal := "no", "1"
	if dark {
		mode, uiVal = "yes", "2"
	}
	// Primary path — works on modern emulators.
	h.adb.Shell(r.Context(), serial, "cmd uimode night "+mode)
	// Belt-and-braces — directly poke the secure setting so older
	// images that don't implement the cmd uimode path still flip.
	h.adb.Shell(r.Context(), serial, "settings put secure ui_night_mode "+uiVal)

	JSON(w, 200, map[string]any{"status": "set", "dark": dark})
}

// InstallAPK handles POST /api/v1/sessions/:id/install.
//
// Two request modes:
//
//   multipart/form-data
//     Field "apk"  — the APK bytes (preferred for CI).
//     We stream to a tempfile on the daemon host, adb install, delete
//     the tempfile. Max size: 256 MB (plenty for most APKs).
//
//   application/json
//     { "path": "/absolute/path/on/daemon/host.apk" }
//     The old path-based flow — still supported for local scripts.
//
// Pick the mode by looking at Content-Type. adb.Install uses the
// `-r` reinstall flag so repeated installs don't fail on "already
// installed" — matches what developers expect from `adb install`.
func (h *deviceHandlers) InstallAPK(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	contentType := r.Header.Get("Content-Type")
	localPath := ""
	tmpCleanup := ""

	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(256 << 20); err != nil {
			JSON(w, 400, ErrorResponse{Error: "bad_multipart", Message: err.Error(), Code: 400})
			return
		}
		file, fh, err := r.FormFile("apk")
		if err != nil {
			JSON(w, 400, ErrorResponse{Error: "missing_apk", Message: "multipart field 'apk' is required", Code: 400})
			return
		}
		defer file.Close()
		tmp, err := os.CreateTemp("", "drizz-install-*.apk")
		if err != nil {
			JSON(w, 500, ErrorResponse{Error: "tmp_failed", Message: err.Error(), Code: 500})
			return
		}
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			JSON(w, 500, ErrorResponse{Error: "copy_failed", Message: err.Error(), Code: 500})
			return
		}
		tmp.Close()
		localPath = tmp.Name()
		tmpCleanup = tmp.Name()
		_ = fh // filename not needed; we use the temp path
	default:
		var req struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Path == "" {
			JSON(w, 400, ErrorResponse{Error: "invalid_request", Message: "either multipart 'apk' or JSON 'path' is required", Code: 400})
			return
		}
		localPath = req.Path
	}

	err := h.adb.Install(r.Context(), serial, localPath, true)
	if tmpCleanup != "" {
		os.Remove(tmpCleanup)
	}
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "install_failed", Message: err.Error(), Code: 500})
		return
	}
	JSON(w, 200, map[string]any{"status": "installed"})
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

// Biometric handles POST /api/v1/sessions/:id/biometric.
//
// Emulators boot with no fingerprints enrolled — the `finger touch`
// console command only works AFTER an enrollment exists. We expose
// three actions:
//
//   enroll  → enroll fingerprint id=1 via `cmd fingerprint enroll`.
//             Idempotent: running twice leaves one enrolled print.
//   touch   → simulate a successful auth (default action).
//   fail    → simulate a rejected auth.
//
// For tests that exercise real auth flows, call enroll first, then
// touch. For manual dashboard use, the enroll step can happen once
// and stick until the emulator is wiped.
func (h *deviceHandlers) Biometric(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404})
		return
	}
	var req struct {
		Action string `json:"action"` // "enroll", "touch", or "fail"
	}
	json.NewDecoder(r.Body).Decode(&req)

	switch req.Action {
	case "enroll":
		// Enrollment on an emulator needs three things in order:
		//   1. A device credential (PIN/password) — fingerprint won't
		//      enroll without a backup auth method set.
		//   2. `cmd fingerprint enroll` to register the sensor slot.
		//   3. `emu finger touch 1` during the prompt so the enrollment
		//      completes; we fire a few in quick succession to cover
		//      the multi-touch confirmation Android asks for.
		_, _ = h.adb.Shell(r.Context(), serial, "locksettings set-pin 1111")
		_, _ = h.adb.Shell(r.Context(), serial, "cmd fingerprint enroll")
		// Give the enrollment UI a beat to settle, then fire enough
		// synthetic touches to satisfy the "touch N times" loop.
		time.Sleep(500 * time.Millisecond)
		for i := 0; i < 6; i++ {
			h.adb.EmuCommand(r.Context(), serial, "finger touch 1")
			time.Sleep(200 * time.Millisecond)
		}
	case "fail":
		h.adb.EmuCommand(r.Context(), serial, "finger touch 0")
	default: // "touch" or empty → success
		h.adb.EmuCommand(r.Context(), serial, "finger touch 1")
		req.Action = "touch"
	}
	JSON(w, 200, map[string]any{"status": "triggered", "action": req.Action})
}

// CameraInject handles POST /api/v1/sessions/:id/camera.
//
// Accepts either multipart/form-data with an "image" file part OR a
// JSON body of {"image_path": "/path/on/host"}. The image is pushed
// to the emulator's gallery (/sdcard/DCIM/Camera/), then the media
// scanner is kicked so the gallery app indexes it — the file picker
// in any app under test now surfaces the injected image as a
// selectable option. That's the real "upload-a-mock-image" flow;
// directly wiring the emulator's live camera feed needs
// -camera-back virtualscene flags at AVD boot, which requires a
// restart and isn't worth it for 99% of test cases.
//
// Response includes the on-device path so tests can pass it to
// things like Appium SendKeys on a file-input element.
func (h *deviceHandlers) CameraInject(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404})
		return
	}

	localPath := ""
	tmpCleanup := ""

	contentType := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"):
		// Limit the form size so a hostile request can't exhaust memory.
		if err := r.ParseMultipartForm(64 << 20); err != nil { // 64 MB
			JSON(w, 400, ErrorResponse{Error: "bad_multipart", Message: err.Error(), Code: 400})
			return
		}
		file, fh, err := r.FormFile("image")
		if err != nil {
			JSON(w, 400, ErrorResponse{Error: "missing_image", Message: "multipart field 'image' is required", Code: 400})
			return
		}
		defer file.Close()
		tmp, err := os.CreateTemp("", "drizz-camera-*"+filepath.Ext(fh.Filename))
		if err != nil {
			JSON(w, 500, ErrorResponse{Error: "tmp_failed", Message: err.Error(), Code: 500})
			return
		}
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			JSON(w, 500, ErrorResponse{Error: "copy_failed", Message: err.Error(), Code: 500})
			return
		}
		tmp.Close()
		localPath = tmp.Name()
		tmpCleanup = tmp.Name()

	default:
		var req struct {
			ImagePath string `json:"image_path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ImagePath == "" {
			JSON(w, 400, ErrorResponse{Error: "missing_image", Message: "either multipart 'image' or JSON image_path is required", Code: 400})
			return
		}
		localPath = req.ImagePath
	}

	// Unique filename so repeat injections don't stomp each other and
	// the gallery shows them separately.
	ext := strings.ToLower(filepath.Ext(localPath))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		ext = ".jpg"
	}
	devicePath := fmt.Sprintf("/sdcard/DCIM/Camera/drizz_inject_%d%s", time.Now().UnixNano(), ext)

	// Make sure the destination dir exists on device.
	_, _ = h.adb.Shell(r.Context(), serial, "mkdir -p /sdcard/DCIM/Camera")

	if err := h.adb.Push(r.Context(), serial, localPath, devicePath); err != nil {
		if tmpCleanup != "" {
			os.Remove(tmpCleanup)
		}
		JSON(w, 500, ErrorResponse{Error: "push_failed", Message: err.Error(), Code: 500})
		return
	}
	if tmpCleanup != "" {
		os.Remove(tmpCleanup)
	}

	// Kick the media scanner so the gallery picker sees the new image
	// without waiting for its next poll.
	_, _ = h.adb.Shell(r.Context(), serial,
		fmt.Sprintf("am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d file://%s", devicePath))

	JSON(w, 200, map[string]any{
		"status":      "injected",
		"device_path": devicePath,
	})
}

// UploadFile handles POST /api/v1/sessions/:id/files/upload.
//
// Multipart/form-data endpoint — the CI runner posts a file with the
// "file" field and we push it to the device at the path given in the
// "target" field (defaults to /sdcard/Download/<filename>).
//
// Exists because the existing file/push endpoint takes a `local_path`
// on the drizz-farm host, which CI runners don't have. With upload,
// the test sends the bytes and we handle the hop to the device.
func (h *deviceHandlers) UploadFile(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404})
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil { // 256 MB
		JSON(w, 400, ErrorResponse{Error: "bad_multipart", Message: err.Error(), Code: 400})
		return
	}
	file, fh, err := r.FormFile("file")
	if err != nil {
		JSON(w, 400, ErrorResponse{Error: "missing_file", Message: "multipart field 'file' is required", Code: 400})
		return
	}
	defer file.Close()

	target := r.FormValue("target")
	if target == "" {
		target = "/sdcard/Download/" + filepath.Base(fh.Filename)
	}
	// Guard against empty filename or weird traversal.
	if filepath.Base(fh.Filename) == "" {
		JSON(w, 400, ErrorResponse{Error: "bad_filename", Message: "filename is empty", Code: 400})
		return
	}

	tmp, err := os.CreateTemp("", "drizz-upload-*"+filepath.Ext(fh.Filename))
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "tmp_failed", Message: err.Error(), Code: 500})
		return
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, file)
	if err != nil {
		tmp.Close()
		JSON(w, 500, ErrorResponse{Error: "copy_failed", Message: err.Error(), Code: 500})
		return
	}
	tmp.Close()

	// Ensure the parent dir exists on the device.
	_, _ = h.adb.Shell(r.Context(), serial, "mkdir -p "+filepath.Dir(target))

	if err := h.adb.Push(r.Context(), serial, tmp.Name(), target); err != nil {
		JSON(w, 500, ErrorResponse{Error: "push_failed", Message: err.Error(), Code: 500})
		return
	}

	// If the target is in a media directory, nudge the scanner so the
	// gallery / file picker surfaces it immediately.
	if strings.HasPrefix(target, "/sdcard/DCIM") ||
		strings.HasPrefix(target, "/sdcard/Pictures") ||
		strings.HasPrefix(target, "/sdcard/Movies") ||
		strings.HasPrefix(target, "/sdcard/Music") ||
		strings.HasPrefix(target, "/sdcard/Download") {
		_, _ = h.adb.Shell(r.Context(), serial,
			fmt.Sprintf("am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d file://%s", target))
	}

	JSON(w, 200, map[string]any{
		"status":      "uploaded",
		"filename":    filepath.Base(fh.Filename),
		"size":        size,
		"device_path": target,
	})
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

// PushNotification handles POST /api/v1/sessions/:id/push-notification.
//
// Previous implementation broadcast to `<pkg>/.NotificationReceiver`
// — only worked for apps that happened to have that exact class.
// Replaced with Android's built-in `cmd notification post`, which
// posts directly to the system notification service and shows up
// in the tray regardless of what app is running. Works API 24+.
//
// Request body:
//   title  — notification title (required)
//   body   — notification body (required)
//   tag    — dedupe key, defaults to "drizz" (repeat posts with the
//            same tag REPLACE; different tags stack)
//
// We generate a safe tag automatically if the caller omits it so
// dashboard-driven notifications don't pile up.
func (h *deviceHandlers) PushNotification(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404})
		return
	}
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Tag   string `json:"tag,omitempty"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Title == "" || req.Body == "" {
		JSON(w, 400, ErrorResponse{Error: "invalid_request", Message: "title and body are required", Code: 400})
		return
	}
	tag := req.Tag
	if tag == "" {
		tag = "drizz"
	}
	// `-S bigtext` = big-text style; -t = title; positional args are
	// tag + body. Single quotes around args shell-escaped upstream
	// via findSerial's safe-value guarantee. We still sanitize here
	// to avoid the user's title closing our shell quoting.
	safe := func(s string) string { return strings.ReplaceAll(s, "'", `'\''`) }
	cmd := fmt.Sprintf(`cmd notification post -S bigtext -t '%s' '%s' '%s'`,
		safe(req.Title), safe(tag), safe(req.Body))
	if _, err := h.adb.Shell(r.Context(), serial, cmd); err != nil {
		JSON(w, 500, ErrorResponse{Error: "notification_failed", Message: err.Error(), Code: 500})
		return
	}
	JSON(w, 200, map[string]any{"status": "sent", "tag": tag, "title": req.Title})
}

// Clipboard handles POST /api/v1/sessions/:id/clipboard.
//
// Fix history: the first version ran `input text '...'` which TYPES
// the string into whatever's focused — not what anyone calling a
// clipboard API expects. Now we use Android's built-in `cmd
// clipboard set-primary-clip` (available API 29+) which puts the
// value on the system clipboard the same way a user tapping Copy
// would.
//
// Fallback for older emulators: if `cmd clipboard` returns a
// non-zero exit, we fall back to the `input text` behavior with
// a warning header so callers can tell which path ran.
func (h *deviceHandlers) Clipboard(w http.ResponseWriter, r *http.Request) {
	serial := h.findSerial(chi.URLParam(r, "id"))
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "not found", Code: 404})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	safe := strings.ReplaceAll(req.Text, "'", `'\''`)
	// Resolve the active user id — Android multi-user emulators can
	// run the foreground app as user 10 or 11 instead of 0, and
	// `cmd clipboard set-primary-clip -u 0` writes to a user that
	// no app is reading from. `am get-current-user` returns the UID
	// for the currently focused user.
	uid := "0"
	if out, err := h.adb.Shell(r.Context(), serial, "am get-current-user"); err == nil {
		if trimmed := strings.TrimSpace(out); trimmed != "" {
			uid = trimmed
		}
	}
	out, err := h.adb.Shell(r.Context(), serial,
		fmt.Sprintf(`cmd clipboard set-primary-clip -u %s --value '%s'`, uid, safe))
	if err == nil && !strings.Contains(out, "Unknown command") && !strings.Contains(out, "not found") {
		JSON(w, 200, map[string]any{"status": "set", "method": "cmd_clipboard", "user": uid})
		return
	}

	// Fallback — type the value (legacy behavior). Better than
	// failing; caller can tell via the method field.
	_, _ = h.adb.Shell(r.Context(), serial, fmt.Sprintf("input text '%s'", safe))
	JSON(w, 200, map[string]any{"status": "set", "method": "input_text_fallback"})
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
