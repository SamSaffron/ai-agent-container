package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"dv/internal/config"
	"dv/internal/docker"
	"dv/internal/xdg"
)

var restartDiscourseCmd = &cobra.Command{
	Use:   "discourse",
	Short: "Smart restart of Discourse services inside the container",
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
			fmt.Fprintf(cmd.OutOrStdout(), "Container '%s' does not exist. Run 'dv start' first.\n", name)
			return nil
		}

		// Ensure container is running
		if !docker.Running(name) {
			fmt.Fprintf(cmd.OutOrStdout(), "Starting container '%s'...\n", name)
			if err := docker.Start(name); err != nil {
				return err
			}
		}

		// Determine workdir from the associated image if known; fall back to selected image
		imgName := cfg.ContainerImages[name]
		var imgCfg config.ImageConfig
		if imgName != "" {
			imgCfg = cfg.Images[imgName]
		} else {
			_, imgCfg, err = resolveImage(cfg, "")
			if err != nil {
				return err
			}
		}
		workdir := imgCfg.Workdir

		// Stop services that exist under /etc/service
		fmt.Fprintf(cmd.OutOrStdout(), "Stopping services (if present)...\n")
		stopScript := `set -e
has_service() { [ -d "/etc/service/$1" ]; }
if has_service unicorn; then sv stop unicorn || true; fi
if has_service ember-cli; then sv stop ember-cli || true; fi
if has_service sidekiq; then sv stop sidekiq || true; fi
sleep 1`
		_, _ = docker.ExecAsRoot(name, workdir, []string{"bash", "-lc", stopScript})

		// Kill leftover app processes owned by 'discourse' (avoid killing runsv)
		fmt.Fprintf(cmd.OutOrStdout(), "Killing leftover unicorn/sidekiq processes (if any)...\n")
		killScript := `set -e
# Only kill processes owned by discourse user to avoid killing runsv
pkill -u discourse -9 -f 'bin/unicorn' 2>/dev/null || true
pkill -u discourse -9 -f 'sidekiq' 2>/dev/null || true
sleep 1`
		_, _ = docker.ExecAsRoot(name, workdir, []string{"bash", "-lc", killScript})

		// Start available services
		fmt.Fprintf(cmd.OutOrStdout(), "Starting services (if present)...\n")
		startScript := `set -e
has_service() { [ -d "/etc/service/$1" ]; }
if has_service sidekiq; then sv start sidekiq || true; fi
if has_service unicorn; then sv start unicorn || true; fi
if has_service ember-cli; then sv start ember-cli || true; fi
sleep 1`
		_, _ = docker.ExecAsRoot(name, workdir, []string{"bash", "-lc", startScript})

		// Print status only for services that exist
		fmt.Fprintf(cmd.OutOrStdout(), "Service status:\n")
		statusScript := `set -e
services=()
for s in sidekiq unicorn ember-cli; do
  [ -d "/etc/service/$s" ] && services+=("$s")
done
if [ ${#services[@]} -gt 0 ]; then
  sv status "${services[@]}" || true
else
  echo "No runit services found"
fi`
		if out, err := docker.ExecAsRoot(name, workdir, []string{"bash", "-lc", statusScript}); err == nil {
			fmt.Fprint(cmd.OutOrStdout(), out)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Discourse services restarted.\n")
		return nil
	},
}

func init() {
	restartDiscourseCmd.Flags().String("name", "", "Container name (defaults to selected or default)")
	restartCmd.AddCommand(restartDiscourseCmd)
}
