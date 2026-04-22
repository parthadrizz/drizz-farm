package cmd

// `drizz-farm stop` — clean daemon shutdown.
//
// The old version just sent SIGTERM and returned. When the daemon hung
// mid-shutdown (slow emulator kill, scrcpy deadlock, broken adb), users
// were left with a zombie daemon + a half-running pool. Now we:
//
//   1. SIGTERM the daemon via its pidfile.
//   2. Poll the pidfile for up to 20s, reporting progress.
//   3. If still alive, SIGKILL — then sweep orphaned children
//      (qemu-system, emulator, scrcpy) since SIGKILL bypasses our
//      own graceful cleanup.
//   4. Always run a final orphan sweep after success too, because the
//      daemon may have exited mid-cleanup.
//
// --force skips the 20s wait and goes straight to SIGKILL + sweep.
//   (Use after a real hang — prefer the default.)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/daemon"
)

var stopForce bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the drizz-farm daemon + sweep orphaned emulator/scrcpy processes",
	RunE:  runStop,
}

func init() {
	stopCmd.Flags().BoolVar(&stopForce, "force", false, "SIGKILL immediately, skip the graceful wait")
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".drizz-farm")
	pidFile := daemon.NewPIDFile(dataDir)

	if !pidFile.IsRunning() {
		fmt.Println("drizz-farm is not running.")
		// Still sweep — orphaned emulator/scrcpy processes may be lurking
		// from a previous unclean exit.
		sweepOrphans(true)
		return nil
	}

	pid, err := pidFile.Read()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}

	if stopForce {
		fmt.Printf("Force-killing drizz-farm (PID %d)...\n", pid)
		_ = pidFile.Signal(syscall.SIGKILL)
		time.Sleep(500 * time.Millisecond)
		sweepOrphans(false)
		return nil
	}

	fmt.Printf("Stopping drizz-farm (PID %d)...\n", pid)
	if err := pidFile.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Poll for graceful exit. Daemon shutdown can legitimately take
	// time when many emulators are running (each gets SIGTERM → 10s
	// wait → SIGKILL).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !pidFile.IsRunning() {
			fmt.Println("Daemon exited cleanly.")
			sweepOrphans(false)
			return nil
		}
		fmt.Print(".")
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()
	fmt.Println("Daemon didn't exit in 20s — escalating to SIGKILL.")
	_ = pidFile.Signal(syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)
	sweepOrphans(false)
	return nil
}

// sweepOrphans looks for and kills leftover child processes that the
// daemon normally cleans up itself. Called after every stop — whether
// graceful, forced, or when the daemon was already gone. Safe to run
// when nothing's orphaned: pkill with no matches exits non-zero but
// we ignore that.
//
// Scope:
//   - qemu-system-* — the actual emulator VM process.
//   - emulator — Google's launcher process.
//   - crashpad_handler — emulator's crash reporter.
//   - scrcpy — streaming server we spawn per session.
//
// If `quiet` is true we don't print "no leftovers found" noise.
func sweepOrphans(quiet bool) {
	targets := []string{
		"qemu-system-aarch64",
		"qemu-system-x86_64",
		"crashpad_handler",
		"scrcpy",
	}
	// `emulator` is intentionally handled via pgrep on the specific
	// binary name — matching just "emulator" via pkill -f would also
	// kill unrelated apps with "emulator" in their args.
	killed := 0
	for _, name := range targets {
		out, _ := exec.Command("pgrep", "-f", name).Output()
		if len(out) == 0 {
			continue
		}
		if err := exec.Command("pkill", "-9", "-f", name).Run(); err == nil {
			killed++
			fmt.Printf("  ✓ killed orphan: %s\n", name)
		}
	}
	// Kill "emulator" children specifically owned by our user. Using
	// pgrep -U to scope.
	uid := fmt.Sprintf("%d", os.Getuid())
	out, _ := exec.Command("pgrep", "-U", uid, "-x", "emulator").Output()
	if len(out) > 0 {
		if err := exec.Command("pkill", "-9", "-U", uid, "-x", "emulator").Run(); err == nil {
			killed++
			fmt.Println("  ✓ killed orphan: emulator")
		}
	}
	if killed == 0 && !quiet {
		fmt.Println("  No orphaned processes found.")
	}
}
