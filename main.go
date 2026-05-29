package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func main() {
 	buildCmd := flag.NewFlagSet("build", flag.ExitOnError)
	pkgName := buildCmd.String("name", "mema", "Name des Pakets")
	pkgVer := buildCmd.String("version", "0.0.1", "Version des Pakets")
	if len(os.Args) < 2 {
		fmt.Println("Usage: packer <init|build|index>")
		return
	}

	switch os.Args[1] {
	case "init":
		name:="myNewAPTPackage"
		if len(os.Args)>3{
			name= os.Args[2]
		}
		os.MkdirAll(name, 0755)
		fmt.Println("Basis-Struktur bereit.")
	case "build":
  buildCmd.Parse(os.Args[2:])
		runBuild(*pkgName, *pkgVer)
	case "index":
		runIndex()
	case "list":
		fmt.Println("TODO!")
	default:
		fmt.Println("Befehl unbekannt.")
	}
}

func runBuild(name, ver string) {
	fmt.Printf("Baue Paket: %s Version: %s\n", name, ver)
	// Hier wird fpm mit den Variablen gefüttert
	cmd := exec.Command("fpm", "-s", "dir", "-t", "deb", "-n", name, "-v", ver, "usr/=/usr/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func runIndex() {
	outFile, err := os.Create("Packages")
	if err != nil {
		fmt.Printf("Fehler beim Erstellen von Packages: %v\n", err)
		return
	}
	defer outFile.Close()

	cmd := exec.Command("dpkg-scanpackages", ".", "/dev/null")
	cmd.Stdout = outFile // Hier leiten wir den Stream direkt um
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("Fehler beim Indizieren: %v\n", err)
	}
	fmt.Println("Packages Datei erfolgreich geschrieben.")
}
