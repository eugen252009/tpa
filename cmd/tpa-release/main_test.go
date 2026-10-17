package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eugen252009/tpa/internal/releaseworkflow"
)

func TestRunTestPhaseRejectsTrackedMetadataMutation(t *testing.T) {
	workspace := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "test source\n")
	write("release-notes.md", "Test release notes.\n")
	write("metadata.json", "{}\n")
	write(".tpa-release.yml", `schema_version: 1
project:
  name: example
  test_command: 'printf changed > metadata.json'
build:
  command: "true"
package:
  name: example
  description: Example package
  maintainer: Example <example@example.invalid>
  stage_root: .release-stage
  metadata_file: metadata.json
  architectures:
    amd64:
      runner: ubuntu-24.04
      runtime: build-only
release:
  notes_file: release-notes.md
`)

	git(t, workspace, "init", "-q")
	git(t, workspace, "config", "user.email", "release-test@example.invalid")
	git(t, workspace, "config", "user.name", "Release Test")
	git(t, workspace, "add", ".")
	git(t, workspace, "commit", "-q", "-m", "test source")
	commit := git(t, workspace, "rev-parse", "HEAD")
	cfg, _, err := releaseworkflow.LoadConfig(".tpa-release.yml", workspace)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := releaseworkflow.CreatePlan(cfg, workspace, "workflow_dispatch", "refs/heads/main", commit, 1)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	data, err := json.Marshal(struct {
		Plan releaseworkflow.Plan `json:"plan"`
	}{Plan: plan})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	err = runCommand([]string{"--config", ".tpa-release.yml", "--workspace", workspace,
		"--plan", planPath, "--phase", "test"})
	if err == nil || !strings.Contains(err.Error(), "modified tracked source file") {
		t.Fatalf("test phase did not reject tracked metadata mutation: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, output, err)
	}
	return strings.TrimSpace(string(output))
}
