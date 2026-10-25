package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/eugen252009/tpa/internal/version"
	"github.com/eugen252009/tpa/internals/aptpackage"
)

type commandDescription struct{ Name, Description string }

type verifyJSONDocument struct {
	Valid        bool   `json:"valid"`
	SupportEmail string `json:"supportEmail,omitempty"`
	aptpackage.RepositoryReport
	Error string `json:"error,omitempty"`
}

var publicCommands = []commandDescription{
	{"init", "Initialize a package tree"},
	{"build", "Build a Debian package"},
	{"check", "Check package-tree metadata without modification"},
	{"compose", "Build a new repository directly from metadata-only JSON definitions"},
	{"parse", "Read package metadata"},
	{"pack", "Build an APT repository"},
	{"publish", "Publish an already-signed repository to an explicit TPA.run SSH destination"},
	{"stage", "Create, upload, inspect or cancel a private repository-scoped CAS stage"},
	{"sign", "Finalize a staged release with owner-side signing and atomic SSH-CAS publication"},
	{"inspect", "Inspect an existing repository"},
	{"capabilities", "Print machine-readable TPA capabilities"},
	{"completion", "Print shell completion for bash, zsh, or fish"},
	{"verify", "Verify an existing repository"},
	{"unlist", "Remove a package from repository metadata"},
	{"delete", "Unlist and permanently remove a package artifact"},
	{"json", "Build from JSON configuration"},
	{"schema", "Print the configuration schema"},
	{"version", "Print the TPA version"},
	{"register", "Register with a TPA.run account server (pinned customer SSH)"},
	{"login", "Log in to a TPA.run account server (pinned customer SSH)"},
}

func writeIdentityHeader(output io.Writer) {
	fmt.Fprintf(output, "%s %s - %s\n%s\nSupport / bug reports: %s\n", version.ProductName, version.Version, version.ProductDescription, version.ProjectURL, version.SupportEmail)
}

func writeGeneralHelp(output io.Writer) {
	writeIdentityHeader(output)
	fmt.Fprintln(output, "\nUsage:\n  tpa <command> [options]\n\nCommands:")
	for _, item := range publicCommands {
		fmt.Fprintf(output, "  %-8s %s\n", item.Name, item.Description)
	}
	fmt.Fprintln(output, "\nOptions:\n  -h, --help     Show this help\n  -version, --version  Show the TPA version\n\nSee the tpa(1) manual for command options.")
}

func isPublicCommand(name string) bool {
	for _, item := range publicCommands {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func usageError(output io.Writer, message string) int {
	fmt.Fprintf(output, "error: %s\nTPA version: %s\nRun 'tpa --help' for usage.\nSupport / bug reports: %s\n", message, version.Version, version.SupportEmail)
	return 2
}

func commandFailure(output io.Writer, message string) int {
	if !strings.Contains(message, "https://") {
		message += "\nSee: https://github.com/eugen252009/tpa#readme"
	}
	fmt.Fprintf(output, "error: %s\nTPA version: %s\nSupport / bug reports: %s\n", message, version.Version, version.SupportEmail)
	return 1
}

func parseRepositoryArchitectures(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("-architectures must be a comma-separated non-empty list")
	}
	parts := strings.Split(value, ",")
	architectures := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		architecture := strings.TrimSpace(part)
		if architecture == "" {
			return nil, fmt.Errorf("-architectures contains an empty value")
		}
		if seen[architecture] {
			return nil, fmt.Errorf("-architectures repeats %q", architecture)
		}
		seen[architecture] = true
		architectures = append(architectures, architecture)
	}
	return architectures, nil
}

