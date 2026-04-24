package session

import (
	"fmt"
	"time"
)

// SessionState represents the lifecycle of a session.
type SessionState int

const (
	SessionQueued      SessionState = iota // Waiting for an emulator
	SessionActive                          // Emulator allocated, in use
	SessionReleased                        // Explicitly released by user
	SessionTimedOut                        // Auto-released due to timeout
	SessionErrored                         // Released due to error
	SessionInterrupted                     // Underlying emulator disappeared mid-session
)

var stateNames = map[SessionState]string{
	SessionQueued:      "queued",
	SessionActive:      "active",
	SessionReleased:    "released",
	SessionTimedOut:    "timed_out",
	SessionErrored:     "errored",
	SessionInterrupted: "interrupted",
}

// String returns the lowercase string representation of a SessionState.
func (s SessionState) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return "unknown"
}

// MarshalJSON implements json.Marshaler.
func (s SessionState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SessionState) UnmarshalJSON(data []byte) error {
	str := string(data)
	if len(str) >= 2 && str[0] == '"' {
		str = str[1 : len(str)-1]
	}
	for state, name := range stateNames {
		if name == str {
			*s = state
			return nil
		}
	}
	return fmt.Errorf("unknown session state: %s", str)
}

// Session represents a user's active session with an emulator.
type Session struct {
	ID          string       `json:"id"`
	NodeName    string       `json:"node_name"`
	Profile     string       `json:"profile"`
	Platform    string       `json:"platform"` // "android" or "ios"
	InstanceID  string       `json:"instance_id"`
	// DeviceName is the human-readable AVD / device name (e.g.
	// "Pixel_7_API_34"), captured at allocation time. Sessions list
	// shows this instead of the opaque emulator-5554 serial so users
	// can tell their runs apart at a glance.
	DeviceName  string       `json:"device_name,omitempty"`
	State       SessionState `json:"state"`
	Connection  ConnectionInfo `json:"connection"`
	ClientID    string       `json:"client_id,omitempty"`
	ClientName  string       `json:"client_name,omitempty"`
	Source      string       `json:"source,omitempty"` // "cli", "api", "mcp", "dashboard"
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	ReleasedAt  *time.Time   `json:"released_at,omitempty"`

	// Capture configuration — what the session wants recorded. Set at
	// create time via CreateSessionRequest.Capabilities. The broker
	// auto-starts the corresponding capture subsystems on activation
	// and tears them down on release.
	Capabilities *SessionCapabilities `json:"capabilities,omitempty"`
}

// ConnectionInfo holds the connection details for a session.
type ConnectionInfo struct {
	Host         string `json:"host"`
	NodeName     string `json:"node_name,omitempty"`
	DeviceKind   string `json:"device_kind,omitempty"`
	ADBPort      int    `json:"adb_port,omitempty"`
	ADBSerial    string `json:"adb_serial,omitempty"`
	ConsolePort  int    `json:"console_port,omitempty"`
	AppiumURL    string `json:"appium_url,omitempty"` // WebDriver endpoint
	UDID         string `json:"udid,omitempty"`       // iOS future
}

// IsActive returns true if the session is still in use.
func (s *Session) IsActive() bool {
	return s.State == SessionActive || s.State == SessionQueued
}

// IsExpired returns true if the session has passed its expiry time.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// CreateSessionRequest is the input for creating a new session.
type CreateSessionRequest struct {
	Platform   string            `json:"platform"`
	Profile    string            `json:"profile"`
	ClientID   string            `json:"client_id,omitempty"`
	ClientName string            `json:"client_name,omitempty"`
	Source     string            `json:"source,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	TimeoutMin int               `json:"timeout_minutes,omitempty"`

	// Specific-device allocation. If DeviceID is set, the broker asks
	// the pool for exactly that instance — 409 if it's already in use,
	// 403 if it's reserved and the caller isn't a manual/dashboard
	// source. AVDName is the fallback by-name lookup. Profile is
	// ignored when either of these is set (the device already has a
	// profile).
	DeviceID string `json:"device_id,omitempty"`
	AVDName  string `json:"avd_name,omitempty"`

	// Declarative capture settings. The broker auto-starts the
	// corresponding subsystems on session activation and tears them
	// down on release. Screenshot capture is on-demand via the
	// per-session endpoint but gated on CaptureScreenshots=true.
	Capabilities *SessionCapabilities `json:"capabilities,omitempty"`
}

// SessionCapabilities describes what the session wants captured. Zero
// values mean "don't capture." RetentionHours is best-effort and read
// by a future cleanup job; 0 means "use daemon default."
type SessionCapabilities struct {
	RecordVideo        bool `json:"record_video,omitempty"`
	CaptureLogcat      bool `json:"capture_logcat,omitempty"`
	CaptureScreenshots bool `json:"capture_screenshots,omitempty"`
	CaptureNetwork     bool `json:"capture_network,omitempty"` // reserved for mitmproxy integration
	RetentionHours     int  `json:"retention_hours,omitempty"`
}
