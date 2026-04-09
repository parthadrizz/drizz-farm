package pool

import "github.com/drizz-dev/drizz-farm/internal/device"

// Re-export device types so pool consumers don't need to import device package directly.
type (
	DeviceKind     = device.Kind
	Device         = device.Device
	DeviceScanner  = device.Scanner
	ConnectionInfo = device.ConnectionInfo
)

// Re-export constants.
const (
	DeviceAndroidEmulator = device.AndroidEmulator
	DeviceAndroidUSB      = device.AndroidUSB
	DeviceIOSSimulator    = device.IOSSimulator
	DeviceIOSUSB          = device.IOSUSB
)
