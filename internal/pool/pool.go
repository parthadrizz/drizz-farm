package pool

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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

// StartOptions configures which AVDs to boot.
type StartOptions struct {
	// AVDNames is an optional list of specific AVDs to boot.
	// If empty, boots all warmup AVDs from config.
	AVDNames []string
}

// Start initializes the pool and warms up emulators.
// Pass nil options to boot all warmup AVDs from config.
func (p *Pool) Start(ctx context.Context, opts *StartOptions) error {
	ctx, p.cancel = context.WithCancel(ctx)

	// Determine which AVDs to boot
	type bootJob struct {
		avdName     string
		profileName string
	}
	var jobs []bootJob

	if opts != nil && len(opts.AVDNames) > 0 {
		// Boot specific AVDs
		for _, name := range opts.AVDNames {
			profileName := guessProfileForAVD(name, p.cfg)
			jobs = append(jobs, bootJob{avdName: name, profileName: profileName})
		}
	} else {
		// Discover ALL AVDs on the machine
		allAVDs, err := p.avdMgr.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("pool: could not list AVDs, falling back to config warmup")
			// Fallback to config warmup
			for _, w := range p.cfg.Pool.Warmup {
				for i := 0; i < w.Count; i++ {
					jobs = append(jobs, bootJob{
						avdName:     android.AVDName(w.Profile, i),
						profileName: w.Profile,
					})
				}
			}
		} else {
			// Build jobs from all available AVDs
			var candidates []bootJob
			for _, avd := range allAVDs {
				profileName := guessProfileForAVD(avd.Name, p.cfg)
				candidates = append(candidates, bootJob{avdName: avd.Name, profileName: profileName})
			}

			// Shuffle and pick up to max_concurrent
			rand.Shuffle(len(candidates), func(i, j int) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			})

			limit := p.cfg.Pool.MaxConcurrent
			if limit > len(candidates) {
				limit = len(candidates)
			}
			jobs = candidates[:limit]

			log.Info().
				Int("available_avds", len(candidates)).
				Int("selected", len(jobs)).
				Msg("pool: randomly selected AVDs to boot")
		}
	}

	log.Info().
		Int("max_concurrent", p.cfg.Pool.MaxConcurrent).
		Int("booting", len(jobs)).
		Msg("pool: starting")

	// Boot in parallel
	var wg sync.WaitGroup
	for _, job := range jobs {
		if p.totalInstances() >= p.cfg.Pool.MaxConcurrent {
			log.Warn().Str("avd", job.avdName).Msg("pool: max concurrent reached, skipping")
			break
		}
		wg.Add(1)
		go func(j bootJob) {
			defer wg.Done()
			if _, err := p.bootExisting(ctx, j.avdName, j.profileName); err != nil {
				log.Error().Err(err).Str("avd", j.avdName).Msg("pool: failed to boot emulator")
			}
		}(job)
	}
	wg.Wait()

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

// warmOne boots an existing AVD for the warmup pool. Never creates AVDs.
func (p *Pool) warmOne(ctx context.Context, profileName string, index int) error {
	avdName := android.AVDName(profileName, index)
	inst, err := p.bootExisting(ctx, avdName, profileName)
	if err != nil {
		return err
	}
	_ = inst
	return nil
}

// createAndBoot creates an AVD, boots it, saves a clean snapshot, and registers it.
// Used for on-demand allocation when pool is under capacity.
func (p *Pool) createAndBoot(ctx context.Context, profileName string, index int) (*EmulatorInstance, error) {
	profile, ok := p.cfg.Pool.Profiles.Android[profileName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProfileNotFound, profileName)
	}

	avdName := android.AVDName(profileName, index)

	// Create AVD
	log.Info().Str("avd", avdName).Str("profile", profileName).Msg("pool: creating AVD")
	if err := p.avdMgr.Create(ctx, avdName, profile); err != nil {
		return nil, fmt.Errorf("create AVD %s: %w", avdName, err)
	}

	return p.bootExisting(ctx, avdName, profileName)
}

