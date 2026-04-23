package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/drizz-dev/drizz-farm/internal/buildinfo"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/daemon"
	"github.com/drizz-dev/drizz-farm/internal/discovery"
	"github.com/drizz-dev/drizz-farm/internal/telemetry"
)

var setupAutoInstall bool

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up drizz-farm — create or join a mesh, check prerequisites",
	Long: `Interactive setup wizard:
  1. Scans LAN for existing drizz-farm meshes
  2. Creates a new mesh or joins an existing one
  3. Checks prerequisites (Android SDK, Java, etc.)
  4. Saves config to ~/.drizz-farm/config.yaml

Run this once on a fresh machine before 'drizz-farm start'.`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().BoolVar(&setupAutoInstall, "install", false, "automatically install missing prerequisites")
	rootCmd.AddCommand(setupCmd)
}

func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  drizz-farm setup")
	fmt.Println("  ━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Groups are created/joined from the dashboard — not here.
	// Setup just creates a standalone node. User adds it to a group later.
	meshName, meshKey := "", ""
	_ = reader

	// --- Step 2: Prerequisites Check ---
	fmt.Println()
	fmt.Println("  Checking prerequisites...")
	fmt.Println()
	printHardwareCapacity()
	fmt.Println()

	allOK := EnsurePrereqs(true)
	if !allOK {
		fmt.Println("\n  Some prerequisites still missing. Fix manually and re-run 'drizz-farm setup'.")
	} else {
		fmt.Println("\n  ✓ All prerequisites installed")
	}

	// --- Step 3: Save Config ---
	if err := saveSetupConfig(meshName, meshKey); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("  ✓ Config saved")

	// --- Step 3.5: Register the install (mandatory email) ---
	if err := registerInstall(reader); err != nil {
		// Non-fatal — if the network is broken or the user aborted, keep going.
		// We'll still store what we got locally so heartbeats can pick it up later.
		log.Warn().Err(err).Msg("setup: registration had issues (continuing)")
	}

	// --- Step 4: Install as launchd service (auto-start on boot) ---
	if err := installAutoStart(); err != nil {
		log.Warn().Err(err).Msg("setup: failed to install auto-start (continuing)")
		fmt.Printf("  ⚠ Auto-start install failed: %v\n", err)
		fmt.Println("    You can run manually with 'drizz-farm start'")
	} else {
		fmt.Println("  ✓ Auto-start enabled (launchd)")
	}

	fmt.Println()
	fmt.Println("  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  ✓ drizz-farm is running")
	fmt.Println()
	hostname, _ := os.Hostname()
	localHost := hostname
	if !strings.HasSuffix(localHost, ".local") {
		localHost += ".local"
	}
	fmt.Printf("  URL: http://%s:9401\n", localHost)
	fmt.Println("  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	return nil
}

// registerInstall prompts for an email + org name and sends a one-shot
// signup event to api.drizz.ai. Email is mandatory — the prompt keeps
// asking until the user provides something that looks like an email.
// Everything else (org name) is optional. No verification step.
//
// If the user has already registered (install-id file exists AND a sibling
// "registered" marker exists), we skip this entirely — re-running setup
// on an existing install shouldn't re-prompt.
func registerInstall(reader *bufio.Reader) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")
	markerPath := filepath.Join(dataDir, ".registered")

	if _, err := os.Stat(markerPath); err == nil {
		// Already registered on a previous setup run — keep the existing record.
		return nil
	}

	installID, err := telemetry.GetOrCreateInstallID(dataDir)
	if err != nil {
		return fmt.Errorf("install-id: %w", err)
	}

	fmt.Println()
	fmt.Println("  Almost there. Tell us who's using drizz-farm — we'll")
	fmt.Println("  send occasional updates and a welcome email.")
	fmt.Println()

	// Email — mandatory.
	var email string
	for {
		fmt.Print("  → Email: ")
		raw, _ := reader.ReadString('\n')
		email = strings.TrimSpace(raw)
		if telemetry.ValidateEmail(email) {
			break
		}
		fmt.Println("    Please enter a valid email.")
	}

	// Org — optional, press enter to skip.
	fmt.Print("  → Team / company name (optional): ")
	rawOrg, _ := reader.ReadString('\n')
	org := strings.TrimSpace(rawOrg)

	hostname, _ := os.Hostname()
	telemetry.Signup(context.Background(), telemetry.SignupRequest{
		InstallID: installID,
		Email:     email,
		OrgName:   org,
		Hostname:  hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Version:   buildinfo.Version,
	})

	// Drop a marker so re-running setup doesn't re-prompt.
	_ = os.WriteFile(markerPath, []byte(email+"\n"), 0644)
	fmt.Printf("  ✓ Registered as %s\n", email)
	return nil
}

