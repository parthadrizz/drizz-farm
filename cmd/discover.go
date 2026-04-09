package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/discovery"
)

var discoverTimeout int

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover drizz-farm nodes on the local network",
	RunE:  runDiscover,
}

func init() {
	discoverCmd.Flags().IntVar(&discoverTimeout, "timeout", 3, "discovery timeout in seconds")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	fmt.Println("Scanning for drizz-farm nodes on LAN...")

	ctx := context.Background()
	nodes, err := discovery.Browse(ctx, time.Duration(discoverTimeout)*time.Second)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}

	if len(nodes) == 0 {
		fmt.Println("\nNo drizz-farm nodes found on the network.")
		fmt.Println("Start a node with: drizz-farm start")
		return nil
	}

	fmt.Printf("\nFound %d node(s):\n\n", len(nodes))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tPORT\tCAPACITY\tANDROID\tiOS\tVERSION\tTIER")
	for _, node := range nodes {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
			node.Name, node.Host, node.Port,
			node.TotalCapacity, node.AndroidAvail, node.IOSAvail,
			node.Version, node.Tier)
	}
	w.Flush()

	return nil
}
