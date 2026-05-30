package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func main() {
	namePtr := flag.String("name", "myNewAPTPackage", "Name of the package")
	verPtr := flag.String("ver", "0.0.1", "Version of the package")
	gpgPtr := flag.String("gpg", "", "GPG Key ID for signing, None for no gpg signing")
	archPtr := flag.String("arch", "all", "Architecture of the package (e.g., all, amd64)")
	maintainerPtr := flag.String("maintainer", "No Maintainer", "Maintainer contact information")
	descriptionPtr := flag.String("desc", "No Description", "Short description of the package")
	inDirPtr := flag.String("in", ".", "your input")
	outDirPtr := flag.String("out", ".", "Output directory for the .deb file")
	force := flag.Bool("force", false, "Skip validation")
	flat := flag.Bool("flat", false, "build a Flat Repository")

	if len(os.Args) < 2 {
		fmt.Println("Usage: packer <init|build|pack>")
		flag.PrintDefaults()
		return
	}
	flag.CommandLine.Parse(os.Args[2:])

	aptpkg := aptpackage.BuildContext{}
	aptpkg.InDir = *inDirPtr
	aptpkg.OutDir = *outDirPtr
	aptpkg.Control.Init(*namePtr, *archPtr, *verPtr, *maintainerPtr, *descriptionPtr)

	switch os.Args[1] {
	case "init":
		fmt.Printf("Initialisiere Paket '%s' in: %s\n", aptpkg.Control.Package, aptpkg.OutDir)
		aptpackage.InitPackage(aptpkg)
	case "build":
		aptpackage.Build(aptpkg)
	case "parse":
		pkg, err := aptpackage.ParsePackage(*inDirPtr)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(pkg)
	case "pack":
		aptpkg := aptpackage.PackPackage{
			Context: aptpackage.BuildContext{
				OutDir: *outDirPtr,
				InDir:  *inDirPtr,
			},
			GPG:   *gpgPtr,
			Force: *force,
			Flat:  *flat,
		}
		if err := aptpkg.Pack(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	default:
		fmt.Println("Befehl unbekannt.")
	}
}