// installAutoStart installs drizz-farm as a launchd service.
func installAutoStart() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find binary: %w", err)
	}
	binaryPath, _ = filepath.Abs(binaryPath)

	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")
	configPath := filepath.Join(dataDir, "config.yaml")

	// Reinstall to pick up any binary path changes
	if daemon.IsLaunchdInstalled() {
		_ = daemon.UninstallLaunchd()
	}

	return daemon.InstallLaunchd(daemon.LaunchdConfig{
		BinaryPath: binaryPath,
		ConfigPath: configPath,
		DataDir:    dataDir,
		LogDir:     dataDir,
	})
}

// setupMesh handles the mesh setup step.
// Two options: standalone node or join an existing mesh.
// Mesh creation happens from the dashboard, not the CLI.
func setupMesh(reader *bufio.Reader) (string, string, error) {
	fmt.Println("  1. Create standalone node")
	fmt.Println("  2. Join a mesh")
	fmt.Println()

	for {
		fmt.Print("  → Choice: ")
		c, _ := reader.ReadString('\n')
		c = strings.TrimSpace(c)

		if c == "1" {
			fmt.Println("  ✓ Standalone node — create or join a mesh later from the dashboard")
			return "", "", nil
		}
		if c == "2" {
			return joinByAddress(reader)
		}
		fmt.Println("  Invalid choice, try again.")
	}
}

type discoveredMesh struct {
	name      string
	nodeCount int
	nodes     []discovery.Node
}

func discoverMeshes() []discoveredMesh {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Scan for any _drizz-*._tcp services on the network
	// Try common names and the "default" mesh
	// Scan for ALL drizz-farm nodes (empty mesh name = no filter)
	allNodes, err := discovery.BrowseMesh(ctx, 3*time.Second, "")
	if err != nil {
		return nil
	}

	// Group nodes by mesh name
	meshMap := make(map[string][]discovery.Node)
	for _, n := range allNodes {
		name := n.MeshName
		if name == "" {
			name = "default"
		}
		meshMap[name] = append(meshMap[name], n)
	}

	// Build sorted list (most nodes first)
	var meshes []discoveredMesh
	for name, nodes := range meshMap {
		meshes = append(meshes, discoveredMesh{name: name, nodeCount: len(nodes), nodes: nodes})
	}
	// Sort by node count descending
	for i := 0; i < len(meshes); i++ {
		for j := i + 1; j < len(meshes); j++ {
			if meshes[j].nodeCount > meshes[i].nodeCount {
				meshes[i], meshes[j] = meshes[j], meshes[i]
			}
		}
	}

	return meshes
}

func joinByAddress(reader *bufio.Reader) (string, string, error) {
	fmt.Print("  → Node address (hostname or IP, e.g. mac-mini.local:9401 or 192.168.1.10:9401): ")
	addr, _ := reader.ReadString('\n')
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", fmt.Errorf("address is required")
	}
	// Add default port if not specified
	if !strings.Contains(addr, ":") {
		addr = addr + ":9401"
	}

	// Fetch mesh info from the remote node
	fmt.Printf("  → Connecting to %s...\n", addr)
	cmd := exec.Command("curl", "-sf", "--max-time", "5", fmt.Sprintf("http://%s/api/v1/node/health", addr))
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("cannot reach %s — is drizz-farm running there?", addr)
	}

	// Parse mesh name from health response
	var health map[string]any
	if err := json.Unmarshal(out, &health); err != nil {
		return "", "", fmt.Errorf("invalid response from %s", addr)
	}
	meshInfo, _ := health["mesh"].(map[string]any)
	remoteMeshName := ""
	if meshInfo != nil {
		remoteMeshName, _ = meshInfo["name"].(string)
	}
	remoteNode, _ := health["node"].(string)

	if remoteMeshName == "" {
		remoteMeshName = remoteNode
	}

	fmt.Printf("  ✓ Found mesh \"%s\" on %s\n", remoteMeshName, remoteNode)
	fmt.Println()

	fmt.Printf("  → Mesh key for \"%s\": ", remoteMeshName)
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("mesh key is required")
	}

	// Verify key via handshake
	if !verifyMeshKey(addr, key) {
		return "", "", fmt.Errorf("invalid mesh key — handshake failed")
	}

	fmt.Printf("  ✓ Handshake successful — joined \"%s\"\n", remoteMeshName)
	return remoteMeshName, key, nil
}

