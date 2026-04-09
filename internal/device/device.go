// Package device defines the shared interfaces and types for all device types
// in the drizz-farm pool. Both the pool and concrete device implementations
// (android emulators, USB phones, iOS simulators) import this package.
package device

import "context"

// Kind identifies the type of device in the pool.
type Kind string

const (
	AndroidEmulator Kind = "android_emulator"
	AndroidUSB      Kind = "android_usb"
	IOSSimulator    Kind = "ios_simulator"
	IOSUSB          Kind = "ios_usb"
)

// ConnectionInfo holds connection details for a device.
// Fields are populated based on device type — unused fields are omitted in JSON.
type ConnectionInfo struct {
	Host        string `json:"host,omitempty"`
	DeviceKind  string `json:"device_kind,omitempty"`
	ADBPort     int    `json:"adb_port,omitempty"`
	ADBSerial   string `json:"adb_serial,omitempty"`
	ConsolePort int    `json:"console_port,omitempty"`
	UDID        string `json:"udid,omitempty"`
}

// Device is the abstraction for any device in the pool.
type Device interface {
	Kind() Kind
	Serial() string
	DisplayName() string

	Prepare(ctx context.Context) error
	Reset(ctx context.Context) error
	Shutdown(ctx context.Context) error
	HealthCheck(ctx context.Context) error

	GetConnectionInfo() ConnectionInfo

	CanBoot() bool
	CanSnapshot() bool
	CanDestroy() bool
}

// Scanner discovers devices of a specific kind.
type Scanner interface {
	Scan(ctx context.Context) ([]Device, error)
}
