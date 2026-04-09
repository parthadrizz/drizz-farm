package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/buildinfo"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print drizz-farm version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("drizz-farm %s\n", buildinfo.Version)
		fmt.Printf("  commit:  %s\n", buildinfo.Commit)
		fmt.Printf("  built:   %s\n", buildinfo.BuildDate)
		fmt.Printf("  go:      %s\n", runtime.Version())
		fmt.Printf("  os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