func createNewMesh(reader *bufio.Reader) (string, string, error) {
	fmt.Print("  → Mesh name: ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}

	// Generate a random key
	key := generateMeshKey()

	fmt.Println()
	fmt.Printf("  ✓ Mesh \"%s\" created\n", name)
	fmt.Printf("  ✓ Key: %s\n", key)
	fmt.Println()
	fmt.Println("  Share this key with other nodes to join this mesh.")

	return name, key, nil
}

func joinExistingMesh(reader *bufio.Reader, mesh discoveredMesh) (string, string, error) {
	fmt.Printf("\n  → Mesh key for \"%s\": ", mesh.name)
	key, _ := reader.ReadString('\n')
	key = strings.TrimSpace(key)

	if key == "" {
		return "", "", fmt.Errorf("mesh key is required")
	}

	// Verify key by handshaking with a node in the mesh
	verified := false
	for _, node := range mesh.nodes {
		addr := fmt.Sprintf("%s:%d", node.Host, node.Port)
		if verifyMeshKey(addr, key) {
			verified = true
			break
		}
	}

	if !verified {
		return "", "", fmt.Errorf("invalid mesh key — no node accepted the handshake")
	}

	fmt.Println()
	fmt.Printf("  ✓ Handshake successful\n")
	fmt.Printf("  ✓ Joined mesh \"%s\" (%d node(s))\n", mesh.name, mesh.nodeCount)

	// Offer to import config from an existing node
	if len(mesh.nodes) > 0 {
		fmt.Println()
		fmt.Println("  → Import pool config from existing node?")
		for i, n := range mesh.nodes {
			fmt.Printf("    %d. %s (%s)\n", i+1, n.Name, n.Host)
		}
		fmt.Printf("    %d. Configure manually\n", len(mesh.nodes)+1)
		fmt.Println()
		fmt.Print("  → Choice: ")
		choiceStr, _ := reader.ReadString('\n')
		choiceStr = strings.TrimSpace(choiceStr)
		importChoice := 0
		fmt.Sscanf(choiceStr, "%d", &importChoice)

		if importChoice >= 1 && importChoice <= len(mesh.nodes) {
			node := mesh.nodes[importChoice-1]
			if err := importConfigFromPeer(node); err != nil {
				fmt.Printf("  ⚠ Config import failed: %v (using defaults)\n", err)
			} else {
				fmt.Printf("  ✓ Config imported from %s\n", node.Name)
			}
		}
	}

	return mesh.name, key, nil
}

func verifyMeshKey(nodeAddr, key string) bool {
	body := fmt.Sprintf(`{"mesh_key":"%s"}`, key)
	cmd := exec.Command("curl", "-sf", "--max-time", "5", "-X", "POST",
		fmt.Sprintf("http://%s/api/v1/federation/handshake", nodeAddr),
		"-H", "Content-Type: application/json",
		"-d", body)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "ok")
}

func importConfigFromPeer(node discovery.Node) error {
	// Fetch pool config from the peer's API
	addr := fmt.Sprintf("http://%s:%d/api/v1/config", node.Host, node.Port)
	cmd := exec.Command("curl", "-sf", addr)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("fetch config from %s: %w", node.Name, err)
	}

	// Parse and apply pool settings
	var peerCfg map[string]any
	if err := yaml.Unmarshal(out, &peerCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// We'll use the peer's pool config but not overwrite mesh/node settings
	fmt.Printf("  ✓ Imported pool settings from %s\n", node.Name)
	return nil
}

