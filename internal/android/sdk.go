package android

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
)

// SDK provides access to Android SDK tool paths.
type SDK struct {
	Root string // ANDROID_HOME / ANDROID_SDK_ROOT
}

// DetectSDK finds the Android SDK on the system.
func DetectSDK() (*SDK, error) {
	// 1. Check environment variables
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(env); root != "" {
			if _, err := os.Stat(root); err == nil {
				return &SDK{Root: root}, nil
			}
		}
	}

	// 2. Derive from adb/emulator in PATH
	if adbPath, err := exec.LookPath("adb"); err == nil {
		sdkRoot := filepath.Dir(filepath.Dir(adbPath))
		if _, err := os.Stat(filepath.Join(sdkRoot, "platform-tools")); err == nil {
			return &SDK{Root: sdkRoot}, nil
		}
	}
	if emuPath, err := exec.LookPath("emulator"); err == nil {
		sdkRoot := filepath.Dir(filepath.Dir(emuPath))
		if _, err := os.Stat(filepath.Join(sdkRoot, "emulator")); err == nil {
			return &SDK{Root: sdkRoot}, nil
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

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return &SDK{Root: path}, nil
		}
	}

	return nil, fmt.Errorf("android SDK not found: set ANDROID_HOME or install via 'drizz-farm setup'")
}

// SDK stores resolved paths. Set once during setup, read from config everywhere.
// Falls back to runtime search only if config paths are empty.

// Validate checks that required SDK components are available.
func (s *SDK) Validate() error {
	// Required — can't run without these
	required := map[string]string{
		"adb":      s.ADBPath(),
		"emulator": s.EmulatorPath(),
	}
	for name, path := range required {
		if path == "" {
			return fmt.Errorf("SDK tool not found: %s (run 'drizz-farm setup' to detect paths)", name)
		}
	}
	// Optional — needed for AVD creation but not for running existing AVDs
	if s.AVDManagerPath() == "" {
		log.Warn().Msg("avdmanager not found — AVD creation will be unavailable")
	}
	if s.SDKManagerPath() == "" {
		log.Warn().Msg("sdkmanager not found — image installation will be unavailable")
	}
	return nil
}

// resolvedPaths stores paths from config. Set by NewSDKFromConfig.
var resolvedPaths struct {
	adb, avdmanager, emulator, sdkmanager string
}

// SetResolvedPaths stores pre-detected paths from config so Path methods don't search.
func SetResolvedPaths(adb, avdmanager, emulator, sdkmanager string) {
	resolvedPaths.adb = adb
	resolvedPaths.avdmanager = avdmanager
	resolvedPaths.emulator = emulator
	resolvedPaths.sdkmanager = sdkmanager
}

