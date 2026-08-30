package aptpackage

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type releaseChecksum struct {
	SHA256 string
	Size   int64
}

// verifyRepository verifies the complete chain from indexed package artifacts
// through Release and, when configured, the InRelease signature.
func verifyRepository(cfg Config) error {
	component := cfg.Repo.Components
	if component == "" {
		component = "main"
	}
	codename := cfg.Repo.Codename
	if codename == "" {
		codename = "stable"
	}
	repoRoot := cfg.OutDir
	distDir := filepath.Join(repoRoot, "dists", codename)
	releasePath := filepath.Join(distDir, "Release")
	releaseData, err := os.ReadFile(releasePath)
	if err != nil {
		return fmt.Errorf("read Release: %w", err)
	}
	releaseChecksums, err := parseReleaseChecksums(releaseData)
	if err != nil {
		return fmt.Errorf("parse Release: %w", err)
	}

	for rel, expected := range releaseChecksums {
		path, err := safeRepositoryPath(distDir, rel)
		if err != nil {
			return fmt.Errorf("invalid Release path %q: %w", rel, err)
		}
		if err := verifyFile(path, expected.Size, expected.SHA256); err != nil {
			return fmt.Errorf("verify Release entry %s: %w", rel, err)
		}
	}

	componentDir := filepath.Join(distDir, component)
	entries, err := os.ReadDir(componentDir)
	if err != nil {
		return fmt.Errorf("read component metadata: %w", err)
	}
	binaryDirectories := 0
	for _, entry := range entries {
		if !entry.IsDir() || !startsWithBinary(entry.Name()) {
			continue
		}
		binaryDirectories++
		for _, name := range []string{"Packages", "Packages.gz"} {
			rel := filepath.ToSlash(filepath.Join(component, entry.Name(), name))
			if _, ok := releaseChecksums[rel]; !ok {
				return fmt.Errorf("Release is missing checksum for %s", rel)
			}
		}
		packagesPath := filepath.Join(componentDir, entry.Name(), "Packages")
		if err := verifyPackageIndex(repoRoot, packagesPath); err != nil {
			return fmt.Errorf("verify %s: %w", filepath.ToSlash(filepath.Join(component, entry.Name(), "Packages")), err)
		}
	}
	if binaryDirectories == 0 {
		return fmt.Errorf("no binary package metadata found")
	}

	inReleasePath := filepath.Join(distDir, "InRelease")
	if cfg.GPG == "" {
		if _, err := os.Stat(inReleasePath); err == nil {
			return fmt.Errorf("unsigned repository contains stale InRelease")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect InRelease: %w", err)
		}
		return nil
	}
	fingerprint, err := signingFingerprint(cfg.GPG)
	if err != nil {
		return err
	}
	if err := verifyInRelease(inReleasePath, releaseData, fingerprint); err != nil {
		return err
	}
	return nil
}

func parseReleaseChecksums(data []byte) (map[string]releaseChecksum, error) {
	checksums := make(map[string]releaseChecksum)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	inSHA256 := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "SHA256:" {
			inSHA256 = true
			continue
		}
		if !inSHA256 {
			continue
		}
		if len(line) == 0 || line[0] != ' ' {
			inSHA256 = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid SHA256 entry %q", line)
		}
		if !validSHA256(fields[0]) {
			return nil, fmt.Errorf("invalid SHA256 digest %q", fields[0])
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid size %q", fields[1])
		}
		if _, exists := checksums[fields[2]]; exists {
			return nil, fmt.Errorf("duplicate SHA256 entry %s", fields[2])
		}
		checksums[fields[2]] = releaseChecksum{SHA256: fields[0], Size: size}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("Release has no SHA256 entries")
	}
	return checksums, nil
}

