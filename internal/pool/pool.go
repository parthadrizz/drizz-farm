// Package pool manages a fleet of Android emulators and physical devices,
// providing on-demand boot, session allocation via semaphore-based concurrency
// control, idle cleanup, and health monitoring. Devices are abstracted behind
// the Device interface so emulators, USB phones, and (future) iOS simulators
// share the same lifecycle.
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
	ErrPoolExhausted  = errors.New("pool exhausted: no devices available")
	ErrProfileNotFound = errors.New("profile not found")
	ErrInstanceNotFound = errors.New("instance not found")
)

// PoolStatus is a snapshot of pool state for API responses.
type PoolStatus struct {
	NodeName      string             `json:"node_name"`
	TotalCapacity int                `json:"total_capacity"`
	Warm          int                `json:"warm"`
	Allocated     int                `json:"allocated"`
	Booting       int                `json:"booting"`
	Resetting     int                `json:"resetting"`
	Error         int                `json:"error"`
	Instances     []InstanceSnapshot `json:"instances"`
}

// OnDeviceReady is called when a device becomes warm (ready for allocation).
type OnDeviceReady func()

// Pool manages a unified fleet of emulators and physical devices.
type Pool struct {
	mu  sync.RWMutex
	sem chan struct{} // semaphore: limits concurrent devices to MaxConcurrent

	nodeName  string
	cfg       *config.Config
	instances map[string]*DeviceInstance
	bootingAVDs map[string]bool // AVD names currently being booted
	avdMu     sync.Mutex       // protects AVD name selection only

	// Android emulator dependencies
	sdk       *android.SDK
	adb       *android.ADBClient
	avdMgr    *android.AVDManager
	emuCtrl   *android.EmulatorController
	portAlloc *android.PortAllocator
	runner    android.CommandRunner

	// Device scanners (USB auto-discovery)
	scanners []DeviceScanner

	onReady OnDeviceReady
	cancel  context.CancelFunc
}

// New creates a new Pool.
func New(cfg *config.Config, sdk *android.SDK, runner android.CommandRunner) *Pool {
	adb := android.NewADBClient(sdk, runner)
	return &Pool{
		nodeName:    cfg.Node.Name,
		cfg:         cfg,
		sem:         make(chan struct{}, cfg.Pool.MaxConcurrent),
		instances:   make(map[string]*DeviceInstance),
		bootingAVDs: make(map[string]bool),
		sdk:       sdk,
		adb:       adb,
		avdMgr:    android.NewAVDManager(sdk, runner),
		emuCtrl:   android.NewEmulatorController(sdk, runner),
		portAlloc: android.NewPortAllocator(cfg.Pool.PortRangeMin, cfg.Pool.PortRangeMax),
		runner:    runner,
	}
}

// SetOnReady sets the callback for when a device becomes warm.
func (p *Pool) SetOnReady(fn OnDeviceReady) {
	p.onReady = fn
}

// RegisterScanner adds a device scanner for auto-discovery.
func (p *Pool) RegisterScanner(s DeviceScanner) {
	p.scanners = append(p.scanners, s)
}

// notifyReady signals the broker that a device has transitioned to warm state,
// triggering queue drain for any waiting session requests.
func (p *Pool) notifyReady() {
	if p.onReady != nil {
		go p.onReady()
	}
}

// Start initializes the pool — adopts already-running emulators, then monitors.
func (p *Pool) Start(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)

	// Adopt any already-running emulators (from previous sessions or manual starts)
	p.adoptRunningEmulators(ctx)

	log.Info().
		Int("max_concurrent", p.cfg.Pool.MaxConcurrent).
		Int("scanners", len(p.scanners)).
		Int("adopted", len(p.instances)).
		Msg("pool: ready (devices boot on-demand)")

	go p.maintenanceLoop(ctx)
	return nil
}

