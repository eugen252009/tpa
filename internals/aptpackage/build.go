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
	controlStage := startStage(stageBuildControlParse)
	out, err := os.ReadFile(controlpath)
	if err != nil {
		controlStage()
		return fmt.Errorf("error while reading file: %w", err)
	}
	control, err := ParseControl(out)
	if err != nil {
		controlStage()
		return fmt.Errorf("parsing file failed: %w", err)
	}
	controlStage()
	outputDirStage := startStage(stageBuildOutputDirs)
	err = os.MkdirAll(DEBIANpath, 0o755)
	if err != nil {
		outputDirStage()
		return fmt.Errorf("could not create folders: %s %s", c.OutDir, err)
	}
	outputDirStage()
	scriptStage := startStage(stageBuildScripts)
	scripts := []string{"postinst", "preinst", "prerm", "postrm"}
	for _, s := range scripts {
		path := fmt.Sprintf("%s/%s", DEBIANpath, s)
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			scriptStage()
			return fmt.Errorf("inspect maintainer script %s: %w", path, statErr)
		}
		if err = os.Chmod(path, 0o755); err != nil {
			scriptStage()
			return fmt.Errorf("chmod failed: %s %s", path, err)
		}
	}
	scriptStage()
	outputDirStage = startStage(stageBuildOutputDirs)
	if err = os.MkdirAll(filepath.Dir(c.OutDir), 0o755); err != nil {
		outputDirStage()
		return fmt.Errorf("create output directory: %w", err)
	}
	outputDirStage()
	packagename := fmt.Sprintf("%s_%s_%s.deb", control.Name, control.Version, control.Architecture)
	cmd := exec.Command(
		"dpkg-deb",
		"--root-owner-group",
		"--build", c.InDir, c.OutDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	startStageTimer := startStage(stageDPKGStart)
	if err := cmd.Start(); err != nil {
		startStageTimer()
		return fmt.Errorf("error building %s: %w", packagename, err)
	}
	startStageTimer()
	waitStageTimer := startStage(stageDPKGWait)
	if err := cmd.Wait(); err != nil {
		waitStageTimer()
		return fmt.Errorf("error building %s: %w", packagename, err)
	}
	waitStageTimer()
	return nil
}
