package health

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type mockProbe struct {
	name    string
	healthy bool
}

func (p *mockProbe) Name() string { return p.name }
func (p *mockProbe) Check(_ context.Context, _ string) error {
	if p.healthy {
		return nil
	}
	return fmt.Errorf("mock probe %s: unhealthy", p.name)
}

func TestCheckerRegistration(t *testing.T) {
	checker := NewChecker(nil, 15*time.Second, 3, nil)

	inst := &pool.EmulatorInstance{
		ID:     "test-1",
		Serial: "emulator-5554",
	}

	checker.Register(inst)
	if len(checker.instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(checker.instances))
	}

	checker.Unregister("test-1")
	if len(checker.instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(checker.instances))
	}
}

func TestCheckerCallsOnUnhealthy(t *testing.T) {
	unhealthyCalled := make(chan string, 1)

	probe := &mockProbe{name: "test", healthy: false}
	checker := NewChecker(
		[]Probe{probe},
		100*time.Millisecond,
		2,
		func(instanceID string) {
			unhealthyCalled <- instanceID
		},
	)

	inst := &pool.EmulatorInstance{
		ID:     "test-1",
		Serial: "emulator-5554",
		State:  pool.StateWarm,
	}
	checker.Register(inst)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually run checks to trigger threshold
	checker.checkInstance(ctx, inst)
	checker.checkInstance(ctx, inst)

	select {
	case id := <-unhealthyCalled:
		if id != "test-1" {
			t.Errorf("expected 'test-1', got '%s'", id)
		}
	case <-time.After(time.Second):
		t.Fatal("expected onUnhealthy to be called")
	}
}

func TestCheckerHealthyInstance(t *testing.T) {
	probe := &mockProbe{name: "test", healthy: true}
	checker := NewChecker([]Probe{probe}, 100*time.Millisecond, 3, nil)

	inst := &pool.EmulatorInstance{
		ID:     "test-1",
		Serial: "emulator-5554",
		State:  pool.StateWarm,
	}
	checker.Register(inst)

	checker.checkInstance(context.Background(), inst)
	if !inst.IsHealthy() {
		t.Error("expected instance to be healthy")
	}
}
