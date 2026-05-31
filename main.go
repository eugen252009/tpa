package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func main() {
	cfg := aptpackage.Config{}
	flag.StringVar(&cfg.Control.Name, "name", "myNewAPTPackage", "Name of the package")
	flag.StringVar(&cfg.Control.Version, "ver", "0.0.1", "Version of the package")
	flag.StringVar(&cfg.Control.Maintainer, "maintainer", "No Maintainer", "Maintainer contact info")
	flag.StringVar(&cfg.Control.Description, "desc", "No Description", "Short description")
	flag.StringVar(&cfg.Control.Architecture, "arch", "all", "Architecture (e.g. all, amd64)")
	flag.StringVar(&cfg.Control.Depends, "depends", "", "Package dependencies")
	flag.StringVar(&cfg.Control.Homepage, "homepage", "", "Homepage URL")
	flag.StringVar(&cfg.Control.Section, "section", "utils", "Package section")
	flag.StringVar(&cfg.Control.Priority, "priority", "optional", "Package priority")
	flag.StringVar(&cfg.Control.PreDepends, "pre-depends", "", "Pre-dependencies")
	flag.StringVar(&cfg.Control.Recommends, "recommends", "", "Recommended packages")
	flag.StringVar(&cfg.Control.Suggests, "suggests", "", "Suggested packages")
	flag.StringVar(&cfg.Control.Breaks, "breaks", "", "Packages this breaks")
	flag.StringVar(&cfg.Control.Conflicts, "conflicts", "", "Conflicting packages")
	flag.StringVar(&cfg.Control.Replaces, "replaces", "", "Replaced packages")
	flag.StringVar(&cfg.Control.Provides, "provides", "", "Provided features")
	flag.StringVar(&cfg.Control.BuiltUsing, "built-using", "", "Built-using info")
	flag.StringVar(&cfg.Control.Essential, "essential", "no", "Essential package (yes/no)")
	flag.StringVar(&cfg.Control.MultiArch, "multi-arch", "no", "Multi-Arch support")
	flag.StringVar(&cfg.Control.PreInstBody, "preinst", "", "Path or content for preinst")
	flag.StringVar(&cfg.Control.PostInstBody, "postinst", "", "Path or content for postinst")
	flag.StringVar(&cfg.Control.PreRmBody, "prerm", "", "Path or content for prerm")
	flag.StringVar(&cfg.Control.PostRmBody, "postrm", "", "Path or content for postrm")
	flag.StringVar(&cfg.Repo.Origin, "origin", "TPA-Repo", "Repository Origin")
	flag.StringVar(&cfg.Repo.Label, "label", "TPA-Repo", "Repository Label")
	flag.StringVar(&cfg.Repo.Suite, "suite", "stable", "Repository Suite")
	flag.StringVar(&cfg.Repo.Architectures, "archs", "amd64", "Space separated architectures")
	flag.StringVar(&cfg.Repo.Components, "components", "main", "Components (e.g. main)")
	flag.StringVar(&cfg.Repo.Codename, "codename", "stable", "Distribution Codename")
	flag.StringVar(&cfg.InDir, "in", ".", "Your input directory")
	flag.StringVar(&cfg.OutDir, "out", ".", "Output directory for the .deb file")
	flag.StringVar(&cfg.GPG, "gpg", "", "GPG Key ID for signing, empty for no gpg signing")

	if len(os.Args) < 2 {
		fmt.Println("Usage: tpa <init|build|parse|pack|json|schema>")
		flag.PrintDefaults()
		return
	}
	flag.CommandLine.Parse(os.Args[2:])

	switch os.Args[1] {
	case "init":
		fmt.Printf("Initializing package '%s' in: %s\n", cfg.Control.Name, cfg.OutDir)
		err := aptpackage.InitPackage(cfg)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Printf("Package structure for '%s' created.\n", cfg.InDir+"/"+cfg.Control.Name)
	case "build":
		err := aptpackage.Build(cfg)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Printf("Package %s.deb successfully created!\n", cfg.Control.Name)
	case "parse":
		pkg, err := aptpackage.ParsePackage(cfg.InDir)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(pkg)
	case "pack":
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Build failed: %s\n", r)
				os.Exit(1)
			}
		}()

		if err := aptpackage.Pack(cfg); err != nil {
			if cleanupErr := os.RemoveAll(cfg.OutDir); cleanupErr != nil {
				panic(fmt.Sprintf("Build error: %v\nFailed to clean up OutDir: %v", err, cleanupErr))
			}
			panic(fmt.Sprintf("Build failed: %v", err))
		}
		fmt.Println("Repo build complete!")
	case "json":
		bytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			panic(err)
		}
		var data aptpackage.Control
		err = json.Unmarshal(bytes, &data)
		if err != nil {
			panic(err)
		}
		aptpackage.JSONBuild(cfg)
	case "schema":
		fmt.Println(aptpackage.JSONSCHEMA)
	default:
		fmt.Println("Unknown command.")
	}
}
