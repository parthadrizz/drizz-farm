package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/daemon"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

func toDisplayState(s pool.EmulatorState) string {
	switch s {
	case pool.StateCreating:
		return "CREATING"
	case pool.StateBooting:
		return "BOOTING"
	case pool.StateWarm:
		return "ONLINE"
	case pool.StateAllocated:
		return "ALLOCATED"
	case pool.StateResetting:
		return "RESETTING"
	case pool.StateDestroying:
		return "SHUTTING DOWN"
	case pool.StateError:
		return "ERROR"
	default:
		return "OFFLINE"
	}
}

func stateIcon(displayState string) string {
	switch displayState {
	case "ONLINE", "ALLOCATED":
		return "●"
	case "CREATING", "BOOTING":
		return "◐"
	case "RESETTING", "SHUTTING DOWN":
		return "↻"
	case "ERROR":
		return "✗"
	default:
		return "○"
	}
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show drizz-farm status — daemon, pool, AVDs, hardware",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")
	configPath := filepath.Join(dataDir, "config.yaml")

	fmt.Println("drizz-farm Status")
	fmt.Println("━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// 1. Daemon status
	pidFile := daemon.NewPIDFile(dataDir)
	daemonRunning := pidFile.IsRunning()
	pid, _ := pidFile.Read()

	if daemonRunning {
		fmt.Printf("  Daemon:    RUNNING (PID %d)\n", pid)
	} else {
		fmt.Printf("  Daemon:    STOPPED\n")
	}

	// 2. Config
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("  Config:    %s\n", configPath)
	} else {
		fmt.Printf("  Config:    not found (run 'drizz-farm create')\n")
	}

	// 3. SDK
	sdk, sdkErr := android.DetectSDK()
	if sdkErr != nil {
		fmt.Printf("  SDK:       not found\n")
	} else {
		fmt.Printf("  SDK:       %s\n", sdk.Root)
	}

	// 4. AVDs on disk — all are available to drizz-farm
	fmt.Println()
	fmt.Println("  AVDs (all available to pool):")
	if sdkErr == nil {
		runner := &android.DefaultRunner{}
		avdMgr := android.NewAVDManager(sdk, runner)
		avds, err := avdMgr.List(context.Background())
		if err == nil && len(avds) > 0 {
			for _, avd := range avds {
				fmt.Printf("    ● %s\n", avd.Name)
			}
			fmt.Printf("    %d available\n", len(avds))
		} else {
			fmt.Printf("    none (run 'drizz-farm create')\n")
		}
	}

	// 5. Emulators — show ALL AVDs with their real status
	fmt.Println()
	fmt.Println("  Emulators:")
	if sdkErr == nil {
		runner := &android.DefaultRunner{}
		avdMgr := android.NewAVDManager(sdk, runner)
		avds, err := avdMgr.List(context.Background())

		// Get pool state if daemon is running
		poolByAVD := make(map[string]pool.InstanceSnapshot)
		if daemonRunning {
			resp, apiErr := http.Get("http://127.0.0.1:9401/api/v1/pool")
			if apiErr == nil {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				var status pool.PoolStatus
				if json.Unmarshal(body, &status) == nil {
					for _, inst := range status.Instances {
						poolByAVD[inst.AVDName] = inst
					}
				}
			}
		}

		if err == nil && len(avds) > 0 {
			for _, avd := range avds {
				inst, inPool := poolByAVD[avd.Name]
				if !inPool {
					fmt.Printf("    ○ %-35s OFFLINE\n", avd.Name)
					continue
				}

				displayState := toDisplayState(inst.State)
				icon := stateIcon(displayState)
				line := fmt.Sprintf("    %s %-35s %s", icon, avd.Name, displayState)

				if displayState == "ONLINE" {
					line += fmt.Sprintf("  %s  port:%d", inst.Serial, inst.ADBPort)
					if inst.SessionID != "" {
						line += fmt.Sprintf("  session:%s", inst.SessionID)
					}
				}
				fmt.Println(line)
			}
		} else {
			fmt.Println("    no AVDs found (run 'drizz-farm create')")
		}
	}

	// 6. Hardware
	fmt.Println()
	fmt.Println("  Hardware:")
	fmt.Printf("    Platform:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("    CPUs:      %d\n", runtime.NumCPU())
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
		if err == nil {
			var memBytes int64
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &memBytes)
			fmt.Printf("    RAM:       %d GB\n", memBytes/(1024*1024*1024))
		}
	}

	// 7. If daemon is running, show pool summary + sessions
	if daemonRunning {
		// Pool summary
		resp, err := http.Get("http://127.0.0.1:9401/api/v1/pool")
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var status pool.PoolStatus
			if json.Unmarshal(body, &status) == nil {
				fmt.Println()
				fmt.Printf("  Pool:  %d warm, %d allocated, %d booting, %d error (capacity: %d)\n",
					status.Warm, status.Allocated, status.Booting, status.Error, status.TotalCapacity)
			}
		}

		// Sessions
		resp2, err := http.Get("http://127.0.0.1:9401/api/v1/sessions")
		if err == nil {
			defer resp2.Body.Close()
			body2, _ := io.ReadAll(resp2.Body)
			var result struct {
				Active int `json:"active"`
				Queued int `json:"queued"`
			}
			if json.Unmarshal(body2, &result) == nil {
				fmt.Printf("  Sessions:  %d active, %d queued\n", result.Active, result.Queued)
			}
		}

		// Uptime
		resp3, err := http.Get("http://127.0.0.1:9401/api/v1/node/health")
		if err == nil {
			defer resp3.Body.Close()
			body3, _ := io.ReadAll(resp3.Body)
			var health struct {
				Uptime  string `json:"uptime"`
				Version string `json:"version"`
			}
			if json.Unmarshal(body3, &health) == nil {
				fmt.Printf("  Uptime:  %s\n", health.Uptime)
			}
		}
	}

	fmt.Println()
	return nil
}
