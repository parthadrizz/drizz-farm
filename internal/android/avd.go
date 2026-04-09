package android

import (
	"context"
	"fmt"
	"strings"

	"github.com/drizz-dev/drizz-farm/internal/config"
)

// AVDInfo holds metadata about an existing AVD.
type AVDInfo struct {
	Name   string
	Device string
	Path   string
	Target string
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
func (m *AVDManager) Create(ctx context.Context, name string, profile config.AndroidProfile) error {
	args := []string{
		"create", "avd",
		"--name", name,
		"--package", profile.SystemImage,
		"--device", profile.Device,
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
			avds = append(avds, AVDInfo{Name: name})
		}
	}
	return avds, nil
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