func buildAndPublishRepository(cfg aptpackage.Config, destination aptpackage.RepositoryDestination, empty bool, architectures []string, atomicPath, sshConfigPath string, progress io.Writer) (string, bool, func() error, error) {
	if atomicPath != "" {
		if sshConfigPath != "" {
			return "", false, nil, fmt.Errorf("--ssh-config is only valid for SSH/SFTP output")
		}
		atomicDestination, err := aptpackage.ParseRepositoryDestination(atomicPath)
		if err != nil {
			return "", false, nil, fmt.Errorf("parse atomic publication path: %w", err)
		}
		if atomicDestination.Kind != aptpackage.DestinationLocal {
			return "", false, nil, fmt.Errorf("--atomic-publish supports local filesystem paths only; SSH replacement is unsupported")
		}
		if empty {
			return "", false, nil, fmt.Errorf("--atomic-publish cannot be used with an empty repository")
		}
		cfg.OutDir = atomicDestination.LocalPath
		if err := aptpackage.AtomicPack(cfg, cfg.OutDir); err != nil {
			return "", false, nil, err
		}
		return cfg.OutDir, false, nil, nil
	}
	if destination.Kind == aptpackage.DestinationLocal {
		if sshConfigPath != "" {
			return "", false, nil, fmt.Errorf("--ssh-config is only valid for SSH/SFTP output")
		}
		cfg.OutDir = destination.LocalPath
		if empty {
			if err := aptpackage.InitializeRepository(cfg, architectures); err != nil {
				return "", false, nil, err
			}
		} else if err := aptpackage.Pack(cfg); err != nil {
			return "", false, nil, err
		}
		return cfg.OutDir, false, nil, nil
	}
	if progress != nil {
		fmt.Fprintf(progress, "Building local repository candidate for %s.\n", cfg.OutDir)
	}
	candidateParent, err := os.MkdirTemp("", ".tpa-ssh-candidate-*")
	if err != nil {
		return "", false, nil, fmt.Errorf("create local SSH publication candidate: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(candidateParent) }
	cfg.OutDir = filepath.Join(candidateParent, "repository")
	if empty {
		err = aptpackage.InitializeRepository(cfg, architectures)
	} else {
		err = aptpackage.Pack(cfg)
	}
	if err != nil {
		_ = cleanup()
		return "", true, nil, err
	}
	destination.SSHConfigPath = sshConfigPath
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	phaseProgress := func(phase aptpackage.RemotePublishPhase) {
		if progress != nil {
			fmt.Fprintf(progress, "SSH publication: %s\n", phase)
		}
	}
	if err := aptpackage.PublishRepositoryToSSHWithProgress(ctx, cfg, destination, phaseProgress); err != nil {
		_ = cleanup()
		return "", true, nil, err
	}
	return cfg.OutDir, true, cleanup, nil
}

func unknownCommand(output io.Writer, command string) int {
	writeIdentityHeader(output)
	fmt.Fprintf(output, "\nerror: unknown command %q\n\nUsage:\n  tpa <command> [options]\n\nRun 'tpa --help' for more information.\n", command)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithTerminal(args, stdin, stdout, stderr, stdinIsTerminal())
}

func runWithTerminal(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool) int {
	defer writeStageMetrics()
	if len(args) == 0 {
		writeGeneralHelp(stdout)
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" || hasHelpFlag(args[1:]) {
		writeGeneralHelp(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "-version" {
		if len(args) != 1 {
			return usageError(stderr, "version option does not accept arguments")
		}
		fmt.Fprintf(stdout, "tpa %s\n", version.Version)
		return 0
	}
	command := args[0]
	if !isPublicCommand(command) {
		return unknownCommand(stderr, command)
	}
	if command == "stage" || command == "sign" {
		return runHostedStage(args, stdout, stderr)
	}
	if command == "completion" {
		if len(args) != 2 || (args[1] != "bash" && args[1] != "zsh" && args[1] != "fish") {
			return usageError(stderr, "usage: tpa completion bash|zsh|fish")
		}
		if err := writeCompletion(stdout, args[1]); err != nil {
			return commandFailure(stderr, fmt.Sprintf("write completion: %v", err))
		}
		return 0
	}
	if command == "register" || command == "login" {
		return runCustomerAccount(args, stdin, stdout, stderr)
	}
	if command == "capabilities" {
		if len(args) != 1 {
			return usageError(stderr, "capabilities does not accept arguments")
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(aptpackage.GetCapabilities()); err != nil {
			return commandFailure(stderr, fmt.Sprintf("write capabilities: %v", err))
		}
		return 0
	}
	cfg := aptpackage.Config{}
	flags := flag.NewFlagSet("tpa", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	flags.StringVar(&cfg.Control.Name, "name", "my-new-apt-package", "Debian package name (lowercase letters, digits, +, -, .)")
	flags.StringVar(&cfg.Control.Version, "ver", "0.0.1", "Version of the package")
	flags.StringVar(&cfg.Control.Maintainer, "maintainer", "No Maintainer", "Maintainer contact info")
	flags.StringVar(&cfg.Control.Description, "desc", "No Description", "Short description")
	flags.StringVar(&cfg.Control.Architecture, "arch", "all", "Architecture (e.g. all, amd64)")
	flags.StringVar(&cfg.Control.Depends, "depends", "", "Package dependencies")
	flags.StringVar(&cfg.Control.Homepage, "homepage", "", "Homepage URL")
	flags.StringVar(&cfg.Control.Section, "section", "utils", "Package section")
	flags.StringVar(&cfg.Control.Priority, "priority", "optional", "Package priority")
	flags.StringVar(&cfg.Control.PreDepends, "pre-depends", "", "Pre-dependencies")
	flags.StringVar(&cfg.Control.Recommends, "recommends", "", "Recommended packages")
	flags.StringVar(&cfg.Control.Suggests, "suggests", "", "Suggested packages")
	flags.StringVar(&cfg.Control.Breaks, "breaks", "", "Packages this breaks")
	flags.StringVar(&cfg.Control.Conflicts, "conflicts", "", "Conflicting packages")
	flags.StringVar(&cfg.Control.Replaces, "replaces", "", "Replaced packages")
	flags.StringVar(&cfg.Control.Provides, "provides", "", "Provided features")
	flags.StringVar(&cfg.Control.BuiltUsing, "built-using", "", "Built-using info")
	flags.StringVar(&cfg.Control.Essential, "essential", "no", "Essential package (yes/no)")
	flags.StringVar(&cfg.Control.MultiArch, "multi-arch", "no", "Multi-Arch support")
	flags.StringVar(&cfg.Scripts.PreInst, "preinst", "", "Path or content for preinst")
	flags.StringVar(&cfg.Scripts.PostInst, "postinst", "", "Path or content for postinst")
	flags.StringVar(&cfg.Scripts.PreRm, "prerm", "", "Path or content for prerm")
	flags.StringVar(&cfg.Scripts.PostRm, "postrm", "", "Path or content for postrm")
	flags.StringVar(&cfg.Repo.Origin, "origin", "TPA-Repo", "Repository Origin")
	flags.StringVar(&cfg.Repo.Label, "label", "TPA-Repo", "Repository Label")
	flags.StringVar(&cfg.Repo.Suite, "suite", "stable", "Repository Suite")
	flags.StringVar(&cfg.Repo.Components, "components", "main", "Components (e.g. main)")
	flags.StringVar(&cfg.Repo.Codename, "codename", "stable", "Distribution Codename")
	flags.StringVar(&cfg.Repo.Description, "repo-description", "", "Repository description")
	architectures := flags.String("architectures", "", "Comma-separated repository architectures for pack --empty")
	emptyRepository := flags.Bool("empty", false, "Create an empty APT repository instead of indexing packages")
	flags.StringVar(&cfg.InDir, "in", ".", "Your input directory")
	packageName := flags.String("package", "", "Package name for unlist/delete or exact inspect lookup")
	flags.StringVar(&cfg.OutDir, "out", ".", "Output directory for the .deb file")
	flags.StringVar(&cfg.GPG, "gpg", "", "GPG Key ID or full fingerprint for signing, empty for no signing")
	workers := flags.Int("workers", 0, "Bounded package workers for pack/compose (0 uses GOMAXPROCS)")
	memoryMiB := flags.Int64("memory-mib", 64, "Compose archive-worker scratch memory budget in MiB")
	epoch := flags.Int64("epoch", 0, "Compose reproducible package and Release Unix timestamp")
	output := flags.String("output", "", "Repository output path or SSH destination (alias for -out)")
	atomicPublish := flags.String("atomic-publish", "", "Atomically publish the repository at this path")
	sshConfig := flags.String("ssh-config", "", "OpenSSH client config for SSH/SFTP output")
	generationManifest := flags.String("generation-manifest", "", "Write a portable inventory for the verified repository generation")
	repositoryID := flags.String("repository-id", "", "Repository identity for -generation-manifest")
	generationID := flags.String("generation-id", "", "Generation identity for -generation-manifest")
	parentGeneration := flags.String("parent-generation", "", "Expected parent generation for -generation-manifest")
	noProvenance := flags.Bool("no-provenance", false, "Disable automatic TPA provenance metadata")
	yes := flags.Bool("yes", false, "Confirm destructive package deletion")
	jsonOutput := flags.Bool("json", false, "Write check/inspect/verify/publish output as JSON")
	keyringPath := flags.String("keyring", "", "Public OpenPGP key export for repository signature verification")
	fingerprint := flags.String("fingerprint", "", "Expected full OpenPGP signer fingerprint")
	requireSigned := flags.Bool("require-signed", false, "Require a valid signed InRelease")

	flagArgs := args[1:]
	// The standard flag package stops at the first positional argument. Move
	// pack's optional config path behind flags so `pack config.json --output`
	// remains compatible with the documented form.
	if command == "pack" && len(flagArgs) > 0 && !strings.HasPrefix(flagArgs[0], "-") {
		flagArgs = append(append([]string{}, flagArgs[1:]...), flagArgs[0])
	}
	if command == "inspect" || command == "verify" || command == "check" {
		flagArgs = moveRepositoryLocator(flagArgs)
	}
	if err := flags.Parse(flagArgs); err != nil {
		return usageError(stderr, err.Error())
	}
	seenFlags := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { seenFlags[f.Name] = true })
	if command == "pack" {
		if seenFlags["output"] && seenFlags["out"] {
			return usageError(stderr, "-out and --output cannot be used together")
		}
		if seenFlags["atomic-publish"] && (seenFlags["output"] || seenFlags["out"]) {
			return usageError(stderr, "--atomic-publish cannot be combined with -out or --output")
		}
		positional := flags.Args()
		if len(positional) > 1 {
			return usageError(stderr, "pack accepts at most one input directory or JSON config path")
		}
		if *emptyRepository {
			if len(positional) != 0 || seenFlags["in"] {
				return usageError(stderr, "pack --empty does not accept package input")
			}
			if seenFlags["atomic-publish"] {
				return usageError(stderr, "pack --empty cannot replace an existing repository")
			}
		} else {
			if seenFlags["architectures"] {
				return usageError(stderr, "-architectures is only valid with pack --empty")
			}
			if len(positional) == 1 {
				if seenFlags["in"] {
					return usageError(stderr, "pack input directory cannot be specified both positionally and with -in")
				}
				info, statErr := os.Stat(positional[0])
				if statErr != nil {
					return usageError(stderr, fmt.Sprintf("read pack input %s: %v", positional[0], statErr))
				}
				if info.IsDir() {
					cfg.InDir = positional[0]
				} else {
					data, readErr := os.ReadFile(positional[0])
					if readErr != nil {
						return usageError(stderr, fmt.Sprintf("read config: %v", readErr))
					}
					if err := json.Unmarshal(data, &cfg); err != nil {
						return usageError(stderr, fmt.Sprintf("parse config: %v", err))
					}
				}
			}
		}
		if seenFlags["in"] {
			cfg.InDir = flags.Lookup("in").Value.String()
		}
		if seenFlags["output"] {
			cfg.OutDir = *output
		} else if seenFlags["out"] {
			cfg.OutDir = flags.Lookup("out").Value.String()
		}
		if seenFlags["atomic-publish"] {
			cfg.OutDir = *atomicPublish
		}
		if *workers < 0 || *workers > aptpackage.MaxPackWorkers {
			return usageError(stderr, fmt.Sprintf("-workers must be between 0 and %d", aptpackage.MaxPackWorkers))
		}
		cfg.Workers = *workers
	}
	if *noProvenance {
		disabled := false
		cfg.Provenance = &disabled
	}

	if seenFlags["ssh-config"] && command != "pack" && command != "publish" {
		return usageError(stderr, "--ssh-config is only valid with pack")
	}
	if seenFlags["empty"] && command != "pack" {
		return usageError(stderr, "--empty is only valid with pack")
	}
	if seenFlags["yes"] && command != "delete" {
		return usageError(stderr, "--yes is only valid with delete")
	}
	if command != "inspect" && command != "verify" && command != "publish" {
		for _, name := range []string{"json", "keyring", "fingerprint", "require-signed"} {
			if seenFlags[name] && !(command == "check" && name == "json") {
				return usageError(stderr, fmt.Sprintf("-%s is only valid with inspect or verify", name))
			}
		}
	}

	if command != "compose" && (seenFlags["memory-mib"] || seenFlags["epoch"]) {
		return usageError(stderr, "-memory-mib and -epoch are only valid with compose")
	}
	switch command {
	case "publish":
		allowed := map[string]bool{"in": true, "out": true, "output": true, "ssh-config": true, "keyring": true, "fingerprint": true, "generation-id": true, "parent-generation": true, "json": true}
		for name := range seenFlags {
			if !allowed[name] {
				return usageError(stderr, fmt.Sprintf("-%s is not supported by publish", name))
			}
		}
		if flags.NArg() != 0 || !seenFlags["in"] || *keyringPath == "" || seenFlags["out"] == seenFlags["output"] {
			return usageError(stderr, "publish requires -in finished-repository, exactly one -out/--output tparun+ssh://host/repository-id and -keyring owner-public.gpg")
		}
		target := cfg.OutDir
		if seenFlags["output"] {
			target = *output
		}
		public, err := readLimitedFile(*keyringPath, aptpackage.MaxRepositoryKeyringSize)
		if err != nil {
			return commandFailure(stderr, err.Error())
		}
		result, err := aptpackage.PublishRepositoryToTPARunSSH(context.Background(), cfg.InDir, target, aptpackage.HostedPublishOptions{SSHConfig: *sshConfig, Keyring: public, Fingerprint: *fingerprint, GenerationID: *generationID, ParentGeneration: *parentGeneration})
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("publish signed repository: %v", err))
		}
		if *jsonOutput {
			json.NewEncoder(stdout).Encode(result)
		} else {
			fmt.Fprintf(stdout, "Generation %s activated; original APT signer %s.\n", result.GenerationID, result.APTSigner)
		}
	case "compose":
		allowed := map[string]bool{"in": true, "out": true, "workers": true, "memory-mib": true, "epoch": true, "gpg": true, "origin": true, "label": true, "suite": true, "codename": true, "components": true, "repo-description": true}
		for flag := range seenFlags {
			if !allowed[flag] {
				return usageError(stderr, fmt.Sprintf("-%s is not supported by compose; package fields/provenance/scripts belong in the input JSON definitions", flag))
			}
		}
		if len(flags.Args()) != 0 {
			return usageError(stderr, "compose requires -in definitions.json, definitions.jsonl or a directory")
		}
		if !seenFlags["out"] || seenFlags["output"] || seenFlags["atomic-publish"] || seenFlags["generation-manifest"] {
			return usageError(stderr, "compose requires -out with a new local destination; replacement/remote/manifest options are not supported")
		}
		if *memoryMiB < 1 || *memoryMiB > 4096 {
			return usageError(stderr, "-memory-mib must be between 1 and 4096")
		}
		if !seenFlags["epoch"] && os.Getenv("SOURCE_DATE_EPOCH") != "" {
			value, err := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64)
			if err != nil {
				return usageError(stderr, "invalid SOURCE_DATE_EPOCH")
			}
			*epoch = value
		}
		if err := aptpackage.Compose(context.Background(), cfg, cfg.InDir, aptpackage.DirectOptions{Workers: *workers, MemoryBytes: *memoryMiB << 20, Epoch: *epoch}); err != nil {
			return commandFailure(stderr, fmt.Sprintf("compose repository: %v", err))
		}
		fmt.Fprintln(stdout, "Direct repository built and independently verified.")
	case "check":
		if len(flags.Args()) > 1 || (len(flags.Args()) == 1 && seenFlags["in"]) {
			return usageError(stderr, "check accepts one package tree, positionally or with -in")
		}
		if len(flags.Args()) == 1 {
			cfg.InDir = flags.Args()[0]
		}
		report, err := aptpackage.CheckPackage(cfg.InDir)
		if *jsonOutput {
			if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
				return commandFailure(stderr, encodeErr.Error())
			}
		} else if err != nil {
			return commandFailure(stderr, err.Error())
		} else {
			fmt.Fprintln(stdout, "Package checks passed (supported metadata checks only; not a full Debian Policy or runtime audit).")
		}
		if err != nil {
			return 1
		}
	case "init":
		fmt.Fprintf(stdout, "Initializing package '%s' in: %s\n", cfg.Control.Name, cfg.OutDir)
		if err := aptpackage.InitPackage(cfg); err != nil {
			return commandFailure(stderr, fmt.Sprintf("initialize package: %v", err))
		}
		fmt.Fprintf(stdout, "Package structure for '%s' created.\n", cfg.InDir+"/"+cfg.Control.Name)
	case "build":
		if err := aptpackage.Build(cfg); err != nil {
			return commandFailure(stderr, fmt.Sprintf("build package: %v", err))
		}
		fmt.Fprintln(stdout, "Package successfully created!")
	case "parse":
		pkg, err := aptpackage.ParsePackage(cfg.InDir)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("parse package: %v", err))
		}
		fmt.Fprintln(stdout, pkg)
	case "pack":
		if seenFlags["empty"] && !*emptyRepository {
			return usageError(stderr, "--empty must be enabled to use this option")
		}
		destination, err := aptpackage.ParseRepositoryDestination(cfg.OutDir)
		if err != nil {
			return usageError(stderr, fmt.Sprintf("invalid repository output: %v", err))
		}
		destination.SSHConfigPath = *sshConfig
		architectureList := []string{"all"}
		if seenFlags["architectures"] {
			architectureList, err = parseRepositoryArchitectures(*architectures)
			if err != nil {
				return usageError(stderr, err.Error())
			}
		}
		repositoryRoot, remote, cleanup, err := buildAndPublishRepository(cfg, destination, *emptyRepository, architectureList, *atomicPublish, *sshConfig, stdout)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("repository build failed: %v", err))
		}
		if *generationManifest != "" {
			manifestPath, pathErr := filepath.Abs(*generationManifest)
			rootPath, rootErr := filepath.Abs(repositoryRoot)
			if pathErr != nil || rootErr != nil {
				if cleanup != nil {
					_ = cleanup()
				}
				return commandFailure(stderr, "resolve generation manifest path failed")
			}
			rel, relErr := filepath.Rel(rootPath, manifestPath)
			if relErr != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				if cleanup != nil {
					_ = cleanup()
				}
				return usageError(stderr, "generation manifest must be outside the repository tree")
			}
			manifest, manifestErr := aptpackage.CreateGenerationManifest(repositoryRoot, *repositoryID, *generationID, *parentGeneration)
			if manifestErr == nil {
				manifestErr = aptpackage.VerifyGenerationManifest(repositoryRoot, manifest, *repositoryID)
			}
			if manifestErr == nil {
				manifestErr = aptpackage.WriteGenerationManifest(manifestPath, manifest)
			}
			if manifestErr != nil {
				if cleanup != nil {
					_ = cleanup()
				}
				if remote {
					return commandFailure(stderr, fmt.Sprintf("repository is published and verified remotely, but generation manifest creation failed: %v", manifestErr))
				}
				return commandFailure(stderr, fmt.Sprintf("create generation manifest: %v", manifestErr))
			}
		}
		if cleanup != nil {
			if err := cleanup(); err != nil {
				return commandFailure(stderr, fmt.Sprintf("repository is published and verified, but local candidate cleanup failed: %v", err))
			}
		}
		if remote {
			if *emptyRepository {
				fmt.Fprintf(stdout, "Empty repository initialized and verified at %s.\n", cfg.OutDir)
			} else {
				fmt.Fprintf(stdout, "Repo build complete and verified at %s.\n", cfg.OutDir)
			}
		} else if *emptyRepository {
			fmt.Fprintf(stdout, "Empty repository initialized at %s.\n", repositoryRoot)
		} else {
			fmt.Fprintln(stdout, "Repo build complete!")
		}
	case "json":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("read JSON: %v", err))
		}
		if err = json.Unmarshal(data, &cfg); err != nil {
			return usageError(stderr, fmt.Sprintf("parse JSON: %v", err))
		}
		if *noProvenance {
			disabled := false
			cfg.Provenance = &disabled
		}
		if err = aptpackage.JSONBuild(cfg); err != nil {
			return commandFailure(stderr, fmt.Sprintf("build JSON package: %v", err))
		}
	case "inspect", "verify":
		allowed := map[string]bool{"package": true, "ver": true, "arch": true, "codename": true, "workers": true, "json": true, "keyring": true, "fingerprint": true, "require-signed": true}
		for name := range seenFlags {
			if !allowed[name] {
				return usageError(stderr, fmt.Sprintf("-%s is not valid with %s", name, command))
			}
		}
		positional := flags.Args()
		if len(positional) != 1 {
			return usageError(stderr, fmt.Sprintf("usage: tpa %s [options] <repository-path-or-URL>", command))
		}
		if command == "inspect" && (seenFlags["package"] || seenFlags["ver"] || seenFlags["arch"]) && !(seenFlags["package"] && seenFlags["ver"] && seenFlags["arch"]) {
			return usageError(stderr, "exact inspect lookup requires -package, -ver, and -arch")
		}
		if command == "verify" && (seenFlags["package"] || seenFlags["ver"] || seenFlags["arch"]) {
			return usageError(stderr, "verify checks the entire repository; exact package lookup is available with inspect")
		}
		if *workers < 0 || *workers > aptpackage.MaxPackWorkers {
			return usageError(stderr, fmt.Sprintf("-workers must be between 0 and %d", aptpackage.MaxPackWorkers))
		}
		locator, err := aptpackage.ParseRepositoryLocator(positional[0])
		if err != nil {
			return repositoryCommandError(command, *jsonOutput, stdout, stderr, err)
		}
		reader, err := aptpackage.OpenRepositoryReader(locator)
		if err != nil {
			return repositoryCommandError(command, *jsonOutput, stdout, stderr, err)
		}
		defer reader.Close()
		var keyring []byte
		if *keyringPath != "" {
			keyring, err = readLimitedFile(*keyringPath, aptpackage.MaxRepositoryKeyringSize)
			if err != nil {
				return repositoryCommandError(command, *jsonOutput, stdout, stderr, fmt.Errorf("read public keyring: %w", err))
			}
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		options := aptpackage.RepositoryOptions{Codename: cfg.Repo.Codename, Keyring: keyring, Fingerprint: *fingerprint, RequireSigned: *requireSigned, Workers: *workers}
		if command == "inspect" && seenFlags["package"] {
			options.Package, options.Version, options.Architecture = *packageName, cfg.Control.Version, cfg.Control.Architecture
		}
		var report aptpackage.RepositoryReport
		if command == "inspect" {
			report, err = aptpackage.InspectRepository(ctx, reader, options)
		} else {
			report, err = aptpackage.VerifyRepository(ctx, reader, options)
		}
		if *jsonOutput {
			if command == "verify" {
				result := verifyJSONDocument{Valid: err == nil, RepositoryReport: report}
				if err != nil {
					result.Error = err.Error()
					result.Diagnostics = aptpackage.ErrorDiagnostics(err)
					result.SupportEmail = version.SupportEmail
				}
				_ = json.NewEncoder(stdout).Encode(result)
			} else if err != nil {
				_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error(), "supportEmail": version.SupportEmail})
			} else {
				_ = json.NewEncoder(stdout).Encode(report)
			}
		} else if err == nil {
			printRepositoryReport(stdout, command, positional[0], report)
		}
		if err != nil && !*jsonOutput {
			messages := make([]string, 0)
			for _, diagnostic := range aptpackage.ErrorDiagnostics(err) {
				messages = append(messages, diagnostic.Error())
			}
			for _, skipped := range report.Skipped {
				messages = append(messages, "Skipped: "+skipped)
			}
			return commandFailure(stderr, fmt.Sprintf("%s repository: %s", command, strings.Join(messages, "\n")))
		}
		if err != nil {
			return 1
		}
	case "schema":
		fmt.Fprint(stdout, aptpackage.JSONSCHEMA)
	case "unlist", "delete":
		if len(flags.Args()) != 0 {
			return usageError(stderr, fmt.Sprintf("%s does not accept positional arguments", command))
		}
		for _, name := range []string{"in", "package", "ver", "arch"} {
			if !seenFlags[name] {
				return usageError(stderr, fmt.Sprintf("%s requires -%s", command, name))
			}
		}
		identity := aptpackage.PackageIdentity{Package: *packageName, Version: cfg.Control.Version, Architecture: cfg.Control.Architecture}
		if command == "unlist" {
			if err := aptpackage.Unlist(cfg, cfg.InDir, identity); err != nil {
				return commandFailure(stderr, fmt.Sprintf("unlist package: %v", err))
			}
			fmt.Fprintf(stdout, "Unlisted package %s; artifact retained.\n", identity)
			return 0
		}
		if !*yes && !interactive {
			return commandFailure(stderr, "delete requires an interactive terminal or --yes")
		}
		target, err := aptpackage.InspectDeleteTarget(cfg, cfg.InDir, identity)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("inspect delete target: %v", err))
		}
		printDeleteSummary(stdout, target)
		if !*yes {
			fmt.Fprint(stdout, "Are you sure you want to permanently delete this package? [y/N] ")
			line, readErr := bufio.NewReader(stdin).ReadString('\n')
			if readErr != nil && !(readErr == io.EOF && line != "") {
				fmt.Fprintln(stdout, "\nDeletion cancelled; no repository files were changed.")
				return 0
			}
			answer := strings.ToLower(strings.TrimSpace(line))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(stdout, "Deletion cancelled; no repository files were changed.")
				return 0
			}
		} else {
			fmt.Fprintln(stdout, "Confirmed by --yes.")
		}
		result, err := aptpackage.Delete(cfg, cfg.InDir, identity)
		if err != nil {
			return commandFailure(stderr, fmt.Sprintf("delete package: %v", err))
		}
		if result.WasListed {
			fmt.Fprintf(stdout, "Deleted package %s after verified unlisting.\n", identity)
		} else {
			fmt.Fprintf(stdout, "Deleted unlisted package artifact %s.\n", identity)
		}
	case "version":
		if len(flags.Args()) != 0 || len(seenFlags) != 0 {
			return usageError(stderr, "version does not accept arguments or flags")
		}
		fmt.Fprintln(stdout, version.Version)
	default:
		return unknownCommand(stderr, command)
	}
	return 0
}

