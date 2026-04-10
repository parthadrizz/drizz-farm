package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
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
	"github.com/drizz-dev/drizz-farm/internal/discovery"
	"github.com/drizz-dev/drizz-farm/internal/federation"
	"github.com/drizz-dev/drizz-farm/internal/health"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
	"github.com/drizz-dev/drizz-farm/internal/store"
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

	// Detect Android SDK
	sdk, err := android.DetectSDK()
	if err != nil {
		return fmt.Errorf("android SDK: %w", err)
	}
	if err := sdk.Validate(); err != nil {
		return fmt.Errorf("android SDK validation: %w", err)
	}
	log.Info().Str("sdk", sdk.Root).Msg("android SDK detected")

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

	// Federation registry
	fedRegistry := federation.NewRegistry(getLANIP(), cfg.API.Port)
	fedRegistry.UpdateSelf(cfg.Node.Name, cfg.Pool.MaxConcurrent, cfg.Pool.MaxConcurrent, runtime.NumCPU(), int(getSystemMemoryMB()))
	fedRegistry.SetSelfUpdateFn(func() {
		status := emulatorPool.Status()
		fedRegistry.UpdateSelf(cfg.Node.Name, status.TotalCapacity, status.Warm, runtime.NumCPU(), int(getSystemMemoryMB()))
	})
	fedRegistry.StartRefreshLoop(ctx, 10*time.Second)

	// Session broker
	broker := session.NewBroker(cfg, emulatorPool, dataStore, fedRegistry)
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

	// mDNS announcement
	var announcer *discovery.Announcer
	if cfg.Network.MDNS.Enabled {
		announcer, err = discovery.NewAnnouncer(ctx, discovery.AnnounceConfig{
			NodeName:      cfg.Node.Name,
			Port:          cfg.API.Port,
			Version:       buildinfo.Version,
			Tier:          string(lic.Current().Tier),
			TotalCapacity: cfg.Pool.MaxConcurrent,
		})
		if err != nil {
			log.Warn().Err(err).Msg("mDNS announcement failed (continuing)")
		}
	}

	// Discover peers via mDNS and add to federation
	if cfg.Network.MDNS.Enabled {
		go func() {
			time.Sleep(3 * time.Second) // Wait for own mDNS to settle
			nodes, err := discovery.Browse(ctx, 5*time.Second)
			if err == nil {
				for _, n := range nodes {
					fedRegistry.AddPeer(n.Name, n.Host, n.Port)
				}
				if len(nodes) > 0 {
					log.Info().Int("peers", len(nodes)).Msg("federation: discovered peers")
				}
			}
			// Re-discover every 30 seconds
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					nodes, err := discovery.Browse(ctx, 3*time.Second)
					if err == nil {
						for _, n := range nodes {
							fedRegistry.AddPeer(n.Name, n.Host, n.Port)
						}
					}
				}
			}
		}()
	}

	// API server
	server := api.NewServer(cfg, emulatorPool, broker, lic, api.ServerDeps{
		StartedAt: startedAt,
		SDK:       sdk,
		Runner:    runner,
		Store:      dataStore,
		Federation: fedRegistry,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	log.Info().
		Str("node", cfg.Node.Name).
		Int("api_port", cfg.API.Port).
		Str("tier", string(lic.Current().Tier)).
		Int("capacity", cfg.Pool.MaxConcurrent).
		Msg("drizz-farm is LIVE — emulators boot on-demand")

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  drizz-farm is LIVE on %s:%d\n", cfg.API.Host, cfg.API.Port)
	fmt.Printf("  Node: %s | Tier: %s\n", cfg.Node.Name, lic.Current().Tier)
	fmt.Printf("  Emulators boot on-demand (capacity: %d)\n", cfg.Pool.MaxConcurrent)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

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

	// Stop in order: API → broker → health → pool → mDNS
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("api shutdown error")
	}
	broker.Stop()
	healthChecker.Stop()
	if err := emulatorPool.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("pool shutdown error")
	}
	if announcer != nil {
		announcer.Shutdown()
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
