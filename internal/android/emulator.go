package android

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/rs/zerolog/log"
)

const (
	cleanSnapshotName = "drizz_clean"
	defaultBootTimeout = 120 * time.Second
)

// EmulatorProcess represents a running emulator instance.
type EmulatorProcess struct {
	Cmd     *exec.Cmd
	PID     int
	AVDName string
	Ports   PortPair
	Serial  string // e.g., "emulator-5554"
}

// BootOptions configures how an emulator boots.
type BootOptions struct {
	Profile       config.AndroidProfile
	Ports         PortPair
	FromSnapshot  bool   // Boot from clean snapshot (fast boot)
	SnapshotName  string // Snapshot to load (default: drizz_clean)
	NoWindow      bool
	NoAudio       bool
}

// EmulatorController manages emulator process lifecycle.
type EmulatorController struct {
	sdk    *SDK
	runner CommandRunner
}

// NewEmulatorController creates a new emulator controller.
func NewEmulatorController(sdk *SDK, runner CommandRunner) *EmulatorController {
	return &EmulatorController{sdk: sdk, runner: runner}
}

// Boot starts an emulator instance.
func (e *EmulatorController) Boot(ctx context.Context, avdName string, opts BootOptions) (*EmulatorProcess, error) {
	args := []string{
		"-avd", avdName,
		"-port", strconv.Itoa(opts.Ports.Console),
		"-memory", strconv.Itoa(opts.Profile.RAMMB),
		"-no-boot-anim",
	}

	if opts.Profile.DiskSizeMB > 0 {
		args = append(args, "-partition-size", strconv.Itoa(opts.Profile.DiskSizeMB))
	}

	// GPU acceleration
	gpu := opts.Profile.GPU
	if gpu == "" {
		gpu = "host"
	}
	args = append(args, "-gpu", gpu)

	if opts.NoWindow {
		args = append(args, "-no-window")
	}
	if opts.NoAudio {
		args = append(args, "-no-audio")
	}

	// Snapshot boot
	if opts.FromSnapshot {
		snapName := opts.SnapshotName
		if snapName == "" {
			snapName = cleanSnapshotName
		}
		args = append(args, "-snapshot", snapName, "-no-snapshot-save")
	}

	cmd, err := e.runner.Start(ctx, e.sdk.EmulatorPath(), args...)
	if err != nil {
		return nil, fmt.Errorf("boot emulator %s: %w", avdName, err)
	}

	proc := &EmulatorProcess{
		Cmd:     cmd,
		PID:     cmd.Process.Pid,
		AVDName: avdName,
		Ports:   opts.Ports,
		Serial:  fmt.Sprintf("emulator-%d", opts.Ports.Console),
	}

	log.Info().
		Str("avd", avdName).
		Int("pid", proc.PID).
		Str("serial", proc.Serial).
		Int("console_port", opts.Ports.Console).
		Int("adb_port", opts.Ports.ADB).
		Bool("snapshot_boot", opts.FromSnapshot).
		Msg("emulator booting")

	return proc, nil
}

// Kill terminates an emulator process gracefully, falling back to SIGKILL.
func (e *EmulatorController) Kill(proc *EmulatorProcess) error {
	if proc == nil || proc.Cmd == nil || proc.Cmd.Process == nil {
		return nil
	}

	pid := proc.PID
	log.Info().Int("pid", pid).Str("avd", proc.AVDName).Msg("killing emulator")

	// Try SIGTERM first
	if err := proc.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		if !isProcessDone(err) {
			return fmt.Errorf("kill emulator pid %d: %w", pid, err)
		}
		return nil
	}

	// Wait up to 10 seconds for graceful exit
	done := make(chan error, 1)
	go func() {
		_, err := proc.Cmd.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		// Force kill
		log.Warn().Int("pid", pid).Msg("emulator did not exit gracefully, sending SIGKILL")
		if err := proc.Cmd.Process.Kill(); err != nil {
			if !isProcessDone(err) {
				return fmt.Errorf("force kill emulator pid %d: %w", pid, err)
			}
		}
		return nil
	}
}

// SnapshotSave saves the current state as a named snapshot.
func (e *EmulatorController) SnapshotSave(ctx context.Context, adb *ADBClient, serial string, name string) error {
	_, err := adb.EmuCommand(ctx, serial, fmt.Sprintf("avd snapshot save %s", name))
	if err != nil {
		return fmt.Errorf("snapshot save %s on %s: %w", name, serial, err)
	}
	log.Info().Str("serial", serial).Str("snapshot", name).Msg("snapshot saved")
	return nil
}

// SnapshotLoad restores a named snapshot.
func (e *EmulatorController) SnapshotLoad(ctx context.Context, adb *ADBClient, serial string, name string) error {
	_, err := adb.EmuCommand(ctx, serial, fmt.Sprintf("avd snapshot load %s", name))
	if err != nil {
		return fmt.Errorf("snapshot load %s on %s: %w", name, serial, err)
	}
	log.Info().Str("serial", serial).Str("snapshot", name).Msg("snapshot loaded")
	return nil
}

// SaveCleanSnapshot waits for boot and saves the initial clean state.
func (e *EmulatorController) SaveCleanSnapshot(ctx context.Context, adb *ADBClient, serial string, bootTimeout time.Duration) error {
	if bootTimeout == 0 {
		bootTimeout = defaultBootTimeout
	}

	log.Info().Str("serial", serial).Msg("waiting for device to come online...")

	// Wait for device to be online
	if err := adb.WaitForDevice(ctx, serial, bootTimeout); err != nil {
		return fmt.Errorf("save clean snapshot: %w", err)
	}
	log.Info().Str("serial", serial).Msg("device online, waiting for boot to complete...")

	// Wait for boot to complete
	if err := adb.WaitForBoot(ctx, serial, bootTimeout); err != nil {
		return fmt.Errorf("save clean snapshot: %w", err)
	}
	log.Info().Str("serial", serial).Msg("boot complete, saving clean snapshot...")

	// Brief pause to let things settle
	time.Sleep(2 * time.Second)

	// Save the clean snapshot
	if err := e.SnapshotSave(ctx, adb, serial, cleanSnapshotName); err != nil {
		return err
	}
	log.Info().Str("serial", serial).Msg("clean snapshot saved — future boots will be fast (~5s)")
	return nil
}

// IsRunning checks if an emulator process is still alive.
func (e *EmulatorController) IsRunning(proc *EmulatorProcess) bool {
	if proc == nil || proc.Cmd == nil || proc.Cmd.Process == nil {
		return false
	}
	// On Unix, sending signal 0 checks if process exists
	err := proc.Cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

func isProcessDone(err error) bool {
	return err != nil && (os.IsPermission(err) || err.Error() == "os: process already finished")
}
