package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
)

var (
	createForce    bool
	createCount    int
	createProfile  string
	createDevice   string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create the emulator farm — detect images, generate config, create AVDs",
	Long: `Detects installed system images and device definitions, generates config.yaml,
and creates the AVDs that 'drizz-farm start' will boot.

Run 'drizz-farm setup' first to ensure all prerequisites are installed.`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().BoolVar(&createForce, "force", false, "overwrite existing config and recreate AVDs")
	createCmd.Flags().IntVarP(&createCount, "count", "n", 0, "number of emulators to create (default: auto-detect from hardware)")
	createCmd.Flags().StringVar(&createProfile, "image", "", "specific system image to use (default: auto-detect best)")
	createCmd.Flags().StringVar(&createDevice, "device", "", "device definition to use (default: pixel_7)")
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	fmt.Println("drizz-farm Create")
	fmt.Println("━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Detect SDK
	sdk, err := android.DetectSDK()
	if err != nil {
		fmt.Printf("Android SDK not found: %v\n", err)
		fmt.Println("Run 'drizz-farm setup' first.")
		return nil
	}

	if err := sdk.Validate(); err != nil {
		fmt.Printf("SDK validation failed: %v\n", err)
		fmt.Println("Run 'drizz-farm setup --install' first.")
		return nil
	}

	ctx := context.Background()
	runner := &android.DefaultRunner{}

	// 1. Detect installed system images
	images, err := sdk.ListInstalledSystemImages(ctx, runner)
	if err != nil {
		return fmt.Errorf("list system images: %w", err)
	}

	var arm64Images []android.InstalledSystemImage
	for _, img := range images {
		if img.Arch == "arm64-v8a" {
			arm64Images = append(arm64Images, img)
		}
	}

	if len(arm64Images) == 0 {
		fmt.Println("No arm64 system images found.")
		fmt.Println("Install one with:")
		fmt.Printf("  %s --install 'system-images;android-35;google_apis;arm64-v8a'\n", sdk.SDKManagerPath())
		return nil
	}

	fmt.Printf("System images found:\n")
	for i, img := range arm64Images {
		fmt.Printf("  [%d] %s\n", i+1, img.Path)
	}

	// 2. Select image
	selectedImage := arm64Images[0] // default: first (newest after sort)
	if createProfile != "" {
		found := false
		for _, img := range arm64Images {
			if img.Path == createProfile || strings.Contains(img.Path, createProfile) {
				selectedImage = img
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("image %q not found in installed images", createProfile)
		}
	} else {
		// Sort: prefer newer API, prefer playstore
		sort.Slice(arm64Images, func(i, j int) bool {
			return arm64Images[i].APIName > arm64Images[j].APIName
		})
		selectedImage = arm64Images[0]
	}
	fmt.Printf("\nUsing: %s\n", selectedImage.Path)

	// 3. Detect device definitions
	devices, err := sdk.ListInstalledDevices(ctx, runner)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}

	device := "pixel_7"
	if createDevice != "" {
		device = createDevice
	} else {
		// Find best available
		preferred := []string{"pixel_7", "pixel_8", "pixel_6", "pixel_5", "medium_phone"}
		for _, p := range preferred {
			for _, d := range devices {
				if d == p {
					device = p
					goto deviceFound
				}
			}
		}
		if len(devices) > 0 {
			device = devices[0]
		}
	}
deviceFound:
	fmt.Printf("Device: %s\n", device)

	// 4. Calculate emulator count
	count := createCount
	if count == 0 {
		count = recommendedEmulatorCount()
	}
	fmt.Printf("Emulators: %d\n", count)

	// 5. Check/create config
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".drizz-farm")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "artifacts"), 0755); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil && !createForce {
		fmt.Printf("\nConfig already exists: %s\n", configPath)
		fmt.Println("Run with --force to overwrite.")
		fmt.Println("Or just run: drizz-farm start")
		return nil
	}

	// 6. Build profile name
	profileName := sanitizeProfileName(selectedImage.APIName, selectedImage.Variant)

	// 7. Generate config
	configYAML := buildConfig(profileName, device, selectedImage.Path, count)
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("\nConfig written: %s\n", configPath)

	// 8. Create AVDs
	fmt.Printf("\nCreating %d AVD(s)...\n", count)
	avdMgr := android.NewAVDManager(sdk, runner)

	created := 0
	for i := 0; i < count; i++ {
		avdName := android.AVDName(profileName, i)

		// Check if already exists
		exists, _ := avdMgr.Exists(ctx, avdName)
		if exists && !createForce {
			fmt.Printf("  ✓ %s (already exists)\n", avdName)
			created++
			continue
		}

		fmt.Printf("  → Creating %s...", avdName)
		err := avdMgr.Create(ctx, avdName, configToProfile(device, selectedImage.Path))
		if err != nil {
			fmt.Printf(" FAILED: %v\n", err)
			continue
		}
		fmt.Printf(" ✓\n")
		created++
	}

	fmt.Printf("\n%d/%d AVDs ready.\n", created, count)
	fmt.Println("\nNext: drizz-farm start")
	if created > 0 {
		fmt.Println("  Or: drizz-farm start --visible  (to see emulator windows)")
	}

	return nil
}

