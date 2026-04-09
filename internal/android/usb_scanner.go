package android

import (
	"context"
	"strings"

	"github.com/drizz-dev/drizz-farm/internal/device"
	"github.com/rs/zerolog/log"
)

// USBScanner discovers USB-connected Android devices via `adb devices`.
type USBScanner struct {
	adb *ADBClient
}

// NewUSBScanner creates a new USB device scanner.
func NewUSBScanner(adb *ADBClient) *USBScanner {
	return &USBScanner{adb: adb}
}

// Scan returns all USB-connected Android devices (excludes emulators).
func (s *USBScanner) Scan(ctx context.Context) ([]device.Device, error) {
	devices, err := s.adb.Devices(ctx)
	if err != nil {
		return nil, err
	}

	var result []device.Device
	for _, d := range devices {
		// Skip emulators — they have serials like "emulator-5554"
		if strings.HasPrefix(d.Serial, "emulator-") {
			continue
		}
		// Only include fully authorized devices
		if d.State != "device" {
			log.Debug().Str("serial", d.Serial).Str("state", d.State).Msg("usb: skipping non-ready device")
			continue
		}
		result = append(result, NewUSBDevice(d.Serial, s.adb))
	}

	return result, nil
}