func moveRepositoryLocator(args []string) []string {
	valueFlags := map[string]bool{"in": true, "package": true, "ver": true, "arch": true, "codename": true, "workers": true, "keyring": true, "fingerprint": true}
	var options, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		options = append(options, arg)
		if !strings.Contains(arg, "=") {
			name := strings.TrimLeft(arg, "-")
			if valueFlags[name] && index+1 < len(args) {
				index++
				options = append(options, args[index])
			}
		}
	}
	return append(options, positional...)
}

func readLimitedFile(filename string, maximum int64) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	return data, nil
}

func repositoryCommandError(command string, jsonOutput bool, stdout, stderr io.Writer, err error) int {
	if jsonOutput {
		if command == "verify" {
			_ = json.NewEncoder(stdout).Encode(verifyJSONDocument{Valid: false, Error: err.Error(), SupportEmail: version.SupportEmail, RepositoryReport: aptpackage.RepositoryReport{Diagnostics: aptpackage.ErrorDiagnostics(err), Skipped: []string{"repository verification: input or prerequisite unavailable"}}})
		} else {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error(), "supportEmail": version.SupportEmail})
		}
	} else {
		return commandFailure(stderr, fmt.Sprintf("%s repository: %v", command, err))
	}
	return 1
}

func printRepositoryReport(stdout io.Writer, command, locator string, report aptpackage.RepositoryReport) {
	fmt.Fprintf(stdout, "Repository: %s\n", terminalSafe(locator))
	if report.Origin != "" {
		fmt.Fprintf(stdout, "Origin: %s\n", terminalSafe(report.Origin))
	}
	if report.Label != "" {
		fmt.Fprintf(stdout, "Label: %s\n", terminalSafe(report.Label))
	}
	if report.Suite != "" {
		fmt.Fprintf(stdout, "Suite: %s\n", terminalSafe(report.Suite))
	}
	if report.Codename != "" {
		fmt.Fprintf(stdout, "Codename: %s\n", terminalSafe(report.Codename))
	}
	fmt.Fprintf(stdout, "Architectures: %s\nComponents: %s\n", terminalSafe(strings.Join(report.Architectures, ", ")), terminalSafe(strings.Join(report.Components, ", ")))
	if report.Signed {
		if report.SignatureVerified {
			fmt.Fprintf(stdout, "Signature: verified (%s)\n", terminalSafe(report.Signer))
		} else {
			fmt.Fprintln(stdout, "Signature: present (not verified; provide -keyring to verify)")
		}
	} else {
		fmt.Fprintln(stdout, "Signature: unsigned")
	}
	fmt.Fprintf(stdout, "Packages: %d\n", report.PackageCount)
	if command == "verify" {
		fmt.Fprintln(stdout, "Repository verification: passed")
	}
	if command != "inspect" || len(report.Packages) == 0 {
		return
	}
	fmt.Fprintln(stdout, "\nPackage entries:")
	for _, entry := range report.Packages {
		fmt.Fprintf(stdout, "  %s %s %s  %s  %d bytes  SHA256 %s\n", terminalSafe(entry.Package), terminalSafe(entry.Version), terminalSafe(entry.Architecture), terminalSafe(entry.Filename), entry.Size, terminalSafe(entry.SHA256))
		keys := make([]string, 0, len(entry.Control))
		for key := range entry.Control {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key != "Package" && key != "Version" && key != "Architecture" && key != "Filename" && key != "Size" && key != "SHA256" {
				fmt.Fprintf(stdout, "    %s: %s\n", terminalSafe(key), strings.ReplaceAll(terminalSafe(entry.Control[key]), "\n", "\n      "))
			}
		}
	}
}

func terminalSafe(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, value)
}

func printDeleteSummary(stdout io.Writer, target aptpackage.DeleteTarget) {
	fmt.Fprintln(stdout, "Package:")
	fmt.Fprintf(stdout, "  Package:      %s\n", target.Identity.Package)
	fmt.Fprintf(stdout, "  Version:      %s\n", target.Identity.Version)
	fmt.Fprintf(stdout, "  Architecture: %s\n", target.Identity.Architecture)
	fmt.Fprintln(stdout, "\nThis will:")
	fmt.Fprintln(stdout, "  - remove the package from repository metadata if it is still listed")
	fmt.Fprintln(stdout, "  - regenerate and verify affected metadata before publication when listed")
	fmt.Fprintf(stdout, "  - remove the physical .deb artifact (%s)\n\n", target.Filename)
}
