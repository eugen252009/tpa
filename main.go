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
	outDirPtr := flag.String("out", ".", "Output directory for the .deb file")
	force := flag.Bool("force", false, "Skip validation")
	flat := flag.Bool("flat", false, "build a Flat Repository")

	if len(os.Args) < 2 {
		fmt.Println("Usage: packer <init|build|index|pack>")
		flag.PrintDefaults()
		return
	}
	flag.CommandLine.Parse(os.Args[2:])

	aptpkg := aptpackage.Control{}
	aptpkg.Init(*namePtr, *archPtr, *verPtr, *maintainerPtr, *descriptionPtr)

	switch os.Args[1] {
	case "init":
		remainingArgs := flag.Args()
		name := *namePtr
		targetPath := "." // Standardpfad, falls nichts angegeben ist

		if len(remainingArgs) > 0 {
			// Erstes Argument ist immer der Name
			name = remainingArgs[0]
		}

		if len(remainingArgs) > 1 {
			// Zweites Argument ist der Pfad
			targetPath = remainingArgs[1]
		}

		// Hier kannst du den Pfad nutzen
		fmt.Printf("Initialisiere Paket '%s' in: %s\n", name, targetPath)

		aptpkg.Package = name
		// Falls deine Funktion InitPackage auch einen Pfad entgegennehmen kann:
		// aptpackage.InitPackage(aptpkg, targetPath)

		// Falls du den Pfad manuell setzen musst:
		// os.Chdir(targetPath)
		aptpackage.InitPackage(aptpkg)
	case "build":
		aptpackage.Build(aptpkg)
	case "pack":
		aptpkg := aptpackage.PackPackage{
			Control: aptpkg,
			GPG:     *gpgPtr,
			OutDir:  *outDirPtr,
			Force:   *force,
			Flat:    *flat,
		}
		aptpkg.Pack()
	default:
		fmt.Println("Befehl unbekannt.")
	}
}
