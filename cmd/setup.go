package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var setupAutoInstall bool

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Check and install prerequisites for drizz-farm",
	Long: `Verifies that all required tools are installed and configured:
  - Homebrew (macOS)
  - Java JDK (for Android SDK tools)
  - Android SDK (command-line tools, platform-tools, emulator)
  - Xcode Command Line Tools (for iOS Simulator support)

Run this once on a fresh machine before 'drizz-farm create'.`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().BoolVar(&setupAutoInstall, "install", false, "automatically install missing prerequisites")
	rootCmd.AddCommand(setupCmd)
}

type checkResult struct {
	name    string
	ok      bool
	path    string
	detail  string
	fixCmd  string
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println("drizz-farm Prerequisites Check")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Platform: %s/%s (%d CPUs)\n\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())

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

	// Print results
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

	// Hardware capacity
	fmt.Println()
	printHardwareCapacity()

	if allOK {
		fmt.Println("\nAll prerequisites met!")
		fmt.Println("Next: drizz-farm create")
		return nil
	}

	fmt.Printf("\n%d prerequisite(s) missing.\n", len(failures))

	if setupAutoInstall {
		fmt.Println("\nInstalling missing prerequisites...")
		for _, f := range failures {
			if f.fixCmd == "" {
				fmt.Printf("  ⊘ %-28s (manual install required)\n", f.name)
				continue
			}
			fmt.Printf("  → Installing %s: %s\n", f.name, f.fixCmd)
			parts := strings.Fields(f.fixCmd)
			out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
			if err != nil {
				fmt.Printf("    FAILED: %s\n    %s\n", err, strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("    ✓ Done\n")
			}
		}
		fmt.Println("\nRe-run 'drizz-farm setup' to verify.")
	} else {
		fmt.Println("\nTo auto-install, run:")
		fmt.Println("  drizz-farm setup --install")
		fmt.Println("\nOr install manually:")
		for _, f := range failures {
			if f.fixCmd != "" {
				fmt.Printf("  %s\n", f.fixCmd)
			}
		}
	}

	return nil
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

	// Check JAVA_HOME first
	if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
		javac := filepath.Join(javaHome, "bin", "javac")
		if _, err := os.Stat(javac); err == nil {
			r.ok = true
			r.path = javaHome
			r.detail = fmt.Sprintf("JAVA_HOME=%s", javaHome)
			return r
		}
	}

	// Check PATH
	path, err := exec.LookPath("javac")
	if err != nil {
		r.detail = "not found (needed for Android SDK tools)"
		r.fixCmd = "brew install openjdk"
		return r
	}

	// Get version
	out, _ := exec.Command("javac", "-version").CombinedOutput()
	ver := strings.TrimSpace(string(out))

	r.ok = true
	r.path = path
	r.detail = ver
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

	// Get version
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

	// Get version
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
	r.detail = fmt.Sprintf("%d image(s): %s", len(images), strings.Join(images, ", "))
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

	path := strings.TrimSpace(string(out))
	r.ok = true
	r.path = path
	r.detail = path
	return r
}

func printHardwareCapacity() {
	cpus := runtime.NumCPU()

	// Get total RAM (macOS)
	totalRAMGB := 0
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
		if err == nil {
			var memBytes int64
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &memBytes)
			totalRAMGB = int(memBytes / (1024 * 1024 * 1024))
		}
	}

	// Get free disk
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

	// Calculate capacity
	// Reserve 4GB for macOS + daemon, each emulator ~2.5GB RAM
	usableRAM := totalRAMGB - 4
	if usableRAM < 0 {
		usableRAM = 0
	}
	maxByRAM := usableRAM * 1000 / 2500 // 2.5GB per emulator
	maxByCPU := cpus / 2                  // ~2 cores per emulator
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

	// Disk: ~5GB per AVD
	maxByDisk := freeDiskGB / 5

	fmt.Println("  Hardware Capacity:")
	if totalRAMGB > 0 {
		fmt.Printf("    RAM:        %d GB (can run ~%d emulators)\n", totalRAMGB, maxByRAM)
	}
	fmt.Printf("    CPUs:       %d cores (can run ~%d emulators)\n", cpus, maxByCPU)
	if freeDiskGB > 0 {
		fmt.Printf("    Disk free:  %d GB (can store ~%d AVDs)\n", freeDiskGB, maxByDisk)
	}
	fmt.Printf("    Recommended: %d concurrent emulators\n", recommended)
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
