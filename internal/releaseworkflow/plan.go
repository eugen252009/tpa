package releaseworkflow

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Plan struct {
	SchemaVersion      int                           `json:"schema_version"`
	Project            string                        `json:"project"`
	Package            string                        `json:"package"`
	Version            string                        `json:"version"`
	Tag                string                        `json:"tag,omitempty"`
	Ref                string                        `json:"ref"`
	SourceCommit       string                        `json:"source_commit"`
	SourceDateEpoch    int64                         `json:"source_date_epoch"`
	ConfigSHA256       string                        `json:"config_sha256"`
	ReleaseNotesSHA256 string                        `json:"release_notes_sha256"`
	SourceInputs       map[string]string             `json:"source_inputs"`
	Release            bool                          `json:"release"`
	Publish            bool                          `json:"publish"`
	Reproducibility    bool                          `json:"reproducibility"`
	Architectures      []string                      `json:"architectures"`
	ArchitectureSpecs  map[string]ArchitectureConfig `json:"architecture_specs"`
}

type MatrixEntry struct {
	Architecture string `json:"architecture"`
	Runner       string `json:"runner"`
	Runtime      string `json:"runtime"`
	Rebuild      int    `json:"rebuild"`
}

func CreatePlan(cfg Config, workspace, eventName, ref, commit string, runNumber int64) (Plan, []MatrixEntry, error) {
	if len(commit) != 40 || !isLowerHex(commit) {
		return Plan{}, nil, fmt.Errorf("source commit must be a full lowercase 40-character SHA-1")
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return Plan{}, nil, err
	}
	current, err := gitOutput(workspace, "rev-parse", "HEAD")
	if err != nil {
		return Plan{}, nil, fmt.Errorf("resolve checked-out source commit: %w", err)
	}
	if current != commit {
		return Plan{}, nil, fmt.Errorf("checked-out source commit %s does not match workflow commit %s", current, commit)
	}
	status, err := gitOutput(workspace, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return Plan{}, nil, fmt.Errorf("check source checkout status: %w", err)
	}
	if status != "" {
		return Plan{}, nil, fmt.Errorf("source checkout is not clean; release inputs must match the selected commit")
	}
	if err := validateSourceTree(workspace, commit); err != nil {
		return Plan{}, nil, err
	}

	plan := Plan{SchemaVersion: 1, Project: cfg.Project.Name, Package: cfg.Package.Name, Ref: ref,
		SourceCommit: commit, Reproducibility: cfg.Reproducibility, ArchitectureSpecs: cfg.Package.Architectures}
	isTag := strings.HasPrefix(ref, "refs/tags/")
	if isTag {
		plan.Tag = strings.TrimPrefix(ref, "refs/tags/")
		if strings.Contains(plan.Tag, "/") || plan.Tag == "" {
			return Plan{}, nil, fmt.Errorf("invalid release tag reference %q", ref)
		}
		version, mapErr := DebianVersionFromTag(plan.Tag)
		if mapErr != nil {
			return Plan{}, nil, mapErr
		}
		tagCommit, resolveErr := gitOutput(workspace, "rev-parse", "--verify", "refs/tags/"+plan.Tag+"^{commit}")
		if resolveErr != nil {
			return Plan{}, nil, fmt.Errorf("resolve exact release tag %s: %w", plan.Tag, resolveErr)
		}
		if tagCommit != commit {
			return Plan{}, nil, fmt.Errorf("tag %s resolves to %s, not workflow source commit %s", plan.Tag, tagCommit, commit)
		}
		plan.Version = version
		plan.Release = true
		plan.Publish = eventName == "push"
		if err := checkSourceVersion(cfg, workspace, commit, plan.Version); err != nil {
			return Plan{}, nil, err
		}
	} else {
		if eventName == "push" {
			return Plan{}, nil, fmt.Errorf("a push release must reference a Git tag")
		}
		if runNumber < 1 {
			return Plan{}, nil, fmt.Errorf("qualification run number must be positive")
		}
		plan.Version = "0.0.0~qualification." + strconv.FormatInt(runNumber, 10)
	}
	if err := validateDebianVersion(plan.Version); err != nil {
		return Plan{}, nil, err
	}
	epochText, err := gitOutput(workspace, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return Plan{}, nil, fmt.Errorf("resolve source commit timestamp: %w", err)
	}
	plan.SourceDateEpoch, err = strconv.ParseInt(strings.TrimSpace(epochText), 10, 64)
	if err != nil || plan.SourceDateEpoch < 0 {
		return Plan{}, nil, fmt.Errorf("source commit has an invalid canonical timestamp")
	}
	plan.Architectures = cfg.SortedArchitectures()
	if err := validateDebianVersion(plan.Version); err != nil {
		return Plan{}, nil, err
	}
	if err := cfg.Validate(workspace); err != nil {
		return Plan{}, nil, err
	}
	if err := bindPlanInputs(cfg, workspace, &plan); err != nil {
		return Plan{}, nil, err
	}
	rebuilds := 1
	if cfg.Reproducibility {
		rebuilds = 2
	}
	matrix := make([]MatrixEntry, 0, len(plan.Architectures)*rebuilds)
	for _, arch := range plan.Architectures {
		spec := cfg.Package.Architectures[arch]
		for build := 1; build <= rebuilds; build++ {
			matrix = append(matrix, MatrixEntry{Architecture: arch, Runner: spec.Runner, Runtime: spec.Runtime, Rebuild: build})
		}
	}
	return plan, matrix, nil
}

