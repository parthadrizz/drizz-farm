package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	"github.com/drizz-dev/drizz-farm/internal/health"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the drizz-farm daemon",
	Long:  "Starts the emulator pool manager daemon, warming up emulators and serving the REST API.",
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	startedAt := time.Now()

	// Load config
	cfg, err := config.Load()
	if err != nil {
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

	// Initialize pool
	runner := &android.DefaultRunner{}
	emulatorPool := pool.New(cfg, sdk, runner)
	if err := emulatorPool.Start(ctx); err != nil {
		return fmt.Errorf("pool start: %w", err)
	}

	// Session broker
	broker := session.NewBroker(cfg, emulatorPool)
	broker.Start(ctx)

	// Health checker
	adb := android.NewADBClient(sdk, runner)
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
		poolStatus := emulatorPool.Status()
		announcer, err = discovery.NewAnnouncer(ctx, discovery.AnnounceConfig{
			NodeName:      cfg.Node.Name,
			Port:          cfg.API.Port,
			Version:       buildinfo.Version,
			Tier:          string(lic.Current().Tier),
			AndroidAvail:  poolStatus.Warm,
			TotalCapacity: poolStatus.TotalCapacity,
		})
		if err != nil {
			log.Warn().Err(err).Msg("mDNS announcement failed (continuing)")
		}
	}

	// API server
	server := api.NewServer(cfg, emulatorPool, broker, lic, api.ServerDeps{StartedAt: startedAt})

	// Start API in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	log.Info().
		Str("node", cfg.Node.Name).
		Int("api_port", cfg.API.Port).
		Str("tier", string(lic.Current().Tier)).
		Msg("drizz-farm is LIVE")

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("  drizz-farm is LIVE on %s:%d\n", cfg.API.Host, cfg.API.Port)
	fmt.Printf("  Node: %s | Tier: %s\n", cfg.Node.Name, lic.Current().Tier)
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
