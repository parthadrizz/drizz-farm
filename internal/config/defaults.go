package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
)

// generateMeshID creates a unique mesh identifier: 8 hex chars timestamp + 8 random.
func generateMeshID() string {
	ts := fmt.Sprintf("%08x", time.Now().Unix())
	b := make([]byte, 4)
	rand.Read(b)
	return ts + hex.EncodeToString(b)
}

// generateMeshKey creates a random auth key for mesh peer authentication.
func generateMeshKey() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func applyDefaults(cfg *Config) {
	// Node defaults
	if cfg.Node.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "drizz-farm-node"
		}
		cfg.Node.Name = hostname
	}
	if cfg.Node.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.Node.DataDir = filepath.Join(home, ".drizz-farm")
	}
	if cfg.Node.LogLevel == "" {
		cfg.Node.LogLevel = "info"
	}
	if cfg.Node.MetricsPort == 0 {
		cfg.Node.MetricsPort = 9402
	}

	// Mesh defaults
	if cfg.Mesh.ID == "" {
		cfg.Mesh.ID = generateMeshID()
	}
	if cfg.Mesh.Name == "" {
		cfg.Mesh.Name = cfg.Node.Name // defaults to hostname
	}
	if cfg.Mesh.Key == "" {
		cfg.Mesh.Key = generateMeshKey()
	}

	// API defaults
	if cfg.API.Host == "" {
		cfg.API.Host = "0.0.0.0"
	}
	if cfg.API.Port == 0 {
		cfg.API.Port = 9401
	}
	if cfg.API.GRPCPort == 0 {
		cfg.API.GRPCPort = 9400
	}

	// Pool defaults
	if cfg.Pool.MaxConcurrent == 0 {
		cfg.Pool.MaxConcurrent = recommendedMaxEmulators()
	}
	if cfg.Pool.SessionTimeoutMinutes == 0 {
		cfg.Pool.SessionTimeoutMinutes = 60
	}
	if cfg.Pool.SessionMaxMinutes == 0 {
		cfg.Pool.SessionMaxMinutes = 180
	}
	if cfg.Pool.QueueMaxSize == 0 {
		cfg.Pool.QueueMaxSize = 20
	}
	if cfg.Pool.QueueTimeoutSeconds == 0 {
		cfg.Pool.QueueTimeoutSeconds = 300
	}
	if cfg.Pool.PortRangeMin == 0 {
		cfg.Pool.PortRangeMin = 5554
	}
	if cfg.Pool.PortRangeMax == 0 {
		cfg.Pool.PortRangeMax = 5700
	}

	// Network defaults
	if len(cfg.Network.AllowedCIDRs) == 0 {
		cfg.Network.AllowedCIDRs = []string{"192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "127.0.0.0/8"}
	}
	if cfg.Network.MDNS.ServiceType == "" {
		cfg.Network.MDNS.ServiceType = "_drizz-farm._tcp"
	}

	// Health check defaults
	if cfg.HealthCheck.IntervalSeconds == 0 {
		cfg.HealthCheck.IntervalSeconds = 15
	}
	if cfg.HealthCheck.UnhealthyThreshold == 0 {
		cfg.HealthCheck.UnhealthyThreshold = 3
	}

	// Cleanup defaults
	if cfg.Cleanup.OnSessionEnd == "" {
		cfg.Cleanup.OnSessionEnd = "snapshot_restore"
	}
	if cfg.Cleanup.OrphanCheckIntervalSecs == 0 {
		cfg.Cleanup.OrphanCheckIntervalSecs = 60
	}
	if cfg.Cleanup.DiskCleanupThresholdGB == 0 {
		cfg.Cleanup.DiskCleanupThresholdGB = 20
	}
	if cfg.Cleanup.IdleTimeoutMinutes == 0 {
		cfg.Cleanup.IdleTimeoutMinutes = 5
	}

	// Artifacts defaults
	if cfg.Artifacts.StoragePath == "" {
		cfg.Artifacts.StoragePath = filepath.Join(cfg.Node.DataDir, "artifacts")
	}
	if cfg.Artifacts.RetentionDays == 0 {
		cfg.Artifacts.RetentionDays = 30
	}

	// License defaults
	if cfg.License.ValidationEndpoint == "" {
		cfg.License.ValidationEndpoint = "https://license.drizz.dev/v1/validate"
	}
	if cfg.License.GracePeriodHours == 0 {
		cfg.License.GracePeriodHours = 72
	}
}

// recommendedMaxEmulators returns a sensible default based on available RAM.
func recommendedMaxEmulators() int {
	// Reserve 4GB for macOS + daemon. Each emulator ~2.5GB.
	// On non-darwin, be conservative.
	if runtime.GOOS != "darwin" {
		return 2
	}

	// We can't reliably get total RAM from pure Go on macOS without CGO.
	// Default to a conservative number; setup wizard will detect actual RAM.
	log.Debug().Msg("config: using default max_concurrent=4 (run 'drizz-farm setup' for hardware-optimized config)")
	return 4
}