// bootExisting boots an AVD that already exists on disk.
// Used by warmup (start) — never creates AVDs.
func (p *Pool) bootExisting(ctx context.Context, avdName string, profileName string) (*EmulatorInstance, error) {
	profile, ok := p.cfg.Pool.Profiles.Android[profileName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProfileNotFound, profileName)
	}

	// Verify AVD exists
	exists, err := p.avdMgr.Exists(ctx, avdName)
	if err != nil {
		return nil, fmt.Errorf("check AVD %s: %w", avdName, err)
	}
	if !exists {
		return nil, fmt.Errorf("AVD %s does not exist (run 'drizz-farm create' first)", avdName)
	}

	id := uuid.New().String()[:8]
	inst := &EmulatorInstance{
		ID:          id,
		AVDName:     avdName,
		ProfileName: profileName,
		State:       StateCreating,
		CreatedAt:   time.Now(),
	}

	// Register instance early
	p.mu.Lock()
	p.instances[id] = inst
	p.mu.Unlock()

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
		NoWindow: !p.cfg.Pool.VisibleEmulators,
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

	// Watch process — detect death immediately
	go p.watchProcess(inst)

	return inst, nil
}

// watchProcess waits for an emulator process to exit and removes it from the pool.
func (p *Pool) watchProcess(inst *EmulatorInstance) {
	if inst.Process == nil || inst.Process.Cmd == nil || inst.Process.Cmd.Process == nil {
		return
	}

	// Wait blocks until the process exits
	_, _ = inst.Process.Cmd.Process.Wait()

	state := inst.GetState()
	// If it's already being destroyed/reset by us, don't interfere
	if state == StateDestroying || state == StateResetting {
		return
	}

	log.Warn().
		Str("instance", inst.ID).
		Str("avd", inst.AVDName).
		Str("previous_state", state.String()).
		Msg("pool: emulator process exited unexpectedly")

	p.portAlloc.Release(inst.Ports)
	p.removeInstance(inst.ID)
}

// resetInstance resets an emulator to clean state after a session.
// Tries snapshot restore first (fast ~5s), falls back to reboot.
func (p *Pool) resetInstance(ctx context.Context, inst *EmulatorInstance) error {
	log.Info().Str("instance", inst.ID).Str("avd", inst.AVDName).Msg("pool: resetting emulator")

	// Try snapshot restore (only works if we saved one during boot)
	err := p.emuCtrl.SnapshotLoad(ctx, p.adb, inst.Serial, "drizz_clean")
	if err == nil {
		return nil
	}

	// Snapshot failed — fall back to clearing user-installed apps
	log.Warn().Err(err).Str("instance", inst.ID).Msg("pool: snapshot restore failed, clearing apps instead")
	packages, listErr := p.adb.ListThirdPartyPackages(ctx, inst.Serial)
	if listErr == nil {
		for _, pkg := range packages {
			_ = p.adb.Uninstall(ctx, inst.Serial, pkg)
		}
	}
	return nil
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
	ticker := time.NewTicker(10 * time.Second)
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

// runMaintenance checks process liveness, handles errors, and replenishes pool.
func (p *Pool) runMaintenance(ctx context.Context) {
	// 1. Check process liveness — detect manually killed emulators
	p.mu.RLock()
	var dead []*EmulatorInstance
	for _, inst := range p.instances {
		state := inst.GetState()
		if state == StateWarm || state == StateAllocated {
			if !p.emuCtrl.IsRunning(inst.Process) {
				dead = append(dead, inst)
			}
		}
	}
	p.mu.RUnlock()

	for _, inst := range dead {
		log.Warn().
			Str("instance", inst.ID).
			Str("avd", inst.AVDName).
			Msg("pool: emulator process died, removing from pool")
		p.portAlloc.Release(inst.Ports)
		p.removeInstance(inst.ID)
	}

	// 2. Handle errored instances
	p.mu.RLock()
	var errored []*EmulatorInstance
	for _, inst := range p.instances {
		if inst.GetState() == StateError {
			errored = append(errored, inst)
		}
	}
	p.mu.RUnlock()

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

// guessProfileForAVD tries to match an AVD name to a configured profile.
// Falls back to the first configured profile if no match found.
func guessProfileForAVD(avdName string, cfg *config.Config) string {
	// Check if AVD name matches any warmup profile pattern (drizz_<profile>_N)
	for profileName := range cfg.Pool.Profiles.Android {
		prefix := "drizz_" + profileName + "_"
		if len(avdName) > len(prefix) && avdName[:len(prefix)] == prefix {
			return profileName
		}
	}
	// Fallback: return first profile (for user-created AVDs like Pixel_8_API_34-ext8)
	for profileName := range cfg.Pool.Profiles.Android {
		return profileName
	}
	return ""
}
