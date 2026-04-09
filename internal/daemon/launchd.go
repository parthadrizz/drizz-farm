package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>dev.drizz.farm</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>start</string>
        <string>--config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/drizz-farm.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/drizz-farm.err</string>
    <key>WorkingDirectory</key>
    <string>{{.DataDir}}</string>
</dict>
</plist>
`

// LaunchdConfig holds the template parameters for the plist.
type LaunchdConfig struct {
	BinaryPath string
	ConfigPath string
	DataDir    string
	LogDir     string
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "dev.drizz.farm.plist")
}

// InstallLaunchd writes the launchd plist and loads it.
func InstallLaunchd(cfg LaunchdConfig) error {
	if cfg.LogDir == "" {
		cfg.LogDir = cfg.DataDir
	}

	// Ensure log dir exists
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return fmt.Errorf("parse plist template: %w", err)
	}

	path := plistPath()
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, cfg); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load the service
	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

// UninstallLaunchd unloads and removes the plist.
func UninstallLaunchd() error {
	path := plistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	return os.Remove(path)
}

// IsLaunchdInstalled returns true if the plist exists.
func IsLaunchdInstalled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}
