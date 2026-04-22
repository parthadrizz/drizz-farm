package telemetry

import (
	"context"
	"runtime"
	"time"
)

// HeartbeatCollector is the interface the daemon implements so the
// heartbeat loop can gather current stats without a tight package
// coupling. Pool, broker, and registry all get wrapped in the closure
// that cmd/start.go passes in.
type HeartbeatCollector func() (nodeCount, sessionsToday, emulatorsToday int)

// StartHeartbeatLoop kicks off a background goroutine that pings the
// telemetry API once a day. First beat goes after 5 minutes (so we
// don't flood on rapid daemon restarts during dev). Cancel via ctx.
//
// Fully disabled when DRIZZ_TELEMETRY=off.
func StartHeartbeatLoop(ctx context.Context, installID, version string, collect HeartbeatCollector) {
	if HeartbeatDisabled() {
		return
	}
	startedAt := time.Now()

	go func() {
		// Initial delay — don't fire on startup, wait 5 min.
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
		}

		sendBeat(ctx, installID, version, startedAt, collect)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendBeat(ctx, installID, version, startedAt, collect)
			}
		}
	}()
}

func sendBeat(ctx context.Context, installID, version string, startedAt time.Time, collect HeartbeatCollector) {
	nodes, sessions, emulators := 1, 0, 0
	if collect != nil {
		nodes, sessions, emulators = collect()
	}
	Heartbeat(ctx, HeartbeatRequest{
		InstallID:      installID,
		Version:        version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		NodeCount:      nodes,
		SessionsToday:  sessions,
		EmulatorsToday: emulators,
		UptimeSeconds:  int64(time.Since(startedAt).Seconds()),
	})
}
