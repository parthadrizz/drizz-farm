package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config is the root configuration for drizz-farm.
type Config struct {
	Node        NodeConfig        `yaml:"node"        mapstructure:"node"`
	Pool        PoolConfig        `yaml:"pool"        mapstructure:"pool"`
	API         APIConfig         `yaml:"api"         mapstructure:"api"`
	Network     NetworkConfig     `yaml:"network"     mapstructure:"network"`
	HealthCheck HealthCheckConfig `yaml:"health_check" mapstructure:"health_check"`
	Cleanup     CleanupConfig     `yaml:"cleanup"     mapstructure:"cleanup"`
	Artifacts   ArtifactsConfig   `yaml:"artifacts"   mapstructure:"artifacts"`
	License     LicenseConfig     `yaml:"license"     mapstructure:"license"`
}

type NodeConfig struct {
	Name        string `yaml:"name"         mapstructure:"name"`
	DataDir     string `yaml:"data_dir"     mapstructure:"data_dir"`
	LogLevel    string `yaml:"log_level"    mapstructure:"log_level"`
	MetricsPort int    `yaml:"metrics_port" mapstructure:"metrics_port"`
}

type PoolConfig struct {
	MaxConcurrent         int             `yaml:"max_concurrent"           mapstructure:"max_concurrent"`
	SessionTimeoutMinutes int             `yaml:"session_timeout_minutes"  mapstructure:"session_timeout_minutes"`
	SessionMaxMinutes     int             `yaml:"session_max_duration_minutes" mapstructure:"session_max_duration_minutes"`
	QueueMaxSize          int             `yaml:"queue_max_size"           mapstructure:"queue_max_size"`
	QueueTimeoutSeconds   int             `yaml:"queue_timeout_seconds"    mapstructure:"queue_timeout_seconds"`
	PortRangeMin          int             `yaml:"port_range_min"           mapstructure:"port_range_min"`
	PortRangeMax          int             `yaml:"port_range_max"           mapstructure:"port_range_max"`
	Warmup                []WarmupConfig  `yaml:"warmup"                   mapstructure:"warmup"`
	Profiles              ProfilesConfig  `yaml:"profiles"                 mapstructure:"profiles"`
}

type WarmupConfig struct {
	Profile string `yaml:"profile" mapstructure:"profile"`
	Count   int    `yaml:"count"   mapstructure:"count"`
}

type ProfilesConfig struct {
	Android map[string]AndroidProfile `yaml:"android" mapstructure:"android"`
	IOS     map[string]IOSProfile     `yaml:"ios"     mapstructure:"ios"`
}

type AndroidProfile struct {
	Device              string `yaml:"device"                mapstructure:"device"`
	SystemImage         string `yaml:"system_image"          mapstructure:"system_image"`
	RAMMB               int    `yaml:"ram_mb"                mapstructure:"ram_mb"`
	HeapMB              int    `yaml:"heap_mb"               mapstructure:"heap_mb"`
	DiskSizeMB          int    `yaml:"disk_size_mb"          mapstructure:"disk_size_mb"`
	GPU                 string `yaml:"gpu"                   mapstructure:"gpu"`
	Snapshot            bool   `yaml:"snapshot"              mapstructure:"snapshot"`
	BootTimeoutSeconds  int    `yaml:"boot_timeout_seconds"  mapstructure:"boot_timeout_seconds"`
}

type IOSProfile struct {
	Runtime            string `yaml:"runtime"               mapstructure:"runtime"`
	DeviceType         string `yaml:"device_type"            mapstructure:"device_type"`
	BootTimeoutSeconds int    `yaml:"boot_timeout_seconds"  mapstructure:"boot_timeout_seconds"`
}

type APIConfig struct {
	Host     string `yaml:"host"      mapstructure:"host"`
	Port     int    `yaml:"port"      mapstructure:"port"`
	GRPCPort int    `yaml:"grpc_port" mapstructure:"grpc_port"`
}

