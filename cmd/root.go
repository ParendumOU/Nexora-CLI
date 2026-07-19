// Package cmd wires the cobra command tree. The bare `nexora` command launches the TUI;
// subcommands handle auth/config (login, pair, instance, version).
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
	"gitlab.com/parendum/nexora/nexora-cli/internal/selfupdate"
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
		updateHint := checkForUpdate(cfg)
		return tui.Run(client, cfg.Current, Version, updateHint, localExec, yolo, cfg.UIMode,
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

// checkForUpdate returns the newer version tag to hint in the TUI header, or "" if none.
// The GitHub lookup is throttled to once per day (cached in config); in between, the
// hint is derived from the cached tag so launch stays instant. Never fails the launch.
func checkForUpdate(cfg *config.Config) string {
	if !selfupdate.CheckEnabled(Version) {
		return ""
	}
	latest := cfg.LatestKnownVersion
	if time.Since(time.Unix(cfg.LastUpdateCheck, 0)) > selfupdate.DefaultCheckInterval {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if rel, err := selfupdate.Latest(ctx); err == nil {
			latest = rel.Tag
			cfg.LatestKnownVersion = rel.Tag
			cfg.LastUpdateCheck = time.Now().Unix()
			_ = cfg.Save()
		}
	}
	if selfupdate.IsNewer(Version, latest) {
		return latest
	}
	return ""
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
	selfupdate.CleanupOld() // remove a leftover <exe>.old from a prior Windows self-update
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
