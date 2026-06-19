package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
)

var (
	envScope string
	envName  string
	envDesc  string
	envYes   bool
)

func init() {
	envSetCmd.Flags().StringVar(&envScope, "scope", "user", "scope: user (personal) or org (shared)")
	envSetCmd.Flags().StringVar(&envName, "name", "", "unique name for this value (defaults to the key; use to keep duplicates of one key)")
	envSetCmd.Flags().StringVar(&envDesc, "desc", "", "optional description")
	envDeleteCmd.Flags().BoolVar(&envYes, "yes", false, "skip the confirmation prompt")
	envCmd.AddCommand(envListCmd, envSetCmd, envDeleteCmd)
	rootCmd.AddCommand(envCmd)
}

// authedClient builds an API client for the current instance, or errors if not logged in.
func authedClient() (*api.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	inst := cfg.CurrentInstance()
	if inst == nil {
		return nil, fmt.Errorf("no active instance — run `nexora login` or `nexora pair` first")
	}
	return newClient(cfg, inst), nil
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage tool credentials (org/user environment variables).",
	Long: "Store API keys and secrets that tools use, scoped to your organization " +
		"(shared) or your profile (personal). Values resolve org-first, then user, at " +
		"tool-execution time — no server .env needed. Values are write-only (never shown).",
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured environment variables (org + personal).",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := authedClient()
		if err != nil {
			return err
		}
		vars, err := c.ListEnvVars(context.Background())
		if err != nil {
			return err
		}
		if len(vars) == 0 {
			fmt.Println("No environment variables. Add one with `nexora env set KEY VALUE`.")
			return nil
		}
		fmt.Printf("%-8s %-30s %s\n", "SCOPE", "KEY", "NAME")
		for _, v := range vars {
			fmt.Printf("%-8s %-30s %s\n", v.Scope, v.Key, v.Name)
		}
		return nil
	},
}

var envSetCmd = &cobra.Command{
	Use:   "set <KEY> <VALUE>",
	Short: "Set an environment variable (--scope user|org, --name for duplicates).",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		scope := strings.ToLower(strings.TrimSpace(envScope))
		if scope != "org" {
			scope = "user"
		}
		name := envName
		if strings.TrimSpace(name) == "" {
			name = key
		}
		c, err := authedClient()
		if err != nil {
			return err
		}
		orgID := ""
		if scope == "org" {
			orgs, err := c.ListOrgs(context.Background())
			if err != nil {
				return err
			}
			if len(orgs) == 0 {
				return fmt.Errorf("no organization available for org scope")
			}
			orgID = orgs[0].ID
		}
		v, err := c.CreateEnvVar(context.Background(), scope, orgID, key, name, value, envDesc)
		if err != nil {
			return err
		}
		fmt.Printf("Saved %s (%s · %s).\n", v.Key, v.Scope, v.Name)
		return nil
	},
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete <KEY> [name]",
	Short: "Delete an environment variable by key (and optional name to disambiguate).",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		wantName := ""
		if len(args) == 2 {
			wantName = args[1]
		}
		c, err := authedClient()
		if err != nil {
			return err
		}
		vars, err := c.ListEnvVars(context.Background())
		if err != nil {
			return err
		}
		var matches []api.EnvVar
		for _, v := range vars {
			if v.Key == key && (wantName == "" || v.Name == wantName) {
				matches = append(matches, v)
			}
		}
		if len(matches) == 0 {
			return fmt.Errorf("no variable found for key %q", key)
		}
		if len(matches) > 1 {
			fmt.Printf("%d variables match %q — pass a name to disambiguate:\n", len(matches), key)
			for _, v := range matches {
				fmt.Printf("  %s · %s\n", v.Scope, v.Name)
			}
			return nil
		}
		m := matches[0]
		if !envYes {
			fmt.Printf("Delete %s variable %q (%s)? [y/N] ", m.Scope, m.Name, m.Key)
			var resp string
			fmt.Scanln(&resp)
			if strings.ToLower(strings.TrimSpace(resp)) != "y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}
		if err := c.DeleteEnvVar(context.Background(), m.ID); err != nil {
			return err
		}
		fmt.Printf("Deleted %s (%s · %s).\n", m.Key, m.Scope, m.Name)
		return nil
	},
}
