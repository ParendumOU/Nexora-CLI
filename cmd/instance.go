package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gitlab.com/parendum/nexora/nexora-cli/internal/config"
)

func init() {
	instanceCmd.AddCommand(instanceListCmd, instanceUseCmd)
	rootCmd.AddCommand(instanceCmd)
}

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage saved Nexora instances.",
}

var instanceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved instances.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(cfg.Instances) == 0 {
			fmt.Println("No instances. Run `nexora login` or `nexora pair`.")
			return nil
		}
		for name, inst := range cfg.Instances {
			marker := "  "
			if name == cfg.Current {
				marker = "* "
			}
			fmt.Printf("%s%-16s %s (%s)\n", marker, name, inst.URL, inst.UserEmail)
		}
		return nil
	},
}

var instanceUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Switch the active instance.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if _, ok := cfg.Instances[args[0]]; !ok {
			return fmt.Errorf("no instance named %q", args[0])
		}
		cfg.Current = args[0]
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("Switched to %q.\n", args[0])
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the NexoraCLI version.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("nexora", Version)
	},
}

func init() { rootCmd.AddCommand(versionCmd) }
