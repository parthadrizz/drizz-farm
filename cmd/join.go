package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/registry"
)

var joinYes bool

var joinCmd = &cobra.Command{
	Use:   "join <peer-url> <group-key>",
	Short: "Join an existing drizz-farm group",
	Long: `Joins an existing drizz-farm group by fetching the node list
from a peer. The peer must be reachable and you must have the group key.

Example:
  drizz-farm join http://mac-mini-1.local:9401 abc123def456...

The daemon will import the group config locally and register this machine
with the peer so other members see it.

Joining shares this machine with everyone else in the group: any group
member can boot emulators, allocate sessions, and stream this Mac's
screens. Use --yes to skip the confirmation prompt.`,
	Args: cobra.ExactArgs(2),
	RunE: runJoin,
}

func init() {
	joinCmd.Flags().BoolVarP(&joinYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(joinCmd)
}

func runJoin(cmd *cobra.Command, args []string) error {
	peerURL := strings.TrimRight(args[0], "/")
	groupKey := args[1]

	// Spell out the implications of joining and require confirmation —
	// joining is reversible (drizz-farm leave), but it's not benign:
	// every member of the group can drive emulators on this Mac.
	if !joinYes {
		fmt.Println()
		fmt.Println("  ⚠  Joining a group means:")
		fmt.Println("       • Any group member can list, boot, and stop emulators on this Mac.")
		fmt.Println("       • Any group member can stream this Mac's screens + claim sessions.")
		fmt.Println("       • Any group member can read AVD names and device serials.")
		fmt.Println()
		fmt.Println("     Group members CANNOT:")
		fmt.Println("       • Read your files outside ~/.drizz-farm.")
		fmt.Println("       • Remove this machine from the group (only you can `drizz-farm leave`).")
		fmt.Println()
		fmt.Printf("     Joining %q via %s.\n", groupKey[:min(8, len(groupKey))]+"…", peerURL)
		fmt.Print("     Continue? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans != "y" && ans != "yes" {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	// Load config so we know our own name + external URL.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w (run 'drizz-farm setup' first)", err)
	}

	// Determine our identity the same way the API server does.
	selfName := cfg.Node.Name
	if selfName == "" {
		h, _ := os.Hostname()
		selfName = h
	}
	selfURL := cfg.Node.ExternalURL
	if selfURL == "" {
		h, _ := os.Hostname()
		if !strings.HasSuffix(h, ".local") {
			h += ".local"
		}
		selfURL = fmt.Sprintf("http://%s:%d", h, cfg.API.Port)
	}

	// Open the registry and join.
	regPath := filepath.Join(cfg.DataDir(), "nodes.yaml")
	reg, err := registry.New(regPath)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	groupName, err := reg.JoinGroup(ctx, peerURL, groupKey, selfName, selfURL)
	if err != nil {
		return fmt.Errorf("join: %w", err)
	}

	fmt.Printf("  ✓ Joined group %q (via %s)\n", groupName, peerURL)
	fmt.Printf("  ✓ Registered as %q → %s\n", selfName, selfURL)
	fmt.Println()
	fmt.Println("  Restart drizz-farm (or run 'drizz-farm start') to pick up the new config.")
	return nil
}

// Sanity guard — catches build-time drift if someone removes net package use above.
var _ = net.IPv4
