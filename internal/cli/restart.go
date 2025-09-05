package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"dv/internal/config"
	"dv/internal/docker"
	"dv/internal/xdg"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the container",
	RunE: func(cmd *cobra.Command, args []string) error {
		configDir, err := xdg.ConfigDir()
		if err != nil {
			return err
		}
		cfg, err := config.LoadOrCreate(configDir)
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			name = currentAgentName(cfg)
		}

		if !docker.Exists(name) {
			fmt.Fprintf(cmd.OutOrStdout(), "Container '%s' does not exist\n", name)
			return nil
		}

		if docker.Running(name) {
			fmt.Fprintf(cmd.OutOrStdout(), "Stopping container '%s'...\n", name)
			if err := docker.Stop(name); err != nil {
				return err
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Starting container '%s'...\n", name)
		if err := docker.Start(name); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Container '%s' restarted successfully\n", name)
		return nil
	},
}

func init() {
	restartCmd.Flags().String("name", "", "Container name (defaults to selected or default)")
}
