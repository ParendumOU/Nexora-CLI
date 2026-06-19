package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
	"golang.org/x/term"
)

var (
	loginURL   string
	loginName  string
	loginEmail string
	loginKey   string
)

func init() {
	loginCmd.Flags().StringVar(&loginURL, "url", "", "instance base URL, e.g. https://nexora.example.com")
	loginCmd.Flags().StringVar(&loginName, "name", "default", "local name for this instance")
	loginCmd.Flags().StringVar(&loginEmail, "email", "", "account email")
	loginCmd.Flags().StringVar(&loginKey, "api-key", "", "use an nxr_ API key instead of email/password")
	rootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate against a Nexora instance and save it locally.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		r := bufio.NewReader(os.Stdin)
		url := loginURL
		if url == "" {
			url = prompt(r, "Instance URL: ")
		}
		url = strings.TrimRight(strings.TrimSpace(url), "/")

		inst := &config.Instance{URL: url}

		// API-key path: no interactive login, just store the key.
		if loginKey != "" {
			inst.APIKey = strings.TrimSpace(loginKey)
			cfg.Set(loginName, inst)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("Saved instance %q (API key auth).\n", loginName)
			return nil
		}

		email := loginEmail
		if email == "" {
			email = prompt(r, "Email: ")
		}
		fmt.Print("Password: ")
		pwBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}

		c := api.New(url, "", "", "")
		ctx := context.Background()
		tr, err := c.Login(ctx, strings.TrimSpace(email), string(pwBytes))
		if err != nil {
			return err
		}
		if tr.RequiresTOTP {
			code := prompt(r, "2FA code: ")
			tr, err = c.TotpLogin(ctx, tr.TOTPToken, strings.TrimSpace(code))
			if err != nil {
				return err
			}
		}
		inst.AccessToken = tr.AccessToken
		inst.RefreshToken = tr.RefreshToken
		inst.UserEmail = strings.TrimSpace(email)
		cfg.Set(loginName, inst)
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Logged in. Saved instance %q. Run `nexora` to start.\n", loginName)
		return nil
	},
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}