// adoptRunningEmulators scans ADB for already-running devices and adds them to the pool.
func (p *Pool) adoptRunningEmulators(ctx context.Context) {
	devices, err := p.adb.Devices(ctx)
	if err != nil {
		return
	}

	for _, dev := range devices {
		if dev.State != "device" {
			continue
		}

		// Check if already in pool
		alreadyTracked := false
		p.mu.RLock()
		for _, inst := range p.instances {
			if inst.Device != nil && inst.Device.Serial() == dev.Serial {
				alreadyTracked = true
				break
			}
		}
		p.mu.RUnlock()
		if alreadyTracked {
			continue
		}

		// Create a lightweight device wrapper for this running emulator
		emuDev := android.NewRunningEmulatorDevice(dev.Serial, p.adb)
		if err := emuDev.Prepare(ctx); err != nil {
			continue
		}

		inst := p.createInstance(emuDev, "")
		if err := inst.TransitionTo(StateWarm); err != nil {
			p.removeInstance(inst.ID)
			continue
		}
		inst.RecordHealthCheck(true)
		inst.TouchActivity()

		go p.watchDevice(inst)

		log.Info().Str("serial", dev.Serial).Str("name", emuDev.DisplayName()).Msg("pool: adopted running emulator")
	}
}

// Stop gracefully shuts down all managed devices.
func (p *Pool) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}

	p.mu.Lock()
	instances := make([]*DeviceInstance, 0, len(p.instances))
	for _, inst := range p.instances {
		instances = append(instances, inst)
	}
	p.mu.Unlock()

	log.Info().Int("instances", len(instances)).Msg("pool: stopping (parallel)")

	// Destroy all instances in parallel — sequential shutdown with N
	// emulators and 10s SIGTERM waits stacks up past the 30s shutdown
	// budget. Each goroutine runs its own destroy so they run
	// concurrently; errors are collected via a channel.
	errCh := make(chan error, len(instances))
	var wg sync.WaitGroup
	for _, inst := range instances {
		inst := inst
		wg.Add(1)
		go func() {
			defer wg.Done()
			if inst.Device != nil && inst.Device.CanDestroy() {
				if err := p.destroyInstance(ctx, inst); err != nil {
					errCh <- err
				}
			} else {
				// USB devices — just remove from pool, don't shut down.
				p.removeInstance(inst.ID)
			}
		}()
	}
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("pool: %d errors during shutdown", len(errs))
	}
	return nil
}

// AllocateOpts lets callers express "give me a specific device" vs
// "any matching profile," plus a source tag that decides whether
// reserved devices are skipped. Zero-value = "any warm, profile-blind,
// treat as automated source (skip reservations)."
type AllocateOpts struct {
	ProfileName string // if set, prefer matching profile
	InstanceID  string // if set, allocate exactly this instance (fails if not free)
	AVDName     string // if set, allocate the instance whose AVD name matches
	Source      string // "manual" / "dashboard" can use reserved instances; anything else skips them
}

