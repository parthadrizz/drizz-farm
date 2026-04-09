package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadWithDefaults(t *testing.T) {
	// Reset viper for clean test
	viper.Reset()

	// Set minimal required config
	viper.Set("pool.profiles.android", map[string]any{
		"pixel_7_api34": map[string]any{
			"device":       "pixel_7",
			"system_image": "system-images;android-34;google_apis;arm64-v8a",
			"ram_mb":       2048,
		},
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Check defaults were applied
	if cfg.API.Port != 9401 {
		t.Errorf("expected API port 9401, got %d", cfg.API.Port)
	}
	if cfg.API.GRPCPort != 9400 {
		t.Errorf("expected gRPC port 9400, got %d", cfg.API.GRPCPort)
	}
	if cfg.Pool.SessionTimeoutMinutes != 60 {
		t.Errorf("expected session timeout 60, got %d", cfg.Pool.SessionTimeoutMinutes)
	}
	if cfg.Pool.PortRangeMin != 5554 {
		t.Errorf("expected port range min 5554, got %d", cfg.Pool.PortRangeMin)
	}
	if cfg.HealthCheck.IntervalSeconds != 15 {
		t.Errorf("expected health check interval 15, got %d", cfg.HealthCheck.IntervalSeconds)
	}
	if cfg.Node.Name == "" {
		t.Error("expected node name to be set from hostname")
	}
}

func TestValidateRequiresProfiles(t *testing.T) {
	viper.Reset()
	// No profiles set
	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for missing profiles")
	}
}

func TestValidateRequiresSystemImage(t *testing.T) {
	viper.Reset()
	viper.Set("pool.profiles.android", map[string]any{
		"bad_profile": map[string]any{
			"device": "pixel_7",
			// missing system_image
		},
	})

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for missing system_image")
	}
}

func TestDataDir(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".drizz-farm")
	if cfg.DataDir() != expected {
		t.Errorf("expected data dir %s, got %s", expected, cfg.DataDir())
	}

	cfg.Node.DataDir = "/custom/path"
	if cfg.DataDir() != "/custom/path" {
		t.Errorf("expected custom data dir /custom/path, got %s", cfg.DataDir())
	}
}
