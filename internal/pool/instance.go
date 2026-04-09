package pool

import (
	"sync"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/android"
)

// EmulatorInstance tracks the full state of one emulator in the pool.
type EmulatorInstance struct {
	mu sync.RWMutex

	ID          string            `json:"id"`
	AVDName     string            `json:"avd_name"`
	ProfileName string            `json:"profile"`
	State       EmulatorState     `json:"state"`
	Ports       android.PortPair  `json:"ports"`
	Serial      string            `json:"serial"` // e.g., "emulator-5554"
	SessionID   string            `json:"session_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	AllocatedAt *time.Time        `json:"allocated_at,omitempty"`
	LastHealthy time.Time         `json:"last_healthy"`
	HealthFails int               `json:"health_fails"`
	Process     *android.EmulatorProcess `json:"-"`
}

// TransitionTo attempts a state transition. Returns an error if the transition is invalid.
func (e *EmulatorInstance) TransitionTo(target EmulatorState) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.State.CanTransitionTo(target) {
		return &InvalidTransitionError{From: e.State, To: target, InstanceID: e.ID}
	}
	e.State = target
	return nil
}

// GetState returns the current state (thread-safe).
func (e *EmulatorInstance) GetState() EmulatorState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.State
}

// SetSession assigns a session to this instance.
func (e *EmulatorInstance) SetSession(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.SessionID = sessionID
	now := time.Now()
	e.AllocatedAt = &now
}

// ClearSession removes session assignment.
func (e *EmulatorInstance) ClearSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.SessionID = ""
	e.AllocatedAt = nil
}

// RecordHealthCheck records a health check result.
func (e *EmulatorInstance) RecordHealthCheck(healthy bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if healthy {
		e.HealthFails = 0
		e.LastHealthy = time.Now()
	} else {
		e.HealthFails++
	}
}

// IsHealthy returns true if the instance is considered healthy.
func (e *EmulatorInstance) IsHealthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.HealthFails == 0
}

// Snapshot returns a read-only copy of the instance state for API responses.
func (e *EmulatorInstance) Snapshot() InstanceSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return InstanceSnapshot{
		ID:          e.ID,
		AVDName:     e.AVDName,
		ProfileName: e.ProfileName,
		State:       e.State,
		Serial:      e.Serial,
		ConsolePort: e.Ports.Console,
		ADBPort:     e.Ports.ADB,
		SessionID:   e.SessionID,
		CreatedAt:   e.CreatedAt,
		AllocatedAt: e.AllocatedAt,
		LastHealthy: e.LastHealthy,
		HealthFails: e.HealthFails,
	}
}

// InstanceSnapshot is a read-only copy of instance state, safe for JSON serialization.
type InstanceSnapshot struct {
	ID          string        `json:"id"`
	AVDName     string        `json:"avd_name"`
	ProfileName string        `json:"profile"`
	State       EmulatorState `json:"state"`
	Serial      string        `json:"serial"`
	ConsolePort int           `json:"console_port"`
	ADBPort     int           `json:"adb_port"`
	SessionID   string        `json:"session_id,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	AllocatedAt *time.Time    `json:"allocated_at,omitempty"`
	LastHealthy time.Time     `json:"last_healthy"`
	HealthFails int           `json:"health_fails"`
}

// InvalidTransitionError indicates an invalid state transition attempt.
type InvalidTransitionError struct {
	From       EmulatorState
	To         EmulatorState
	InstanceID string
}

func (e *InvalidTransitionError) Error() string {
	return "invalid state transition for instance " + e.InstanceID + ": " + e.From.String() + " → " + e.To.String()
}
