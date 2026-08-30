package aptpackage

import (
	"bufio"
	"bytes"
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

type repositoryPackage struct {
	Control       Control
	ControlStanza []byte
	Dist          string
	Size          int64
	SHA256        string
}

// Pack creates and verifies repository metadata and copies packages into the
// pool. Pool population is deliberately independent of signing: unsigned
// repositories are useful for local testing and must still be complete.
func Pack(cfg Config) error {
	if err := buildRepository(cfg); err != nil {
		return err
	}
	return verifyRepository(cfg)
}

// buildRepository materializes a repository at cfg.OutDir. AtomicPack uses it
// directly so it can set publication permissions before final verification.
func buildRepository(cfg Config) error {
	entries, err := os.ReadDir(cfg.InDir)
	if err != nil {
		return fmt.Errorf("read package directory: %w", err)
	}

	pkgs := make([]repositoryPackage, 0)
	identities := make(map[string]repositoryPackage)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".deb") {
			continue
		}
		path := filepath.Join(cfg.InDir, entry.Name())
		rawControl, err := readPackageControl(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		control, err := ParseControl(rawControl)
		if err != nil {
			return fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		stanza, err := repositoryControlStanza(rawControl)
		if err != nil {
			return fmt.Errorf("prepare metadata for %s: %w", entry.Name(), err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package is not a regular file: %s", entry.Name())
		}
		hash, err := getHash(path)
		if err != nil {
			return err
		}
		pkg := repositoryPackage{Control: control, ControlStanza: stanza, Dist: path, Size: info.Size(), SHA256: hash}
		identity := control.Name + "\x00" + control.Version + "\x00" + control.Architecture
		if previous, ok := identities[identity]; ok {
			equal, err := filesEqual(previous.Dist, pkg.Dist)
			if err != nil {
				return fmt.Errorf("compare duplicate package identity %s %s %s: %w", control.Name, control.Version, control.Architecture, err)
			}
			if !equal {
				return fmt.Errorf("conflicting package identity %s %s %s in %s and %s", control.Name, control.Version, control.Architecture, filepath.Base(previous.Dist), entry.Name())
			}
			// Byte-identical retries are idempotent: retain the first filename and
			// emit exactly one Packages entry.
			continue
		}
		identities[identity] = pkg
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no .deb packages found in %s", cfg.InDir)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Control.Architecture != pkgs[j].Control.Architecture {
			return pkgs[i].Control.Architecture < pkgs[j].Control.Architecture
		}
		if pkgs[i].Control.Name != pkgs[j].Control.Name {
			return pkgs[i].Control.Name < pkgs[j].Control.Name
		}
		if pkgs[i].Control.Version != pkgs[j].Control.Version {
			return pkgs[i].Control.Version < pkgs[j].Control.Version
		}
		return filepath.Base(pkgs[i].Dist) < filepath.Base(pkgs[j].Dist)
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

	byArch := make(map[string][]repositoryPackage)
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
		if err := copyFile(pkg.Dist, dest); err != nil {
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
			relPath := filepath.ToSlash(filepath.Join("pool", component, string(pkg.Control.Name[0]), pkg.Control.Name, filepath.Base(pkg.Dist)))
			if _, err = packages.Write(pkg.ControlStanza); err == nil {
				_, err = fmt.Fprintf(packages, "Filename: %s\nSize: %d\nSHA256: %s\n\n", relPath, pkg.Size, pkg.SHA256)
			}
			if err != nil {
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
	fingerprint, err := signingFingerprint(cfg.GPG)
	if err != nil {
		return err
	}
	inRelease := filepath.Join(distDir, "InRelease")
	cmd := exec.Command("gpg", "--batch", "--yes", "--clearsign", "-u", fingerprint, "-o", inRelease, releasePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sign Release: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// repositoryControlStanza preserves the control metadata emitted by dpkg-deb
// while removing fields whose values must be derived from the published file.
func repositoryControlStanza(raw []byte) ([]byte, error) {
	controlled := map[string]bool{"filename": true, "size": true, "sha256": true}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var out bytes.Buffer
	skip := false
	seenField := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if !seenField {
				return nil, fmt.Errorf("control continuation without a field")
			}
			if !skip {
				out.WriteString(line)
				out.WriteByte('\n')
			}
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("invalid control field %q", line)
		}
		seenField = true
		skip = controlled[strings.ToLower(line[:colon])]
		if !skip {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read control stanza: %w", err)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("empty control stanza")
	}
	return out.Bytes(), nil
}

func gzipFile(path string) error {
	cmd := exec.Command("gzip", "-fk", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compress %s: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyFile(src, dst string) error {
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

func filesEqual(first, second string) (bool, error) {
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false, err
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false, err
	}
	if firstInfo.Size() != secondInfo.Size() {
		return false, nil
	}
	firstFile, err := os.Open(first)
	if err != nil {
		return false, err
	}
	defer firstFile.Close()
	secondFile, err := os.Open(second)
	if err != nil {
		return false, err
	}
	defer secondFile.Close()
	firstBuffer := make([]byte, 64*1024)
	secondBuffer := make([]byte, 64*1024)
	for {
		firstN, firstErr := firstFile.Read(firstBuffer)
		secondN, secondErr := secondFile.Read(secondBuffer)
		if firstN != secondN || !bytes.Equal(firstBuffer[:firstN], secondBuffer[:secondN]) {
			return false, nil
		}
		if firstErr == io.EOF && secondErr == io.EOF {
			return true, nil
		}
		if firstErr != nil {
			return false, firstErr
		}
		if secondErr != nil {
			return false, secondErr
		}
	}
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
