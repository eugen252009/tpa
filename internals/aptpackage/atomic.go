package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicPack builds into a sibling directory and publishes it with a
// same-filesystem Linux rename exchange. The live repository is untouched if
// building or validation fails.
func AtomicPack(cfg Config, livePath string) error {
	if livePath == "" {
		return fmt.Errorf("atomic publish path is empty")
	}
	live, err := filepath.Abs(livePath)
	if err != nil {
		return fmt.Errorf("resolve publish path: %w", err)
	}
	if live == string(filepath.Separator) {
		return fmt.Errorf("refusing to publish over filesystem root")
	}
	parent := filepath.Dir(live)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create publish parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(live)+".tpa-staging-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	_ = os.Chmod(staging, 0o700)
	defer os.RemoveAll(staging)

	cfg.OutDir = staging
	if err := Pack(cfg); err != nil {
		return err
	}
	if err := validateRepository(cfg); err != nil {
		return fmt.Errorf("validate staging repository: %w", err)
	}
	if err := publishDirectory(staging, live); err != nil {
		return fmt.Errorf("publish repository: %w", err)
	}
	return nil
}

func validateRepository(cfg Config) error {
	component := cfg.Repo.Components
	if component == "" {
		component = "main"
	}
	codename := cfg.Repo.Codename
	if codename == "" {
		codename = "stable"
	}
	dist := filepath.Join(cfg.OutDir, "dists", codename)
	for _, path := range []string{filepath.Join(cfg.OutDir, "pool"), filepath.Join(dist, "Release")} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() && path != filepath.Join(dist, "Release") {
			if err != nil {
				return fmt.Errorf("missing %s: %w", path, err)
			}
			return fmt.Errorf("invalid repository path %s", path)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dist, component))
	if err != nil {
		return fmt.Errorf("read component metadata: %w", err)
	}
	found := 0
	for _, entry := range entries {
		if !entry.IsDir() || !startsWithBinary(entry.Name()) {
			continue
		}
		found++
		binary := filepath.Join(dist, component, entry.Name())
		for _, name := range []string{"Packages", "Packages.gz"} {
			if info, err := os.Stat(filepath.Join(binary, name)); err != nil || info.IsDir() {
				return fmt.Errorf("missing %s", filepath.Join(binary, name))
			}
		}
	}
	if found == 0 {
		return fmt.Errorf("no binary package metadata found")
	}
	if cfg.GPG != "" {
		if info, err := os.Stat(filepath.Join(dist, "InRelease")); err != nil || info.IsDir() {
			return fmt.Errorf("signed repository is missing InRelease")
		}
	}
	return nil
}

func startsWithBinary(name string) bool {
	return len(name) > len("binary-") && name[:len("binary-")] == "binary-"
}
