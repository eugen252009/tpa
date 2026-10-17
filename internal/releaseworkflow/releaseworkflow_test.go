package releaseworkflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRepositoryReleaseConfigurationAndPinnedActions(t *testing.T) {
	root := repositoryRoot(t)
	cfg, _, err := LoadConfig(".tpa-release.yml", root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Package.Architectures["riscv64"].Runtime != "emulated" || cfg.Package.Qualification.InstallCommand == "" {
		t.Fatalf("unexpected TPA dogfood architecture qualification: %+v", cfg.Package.Architectures)
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "reusable-deb-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	if got := strings.Count(workflowText, "repository: ${{ inputs.tpa-repository }}"); got != 5 {
		t.Fatalf("expected all five TPA source checkouts to use the configured repository; found %d", got)
	}
	if !strings.Contains(workflowText, "default: eugen252009/tpa") {
		t.Fatal("the upstream TPA repository must remain the default")
	}
	pinRE := regexp.MustCompile(`(?m)^\s*uses:\s+[^\s]+@([0-9a-f]{40})\s*$`)
	pins := pinRE.FindAllSubmatch(workflow, -1)
	if len(pins) < 5 {
		t.Fatalf("expected immutable full-SHA action pins; found %d", len(pins))
	}
	for _, line := range strings.Split(string(workflow), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "uses:") && strings.Contains(trimmed, "@") && !pinRE.MatchString(line) {
			t.Errorf("workflow action is not pinned to a full SHA: %s", line)
		}
	}
}

func TestDebianVersionFromTag(t *testing.T) {
	cases := map[string]string{
		"v1.2.3": "1.2.3", "v1.2.3-alpha": "1.2.3~alpha", "v1.2.3-alpha.2": "1.2.3~alpha.2",
		"v1.2.3-beta.1": "1.2.3~beta.1", "v1.2.3-rc.10": "1.2.3~rc.10",
	}
	for tag, want := range cases {
		got, err := DebianVersionFromTag(tag)
		if err != nil || got != want {
			t.Errorf("DebianVersionFromTag(%q) = %q, %v; want %q", tag, got, err, want)
		}
	}
	for _, tag := range []string{"1.2.3", "v01.2.3", "v1.2", "v1.2.3+build", "v1.2.3-preview.1"} {
		if _, err := DebianVersionFromTag(tag); err == nil {
			t.Errorf("accepted unsupported tag %q", tag)
		}
	}
	for _, expression := range [][3]string{
		{"1.2.3~alpha.1", "lt", "1.2.3~beta.1"},
		{"1.2.3~beta.1", "lt", "1.2.3~rc.1"},
		{"1.2.3~rc.10", "gt", "1.2.3~rc.2"},
		{"1.2.3~rc.1", "lt", "1.2.3"},
	} {
		cmd := exec.Command("dpkg", "--compare-versions", expression[0], expression[1], expression[2])
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("dpkg version order %v failed: %s %v", expression, output, err)
		}
	}
}

func TestSafeWorkspacePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeWorkspacePath(root, "outside/new/file"); err == nil {
		t.Fatal("accepted path through external symlink")
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeWorkspacePath(root, "dangling/file"); err == nil {
		t.Fatal("accepted path through dangling symlink")
	}
	if _, err := safeWorkspacePath(root, "inside/new/file"); err != nil {
		t.Fatalf("rejected safe not-yet-created path: %v", err)
	}
}

