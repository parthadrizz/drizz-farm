package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/license"
)

var activateCmd = &cobra.Command{
	Use:   "activate [LICENSE_KEY]",
	Short: "Activate a drizz-farm license",
	Args:  cobra.ExactArgs(1),
	RunE:  runActivate,
}

func init() {
	rootCmd.AddCommand(activateCmd)
}

func runActivate(cmd *cobra.Command, args []string) error {
	key := args[0]

	validator := license.NewValidator()
	lic, err := validator.Validate(key)
	if err != nil {
		return fmt.Errorf("license validation failed: %w", err)
	}

	// Save key to config
	home, _ := os.UserHomeDir()
	keyPath := filepath.Join(home, ".drizz-farm", "license.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		return fmt.Errorf("save license key: %w", err)
	}

	fmt.Printf("License activated!\n\n")
	fmt.Printf("  Tier:    %s\n", lic.Tier)
	fmt.Printf("  Expires: %s\n", lic.ExpiresAt.Format("2006-01-02"))

	if license.IsUnlimited(lic.Limits.MaxEmulators) {
		fmt.Printf("  Emulators: unlimited\n")
	} else {
		fmt.Printf("  Emulators: %d\n", lic.Limits.MaxEmulators)
	}
	if license.IsUnlimited(lic.Limits.MaxSeats) {
		fmt.Printf("  Seats: unlimited\n")
	} else {
		fmt.Printf("  Seats: %d\n", lic.Limits.MaxSeats)
	}

	return nil
}