// AllocateWith is the generalized allocator. Allocate (profile-only) delegates to it.
func (p *Pool) AllocateWith(ctx context.Context, opts AllocateOpts) (*DeviceInstance, error) {
	manualCaller := opts.Source == "manual" || opts.Source == "dashboard"

	// Specific-device paths fail fast for InstanceID (IDs only exist for
	// already-tracked instances), but AVDName supports boot-on-demand:
	// the dashboard's New Session modal lists every AVD known to the
	// machine, not just warm ones, so picking a cold AVD has to Just
	// Work instead of erroring "device not available: state=cold".
	if opts.InstanceID != "" {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, inst := range p.instances {
			if inst.ID != opts.InstanceID {
				continue
			}
			state := inst.GetState()
			if state != StateWarm {
				return nil, fmt.Errorf("device %s not available: state=%s", inst.ID, state)
			}
			if inst.IsReserved() && !manualCaller {
				return nil, fmt.Errorf("device %s is reserved for manual use", inst.ID)
			}
			if err := inst.TransitionTo(StateAllocated); err != nil {
				return nil, err
			}
			log.Info().Str("instance", inst.ID).Str("device", inst.Device.DisplayName()).Str("source", opts.Source).Msg("pool: allocated (specific)")
			return inst, nil
		}
		return nil, fmt.Errorf("no device with id=%q", opts.InstanceID)
	}

	if opts.AVDName != "" {
		// Is this AVD already a tracked instance?
		p.mu.Lock()
		for _, inst := range p.instances {
			if inst.Device == nil || inst.Device.DisplayName() != opts.AVDName {
				continue
			}
			state := inst.GetState()
			if state == StateWarm {
				if inst.IsReserved() && !manualCaller {
					p.mu.Unlock()
					return nil, fmt.Errorf("device %s is reserved for manual use", inst.ID)
				}
				if err := inst.TransitionTo(StateAllocated); err != nil {
					p.mu.Unlock()
					return nil, err
				}
				p.mu.Unlock()
				log.Info().Str("instance", inst.ID).Str("avd", opts.AVDName).Str("source", opts.Source).Msg("pool: allocated (specific warm)")
				return inst, nil
			}
			// Tracked but busy — don't boot a second copy, fail clearly.
			p.mu.Unlock()
			return nil, fmt.Errorf("AVD %q is currently %s", opts.AVDName, state)
		}
		p.mu.Unlock()

		// Not tracked — boot on demand. Acquire a semaphore slot like
		// allocateAny does so we respect max_concurrent.
		select {
		case p.sem <- struct{}{}:
		default:
			return nil, ErrPoolExhausted
		}
		p.mu.Lock()
		p.bootingAVDs[opts.AVDName] = true
		p.mu.Unlock()
		log.Info().Str("avd", opts.AVDName).Str("source", opts.Source).Msg("pool: booting on-demand (specific)")
		guessedProfile := guessProfileForAVD(opts.AVDName, p.cfg)
		inst, err := p.bootEmulator(ctx, opts.AVDName, guessedProfile)
		p.mu.Lock()
		delete(p.bootingAVDs, opts.AVDName)
		p.mu.Unlock()
		if err != nil {
			<-p.sem
			return nil, fmt.Errorf("pool: on-demand boot failed for %s: %w", opts.AVDName, err)
		}
		if err := inst.TransitionTo(StateAllocated); err != nil {
			<-p.sem
			return nil, err
		}
		return inst, nil
	}

	return p.Allocate(ctx, opts.ProfileName)
}

// Allocate finds a warm device and transitions it to allocated.
// If none warm, boots an emulator on-demand or picks a discovered USB device.
func (p *Pool) Allocate(ctx context.Context, profileName string) (*DeviceInstance, error) {
	return p.allocateAny(ctx, profileName, false)
}

// AllocateForManual is like Allocate but includes reserved devices in
// the candidate set — the dashboard / human operator is the one
// reservation was meant to protect, not a thing we gate.
func (p *Pool) AllocateForManual(ctx context.Context, profileName string) (*DeviceInstance, error) {
	return p.allocateAny(ctx, profileName, true)
}

func (p *Pool) allocateAny(ctx context.Context, profileName string, includeReserved bool) (*DeviceInstance, error) {
	p.mu.Lock()

	// 1. Find a warm instance (prefer profile match, fall back to any).
	//    Automated callers (includeReserved=false) skip Reserved instances
	//    so manual-reserved devices don't get yanked out from under a
	//    human's nose by a CI job that happened to match the profile.
	var anyWarm *DeviceInstance
	for _, inst := range p.instances {
		if inst.GetState() != StateWarm {
			continue
		}
		if !includeReserved && inst.IsReserved() {
			continue
		}
		if profileName != "" && inst.ProfileName == profileName {
			if err := inst.TransitionTo(StateAllocated); err != nil {
				continue
			}
			p.mu.Unlock()
			log.Info().Str("instance", inst.ID).Str("device", inst.Device.DisplayName()).Msg("pool: allocated (profile match)")
			return inst, nil
		}
		if anyWarm == nil {
			anyWarm = inst
		}
	}

	if anyWarm != nil {
		if err := anyWarm.TransitionTo(StateAllocated); err == nil {
			p.mu.Unlock()
			log.Info().Str("instance", anyWarm.ID).Str("device", anyWarm.Device.DisplayName()).Msg("pool: allocated (any warm)")
			return anyWarm, nil
		}
	}

	// 2. No warm — try to acquire a semaphore slot (non-blocking)
	p.mu.Unlock()

	select {
	case p.sem <- struct{}{}: // acquired slot
	default:
		return nil, ErrPoolExhausted // pool full
	}

	// Pick a unique AVD name (short lock to prevent double-selection)
	p.avdMu.Lock()
	usedAVDs := make(map[string]bool)
	p.mu.RLock()
	for _, inst := range p.instances {
		if inst.Device != nil {
			usedAVDs[inst.Device.DisplayName()] = true
		}
	}
	for avd := range p.bootingAVDs {
		usedAVDs[avd] = true
	}
	p.mu.RUnlock()

	avdName, err := p.findUnbootedAVDExcluding(ctx, usedAVDs)
	if err != nil || avdName == "" {
		p.avdMu.Unlock()
		<-p.sem // release slot
		return nil, ErrPoolExhausted
	}

	p.mu.Lock()
	p.bootingAVDs[avdName] = true
	p.mu.Unlock()
	p.avdMu.Unlock() // AVD reserved, others can pick a different one

	guessedProfile := guessProfileForAVD(avdName, p.cfg)
	log.Info().Str("avd", avdName).Msg("pool: booting on-demand")

	// Boot in parallel — semaphore holds our slot
	inst, err := p.bootEmulator(ctx, avdName, guessedProfile)

	p.mu.Lock()
	delete(p.bootingAVDs, avdName)
	p.mu.Unlock()

	if err != nil {
		<-p.sem // release slot on failure
		return nil, fmt.Errorf("pool: on-demand boot failed: %w", err)
	}

	if err := inst.TransitionTo(StateAllocated); err != nil {
		<-p.sem
		return nil, err
	}
	return inst, nil
}

