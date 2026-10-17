package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eugen252009/tpa/internal/releaseworkflow"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = planCommand(os.Args[2:])
	case "run":
		err = runCommand(os.Args[2:])
	case "package":
		err = packageCommand(os.Args[2:])
	case "record":
		err = recordCommand(os.Args[2:])
	case "bundle":
		err = bundleCommand(os.Args[2:])
	case "verify-bundle":
		err = verifyBundleCommand(os.Args[2:])
	case "publish":
		err = publishCommand(os.Args[2:])
	case "help", "--help", "-h":
		usage()
		return
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tpa-release:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: tpa-release <command>

Commands:
  plan          Validate project configuration, tag, source commit, and matrix
  run           Run a project-configured build/test/qualification command
  package       Build a .deb by calling the pinned TPA init/build engine
  record        Record package and qualification results for one architecture
  bundle        Verify builds and create checksums, manifest, source, provenance
  verify-bundle Independently validate a release bundle
  publish       Safely publish a complete bundle as a GitHub Release`)
}

func planCommand(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	eventName := flags.String("event-name", "", "GitHub event name")
	ref := flags.String("ref", "", "GitHub ref")
	commit := flags.String("commit", "", "exact source commit SHA")
	runNumber := flags.Int64("run-number", 1, "GitHub run number for qualification builds")
	output := flags.String("output", "", "write plan JSON to this file")
	githubOutput := flags.String("github-output", "", "append workflow outputs to this file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, matrix, err := releaseworkflow.CreatePlan(cfg, *workspace, *eventName, *ref, *commit, *runNumber)
	if err != nil {
		return err
	}
	result := struct {
		Plan   releaseworkflow.Plan          `json:"plan"`
		Matrix []releaseworkflow.MatrixEntry `json:"matrix"`
	}{Plan: plan, Matrix: matrix}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if *output != "" {
		if err := os.WriteFile(*output, append(encoded, '\n'), 0600); err != nil {
			return err
		}
	}
	if *githubOutput != "" {
		values := map[string]string{
			"version": plan.Version, "tag": plan.Tag, "commit": plan.SourceCommit,
			"source_date_epoch": strconv.FormatInt(plan.SourceDateEpoch, 10),
			"publish":           strconv.FormatBool(plan.Publish),
			"release":           strconv.FormatBool(plan.Release),
			"matrix":            string(mustJSON(matrix)),
			"has_upgrade":       strconv.FormatBool(releaseworkflow.CommandConfigured(cfg.Package.Qualification.UpgradeCommand)),
		}
		if err := appendGitHubOutputs(*githubOutput, values); err != nil {
			return err
		}
	}
	fmt.Println(string(encoded))
	return nil
}

func runCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	phase := flags.String("phase", "", "build, test, qualify, install, or upgrade")
	architecture := flags.String("architecture", "", "Debian architecture")
	packagePath := flags.String("package", "", "built Debian package")
	rebuild := flags.Int("rebuild", 1, "independent rebuild number")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	if err := releaseworkflow.ValidatePlanInputs(cfg, *workspace, plan); err != nil {
		return err
	}
	if err := releaseworkflow.ValidateWorkspaceChanges(cfg, *workspace); err != nil {
		return err
	}
	stage, err := cfg.PackageStageRoot(*workspace, *architecture)
	if *phase == "test" {
		stage = ""
		err = nil
	}
	if err != nil {
		return err
	}
	command := ""
	switch *phase {
	case "build":
		command = cfg.Build.Command
	case "test":
		command = cfg.Project.TestCommand
	case "qualify":
		command = cfg.Package.Qualification.Command
	case "install":
		command = cfg.Package.Qualification.InstallCommand
	case "upgrade":
		command = cfg.Package.Qualification.UpgradeCommand
	default:
		return fmt.Errorf("unsupported command phase %q", *phase)
	}
	if strings.TrimSpace(command) == "" {
		if *phase == "qualify" {
			fmt.Println("no architecture-specific qualification command configured")
			return nil
		}
		return fmt.Errorf("phase %s is not configured", *phase)
	}
	if *phase == "build" {
		if _, err := os.Lstat(stage); err == nil {
			return fmt.Errorf("configured stage root already exists; refusing to remove project files: %s", stage)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(stage, 0755); err != nil {
			return err
		}
	}
	metadataFile := ""
	if cfg.Package.MetadataFile != "" {
		metadataFile, err = releaseworkflow.SafeProjectPath(*workspace, cfg.Package.MetadataFile)
		if err != nil {
			return err
		}
	}
	buildFlagsJSON, err := json.Marshal(cfg.Build.Flags)
	if err != nil {
		return err
	}
	env := map[string]string{
		"TPA_RELEASE_BUILD_FLAGS":   string(buildFlagsJSON),
		"TPA_RELEASE_PHASE":         *phase,
		"TPA_RELEASE_PROJECT":       cfg.Project.Name,
		"TPA_RELEASE_PACKAGE":       cfg.Package.Name,
		"TPA_RELEASE_VERSION":       plan.Version,
		"TPA_RELEASE_TAG":           plan.Tag,
		"TPA_RELEASE_SOURCE_COMMIT": plan.SourceCommit,
		"SOURCE_DATE_EPOCH":         strconv.FormatInt(plan.SourceDateEpoch, 10),
		"TPA_RELEASE_ARCH":          *architecture,
		"DEB_HOST_ARCH":             *architecture,
		"TPA_RELEASE_STAGE_ROOT":    stage,
		"TPA_RELEASE_PACKAGE_FILE":  *packagePath,
		"TPA_RELEASE_METADATA_FILE": metadataFile,
		"TPA_RELEASE_REBUILD":       strconv.Itoa(*rebuild),
	}
	if err := releaseworkflow.ExecuteProjectCommand(command, *workspace, env); err != nil {
		return err
	}
	if err := releaseworkflow.ValidatePlanInputs(cfg, *workspace, plan); err != nil {
		return err
	}
	if err := releaseworkflow.ValidateWorkspaceChanges(cfg, *workspace); err != nil {
		return err
	}
	return nil
}

func packageCommand(args []string) error {
	flags := flag.NewFlagSet("package", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	architecture := flags.String("architecture", "", "Debian architecture")
	outputDir := flags.String("output", "", "package output directory")
	tpaBinary := flags.String("tpa", "", "pinned TPA package builder")
	tpaCommit := flags.String("tpa-commit", "", "pinned TPA builder source commit")
	rebuild := flags.Int("rebuild", 1, "independent rebuild number")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	record, err := releaseworkflow.BuildPackage(cfg, plan, releaseworkflow.PackageBuild{Workspace: *workspace,
		OutputDir: *outputDir, Architecture: *architecture, TPABinary: *tpaBinary, TPACommit: *tpaCommit, BuildNumber: *rebuild})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func recordCommand(args []string) error {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	architecture := flags.String("architecture", "", "Debian architecture")
	packagePath := flags.String("package", "", "built Debian package")
	tpaVersion := flags.String("tpa-version", "", "pinned TPA builder version")
	rebuild := flags.Int("rebuild", 1, "independent rebuild number")
	output := flags.String("output", "", "qualification record output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	if err := releaseworkflow.ValidatePlanInputs(cfg, *workspace, plan); err != nil {
		return err
	}
	if err := releaseworkflow.ValidateWorkspaceChanges(cfg, *workspace); err != nil {
		return err
	}
	artifactDir := filepath.Dir(*packagePath)
	metadataBytes, err := readRegularFile(filepath.Join(artifactDir, "package-metadata.json"))
	if err != nil {
		return fmt.Errorf("read package metadata sidecar: %w", err)
	}
	var controlFields map[string]string
	if err := json.Unmarshal(metadataBytes, &controlFields); err != nil {
		return err
	}
	buildBytes, err := readRegularFile(filepath.Join(artifactDir, "build-record.json"))
	if err != nil {
		return fmt.Errorf("read TPA package build record: %w", err)
	}
	var record releaseworkflow.PackageRecord
	if err := json.Unmarshal(buildBytes, &record); err != nil {
		return err
	}
	if record.BuildNumber != *rebuild || record.Filename != filepath.Base(*packagePath) || record.Package != cfg.Package.Name || record.Version != plan.Version || record.Architecture != *architecture || !equalStringMaps(record.ControlFields, controlFields) {
		return fmt.Errorf("package build record does not match release identity or metadata")
	}
	digest, size, err := releaseworkflow.Digest(*packagePath)
	if err != nil {
		return err
	}
	if digest != record.SHA256 || size != record.Size {
		return fmt.Errorf("package bytes changed after the TPA build")
	}
	if err := releaseworkflow.VerifyPackage(cfg, plan, *architecture, *packagePath, *tpaVersion, os.Getenv("TPA_RELEASE_TPA_COMMIT"), record.ControlFields); err != nil {
		return err
	}
	spec := cfg.Package.Architectures[*architecture]
	install := "not-runtime-qualified"
	if spec.Runtime == "native" || spec.Runtime == "emulated" {
		install = "passed"
	}
	upgrade := "not-configured"
	if spec.Runtime == "build-only" {
		upgrade = "not-applicable"
	}
	if releaseworkflow.CommandConfigured(cfg.Package.Qualification.UpgradeCommand) {
		upgrade = "passed"
	}
	packageQualification := "not-configured"
	if releaseworkflow.CommandConfigured(cfg.Package.Qualification.Command) {
		packageQualification = "passed"
	}
	record.RuntimeQualification = spec.Runtime
	record.PackageQualification = packageQualification
	record.InstallQualification = install
	record.UpgradeQualification = upgrade
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	packageDir, err := filepath.Abs(artifactDir)
	if err != nil {
		return err
	}
	outputDir, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if packageDir != outputDir {
		return fmt.Errorf("qualification record output must match the package artifact directory")
	}
	outputInfo, err := os.Lstat(outputDir)
	if err != nil || !outputInfo.IsDir() || outputInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("qualification output must be a real package artifact directory")
	}
	qualificationFile, err := os.OpenFile(filepath.Join(outputDir, "qualification.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := qualificationFile.Write(append(encoded, '\n')); err != nil {
		_ = qualificationFile.Close()
		return err
	}
	if err := qualificationFile.Close(); err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func bundleCommand(args []string) error {
	flags := flag.NewFlagSet("bundle", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	artifacts := flags.String("artifacts", "", "downloaded per-build artifact root")
	output := flags.String("output", "", "new output bundle directory")
	tpaCommit := flags.String("tpa-commit", "", "pinned TPA builder source commit")
	tpaVersion := flags.String("tpa-version", "", "pinned TPA builder version")
	goVersion := flags.String("go-version", "", "Go toolchain version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	manifest, err := releaseworkflow.CreateBundle(cfg, releaseworkflow.BundleOptions{
		Workspace: *workspace, ArtifactsRoot: *artifacts, OutputDir: *output, Plan: plan,
		TPACommit: *tpaCommit, TPAVersion: *tpaVersion, GoVersion: *goVersion,
		WorkflowRef: os.Getenv("GITHUB_WORKFLOW_REF"), WorkflowSHA: os.Getenv("GITHUB_WORKFLOW_SHA"),
		RunID: os.Getenv("GITHUB_RUN_ID"), RunAttempt: os.Getenv("GITHUB_RUN_ATTEMPT"), SourceRepository: os.Getenv("GITHUB_REPOSITORY"),
	})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func publishCommand(args []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	bundle := flags.String("bundle", "", "qualified release bundle")
	repository := flags.String("repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub owner/name")
	tpaVersion := flags.String("tpa-version", "", "pinned TPA builder version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	return releaseworkflow.PublishGitHubRelease(releaseworkflow.PublishOptions{Workspace: *workspace, BundleDir: *bundle,
		Repository: *repository, Config: cfg, Plan: plan, TPAVersion: *tpaVersion})
}

func verifyBundleCommand(args []string) error {
	flags := flag.NewFlagSet("verify-bundle", flag.ContinueOnError)
	configPath := flags.String("config", "", "project release YAML")
	workspace := flags.String("workspace", ".", "project checkout")
	planPath := flags.String("plan", "", "release plan JSON")
	bundle := flags.String("bundle", "", "release bundle directory")
	tpaVersion := flags.String("tpa-version", "", "pinned TPA builder version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, _, err := releaseworkflow.LoadConfig(*configPath, *workspace)
	if err != nil {
		return err
	}
	plan, err := readPlan(*planPath)
	if err != nil {
		return err
	}
	if err := releaseworkflow.ValidatePlanInputs(cfg, *workspace, plan); err != nil {
		return err
	}
	if err := releaseworkflow.ValidateWorkspaceChanges(cfg, *workspace); err != nil {
		return err
	}
	if err := releaseworkflow.VerifyBundle(cfg, plan, *bundle, *tpaVersion, os.Getenv("TPA_RELEASE_TPA_COMMIT")); err != nil {
		return err
	}
	fmt.Println("release bundle verified")
	return nil
}

func readPlan(path string) (releaseworkflow.Plan, error) {
	if path == "" {
		return releaseworkflow.Plan{}, fmt.Errorf("--plan is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseworkflow.Plan{}, err
	}
	var wrapper struct {
		Plan releaseworkflow.Plan `json:"plan"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return releaseworkflow.Plan{}, err
	}
	if wrapper.Plan.SchemaVersion != 1 {
		return releaseworkflow.Plan{}, fmt.Errorf("unsupported release plan schema")
	}
	return wrapper.Plan, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("%s must be a regular file no larger than 1 MiB", path)
	}
	return os.ReadFile(path)
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func appendGitHubOutputs(path string, values map[string]string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	for key, value := range values {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("workflow output %s unexpectedly contains a line break", key)
		}
		if _, err := fmt.Fprintf(file, "%s=%s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}
