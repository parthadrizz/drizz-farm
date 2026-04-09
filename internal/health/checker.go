package health

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/pool"
)

// OnUnhealthy is called when an instance exceeds the unhealthy threshold.
type OnUnhealthy func(instanceID string)

// Checker runs periodic health checks on emulator instances.
type Checker struct {
	mu sync.Mutex

	probes    []Probe
	interval  time.Duration
	threshold int
	onUnhealthy OnUnhealthy

	instances map[string]*pool.EmulatorInstance
	cancel    context.CancelFunc
}

// NewChecker creates a health checker.
func NewChecker(probes []Probe, interval time.Duration, threshold int, onUnhealthy OnUnhealthy) *Checker {
	return &Checker{
		probes:      probes,
		interval:    interval,
		threshold:   threshold,
		onUnhealthy: onUnhealthy,
		instances:   make(map[string]*pool.EmulatorInstance),
	}
}

// Start begins the health check loop.
func (c *Checker) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	go c.loop(ctx)
}

// Stop cancels the health check loop.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// Register adds an instance to be monitored.
func (c *Checker) Register(inst *pool.EmulatorInstance) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instances[inst.ID] = inst
	log.Debug().Str("instance", inst.ID).Str("serial", inst.Serial).Msg("health: registered instance")
}

// Unregister removes an instance from monitoring.
func (c *Checker) Unregister(instanceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.instances, instanceID)
	log.Debug().Str("instance", instanceID).Msg("health: unregistered instance")
}

func (c *Checker) loop(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkAll(ctx)
		}
	}
}

func (c *Checker) checkAll(ctx context.Context) {
	c.mu.Lock()
	// Snapshot the instances to check
	toCheck := make([]*pool.EmulatorInstance, 0, len(c.instances))
	for _, inst := range c.instances {
		state := inst.GetState()
		// Only check warm and allocated instances
		if state == pool.StateWarm || state == pool.StateAllocated {
			toCheck = append(toCheck, inst)
		}
	}
	c.mu.Unlock()

	for _, inst := range toCheck {
		c.checkInstance(ctx, inst)
	}
}

func (c *Checker) checkInstance(ctx context.Context, inst *pool.EmulatorInstance) {
	healthy := true
	for _, probe := range c.probes {
		if err := probe.Check(ctx, inst.Serial); err != nil {
			log.Warn().
				Err(err).
				Str("instance", inst.ID).
				Str("probe", probe.Name()).
				Msg("health: probe failed")
			healthy = false
			break
		}
	}

	inst.RecordHealthCheck(healthy)

	if !healthy && inst.HealthFails >= c.threshold {
		log.Error().
			Str("instance", inst.ID).
			Int("consecutive_failures", inst.HealthFails).
			Int("threshold", c.threshold).
			Msg("health: instance exceeded unhealthy threshold")

		if c.onUnhealthy != nil {
			c.onUnhealthy(inst.ID)
		}
	}
}
