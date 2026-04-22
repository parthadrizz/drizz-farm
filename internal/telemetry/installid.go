// Package telemetry handles optional, opt-out lead capture and anonymous
// heartbeats to api.drizz.ai. Everything here is best-effort — network
// failures never block the daemon.
//
// Privacy model:
//   - install-id is a UUID generated at first setup. No device serial, no MAC.
//   - Email + org name are opt-in (user must type them at setup prompt).
//   - Heartbeat sends install-id, version, OS, node/session counts. Never PII.
//   - Fully disabled with DRIZZ_TELEMETRY=off or telemetry.enabled: false in config.
package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const installIDFileName = "install-id"

// GetOrCreateInstallID reads the install-id from the data dir, or creates
// one if it doesn't exist. The ID persists across daemon restarts so
// heartbeats map to a stable "install" from the server's perspective.
func GetOrCreateInstallID(dataDir string) (string, error) {
	path := filepath.Join(dataDir, installIDFileName)
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return id, nil
		}
	}
	id := uuid.NewString()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(id), 0644); err != nil {
		return "", fmt.Errorf("save install-id: %w", err)
	}
	return id, nil
}
