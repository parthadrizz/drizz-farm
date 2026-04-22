package cmd

import (
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

var joinCmd = &cobra.Command{
	Use:   "join <peer-url> <group-key>",
	Short: "Join an existing drizz-farm group",
	Long: `Joins an existing drizz-farm group by fetching the node list
from a peer. The peer must be reachable and you must have the group key.

Example:
  drizz-farm join http://mac-mini-1.local:9401 abc123def456...

The daemon will import the group config locally and register this machine
with the peer so other members see it.`,
	Args: cobra.ExactArgs(2),
	RunE: runJoin,
}

func init() {
	rootCmd.AddCommand(joinCmd)
}

func runJoin(cmd *cobra.Command, args []string) error {
	peerURL := strings.TrimRight(args[0], "/")
	groupKey := args[1]

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
