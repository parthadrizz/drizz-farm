// Package appium manages Appium server instances per device session.
// When a session is created, an Appium server starts on a unique port
// pointed at that device's ADB serial. The WebDriver URL is returned
// so any Appium test script can connect.
package appium

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Instance represents a running Appium server for one device.
//
// WebDriverURL is the URL EXTERNAL clients should use (built from the
// daemon's LAN IP so remote test runners on other hosts can talk to it
// directly when they want to bypass our /wd/hub proxy).
//
// LocalURL is what the daemon itself uses for its internal
// /wd/hub → Appium forwarding. Always 127.0.0.1 — Appium binds
// 0.0.0.0 so the loopback address works regardless of whether the
// daemon's cached LAN IP is still current. Previously we used the
// cached LAN IP for both; once the host moved networks (Wi-Fi
// switched, VPN toggled), the proxy forward timed out even though
// Appium was perfectly healthy on localhost.
type Instance struct {
	Port         int
	Serial       string
	WebDriverURL string // for external clients: http://<LAN IP>:port
	LocalURL     string // for in-process forwarding: http://127.0.0.1:port
	cmd          *exec.Cmd
	cancel       context.CancelFunc
}

// Manager manages Appium server instances.
type Manager struct {
	mu           sync.Mutex
	instances    map[string]*Instance // keyed by session/instance ID
	usedPorts    map[int]bool         // ports currently allocated to a session
	appiumBin    string
	basePort     int
	hostIP       string
	androidHome  string // exported into each spawned Appium's env
	javaHome     string // same; set from cfg.SDK.Java parent
}

// NewManager creates an Appium manager. sdkRoot + javaHome come from the
// daemon's resolved SDK paths and are forwarded into every spawned
// Appium process so the UiAutomator2 driver finds adb / aapt / emulator
// even when the daemon was started from launchd (no interactive shell,
// no $ANDROID_HOME) or from a terminal that doesn't export them.
//
// This is the single most annoying operator-side footgun in Appium:
// you install it, run your tests, and get cryptic "Neither ANDROID_HOME
// nor ANDROID_SDK_ROOT environment variable was exported" from inside
// the driver — even though you set it in .zshrc — because the server
// spawning Appium doesn't inherit your shell env. We set it explicitly.
func NewManager(hostIP, androidHome, javaHome string) *Manager {
	appiumBin, err := exec.LookPath("appium")
	if err != nil {
		log.Warn().Msg("appium: binary not found — Appium hosting disabled")
		return &Manager{
			hostIP:      hostIP,
			instances:   make(map[string]*Instance),
			usedPorts:   map[int]bool{},
			androidHome: androidHome,
			javaHome:    javaHome,
		}
	}
	log.Info().Str("bin", appiumBin).Str("android_home", androidHome).Str("java_home", javaHome).Msg("appium: found")
	return &Manager{
		appiumBin:   appiumBin,
		instances:   make(map[string]*Instance),
		usedPorts:   make(map[int]bool),
		basePort:    4723,
		hostIP:      hostIP,
		androidHome: androidHome,
		javaHome:    javaHome,
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

	// Find free port — skip ports we've already handed out but whose
	// servers may still be tearing down. Previously two concurrent
	// Start() calls would both probe from basePort, both see 4723 as
	// free (the probe listener was immediately closed), and both get
	// assigned 4723 → port collision → one wins, the others sit
	// waiting on "server did not become ready in 30s". Tracking the
	// used set under the same mutex that guards instances closes the
	// race.
	port, err := m.findFreePortLocked(m.basePort)
	if err != nil {
		return nil, fmt.Errorf("appium: no free port: %w", err)
	}
	m.usedPorts[port] = true

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, m.appiumBin,
		"--port", fmt.Sprintf("%d", port),
		"--default-capabilities",
		fmt.Sprintf(`{"platformName":"Android","appium:udid":"%s","appium:automationName":"UiAutomator2","appium:noReset":true}`, serial),
		"--relaxed-security",
		"--allow-insecure", "adb_shell",
		"--log-no-colors",
	)

	// Inherit current env + inject the SDK/JDK roots Appium's drivers
	// require. Without ANDROID_HOME in particular, UiAutomator2
	// driver fails createSession with "Neither ANDROID_HOME nor
	// ANDROID_SDK_ROOT environment variable was exported" — even when
	// appium itself started fine and answered on port.
	env := os.Environ()
	if m.androidHome != "" {
		env = append(env, "ANDROID_HOME="+m.androidHome)
		env = append(env, "ANDROID_SDK_ROOT="+m.androidHome)
	}
	if m.javaHome != "" {
		env = append(env, "JAVA_HOME="+m.javaHome)
	}
	cmd.Env = env

	cmd.SysProcAttr = nil // inherits process group

	if err := cmd.Start(); err != nil {
		delete(m.usedPorts, port)
		cancel()
		return nil, fmt.Errorf("appium: start failed: %w", err)
	}

	inst := &Instance{
		Port:         port,
		Serial:       serial,
		WebDriverURL: fmt.Sprintf("http://%s:%d", m.hostIP, port),
		LocalURL:     fmt.Sprintf("http://127.0.0.1:%d", port),
		cmd:          cmd,
		cancel:       cancel,
	}
	m.instances[sessionID] = inst

	log.Info().
		Int("port", port).
		Str("serial", serial).
		Str("url", inst.WebDriverURL).
		Msg("appium: starting server")

	// Release the manager lock while we wait for readiness — holding
	// it across a 30s TCP probe would serialize every concurrent
	// session's Start() call and make max_concurrent > 1 pointless.
	// usedPorts[port] has already been reserved, so a parallel caller
	// will pick a different port.
	m.mu.Unlock()
	ready := waitForPort(port, 30*time.Second)
	m.mu.Lock()

	if !ready {
		// Appium didn't come up. Unwire so the next session can try
		// this port again and the caller sees a real error instead
		// of a dangling "session created" with no endpoint behind it.
		delete(m.instances, sessionID)
		delete(m.usedPorts, port)
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		log.Warn().Int("port", port).Str("session", sessionID).Msg("appium: server did not become ready in 30s")
		return nil, fmt.Errorf("appium: server on port %d never became ready", port)
	}

	log.Info().
		Int("port", port).
		Str("serial", serial).
		Str("session", sessionID).
		Msg("appium: server ready")

	return inst, nil
}

// waitForPort polls TCP connect on 127.0.0.1:<port> until it succeeds
// or the deadline passes. Returns true if the port accepted a
// connection. Used by Start() to block the caller until Appium is
// actually listening — previously readiness was checked in a goroutine
// that did nothing with the result, so broker.Create would return an
// AppiumURL before the server was up and the first POST /session got
// "connection refused".
func waitForPort(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// Stop kills the Appium server for a session.
func (m *Manager) Stop(sessionID string) {
	m.mu.Lock()
	inst, ok := m.instances[sessionID]
	if ok {
		delete(m.instances, sessionID)
		delete(m.usedPorts, inst.Port)
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

// findFreePortLocked must be called with m.mu held. Skips ports that
// are either bound by something else OR reserved by another in-flight
// session in this manager — we can't rely on net.Listen probes alone
// because a port reserved a millisecond ago by another goroutine still
// looks free to the probe until the Appium child actually binds it.
func (m *Manager) findFreePortLocked(startFrom int) (int, error) {
	for port := startFrom; port < startFrom+100; port++ {
		if m.usedPorts[port] {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port in range %d-%d", startFrom, startFrom+100)
}
