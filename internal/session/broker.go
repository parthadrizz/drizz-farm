package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/appium"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/federation"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/store"
	"github.com/drizz-dev/drizz-farm/internal/webhook"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionNotActive = errors.New("session is not active")
)

// Broker manages session lifecycle.
type Broker struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	pool       *pool.Pool
	queue      *Queue
	cfg        *config.Config
	store      *store.Store
	webhook    *webhook.Sender
	appium     *appium.Manager
	federation *federation.Registry
	hostIP     string
	readyCh    chan struct{}

	cancel context.CancelFunc
}

// NewBroker creates a new session broker.
func NewBroker(cfg *config.Config, p *pool.Pool, s *store.Store, fed *federation.Registry) *Broker {
	queueTimeout := time.Duration(cfg.Pool.QueueTimeoutSeconds) * time.Second
	readyCh := make(chan struct{}, 10) // buffered so notify never blocks

	// Collect webhook URLs
	var webhookURLs []string
	for _, wh := range cfg.Webhooks {
		webhookURLs = append(webhookURLs, wh.URLs...)
	}

	b := &Broker{
		sessions:   make(map[string]*Session),
		pool:       p,
		store:      s,
		webhook:    webhook.NewSender(webhookURLs),
		appium:     appium.NewManager(detectLANIP()),
		federation: fed,
		queue:    NewQueue(cfg.Pool.QueueMaxSize, queueTimeout),
		cfg:      cfg,
		hostIP:   detectLANIP(),
		readyCh:  readyCh,
	}

	// Register callback — pool notifies us when emulator becomes warm
	p.SetOnReady(func() {
		select {
		case readyCh <- struct{}{}:
		default: // don't block if channel is full
		}
	})

	return b
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
	b.appium.StopAll()
}

