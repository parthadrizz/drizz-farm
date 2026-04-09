package pool

import (
	"testing"
	"time"
)

func TestEmulatorStateTransitions(t *testing.T) {
	tests := []struct {
		from    EmulatorState
		to      EmulatorState
		allowed bool
	}{
		{StateCreating, StateBooting, true},
		{StateCreating, StateError, true},
		{StateCreating, StateWarm, false},
		{StateBooting, StateWarm, true},
		{StateBooting, StateError, true},
		{StateBooting, StateAllocated, false},
		{StateWarm, StateAllocated, true},
		{StateWarm, StateDestroying, true},
		{StateWarm, StateBooting, false},
		{StateAllocated, StateResetting, true},
		{StateAllocated, StateError, true},
		{StateAllocated, StateWarm, false},
		{StateResetting, StateWarm, true},
		{StateResetting, StateError, true},
		{StateError, StateDestroying, true},
		{StateError, StateBooting, true},
		{StateDestroying, StateWarm, false},
		{StateDestroying, StateBooting, false},
	}

	for _, tt := range tests {
		result := tt.from.CanTransitionTo(tt.to)
		if result != tt.allowed {
			t.Errorf("%s → %s: expected allowed=%v, got %v", tt.from, tt.to, tt.allowed, result)
		}
	}
}

func TestEmulatorStateString(t *testing.T) {
	if StateWarm.String() != "warm" {
		t.Errorf("expected 'warm', got '%s'", StateWarm.String())
	}
	if StateAllocated.String() != "allocated" {
		t.Errorf("expected 'allocated', got '%s'", StateAllocated.String())
	}
}

func TestInstanceTransition(t *testing.T) {
	inst := &EmulatorInstance{
		ID:    "test-1",
		State: StateWarm,
	}

	// Valid transition
	if err := inst.TransitionTo(StateAllocated); err != nil {
		t.Fatalf("expected valid transition, got error: %v", err)
	}
	if inst.GetState() != StateAllocated {
		t.Errorf("expected allocated, got %s", inst.GetState())
	}

	// Invalid transition
	if err := inst.TransitionTo(StateWarm); err == nil {
		t.Fatal("expected error for invalid transition allocated→warm")
	}
}

func TestInstanceSessionAssignment(t *testing.T) {
	inst := &EmulatorInstance{
		ID:    "test-1",
		State: StateAllocated,
	}

	inst.SetSession("session-abc")
	if inst.SessionID != "session-abc" {
		t.Errorf("expected 'session-abc', got '%s'", inst.SessionID)
	}
	if inst.AllocatedAt == nil {
		t.Error("expected AllocatedAt to be set")
	}

	inst.ClearSession()
	if inst.SessionID != "" {
		t.Errorf("expected empty session, got '%s'", inst.SessionID)
	}
	if inst.AllocatedAt != nil {
		t.Error("expected AllocatedAt to be nil")
	}
}

func TestInstanceHealthTracking(t *testing.T) {
	inst := &EmulatorInstance{ID: "test-1"}

	inst.RecordHealthCheck(true)
	if !inst.IsHealthy() {
		t.Error("expected healthy after positive check")
	}
	if inst.LastHealthy.IsZero() {
		t.Error("expected LastHealthy to be set")
	}

	inst.RecordHealthCheck(false)
	inst.RecordHealthCheck(false)
	if inst.IsHealthy() {
		t.Error("expected unhealthy after 2 negative checks")
	}
	if inst.HealthFails != 2 {
		t.Errorf("expected 2 health fails, got %d", inst.HealthFails)
	}

	inst.RecordHealthCheck(true)
	if !inst.IsHealthy() {
		t.Error("expected healthy after positive check resets counter")
	}
	if inst.HealthFails != 0 {
		t.Errorf("expected 0 health fails after reset, got %d", inst.HealthFails)
	}
}

func TestInstanceSnapshot(t *testing.T) {
	now := time.Now()
	inst := &EmulatorInstance{
		ID:          "test-1",
		AVDName:     "drizz_pixel_7_0",
		ProfileName: "pixel_7_api34",
		State:       StateWarm,
		Serial:      "emulator-5554",
		CreatedAt:   now,
	}

	snap := inst.Snapshot()
	if snap.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got '%s'", snap.ID)
	}
	if snap.State != StateWarm {
		t.Errorf("expected state warm, got %s", snap.State)
	}
	if snap.ProfileName != "pixel_7_api34" {
		t.Errorf("expected profile 'pixel_7_api34', got '%s'", snap.ProfileName)
	}
}
