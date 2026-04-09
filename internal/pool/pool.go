package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
)

var (
	ErrPoolExhausted = errors.New("pool exhausted: no emulators available")
	ErrProfileNotFound = errors.New("profile not found")
	ErrInstanceNotFound = errors.New("instance not found")
)

// PoolStatus is a snapshot of pool state for API responses.
type PoolStatus struct {
	TotalCapacity int                `json:"total_capacity"`
	Warm          int                `json:"warm"`
	Allocated     int                `json:"allocated"`
	Booting       int                `json:"booting"`
	Resetting     int                `json:"resetting"`
	Error         int                `json:"error"`
	Instances     []InstanceSnapshot `json:"instances"`
}

// Pool manages the emulator fleet.
type Pool struct {
	mu sync.RWMutex

	cfg       *config.Config
	instances map[string]*EmulatorInstance // keyed by instance ID

	sdk       *android.SDK
	adb       *android.ADBClient
	avdMgr    *android.AVDManager
	emuCtrl   *android.EmulatorController
	portAlloc *android.PortAllocator
	runner    android.CommandRunner

	cancel context.CancelFunc
}

// New creates a new Pool.
func New(cfg *config.Config, sdk *android.SDK, runner android.CommandRunner) *Pool {
	adb := android.NewADBClient(sdk, runner)
	return &Pool{
		cfg:       cfg,
		instances: make(map[string]*EmulatorInstance),
		sdk:       sdk,
		adb:       adb,
		avdMgr:    android.NewAVDManager(sdk, runner),
		emuCtrl:   android.NewEmulatorController(sdk, runner),
		portAlloc: android.NewPortAllocator(cfg.Pool.PortRangeMin, cfg.Pool.PortRangeMax),
		runner:    runner,
	}
}

// Start initializes the pool and warms up emulators based on config.
func (p *Pool) Start(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)

	log.Info().
		Int("max_concurrent", p.cfg.Pool.MaxConcurrent).
		Int("warmup_profiles", len(p.cfg.Pool.Warmup)).
		Msg("pool: starting")

	// Warm up emulators from config
	for _, w := range p.cfg.Pool.Warmup {
		for i := 0; i < w.Count; i++ {
			if err := p.warmOne(ctx, w.Profile, i); err != nil {
				log.Error().Err(err).Str("profile", w.Profile).Int("index", i).Msg("pool: failed to warm emulator")
				// Don't fail startup for individual emulator failures
			}
		}
	}

	// Start maintenance loop
	go p.maintenanceLoop(ctx)

	log.Info().Int("instances", len(p.instances)).Msg("pool: started")
	return nil
}

// Stop gracefully shuts down all emulators.
func (p *Pool) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}

	p.mu.Lock()
	instances := make([]*EmulatorInstance, 0, len(p.instances))
	for _, inst := range p.instances {
		instances = append(instances, inst)
	}
	p.mu.Unlock()

	log.Info().Int("instances", len(instances)).Msg("pool: stopping all emulators")

	var errs []error
	for _, inst := range instances {
		if err := p.destroyInstance(inst); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("pool: %d errors during shutdown", len(errs))
	}
	return nil
}

// Allocate finds a warm emulator matching the profile and transitions it to allocated.
func (p *Pool) Allocate(ctx context.Context, profileName string) (*EmulatorInstance, error) {
	// Validate profile exists
	if _, ok := p.cfg.Pool.Profiles.Android[profileName]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrProfileNotFound, profileName)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Find a warm instance matching the profile
	for _, inst := range p.instances {
		if inst.GetState() == StateWarm && inst.ProfileName == profileName {
			if err := inst.TransitionTo(StateAllocated); err != nil {
				continue
			}
			log.Info().
				Str("instance", inst.ID).
				Str("profile", profileName).
				Str("serial", inst.Serial).
				Msg("pool: allocated emulator")
			return inst, nil
		}
	}

	// No warm instance found — try to create one on-demand if under capacity
	if len(p.instances) < p.cfg.Pool.MaxConcurrent {
		p.mu.Unlock()
		inst, err := p.createAndBoot(ctx, profileName, len(p.instances))
		p.mu.Lock()
		if err != nil {
			return nil, fmt.Errorf("pool: on-demand create failed: %w", err)
		}
		if err := inst.TransitionTo(StateAllocated); err != nil {
			return nil, err
		}
		return inst, nil
	}

	return nil, ErrPoolExhausted
}

