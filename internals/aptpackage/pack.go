package aptpackage

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func (p *PackPackage) Pack() error {
	// 1. Basis-Struktur definieren (Tree-Struktur)
	// Struktur: pool/main/<name_anfang>/<name>/...
	prefix := p.Control.Package[0:1]
	poolDir := filepath.Join(p.OutDir, "pool", "main", prefix, p.Control.Package)
	distsDir := filepath.Join(p.OutDir, "dists", "stable", "main", "binary-"+p.Control.Architecture)

	os.MkdirAll(poolDir, 0o755)
	os.MkdirAll(distsDir, 0o755)

	// 2. Paket in den Pool kopieren
	debName := fmt.Sprintf("%s_%s_%s.deb", p.Control.Package, p.Control.Version, p.Control.Architecture)
	src := filepath.Join(p.OutDir, debName) // Angenommen, das .deb liegt bereits im OutDir
	dst := filepath.Join(poolDir, debName)

	if err := CopyFile(src, dst); err != nil {
		return err
	}

	// 3. Packages-Datei generieren (via dpkg-scanpackages)
	// Wir scannen das 'pool' Verzeichnis relativ zum OutDir
	cmd := exec.Command("dpkg-scanpackages", "pool", "/dev/null")
	cmd.Dir = p.OutDir
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	packageFilePath := filepath.Join(distsDir, "Packages")
	if err := os.WriteFile(packageFilePath, output, 0o644); err != nil {
		return err
	}

	// 4. Release-Datei erstellen und signieren
	// Hier wird eine einfache Release-Datei erstellt
	releaseContent := fmt.Sprintf("Origin: MyRepo\nSuite: stable\nCodename: stable\nArchitectures: %s\nComponents: main\n", p.Control.Architecture)
	releasePath := filepath.Join(p.OutDir, "dists", "stable", "Release")
	os.WriteFile(releasePath, []byte(releaseContent), 0o644)

	// 5. GPG Signierung
	if p.GPG != "" {
		fmt.Printf("Signiere Repository mit GPG Key: %s\n", p.GPG)
		// Signiert die Release-Datei zu InRelease (Standard für apt Repos)
		signCmd := exec.Command("gpg", "--clearsign", "-o", filepath.Join(p.OutDir, "dists", "stable", "InRelease"), releasePath)
		if err := signCmd.Run(); err != nil {
			return fmt.Errorf("GPG Signierung fehlgeschlagen: %w", err)
		}
	}

	return nil
}

func CopyFile(src, dst string) error {
	// Quelldatei öffnen
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Zieldatei erstellen
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Inhalt kopieren
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Datei-Modus (Berechtigungen) anpassen, damit das .deb ausführbar bleibt/korrekt ist
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
