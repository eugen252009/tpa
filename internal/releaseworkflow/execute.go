package releaseworkflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func SafeProjectPath(workspace, path string) (string, error) {
	return safeWorkspacePath(workspace, path)
}

func Digest(path string) (string, int64, error) { return fileDigest(path) }

func ExecuteProjectCommand(command, workspace string, values map[string]string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("project command is empty")
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", command)
	cmd.Dir = workspace
	cmd.Env = overlayEnvironment(os.Environ(), values)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("project command failed: %w", err)
	}
	return nil
}

func overlayEnvironment(original []string, overrides map[string]string) []string {
	result := make([]string, 0, len(original)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, entry := range original {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, replace := overrides[name]; replace {
			if !seen[name] {
				result = append(result, name+"="+overrides[name])
				seen[name] = true
			}
			continue
		}
		result = append(result, entry)
	}
	for name, value := range overrides {
		if !seen[name] {
			result = append(result, name+"="+value)
		}
	}
	return result
}
