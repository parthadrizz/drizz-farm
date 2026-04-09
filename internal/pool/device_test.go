package pool

import (
	"context"
	"testing"

	"github.com/drizz-dev/drizz-farm/internal/device"
)

// mockDevice implements device.Device for testing.
type mockDevice struct {
	kind     device.Kind
	serial   string
	name     string
	prepared bool
	reset    bool
	shutdown bool
	healthy  bool
}

func (d *mockDevice) Kind() device.Kind          { return d.kind }
func (d *mockDevice) Serial() string             { return d.serial }
func (d *mockDevice) DisplayName() string        { return d.name }
func (d *mockDevice) CanBoot() bool              { return d.kind == device.AndroidEmulator }
func (d *mockDevice) CanSnapshot() bool          { return d.kind == device.AndroidEmulator }
func (d *mockDevice) CanDestroy() bool           { return d.kind == device.AndroidEmulator }

func (d *mockDevice) Prepare(_ context.Context) error  { d.prepared = true; return nil }
func (d *mockDevice) Reset(_ context.Context) error     { d.reset = true; return nil }
func (d *mockDevice) Shutdown(_ context.Context) error  { d.shutdown = true; return nil }
func (d *mockDevice) HealthCheck(_ context.Context) error {
	if d.healthy { return nil }
	return &InvalidTransitionError{InstanceID: "mock"}
}
func (d *mockDevice) GetConnectionInfo() ConnectionInfo {
	return ConnectionInfo{ADBSerial: d.serial, DeviceKind: string(d.kind)}
}

func TestDeviceInstanceWithDevice(t *testing.T) {
	dev := &mockDevice{kind: device.AndroidEmulator, serial: "emu-5554", name: "Pixel 7", healthy: true}
	inst := &DeviceInstance{
		ID:     "test-1",
		State:  StateWarm,
		Device: dev,
	}

	snap := inst.Snapshot()
	if snap.Serial != "emu-5554" {
		t.Errorf("expected serial 'emu-5554', got '%s'", snap.Serial)
	}
	if snap.DeviceName != "Pixel 7" {
		t.Errorf("expected name 'Pixel 7', got '%s'", snap.DeviceName)
	}
	if snap.DeviceKind != device.AndroidEmulator {
		t.Errorf("expected kind AndroidEmulator, got '%s'", snap.DeviceKind)
	}
	if snap.Connection.ADBSerial != "emu-5554" {
		t.Errorf("expected connection serial 'emu-5554', got '%s'", snap.Connection.ADBSerial)
	}
}

func TestDeviceInstanceUSB(t *testing.T) {
	dev := &mockDevice{kind: device.AndroidUSB, serial: "R5CR123", name: "Pixel 8 (R5CR123)", healthy: true}

	if dev.CanBoot() {
		t.Error("USB device should not be bootable")
	}
	if dev.CanSnapshot() {
		t.Error("USB device should not support snapshots")
	}
	if dev.CanDestroy() {
		t.Error("USB device should not be destroyable")
	}
}

func TestDeviceInstanceEmulator(t *testing.T) {
	dev := &mockDevice{kind: device.AndroidEmulator, serial: "emu-5554", name: "drizz_pixel7_0", healthy: true}

	if !dev.CanBoot() {
		t.Error("emulator should be bootable")
	}
	if !dev.CanSnapshot() {
		t.Error("emulator should support snapshots")
	}
	if !dev.CanDestroy() {
		t.Error("emulator should be destroyable")
	}
}

func TestDeviceReset(t *testing.T) {
	dev := &mockDevice{kind: device.AndroidUSB, serial: "R5CR123", name: "phone", healthy: true}
	dev.Reset(context.Background())
	if !dev.reset {
		t.Error("expected Reset to be called")
	}
}

func TestDeviceShutdown(t *testing.T) {
	dev := &mockDevice{kind: device.AndroidEmulator, serial: "emu-5554", name: "emu", healthy: true}
	dev.Shutdown(context.Background())
	if !dev.shutdown {
		t.Error("expected Shutdown to be called")
	}
}

func TestStateTransitionCreatingToWarm(t *testing.T) {
	// USB devices skip boot — go Creating → Warm
	inst := &DeviceInstance{ID: "usb-1", State: StateCreating}
	if err := inst.TransitionTo(StateWarm); err != nil {
		t.Errorf("Creating → Warm should be valid for USB, got: %v", err)
	}
}

func TestStateTransitionErrorToWarm(t *testing.T) {
	// Recovery: Error → Warm
	inst := &DeviceInstance{ID: "err-1", State: StateError}
	if err := inst.TransitionTo(StateWarm); err != nil {
		t.Errorf("Error → Warm should be valid for recovery, got: %v", err)
	}
}