// Release returns a device to the pool after use.
func (p *Pool) Release(ctx context.Context, instanceID string) error {
	p.mu.RLock()
	inst, ok := p.instances[instanceID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrInstanceNotFound, instanceID)
	}

	log.Info().Str("instance", instanceID).Str("device", inst.Device.DisplayName()).Msg("pool: releasing")

	if err := inst.TransitionTo(StateResetting); err != nil {
		return err
	}
	inst.ClearSession()

	go func() {
		if err := inst.Device.Reset(ctx); err != nil {
			log.Error().Err(err).Str("instance", instanceID).Msg("pool: reset failed")
			_ = inst.TransitionTo(StateError)
			return
		}
		if err := inst.TransitionTo(StateWarm); err != nil {
			log.Error().Err(err).Str("instance", instanceID).Msg("pool: transition to warm failed")
			return
		}
		inst.TouchActivity()
		p.notifyReady()
	}()

	return nil
}

// Status returns a snapshot of pool state.
func (p *Pool) Status() PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := PoolStatus{
		NodeName:      p.nodeName,
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

// Available returns count of warm devices.
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
func (p *Pool) GetInstance(id string) (*DeviceInstance, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	inst, ok := p.instances[id]
	return inst, ok
}

// BootAVD manually boots a specific AVD (called from API/UI).
func (p *Pool) BootAVD(ctx context.Context, avdName string) (*DeviceInstance, error) {
	// Check if already in pool
	p.mu.RLock()
	for _, inst := range p.instances {
		if inst.Device != nil && inst.Device.DisplayName() == avdName {
			p.mu.RUnlock()
			return inst, nil // Already booted
		}
	}
	p.mu.RUnlock()

	if p.totalInstances() >= p.cfg.Pool.MaxConcurrent {
		return nil, ErrPoolExhausted
	}

	profileName := guessProfileForAVD(avdName, p.cfg)
	return p.bootEmulator(ctx, avdName, profileName)
}

// ShutdownInstance manually shuts down a specific instance (called from API/UI).
func (p *Pool) ShutdownInstance(ctx context.Context, instanceID string) error {
	p.mu.RLock()
	inst, ok := p.instances[instanceID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrInstanceNotFound, instanceID)
	}

	if inst.GetState() == StateAllocated {
		return fmt.Errorf("cannot shutdown allocated instance %s (release session first)", instanceID)
	}

	log.Info().Str("instance", instanceID).Msg("pool: manual shutdown requested")
	return p.destroyInstance(ctx, inst)
}

// --- Emulator Boot ---

