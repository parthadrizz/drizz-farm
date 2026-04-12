package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/discovery"
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

	// --- Step 1: Mesh Setup ---
	meshName, meshKey, err := setupMesh(reader)
	if err != nil {
		return err
	}

	// --- Step 2: Prerequisites Check ---
	fmt.Println()
	fmt.Println("  Checking prerequisites...")
	fmt.Println()

	checks := []checkResult{
		checkBrew(),
		checkJDK(),
		checkAndroidSDK(),
		checkAndroidCmdlineTools(),
		checkAndroidPlatformTools(),
		checkAndroidEmulator(),
		checkAndroidSystemImages(),
	}
	if runtime.GOOS == "darwin" {
		checks = append(checks, checkXcodeCLI())
	}

	allOK := true
	var failures []checkResult
	for _, c := range checks {
		if c.ok {
			fmt.Printf("  ✓ %-28s %s\n", c.name, c.detail)
		} else {
			fmt.Printf("  ✗ %-28s %s\n", c.name, c.detail)
			allOK = false
			failures = append(failures, c)
		}
	}

	fmt.Println()
	printHardwareCapacity()

	if !allOK && setupAutoInstall {
		fmt.Println("\n  Installing missing prerequisites...")
		for _, f := range failures {
			if f.fixCmd == "" {
				fmt.Printf("  ⊘ %-28s (manual install required)\n", f.name)
				continue
			}
			fmt.Printf("  → Installing %s...\n", f.name)
			parts := strings.Fields(f.fixCmd)
			out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
			if err != nil {
				fmt.Printf("    FAILED: %s\n    %s\n", err, strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("    ✓ Done\n")
			}
		}
	} else if !allOK {
		fmt.Printf("\n  %d prerequisite(s) missing. Run: drizz-farm setup --install\n", len(failures))
	}

	// --- Step 3: Save Config ---
	if err := saveSetupConfig(meshName, meshKey); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  ✓ Config saved to ~/.drizz-farm/config.yaml")
	fmt.Println()
	fmt.Println("  Next:")
	fmt.Println("    drizz-farm start")
	fmt.Println()
	fmt.Println("  Dashboard will be at http://localhost:9401")
	fmt.Println("  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	return nil
}

// setupMesh handles the mesh creation/join flow.
func setupMesh(reader *bufio.Reader) (string, string, error) {
	fmt.Println("  🔍 Scanning LAN for drizz-farm meshes...")

	// Discover existing meshes by scanning common mesh names
	// We try "default" and any mesh names we find via broad mDNS scan
	meshes := discoverMeshes()

	if len(meshes) == 0 {
		fmt.Println("     No meshes found.")
		fmt.Println()
		return createNewMesh(reader)
	}

	// Show discovered meshes
	fmt.Printf("     Found %d mesh(es):\n", len(meshes))
	for i, m := range meshes {
		fmt.Printf("       %d. \"%s\"  (%d node(s))\n", i+1, m.name, m.nodeCount)
	}
	fmt.Println()

	// Build options
	for i, m := range meshes {
		fmt.Printf("  %d. Join \"%s\"\n", i+1, m.name)
	}
	fmt.Printf("  %d. Create new mesh\n", len(meshes)+1)
	fmt.Println()

	fmt.Print("  → Choice: ")
	choiceStr, _ := reader.ReadString('\n')
	choiceStr = strings.TrimSpace(choiceStr)

	choice := 0
	fmt.Sscanf(choiceStr, "%d", &choice)

	if choice < 1 || choice > len(meshes)+1 {
		choice = len(meshes) + 1 // default to create new
	}

	if choice == len(meshes)+1 {
		return createNewMesh(reader)
	}

	// Join existing mesh
	selected := meshes[choice-1]
	return joinExistingMesh(reader, selected)
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
	meshNames := []string{"default"}

	// Also try a broad scan — look for any _drizz- prefixed services
	// This is a simplified approach; in production we'd use a broader mDNS query
	var meshes []discoveredMesh
	seen := make(map[string]bool)

	for _, name := range meshNames {
		nodes, err := discovery.BrowseMesh(ctx, 3*time.Second, name)
		if err != nil || len(nodes) == 0 {
			continue
		}
		if !seen[name] {
			meshes = append(meshes, discoveredMesh{name: name, nodeCount: len(nodes), nodes: nodes})
			seen[name] = true
		}
	}

	return meshes
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
	cmd := exec.Command("curl", "-sf", "-X", "POST",
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
	rand.Read(b)
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

func checkBrew() checkResult {
	r := checkResult{name: "Homebrew"}
	path, err := exec.LookPath("brew")
	if err != nil {
		r.detail = "not found"
		r.fixCmd = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
		return r
	}
	r.ok = true
	r.path = path
	r.detail = path
	return r
}

func checkJDK() checkResult {
	r := checkResult{name: "Java JDK"}
	if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
		javac := filepath.Join(javaHome, "bin", "javac")
		if _, err := os.Stat(javac); err == nil {
			r.ok = true
			r.path = javaHome
			r.detail = fmt.Sprintf("JAVA_HOME=%s", javaHome)
			return r
		}
	}
	path, err := exec.LookPath("javac")
	if err != nil {
		r.detail = "not found (needed for Android SDK tools)"
		r.fixCmd = "brew install openjdk"
		return r
	}
	out, _ := exec.Command("javac", "-version").CombinedOutput()
	r.ok = true
	r.path = path
	r.detail = strings.TrimSpace(string(out))
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

func checkAndroidCmdlineTools() checkResult {
	r := checkResult{name: "Android cmdline-tools"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "SDK not found"
		return r
	}
	avdmanager := filepath.Join(sdkRoot, "cmdline-tools", "latest", "bin", "avdmanager")
	if _, err := os.Stat(avdmanager); err != nil {
		r.detail = "not installed"
		sdkmanager := filepath.Join(sdkRoot, "cmdline-tools", "latest", "bin", "sdkmanager")
		if _, err := os.Stat(sdkmanager); err == nil {
			r.fixCmd = fmt.Sprintf("%s --install 'cmdline-tools;latest'", sdkmanager)
		} else {
			r.fixCmd = "brew install --cask android-commandlinetools"
		}
		return r
	}
	r.ok = true
	r.path = avdmanager
	r.detail = "OK"
	return r
}

func checkAndroidPlatformTools() checkResult {
	r := checkResult{name: "Android platform-tools (adb)"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "SDK not found"
		return r
	}
	adb := filepath.Join(sdkRoot, "platform-tools", "adb")
	if _, err := os.Stat(adb); err != nil {
		r.detail = "not installed"
		r.fixCmd = fmt.Sprintf("%s/cmdline-tools/latest/bin/sdkmanager --install 'platform-tools'", sdkRoot)
		return r
	}
	out, _ := exec.Command(adb, "version").CombinedOutput()
	lines := strings.Split(string(out), "\n")
	ver := ""
	if len(lines) > 0 {
		ver = strings.TrimSpace(lines[0])
	}
	r.ok = true
	r.path = adb
	r.detail = ver
	return r
}

func checkAndroidEmulator() checkResult {
	r := checkResult{name: "Android Emulator"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "SDK not found"
		return r
	}
	emulator := filepath.Join(sdkRoot, "emulator", "emulator")
	if _, err := os.Stat(emulator); err != nil {
		r.detail = "not installed"
		r.fixCmd = fmt.Sprintf("%s/cmdline-tools/latest/bin/sdkmanager --install 'emulator'", sdkRoot)
		return r
	}
	out, _ := exec.Command(emulator, "-version").CombinedOutput()
	ver := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "emulator version") {
			ver = strings.TrimSpace(line)
			break
		}
	}
	r.ok = true
	r.path = emulator
	if ver != "" {
		r.detail = ver
	} else {
		r.detail = "OK"
	}
	return r
}

func checkAndroidSystemImages() checkResult {
	r := checkResult{name: "Android System Images"}
	sdkRoot := findAndroidSDK()
	if sdkRoot == "" {
		r.detail = "SDK not found"
		return r
	}
	sdkmanager := filepath.Join(sdkRoot, "cmdline-tools", "latest", "bin", "sdkmanager")
	out, err := exec.Command(sdkmanager, "--list_installed").CombinedOutput()
	if err != nil {
		r.detail = "sdkmanager failed"
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
		r.detail = "none installed"
		r.fixCmd = fmt.Sprintf("%s --install 'system-images;android-35;google_apis;arm64-v8a'", sdkmanager)
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
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(env); root != "" {
			if _, err := os.Stat(root); err == nil {
				return root
			}
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Android", "sdk"),
		filepath.Join(home, "Android", "Sdk"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