func verifyPackageIndex(repoRoot, packagesPath string) error {
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		return err
	}
	paragraphs, err := parseControlParagraphs(data)
	if err != nil {
		return err
	}
	if len(paragraphs) == 0 {
		return fmt.Errorf("Packages has no entries")
	}
	for index, fields := range paragraphs {
		filename := fields["filename"]
		sizeValue := fields["size"]
		hash := fields["sha256"]
		if filename == "" || sizeValue == "" || hash == "" {
			return fmt.Errorf("entry %d is missing Filename, Size, or SHA256", index+1)
		}
		if !strings.HasPrefix(filename, "pool/") {
			return fmt.Errorf("entry %d has non-pool Filename %q", index+1, filename)
		}
		size, err := strconv.ParseInt(sizeValue, 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("entry %d has invalid Size %q", index+1, sizeValue)
		}
		if !validSHA256(hash) {
			return fmt.Errorf("entry %d has invalid SHA256 %q", index+1, hash)
		}
		artifactPath, err := safeRepositoryPath(repoRoot, filename)
		if err != nil {
			return fmt.Errorf("entry %d has invalid Filename: %w", index+1, err)
		}
		if err := verifyFile(artifactPath, size, hash); err != nil {
			return fmt.Errorf("entry %d artifact %s: %w", index+1, filename, err)
		}
	}
	return nil
}

func parseControlParagraphs(data []byte) ([]map[string]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	paragraphs := make([]map[string]string, 0)
	fields := make(map[string]string)
	flush := func() {
		if len(fields) != 0 {
			paragraphs = append(paragraphs, fields)
			fields = make(map[string]string)
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("invalid field %q", line)
		}
		name := strings.ToLower(line[:colon])
		if _, duplicate := fields[name]; duplicate {
			return nil, fmt.Errorf("duplicate field %s", line[:colon])
		}
		fields[name] = strings.TrimSpace(line[colon+1:])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return paragraphs, nil
}

func safeRepositoryPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return "", fmt.Errorf("path must be a non-empty relative slash path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != relative {
		return "", fmt.Errorf("path escapes or is not canonical")
	}
	return filepath.Join(root, clean), nil
}

func verifyFile(path string, expectedSize int64, expectedHash string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if info.Size() != expectedSize {
		return fmt.Errorf("size mismatch: got %d, want %d", info.Size(), expectedSize)
	}
	hash, err := getHash(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(hash, expectedHash) {
		return fmt.Errorf("SHA256 mismatch: got %s, want %s", hash, expectedHash)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func signingFingerprint(selector string) (string, error) {
	cmd := exec.Command("gpg", "--batch", "--with-colons", "--fingerprint", "--list-secret-keys", selector)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve signing key %q: %w: %s", selector, err, strings.TrimSpace(string(output)))
	}
	fingerprints := make([]string, 0, 1)
	wantPrimaryFingerprint := false
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 10 {
			continue
		}
		switch fields[0] {
		case "sec", "sec#", "sec>":
			wantPrimaryFingerprint = true
		case "ssb", "ssb#", "ssb>":
			wantPrimaryFingerprint = false
		case "fpr":
			if wantPrimaryFingerprint {
				fingerprints = append(fingerprints, strings.ToUpper(fields[9]))
				wantPrimaryFingerprint = false
			}
		}
	}
	if len(fingerprints) != 1 || !isFingerprint(fingerprints[0]) {
		return "", fmt.Errorf("signing selector %q must resolve to exactly one full primary fingerprint", selector)
	}
	return fingerprints[0], nil
}

func verifyInRelease(path string, releaseData []byte, expectedFingerprint string) error {
	cmd := exec.Command("gpg", "--batch", "--status-fd=1", "--verify", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify InRelease: %w: %s", err, strings.TrimSpace(string(output)))
	}
	validSigner := false
	for _, line := range strings.Split(string(output), "\n") {
		marker := "[GNUPG:] VALIDSIG "
		position := strings.Index(line, marker)
		if position < 0 {
			continue
		}
		fields := strings.Fields(line[position+len(marker):])
		if len(fields) == 0 {
			continue
		}
		if strings.EqualFold(fields[0], expectedFingerprint) || (isFingerprint(fields[len(fields)-1]) && strings.EqualFold(fields[len(fields)-1], expectedFingerprint)) {
			validSigner = true
		}
	}
	if !validSigner {
		return fmt.Errorf("InRelease was not signed by expected fingerprint %s", expectedFingerprint)
	}
	plain, err := exec.Command("gpg", "--batch", "--decrypt", path).Output()
	if err != nil {
		return fmt.Errorf("read signed InRelease payload: %w", err)
	}
	if !bytes.Equal(plain, releaseData) {
		return fmt.Errorf("InRelease payload does not match Release")
	}
	return nil
}

func isFingerprint(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
