package aptpackage

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type releaseChecksum struct {
	SHA256 string
	Size   int64
}

// verifyRepository verifies the complete chain from indexed package artifacts
// through Release and, when configured, the InRelease signature.
func verifyRepository(cfg Config) error {
	workers, err := packWorkerCount(cfg.Workers)
	if err != nil {
		return err
	}
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
	releaseParseStage := startStage(stageVerifyReleaseParse)
	releaseData, err := os.ReadFile(releasePath)
	if err != nil {
		releaseParseStage()
		return fmt.Errorf("read Release: %w", err)
	}
	releaseChecksums, err := parseReleaseChecksums(releaseData)
	releaseParseStage()
	if err != nil {
		return fmt.Errorf("parse Release: %w", err)
	}

	releaseFilesStage := startStage(stageVerifyReleaseFiles)
	for rel, expected := range releaseChecksums {
		path, err := safeRepositoryPath(distDir, rel)
		if err != nil {
			releaseFilesStage()
			return fmt.Errorf("invalid Release path %q: %w", rel, err)
		}
		if err := verifyFile(path, expected.Size, expected.SHA256); err != nil {
			releaseFilesStage()
			return fmt.Errorf("verify Release entry %s: %w", rel, err)
		}
	}
	releaseFilesStage()

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
		if err := verifyPackageIndex(repoRoot, packagesPath, workers); err != nil {
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
	keyLookupStage := startStage(stageSignKeyLookup)
	fingerprint, err := signingFingerprint(cfg.GPG)
	keyLookupStage()
	if err != nil {
		return err
	}
	verifySignatureStage := startStage(stageVerifySignature)
	if err := verifyInRelease(inReleasePath, releaseData, fingerprint); err != nil {
		verifySignatureStage()
		return err
	}
	verifySignatureStage()
	return nil
}

func parseReleaseChecksums(data []byte) (map[string]releaseChecksum, error) {
	_, checksums, err := parseRepositoryRelease(data)
	return checksums, err
}

func verifyPackageIndex(repoRoot, packagesPath string, workers int) error {
	indexParseStage := startStage(stageVerifyIndexReadParse)
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		indexParseStage()
		return err
	}
	architecture := strings.TrimPrefix(filepath.Base(filepath.Dir(packagesPath)), "binary-")
	entries, err := parseRepositoryPackages(data, architecture)
	indexParseStage()
	if err != nil {
		return err
	}
	if err := verifyPackagesCompression(packagesPath); err != nil {
		return fmt.Errorf("Packages.gz does not match Packages: %w", err)
	}
	artifactStage := startStage(stageVerifyArtifacts)
	err = runPackageJobs(len(entries), workers, func(index int) error {
		entry := entries[index]
		artifactPath, err := safeRepositoryPath(repoRoot, entry.Filename)
		if err != nil {
			return fmt.Errorf("entry %d has invalid Filename: %w", index+1, err)
		}
		if err := verifyFile(artifactPath, entry.Size, entry.SHA256); err != nil {
			return fmt.Errorf("entry %d artifact %s: %w", index+1, entry.Filename, err)
		}
		return nil
	})
	artifactStage()
	return err
}

func verifyPackagesCompression(packagesPath string) error {
	plain, err := os.Open(packagesPath)
	if err != nil {
		return err
	}
	defer plain.Close()
	compressed, err := os.Open(packagesPath + ".gz")
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer reader.Close()
	plainBuffer := make([]byte, 32*1024)
	gzipBuffer := make([]byte, 32*1024)
	for {
		plainN, plainErr := plain.Read(plainBuffer)
		gzipN, gzipErr := reader.Read(gzipBuffer)
		if plainN != gzipN || !bytes.Equal(plainBuffer[:plainN], gzipBuffer[:gzipN]) {
			return fmt.Errorf("decompressed bytes differ")
		}
		if plainErr == io.EOF && gzipErr == io.EOF {
			return nil
		}
		if plainErr != nil && plainErr != io.EOF {
			return plainErr
		}
		if gzipErr != nil && gzipErr != io.EOF {
			return gzipErr
		}
	}
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
	verifyHashStage := startStage(stageVerifyHash)
	hash, err := getHash(path)
	verifyHashStage()
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
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("InRelease is missing or not a regular file")
	}
	if err = requireTerminalInReleaseSignature(path, info.Size()); err != nil {
		return err
	}
	cmd := exec.Command("gpg", "--batch", "--status-fd=1", "--verify", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify InRelease: %w: %s", err, strings.TrimSpace(string(output)))
	}
	fingerprint, err := verifiedOpenPGPSigner(output)
	if err != nil {
		return fmt.Errorf("verify InRelease status: %w", err)
	}
	if !strings.EqualFold(fingerprint, expectedFingerprint) {
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

func requireTerminalInReleaseSignature(path string, size int64) error {
	const footer = "-----END PGP SIGNATURE-----\n"
	if size < int64(len(footer)) {
		return fmt.Errorf("InRelease signature footer missing")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Seek(-int64(len(footer)), io.SeekEnd); err != nil {
		return err
	}
	tail := make([]byte, len(footer))
	if _, err = io.ReadFull(file, tail); err != nil || string(tail) != footer {
		return fmt.Errorf("InRelease contains trailing data or a non-canonical signature footer")
	}
	return nil
}

func verifiedOpenPGPSigner(status []byte) (string, error) {
	valid := ""
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "[GNUPG:] ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "[GNUPG:] "))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "EXPKEYSIG", "EXPSIG", "REVKEYSIG", "BADSIG", "ERRSIG", "NO_PUBKEY", "NODATA", "KEYEXPIRED", "SIGEXPIRED":
			return "", fmt.Errorf("OpenPGP status %s is not acceptable", fields[0])
		case "VALIDSIG":
			if len(fields) < 2 || valid != "" {
				return "", fmt.Errorf("missing or ambiguous valid signature")
			}
			valid = strings.ToUpper(fields[1])
			if len(fields) > 10 && isFingerprint(fields[len(fields)-1]) {
				valid = strings.ToUpper(fields[len(fields)-1])
			}
		}
	}
	if valid == "" {
		return "", fmt.Errorf("no valid OpenPGP signature")
	}
	return valid, nil
}

func isFingerprint(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