// Release returns an emulator to the pool after use.
func (p *Pool) Release(ctx context.Context, instanceID string) error {
	p.mu.RLock()
	inst, ok := p.instances[instanceID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrInstanceNotFound, instanceID)
	}

	log.Info().Str("instance", instanceID).Msg("pool: releasing emulator")

	if err := inst.TransitionTo(StateResetting); err != nil {
		return err
	}
	inst.ClearSession()

	// Reset in background
	go func() {
		if err := p.resetInstance(ctx, inst); err != nil {
			log.Error().Err(err).Str("instance", instanceID).Msg("pool: reset failed")
			_ = inst.TransitionTo(StateError)
			return
		}
		if err := inst.TransitionTo(StateWarm); err != nil {
			log.Error().Err(err).Str("instance", instanceID).Msg("pool: transition to warm failed")
		}
	}()

	return nil
}

// Status returns a snapshot of pool state.
func (p *Pool) Status() PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := PoolStatus{
		TotalCapacity: p.cfg.Pool.MaxConcurrent,
		Instances:     make([]InstanceSnapshot, 0, len(p.instances)),
	}

	for _, inst := range p.instances {
		snap := inst.Snapshot()
		status.Instances = append(status.Instances, snap)
		switch snap.State {
		case StateWarm:
			status.Warm++
		case StateAllocated:
			status.Allocated++
		case StateBooting:
			status.Booting++
		case StateResetting:
			status.Resetting++
		case StateError:
			status.Error++
		}
	}

	return status
}

// Available returns count of warm emulators for a given profile (empty = any).
func (p *Pool) Available(profileName string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, inst := range p.instances {
		if inst.GetState() == StateWarm {
			if profileName == "" || inst.ProfileName == profileName {
				count++
			}
		}
	}
	return count
}

// GetInstance returns an instance by ID.
func (p *Pool) GetInstance(id string) (*EmulatorInstance, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	inst, ok := p.instances[id]
	return inst, ok
}

// warmOne creates and boots a single emulator for the warmup pool.
func (p *Pool) warmOne(ctx context.Context, profileName string, index int) error {
	inst, err := p.createAndBoot(ctx, profileName, index)
	if err != nil {
		return err
	}
	_ = inst // Already registered in p.instances by createAndBoot
	return nil
}

// createAndBoot creates an AVD, boots it, saves a clean snapshot, and registers it.
func (p *Pool) createAndBoot(ctx context.Context, profileName string, index int) (*EmulatorInstance, error) {
	profile, ok := p.cfg.Pool.Profiles.Android[profileName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProfileNotFound, profileName)
	}

	id := uuid.New().String()[:8]
	avdName := android.AVDName(profileName, index)

	inst := &EmulatorInstance{
		ID:          id,
		AVDName:     avdName,
		ProfileName: profileName,
		State:       StateCreating,
		CreatedAt:   time.Now(),
	}

	// Register instance early so pool tracking is accurate
	p.mu.Lock()
	p.instances[id] = inst
	p.mu.Unlock()

	// Create AVD (idempotent — uses --force)
	log.Info().Str("avd", avdName).Str("profile", profileName).Msg("pool: creating AVD")
	if err := p.avdMgr.Create(ctx, avdName, profile); err != nil {
		p.removeInstance(id)
		return nil, fmt.Errorf("create AVD %s: %w", avdName, err)
	}

	// Allocate ports
	ports, err := p.portAlloc.Allocate()
	if err != nil {
		p.removeInstance(id)
		return nil, fmt.Errorf("allocate ports: %w", err)
	}
	inst.Ports = ports
	inst.Serial = fmt.Sprintf("emulator-%d", ports.Console)

	// Transition to booting
	if err := inst.TransitionTo(StateBooting); err != nil {
		p.portAlloc.Release(ports)
		p.removeInstance(id)
		return nil, err
	}

	// Boot emulator
	bootOpts := android.BootOptions{
		Profile:  profile,
		Ports:    ports,
		NoWindow: true,
		NoAudio:  true,
	}

	// Check if clean snapshot exists (for fast reboot)
	// First boot: no snapshot → full boot → save snapshot
	// Subsequent: snapshot boot
	proc, err := p.emuCtrl.Boot(ctx, avdName, bootOpts)
	if err != nil {
		p.portAlloc.Release(ports)
		p.removeInstance(id)
		return nil, fmt.Errorf("boot emulator %s: %w", avdName, err)
	}
	inst.Process = proc

	// Wait for boot and save clean snapshot
	bootTimeout := time.Duration(profile.BootTimeoutSeconds) * time.Second
	if bootTimeout == 0 {
		bootTimeout = 120 * time.Second
	}

	if err := p.emuCtrl.SaveCleanSnapshot(ctx, p.adb, inst.Serial, bootTimeout); err != nil {
		log.Warn().Err(err).Str("instance", id).Msg("pool: failed to save clean snapshot (continuing)")
	}

	// Transition to warm
	if err := inst.TransitionTo(StateWarm); err != nil {
		return nil, err
	}
	inst.RecordHealthCheck(true)

	log.Info().
		Str("instance", id).
		Str("serial", inst.Serial).
		Str("profile", profileName).
		Msg("pool: emulator warm and ready")

	return inst, nil
}

