package android

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DeviceInfo represents a connected ADB device.
type DeviceInfo struct {
	Serial string
	State  string // "device", "offline", "unauthorized"
}

// ADBClient wraps adb commands.
type ADBClient struct {
	sdk    *SDK
	runner CommandRunner
}

// NewADBClient creates a new ADB client.
func NewADBClient(sdk *SDK, runner CommandRunner) *ADBClient {
	return &ADBClient{sdk: sdk, runner: runner}
}

// Devices returns all connected ADB devices.
func (a *ADBClient) Devices(ctx context.Context) ([]DeviceInfo, error) {
	out, err := a.runner.Run(ctx, a.sdk.ADBPath(), "devices")
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}

	var devices []DeviceInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines[1:] { // Skip header "List of devices attached"
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			devices = append(devices, DeviceInfo{
				Serial: parts[0],
				State:  parts[1],
			})
		}
	}
	return devices, nil
}

// WaitForDevice waits until the device with the given serial is online.
func (a *ADBClient) WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "wait-for-device")
	if err != nil {
		return fmt.Errorf("adb wait-for-device %s: %w", serial, err)
	}
	return nil
}

// WaitForBoot waits until sys.boot_completed is "1".
func (a *ADBClient) WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		prop, err := a.GetProp(ctx, serial, "sys.boot_completed")
		if err == nil && strings.TrimSpace(prop) == "1" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("adb: device %s did not finish booting within %s", serial, timeout)
}

// GetProp reads a system property from the device.
func (a *ADBClient) GetProp(ctx context.Context, serial string, prop string) (string, error) {
	out, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "shell", "getprop", prop)
	if err != nil {
		return "", fmt.Errorf("adb getprop %s on %s: %w", prop, serial, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Shell executes a shell command on the device.
func (a *ADBClient) Shell(ctx context.Context, serial string, command string) (string, error) {
	out, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "shell", command)
	if err != nil {
		return "", fmt.Errorf("adb shell on %s: %w", serial, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Install installs an APK on the device.
func (a *ADBClient) Install(ctx context.Context, serial string, apkPath string, grantPerms bool) error {
	args := []string{"-s", serial, "install"}
	if grantPerms {
		args = append(args, "-g")
	}
	args = append(args, apkPath)

	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), args...)
	if err != nil {
		return fmt.Errorf("adb install on %s: %w", serial, err)
	}
	return nil
}

// Uninstall removes a package from the device.
func (a *ADBClient) Uninstall(ctx context.Context, serial string, packageName string) error {
	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "uninstall", packageName)
	if err != nil {
		return fmt.Errorf("adb uninstall %s on %s: %w", packageName, serial, err)
	}
	return nil
}

// Forward sets up a port forward from host to device.
func (a *ADBClient) Forward(ctx context.Context, serial string, localPort, remotePort int) error {
	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "forward",
		fmt.Sprintf("tcp:%d", localPort), fmt.Sprintf("tcp:%d", remotePort))
	if err != nil {
		return fmt.Errorf("adb forward %d→%d on %s: %w", localPort, remotePort, serial, err)
	}
	return nil
}

// Push copies a file from host to device.
func (a *ADBClient) Push(ctx context.Context, serial string, localPath, remotePath string) error {
	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "push", localPath, remotePath)
	if err != nil {
		return fmt.Errorf("adb push to %s: %w", serial, err)
	}
	return nil
}

// Pull copies a file from device to host.
func (a *ADBClient) Pull(ctx context.Context, serial string, remotePath, localPath string) error {
	_, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "pull", remotePath, localPath)
	if err != nil {
		return fmt.Errorf("adb pull from %s: %w", serial, err)
	}
	return nil
}

// Screencap takes a screenshot and returns the PNG data.
func (a *ADBClient) Screencap(ctx context.Context, serial string) ([]byte, error) {
	out, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "exec-out", "screencap", "-p")
	if err != nil {
		return nil, fmt.Errorf("adb screencap on %s: %w", serial, err)
	}
	return out, nil
}

// EmuCommand sends a command to the emulator console via adb.
func (a *ADBClient) EmuCommand(ctx context.Context, serial string, command string) (string, error) {
	out, err := a.runner.Run(ctx, a.sdk.ADBPath(), "-s", serial, "emu", command)
	if err != nil {
		return "", fmt.Errorf("adb emu %s on %s: %w", command, serial, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ListThirdPartyPackages returns installed third-party package names.
func (a *ADBClient) ListThirdPartyPackages(ctx context.Context, serial string) ([]string, error) {
	out, err := a.Shell(ctx, serial, "pm list packages -3")
	if err != nil {
		return nil, err
	}
	var packages []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			packages = append(packages, strings.TrimPrefix(line, "package:"))
		}
	}
	return packages, nil
}
