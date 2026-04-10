// Package appium manages Appium server instances per device session.
// When a session is created, an Appium server starts on a unique port
// pointed at that device's ADB serial. The WebDriver URL is returned
// so any Appium test script can connect.
package appium

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Instance represents a running Appium server for one device.
type Instance struct {
	Port       int
	Serial     string
	WebDriverURL string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
}

// Manager manages Appium server instances.
type Manager struct {
	mu        sync.Mutex
	instances map[string]*Instance // keyed by session/instance ID
	appiumBin string
	basePort  int
	hostIP    string
}

// NewManager creates an Appium manager.
func NewManager(hostIP string) *Manager {
	// Find appium binary
	appiumBin, err := exec.LookPath("appium")
	if err != nil {
		log.Warn().Msg("appium: binary not found — Appium hosting disabled")
		return &Manager{hostIP: hostIP, instances: make(map[string]*Instance)}
	}
	log.Info().Str("bin", appiumBin).Msg("appium: found")
	return &Manager{
		appiumBin: appiumBin,
		instances: make(map[string]*Instance),
		basePort:  4723,
		hostIP:    hostIP,
	}
}

// IsAvailable returns true if Appium is installed.
func (m *Manager) IsAvailable() bool {
	return m.appiumBin != ""
}

// Start launches an Appium server for a device session.
func (m *Manager) Start(sessionID string, serial string) (*Instance, error) {
	if m.appiumBin == "" {
		return nil, fmt.Errorf("appium not installed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Already running?
	if inst, ok := m.instances[sessionID]; ok {
		return inst, nil
	}

	// Find free port
	port, err := findFreePort(m.basePort)
	if err != nil {
		return nil, fmt.Errorf("appium: no free port: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.appiumBin,
		"--port", fmt.Sprintf("%d", port),
		"--default-capabilities",
		fmt.Sprintf(`{"platformName":"Android","appium:udid":"%s","appium:automationName":"UiAutomator2","appium:noReset":true}`, serial),
		"--relaxed-security",
		"--allow-insecure", "adb_shell",
		"--log-no-colors",
	)

	// Don't tie to parent process
	cmd.SysProcAttr = nil // inherits process group

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("appium: start failed: %w", err)
	}

	inst := &Instance{
		Port:         port,
		Serial:       serial,
		WebDriverURL: fmt.Sprintf("http://%s:%d", m.hostIP, port),
		cmd:          cmd,
		cancel:       cancel,
	}
	m.instances[sessionID] = inst

	// Wait for Appium to be ready
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
			if err == nil {
				conn.Close()
				log.Info().
					Int("port", port).
					Str("serial", serial).
					Str("session", sessionID).
					Msg("appium: server ready")
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		log.Warn().Int("port", port).Msg("appium: server did not become ready in 30s")
	}()

	log.Info().
		Int("port", port).
		Str("serial", serial).
		Str("url", inst.WebDriverURL).
		Msg("appium: starting server")

	return inst, nil
}

// Stop kills the Appium server for a session.
func (m *Manager) Stop(sessionID string) {
	m.mu.Lock()
	inst, ok := m.instances[sessionID]
	if ok {
		delete(m.instances, sessionID)
	}
	m.mu.Unlock()

	if ok && inst.cancel != nil {
		inst.cancel()
		if inst.cmd != nil && inst.cmd.Process != nil {
			inst.cmd.Process.Kill()
		}
		log.Info().Int("port", inst.Port).Str("session", sessionID).Msg("appium: server stopped")
	}
}

// StopAll kills all running Appium servers.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Stop(id)
	}
}

// Get returns the Appium instance for a session.
func (m *Manager) Get(sessionID string) *Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.instances[sessionID]
}

func findFreePort(startFrom int) (int, error) {
	for port := startFrom; port < startFrom+100; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port in range %d-%d", startFrom, startFrom+100)
}
