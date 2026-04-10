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
	mu sync.RWMutex

	cfg       *config.Config
	instances map[string]*DeviceInstance

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
		cfg:       cfg,
		instances: make(map[string]*DeviceInstance),
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

		// watchDevice disabled — was killing emulators during ADB-heavy operations
	// go p.watchDevice(inst)

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

	log.Info().Int("instances", len(instances)).Msg("pool: stopping")

	var errs []error
	for _, inst := range instances {
		if inst.Device != nil && inst.Device.CanDestroy() {
			if err := p.destroyInstance(ctx, inst); err != nil {
				errs = append(errs, err)
			}
		} else {
			// USB devices — just remove from pool, don't shut down
			p.removeInstance(inst.ID)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("pool: %d errors during shutdown", len(errs))
	}
	return nil
}

// Allocate finds a warm device and transitions it to allocated.
// If none warm, boots an emulator on-demand or picks a discovered USB device.
func (p *Pool) Allocate(ctx context.Context, profileName string) (*DeviceInstance, error) {
	p.mu.Lock()

	// 1. Find a warm instance (prefer profile match, fall back to any)
	var anyWarm *DeviceInstance
	for _, inst := range p.instances {
		if inst.GetState() != StateWarm {
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

	// 2. No warm — boot emulator on-demand if under capacity
	underCapacity := len(p.instances) < p.cfg.Pool.MaxConcurrent
	p.mu.Unlock() // Release lock before external commands

	if underCapacity {
		avdName, err := p.findUnbootedAVD(ctx)
		if err != nil || avdName == "" {
			return nil, ErrPoolExhausted
		}

		guessedProfile := guessProfileForAVD(avdName, p.cfg)
		log.Info().Str("avd", avdName).Msg("pool: booting on-demand")

		inst, err := p.bootEmulator(ctx, avdName, guessedProfile)
		if err != nil {
			return nil, fmt.Errorf("pool: on-demand boot failed: %w", err)
		}
		if err := inst.TransitionTo(StateAllocated); err != nil {
			return nil, err
		}
		return inst, nil
	}

	return nil, ErrPoolExhausted
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

	// Create instance
	inst := p.createInstance(dev, profileName)

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
	// watchDevice disabled — was killing emulators during ADB-heavy operations
	// go p.watchDevice(inst)

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

func (p *Pool) destroyInstance(ctx context.Context, inst *DeviceInstance) error {
	_ = inst.TransitionTo(StateDestroying)

	if inst.Device != nil {
		if err := inst.Device.Shutdown(ctx); err != nil {
			log.Error().Err(err).Str("instance", inst.ID).Msg("pool: shutdown failed")
		}
	}

	p.removeInstance(inst.ID)
	log.Info().Str("instance", inst.ID).Msg("pool: instance destroyed")
	return nil
}

func (p *Pool) removeInstance(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.instances, id)
}

// --- Device Watching ---

// watchDevice monitors a device. Waits for grace period before health checking.
func (p *Pool) watchDevice(inst *DeviceInstance) {
	// Grace period — don't health check during initial boot/snapshot
	time.Sleep(60 * time.Second)

	ticker := time.NewTicker(30 * time.Second) // Check every 30s, not 15s
	defer ticker.Stop()

	for range ticker.C {
		state := inst.GetState()
		if state == StateDestroying {
			return
		}

		// Skip health checks during resetting
		if state == StateResetting || state == StateBooting {
			continue
		}

		p.mu.RLock()
		_, exists := p.instances[inst.ID]
		p.mu.RUnlock()
		if !exists {
			return
		}

		if inst.Device != nil {
			if err := inst.Device.HealthCheck(context.Background()); err != nil {
				inst.RecordHealthCheck(false)
				// Only kill if unhealthy for 5+ minutes straight
				if time.Since(inst.LastHealthy) > 5*time.Minute && inst.HealthFails > 0 {
					log.Warn().
						Str("instance", inst.ID).
						Str("device", inst.Device.DisplayName()).
						Dur("unhealthy_for", time.Since(inst.LastHealthy)).
						Msg("pool: device unhealthy for 5+ minutes, removing")
					_ = p.destroyInstance(context.Background(), inst)
					return
				}
			} else {
				inst.RecordHealthCheck(true)
			}
		}
	}
}

// --- Maintenance ---

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

func (p *Pool) runMaintenance(ctx context.Context) {
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

func (p *Pool) totalInstances() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.instances)
}

// findUnbootedAVD finds an AVD not currently in the pool.
// IMPORTANT: caller must NOT hold p.mu — this runs external commands and reads p.instances.
func (p *Pool) findUnbootedAVD(ctx context.Context) (string, error) {
	allAVDs, err := p.avdMgr.List(ctx)
	if err != nil {
		return "", err
	}

	// Read instances without lock — caller already released it or we're outside lock
	booted := make(map[string]bool)
	for _, inst := range p.instances {
		if inst.Device != nil {
			booted[inst.Device.DisplayName()] = true
		}
	}

	for _, avd := range allAVDs {
		if !booted[avd.Name] {
			return avd.Name, nil
		}
	}
	return "", nil
}

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
