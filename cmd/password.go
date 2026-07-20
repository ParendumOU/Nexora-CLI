package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
)

func init() { rootCmd.AddCommand(setPasswordCmd) }

var setPasswordCmd = &cobra.Command{
	Use:   "set-password",
	Short: "Change your password.",
	Long: "Change the password for your account. You must enter your current password. " +
		"Accounts created from a terminal invite have no password until an organization " +
		"owner or admin enables web sign-in and hands you the generated one.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		inst := cfg.CurrentInstance()
		if inst == nil {
			return errors.New("no instance configured — run `nexora join`, `nexora login` or `nexora pair` first")
		}
		client := newClient(cfg, inst)
		ctx := context.Background()

		me, err := client.Me(ctx)
		if err != nil {
			return fmt.Errorf("could not reach the instance: %w", err)
		}

		if !me.HasPassword {
			return errors.New("password sign-in is not enabled for this account — " +
				"ask your organization owner or admin to enable it, then change the " +
				"password they give you")
		}
		current, err := readSecret("Current password: ")
		if err != nil {
			return err
		}
		if current == "" {
			return errors.New("current password is required")
		}

		newPw, err := readSecret("New password: ")
		if err != nil {
			return err
		}
		confirm, err := readSecret("Confirm new password: ")
		if err != nil {
			return err
		}
		if newPw != confirm {
			return errors.New("passwords do not match")
		}
		if err := validatePassword(newPw); err != nil {
			return err
		}

		if err := client.SetPassword(ctx, current, newPw); err != nil {
			return err
		}
		fmt.Printf("Password changed. Sign in on the web at %s with %s.\n", inst.URL, me.Email)
		return nil
	},
}

// readSecret prompts on stderr and reads a line from the terminal without echoing it.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// validatePassword mirrors the server's rules so the user gets a fast, local error.
func validatePassword(p string) error {
	var missing []string
	if len(p) < 8 {
		missing = append(missing, "at least 8 characters")
	}
	if !strings.ContainsFunc(p, func(r rune) bool { return r >= 'A' && r <= 'Z' }) {
		missing = append(missing, "one uppercase letter")
	}
	if !strings.ContainsFunc(p, func(r rune) bool { return r >= 'a' && r <= 'z' }) {
		missing = append(missing, "one lowercase letter")
	}
	if !strings.ContainsFunc(p, func(r rune) bool { return r >= '0' && r <= '9' }) {
		missing = append(missing, "one digit")
	}
	if len(missing) > 0 {
		return fmt.Errorf("password must contain: %s", strings.Join(missing, ", "))
	}
	return nil
}