type NetworkConfig struct {
	AllowedCIDRs []string    `yaml:"allowed_cidrs" mapstructure:"allowed_cidrs"`
	MDNS         MDNSConfig  `yaml:"mdns"          mapstructure:"mdns"`
}

type MDNSConfig struct {
	Enabled     bool   `yaml:"enabled"      mapstructure:"enabled"`
	ServiceType string `yaml:"service_type" mapstructure:"service_type"`
}

type HealthCheckConfig struct {
	IntervalSeconds    int `yaml:"interval_seconds"    mapstructure:"interval_seconds"`
	UnhealthyThreshold int `yaml:"unhealthy_threshold" mapstructure:"unhealthy_threshold"`
}

type CleanupConfig struct {
	OnSessionEnd             string `yaml:"on_session_end"              mapstructure:"on_session_end"`
	OrphanCheckIntervalSecs  int    `yaml:"orphan_check_interval_seconds" mapstructure:"orphan_check_interval_seconds"`
	DiskCleanupThresholdGB   int    `yaml:"disk_cleanup_threshold_gb"   mapstructure:"disk_cleanup_threshold_gb"`
}

type ArtifactsConfig struct {
	StoragePath        string `yaml:"storage_path"         mapstructure:"storage_path"`
	VideoRecording     bool   `yaml:"video_recording"      mapstructure:"video_recording"`
	ScreenshotOnFail   bool   `yaml:"screenshot_on_failure" mapstructure:"screenshot_on_failure"`
	LogcatCapture      bool   `yaml:"logcat_capture"       mapstructure:"logcat_capture"`
	NetworkHARCapture  bool   `yaml:"network_har_capture"  mapstructure:"network_har_capture"`
	RetentionDays      int    `yaml:"retention_days"       mapstructure:"retention_days"`
}

type LicenseConfig struct {
	Key                string `yaml:"key"                  mapstructure:"key"`
	ValidationEndpoint string `yaml:"validation_endpoint"  mapstructure:"validation_endpoint"`
	GracePeriodHours   int    `yaml:"grace_period_hours"   mapstructure:"grace_period_hours"`
}

// Load reads the configuration from viper (already initialized) into a Config struct.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	applyDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: validate: %w", err)
	}
	return cfg, nil
}

// Validate checks that the configuration is sane.
func (c *Config) Validate() error {
	if c.Pool.MaxConcurrent < 1 {
		return fmt.Errorf("pool.max_concurrent must be >= 1, got %d", c.Pool.MaxConcurrent)
	}
	if c.API.Port < 1 || c.API.Port > 65535 {
		return fmt.Errorf("api.port must be 1-65535, got %d", c.API.Port)
	}
	if c.Pool.PortRangeMin >= c.Pool.PortRangeMax {
		return fmt.Errorf("pool.port_range_min (%d) must be < port_range_max (%d)", c.Pool.PortRangeMin, c.Pool.PortRangeMax)
	}
	if len(c.Pool.Profiles.Android) == 0 && len(c.Pool.Profiles.IOS) == 0 {
		return fmt.Errorf("at least one profile (android or ios) must be defined")
	}
	for name, p := range c.Pool.Profiles.Android {
		if p.SystemImage == "" {
			return fmt.Errorf("android profile %q: system_image is required", name)
		}
		if p.Device == "" {
			return fmt.Errorf("android profile %q: device is required", name)
		}
	}
	return nil
}

// DataDir returns the resolved data directory path.
func (c *Config) DataDir() string {
	if c.Node.DataDir != "" {
		return c.Node.DataDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".drizz-farm")
}

// ArtifactsDir returns the resolved artifacts directory path.
func (c *Config) ArtifactsDir() string {
	if c.Artifacts.StoragePath != "" {
		if c.Artifacts.StoragePath[0] == '~' {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, c.Artifacts.StoragePath[1:])
		}
		return c.Artifacts.StoragePath
	}
	return filepath.Join(c.DataDir(), "artifacts")
}
