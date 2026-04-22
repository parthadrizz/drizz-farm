package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// DefaultAPIURL is drizz.ai's lead-capture endpoint. Override with
// DRIZZ_API_URL env var for testing or self-hosted deployments.
const DefaultAPIURL = "https://api.drizz.ai/v1"

// HeartbeatDisabled returns true when anonymous activity heartbeats are
// suppressed (DRIZZ_TELEMETRY=off). Signup (lead capture) is NOT affected
// by this — you always register so we know who runs drizz-farm.
func HeartbeatDisabled() bool {
	v := strings.ToLower(os.Getenv("DRIZZ_TELEMETRY"))
	return v == "off" || v == "0" || v == "false" || v == "no"
}

// apiURL returns the base URL for telemetry calls.
func apiURL() string {
	if v := os.Getenv("DRIZZ_API_URL"); v != "" {
		return v
	}
	return DefaultAPIURL
}

// SignupRequest is the payload sent when a new install is set up
// and the user (optionally) provides an email.
type SignupRequest struct {
	InstallID string `json:"install_id"`
	Email     string `json:"email,omitempty"`
	OrgName   string `json:"org_name,omitempty"`
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
}

// HeartbeatRequest is sent periodically so we know which installs are
// active. Contains no PII — linked to a signup via install_id only.
type HeartbeatRequest struct {
	InstallID      string `json:"install_id"`
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	NodeCount      int    `json:"node_count"`      // how many nodes in this node's group (1 if standalone)
	SessionsToday  int    `json:"sessions_today"`  // sessions created in last 24h
	EmulatorsToday int    `json:"emulators_today"` // emulators booted in last 24h
	UptimeSeconds  int64  `json:"uptime_seconds"`
}

// Signup sends a one-shot lead-capture event. Fire-and-forget.
// Always sent (not affected by DRIZZ_TELEMETRY=off) — email is mandatory
// in the setup flow, so by the time we get here we already have consent.
func Signup(ctx context.Context, req SignupRequest) {
	go post(ctx, "/signup", req)
}

// Heartbeat sends a single heartbeat ping. Fire-and-forget.
// Suppressed by DRIZZ_TELEMETRY=off.
func Heartbeat(ctx context.Context, req HeartbeatRequest) {
	if HeartbeatDisabled() {
		return
	}
	go post(ctx, "/heartbeat", req)
}

// post does the actual HTTP call. Short timeout, silent failure.
// Telemetry should NEVER affect the daemon's operation.
func post(ctx context.Context, path string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL()+path, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("drizz-farm/%s (%s/%s)", versionForUA(), runtime.GOOS, runtime.GOARCH))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Log at debug — users shouldn't see noise from telemetry failures.
		log.Debug().Err(err).Str("path", path).Msg("telemetry: call failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Debug().Int("status", resp.StatusCode).Str("path", path).Msg("telemetry: non-2xx response")
	}
}

// versionForUA is set via an import-time helper so the telemetry package
// doesn't force a cyclic import on buildinfo. Call SetVersion() at startup.
var userAgentVersion = "dev"

// SetVersion configures the version string shown in User-Agent headers.
func SetVersion(v string) { userAgentVersion = v }

func versionForUA() string { return userAgentVersion }