// bootEmulator boots an existing AVD as an EmulatorDevice.
func (p *Pool) bootEmulator(ctx context.Context, avdName string, profileName string) (*DeviceInstance, error) {
	profile, ok := p.cfg.Pool.Profiles.Android[profileName]
	if !ok {
		// Use a default profile for unknown AVDs
		profile = config.AndroidProfile{
			RAMMB:              2048,
			DiskSizeMB:         4096,
			GPU:                "host",
			BootTimeoutSeconds: 120,
		}
	}

	// Verify AVD exists
	exists, err := p.avdMgr.Exists(ctx, avdName)
	if err != nil {
		return nil, fmt.Errorf("check AVD %s: %w", avdName, err)
	}
	if !exists {
		return nil, fmt.Errorf("AVD %s does not exist", avdName)
	}

	// Create EmulatorDevice
	dev := android.NewEmulatorDevice(android.EmulatorDeviceConfig{
		AVDName:   avdName,
		Profile:   profile,
		EmuCtrl:   p.emuCtrl,
		ADB:       p.adb,
		PortAlloc: p.portAlloc,
		Visible:   p.cfg.Pool.VisibleEmulators,
	})

	// Create instance with enriched display info
	inst := p.createInstance(dev, profileName)

	// Enrich display info from AVD config
	avdInfo := android.AVDInfo{Name: avdName}
	android.EnrichAVDInfo(&avdInfo)
	inst.DisplayInfo = avdInfo.DisplayName

	// Boot
	if err := inst.TransitionTo(StateBooting); err != nil {
		p.removeInstance(inst.ID)
		return nil, err
	}

	if err := dev.Prepare(ctx); err != nil {
		p.removeInstance(inst.ID)
		return nil, fmt.Errorf("boot %s: %w", avdName, err)
	}

	// Warm
	if err := inst.TransitionTo(StateWarm); err != nil {
		return nil, err
	}
	inst.RecordHealthCheck(true)
	inst.TouchActivity()

	log.Info().
		Str("instance", inst.ID).
		Str("device", dev.DisplayName()).
		Str("serial", dev.Serial()).
		Msg("pool: device warm and ready")

	p.notifyReady()
	go p.watchDevice(inst)

	return inst, nil
}

// adoptDevice adds a discovered device (e.g., USB) to the pool.
func (p *Pool) adoptDevice(ctx context.Context, dev Device) {
	// Check if already tracked
	p.mu.RLock()
	for _, inst := range p.instances {
		if inst.Device != nil && inst.Device.Serial() == dev.Serial() {
			p.mu.RUnlock()
			return
		}
	}
	p.mu.RUnlock()

	// Prepare (verify online, read model name, etc.)
	if err := dev.Prepare(ctx); err != nil {
		log.Debug().Err(err).Str("serial", dev.Serial()).Msg("pool: skipping device (not ready)")
		return
	}

	inst := p.createInstance(dev, "")

	// USB devices skip boot — go straight to warm
	if err := inst.TransitionTo(StateWarm); err != nil {
		p.removeInstance(inst.ID)
		return
	}
	inst.RecordHealthCheck(true)
	inst.TouchActivity()

	log.Info().
		Str("instance", inst.ID).
		Str("device", dev.DisplayName()).
		Str("kind", string(dev.Kind())).
		Msg("pool: discovered device added to pool")

	p.notifyReady()
}

// createInstance creates a DeviceInstance, registers it, and returns it.
func (p *Pool) createInstance(dev Device, profileName string) *DeviceInstance {
	id := uuid.New().String()[:8]
	inst := &DeviceInstance{
		ID:          id,
		NodeName:    p.nodeName,
		ProfileName: profileName,
		State:       StateCreating,
		CreatedAt:   time.Now(),
		Device:      dev,
	}

	p.mu.Lock()
	p.instances[id] = inst
	p.mu.Unlock()

	return inst
}

// --- Lifecycle ---

// destroyInstance shuts down a device (kills emulator, cleans up scrcpy/screenrecord),
// removes it from the pool, and releases its semaphore slot.
func (p *Pool) destroyInstance(ctx context.Context, inst *DeviceInstance) error {
	_ = inst.TransitionTo(StateDestroying)

	if inst.Device != nil {
		if err := inst.Device.Shutdown(ctx); err != nil {
			log.Error().Err(err).Str("instance", inst.ID).Msg("pool: shutdown failed")
		}
	}

	p.removeInstance(inst.ID)

	// Release semaphore slot
	select {
	case <-p.sem:
	default:
	}

	log.Info().Str("instance", inst.ID).Msg("pool: instance destroyed")
	return nil
}

