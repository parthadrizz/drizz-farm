package android

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/device"
)

// USBDevice implements pool.Device for USB-connected Android phones.
type USBDevice struct {
	serial string // hardware serial e.g. "R5CR1234567"
	model  string // e.g. "Pixel 8"
	adb    *ADBClient
}

// NewUSBDevice creates a USB device reference.
func NewUSBDevice(serial string, adb *ADBClient) *USBDevice {
	return &USBDevice{
		serial: serial,
		adb:    adb,
	}
}

func (d *USBDevice) Kind() device.Kind          { return device.AndroidUSB }
func (d *USBDevice) Serial() string                { return d.serial }
func (d *USBDevice) CanBoot() bool                 { return false }
func (d *USBDevice) CanSnapshot() bool             { return false }
func (d *USBDevice) CanDestroy() bool              { return false }

func (d *USBDevice) DisplayName() string {
	if d.model != "" {
		return fmt.Sprintf("%s (%s)", d.model, d.serial)
	}
	return d.serial
}

func (d *USBDevice) GetConnectionInfo() device.ConnectionInfo {
	return device.ConnectionInfo{
		DeviceKind: string(device.AndroidUSB),
		ADBSerial:  d.serial,
	}
}

// Prepare verifies the device is online and reads its model name.
func (d *USBDevice) Prepare(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Verify device is online
	if err := d.adb.WaitForDevice(ctx, d.serial, 5*time.Second); err != nil {
		return fmt.Errorf("usb device %s not online: %w", d.serial, err)
	}

	// Read model name
	model, err := d.adb.GetProp(ctx, d.serial, "ro.product.model")
	if err == nil && model != "" {
		d.model = strings.TrimSpace(model)
	}

	return nil
}

// Reset is a no-op for USB devices — don't touch the user's phone.
func (d *USBDevice) Reset(ctx context.Context) error {
	// No-op by default. User's phone, user's data.
	return nil
}

// Shutdown is a no-op — you can't kill someone's phone.
func (d *USBDevice) Shutdown(ctx context.Context) error {
	return nil
}

// HealthCheck verifies the device responds to ADB.
func (d *USBDevice) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := d.adb.Shell(ctx, d.serial, "echo ok")
	if err != nil {
		return fmt.Errorf("usb health: %w", err)
	}
	return nil
}
