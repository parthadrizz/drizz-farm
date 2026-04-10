package android

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/device"
)

// RunningEmulatorDevice wraps an already-running emulator that we didn't boot.
// Used to adopt emulators that survived daemon restart or were started manually.
type RunningEmulatorDevice struct {
	serial string
	name   string
	adb    *ADBClient
}

func NewRunningEmulatorDevice(serial string, adb *ADBClient) *RunningEmulatorDevice {
	return &RunningEmulatorDevice{serial: serial, adb: adb}
}

func (d *RunningEmulatorDevice) Kind() device.Kind    { return device.AndroidEmulator }
func (d *RunningEmulatorDevice) Serial() string        { return d.serial }
func (d *RunningEmulatorDevice) CanBoot() bool         { return false } // already running
func (d *RunningEmulatorDevice) CanSnapshot() bool     { return true }
func (d *RunningEmulatorDevice) CanDestroy() bool      { return true }

func (d *RunningEmulatorDevice) DisplayName() string {
	if d.name != "" {
		return d.name
	}
	return d.serial
}

func (d *RunningEmulatorDevice) GetConnectionInfo() device.ConnectionInfo {
	// Parse port from serial: "emulator-5554" → port 5555
	port := 0
	if strings.HasPrefix(d.serial, "emulator-") {
		fmt.Sscanf(d.serial, "emulator-%d", &port)
		port++ // ADB port = console port + 1
	}
	return device.ConnectionInfo{
		DeviceKind: string(device.AndroidEmulator),
		ADBSerial:  d.serial,
		ADBPort:    port,
		ConsolePort: port - 1,
	}
}

func (d *RunningEmulatorDevice) Prepare(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Read AVD name from device
	name, err := d.adb.Shell(ctx, d.serial, "getprop ro.kernel.qemu.avd_name")
	if err == nil && strings.TrimSpace(name) != "" {
		d.name = strings.TrimSpace(name)
	} else {
		// Fallback: try boot.hardware
		model, _ := d.adb.GetProp(ctx, d.serial, "ro.product.model")
		if model != "" {
			d.name = model
		} else {
			d.name = d.serial
		}
	}
	return nil
}

func (d *RunningEmulatorDevice) Reset(ctx context.Context) error {
	// Try snapshot restore, fallback to app uninstall
	_, err := d.adb.EmuCommand(ctx, d.serial, "avd snapshot load drizz_clean")
	if err == nil {
		return nil
	}
	packages, listErr := d.adb.ListThirdPartyPackages(ctx, d.serial)
	if listErr == nil {
		for _, pkg := range packages {
			_ = d.adb.Uninstall(ctx, d.serial, pkg)
		}
	}
	return nil
}

func (d *RunningEmulatorDevice) Shutdown(ctx context.Context) error {
	// Kill the emulator
	d.adb.EmuCommand(ctx, d.serial, "kill")
	return nil
}

func (d *RunningEmulatorDevice) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	val, err := d.adb.GetProp(ctx, d.serial, "sys.boot_completed")
	if err != nil {
		return err
	}
	if strings.TrimSpace(val) != "1" {
		return fmt.Errorf("boot not completed")
	}
	return nil
}
