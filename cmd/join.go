package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
)

var (
	joinURL   string
	joinToken string
	joinName  string
)

func init() {
	joinCmd.Flags().StringVar(&joinURL, "url", "", "instance base URL (or env NEXORA_URL)")
	joinCmd.Flags().StringVar(&joinToken, "token", "", "invite token (or env NEXORA_JOIN_TOKEN)")
	joinCmd.Flags().StringVar(&joinName, "name", "default", "local name for this instance")
	rootCmd.AddCommand(joinCmd)
}

var joinCmd = &cobra.Command{
	Use:   "join",
	Short: "Join a Nexora instance from an invite — no login required.",
	Long: `Redeem an organization invite shared by your admin. It provisions your account,
pairs this terminal, and saves the instance locally. Zero interaction:

  nexora join --url https://nexora.example.com --token <INVITE_TOKEN>

The install one-liner your admin shares runs this for you automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := strings.TrimRight(strings.TrimSpace(firstNonEmpty(joinURL, os.Getenv("NEXORA_URL"))), "/")
		token := strings.TrimSpace(firstNonEmpty(joinToken, os.Getenv("NEXORA_JOIN_TOKEN")))
		if url == "" || token == "" {
			return fmt.Errorf("both --url and --token are required (or set NEXORA_URL / NEXORA_JOIN_TOKEN)")
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		hostname, _ := os.Hostname()
		deviceName := "nexora-cli@" + hostname

		c := api.New(url, "", "", "")
		rr, err := c.CLIRedeem(context.Background(), token, deviceName, runtime.GOOS)
		if err != nil {
			return err
		}

		// The nxr_ API key is the CLI's durable credential: no expiry, no refresh, and
		// it works for both REST and the chat WebSocket. Revoked centrally by disabling
		// the account (or the key) from the web app.
		cfg.Set(joinName, &config.Instance{
			URL:       url,
			APIKey:    rr.APIKey,
			UserEmail: rr.UserEmail,
			UserName:  rr.UserName,
		})
		if err := cfg.Save(); err != nil {
			return err
		}

		verb := "Joined"
		if rr.CreatedAccount {
			verb = "Account created and joined"
		}
		fmt.Printf("%s %s as %s.\n", verb, rr.OrgName, rr.UserEmail)
		fmt.Println("Run `nexora` to start.")
		return nil
	},
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
