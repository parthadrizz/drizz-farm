package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/store"
)

type discoveryHandlers struct {
	sdk    *android.SDK
	runner android.CommandRunner
	store  *store.Store
	cfg    *config.Config
}

type systemImageResponse struct {
	Path    string `json:"path"`
	APIName string `json:"api_name"`
	Variant string `json:"variant"`
	Arch    string `json:"arch"`
}

type avdResponse struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	DeviceModel  string `json:"device_model"`
	AndroidVer   string `json:"android_ver"`
	APILevel     string `json:"api_level"`
	Variant      string `json:"variant"`
}

// SystemImages handles GET /api/v1/discovery/system-images
func (h *discoveryHandlers) SystemImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.sdk.ListInstalledSystemImages(r.Context(), h.runner)
	if err != nil {
		log.Warn().Err(err).Msg("discovery: failed to list system images")
		JSON(w, http.StatusOK, map[string]any{"images": []any{}, "error": err.Error()})
		return
	}

	resp := make([]systemImageResponse, 0, len(images))
	for _, img := range images {
		resp = append(resp, systemImageResponse{
			Path:    img.Path,
			APIName: img.APIName,
			Variant: img.Variant,
			Arch:    img.Arch,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"images": resp})
}

// Devices handles GET /api/v1/discovery/devices
func (h *discoveryHandlers) Devices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.sdk.ListInstalledDevices(r.Context(), h.runner)
	if err != nil {
		log.Warn().Err(err).Msg("discovery: failed to list devices")
		JSON(w, http.StatusOK, map[string]any{"devices": []any{}, "error": err.Error()})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// AVDs handles GET /api/v1/discovery/avds
func (h *discoveryHandlers) AVDs(w http.ResponseWriter, r *http.Request) {
	avdMgr := android.NewAVDManager(h.sdk, h.runner)
	avds, err := avdMgr.List(r.Context())
	if err != nil {
		// Return empty list instead of 500 — avdmanager might not work (no Java, etc.)
		log.Warn().Err(err).Msg("discovery: failed to list AVDs")
		JSON(w, http.StatusOK, map[string]any{"avds": []any{}, "error": err.Error()})
		return
	}

	resp := make([]avdResponse, 0, len(avds))
	for _, avd := range avds {
		resp = append(resp, avdResponse{
			Name: avd.Name, DisplayName: avd.DisplayName,
			DeviceModel: avd.DeviceModel, AndroidVer: avd.AndroidVer,
			APILevel: avd.APILevel, Variant: avd.Variant,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"avds": resp})
}

// AvailableImages handles GET /api/v1/discovery/available-images
func (h *discoveryHandlers) AvailableImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.sdk.ListAvailableSystemImages(r.Context(), h.runner)
	if err != nil {
		log.Warn().Err(err).Msg("discovery: failed to list available images")
		JSON(w, http.StatusOK, map[string]any{"images": []any{}, "error": err.Error()})
		return
	}

	resp := make([]systemImageResponse, 0, len(images))
	for _, img := range images {
		resp = append(resp, systemImageResponse{
			Path:    img.Path,
			APIName: img.APIName,
			Variant: img.Variant,
			Arch:    img.Arch,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"images": resp})
}

// InstallImageStream handles POST /api/v1/discovery/install-image-stream.
// Streams sdkmanager output line-by-line as plain text/chunked so the
// dashboard can show real-time progress for the (often multi-minute)
// system image download. Each line flushed immediately.
//
// Last line is always one of:
//   __STATUS__:ok
//   __STATUS__:error: <message>
// so the client can detect completion without parsing sdkmanager output.
func (h *discoveryHandlers) InstallImageStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "path is required", Code: 400,
		})
		return
	}
	sdkManager := h.sdk.SDKManagerPath()
	if sdkManager == "" {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "install_failed", Message: "sdkmanager not found", Code: 500,
		})
		return
	}

	// Tell intermediaries (CF, nginx) not to buffer.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "no_flush", Message: "streaming not supported by HTTP responder", Code: 500,
		})
		return
	}
	w.WriteHeader(http.StatusOK)

	cmd := exec.CommandContext(r.Context(), "sh", "-c",
		fmt.Sprintf("yes | %s --sdk_root=%s --install '%s'", sdkManager, h.sdk.Root, req.Path))
	if javaPath := h.cfg.SDK.Java; javaPath != "" {
		javaHome := filepath.Dir(filepath.Dir(javaPath))
		cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(w, "__STATUS__:error: pipe stdout: %v\n", err)
		flusher.Flush()
		return
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same stream
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "__STATUS__:error: start sdkmanager: %v\n", err)
		flusher.Flush()
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(w, "\n__STATUS__:error: %v\n", err)
	} else {
		fmt.Fprintln(w, "\n__STATUS__:ok")
	}
	flusher.Flush()
}

