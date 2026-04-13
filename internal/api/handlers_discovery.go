package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/store"
)

type discoveryHandlers struct {
	sdk    *android.SDK
	runner android.CommandRunner
	store  *store.Store
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
		Error(w, fmt.Errorf("list system images: %w", err))
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
		Error(w, fmt.Errorf("list devices: %w", err))
		return
	}
	JSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// AVDs handles GET /api/v1/discovery/avds
func (h *discoveryHandlers) AVDs(w http.ResponseWriter, r *http.Request) {
	avdMgr := android.NewAVDManager(h.sdk, h.runner)
	avds, err := avdMgr.List(r.Context())
	if err != nil {
		Error(w, fmt.Errorf("list AVDs: %w", err))
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
		Error(w, fmt.Errorf("list available images: %w", err))
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

	// Run sdkmanager --install in background
	// For now, run synchronously (can be slow ~1-5 min)
	_, err := h.runner.Run(r.Context(), h.sdk.SDKManagerPath(), "--install", req.Path)
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "install_failed", Message: fmt.Sprintf("Failed to install %s: %v", req.Path, err), Code: 500,
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

	profile := config.AndroidProfile{
		Device:      req.Device,
		SystemImage: req.SystemImage,
		RAMMB:       2048,
	}

	avdMgr := android.NewAVDManager(h.sdk, h.runner)
	created := 0
	var errors []string

	for i := 0; i < req.Count; i++ {
		name := android.AVDName(req.ProfileName, i)
		if err := avdMgr.Create(r.Context(), name, profile); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		created++
		if h.store != nil {
			h.store.RecordAVDCreation(name, req.ProfileName, req.Device, req.SystemImage)
		}
	}

	JSON(w, http.StatusOK, map[string]any{
		"created": created,
		"errors":  errors,
	})
}
