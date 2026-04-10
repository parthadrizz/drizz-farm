package android

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/device"
)

// EmulatorDevice implements pool.Device for Android emulators.
type EmulatorDevice struct {
	avdName   string
	profile   config.AndroidProfile
	ports     PortPair
	serial    string
	process   *EmulatorProcess
	emuCtrl   *EmulatorController
	adb       *ADBClient
	portAlloc *PortAllocator
	visible   bool
}

// EmulatorDeviceConfig holds dependencies needed to create an EmulatorDevice.
type EmulatorDeviceConfig struct {
	AVDName   string
	Profile   config.AndroidProfile
	EmuCtrl   *EmulatorController
	ADB       *ADBClient
	PortAlloc *PortAllocator
	Visible   bool
}

// NewEmulatorDevice creates a new emulator device (not yet booted).
func NewEmulatorDevice(cfg EmulatorDeviceConfig) *EmulatorDevice {
	return &EmulatorDevice{
		avdName:   cfg.AVDName,
		profile:   cfg.Profile,
		emuCtrl:   cfg.EmuCtrl,
		adb:       cfg.ADB,
		portAlloc: cfg.PortAlloc,
		visible:   cfg.Visible,
	}
}

func (d *EmulatorDevice) Kind() device.Kind    { return device.AndroidEmulator }
func (d *EmulatorDevice) Serial() string            { return d.serial }
func (d *EmulatorDevice) DisplayName() string       { return d.avdName }
func (d *EmulatorDevice) CanBoot() bool             { return true }
func (d *EmulatorDevice) CanSnapshot() bool         { return true }
func (d *EmulatorDevice) CanDestroy() bool          { return true }

func (d *EmulatorDevice) GetConnectionInfo() device.ConnectionInfo {
	return device.ConnectionInfo{
		DeviceKind:  string(device.AndroidEmulator),
		ADBPort:     d.ports.ADB,
		ADBSerial:   d.serial,
		ConsolePort: d.ports.Console,
	}
}

// Prepare boots the emulator, waits for it to be ready, and saves a clean snapshot.
func (d *EmulatorDevice) Prepare(ctx context.Context) error {
	// Allocate ports
	ports, err := d.portAlloc.Allocate()
	if err != nil {
		return fmt.Errorf("allocate ports: %w", err)
	}
	d.ports = ports
	d.serial = fmt.Sprintf("emulator-%d", ports.Console)

	// Boot
	bootOpts := BootOptions{
		Profile:  d.profile,
		Ports:    ports,
		NoWindow: !d.visible,
		NoAudio:  true,
	}

	proc, err := d.emuCtrl.Boot(ctx, d.avdName, bootOpts)
	if err != nil {
		d.portAlloc.Release(ports)
		return fmt.Errorf("boot emulator %s: %w", d.avdName, err)
	}
	d.process = proc

	// Wait for boot and save snapshot
	bootTimeout := time.Duration(d.profile.BootTimeoutSeconds) * time.Second
	if bootTimeout == 0 {
		bootTimeout = 120 * time.Second
	}

	if err := d.emuCtrl.SaveCleanSnapshot(ctx, d.adb, d.serial, bootTimeout); err != nil {
		log.Warn().Err(err).Str("avd", d.avdName).Msg("failed to save clean snapshot (continuing)")
	}

	return nil
}

// Reset restores the clean snapshot, falling back to app uninstall.
func (d *EmulatorDevice) Reset(ctx context.Context) error {
	log.Info().Str("avd", d.avdName).Msg("resetting emulator")

	err := d.emuCtrl.SnapshotLoad(ctx, d.adb, d.serial, "drizz_clean")
	if err == nil {
		return nil
	}

	// Fallback: clear third-party apps
	log.Warn().Err(err).Str("avd", d.avdName).Msg("snapshot restore failed, clearing apps")
	packages, listErr := d.adb.ListThirdPartyPackages(ctx, d.serial)
	if listErr == nil {
		for _, pkg := range packages {
			_ = d.adb.Uninstall(ctx, d.serial, pkg)
		}
	}
	return nil
}

// Shutdown kills the emulator process and releases ports.
// Also cleans up any scrcpy/screenrecord processes and ADB forwards.
func (d *EmulatorDevice) Shutdown(ctx context.Context) error {
	serial := d.Serial()

	// Kill scrcpy-server and screenrecord on the device (they survive host-side kills)
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.adb.Shell(cleanupCtx, serial, "pkill -f scrcpy")
	d.adb.Shell(cleanupCtx, serial, "pkill screenrecord")
	// Remove all ADB forwards for this device
	d.adb.ForwardRemoveAll(cleanupCtx, serial)

	if d.process != nil {
		if err := d.emuCtrl.Kill(d.process); err != nil {
			log.Error().Err(err).Str("avd", d.avdName).Msg("failed to kill emulator")
		}
	}
	if d.ports.Console != 0 {
		d.portAlloc.Release(d.ports)
	}
	return nil
}

// HealthCheck verifies the emulator is booted and responsive.
func (d *EmulatorDevice) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	val, err := d.adb.GetProp(ctx, d.serial, "sys.boot_completed")
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	if val != "1" {
		return fmt.Errorf("health: sys.boot_completed=%s", val)
	}
	return nil
}

// IsProcessRunning checks if the emulator process is still alive.
func (d *EmulatorDevice) IsProcessRunning() bool {
	return d.emuCtrl.IsRunning(d.process)
}

// Ports returns the allocated port pair.
func (d *EmulatorDevice) Ports() PortPair {
	return d.ports
}
