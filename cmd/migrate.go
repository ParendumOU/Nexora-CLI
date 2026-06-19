package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
)

var (
	migrateTo      string
	migrateFrom    string
	migrateVectors bool
	migrateMode    string
	migrateYes     bool
)

func init() {
	migrateCmd.Flags().StringVar(&migrateTo, "to", "", "target instance name (must be saved — see `nexora instance list`)")
	migrateCmd.Flags().StringVar(&migrateFrom, "from", "", "source instance name (defaults to the active instance)")
	migrateCmd.Flags().BoolVar(&migrateVectors, "include-vectors", false, "ship embedding vectors verbatim (only if target uses the same embedding model)")
	migrateCmd.Flags().StringVar(&migrateMode, "mode", "skip", "restore mode on the target: skip | overwrite")
	migrateCmd.Flags().BoolVar(&migrateYes, "yes", false, "skip the confirmation prompt")
	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:   "migrate --to <instance>",
	Short: "Migrate everything from one instance into another (e.g. community → Cloud).",
	Long: "Export the full source instance (orgs, agents, chats, knowledge bases, providers, " +
		"settings, custom seeds, file uploads) and restore it into a target instance in one step.\n\n" +
		"Both instances must already be saved (`nexora login`/`nexora pair`) and the target must " +
		"share the source ENCRYPTION_KEY (encrypted secrets ship as-is). The CLI relays the backup " +
		"through this machine, so it works even when the two servers can't reach each other.\n\n" +
		"Note: user passwords and per-device pairings don't transfer — users reset password / re-pair " +
		"on the new instance.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(migrateTo) == "" {
			return fmt.Errorf("--to <instance> is required")
		}
		if migrateMode != "skip" && migrateMode != "overwrite" {
			return fmt.Errorf("--mode must be 'skip' or 'overwrite'")
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		srcName := migrateFrom
		if srcName == "" {
			srcName = cfg.Current
		}
		src := cfg.Instances[srcName]
		if src == nil {
			return fmt.Errorf("no source instance %q — run `nexora login` first", srcName)
		}
		tgt := cfg.Instances[migrateTo]
		if tgt == nil {
			return fmt.Errorf("no target instance %q — save it with `nexora login --name %s` first", migrateTo, migrateTo)
		}
		if srcName == migrateTo {
			return fmt.Errorf("source and target are the same instance")
		}

		fmt.Printf("Migrate:\n  from  %-12s %s\n  to    %-12s %s\n  mode  %s (vectors: %v)\n",
			srcName, src.URL, migrateTo, tgt.URL, migrateMode, migrateVectors)
		if !migrateYes {
			fmt.Print("Proceed? [y/N] ")
			var resp string
			fmt.Scanln(&resp)
			if strings.ToLower(strings.TrimSpace(resp)) != "y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		ctx := context.Background()
		srcClient := newClient(cfg, src)
		tgtClient := newClient(cfg, tgt)

		// 1. Build the backup on the source.
		fmt.Println("→ building backup on source…")
		jobID, err := srcClient.StartBackup(ctx, "instance", migrateVectors)
		if err != nil {
			return fmt.Errorf("start export: %w", err)
		}
		for {
			time.Sleep(3 * time.Second)
			st, err := srcClient.BackupStatus(ctx, jobID)
			if err != nil {
				return fmt.Errorf("poll export: %w", err)
			}
			if st.Status == "done" {
				break
			}
			if st.Status == "failed" {
				return fmt.Errorf("export failed: %s", st.Error)
			}
			fmt.Printf("  …%s\n", st.Status)
		}

		// 2. Download it to a temp file.
		tmp, err := os.CreateTemp("", "nexora-migrate-*.zip")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)
		fmt.Println("→ downloading backup…")
		if err := srcClient.DownloadBackup(ctx, jobID, tmpPath); err != nil {
			return fmt.Errorf("download backup: %w", err)
		}
		if fi, err := os.Stat(tmpPath); err == nil {
			fmt.Printf("  %.1f MB → %s\n", float64(fi.Size())/(1024*1024), filepath.Base(tmpPath))
		}

		// 3. Import into the target. Re-embed only when vectors weren't shipped.
		fmt.Println("→ restoring into target (this can take a while)…")
		summary, err := tgtClient.ImportBackup(ctx, tmpPath, migrateMode, !migrateVectors, false)
		if err != nil {
			return fmt.Errorf("restore into target: %w", err)
		}

		out, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Printf("✓ migration complete.\n%s\n", string(out))
		return nil
	},
}
