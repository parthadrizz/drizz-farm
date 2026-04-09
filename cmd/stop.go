package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/daemon"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the drizz-farm daemon",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")

	pidFile := daemon.NewPIDFile(dataDir)
	if !pidFile.IsRunning() {
		fmt.Println("drizz-farm is not running.")
		return nil
	}

	pid, err := pidFile.Read()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}

	fmt.Printf("Stopping drizz-farm (PID %d)...\n", pid)
	if err := pidFile.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send signal: %w", err)
	}

	fmt.Println("Stop signal sent. Daemon will shut down gracefully.")
	return nil
}