func validateSourceTree(workspace, commit string) error {
	cmd := exec.Command("git", "ls-tree", "-r", "--format=%(objectmode)", commit)
	cmd.Dir = workspace
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	hasSubmodule := false
	for scanner.Scan() {
		if scanner.Text() == "160000" {
			hasSubmodule = true
			break
		}
	}
	scanErr := scanner.Err()
	if hasSubmodule || scanErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if scanErr != nil {
		return scanErr
	}
	if hasSubmodule {
		return fmt.Errorf("source archives do not expand Git submodules; vendor required source files")
	}
	if waitErr != nil {
		return waitErr
	}
	lfsMarker := "version https://git-lfs.github.com/" + "spec/v1"
	lfs := exec.Command("git", "grep", "-q", "-I", "-F", lfsMarker, commit, "--")
	lfs.Dir = workspace
	if err := lfs.Run(); err == nil {
		return fmt.Errorf("source archives do not expand Git LFS objects; vendor required source files")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		return fmt.Errorf("inspect source tree for Git LFS pointers: %w", err)
	}
	return nil
}

func bindPlanInputs(cfg Config, workspace string, plan *Plan) error {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	configBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	configHash := sha256.Sum256(configBytes)
	plan.ConfigSHA256 = hex.EncodeToString(configHash[:])
	if _, err := safeWorkspacePath(workspace, cfg.Release.NotesFile); err != nil {
		return err
	}
	notesRelative := filepath.Clean(filepath.FromSlash(cfg.Release.NotesFile))
	notes, err := readGitFile(workspace, plan.SourceCommit, notesRelative)
	if err != nil {
		return fmt.Errorf("read committed release notes: %w", err)
	}
	notesHash := sha256.Sum256(notes)
	plan.ReleaseNotesSHA256 = hex.EncodeToString(notesHash[:])
	paths := make(map[string]bool)
	if cfg.Project.SourceVersionCheck.File != "" {
		paths[filepath.Clean(filepath.FromSlash(cfg.Project.SourceVersionCheck.File))] = true
	}
	for _, relative := range cfg.Package.Scripts {
		paths[filepath.Clean(filepath.FromSlash(relative))] = true
	}
	plan.SourceInputs = make(map[string]string, len(paths))
	for relative := range paths {
		file, err := safeWorkspacePath(workspace, relative)
		if err != nil {
			return err
		}
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release input %s must be a regular file", relative)
		}
		content, err := readGitFile(workspace, plan.SourceCommit, relative)
		if err != nil {
			return fmt.Errorf("read committed release input %s: %w", relative, err)
		}
		digest := sha256.Sum256(content)
		plan.SourceInputs[filepath.ToSlash(relative)] = hex.EncodeToString(digest[:])
	}
	return nil
}

