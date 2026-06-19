package cmd

import (
	"bufio"
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
	pairURL  string
	pairName string
)

func init() {
	pairCmd.Flags().StringVar(&pairURL, "url", "", "instance base URL")
	pairCmd.Flags().StringVar(&pairName, "name", "default", "local name for this instance")
	rootCmd.AddCommand(pairCmd)
}

var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Pair this terminal using a code from the web app (Settings → Devices).",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		r := bufio.NewReader(os.Stdin)
		url := pairURL
		if url == "" {
			url = prompt(r, "Instance URL: ")
		}
		url = strings.TrimRight(strings.TrimSpace(url), "/")
		code := prompt(r, "Pairing code: ")

		hostname, _ := os.Hostname()
		name := "nexora-cli@" + hostname

		c := api.New(url, "", "", "")
		dp, err := c.DevicePair(context.Background(), strings.TrimSpace(code), name, runtime.GOOS)
		if err != nil {
			return err
		}
		// Device pairing yields an access JWT now; the device_token refreshes it later
		// (stored in RefreshToken for the device-refresh flow — see internal/api).
		cfg.Set(pairName, &config.Instance{
			URL:          url,
			AccessToken:  dp.AccessToken,
			RefreshToken: dp.DeviceToken,
			UserEmail:    dp.UserEmail,
			UserName:     dp.UserName,
		})
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Paired as %s. Saved instance %q. Run `nexora` to start.\n", dp.UserEmail, pairName)
		return nil
	},
}