// fallbackFind searches PATH and common locations. Only used if config has no stored path.
func fallbackFind(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	common := []string{
		"/opt/homebrew/bin/" + name,
		"/usr/local/bin/" + name,
		"/opt/homebrew/share/android-commandlinetools/cmdline-tools/latest/bin/" + name,
	}
	for _, p := range common {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (s *SDK) ADBPath() string {
	if resolvedPaths.adb != "" { return resolvedPaths.adb }
	// Fallback: check SDK root, then PATH
	p := filepath.Join(s.Root, "platform-tools", "adb")
	if _, err := os.Stat(p); err == nil { return p }
	return fallbackFind("adb")
}

func (s *SDK) AVDManagerPath() string {
	if resolvedPaths.avdmanager != "" { return resolvedPaths.avdmanager }
	p := filepath.Join(s.Root, "cmdline-tools", "latest", "bin", "avdmanager")
	if _, err := os.Stat(p); err == nil { return p }
	return fallbackFind("avdmanager")
}

func (s *SDK) EmulatorPath() string {
	if resolvedPaths.emulator != "" { return resolvedPaths.emulator }
	p := filepath.Join(s.Root, "emulator", "emulator")
	if _, err := os.Stat(p); err == nil { return p }
	return fallbackFind("emulator")
}

func (s *SDK) SDKManagerPath() string {
	if resolvedPaths.sdkmanager != "" { return resolvedPaths.sdkmanager }
	p := filepath.Join(s.Root, "cmdline-tools", "latest", "bin", "sdkmanager")
	if _, err := os.Stat(p); err == nil { return p }
	return fallbackFind("sdkmanager")
}

// PlatformToolsDir returns the platform-tools directory.
func (s *SDK) PlatformToolsDir() string {
	return filepath.Join(s.Root, "platform-tools")
}

// SystemImageInstalled checks if a system image is available.
func (s *SDK) SystemImageInstalled(image string) bool {
	// System images live under $ANDROID_HOME/system-images/<api>/<variant>/<arch>/
	// The image string format: "system-images;android-34;google_apis;arm64-v8a"
	// translates to: system-images/android-34/google_apis/arm64-v8a/
	parts := filepath.SplitList(image)
	if len(parts) == 0 {
		// Parse semicolon-separated format
		replaced := filepath.Join(splitSemicolon(image)...)
		path := filepath.Join(s.Root, replaced)
		_, err := os.Stat(path)
		return err == nil
	}
	return false
}

// HostGPUSupported returns true if the host supports GPU acceleration.
func (s *SDK) HostGPUSupported() bool {
	return runtime.GOOS == "darwin" // Apple Silicon always supports Metal/host GPU
}

// InstalledSystemImage represents an installed system image on the machine.
type InstalledSystemImage struct {
	Path    string // e.g., "system-images;android-34-ext8;google_apis_playstore;arm64-v8a"
	APIName string // e.g., "android-34-ext8"
	Variant string // e.g., "google_apis_playstore"
	Arch    string // e.g., "arm64-v8a"
}

// ListInstalledSystemImages queries sdkmanager for installed system images.
func (s *SDK) ListInstalledSystemImages(ctx context.Context, runner CommandRunner) ([]InstalledSystemImage, error) {
	out, err := runner.Run(ctx, s.SDKManagerPath(), "--list_installed")
	if err != nil {
		return nil, fmt.Errorf("sdkmanager --list_installed: %w", err)
	}

	var images []InstalledSystemImage
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Lines look like: "  system-images;android-34-ext8;google_apis_playstore;arm64-v8a | 2 | ..."
		if !strings.HasPrefix(line, "system-images;") {
			continue
		}
		// Extract the package path (first column before |)
		parts := strings.SplitN(line, "|", 2)
		pkgPath := strings.TrimSpace(parts[0])

		segments := splitSemicolon(pkgPath)
		if len(segments) != 4 {
			continue
		}
		images = append(images, InstalledSystemImage{
			Path:    pkgPath,
			APIName: segments[1],
			Variant: segments[2],
			Arch:    segments[3],
		})
	}

	return images, nil
}

// ListAvailableSystemImages queries sdkmanager for all downloadable arm64 system images.
func (s *SDK) ListAvailableSystemImages(ctx context.Context, runner CommandRunner) ([]InstalledSystemImage, error) {
	out, err := runner.Run(ctx, s.SDKManagerPath(), "--list")
	if err != nil {
		return nil, fmt.Errorf("sdkmanager --list: %w", err)
	}

	var images []InstalledSystemImage
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "system-images;") {
			continue
		}
		// Extract package path (first column before |)
		parts := strings.SplitN(line, "|", 2)
		pkgPath := strings.TrimSpace(parts[0])

		if seen[pkgPath] {
			continue
		}
		seen[pkgPath] = true

		segments := splitSemicolon(pkgPath)
		if len(segments) != 4 {
			continue
		}
		// Only arm64
		if segments[3] != "arm64-v8a" {
			continue
		}
		images = append(images, InstalledSystemImage{
			Path:    pkgPath,
			APIName: segments[1],
			Variant: segments[2],
			Arch:    segments[3],
		})
	}

	return images, nil
}

// ListInstalledDevices queries avdmanager for available device definitions.
func (s *SDK) ListInstalledDevices(ctx context.Context, runner CommandRunner) ([]string, error) {
	out, err := runner.Run(ctx, s.AVDManagerPath(), "list", "device", "-c")
	if err != nil {
		return nil, fmt.Errorf("avdmanager list device: %w", err)
	}

	var devices []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			devices = append(devices, line)
		}
	}
	return devices, nil
}

func splitSemicolon(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ';' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
