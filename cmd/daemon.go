package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage drizz-farm as a background service",
	Long: `Run drizz-farm as a launchd service on macOS.
Auto-starts on login, auto-restarts on crash, logs to ~/.drizz-farm/.`,
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install drizz-farm as a launchd service",
	RunE: func(cmd *cobra.Command, args []string) error {
		binaryPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find binary path: %w", err)
		}
		binaryPath, _ = filepath.Abs(binaryPath)

		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".drizz-farm")
		configPath := filepath.Join(dataDir, "config.yaml")

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return fmt.Errorf("no config found — run 'drizz-farm setup' first")
		}

		// If already installed, uninstall first to refresh the plist
		if daemon.IsLaunchdInstalled() {
			fmt.Println("  Reinstalling (unloading existing service)...")
			_ = daemon.UninstallLaunchd()
		}

		if err := daemon.InstallLaunchd(daemon.LaunchdConfig{
			BinaryPath: binaryPath,
			ConfigPath: configPath,
			DataDir:    dataDir,
			LogDir:     dataDir,
		}); err != nil {
			return fmt.Errorf("install: %w", err)
		}

		fmt.Println("  ✓ drizz-farm installed as a launchd service")
		fmt.Println()
		fmt.Println("  Auto-starts on login, auto-restarts on crash.")
		fmt.Printf("  Logs: %s/drizz-farm.log\n", dataDir)
		fmt.Println()
		fmt.Println("  Commands:")
		fmt.Println("    drizz-farm daemon status     — check service state")
		fmt.Println("    drizz-farm daemon uninstall  — remove service")
		return nil
	},
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove drizz-farm launchd service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !daemon.IsLaunchdInstalled() {
			fmt.Println("  drizz-farm is not installed as a service")
			return nil
		}
		if err := daemon.UninstallLaunchd(); err != nil {
			return fmt.Errorf("uninstall: %w", err)
		}
		fmt.Println("  ✓ drizz-farm service removed")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show launchd service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !daemon.IsLaunchdInstalled() {
			fmt.Println("  Not installed. Run 'drizz-farm daemon install' to enable auto-start.")
			return nil
		}
		out, _ := exec.Command("launchctl", "list", "dev.drizz.farm").CombinedOutput()
		fmt.Println(string(out))
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonInstallCmd, daemonUninstallCmd, daemonStatusCmd)
	rootCmd.AddCommand(daemonCmd)
}
