package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionNotActive = errors.New("session is not active")
)

// Broker manages session lifecycle.
type Broker struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	pool   *pool.Pool
	queue  *Queue
	cfg    *config.Config
	hostIP string

	cancel context.CancelFunc
}

// NewBroker creates a new session broker.
func NewBroker(cfg *config.Config, p *pool.Pool) *Broker {
	queueTimeout := time.Duration(cfg.Pool.QueueTimeoutSeconds) * time.Second
	return &Broker{
		sessions: make(map[string]*Session),
		pool:     p,
		queue:    NewQueue(cfg.Pool.QueueMaxSize, queueTimeout),
		cfg:      cfg,
		hostIP:   detectLANIP(),
	}
}

// Start begins the broker's background tasks (timeout enforcement, queue draining).
func (b *Broker) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)
	go b.timeoutLoop(ctx)
	go b.queueDrainLoop(ctx)
}

// Stop cancels background tasks.
func (b *Broker) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

// Create creates a new session, allocating an emulator from the pool.
func (b *Broker) Create(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	if req.Platform == "" {
		req.Platform = "android"
	}
	if req.Profile == "" {
		return nil, fmt.Errorf("profile is required")
	}

	// Try to allocate from pool
	inst, err := b.pool.Allocate(ctx, req.Profile)
	if err != nil {
		if errors.Is(err, pool.ErrPoolExhausted) {
			log.Info().Str("profile", req.Profile).Msg("broker: pool exhausted, queueing request")
			return b.queue.Enqueue(ctx, req)
		}
		return nil, fmt.Errorf("broker: allocate: %w", err)
	}

	return b.createSessionFromInstance(inst, req), nil
}

// Get returns a session by ID.
func (b *Broker) Get(id string) (*Session, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sess, ok := b.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return sess, nil
}

// Release ends a session and returns the emulator to the pool.
func (b *Broker) Release(ctx context.Context, id string) error {
	b.mu.Lock()
	sess, ok := b.sessions[id]
	if !ok {
		b.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if !sess.IsActive() {
		b.mu.Unlock()
		return fmt.Errorf("%w: %s (state: %s)", ErrSessionNotActive, id, sess.State)
	}

	now := time.Now()
	sess.State = SessionReleased
	sess.ReleasedAt = &now
	b.mu.Unlock()

	log.Info().
		Str("session", id).
		Str("instance", sess.InstanceID).
		Dur("duration", now.Sub(sess.CreatedAt)).
		Msg("broker: session released")

	// Release emulator back to pool (triggers snapshot restore)
	if err := b.pool.Release(ctx, sess.InstanceID); err != nil {
		log.Error().Err(err).Str("session", id).Msg("broker: pool release failed")
		return err
	}

	return nil
}

// List returns all sessions (active and recent).
func (b *Broker) List() []*Session {
	b.mu.RLock()
	defer b.mu.RUnlock()

	sessions := make([]*Session, 0, len(b.sessions))
	for _, sess := range b.sessions {
		sessions = append(sessions, sess)
	}
	return sessions
}

// ActiveCount returns the number of active sessions.
func (b *Broker) ActiveCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, sess := range b.sessions {
		if sess.IsActive() {
			count++
		}
	}
	return count
}

// QueueDepth returns the current queue depth.
func (b *Broker) QueueDepth() int {
	return b.queue.Depth()
}

func (b *Broker) createSessionFromInstance(inst *pool.EmulatorInstance, req CreateSessionRequest) *Session {
	sessionID := uuid.New().String()[:12]

	timeout := time.Duration(b.cfg.Pool.SessionTimeoutMinutes) * time.Minute
	if req.TimeoutMin > 0 {
		timeout = time.Duration(req.TimeoutMin) * time.Minute
	}

	maxDuration := time.Duration(b.cfg.Pool.SessionMaxMinutes) * time.Minute
	if timeout > maxDuration {
		timeout = maxDuration
	}

	sess := &Session{
		ID:         sessionID,
		Profile:    req.Profile,
		Platform:   req.Platform,
		InstanceID: inst.ID,
		State:      SessionActive,
		Connection: ConnectionInfo{
			Host:        b.hostIP,
			ADBPort:     inst.Ports.ADB,
			ADBSerial:   inst.Serial,
			ConsolePort: inst.Ports.Console,
		},
		ClientID:   req.ClientID,
		ClientName: req.ClientName,
		Source:     req.Source,
		Labels:     req.Labels,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(timeout),
	}

	inst.SetSession(sessionID)

	b.mu.Lock()
	b.sessions[sessionID] = sess
	b.mu.Unlock()

	log.Info().
		Str("session", sessionID).
		Str("instance", inst.ID).
		Str("profile", req.Profile).
		Str("serial", inst.Serial).
		Str("host", b.hostIP).
		Int("adb_port", inst.Ports.ADB).
		Time("expires", sess.ExpiresAt).
		Msg("broker: session created")

	return sess
}

// timeoutLoop periodically checks for expired sessions.
func (b *Broker) timeoutLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.enforceTimeouts(ctx)
		}
	}
}

func (b *Broker) enforceTimeouts(ctx context.Context) {
	b.mu.RLock()
	var expired []string
	for id, sess := range b.sessions {
		if sess.State == SessionActive && sess.IsExpired() {
			expired = append(expired, id)
		}
	}
	b.mu.RUnlock()

	for _, id := range expired {
		log.Warn().Str("session", id).Msg("broker: session timed out, auto-releasing")
		b.mu.Lock()
		if sess, ok := b.sessions[id]; ok {
			now := time.Now()
			sess.State = SessionTimedOut
			sess.ReleasedAt = &now
		}
		b.mu.Unlock()

		if sess, err := b.Get(id); err == nil {
			_ = b.pool.Release(ctx, sess.InstanceID)
		}
	}
}

// queueDrainLoop tries to fulfill queued requests when emulators become available.
func (b *Broker) queueDrainLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tryDrainQueue(ctx)
		}
	}
}

func (b *Broker) tryDrainQueue(ctx context.Context) {
	for {
		entry := b.queue.TryDequeue()
		if entry == nil {
			return
		}

		inst, err := b.pool.Allocate(ctx, entry.Request.Profile)
		if err != nil {
			if errors.Is(err, pool.ErrPoolExhausted) {
				// Put it back (simplification: just fail it, user can retry)
				entry.ResultCh <- QueueResult{Err: ErrQueueTimeout}
				return
			}
			entry.ResultCh <- QueueResult{Err: err}
			continue
		}

		sess := b.createSessionFromInstance(inst, entry.Request)
		entry.ResultCh <- QueueResult{Session: sess}
	}
}

// detectLANIP returns the first non-loopback IPv4 address.
func detectLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}
