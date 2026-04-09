package session

import (
	"fmt"
	"time"
)

// SessionState represents the lifecycle of a session.
type SessionState int

const (
	SessionQueued   SessionState = iota // Waiting for an emulator
	SessionActive                       // Emulator allocated, in use
	SessionReleased                     // Explicitly released by user
	SessionTimedOut                     // Auto-released due to timeout
	SessionErrored                      // Released due to error
)

var stateNames = map[SessionState]string{
	SessionQueued:   "queued",
	SessionActive:   "active",
	SessionReleased: "released",
	SessionTimedOut: "timed_out",
	SessionErrored:  "errored",
}

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
	Profile     string       `json:"profile"`
	Platform    string       `json:"platform"` // "android" or "ios"
	InstanceID  string       `json:"instance_id"`
	State       SessionState `json:"state"`
	Connection  ConnectionInfo `json:"connection"`
	ClientID    string       `json:"client_id,omitempty"`
	ClientName  string       `json:"client_name,omitempty"`
	Source      string       `json:"source,omitempty"` // "cli", "api", "mcp", "dashboard"
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	ReleasedAt  *time.Time   `json:"released_at,omitempty"`
}

// ConnectionInfo holds the connection details for a session.
type ConnectionInfo struct {
	Host        string `json:"host"`
	ADBPort     int    `json:"adb_port"`
	ADBSerial   string `json:"adb_serial"`
	ConsolePort int    `json:"console_port"`
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
}
