package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"dv/internal/config"
	"dv/internal/docker"
	"dv/internal/xdg"
)

func currentAgentName(cfg config.Config) string {
	name := cfg.SelectedAgent
	if name == "" {
		name = cfg.DefaultContainer
	}
	return name
}

func getenv(keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return out
}

// resolveImage returns the image name and config, given an optional override name.
// If override is empty, the currently selected image is used.
func resolveImage(cfg config.Config, override string) (string, config.ImageConfig, error) {
	name := override
	if name == "" {
		name = cfg.SelectedImage
	}
	img, ok := cfg.Images[name]
	if !ok {
		return "", config.ImageConfig{}, fmt.Errorf("unknown image '%s'", name)
	}
	return name, img, nil
}

// isPortInUse returns true when the given TCP port cannot be bound on localhost.
func isPortInUse(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = l.Close()
	return false
}

// completeAgentNames suggests existing container names for the selected image.
func completeAgentNames(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	configDir, err := xdg.ConfigDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.LoadOrCreate(configDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	_, imgCfg, err := resolveImage(cfg, "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out, _ := runShell("docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Labels}}'")
	var suggestions []string
	prefix := strings.ToLower(strings.TrimSpace(toComplete))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		name, image := parts[0], parts[1]
		labelsField := ""
		if len(parts) >= 3 {
			labelsField = parts[2]
		}
		belongs := false
		if imgNameFromCfg, ok := cfg.ContainerImages[name]; ok && imgNameFromCfg == cfg.SelectedImage {
			belongs = true
		}
		if !belongs {
			if labelMap := parseLabels(labelsField); labelMap["com.dv.owner"] == "dv" && labelMap["com.dv.image-name"] == cfg.SelectedImage {
				belongs = true
			}
		}
		if !belongs {
			if image == imgCfg.Tag {
				belongs = true
			}
		}
		if !belongs {
			continue
		}
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
			suggestions = append(suggestions, name)
		}
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp
}

func autogenName() string {
	return fmt.Sprintf("ai_agent_%s", time.Now().Format("20060102-150405"))
}

func runShell(script string) (string, error) {
	return execCombined("bash", "-lc", script)
}

func execCombined(name string, arg ...string) (string, error) {
	cmd := execCommand(name, arg...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var execCommand = defaultExec

// indirection for testing
func defaultExec(name string, arg ...string) *exec.Cmd { return exec.Command(name, arg...) }

func containerImage(name string) (string, error) {
	out, err := runShell(fmt.Sprintf("docker inspect -f '{{.Config.Image}}' %s 2>/dev/null || true", name))
	return strings.TrimSpace(out), err
}

func ensureContainerRunning(cmd *cobra.Command, cfg config.Config, name string, reset bool) error {
	// Fallback: if container has a recorded image, use that; else use selected image
	imgName := cfg.ContainerImages[name]
	_, imgCfg, err := resolveImage(cfg, imgName)
	if err != nil {
		return err
	}
	workdir := imgCfg.Workdir
	imageTag := imgCfg.Tag
	return ensureContainerRunningWithWorkdir(cmd, cfg, name, workdir, imageTag, imgName, reset)
}

func ensureContainerRunningWithWorkdir(cmd *cobra.Command, cfg config.Config, name string, workdir string, imageTag string, imgName string, reset bool) error {
	if reset && docker.Exists(name) {
		_ = docker.Stop(name)
		_ = docker.Remove(name)
	}
	if !docker.Exists(name) {
		// Choose the first available port starting from configured starting port
		chosenPort := cfg.HostStartingPort
		for isPortInUse(chosenPort) {
			chosenPort++
		}
		labels := map[string]string{
			"com.dv.owner":      "dv",
			"com.dv.image-name": imgName,
			"com.dv.image-tag":  imageTag,
		}
		envs := map[string]string{
			"DISCOURSE_PORT": strconv.Itoa(chosenPort),
		}
		if err := docker.RunDetached(name, workdir, imageTag, chosenPort, cfg.ContainerPort, labels, envs); err != nil {
			return err
		}
	} else if !docker.Running(name) {
		if err := docker.Start(name); err != nil {
			return err
		}
	}
	return nil
}
