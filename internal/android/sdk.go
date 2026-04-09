package android

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SDK provides access to Android SDK tool paths.
type SDK struct {
	Root string // ANDROID_HOME / ANDROID_SDK_ROOT
}

// DetectSDK finds the Android SDK on the system.
func DetectSDK() (*SDK, error) {
	// Check environment variables
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if root := os.Getenv(env); root != "" {
			if _, err := os.Stat(root); err == nil {
				return &SDK{Root: root}, nil
			}
		}
	}

	// Check common locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Android", "sdk"), // macOS (Android Studio)
		filepath.Join(home, "Android", "Sdk"),            // Linux
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return &SDK{Root: path}, nil
		}
	}

	return nil, fmt.Errorf("android SDK not found: set ANDROID_HOME or install via 'drizz-farm setup'")
}

// Validate checks that required SDK components are installed.
func (s *SDK) Validate() error {
	required := []string{
		s.ADBPath(),
		s.AVDManagerPath(),
		s.EmulatorPath(),
		s.SDKManagerPath(),
	}
	for _, tool := range required {
		if _, err := os.Stat(tool); err != nil {
			return fmt.Errorf("SDK tool not found: %s", tool)
		}
	}
	return nil
}

// ADBPath returns the full path to the adb binary.
func (s *SDK) ADBPath() string {
	return filepath.Join(s.Root, "platform-tools", "adb")
}

// AVDManagerPath returns the full path to the avdmanager binary.
func (s *SDK) AVDManagerPath() string {
	return filepath.Join(s.Root, "cmdline-tools", "latest", "bin", "avdmanager")
}

// EmulatorPath returns the full path to the emulator binary.
func (s *SDK) EmulatorPath() string {
	return filepath.Join(s.Root, "emulator", "emulator")
}

// SDKManagerPath returns the full path to the sdkmanager binary.
func (s *SDK) SDKManagerPath() string {
	return filepath.Join(s.Root, "cmdline-tools", "latest", "bin", "sdkmanager")
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
