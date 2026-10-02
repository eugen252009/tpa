package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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

	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: tpa <init|build|parse|pack|json|schema|version>")
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
	case "schema":
		fmt.Fprint(stdout, aptpackage.JSONSCHEMA)
	case "version":
		fmt.Fprintln(stdout, aptpackage.TPAVersion)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", command)
		return 2
	}
	return 0
}
