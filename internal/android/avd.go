package android

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drizz-dev/drizz-farm/internal/config"
)

// AVDInfo holds metadata about an existing AVD, enriched from config files.
type AVDInfo struct {
	Name         string `json:"name"`
	Device       string `json:"device"`        // e.g. "pixel_7"
	DeviceModel  string `json:"device_model"`  // e.g. "Pixel 7"
	Manufacturer string `json:"manufacturer"`  // e.g. "Google"
	APILevel     string `json:"api_level"`     // e.g. "34"
	AndroidVer   string `json:"android_ver"`   // e.g. "14"
	Variant      string `json:"variant"`       // e.g. "google_apis_playstore"
	Arch         string `json:"arch"`          // e.g. "arm64-v8a"
	DisplayName  string `json:"display_name"`  // e.g. "Pixel 7 · Android 14 · Play Store"
	Path         string `json:"path,omitempty"`
	Target       string `json:"target,omitempty"`
}

// AVDManager wraps avdmanager commands.
type AVDManager struct {
	sdk    *SDK
	runner CommandRunner
}

// NewAVDManager creates a new AVD manager.
func NewAVDManager(sdk *SDK, runner CommandRunner) *AVDManager {
	return &AVDManager{sdk: sdk, runner: runner}
}

// Create creates a new AVD with the given name and profile settings.
// If profile.SystemImage or profile.Device are empty, they are auto-detected
// from the installed SDK images (picks the newest arm64 image and "pixel" device).
func (m *AVDManager) Create(ctx context.Context, name string, profile config.AndroidProfile) error {
	systemImage := profile.SystemImage
	device := profile.Device

	// Auto-detect system image if not specified
	if systemImage == "" {
		images, err := m.sdk.ListInstalledSystemImages(ctx, m.runner)
		if err != nil || len(images) == 0 {
			return fmt.Errorf("no system images installed — run 'sdkmanager' to install one")
		}
		// Pick the first available (sorted by API level desc in ListInstalledSystemImages)
		systemImage = images[0].Path
	}

	// Default device if not specified
	if device == "" {
		device = "pixel"
	}

	args := []string{
		"create", "avd",
		"--name", name,
		"--package", systemImage,
		"--device", device,
		"--force",
	}

	// avdmanager prompts for custom hardware profile; pipe "no" to skip
	_, err := m.runner.Run(ctx, m.sdk.AVDManagerPath(), args...)
	if err != nil {
		return fmt.Errorf("avdmanager create %s: %w", name, err)
	}
	return nil
}

// Delete removes an existing AVD.
func (m *AVDManager) Delete(ctx context.Context, name string) error {
	_, err := m.runner.Run(ctx, m.sdk.AVDManagerPath(), "delete", "avd", "--name", name)
	if err != nil {
		return fmt.Errorf("avdmanager delete %s: %w", name, err)
	}
	return nil
}

// List returns all existing AVDs.
func (m *AVDManager) List(ctx context.Context) ([]AVDInfo, error) {
	out, err := m.runner.Run(ctx, m.sdk.AVDManagerPath(), "list", "avd", "-c")
	if err != nil {
		return nil, fmt.Errorf("avdmanager list: %w", err)
	}

	var avds []AVDInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			info := AVDInfo{Name: name}
			EnrichAVDInfo(&info)
			avds = append(avds, info)
		}
	}
	return avds, nil
}