// removeInstance deletes an instance from the pool's instances map by ID.
func (p *Pool) removeInstance(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.instances, id)
}

// --- Device Watching ---

// watchDevice checks if device is still visible in ADB. Removes if completely gone.
func (p *Pool) watchDevice(inst *DeviceInstance) {
	time.Sleep(60 * time.Second) // Grace period

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		state := inst.GetState()
		if state == StateDestroying || state == StateResetting || state == StateBooting {
			continue
		}

		p.mu.RLock()
		_, exists := p.instances[inst.ID]
		p.mu.RUnlock()
		if !exists {
			return
		}

		// Only check if device is visible in ADB — don't run heavy commands
		if inst.Device != nil {
			serial := inst.Device.Serial()
			devices, err := p.adb.Devices(context.Background())
			if err != nil {
				continue // ADB itself failed, don't kill
			}
			found := false
			for _, d := range devices {
				if d.Serial == serial && d.State == "device" {
					found = true
					break
				}
			}
			if !found {
				inst.RecordHealthCheck(false)
				if inst.HealthFails >= 3 { // 3 consecutive misses (90s)
					log.Warn().Str("instance", inst.ID).Str("serial", serial).Msg("pool: device gone from ADB, removing")
					p.removeInstance(inst.ID)
					return
				}
			} else {
				inst.RecordHealthCheck(true)
			}
		}
	}
}

// --- Maintenance ---

// maintenanceLoop runs runMaintenance every 10 seconds until the context is
// cancelled. It handles USB discovery, error cleanup, and idle shutdown.
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

// runMaintenance performs a single maintenance pass: scans for new USB devices,
// prunes disconnected ones, destroys errored instances, and shuts down devices
// that have been idle longer than the configured timeout.
func (p *Pool) runMaintenance(ctx context.Context) {
	// 0. Re-adopt emulators started externally (e.g., from Android Studio or manual `emulator` command)
	// Skip if any device is currently booting — avoids race where a booting emulator
	// gets double-adopted before its Device.Serial() is populated.
	p.mu.RLock()
	anyBooting := false
	for _, inst := range p.instances {
		if inst.State == StateBooting {
			anyBooting = true
			break
		}
	}
	p.mu.RUnlock()
	if !anyBooting {
		p.adoptRunningEmulators(ctx)
	}

	// 1. Scan for USB devices
	for _, scanner := range p.scanners {
		devices, err := scanner.Scan(ctx)
		if err != nil {
			continue
		}

		// Adopt new devices
		for _, dev := range devices {
			p.adoptDevice(ctx, dev)
		}

		// Prune disconnected USB devices
		p.pruneDisconnected(ctx, devices)
	}

	// 2. Handle errored instances
	p.mu.RLock()
	var errored []*DeviceInstance
	for _, inst := range p.instances {
		if inst.GetState() == StateError {
			errored = append(errored, inst)
		}
	}
	p.mu.RUnlock()

	for _, inst := range errored {
		log.Warn().Str("instance", inst.ID).Msg("pool: destroying errored instance")
		_ = p.destroyInstance(ctx, inst)
	}

	// 3. Idle timeout — shut down idle devices that can be destroyed
	idleTimeout := time.Duration(p.cfg.Cleanup.IdleTimeoutMinutes) * time.Minute
	if idleTimeout > 0 {
		p.mu.RLock()
		var idle []*DeviceInstance
		for _, inst := range p.instances {
			state := inst.GetState()
			if state == StateWarm && inst.Device != nil && inst.Device.CanDestroy() && inst.IdleSince() > idleTimeout {
				idle = append(idle, inst)
			}
		}
		p.mu.RUnlock()

		for _, inst := range idle {
			log.Info().
				Str("instance", inst.ID).
				Str("device", inst.Device.DisplayName()).
				Dur("idle", inst.IdleSince()).
				Msg("pool: shutting down idle device")
			_ = p.destroyInstance(ctx, inst)
		}
	}
}

