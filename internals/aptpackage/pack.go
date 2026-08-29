// Package aptpackage provides small helpers for building Debian packages and
// flat APT repositories.
package aptpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Repo struct {
	Control Control
	Dist    string
}

// Pack creates repository metadata and copies packages into the pool. Pool
// population is deliberately independent of signing: unsigned repositories
// are useful for local testing and must still be complete repositories.
func Pack(cfg Config) error {
	entries, err := os.ReadDir(cfg.InDir)
	if err != nil {
		return fmt.Errorf("read package directory: %w", err)
	}

	pkgs := make([]Repo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".deb") {
			continue
		}
		path := filepath.Join(cfg.InDir, entry.Name())
		control, err := ParsePackage(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		pkgs = append(pkgs, Repo{Control: control, Dist: path})
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no .deb packages found in %s", cfg.InDir)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Control.Architecture != pkgs[j].Control.Architecture {
			return pkgs[i].Control.Architecture < pkgs[j].Control.Architecture
		}
		return pkgs[i].Control.Name < pkgs[j].Control.Name
	})

	component := cfg.Repo.Components
	if component == "" {
		component = "main"
	}
	codename := cfg.Repo.Codename
	if codename == "" {
		codename = "stable"
	}
	distDir := filepath.Join(cfg.OutDir, "dists", codename)
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create distribution directory: %w", err)
	}

	byArch := make(map[string][]Repo)
	for _, pkg := range pkgs {
		byArch[pkg.Control.Architecture] = append(byArch[pkg.Control.Architecture], pkg)
	}
	architectures := make([]string, 0, len(byArch))
	for arch := range byArch {
		architectures = append(architectures, arch)
	}
	sort.Strings(architectures)

	// Copy first, so a successful return always leaves a usable pool.
	for _, pkg := range pkgs {
		poolDir := filepath.Join(cfg.OutDir, "pool", component,
			string(pkg.Control.Name[0]), pkg.Control.Name)
		if err := os.MkdirAll(poolDir, 0o755); err != nil {
			return fmt.Errorf("create pool directory: %w", err)
		}
		dest := filepath.Join(poolDir, filepath.Base(pkg.Dist))
		if err := CopyFile(pkg.Dist, dest); err != nil {
			return fmt.Errorf("copy %s: %w", pkg.Dist, err)
		}
	}

	releasePath := filepath.Join(distDir, "Release")
	release, err := os.Create(releasePath)
	if err != nil {
		return fmt.Errorf("create Release: %w", err)
	}
	closeRelease := func() error { return release.Close() }
	origin, label, suite := cfg.Repo.Origin, cfg.Repo.Label, cfg.Repo.Suite
	if origin == "" {
		origin = "TPA-Repo"
	}
	if label == "" {
		label = origin
	}
	if suite == "" {
		suite = codename
	}
	description := cfg.Repo.Description
	if description == "" {
		description = "TPA package repository"
	}
	_, err = fmt.Fprintf(release, "Origin: %s\nLabel: %s\nSuite: %s\nArchitectures: %s\nComponents: %s\nCodename: %s\nDate: %s\nDescription: %s\nSHA256:\n",
		origin, label, suite, strings.Join(architectures, " "), component, codename,
		time.Now().UTC().Format(time.RFC1123Z), description)
	if err != nil {
		_ = closeRelease()
		return fmt.Errorf("write Release: %w", err)
	}

	for _, arch := range architectures {
		binaryDir := filepath.Join(distDir, component, "binary-"+arch)
		if err := os.MkdirAll(binaryDir, 0o755); err != nil {
			_ = closeRelease()
			return fmt.Errorf("create metadata directory: %w", err)
		}
		packagesPath := filepath.Join(binaryDir, "Packages")
		packages, err := os.Create(packagesPath)
		if err != nil {
			_ = closeRelease()
			return fmt.Errorf("create Packages: %w", err)
		}
		for _, pkg := range byArch[arch] {
			info, err := os.Stat(pkg.Dist)
			if err != nil {
				_ = packages.Close()
				_ = closeRelease()
				return fmt.Errorf("stat package: %w", err)
			}
			relPath := filepath.ToSlash(filepath.Join("pool", component, string(pkg.Control.Name[0]), pkg.Control.Name, filepath.Base(pkg.Dist)))
			hash, err := getHash(pkg.Dist)
			if err != nil {
				_ = packages.Close()
				_ = closeRelease()
				return err
			}
			if _, err = fmt.Fprintf(packages, "Package: %s\nVersion: %s\nArchitecture: %s\nFilename: %s\nSize: %d\nSHA256: %s\n\n", pkg.Control.Name, pkg.Control.Version, pkg.Control.Architecture, relPath, info.Size(), hash); err != nil {
				_ = packages.Close()
				_ = closeRelease()
				return fmt.Errorf("write Packages: %w", err)
			}
		}
		if err := packages.Close(); err != nil {
			_ = closeRelease()
			return fmt.Errorf("close Packages: %w", err)
		}
		if err := gzipFile(packagesPath); err != nil {
			_ = closeRelease()
			return err
		}
		for _, path := range []string{packagesPath, packagesPath + ".gz"} {
			hash, err := getHash(path)
			if err != nil {
				_ = closeRelease()
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				_ = closeRelease()
				return err
			}
			rel := filepath.ToSlash(filepath.Join(component, "binary-"+arch, filepath.Base(path)))
			if _, err = fmt.Fprintf(release, " %s %d %s\n", hash, info.Size(), rel); err != nil {
				_ = closeRelease()
				return fmt.Errorf("write Release hash: %w", err)
			}
		}
	}
	if err := closeRelease(); err != nil {
		return fmt.Errorf("close Release: %w", err)
	}
	if cfg.GPG == "" {
		return nil
	}
	inRelease := filepath.Join(distDir, "InRelease")
	cmd := exec.Command("gpg", "--batch", "--yes", "--clearsign", "-u", cfg.GPG, "-o", inRelease, releasePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sign Release: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gzipFile(path string) error {
	cmd := exec.Command("gzip", "-fk", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compress %s: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func CopyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	dest, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dest, source); err != nil {
		_ = dest.Close()
		return err
	}
	return dest.Close()
}

func getHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