func TestCreatePlanBindsTagCommitAndOrdersMatrix(t *testing.T) {
	workspace := makeGitWorkspace(t)
	commit := gitTest(t, workspace, "rev-parse", "HEAD")
	gitTest(t, workspace, "tag", "v1.2.3-rc.1", commit)
	cfg := testConfig()
	cfg.Reproducibility = true
	cfg.Package.Architectures = map[string]ArchitectureConfig{
		"arm64": {Runner: "ubuntu-24.04-arm", Runtime: "native"},
		"amd64": {Runner: "ubuntu-24.04", Runtime: "native"},
	}
	plan, matrix, err := CreatePlan(cfg, workspace, "push", "refs/tags/v1.2.3-rc.1", commit, 9)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "1.2.3~rc.1" || plan.SourceCommit != commit || !plan.Publish || !plan.Release {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(matrix) != 4 || matrix[0].Architecture != "amd64" || matrix[0].Rebuild != 1 || matrix[1].Rebuild != 2 || matrix[2].Architecture != "arm64" {
		t.Fatalf("unexpected deterministic matrix: %+v", matrix)
	}
	if _, _, err := CreatePlan(cfg, workspace, "push", "refs/tags/v1.2.3-rc.1", strings.Repeat("a", 40), 9); err == nil {
		t.Fatal("accepted workflow commit that differs from checked out HEAD")
	}
	if _, _, err := CreatePlan(cfg, workspace, "push", "refs/heads/main", commit, 9); err == nil {
		t.Fatal("accepted branch push as release")
	}
}

func TestPlanBindsConfigurationAndCommittedInputs(t *testing.T) {
	workspace := makeGitWorkspace(t)
	script := filepath.Join(workspace, "postinst")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "add", "postinst")
	gitTest(t, workspace, "commit", "-q", "-m", "add maintainer script")
	commit := gitTest(t, workspace, "rev-parse", "HEAD")
	cfg := testConfig()
	cfg.Package.Scripts = map[string]string{"postinst": "postinst"}
	plan, _, err := CreatePlan(cfg, workspace, "workflow_dispatch", "refs/heads/main", commit, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanInputs(cfg, workspace, plan); err != nil {
		t.Fatal(err)
	}
	changedConfig := cfg
	changedConfig.Build.Command = "echo changed"
	if err := ValidatePlanInputs(changedConfig, workspace, plan); err == nil {
		t.Fatal("accepted configuration changed after planning")
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkspaceChanges(cfg, workspace); err == nil {
		t.Fatal("accepted a changed committed maintainer script")
	}
}

func TestBuildPackageAndReproducibleBundle(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is required")
	}
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("dpkg is required")
	}
	root := repositoryRoot(t)
	workspace := makeGitWorkspace(t)
	commit := gitTest(t, workspace, "rev-parse", "HEAD")
	gitTest(t, workspace, "tag", "v1.2.3-rc.1", commit)
	epochText := gitTest(t, workspace, "show", "-s", "--format=%ct", commit)
	epoch := int64(0)
	if _, err := fmt.Sscan(epochText, &epoch); err != nil {
		t.Fatal(err)
	}
	plan := Plan{SchemaVersion: 1, Project: "example", Package: "example", Version: "1.2.3~rc.1", Tag: "v1.2.3-rc.1",
		Ref: "refs/tags/v1.2.3-rc.1", SourceCommit: commit, SourceDateEpoch: epoch, Release: true, Reproducibility: true,
		Architectures: []string{"amd64"}, ArchitectureSpecs: map[string]ArchitectureConfig{"amd64": {Runner: "ubuntu-24.04", Runtime: "native"}}}
	cfg := testConfig()
	cfg.Project.Name = "example"
	cfg.Package.Name = "example"
	cfg.Reproducibility = true
	cfg.Package.Architectures = map[string]ArchitectureConfig{"amd64": {Runner: "ubuntu-24.04", Runtime: "native"}}
	cfg.Package.ControlFields = map[string]any{"ExampleField": "version-${version}"}
	cfg.Package.MetadataFile = ".release-control-fields.json"
	if err := os.WriteFile(filepath.Join(workspace, cfg.Package.MetadataFile), []byte(`{"MemaSchema":"schema-${version}"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Package.Qualification.Command = "true"
	if err := bindPlanInputs(cfg, workspace, &plan); err != nil {
		t.Fatal(err)
	}
	tpaBinary := filepath.Join(t.TempDir(), "tpa")
	build := exec.Command("go", "build", "-trimpath", "-o", tpaBinary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build TPA package engine: %s: %v", output, err)
	}
	tpaCommit := strings.Repeat("a", 40)
	artifactsRoot := t.TempDir()
	for rebuild := 1; rebuild <= 2; rebuild++ {
		stage := filepath.Join(workspace, "release-stage")
		if err := os.MkdirAll(filepath.Join(stage, "usr", "bin"), 0755); err != nil {
			t.Fatal(err)
		}
		payload := filepath.Join(stage, "usr", "bin", "example")
		if err := os.WriteFile(payload, []byte("#!/bin/sh\nprintf 'example\\n'\n"), 0755); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(payload, time.Unix(epoch, 0), time.Unix(epoch, 0))
		outputDir := filepath.Join(artifactsRoot, artifactName("amd64", rebuild))
		record, err := BuildPackage(cfg, plan, PackageBuild{Workspace: workspace, OutputDir: outputDir, Architecture: "amd64", TPABinary: tpaBinary, TPACommit: tpaCommit, BuildNumber: rebuild})
		if err != nil {
			t.Fatalf("build package %d: %v", rebuild, err)
		}
		record.PackageQualification = "passed"
		record.InstallQualification = "passed"
		record.UpgradeQualification = "not-configured"
		encoded, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "qualification.json"), append(encoded, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		if rebuild == 1 {
			if err := os.RemoveAll(stage); err != nil {
				t.Fatal(err)
			}
		}
	}
	bundleDir := filepath.Join(t.TempDir(), "bundle")
	manifest, err := CreateBundle(cfg, BundleOptions{Workspace: workspace, ArtifactsRoot: artifactsRoot, OutputDir: bundleDir,
		Plan: plan, TPACommit: tpaCommit, TPAVersion: "dev", GoVersion: runtime.Version(), SourceRepository: "example/project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 1 || manifest.Build.ProjectTests != "passed" || len(manifest.Build.RebuildHashes) != 2 || manifest.Artifacts[0].ControlFields["MemaSchema"] != "schema-1.2.3~rc.1" {
		t.Fatalf("unexpected release manifest: %+v", manifest)
	}
	if err := VerifyBundle(cfg, plan, bundleDir, "dev", tpaCommit); err != nil {
		t.Fatal(err)
	}
	plan.Publish = true
	statePath, fakeBin := installFakeGitHubCLI(t)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_STATE", statePath)
	t.Setenv("FAKE_GH_COMMIT", commit)
	t.Setenv("TPA_RELEASE_TPA_COMMIT", tpaCommit)
	options := PublishOptions{Workspace: workspace, BundleDir: bundleDir, Repository: "example/project", Config: cfg, Plan: plan, TPAVersion: "dev"}
	if err := PublishGitHubRelease(options); err != nil {
		t.Fatalf("publish through fake GitHub API: %v", err)
	}
	if err := PublishGitHubRelease(options); err != nil {
		t.Fatalf("idempotent publication retry: %v", err)
	}
	retryBundle := filepath.Join(t.TempDir(), "bundle")
	retryManifest, err := CreateBundle(cfg, BundleOptions{Workspace: workspace, ArtifactsRoot: artifactsRoot, OutputDir: retryBundle,
		Plan: plan, TPACommit: tpaCommit, TPAVersion: "dev", GoVersion: runtime.Version(), SourceRepository: "example/project", RunID: "retry-run", RunAttempt: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if retryManifest.SourceArchive != manifest.SourceArchive || retryManifest.ConfigSHA256 != manifest.ConfigSHA256 || retryManifest.ReleaseNotesSHA256 != manifest.ReleaseNotesSHA256 {
		t.Fatal("repeated bundle changed source or configuration identity")
	}
	retryOptions := options
	retryOptions.BundleDir = retryBundle
	if err := PublishGitHubRelease(retryOptions); err != nil {
		t.Fatalf("idempotent publication with fresh run provenance: %v", err)
	}
	if err := os.WriteFile(filepath.Join(retryBundle, "release-notes.md"), []byte("tampered release notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundle(cfg, plan, retryBundle, "dev", tpaCommit); err == nil {
		t.Fatal("bundle verification accepted modified release notes")
	}
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Draft   bool `json:"draft"`
		Uploads int  `json:"uploads"`
		Assets  []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if state.Draft || state.Uploads != 1 || len(state.Assets) != 5 {
		t.Fatalf("unexpected fake release state: %+v", state)
	}
	checksums := exec.Command("sha256sum", "-c", "SHA256SUMS.txt")
	checksums.Dir = filepath.Join(filepath.Dir(statePath), "assets")
	if output, err := checksums.CombinedOutput(); err != nil {
		t.Fatalf("published release checksums do not match normalized asset names: %s: %v", output, err)
	}
	remotePackageName := strings.ReplaceAll(manifest.Artifacts[0].Filename, "~", ".")
	remotePackage := filepath.Join(filepath.Dir(statePath), "assets", remotePackageName)
	if err := os.WriteFile(remotePackage, []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := PublishGitHubRelease(options); err == nil {
		t.Fatal("published retry accepted modified remote asset bytes")
	}
}

func installFakeGitHubCLI(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "release.json")
	fake := filepath.Join(root, "gh")
	program := `#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
args = sys.argv[1:]
state_path = pathlib.Path(os.environ["FAKE_GH_STATE"])
asset_dir = state_path.parent / "assets"
asset_dir.mkdir(exist_ok=True)
def load():
    return json.loads(state_path.read_text())
def save(value):
    state_path.write_text(json.dumps(value))
def path_arg():
    return next((a for a in args if a.startswith("repos/")), "")
def check_repo():
    if "--repo" not in args or args[args.index("--repo") + 1] != "example/project":
        print("release command omitted explicit repository: " + repr(args), file=sys.stderr); sys.exit(2)
if args[0] == "api":
    target = path_arg()
    if "/commits/" in target:
        print(json.dumps({"sha": os.environ["FAKE_GH_COMMIT"]}))
    elif "--method" in args and "PATCH" in args:
        value = load(); value["draft"] = False; save(value); print(json.dumps(value))
    elif "/releases/tags/" in target:
        if not state_path.exists() or load()["draft"]:
            print("gh: Not Found (HTTP 404)", file=sys.stderr); sys.exit(1)
        print(json.dumps(load()))
    elif target.endswith("/releases?per_page=100"):
        if state_path.exists(): print(json.dumps(load()))
    elif target.endswith("/releases/1"):
        if not state_path.exists():
            print("gh: Not Found (HTTP 404)", file=sys.stderr); sys.exit(1)
        print(json.dumps(load()))
    else:
        print("unexpected fake gh api request: " + repr(args), file=sys.stderr); sys.exit(2)
elif args[:2] == ["release", "create"]:
    check_repo()
    notes = pathlib.Path(args[args.index("--notes-file") + 1]).read_text()
    save({"id": 1, "tag_name": args[2], "draft": True, "name": args[args.index("--title") + 1], "body": notes, "assets": [], "uploads": 0})
elif args[:2] == ["release", "upload"]:
    check_repo()
    value = load()
    sources = []
    index = 3
    while index < len(args):
        if args[index] == "--repo":
            index += 2
            continue
        sources.append(args[index]); index += 1
    for source in sources:
        source_path = pathlib.Path(source)
        remote_name = source_path.name.replace("~", ".")
        shutil.copyfile(source_path, asset_dir / remote_name)
        value["assets"].append({"name": remote_name, "size": source_path.stat().st_size})
    value["uploads"] += 1
    save(value)
elif args[:2] == ["release", "download"]:
    check_repo()
    value = load(); dest = pathlib.Path(args[args.index("--dir") + 1]); dest.mkdir(parents=True, exist_ok=True)
    for asset in value["assets"]: shutil.copyfile(asset_dir / asset["name"], dest / asset["name"])
else:
    print("unexpected fake gh call: " + repr(args), file=sys.stderr); sys.exit(2)
`
	if err := os.WriteFile(fake, []byte(program), 0755); err != nil {
		t.Fatal(err)
	}
	return state, root
}

func makeGitWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "config", "user.email", "release-tests@example.invalid")
	gitTest(t, root, "config", "user.name", "Release Tests")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test source\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-notes.md"), []byte("Test release notes.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", "README.md", "release-notes.md")
	gitTest(t, root, "commit", "-q", "-m", "test source")
	return root
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, output, err)
	}
	return strings.TrimSpace(string(output))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func testConfig() Config {
	return Config{SchemaVersion: 1, Project: ProjectConfig{Name: "test", TestCommand: "true"},
		Build: BuildConfig{Command: "true"}, Package: PackageConfig{Name: "test", Description: "Test package",
			Maintainer: "Release Test <release@example.invalid>", StageRoot: "release-stage", Architectures: map[string]ArchitectureConfig{
				"amd64": {Runner: "ubuntu-24.04", Runtime: "native"}}, Qualification: QualificationConfig{InstallCommand: "true"}},
		Release: ReleaseConfig{NotesFile: "release-notes.md"}}
}