func generateMeshKey() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func saveSetupConfig(meshName, meshKey string) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")
	os.MkdirAll(dataDir, 0755)
	cfgPath := filepath.Join(dataDir, "config.yaml")

	// Load existing config if present, or create new
	var cfg config.Config
	if data, err := os.ReadFile(cfgPath); err == nil {
		yaml.Unmarshal(data, &cfg)
	}

	// Apply mesh settings
	cfg.Mesh.Name = meshName
	cfg.Mesh.Key = meshKey

	// Detect and store SDK paths — done ONCE, used everywhere
	//
	// Order matters: Java → JAVA_HOME → then SDK tools (they need Java)
	// Each tool: find → verify it runs → if broken, install → verify again

	// Step 1: Java (everything depends on this)
	javac := findBinary("javac")
	if javac != "" {
		cfg.SDK.Java = filepath.Join(filepath.Dir(javac), "java")
		if _, err := os.Stat(cfg.SDK.Java); err != nil {
			cfg.SDK.Java = findBinary("java")
		}
	} else {
		cfg.SDK.Java = findBinary("java")
	}
	if cfg.SDK.Java != "" {
		javaHome := filepath.Dir(filepath.Dir(cfg.SDK.Java))
		os.Setenv("JAVA_HOME", javaHome)
		fmt.Printf("  → JAVA_HOME=%s\n", javaHome)
	}

	// Step 2: SDK root + tools that DON'T need Java (adb, emulator)
	cfg.SDK.Root = findAndroidSDK()
	cfg.SDK.ADB = findBinary("adb")
	cfg.SDK.Emulator = findBinary("emulator")

	// Step 3: Tools that NEED Java (avdmanager, sdkmanager)
	// Only verify these if Java is available
	if cfg.SDK.Java != "" {
		cfg.SDK.AVDManager = findAndVerify("avdmanager")
		cfg.SDK.SDKManager = findAndVerify("sdkmanager")
	} else {
		// Just find them (file exists), can't verify without Java
		cfg.SDK.AVDManager = findBinaryNoInstall("avdmanager")
		cfg.SDK.SDKManager = findBinaryNoInstall("sdkmanager")
		if cfg.SDK.AVDManager != "" || cfg.SDK.SDKManager != "" {
			fmt.Println("  ⚠ avdmanager/sdkmanager found but can't verify (Java missing)")
		}
	}

	// Apply defaults for empty fields
	if cfg.Pool.MaxConcurrent == 0 {
		// Auto-detect based on hardware
		cpus := runtime.NumCPU()
		maxByCPU := cpus / 2
		if maxByCPU < 1 {
			maxByCPU = 1
		}
		if maxByCPU > 6 {
			maxByCPU = 6
		}
		cfg.Pool.MaxConcurrent = maxByCPU
	}
	if cfg.API.Port == 0 {
		cfg.API.Port = 9401
	}
	if cfg.API.Host == "" {
		cfg.API.Host = "0.0.0.0"
	}

	// Ensure at least a default profile exists
	if cfg.Pool.Profiles.Android == nil {
		cfg.Pool.Profiles.Android = map[string]config.AndroidProfile{
			"default": {
				RAMMB:              2048,
				HeapMB:             512,
				DiskSizeMB:         4096,
				GPU:                "host",
				Snapshot:            true,
				BootTimeoutSeconds: 120,
			},
		}
	}

	// Populate every unset field with its real default BEFORE we
	// serialize. Without this, zero-valued fields land in the yaml as
	// `session_timeout_minutes: 0` etc., which reads like a broken
	// config — runtime code falls back to defaults-on-read, but the
	// file on disk looks wrong. ApplyDefaults is idempotent so any
	// value we set earlier (SDK paths, MaxConcurrent) survives.
	config.ApplyDefaults(&cfg)

	// Write config
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	header := fmt.Sprintf("# drizz-farm configuration\n# Mesh: %s\n# Generated by 'drizz-farm setup'\n\n", meshName)
	return os.WriteFile(cfgPath, []byte(header+string(data)), 0644)
}

// --- Prerequisite checks (unchanged) ---

type checkResult struct {
	name   string
	ok     bool
	path   string
	detail string
	fixCmd string
}

func checkPkgMgr() checkResult {
	switch runtime.GOOS {
	case "darwin":
		return checkBrew()
	case "linux":
		for _, pm := range []string{"apt-get", "dnf", "yum", "pacman"} {
			if p, err := exec.LookPath(pm); err == nil {
				return checkResult{name: "Package manager", ok: true, path: p, detail: pm}
			}
		}
		return checkResult{name: "Package manager", detail: "none found (need apt/dnf/yum/pacman)"}
	default:
		return checkResult{name: "Package manager", detail: "unsupported OS"}
	}
}

func checkBrew() checkResult {
	r := checkResult{name: "Homebrew"}
	// Try PATH first. If the user just installed brew in the same
	// session, PATH inside this process may not reflect the shell rc
	// change yet — fall back to the standard install locations so we
	// don't falsely flag a working brew as missing.
	if path, err := exec.LookPath("brew"); err == nil {
		r.ok = true
		r.path = path
		r.detail = path
		return r
	}
	for _, candidate := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(candidate); err == nil {
			r.ok = true
			r.path = candidate
			r.detail = candidate + " (not on PATH — open a new shell to pick it up)"
			return r
		}
	}
	r.detail = "not found"
	r.fixCmd = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	return r
}

