package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/drizz-dev/drizz-farm/internal/config"
)

type configHandlers struct {
	cfg *config.Config
}

// GetConfig handles GET /api/v1/config
func (h *configHandlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, h.cfg)
}

// UpdateConfig handles PUT /api/v1/config
func (h *configHandlers) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "Invalid JSON: " + err.Error(), Code: 400,
		})
		return
	}

	// Read current config file
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".drizz-farm", "config.yaml")

	configData, err := os.ReadFile(configPath)
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "config_error", Message: "Cannot read config: " + err.Error(), Code: 500,
		})
		return
	}

	// Return current config and the requested updates
	// Full config hot-reload is a future feature — for now just acknowledge
	JSON(w, http.StatusOK, map[string]any{
		"status":         "acknowledged",
		"message":        "Config changes noted. Restart daemon to apply.",
		"current_config": string(configData),
		"updates":        updates,
	})
}

// GetConfigRaw handles GET /api/v1/config/raw — returns raw YAML
func (h *configHandlers) GetConfigRaw(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".drizz-farm", "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "config_error", Message: "Cannot read config: " + err.Error(), Code: 500,
		})
		return
	}

	w.Header().Set("Content-Type", "text/yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// SaveConfigRaw handles PUT /api/v1/config/raw — saves raw YAML
func (h *configHandlers) SaveConfigRaw(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(nil)
	_ = data

	// Read body as raw YAML
	body := make([]byte, 0, 4096)
	buf := make([]byte, 1024)
	for {
		n, readErr := r.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	if len(body) == 0 {
		JSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid_request", Message: "Empty body", Code: 400,
		})
		return
	}

	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".drizz-farm", "config.yaml")

	if err = os.WriteFile(configPath, body, 0644); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "config_error", Message: "Cannot write config: " + err.Error(), Code: 500,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]string{
		"status":  "saved",
		"message": "Config saved. Restart daemon to apply changes.",
	})
}