// pruneDisconnected removes USB devices that are no longer physically connected.
func (p *Pool) pruneDisconnected(ctx context.Context, currentDevices []Device) {
	// Build set of currently connected serials
	connected := make(map[string]bool)
	for _, dev := range currentDevices {
		connected[dev.Serial()] = true
	}

	p.mu.RLock()
	var disconnected []*DeviceInstance
	for _, inst := range p.instances {
		if inst.Device == nil {
			continue
		}
		// Only prune non-destroyable devices (USB) — emulators are handled by watchDevice
		if !inst.Device.CanDestroy() && !connected[inst.Device.Serial()] {
			disconnected = append(disconnected, inst)
		}
	}
	p.mu.RUnlock()

	for _, inst := range disconnected {
		log.Warn().
			Str("instance", inst.ID).
			Str("device", inst.Device.DisplayName()).
			Msg("pool: device disconnected, removing from pool")
		p.removeInstance(inst.ID)
	}
}

// --- Helpers ---

// totalInstances returns the current number of tracked instances across all states.
func (p *Pool) totalInstances() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.instances)
}

// findUnbootedAVD finds an AVD not currently in the pool.
// IMPORTANT: caller must NOT hold p.mu — this runs external commands and reads p.instances.
func (p *Pool) findUnbootedAVD(ctx context.Context) (string, error) {
	return p.findUnbootedAVDExcluding(ctx, nil)
}

// findUnbootedAVDExcluding returns the name of an AVD that is neither running
// in the pool nor present in the exclude set. Returns ("", nil) when every AVD
// is occupied. Caller must NOT hold p.mu.
func (p *Pool) findUnbootedAVDExcluding(ctx context.Context, exclude map[string]bool) (string, error) {
	allAVDs, err := p.avdMgr.List(ctx)
	if err != nil {
		return "", err
	}

	// Combine: instances already running + explicitly excluded (booting)
	used := make(map[string]bool)
	p.mu.RLock()
	for _, inst := range p.instances {
		if inst.Device != nil {
			used[inst.Device.DisplayName()] = true
		}
	}
	p.mu.RUnlock()
	for name := range exclude {
		used[name] = true
	}

	for _, avd := range allAVDs {
		if !used[avd.Name] {
			return avd.Name, nil
		}
	}
	return "", nil
}

// guessProfileForAVD attempts to match an AVD name to a configured profile by
// checking for a "drizz_{profileName}_" prefix. If no prefix matches it falls
// back to the first available profile, or "" if none are configured.
func guessProfileForAVD(avdName string, cfg *config.Config) string {
	for profileName := range cfg.Pool.Profiles.Android {
		prefix := "drizz_" + profileName + "_"
		if len(avdName) > len(prefix) && avdName[:len(prefix)] == prefix {
			return profileName
		}
	}
	for profileName := range cfg.Pool.Profiles.Android {
		return profileName
	}
	return ""
}

// ---- Reservation management ----

// FindByInstanceID returns a snapshot of the instance with the given
// ID, plus the underlying instance pointer for mutation. Returns
// (nil, nil) when not found.
func (p *Pool) FindByInstanceID(id string) *DeviceInstance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.instances[id]
}

// FindByAVDName returns the instance whose device.DisplayName matches.
// Returns nil when not found.
func (p *Pool) FindByAVDName(name string) *DeviceInstance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, inst := range p.instances {
		if inst.Device != nil && inst.Device.DisplayName() == name {
			return inst
		}
	}
	return nil
}

// ReserveInstance marks the instance as reserved (in-memory only).
// Callers should also persist via store.SetReservation for durability.
func (p *Pool) ReserveInstance(id, label string) error {
	p.mu.RLock()
	inst, ok := p.instances[id]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance %s not found", id)
	}
	inst.Reserve(label)
	return nil
}

// UnreserveInstance clears the in-memory reservation.
func (p *Pool) UnreserveInstance(id string) error {
	p.mu.RLock()
	inst, ok := p.instances[id]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance %s not found", id)
	}
	inst.Unreserve()
	return nil
}

// ApplyReservations takes a map of avd_name → label and marks every
// matching instance as reserved. Used at daemon startup to restore
// reservations from the SQLite store. Unknown AVDs are ignored (they
// may not have been booted yet; the next re-apply on boot picks them up).
func (p *Pool) ApplyReservations(byAVDName map[string]string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, inst := range p.instances {
		if inst.Device == nil {
			continue
		}
		if label, ok := byAVDName[inst.Device.DisplayName()]; ok {
			inst.Reserve(label)
		}
	}
}
