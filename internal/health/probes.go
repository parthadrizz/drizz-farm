package health

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/android"
)

// Probe checks a specific aspect of emulator health.
type Probe interface {
	Name() string
	Check(ctx context.Context, serial string) error
}

// BootProbe checks sys.boot_completed property.
type BootProbe struct {
	adb *android.ADBClient
}

func NewBootProbe(adb *android.ADBClient) *BootProbe {
	return &BootProbe{adb: adb}
}

func (p *BootProbe) Name() string { return "boot" }

func (p *BootProbe) Check(ctx context.Context, serial string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	val, err := p.adb.GetProp(ctx, serial, "sys.boot_completed")
	if err != nil {
		return fmt.Errorf("boot probe: %w", err)
	}
	if strings.TrimSpace(val) != "1" {
		return fmt.Errorf("boot probe: sys.boot_completed = %q (expected 1)", val)
	}
	return nil
}

// ScreenProbe checks that screencap works (validates display/graphics pipeline).
type ScreenProbe struct {
	adb *android.ADBClient
}

func NewScreenProbe(adb *android.ADBClient) *ScreenProbe {
	return &ScreenProbe{adb: adb}
}

func (p *ScreenProbe) Name() string { return "screen" }

func (p *ScreenProbe) Check(ctx context.Context, serial string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	data, err := p.adb.Screencap(ctx, serial)
	if err != nil {
		return fmt.Errorf("screen probe: %w", err)
	}
	if len(data) < 100 { // PNG should be much larger than 100 bytes
		return fmt.Errorf("screen probe: screenshot too small (%d bytes)", len(data))
	}
	return nil
}
