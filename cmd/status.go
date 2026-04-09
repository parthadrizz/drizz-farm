package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/daemon"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show drizz-farm pool status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	pidFile := daemon.NewPIDFile(filepath.Join(home, ".drizz-farm"))

	if !pidFile.IsRunning() {
		fmt.Println("drizz-farm is not running.")
		return nil
	}

	// Call local API
	resp, err := http.Get("http://localhost:9401/api/v1/pool")
	if err != nil {
		return fmt.Errorf("connect to daemon: %w (is drizz-farm running?)", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if jsonOut {
		fmt.Println(string(body))
		return nil
	}

	var status pool.PoolStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Pool Status (capacity: %d)\n", status.TotalCapacity)
	fmt.Printf("  Warm: %d | Allocated: %d | Booting: %d | Error: %d\n\n",
		status.Warm, status.Allocated, status.Booting, status.Error)

	if len(status.Instances) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tPROFILE\tSTATE\tSERIAL\tADB PORT\tSESSION")
		for _, inst := range status.Instances {
			sessionID := "-"
			if inst.SessionID != "" {
				sessionID = inst.SessionID
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
				inst.ID, inst.ProfileName, inst.State, inst.Serial, inst.ADBPort, sessionID)
		}
		w.Flush()
	}

	return nil
}
