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

	// Try multiple properties to find the AVD name
	for _, prop := range []string{
		"ro.kernel.qemu.avd_name",
		"ro.boot.qemu.avd_name",
		"ro.product.model",
	} {
		val, err := d.adb.GetProp(ctx, d.serial, prop)
		if err == nil && strings.TrimSpace(val) != "" {
			d.name = strings.TrimSpace(val)
			return nil
		}
	}

	// Last resort: use shell to check emulator window title
	output, err := d.adb.Shell(ctx, d.serial, "getprop | grep avd")
	if err == nil && output != "" {
		// Parse "[ro.boot.qemu.avd_name]: [drizz_api34_ext8_play_0]"
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "avd_name") {
				parts := strings.SplitN(line, "]: [", 2)
				if len(parts) == 2 {
					d.name = strings.TrimSuffix(strings.TrimSpace(parts[1]), "]")
					return nil
				}
			}
		}
	}

	d.name = d.serial
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
	// Clean up scrcpy/screenrecord processes and ADB forwards before killing
	d.adb.Shell(ctx, d.serial, "pkill -f scrcpy")
	d.adb.Shell(ctx, d.serial, "pkill screenrecord")
	d.adb.ForwardRemoveAll(ctx, d.serial)

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
