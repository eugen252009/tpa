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
	"strings"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithTerminal(args, stdin, stdout, stderr, stdinIsTerminal())
}

func runWithTerminal(args []string, stdin io.Reader, stdout, stderr io.Writer, interactive bool) int {
	defer writeStageMetrics()
	cfg := aptpackage.Config{}
	flags := flag.NewFlagSet("tpa", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.Control.Name, "name", "myNewAPTPackage", "Name of the package")
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
	flags.StringVar(&cfg.InDir, "in", ".", "Your input directory")
	packageName := flags.String("package", "", "Package name for unlist/delete or exact inspect lookup")
	flags.StringVar(&cfg.OutDir, "out", ".", "Output directory for the .deb file")
	flags.StringVar(&cfg.GPG, "gpg", "", "GPG Key ID or full fingerprint for signing, empty for no signing")
	workers := flags.Int("workers", 0, "Bounded package workers for pack (0 uses GOMAXPROCS)")
	output := flags.String("output", "", "Repository output directory (alias for -out)")
	atomicPublish := flags.String("atomic-publish", "", "Atomically publish the repository at this path")
	generationManifest := flags.String("generation-manifest", "", "Write a portable inventory for the verified repository generation")
	repositoryID := flags.String("repository-id", "", "Repository identity for -generation-manifest")
	generationID := flags.String("generation-id", "", "Generation identity for -generation-manifest")
	parentGeneration := flags.String("parent-generation", "", "Expected parent generation for -generation-manifest")
	noProvenance := flags.Bool("no-provenance", false, "Disable automatic TPA provenance metadata")
	yes := flags.Bool("yes", false, "Confirm destructive package deletion")
	jsonOutput := flags.Bool("json", false, "Write inspect/verify output as JSON")
	keyringPath := flags.String("keyring", "", "Public OpenPGP key export for repository signature verification")
	fingerprint := flags.String("fingerprint", "", "Expected full OpenPGP signer fingerprint")
	requireSigned := flags.Bool("require-signed", false, "Require a valid signed InRelease")

	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: tpa <init|build|parse|pack|inspect|verify|unlist|delete|json|schema|version>")
		flags.PrintDefaults()
		return 2
	}
	command := args[0]
	flagArgs := args[1:]
	// The standard flag package stops at the first positional argument. Move
	// pack's optional config path behind flags so `pack config.json --output`
	// remains compatible with the documented form.
	if command == "pack" && len(flagArgs) > 0 && !strings.HasPrefix(flagArgs[0], "-") {
		flagArgs = append(append([]string{}, flagArgs[1:]...), flagArgs[0])
	}
	if command == "inspect" || command == "verify" {
		flagArgs = moveRepositoryLocator(flagArgs)
	}
	if err := flags.Parse(flagArgs); err != nil {
		return 2
	}
	if command == "pack" {
		if *output != "" && *atomicPublish != "" {
			fmt.Fprintln(stderr, "tpa: --output and --atomic-publish are mutually exclusive")
			return 2
		}
		positional := flags.Args()
		if len(positional) > 1 {
			fmt.Fprintln(stderr, "tpa: pack accepts at most one JSON config path")
			return 2
		}
		if len(positional) == 1 {
			data, err := os.ReadFile(positional[0])
			if err != nil {
				fmt.Fprintf(stderr, "tpa: read config: %v\n", err)
				return 2
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				fmt.Fprintf(stderr, "tpa: parse config: %v\n", err)
				return 2
			}
		}
		if *output != "" {
			cfg.OutDir = *output
		}
		if *atomicPublish != "" {
			cfg.OutDir = *atomicPublish
		}
		if *workers < 0 || *workers > aptpackage.MaxPackWorkers {
			fmt.Fprintf(stderr, "tpa: -workers must be between 0 and %d\n", aptpackage.MaxPackWorkers)
			return 2
		}
		cfg.Workers = *workers
	}
	if *noProvenance {
		disabled := false
		cfg.Provenance = &disabled
	}

	seenFlags := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { seenFlags[f.Name] = true })
	if seenFlags["yes"] && command != "delete" {
		fmt.Fprintln(stderr, "tpa: --yes is only valid with delete")
		return 2
	}
	if command != "inspect" && command != "verify" {
		for _, name := range []string{"json", "keyring", "fingerprint", "require-signed"} {
			if seenFlags[name] {
				fmt.Fprintf(stderr, "tpa: -%s is only valid with inspect or verify\n", name)
				return 2
			}
		}
	}

	switch command {
	case "init":
		fmt.Fprintf(stdout, "Initializing package '%s' in: %s\n", cfg.Control.Name, cfg.OutDir)
		if err := aptpackage.InitPackage(cfg); err != nil {
			fmt.Fprintf(stderr, "initialize package: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Package structure for '%s' created.\n", cfg.InDir+"/"+cfg.Control.Name)
	case "build":
		if err := aptpackage.Build(cfg); err != nil {
			fmt.Fprintf(stderr, "build package: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Package successfully created!")
	case "parse":
		pkg, err := aptpackage.ParsePackage(cfg.InDir)
		if err != nil {
			fmt.Fprintf(stderr, "parse package: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, pkg)
	case "pack":
		var err error
		if *atomicPublish != "" {
			err = aptpackage.AtomicPack(cfg, *atomicPublish)
		} else {
			err = aptpackage.Pack(cfg)
		}
		if err != nil {
			fmt.Fprintf(stderr, "Build failed: %v\n", err)
			return 1
		}
		if *generationManifest != "" {
			manifestPath, pathErr := filepath.Abs(*generationManifest)
			rootPath, rootErr := filepath.Abs(cfg.OutDir)
			if pathErr != nil || rootErr != nil {
				fmt.Fprintln(stderr, "tpa: resolve generation manifest path failed")
				return 1
			}
			rel, relErr := filepath.Rel(rootPath, manifestPath)
			if relErr != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				fmt.Fprintln(stderr, "tpa: generation manifest must be outside the repository tree")
				return 2
			}
			manifest, manifestErr := aptpackage.CreateGenerationManifest(cfg.OutDir, *repositoryID, *generationID, *parentGeneration)
			if manifestErr == nil {
				manifestErr = aptpackage.VerifyGenerationManifest(cfg.OutDir, manifest, *repositoryID)
			}
			if manifestErr == nil {
				manifestErr = aptpackage.WriteGenerationManifest(manifestPath, manifest)
			}
			if manifestErr != nil {
				fmt.Fprintf(stderr, "tpa: create generation manifest: %v\n", manifestErr)
				return 1
			}
		}
		fmt.Fprintln(stdout, "Repo build complete!")
	case "json":
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "read JSON: %v\n", err)
			return 1
		}
		if err = json.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(stderr, "parse JSON: %v\n", err)
			return 2
		}
		if *noProvenance {
			disabled := false
			cfg.Provenance = &disabled
		}
		if err = aptpackage.JSONBuild(cfg); err != nil {
			fmt.Fprintf(stderr, "build JSON package: %v\n", err)
			return 1
		}
	case "inspect", "verify":
		allowed := map[string]bool{"package": true, "ver": true, "arch": true, "codename": true, "workers": true, "json": true, "keyring": true, "fingerprint": true, "require-signed": true}
		for name := range seenFlags {
			if !allowed[name] {
				fmt.Fprintf(stderr, "tpa: -%s is not valid with %s\n", name, command)
				return 2
			}
		}
		positional := flags.Args()
		if len(positional) != 1 {
			fmt.Fprintf(stderr, "Usage: tpa %s [options] <repository-path-or-URL>\n", command)
			return 2
		}
		if command == "inspect" && (seenFlags["package"] || seenFlags["ver"] || seenFlags["arch"]) && !(seenFlags["package"] && seenFlags["ver"] && seenFlags["arch"]) {
			fmt.Fprintln(stderr, "tpa: exact inspect lookup requires -package, -ver, and -arch")
			return 2
		}
		if command == "verify" && (seenFlags["package"] || seenFlags["ver"] || seenFlags["arch"]) {
			fmt.Fprintln(stderr, "tpa: verify checks the entire repository; exact package lookup is available with inspect")
			return 2
		}
		if *workers < 0 || *workers > aptpackage.MaxPackWorkers {
			fmt.Fprintf(stderr, "tpa: -workers must be between 0 and %d\n", aptpackage.MaxPackWorkers)
			return 2
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
				result := aptpackage.RepositoryVerifyResult{Valid: err == nil, RepositoryReport: report}
				if err != nil {
					result.Error = err.Error()
				}
				_ = json.NewEncoder(stdout).Encode(result)
			} else if err != nil {
				_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
			} else {
				_ = json.NewEncoder(stdout).Encode(report)
			}
		} else if err == nil {
			printRepositoryReport(stdout, command, positional[0], report)
		}
		if err != nil && !*jsonOutput {
			fmt.Fprintf(stderr, "tpa: %s repository: %v\n", command, err)
		}
		if err != nil {
			return 1
		}
	case "schema":
		fmt.Fprint(stdout, aptpackage.JSONSCHEMA)
	case "unlist", "delete":
		if len(flags.Args()) != 0 {
			fmt.Fprintf(stderr, "tpa: %s does not accept positional arguments\n", command)
			return 2
		}
		for _, name := range []string{"in", "package", "ver", "arch"} {
			if !seenFlags[name] {
				fmt.Fprintf(stderr, "tpa: %s requires -%s\n", command, name)
				return 2
			}
		}
		identity := aptpackage.PackageIdentity{Package: *packageName, Version: cfg.Control.Version, Architecture: cfg.Control.Architecture}
		if command == "unlist" {
			if err := aptpackage.Unlist(cfg, cfg.InDir, identity); err != nil {
				fmt.Fprintf(stderr, "tpa: unlist package: %v\n", err)
				return 1
			}
			fmt.Fprintf(stdout, "Unlisted package %s; artifact retained.\n", identity)
			return 0
		}
		if !*yes && !interactive {
			fmt.Fprintln(stderr, "tpa: delete requires an interactive terminal or --yes")
			return 1
		}
		target, err := aptpackage.InspectDeleteTarget(cfg, cfg.InDir, identity)
		if err != nil {
			fmt.Fprintf(stderr, "tpa: inspect delete target: %v\n", err)
			return 1
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
			fmt.Fprintf(stderr, "tpa: delete package: %v\n", err)
			return 1
		}
		if result.WasListed {
			fmt.Fprintf(stdout, "Deleted package %s after verified unlisting.\n", identity)
		} else {
			fmt.Fprintf(stdout, "Deleted unlisted package artifact %s.\n", identity)
		}
	case "version":
		fmt.Fprintln(stdout, aptpackage.TPAVersion)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", command)
		return 2
	}
	return 0
}

func moveRepositoryLocator(args []string) []string {
	valueFlags := map[string]bool{"package": true, "ver": true, "arch": true, "codename": true, "workers": true, "keyring": true, "fingerprint": true}
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
			_ = json.NewEncoder(stdout).Encode(aptpackage.RepositoryVerifyResult{Valid: false, Error: err.Error()})
		} else {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
		}
	} else {
		fmt.Fprintf(stderr, "tpa: %s repository: %v\n", command, err)
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