func checkJDK() checkResult {
	r := checkResult{name: "Java JDK"}
	p := findBinary("javac")
	if p == "" {
		r.detail = "not found"
		r.fixCmd = "brew install openjdk@17"
		return r
	}
	// Apple's /usr/bin/javac is a stub that hangs or pops a GUI install
	// dialog on machines without a real JDK. Timebox the version probe
	// so setup never freezes — if javac doesn't respond in 3s, treat it
	// as not installed and let the auto-install flow take over.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, "-version").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		r.detail = fmt.Sprintf("not responding (stub at %s) — install a real JDK", p)
		r.fixCmd = "brew install openjdk@17"
		return r
	}
	if err != nil {
		r.detail = fmt.Sprintf("broken (%s)", p)
		return r
	}
	r.ok = true
	r.path = p
	r.detail = fmt.Sprintf("%s (%s)", strings.TrimSpace(string(out)), p)
	return r
}

func checkAndroidSDK() checkResult {
	r := checkResult{name: "Android SDK"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "not found (set ANDROID_HOME)"
		r.fixCmd = "brew install --cask android-commandlinetools"
		return r
	}
	r.ok = true
	r.path = sdkRoot
	r.detail = sdkRoot
	return r
}

// needsRunVerification lists binaries known to have macOS stubs that look real but aren't.
// verifyBinaryWorks checks if a binary actually works.
// Only java/javac need runtime verification (macOS stubs).
// Everything else — file exists = good enough.
func verifyBinaryWorks(path string) bool {
	name := filepath.Base(path)
	if name != "java" && name != "javac" {
		return true
	}
	// Java/javac: run to catch macOS stubs. Apple's /usr/bin/{java,javac}
	// is a stub that normally errors fast but can hang indefinitely on
	// some macOS versions (waiting for a GUI install prompt that never
	// shows). Timebox every probe so setup never freezes.
	for _, flag := range []string{"-version", "--version"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, path, flag)
		err := cmd.Run()
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// appendJavaHome adds JAVA_HOME to env if we've already found java.
// This is needed because sdkmanager/avdmanager scripts look for JAVA_HOME.
func appendJavaHome(env []string) []string {
	// Check if already set
	for _, e := range env {
		if strings.HasPrefix(e, "JAVA_HOME=") {
			return env
		}
	}
	// Find java binary and derive JAVA_HOME
	javaPath := findBinaryNoInstall("java")
	if javaPath == "" {
		return env
	}
	// java is at <java_home>/bin/java
	javaHome := filepath.Dir(filepath.Dir(javaPath))
	return append(env, "JAVA_HOME="+javaHome)
}

// findBinaryNoInstall searches for a binary without attempting to install.
// For java/javac we:
//   - Prefer homebrew/JVM paths first, so we never derive JAVA_HOME from
//     Apple's /usr/bin stub (even when the stub "works" via delegation,
//     JAVA_HOME=/usr is garbage that breaks sdkmanager).
//   - Timebox every probe at 3s so a hanging stub can't freeze the
//     search.
func findBinaryNoInstall(name string) string {
	probe := func(p string) bool {
		if name != "java" && name != "javac" {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, p, "-version").Run() == nil
	}

	// For java/javac, try the real JDK paths BEFORE falling back to the
	// stub at /usr/bin/{java,javac}. The stub may appear to work, but
	// derived JAVA_HOME is wrong.
	if name == "java" || name == "javac" {
		for _, p := range commonSearchPaths(name) {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if probe(p) {
				return p
			}
		}
		// last resort: PATH lookup (may still pick up the stub,
		// but at least we tried the real paths first)
		if p, err := exec.LookPath(name); err == nil && probe(p) {
			return p
		}
		return ""
	}

	// Non-Java binaries: LookPath first (PATH is usually the right answer).
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, p := range commonSearchPaths(name) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// envForBinary maps binary names to the env var that should point to their parent.
var envForBinary = map[string]string{
	"adb":        "ANDROID_HOME",
	"emulator":   "ANDROID_HOME",
	"avdmanager": "ANDROID_HOME",
	"sdkmanager": "ANDROID_HOME",
	"javac":      "JAVA_HOME",
	"java":       "JAVA_HOME",
}

// installCmdForBinary maps binary names to the brew install command.
var installCmdForBinary = map[string]string{
	"adb":        "brew install --cask android-platform-tools",
	"emulator":   "brew install --cask android-commandlinetools && sdkmanager --install emulator",
	"avdmanager": "brew install --cask android-commandlinetools",
	"sdkmanager": "brew install --cask android-commandlinetools",
	"javac":      "brew install --cask temurin || brew install openjdk",
	"java":       "brew install --cask temurin || brew install openjdk",
	"brew":       `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`,
}

var alreadyInstalled = map[string]bool{}

// findAndVerify finds a binary, runs it to verify, installs if broken.
func findAndVerify(name string) string {
	// Find it
	p := findBinaryNoInstall(name)
	if p == "" {
		// Not found → install
		return findBinary(name)
	}
	// Found → verify it actually runs (with JAVA_HOME available).
	// Timeboxed at 5s so a broken Java script can't hang setup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "--version")
	cmd.Env = appendJavaHome(os.Environ())
	if err := cmd.Run(); err != nil {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		cmd2 := exec.CommandContext(ctx2, p, "-version")
		cmd2.Env = appendJavaHome(os.Environ())
		if err2 := cmd2.Run(); err2 != nil {
			// Found but broken → reinstall
			fmt.Printf("  ⚠ %s found at %s but broken — reinstalling\n", name, p)
			alreadyInstalled[name] = false // allow reinstall
			return findBinary(name)
		}
	}
	return p
}

// findBinary finds a binary by:
// 1. Checking the relevant env var (ANDROID_HOME, JAVA_HOME, etc.)
// 2. Checking PATH
// 3. Searching common locations
// If not found, installs it (once) and searches again.
func findBinary(name string) string {
	// 1. Check env var
	if envVar, ok := envForBinary[name]; ok {
		if root := os.Getenv(envVar); root != "" {
			// Derive binary path from env root
			candidates := binaryPathsFromRoot(root, name)
			for _, p := range candidates {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}

	// For java/javac: prefer real JDK locations BEFORE PATH. Apple's
	// /usr/bin stub can "work" (delegates to an installed JDK) but
	// deriving JAVA_HOME from /usr/bin breaks sdkmanager. Always pick
	// the real homebrew/JVM path when present.
	if name == "java" || name == "javac" {
		for _, p := range commonSearchPaths(name) {
			if _, err := os.Stat(p); err == nil {
				if verifyBinaryWorks(p) {
					return p
				}
			}
		}
	}

	// 2. Check PATH — verify it actually runs
	if p, err := exec.LookPath(name); err == nil {
		if verifyBinaryWorks(p) {
			return p
		}
	}

	// 3. Search common locations — verify each one works
	for _, p := range commonSearchPaths(name) {
		if _, err := os.Stat(p); err == nil {
			if verifyBinaryWorks(p) {
				return p
			}
		}
	}

	// Not found. We DO NOT auto-install here — that used to cause
	// silent multi-minute brew installs during the "Checking..." phase
	// with no user feedback. Instead the caller (checkJDK, etc.) reports
	// the miss with a fixCmd, and setup's install phase runs it visibly
	// after all checks finish.
	return ""
}

// binaryPathsFromRoot returns candidate paths for a binary given a root dir.
func binaryPathsFromRoot(root, name string) []string {
	paths := []string{
		filepath.Join(root, "bin", name),
		filepath.Join(root, "platform-tools", name),
		filepath.Join(root, "emulator", name),
		filepath.Join(root, "cmdline-tools", "latest", "bin", name),
	}
	// Check versioned cmdline-tools
	entries, _ := os.ReadDir(filepath.Join(root, "cmdline-tools"))
	for _, e := range entries {
		if e.IsDir() {
			paths = append(paths, filepath.Join(root, "cmdline-tools", e.Name(), "bin", name))
		}
	}
	return paths
}

// commonSearchPaths returns all common locations to search for a binary.
func commonSearchPaths(name string) []string {
	home, _ := os.UserHomeDir()
	sdkRoots := []string{
		filepath.Join(home, "Library", "Android", "sdk"),
		"/Library/Android/sdk",
		"/opt/android-sdk",
	}

	var paths []string
	for _, sdk := range sdkRoots {
		paths = append(paths, binaryPathsFromRoot(sdk, name)...)
	}

	// Brew locations
	paths = append(paths,
		filepath.Join("/opt/homebrew/bin", name),
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/opt/homebrew/share/android-commandlinetools/cmdline-tools/latest/bin", name),
		filepath.Join("/opt/homebrew/Caskroom/android-commandlinetools/latest/cmdline-tools/latest/bin", name),
	)

	// Java-specific
	if name == "javac" || name == "java" {
		paths = append(paths,
			// Homebrew keg-only installs (openjdk@17, openjdk@21, openjdk)
			filepath.Join("/opt/homebrew/opt/openjdk@17/bin", name),
			filepath.Join("/opt/homebrew/opt/openjdk@21/bin", name),
			filepath.Join("/opt/homebrew/opt/openjdk/bin", name),
			filepath.Join("/usr/local/opt/openjdk@17/bin", name),
			filepath.Join("/usr/local/opt/openjdk@21/bin", name),
			filepath.Join("/usr/local/opt/openjdk/bin", name),
			filepath.Join("/Library/Java/JavaVirtualMachines/openjdk.jdk/Contents/Home/bin", name),
		)
		// Ask brew itself where openjdk lives — handles custom prefixes,
		// versioned subkegs, and anything we haven't hardcoded.
		for _, brewBin := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if _, err := os.Stat(brewBin); err != nil {
				continue
			}
			for _, formula := range []string{"openjdk@17", "openjdk@21", "openjdk"} {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				out, err := exec.CommandContext(ctx, brewBin, "--prefix", formula).Output()
				cancel()
				if err != nil {
					continue
				}
				prefix := strings.TrimSpace(string(out))
				if prefix != "" {
					paths = append(paths,
						filepath.Join(prefix, "bin", name),
						filepath.Join(prefix, "libexec", "openjdk.jdk", "Contents", "Home", "bin", name),
					)
				}
			}
			break
		}
		// Search all JVMs
		jvmDir := "/Library/Java/JavaVirtualMachines"
		entries, _ := os.ReadDir(jvmDir)
		for _, e := range entries {
			if e.IsDir() {
				paths = append(paths, filepath.Join(jvmDir, e.Name(), "Contents", "Home", "bin", name))
			}
		}
		// Brew Cellar glob — catch any version
		for _, cellar := range []string{"/opt/homebrew/Cellar", "/usr/local/Cellar"} {
			for _, formula := range []string{"openjdk@17", "openjdk@21", "openjdk"} {
				matches, _ := filepath.Glob(filepath.Join(cellar, formula, "*", "bin", name))
				paths = append(paths, matches...)
				matches2, _ := filepath.Glob(filepath.Join(cellar, formula, "*", "libexec", "openjdk.jdk", "Contents", "Home", "bin", name))
				paths = append(paths, matches2...)
			}
		}
	}

	return paths
}


func checkAndroidCmdlineTools() checkResult {
	r := checkResult{name: "Android cmdline-tools"}
	p := findBinary("avdmanager")
	if p == "" {
		r.detail = "not found"
		return r
	}
	r.ok = true
	r.path = p
	r.detail = p
	return r
}

func checkAndroidPlatformTools() checkResult {
	r := checkResult{name: "Android platform-tools (adb)"}
	adb := findBinary("adb")
	if adb == "" {
		sdkmanager := findBinary("sdkmanager")
		if sdkmanager != "" {
			r.fixCmd = fmt.Sprintf("%s --install 'platform-tools'", sdkmanager)
		} else {
			r.fixCmd = "brew install --cask android-platform-tools"
		}
		r.detail = "not installed"
		return r
	}
	// Timeboxed — a corrupt/broken adb has been known to spin on first
	// invocation while it tries to spawn the daemon.
	ctxAdb, cancelAdb := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAdb()
	out, _ := exec.CommandContext(ctxAdb, adb, "version").CombinedOutput()
	lines := strings.Split(string(out), "\n")
	ver := ""
	if len(lines) > 0 {
		ver = strings.TrimSpace(lines[0])
	}
	r.ok = true
	r.path = adb
	r.detail = fmt.Sprintf("%s (%s)", adb, ver)
	return r
}

func checkAndroidEmulator() checkResult {
	r := checkResult{name: "Android Emulator"}
	emulator := findBinary("emulator")
	if emulator == "" {
		sdkmanager := findBinary("sdkmanager")
		if sdkmanager != "" {
			r.fixCmd = fmt.Sprintf("%s --install 'emulator'", sdkmanager)
		} else {
			r.fixCmd = "brew install --cask android-commandlinetools && sdkmanager --install 'emulator'"
		}
		r.detail = "not installed"
		return r
	}
	// Timeboxed — `emulator -version` is normally <1s but a broken
	// install could stall (e.g. missing libs prompting a dialog).
	ctxEmu, cancelEmu := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelEmu()
	out, _ := exec.CommandContext(ctxEmu, emulator, "-version").CombinedOutput()
	ver := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "emulator version") {
			ver = strings.TrimSpace(line)
			break
		}
	}
	r.ok = true
	r.path = emulator
	r.detail = fmt.Sprintf("%s (%s)", emulator, ver)
	return r
}

func checkAndroidSystemImages() checkResult {
	r := checkResult{name: "Android System Images"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "SDK not found"
		return r
	}
	sdkmanager := findBinaryNoInstall("sdkmanager")
	if sdkmanager == "" {
		r.detail = "sdkmanager not found (install cmdline-tools first)"
		return r
	}
	// sdkmanager is a shell script that shells out to `java`. On a Mac
	// with no real JDK, that hits Apple's /usr/bin/java stub and hangs.
	// Timebox the probe so setup never freezes; treat timeout/error as
	// "no images" and let the user install them from the dashboard.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sdkmanager, "--sdk_root="+sdkRoot, "--list_installed")
	cmd.Env = appendJavaHome(os.Environ())
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		r.detail = "sdkmanager not responding (install JDK + retry)"
		return r
	}
	if err != nil {
		r.detail = "sdkmanager failed (Java may be missing)"
		return r
	}
	var images []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "system-images;") {
			parts := strings.SplitN(line, "|", 2)
			images = append(images, strings.TrimSpace(parts[0]))
		}
	}
	if len(images) == 0 {
		r.detail = "none installed (install from dashboard after start)"
		r.fixCmd = fmt.Sprintf("yes | %s --sdk_root=%s --install 'system-images;android-35;google_apis;arm64-v8a'", sdkmanager, sdkRoot)
		return r
	}
	r.ok = true
	r.detail = fmt.Sprintf("%d image(s)", len(images))
	return r
}

