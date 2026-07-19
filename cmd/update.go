package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/selfupdate"
)

var (
	flagUpdateCheck bool
	flagUpdateForce bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update NexoraCLI to the latest GitHub release.",
	Long: "Check the public GitHub releases and, unless --check is given, download the " +
		"binary for this OS/arch and replace the running nexora executable in place.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()

		rel, err := selfupdate.Latest(ctx)
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}

		if !selfupdate.IsNewer(Version, rel.Tag) {
			if flagUpdateForce {
				fmt.Printf("Already on the latest (%s) — reinstalling anyway (--force).\n", rel.Tag)
			} else {
				fmt.Printf("Already up to date (%s).\n", Version)
				return nil
			}
		} else {
			fmt.Printf("Update available: %s -> %s\n", Version, rel.Tag)
		}

		if flagUpdateCheck {
			fmt.Println("Run `nexora update` to install it.")
			return nil
		}

		fmt.Println("Downloading and installing...")
		path, err := selfupdate.Apply(ctx, rel)
		if err != nil {
			return err
		}
		fmt.Printf("Updated to %s at %s. Restart nexora to use it.\n", rel.Tag, path)
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&flagUpdateCheck, "check", false, "only report whether an update is available; do not install")
	updateCmd.Flags().BoolVar(&flagUpdateForce, "force", false, "reinstall even if already on the latest version")
	rootCmd.AddCommand(updateCmd)
}
