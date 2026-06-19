// Package cmd wires the cobra command tree. The bare `nexora` command launches the TUI;
// subcommands handle auth/config (login, pair, instance, version).
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
	"gitlab.com/parendum/nexora/nexora-cli/internal/tui"
)

// Version is set from main via -ldflags.
var Version = "dev"

var (
	flagLocalExec bool
	flagYolo      bool
)

var rootCmd = &cobra.Command{
	Use:   "nexora",
	Short: "Nexora terminal client — chat with agents, watch tasks, all from the terminal.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		inst := cfg.CurrentInstance()
		if inst == nil {
			fmt.Println("No Nexora instance configured.")
			fmt.Println("  nexora login   — email/password against an instance")
			fmt.Println("  nexora pair    — pair via a code from the web Settings → Devices")
			return nil
		}
		client := newClient(cfg, inst)
		// flags override; otherwise use the persisted preference.
		localExec := flagLocalExec || cfg.LocalExec
		yolo := flagYolo || cfg.LocalYolo
		return tui.Run(client, cfg.Current, Version, localExec, yolo, cfg.UIMode,
			func(le, yo bool) {
				cfg.LocalExec, cfg.LocalYolo = le, yo
				_ = cfg.Save()
			},
			func(mode string) {
				cfg.UIMode = mode
				_ = cfg.Save()
			})
	},
}

func init() {
	rootCmd.Flags().BoolVar(&flagLocalExec, "local-exec", false,
		"run the agent's shell/file tools on THIS machine instead of the server container (toggle in-TUI with /local)")
	rootCmd.Flags().BoolVar(&flagYolo, "yolo", false,
		"with --local-exec: auto-approve every local command without confirmation (dangerous)")
}

// newClient builds an API client whose refreshed tokens are persisted back to config.
func newClient(cfg *config.Config, inst *config.Instance) *api.Client {
	c := api.New(inst.URL, inst.AccessToken, inst.RefreshToken, inst.APIKey)
	c.SetTokenSink(func(access, refresh string) {
		inst.AccessToken = access
		inst.RefreshToken = refresh
		_ = cfg.Save()
	})
	return c
}

// Execute runs the command tree.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
