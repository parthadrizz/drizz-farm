package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
)

type discoveryHandlers struct {
	sdk    *android.SDK
	runner android.CommandRunner
}

type systemImageResponse struct {
	Path    string `json:"path"`
	APIName string `json:"api_name"`
	Variant string `json:"variant"`
	Arch    string `json:"arch"`
}

type avdResponse struct {
	Name string `json:"name"`
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
		resp = append(resp, avdResponse{Name: avd.Name})
	}
	JSON(w, http.StatusOK, map[string]any{"avds": resp})
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
	}

	JSON(w, http.StatusOK, map[string]any{
		"created": created,
		"errors":  errors,
	})
}