// EnrichAVDInfo reads the AVD's config.ini to extract device model, API level, variant.
func EnrichAVDInfo(info *AVDInfo) {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".android", "avd", info.Name+".avd", "config.ini")

	f, err := os.Open(configPath)
	if err != nil {
		info.DisplayName = info.Name
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "hw.device.name":
			info.Device = val
		case "hw.device.manufacturer":
			info.Manufacturer = val
		case "image.sysdir.1":
			// Parse: system-images/android-34-ext8/google_apis_playstore/arm64-v8a/
			segments := strings.Split(strings.TrimSuffix(val, "/"), "/")
			for _, seg := range segments {
				if strings.HasPrefix(seg, "android-") {
					info.APILevel = strings.TrimPrefix(seg, "android-")
					// Strip extension suffixes like "-ext8"
					base := info.APILevel
					if idx := strings.Index(base, "-"); idx > 0 {
						base = base[:idx]
					}
					info.AndroidVer = apiToAndroid(base)
				}
				if strings.Contains(seg, "google_apis") || strings.Contains(seg, "default") {
					info.Variant = seg
				}
				if strings.Contains(seg, "arm64") || strings.Contains(seg, "x86") {
					info.Arch = seg
				}
			}
		}
	}

	// Build human-friendly model name
	info.DeviceModel = prettifyDevice(info.Device)

	// Build display name: "Pixel 7 · Android 14 · Play Store · API 34"
	parts := []string{}
	if info.DeviceModel != "" {
		parts = append(parts, info.DeviceModel)
	}
	if info.AndroidVer != "" {
		parts = append(parts, "Android "+info.AndroidVer)
	}
	if info.Variant != "" {
		parts = append(parts, prettifyVariant(info.Variant))
	}
	if info.APILevel != "" {
		parts = append(parts, "API "+info.APILevel)
	}
	if len(parts) > 0 {
		info.DisplayName = strings.Join(parts, " · ")
	} else {
		info.DisplayName = info.Name
	}
}

// apiToAndroid maps API level to Android version.
func apiToAndroid(api string) string {
	m := map[string]string{
		"35": "15", "34": "14", "33": "13", "32": "12L", "31": "12",
		"30": "11", "29": "10", "28": "9", "27": "8.1", "26": "8.0",
		"25": "7.1", "24": "7.0", "23": "6.0", "22": "5.1", "21": "5.0",
	}
	if v, ok := m[api]; ok {
		return v
	}
	return api
}

// prettifyDevice converts device IDs to readable names.
func prettifyDevice(device string) string {
	m := map[string]string{
		"pixel":   "Pixel", "pixel_2": "Pixel 2", "pixel_3": "Pixel 3",
		"pixel_4": "Pixel 4", "pixel_5": "Pixel 5", "pixel_6": "Pixel 6",
		"pixel_7": "Pixel 7", "pixel_7_pro": "Pixel 7 Pro",
		"pixel_8": "Pixel 8", "pixel_8_pro": "Pixel 8 Pro",
		"pixel_9": "Pixel 9", "pixel_fold": "Pixel Fold",
		"pixel_tablet": "Pixel Tablet",
		"Nexus 5X": "Nexus 5X", "Nexus 6P": "Nexus 6P",
		"medium_phone": "Medium Phone", "small_phone": "Small Phone",
		"medium_tablet": "Medium Tablet",
	}
	if v, ok := m[device]; ok {
		return v
	}
	// Capitalize and replace underscores
	return strings.ReplaceAll(strings.Title(strings.ReplaceAll(device, "_", " ")), "  ", " ")
}

// prettifyVariant converts variant IDs to readable names.
func prettifyVariant(variant string) string {
	m := map[string]string{
		"google_apis_playstore": "Play Store",
		"google_apis":           "Google APIs",
		"default":               "AOSP",
		"google_atd":            "ATD",
	}
	if v, ok := m[variant]; ok {
		return v
	}
	return variant
}

// Exists checks if an AVD with the given name exists.
func (m *AVDManager) Exists(ctx context.Context, name string) (bool, error) {
	avds, err := m.List(ctx)
	if err != nil {
		return false, err
	}
	for _, avd := range avds {
		if avd.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// AVDName generates the drizz-farm AVD name for a profile and index.
func AVDName(profileName string, index int) string {
	return fmt.Sprintf("drizz_%s_%d", profileName, index)
}