func ValidatePlanInputs(cfg Config, workspace string, plan Plan) error {
	if plan.SchemaVersion != 1 || plan.Project != cfg.Project.Name || plan.Package != cfg.Package.Name || len(plan.SourceCommit) != 40 || !isLowerHex(plan.SourceCommit) {
		return fmt.Errorf("release plan identity is invalid for this project")
	}
	if plan.Reproducibility != cfg.Reproducibility || !equalStringSlice(plan.Architectures, cfg.SortedArchitectures()) || len(plan.ArchitectureSpecs) != len(cfg.Package.Architectures) {
		return fmt.Errorf("release plan matrix differs from the validated project configuration")
	}
	for architecture, spec := range cfg.Package.Architectures {
		if plan.ArchitectureSpecs[architecture] != spec {
			return fmt.Errorf("release plan architecture %s differs from configuration", architecture)
		}
	}
	if plan.Tag != "" {
		mapped, err := DebianVersionFromTag(plan.Tag)
		if err != nil || mapped != plan.Version || !plan.Release {
			return fmt.Errorf("release plan tag/version mapping is invalid")
		}
		tagCommit, err := gitOutput(workspace, "rev-parse", "--verify", "refs/tags/"+plan.Tag+"^{commit}")
		if err != nil || tagCommit != plan.SourceCommit {
			return fmt.Errorf("release tag no longer resolves to the planned source commit")
		}
	} else if plan.Release || plan.Publish {
		return fmt.Errorf("non-tag qualification plan cannot publish")
	}
	currentHead, err := gitOutput(workspace, "rev-parse", "HEAD")
	if err != nil || currentHead != plan.SourceCommit {
		return fmt.Errorf("checked-out source no longer matches the release plan")
	}
	epochText, err := gitOutput(workspace, "show", "-s", "--format=%ct", plan.SourceCommit)
	if err != nil {
		return err
	}
	epoch, err := strconv.ParseInt(epochText, 10, 64)
	if err != nil || epoch != plan.SourceDateEpoch {
		return fmt.Errorf("release plan source timestamp is invalid")
	}
	current := Plan{}
	if err := bindPlanInputs(cfg, workspace, &current); err != nil {
		return err
	}
	if len(plan.ConfigSHA256) != 64 || !isLowerHex(plan.ConfigSHA256) || plan.ConfigSHA256 != current.ConfigSHA256 {
		return fmt.Errorf("release configuration differs from the validated source plan")
	}
	if plan.ReleaseNotesSHA256 != current.ReleaseNotesSHA256 || !equalStringMap(plan.SourceInputs, current.SourceInputs) {
		return fmt.Errorf("release notes or package input files differ from the validated source commit")
	}
	return nil
}

func ValidateWorkspaceChanges(cfg Config, workspace string) error {
	tracked, err := gitPathList(workspace, "diff", "--name-only", "-z", "--no-renames", "HEAD", "--")
	if err != nil {
		return err
	}
	untracked, err := gitPathList(workspace, "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return err
	}
	stage := filepath.ToSlash(filepath.Clean(filepath.FromSlash(cfg.Package.StageRoot)))
	metadata := filepath.ToSlash(filepath.Clean(filepath.FromSlash(cfg.Package.MetadataFile)))
	allowedStage := func(value string) bool { return value == stage || strings.HasPrefix(value, stage+"/") }
	for _, value := range tracked {
		if value != "" {
			return fmt.Errorf("project command modified tracked source file %q", value)
		}
	}
	for _, value := range untracked {
		if value == "" || allowedStage(value) || (metadata != "" && value == metadata) {
			continue
		}
		return fmt.Errorf("project command created unexpected source file %q", value)
	}
	return nil
}

func gitPathList(dir string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	parts := strings.Split(string(data), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts, nil
}

func readGitFile(workspace, commit, relative string) ([]byte, error) {
	if strings.ContainsAny(relative, "\x00\r\n") || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("invalid source input path")
	}
	gitPath := filepath.ToSlash(filepath.Clean(relative))
	sizeText, err := gitOutput(workspace, "cat-file", "-s", commit+":"+gitPath)
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("source input %s exceeds 1 MiB", gitPath)
	}
	cmd := exec.Command("git", "show", commit+":"+gitPath)
	cmd.Dir = workspace
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", commit, gitPath, err)
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("source input %s exceeds 1 MiB", gitPath)
	}
	return data, nil
}

func checkSourceVersion(cfg Config, workspace, commit, releaseVersion string) error {
	check := cfg.Project.SourceVersionCheck
	if check.File == "" {
		return nil
	}
	path, err := safeWorkspacePath(workspace, check.File)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(workspace, path)
	if err != nil {
		return err
	}
	data, err := readGitFile(workspace, commit, relative)
	if err != nil {
		return fmt.Errorf("read source version declaration: %w", err)
	}
	pattern, err := regexp.Compile(check.Pattern)
	if err != nil {
		return fmt.Errorf("compile source version check: %w", err)
	}
	matches := pattern.FindAllSubmatch(data, 2)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return fmt.Errorf("source version check did not find exactly one captured version in %s", check.File)
	}
	declared := strings.TrimSpace(string(matches[0][1]))
	mapped := declared
	if !strings.HasPrefix(mapped, "v") {
		mapped = "v" + mapped
	}
	if semverMapped, mapErr := DebianVersionFromTag(mapped); mapErr == nil {
		mapped = semverMapped
	}
	if mapped != releaseVersion {
		return fmt.Errorf("source version %q maps to Debian version %q, but tag selects %q", declared, mapped, releaseVersion)
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, fmt.Errorf("%s is not a regular file no larger than 16 MiB", path)
	}
	return os.ReadFile(path)
}

func validateDebianVersion(version string) error {
	cmd := exec.Command("dpkg", "--validate-version", version)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid Debian version %q: %s", version, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
