package pool

import (
	"sync"
	"time"
)

// DeviceInstance tracks the pool state of any device (emulator or physical).
type DeviceInstance struct {
	mu sync.RWMutex

	ID           string      `json:"id"`
	ProfileName  string      `json:"profile"`
	State        DeviceState `json:"state"`
	SessionID    string      `json:"session_id,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	AllocatedAt  *time.Time  `json:"allocated_at,omitempty"`
	LastActivity time.Time   `json:"last_activity"`
	LastHealthy  time.Time   `json:"last_healthy"`
	HealthFails  int         `json:"health_fails"`

	Device Device `json:"-"` // the actual device — not serialized
}

// TransitionTo attempts a state transition. Returns an error if invalid.
func (d *DeviceInstance) TransitionTo(target DeviceState) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.State.CanTransitionTo(target) {
		return &InvalidTransitionError{From: d.State, To: target, InstanceID: d.ID}
	}
	d.State = target
	return nil
}

// GetState returns the current state (thread-safe).
func (d *DeviceInstance) GetState() DeviceState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State
}

// SetSession assigns a session to this instance.
func (d *DeviceInstance) SetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.SessionID = sessionID
	now := time.Now()
	d.AllocatedAt = &now
	d.LastActivity = now
}

// ClearSession removes session assignment.
func (d *DeviceInstance) ClearSession() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.SessionID = ""
	d.AllocatedAt = nil
}

// TouchActivity updates the last activity timestamp.
func (d *DeviceInstance) TouchActivity() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.LastActivity = time.Now()
}

// IdleSince returns how long since last activity.
func (d *DeviceInstance) IdleSince() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.LastActivity.IsZero() {
		return time.Since(d.CreatedAt)
	}
	return time.Since(d.LastActivity)
}

// RecordHealthCheck records a health check result.
func (d *DeviceInstance) RecordHealthCheck(healthy bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if healthy {
		d.HealthFails = 0
		d.LastHealthy = time.Now()
	} else {
		d.HealthFails++
	}
}

// IsHealthy returns true if the instance is considered healthy.
func (d *DeviceInstance) IsHealthy() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.HealthFails == 0
}

// Snapshot returns a read-only copy for API responses.
func (d *DeviceInstance) Snapshot() InstanceSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap := InstanceSnapshot{
		ID:           d.ID,
		ProfileName:  d.ProfileName,
		State:        d.State,
		SessionID:    d.SessionID,
		CreatedAt:    d.CreatedAt,
		AllocatedAt:  d.AllocatedAt,
		LastActivity: d.LastActivity,
		LastHealthy:  d.LastHealthy,
		HealthFails:  d.HealthFails,
	}

	if d.Device != nil {
		snap.DeviceKind = d.Device.Kind()
		snap.DeviceName = d.Device.DisplayName()
		snap.Serial = d.Device.Serial()
		snap.Connection = d.Device.GetConnectionInfo()
	}

	return snap
}

// InstanceSnapshot is a read-only copy safe for JSON serialization.
type InstanceSnapshot struct {
	ID           string         `json:"id"`
	DeviceKind   DeviceKind     `json:"device_kind"`
	DeviceName   string         `json:"device_name"`
	ProfileName  string         `json:"profile"`
	State        DeviceState    `json:"state"`
	Serial       string         `json:"serial"`
	Connection   ConnectionInfo `json:"connection"`
	SessionID    string         `json:"session_id,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	AllocatedAt  *time.Time     `json:"allocated_at,omitempty"`
	LastActivity time.Time      `json:"last_activity"`
	LastHealthy  time.Time      `json:"last_healthy"`
	HealthFails  int            `json:"health_fails"`
}

// InvalidTransitionError indicates an invalid state transition attempt.
type InvalidTransitionError struct {
	From       DeviceState
	To         DeviceState
	InstanceID string
}

func (e *InvalidTransitionError) Error() string {
	return "invalid state transition for instance " + e.InstanceID + ": " + e.From.String() + " → " + e.To.String()
}