func checkXcodeCLI() checkResult {
	r := checkResult{name: "Xcode CLI Tools"}
	out, err := exec.Command("xcode-select", "-p").CombinedOutput()
	if err != nil {
		r.detail = "not installed"
		r.fixCmd = "xcode-select --install"
		return r
	}
	r.ok = true
	r.path = strings.TrimSpace(string(out))
	r.detail = r.path
	return r
}

func printHardwareCapacity() {
	cpus := runtime.NumCPU()
	totalRAMGB := 0
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
		if err == nil {
			var memBytes int64
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &memBytes)
			totalRAMGB = int(memBytes / (1024 * 1024 * 1024))
		}
	}
	freeDiskGB := 0
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("df", "-g", "/").CombinedOutput()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				if len(fields) > 3 {
					fmt.Sscanf(fields[3], "%d", &freeDiskGB)
				}
			}
		}
	}

	usableRAM := totalRAMGB - 4
	if usableRAM < 0 {
		usableRAM = 0
	}
	maxByRAM := usableRAM * 1000 / 2500
	maxByCPU := cpus / 2
	if maxByCPU < 1 {
		maxByCPU = 1
	}
	recommended := maxByRAM
	if maxByCPU < recommended {
		recommended = maxByCPU
	}
	if recommended > 12 {
		recommended = 12
	}

	fmt.Println("  Hardware:")
	if totalRAMGB > 0 {
		fmt.Printf("    RAM:          %d GB (~%d emulators)\n", totalRAMGB, maxByRAM)
	}
	fmt.Printf("    CPUs:         %d cores (~%d emulators)\n", cpus, maxByCPU)
	if freeDiskGB > 0 {
		fmt.Printf("    Disk free:    %d GB (~%d AVDs)\n", freeDiskGB, freeDiskGB/5)
	}
	fmt.Printf("    Recommended:  %d concurrent emulators\n", recommended)
}

func findAndroidSDK() string {
	// 1. Check env vars first
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(env); root != "" {
			if _, err := os.Stat(root); err == nil {
				return root
			}
		}
	}

	// 2. Try to find adb/emulator in PATH and derive SDK root
	if adbPath, err := exec.LookPath("adb"); err == nil {
		// adb is at <sdk>/platform-tools/adb
		sdkRoot := filepath.Dir(filepath.Dir(adbPath))
		if _, err := os.Stat(filepath.Join(sdkRoot, "platform-tools")); err == nil {
			return sdkRoot
		}
	}
	if emuPath, err := exec.LookPath("emulator"); err == nil {
		// emulator is at <sdk>/emulator/emulator
		sdkRoot := filepath.Dir(filepath.Dir(emuPath))
		if _, err := os.Stat(filepath.Join(sdkRoot, "emulator")); err == nil {
			return sdkRoot
		}
	}

	// 3. Check common locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Android", "sdk"),
		filepath.Join(home, "Android", "Sdk"),
		"/Library/Android/sdk",
		"/opt/android-sdk",
		"/usr/local/android-sdk",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
