package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/api"
	"github.com/drizz-dev/drizz-farm/internal/buildinfo"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/daemon"
	"github.com/drizz-dev/drizz-farm/internal/health"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/registry"
	"github.com/drizz-dev/drizz-farm/internal/session"
	"github.com/drizz-dev/drizz-farm/internal/store"
	"github.com/drizz-dev/drizz-farm/internal/telemetry"
)

var visibleEmulators bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the drizz-farm daemon",
	Long: `Starts the daemon — API server, monitoring, and pool manager.
Emulators are NOT booted at start. They boot on-demand when sessions are created.
Idle emulators shut down automatically after the configured timeout.`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVar(&visibleEmulators, "visible", false, "show emulator windows (for development/debugging)")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	startedAt := time.Now()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		home, _ := os.UserHomeDir()
		configPath := filepath.Join(home, ".drizz-farm", "config.yaml")
		if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
			return fmt.Errorf("no config found at %s\n\nRun these commands first:\n  drizz-farm setup    # check prerequisites\n  drizz-farm create   # create the farm", configPath)
		}
		return fmt.Errorf("load config: %w", err)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir(), 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// PID file
	pidFile := daemon.NewPIDFile(cfg.DataDir())
	if pidFile.IsRunning() {
		return fmt.Errorf("drizz-farm is already running (see %s)", pidFile.Path)
	}
	if err := pidFile.Write(); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer pidFile.Remove()

	// License
	lic := license.NewValidator()
	if cfg.License.Key != "" {
		if _, err := lic.Validate(cfg.License.Key); err != nil {
			log.Warn().Err(err).Msg("license validation failed, using defaults")
		}
	}

	// Set JAVA_HOME from stored config so SDK tools (sdkmanager, avdmanager) can find Java
	if cfg.SDK.Java != "" {
		javaHome := filepath.Dir(filepath.Dir(cfg.SDK.Java))
		os.Setenv("JAVA_HOME", javaHome)
	}

	// Load SDK paths from config, validate each one actually works
	sdkPaths := &cfg.SDK
	redetected := false
	validatePath := func(p *string) {
		if *p == "" { return }
		if _, err := os.Stat(*p); err != nil { *p = ""; redetected = true }
	}
	validatePath(&sdkPaths.ADB)
	validatePath(&sdkPaths.AVDManager)
	validatePath(&sdkPaths.Emulator)
	validatePath(&sdkPaths.SDKManager)
	if sdkPaths.Root != "" { if _, err := os.Stat(sdkPaths.Root); err != nil { sdkPaths.Root = ""; redetected = true } }
	if redetected {
		log.Warn().Msg("some SDK paths in config are stale — re-detecting")
	}
	android.SetResolvedPaths(sdkPaths.ADB, sdkPaths.AVDManager, sdkPaths.Emulator, sdkPaths.SDKManager)

	sdk, err := android.DetectSDK()
	if err != nil {
		return fmt.Errorf("android SDK: %w\nRun 'drizz-farm setup' to detect paths", err)
	}
	if err := sdk.Validate(); err != nil {
		return fmt.Errorf("android SDK validation: %w\nRun 'drizz-farm setup' to fix", err)
	}
	log.Info().Str("sdk", sdk.Root).Msg("android SDK detected")

	// Warn if Xcode CLI tools missing on macOS (emulator may need it)
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("xcode-select", "-p").CombinedOutput(); err != nil || len(strings.TrimSpace(string(out))) == 0 {
			log.Warn().Msg("Xcode Command Line Tools not found; run: xcode-select --install")
		}
	}

	// Context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Apply CLI overrides
	if visibleEmulators {
		cfg.Pool.VisibleEmulators = true
		log.Info().Msg("emulator windows will be visible (--visible flag)")
	}

	// SQLite store
	dataStore, err := store.New(cfg.DataDir())
	if err != nil {
		log.Warn().Err(err).Msg("SQLite store failed (continuing without persistence)")
	} else {
		defer dataStore.Close()
		log.Info().Msg("SQLite store opened")
	}

	// Initialize pool — boots nothing, emulators start on-demand
	runner := &android.DefaultRunner{}
	adb := android.NewADBClient(sdk, runner)
	emulatorPool := pool.New(cfg, sdk, runner)

	// Register USB device scanner — auto-discovers plugged-in phones
	emulatorPool.RegisterScanner(android.NewUSBScanner(adb))

	if err := emulatorPool.Start(ctx); err != nil {
		return fmt.Errorf("pool start: %w", err)
	}

	// Session broker — local-only, no federation
	broker := session.NewBroker(cfg, emulatorPool, dataStore)
	broker.Start(ctx)

	// Health checker
	probes := []health.Probe{
		health.NewBootProbe(adb),
	}
	healthChecker := health.NewChecker(
		probes,
		time.Duration(cfg.HealthCheck.IntervalSeconds)*time.Second,
		cfg.HealthCheck.UnhealthyThreshold,
		func(instanceID string) {
			log.Warn().Str("instance", instanceID).Msg("health: marking instance as error")
			if inst, ok := emulatorPool.GetInstance(instanceID); ok {
				_ = inst.TransitionTo(pool.StateError)
			}
		},
	)
	healthChecker.Start(ctx)

	// Node registry — who else is in this group? (serves GET /nodes)
	registryPath := filepath.Join(cfg.DataDir(), "nodes.yaml")
	nodeReg, err := registry.New(registryPath)
	if err != nil {
		log.Warn().Err(err).Msg("registry: load failed, starting standalone")
		nodeReg, _ = registry.New(registryPath)
	}

	// Optional heartbeat — fires once every 24h if telemetry isn't disabled.
	// Anonymous: just install-id + version + activity counts. Never blocks
	// the daemon; failures are logged at debug level only.
	telemetry.SetVersion(buildinfo.Version)
	if installID, err := telemetry.GetOrCreateInstallID(cfg.DataDir()); err == nil {
		telemetry.StartHeartbeatLoop(ctx, installID, buildinfo.Version, func() (int, int, int) {
			nodeCount := len(nodeReg.Nodes())
			if nodeCount < 1 {
				nodeCount = 1 // standalone = 1
			}
			sessions, emulators := 0, 0
			if dataStore != nil {
				if c, err := dataStore.SessionsSince(time.Now().Add(-24 * time.Hour)); err == nil {
					sessions = c
				}
			}
			// Emulators booted today is a reasonable proxy for "activity."
			// We don't track this separately yet; reuse session count for now.
			emulators = sessions
			return nodeCount, sessions, emulators
		})
	}

	// API server
	server := api.NewServer(cfg, emulatorPool, broker, lic, api.ServerDeps{
		StartedAt: startedAt,
		SDK:       sdk,
		Runner:    runner,
		Store:     dataStore,
		Registry:  nodeReg,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	// ── Startup banner ──────────────────────────────────────────
	hostname, _ := os.Hostname()
	lanIP := getLANIP()
	// macOS hostnames already end in .local — don't double it
	localHost := hostname
	if !strings.HasSuffix(localHost, ".local") {
		localHost = localHost + ".local"
	}
	localURL := fmt.Sprintf("http://%s:%d", localHost, cfg.API.Port)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  drizz-farm is LIVE\n\n")
	if nodeReg.HasGroup() {
		fmt.Printf("  Group:     %s (%d nodes)\n", nodeReg.GroupName(), len(nodeReg.Nodes()))
	} else {
		fmt.Printf("  Mode:      Node (standalone)\n")
	}
	fmt.Printf("  URL:       %s\n", localURL)
	fmt.Printf("  IP:        http://%s:%d\n", lanIP, cfg.API.Port)
	fmt.Printf("  Capacity:  %d emulators\n", cfg.Pool.MaxConcurrent)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	log.Info().
		Str("node", cfg.Node.Name).
		Int("api_port", cfg.API.Port).
		Str("tier", string(lic.Current().Tier)).
		Int("capacity", cfg.Pool.MaxConcurrent).
		Msg("drizz-farm is LIVE — emulators boot on-demand")

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case err := <-errCh:
		if err != nil {
			log.Error().Err(err).Msg("api server error")
		}
	}

	// Graceful shutdown
	log.Info().Msg("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop in order: API → broker → health → pool
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("api shutdown error")
	}
	broker.Stop()
	healthChecker.Stop()
	if err := emulatorPool.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("pool shutdown error")
	}

	cancel()
	log.Info().Dur("uptime", time.Since(startedAt)).Msg("drizz-farm stopped")
	return nil
}

func getLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

func getSystemMemoryMB() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Sys) / 1024 / 1024
}