// InstallImage handles POST /api/v1/discovery/install-image
func (h *discoveryHandlers) InstallImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "path is required", Code: 400,
		})
		return
	}

	// Run sdkmanager --install with JAVA_HOME and --sdk_root
	// Accept licenses automatically with "yes" pipe
	sdkManager := h.sdk.SDKManagerPath()
	if sdkManager == "" {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "install_failed", Message: "sdkmanager not found", Code: 500,
		})
		return
	}
	cmd := exec.CommandContext(r.Context(), "sh", "-c",
		fmt.Sprintf("yes | %s --sdk_root=%s --install '%s'", sdkManager, h.sdk.Root, req.Path))
	// Inject JAVA_HOME from config
	if javaPath := h.cfg.SDK.Java; javaPath != "" {
		javaHome := filepath.Dir(filepath.Dir(javaPath))
		cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "install_failed", Message: fmt.Sprintf("Failed to install %s: %v\n%s", req.Path, err, string(out)), Code: 500,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"status":  "installed",
		"path":    req.Path,
	})
}

type createAVDsRequest struct {
	ProfileName string `json:"profile_name"`
	Device      string `json:"device"`
	SystemImage string `json:"system_image"`
	Count       int    `json:"count"`
	// Optional resource overrides — sane defaults applied when zero.
	RAMMB      int    `json:"ram_mb,omitempty"`
	HeapMB     int    `json:"heap_mb,omitempty"`
	DiskSizeMB int    `json:"disk_size_mb,omitempty"`
	GPU        string `json:"gpu,omitempty"`
}

// CreateAVDs handles POST /api/v1/discovery/create-avds
func (h *discoveryHandlers) CreateAVDs(w http.ResponseWriter, r *http.Request) {
	var req createAVDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "Invalid JSON: " + err.Error(), Code: 400,
		})
		return
	}

	if req.ProfileName == "" || req.Device == "" || req.SystemImage == "" || req.Count < 1 {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "profile_name, device, system_image, and count >= 1 required", Code: 400,
		})
		return
	}

	// Apply defaults, then layer per-request overrides.
	profile := config.AndroidProfile{
		Device:      req.Device,
		SystemImage: req.SystemImage,
		RAMMB:       2048,
	}
	if req.RAMMB > 0 {
		profile.RAMMB = req.RAMMB
	}
	if req.HeapMB > 0 {
		profile.HeapMB = req.HeapMB
	}
	if req.DiskSizeMB > 0 {
		profile.DiskSizeMB = req.DiskSizeMB
	}
	if req.GPU != "" {
		profile.GPU = req.GPU
	}

	avdMgr := android.NewAVDManager(h.sdk, h.runner)
	// Initialize slices (not nil) so the JSON always has `names: []`
	// and `errors: []` instead of null — clients that do `.length`
	// on the fields don't have to null-check.
	names := []string{}
	errors := []string{}

	for i := 0; i < req.Count; i++ {
		name := android.AVDName(req.ProfileName, i)
		if err := avdMgr.Create(r.Context(), name, profile); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		names = append(names, name)
		if h.store != nil {
			h.store.RecordAVDCreation(name, req.ProfileName, req.Device, req.SystemImage)
		}
	}

	JSON(w, http.StatusOK, map[string]any{
		"created": len(names),
		"names":   names,
		"errors":  errors,
	})
}