// Create creates a new session, allocating an emulator from the pool.
func (b *Broker) Create(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	if req.Platform == "" {
		req.Platform = "android"
	}
	if req.Profile == "" {
		// Default to first configured profile
		for name := range b.cfg.Pool.Profiles.Android {
			req.Profile = name
			break
		}
		if req.Profile == "" {
			return nil, fmt.Errorf("no profiles configured")
		}
	}

	// Try to allocate from local pool
	inst, err := b.pool.Allocate(ctx, req.Profile)
	if err != nil {
		if errors.Is(err, pool.ErrPoolExhausted) {
			// Try federation peers before queueing
			if b.federation != nil && b.federation.PeerCount() > 0 {
				peer := b.federation.FindPeerWithCapacity()
				if peer != nil {
					log.Info().Str("peer", peer.Name).Str("host", peer.Host).Msg("broker: routing to peer")
					result, fedErr := b.federation.CreateRemoteSession(peer, req.Profile)
					if fedErr == nil {
						// Store the remote session info so we can proxy release/artifacts
						remoteSess := &Session{
							ID:         result["id"].(string),
							Profile:    req.Profile,
							Platform:   req.Platform,
							State:      SessionActive,
							Source:     req.Source,
							CreatedAt:  time.Now(),
							ExpiresAt:  time.Now().Add(time.Duration(b.cfg.Pool.SessionTimeoutMinutes) * time.Minute),
						}
						// Extract connection info
						if conn, ok := result["connection"].(map[string]any); ok {
							remoteSess.Connection = ConnectionInfo{
								Host:      fmt.Sprintf("%v", conn["host"]),
								AppiumURL: fmt.Sprintf("%v", conn["appium_url"]),
								ADBSerial: fmt.Sprintf("%v", conn["adb_serial"]),
							}
							if port, ok := conn["adb_port"].(float64); ok {
								remoteSess.Connection.ADBPort = int(port)
							}
						}
						// Track which node owns it
						remoteSess.InstanceID = fmt.Sprintf("remote:%s:%d/%s", peer.Host, peer.Port, result["id"])

						b.mu.Lock()
						b.sessions[remoteSess.ID] = remoteSess
						b.mu.Unlock()

						log.Info().Str("session", remoteSess.ID).Str("peer", peer.Name).Msg("broker: remote session created")
						return remoteSess, nil
					}
					log.Warn().Err(fedErr).Str("peer", peer.Name).Msg("broker: peer session failed")
				}
			}

			log.Info().Str("profile", req.Profile).Msg("broker: pool exhausted (local + peers), queueing")
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

	// If remote session, release on the remote node
	if strings.HasPrefix(sess.InstanceID, "remote:") {
		// Format: "remote:host:port/sessionID"
		parts := strings.SplitN(strings.TrimPrefix(sess.InstanceID, "remote:"), "/", 2)
		if len(parts) == 2 && b.federation != nil {
			nodeAddr := parts[0]
			remoteSessionID := parts[1]
			if err := b.federation.ReleaseRemoteSession(nodeAddr, remoteSessionID); err != nil {
				log.Warn().Err(err).Str("node", nodeAddr).Msg("broker: remote release failed")
			}
		}
		// No local pool release needed
		b.appium.Stop(id)
		b.webhook.Send(webhook.Event{
			Type: "session.released", SessionID: id, Duration: now.Sub(sess.CreatedAt).String(),
		})
		return nil
	}

	// Persist to SQLite
	if b.store != nil {
		b.store.RecordSession(sess.ID, sess.Profile, sess.Platform, sess.InstanceID, "", sess.Connection.ADBSerial, sess.Connection.Host, sess.Source, "released", sess.Connection.ADBPort, sess.CreatedAt, sess.ReleasedAt)
		b.store.RecordEvent("session_released", sess.InstanceID, sess.ID, fmt.Sprintf("duration=%s", now.Sub(sess.CreatedAt)))
	}

	// Stop Appium server
	b.appium.Stop(id)

	// Webhook
	b.webhook.Send(webhook.Event{
		Type: "session.released", SessionID: id, InstanceID: sess.InstanceID,
		Profile: sess.Profile, Duration: now.Sub(sess.CreatedAt).String(),
	})

	// Release emulator back to pool (triggers snapshot restore)
	if err := b.pool.Release(ctx, sess.InstanceID); err != nil {
		log.Error().Err(err).Str("session", id).Msg("broker: pool release failed")
		return err
	}

	// Drain queue — try to allocate the freed device to a waiting request
	go b.drainQueue(ctx)

	return nil
}

// drainQueue checks if there are queued requests and tries to allocate devices to them.
func (b *Broker) drainQueue(ctx context.Context) {
	entry := b.queue.TryDequeue()
	if entry == nil {
		return
	}

	log.Info().Str("profile", entry.Request.Profile).Msg("broker: draining queue, allocating to waiting request")

	inst, err := b.pool.Allocate(ctx, entry.Request.Profile)
	if err != nil {
		// Still no capacity — put it back at the front
		log.Debug().Err(err).Msg("broker: queue drain failed, re-queueing")
		b.queue.PushFront(entry)
		return
	}

	sess := b.createSessionFromInstance(inst, entry.Request)
	entry.ResultCh <- QueueResult{Session: sess}
	log.Info().Str("session", sess.ID).Str("serial", inst.Device.Serial()).Msg("broker: queued request fulfilled")
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

func (b *Broker) createSessionFromInstance(inst *pool.DeviceInstance, req CreateSessionRequest) *Session {
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
		Connection: b.buildConnectionInfo(inst),
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
		Str("serial", inst.Device.Serial()).
		Str("host", b.hostIP).
		Str("device", inst.Device.DisplayName()).
		Time("expires", sess.ExpiresAt).
		Msg("broker: session created")

	// Persist to SQLite
	if b.store != nil {
		deviceName := ""
		if inst.Device != nil {
			deviceName = inst.Device.DisplayName()
		}
		conn := b.buildConnectionInfo(inst)
		b.store.RecordSession(sess.ID, sess.Profile, sess.Platform, inst.ID, deviceName, conn.ADBSerial, conn.Host, sess.Source, "active", conn.ADBPort, sess.CreatedAt, nil)
		b.store.RecordEvent("session_created", inst.ID, sess.ID, "profile="+sess.Profile)
	}

	// Start Appium server for this session
	if b.appium.IsAvailable() {
		serial := ""
		if inst.Device != nil {
			serial = inst.Device.Serial()
		}
		if appInst, err := b.appium.Start(sess.ID, serial); err == nil {
			sess.Connection.AppiumURL = appInst.WebDriverURL
			log.Info().Str("session", sess.ID).Str("appium", appInst.WebDriverURL).Msg("broker: Appium server started")
		} else {
			log.Warn().Err(err).Str("session", sess.ID).Msg("broker: Appium start failed (session still works via ADB)")
		}
	}

	// Webhook
	b.webhook.Send(webhook.Event{
		Type: "session.created", SessionID: sess.ID, InstanceID: inst.ID,
		Profile: sess.Profile, Serial: sess.Connection.ADBSerial,
	})

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
			if b.store != nil {
				b.store.RecordSession(sess.ID, sess.Profile, sess.Platform, sess.InstanceID, "", sess.Connection.ADBSerial, sess.Connection.Host, sess.Source, "timed_out", sess.Connection.ADBPort, sess.CreatedAt, sess.ReleasedAt)
				b.store.RecordEvent("session_timed_out", sess.InstanceID, sess.ID, "")
			}
			_ = b.pool.Release(ctx, sess.InstanceID)
		}
	}
}

// queueDrainLoop tries to fulfill queued requests when emulators become available.
// Listens on readyCh (notified by pool) instead of blind polling.
func (b *Broker) queueDrainLoop(ctx context.Context) {
	// Also keep a slow poll as fallback (in case notification is missed)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.readyCh:
			// Pool says an emulator is warm — try to serve queue immediately
			b.tryDrainQueue(ctx)
		case <-ticker.C:
			// Fallback poll
			b.tryDrainQueue(ctx)
		}
	}
}

func (b *Broker) tryDrainQueue(ctx context.Context) {
	if b.queue.Depth() == 0 {
		return
	}

	// Peek at the front — only dequeue if we can actually allocate
	entry := b.queue.TryDequeue()
	if entry == nil {
		return
	}

	inst, err := b.pool.Allocate(ctx, entry.Request.Profile)
	if err != nil {
		if errors.Is(err, pool.ErrPoolExhausted) {
			// Put it back — try again next tick when an emulator might be free
			b.queue.PushFront(entry)
			return
		}
		// Real error — fail this request
		entry.ResultCh <- QueueResult{Err: err}
		return
	}

	sess := b.createSessionFromInstance(inst, entry.Request)
	entry.ResultCh <- QueueResult{Session: sess}

	// Try to drain more if there are still entries
	b.tryDrainQueue(ctx)
}

func (b *Broker) buildConnectionInfo(inst *pool.DeviceInstance) ConnectionInfo {
	if inst.Device == nil {
		return ConnectionInfo{Host: b.hostIP}
	}
	devConn := inst.Device.GetConnectionInfo()
	return ConnectionInfo{
		Host:        b.hostIP,
		DeviceKind:  devConn.DeviceKind,
		ADBPort:     devConn.ADBPort,
		ADBSerial:   devConn.ADBSerial,
		ConsolePort: devConn.ConsolePort,
		UDID:        devConn.UDID,
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
