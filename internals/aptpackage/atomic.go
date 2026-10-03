package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicPack builds into a sibling directory, verifies the complete repository,
// and publishes it with a same-filesystem Linux rename exchange. Its sibling lock
// serializes TPA atomic-pack and lifecycle writers for the same live path.
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
	return withRepositoryLock(live, func() error {
		return atomicPackLocked(cfg, live, parent)
	})
}

func atomicPackLocked(cfg Config, live, parent string) error {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create publish parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(live)+".tpa-staging-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("set staging permissions: %w", err)
	}
	defer os.RemoveAll(staging)

	cfg.OutDir = staging
	if err := buildRepository(cfg); err != nil {
		return err
	}
	if err := prepareRepositoryPermissions(staging); err != nil {
		return fmt.Errorf("prepare repository permissions: %w", err)
	}
	if err := verifyRepository(cfg); err != nil {
		return fmt.Errorf("verify staging repository: %w", err)
	}
	if err := publishDirectory(staging, live); err != nil {
		return fmt.Errorf("publish repository: %w", err)
	}
	return nil
}

// Repository data contains no signing secrets and is made read-only/readable
// for serving processes. GPG homes are outside this tree and remain private.
func prepareRepositoryPermissions(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("repository contains symlink %s", path)
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("repository contains non-regular file %s", path)
		}
		return os.Chmod(path, 0o644)
	})
}

func startsWithBinary(name string) bool {
	return len(name) > len("binary-") && name[:len("binary-")] == "binary-"
}
