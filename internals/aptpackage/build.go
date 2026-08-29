// Package aptpackage serves as a high-performance helper for automating
// the generation and management of Debian package archives and repository structures.
// It abstracts complex packaging, indexing, and filesystem synchronization processes,
// enabling efficient, routing-based processing of repository builds at the filesystem level.
package aptpackage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func Build(c Config) error {
	DEBIANpath := fmt.Sprintf("%s/DEBIAN", c.InDir)
	controlpath := fmt.Sprintf("%s/control", DEBIANpath)
	out, err := os.ReadFile(controlpath)
	if err != nil {
		return fmt.Errorf("error while reading file: %w", err)
	}
	control, err := ParseControl(out)
	if err != nil {
		return fmt.Errorf("parsing file failed: %w", err)
	}
	err = os.MkdirAll(DEBIANpath, 0o755)
	if err != nil {
		return fmt.Errorf("could not create folders: %s %s", c.OutDir, err)
	}
	scripts := []string{"postinst", "preinst", "prerm", "postrm"}
	for _, s := range scripts {
		path := fmt.Sprintf("%s/%s", DEBIANpath, s)
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return fmt.Errorf("inspect maintainer script %s: %w", path, statErr)
		}
		if err = os.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("chmod failed: %s %s", path, err)
		}
	}
	if err = os.MkdirAll(filepath.Dir(c.OutDir), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	packagename := fmt.Sprintf("%s_%s_%s.deb", control.Name, control.Version, control.Architecture)
	cmd := exec.Command(
		"dpkg-deb",
		"--root-owner-group",
		"--build", c.InDir, c.OutDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error building %s: %w", packagename, err)
	}
	return nil
}