func recommendedEmulatorCount() int {
	// Get RAM on macOS
	totalRAMGB := 0
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
		if err == nil {
			var memBytes int64
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &memBytes)
			totalRAMGB = int(memBytes / (1024 * 1024 * 1024))
		}
	}

	cpus := runtime.NumCPU()

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
	if recommended < 2 {
		recommended = 2
	}
	if recommended > 10 {
		recommended = 10
	}
	return recommended
}

func sanitizeProfileName(apiName, variant string) string {
	name := strings.ReplaceAll(apiName, "android-", "api")
	name = strings.ReplaceAll(name, "-", "_")
	if strings.Contains(variant, "playstore") {
		name += "_play"
	}
	return name
}

func configToProfile(device, systemImage string) config.AndroidProfile {
	return config.AndroidProfile{
		Device:      device,
		SystemImage: systemImage,
		RAMMB:       2048,
	}
}

func buildConfig(profileName, device, systemImage string, count int) string {
	var sb strings.Builder

	sb.WriteString(`# drizz-farm configuration
# Generated by 'drizz-farm create'
# Based on detected system images and hardware capacity

node:
  name: ""  # auto-detected from hostname
  log_level: "info"

pool:
`)
	fmt.Fprintf(&sb, "  max_concurrent: %d\n", count)
	sb.WriteString(`  session_timeout_minutes: 60
  session_max_duration_minutes: 180
  queue_max_size: 20
  queue_timeout_seconds: 300
  port_range_min: 5554
  port_range_max: 5700

  warmup:
`)
	fmt.Fprintf(&sb, "    - profile: \"%s\"\n", profileName)
	fmt.Fprintf(&sb, "      count: %d\n", count)

	sb.WriteString(`
  profiles:
    android:
`)
	fmt.Fprintf(&sb, "      %s:\n", profileName)
	fmt.Fprintf(&sb, "        device: \"%s\"\n", device)
	fmt.Fprintf(&sb, "        system_image: \"%s\"\n", systemImage)
	sb.WriteString(`        ram_mb: 2048
        heap_mb: 512
        disk_size_mb: 4096
        gpu: "host"
        snapshot: true
        boot_timeout_seconds: 120

api:
  host: "0.0.0.0"
  port: 9401
  grpc_port: 9400

network:
  allowed_cidrs:
    - "192.168.0.0/16"
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "127.0.0.0/8"
  mdns:
    enabled: true
    service_type: "_drizz-farm._tcp"

health_check:
  interval_seconds: 15
  unhealthy_threshold: 3

cleanup:
  on_session_end: "snapshot_restore"
  orphan_check_interval_seconds: 60
  disk_cleanup_threshold_gb: 20

artifacts:
  video_recording: true
  screenshot_on_failure: true
  logcat_capture: true
  network_har_capture: false
  retention_days: 30
`)
	return sb.String()
}