// resetInstance restores the clean snapshot on an emulator.
func (p *Pool) resetInstance(ctx context.Context, inst *EmulatorInstance) error {
	log.Info().Str("instance", inst.ID).Msg("pool: resetting emulator via snapshot restore")
	return p.emuCtrl.SnapshotLoad(ctx, p.adb, inst.Serial, "drizz_clean")
}

// destroyInstance kills the emulator and cleans up.
func (p *Pool) destroyInstance(inst *EmulatorInstance) error {
	_ = inst.TransitionTo(StateDestroying)

	if inst.Process != nil {
		if err := p.emuCtrl.Kill(inst.Process); err != nil {
			log.Error().Err(err).Str("instance", inst.ID).Msg("pool: failed to kill emulator")
		}
	}

	p.portAlloc.Release(inst.Ports)
	p.removeInstance(inst.ID)

	log.Info().Str("instance", inst.ID).Str("avd", inst.AVDName).Msg("pool: instance destroyed")
	return nil
}

func (p *Pool) removeInstance(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.instances, id)
}

// maintenanceLoop runs periodic pool maintenance.
func (p *Pool) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runMaintenance(ctx)
		}
	}
}

// runMaintenance handles error recovery and warm pool replenishment.
func (p *Pool) runMaintenance(ctx context.Context) {
	p.mu.RLock()
	var errored []*EmulatorInstance
	for _, inst := range p.instances {
		if inst.GetState() == StateError {
			errored = append(errored, inst)
		}
	}
	p.mu.RUnlock()

	// Destroy errored instances
	for _, inst := range errored {
		log.Warn().Str("instance", inst.ID).Msg("pool: destroying errored instance")
		_ = p.destroyInstance(inst)
	}

	// Replenish warm pool if below target
	for _, w := range p.cfg.Pool.Warmup {
		current := p.countByProfileAndState(w.Profile, StateWarm) +
			p.countByProfileAndState(w.Profile, StateAllocated) +
			p.countByProfileAndState(w.Profile, StateBooting) +
			p.countByProfileAndState(w.Profile, StateResetting)

		for current < w.Count && p.totalInstances() < p.cfg.Pool.MaxConcurrent {
			log.Info().Str("profile", w.Profile).Int("current", current).Int("target", w.Count).
				Msg("pool: replenishing warm pool")
			if err := p.warmOne(ctx, w.Profile, current); err != nil {
				log.Error().Err(err).Str("profile", w.Profile).Msg("pool: replenish failed")
				break
			}
			current++
		}
	}
}

func (p *Pool) countByProfileAndState(profile string, state EmulatorState) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, inst := range p.instances {
		if inst.ProfileName == profile && inst.GetState() == state {
			count++
		}
	}
	return count
}

func (p *Pool) totalInstances() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.instances)
}
