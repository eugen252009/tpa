package aptpackage

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PackageIdentity struct {
	Package      string
	Version      string
	Architecture string
}

func (identity PackageIdentity) String() string {
	return identity.Package + " " + identity.Version + " " + identity.Architecture
}

type DeleteTarget struct {
	Identity PackageIdentity
	Listed   bool
	Filename string
}

type DeleteResult struct {
	WasListed bool
}

// ArtifactCleanupError means the package has already been safely unlisted, but
// deleting its now-unreferenced artifact failed.
type ArtifactCleanupError struct {
	Identity PackageIdentity
	Err      error
}

func (err *ArtifactCleanupError) Error() string {
	return fmt.Sprintf("package %s was unlisted, but artifact deletion failed: %v", err.Identity, err.Err)
}

func (err *ArtifactCleanupError) Unwrap() error { return err.Err }

// These seams make the two destructive failure boundaries testable.
var beforeLifecyclePublish = func(string) error { return nil }
var signLifecycleCandidate = signLifecycleRelease
var removeLifecycleArtifact = os.Remove

type lifecycleArtifact struct {
	filename string
	size     int64
	sha256   string
}

type rawControlStanza struct {
	fields map[string]string
	raw    []byte
}

// Unlist removes exactly one Package+Version+Architecture stanza from the APT
// index. The referenced .deb remains in the repository pool.
func Unlist(cfg Config, repository string, identity PackageIdentity) error {
	if err := validatePackageIdentity(identity); err != nil {
		return err
	}
	return withRepositoryLock(repository, func() error {
		root, cfg, err := loadVerifiedLifecycleRepository(cfg, repository)
		if err != nil {
			return err
		}
		artifact, listed, err := findListedArtifact(root, cfg, identity)
		if err != nil {
			return err
		}
		if !listed {
			return fmt.Errorf("package %s is not listed", identity)
		}
		_, err = publishUnlistedCandidate(root, cfg, identity, artifact)
		return err
	})
}

// InspectDeleteTarget reads and verifies the target without modifying the
// repository. Delete re-resolves the identity under its mutation lock.
func InspectDeleteTarget(cfg Config, repository string, identity PackageIdentity) (DeleteTarget, error) {
	if err := validatePackageIdentity(identity); err != nil {
		return DeleteTarget{}, err
	}
	root, cfg, err := loadVerifiedLifecycleRepository(cfg, repository)
	if err != nil {
		return DeleteTarget{}, err
	}
	artifact, listed, err := findListedArtifact(root, cfg, identity)
	if err != nil {
		return DeleteTarget{}, err
	}
	if !listed {
		artifact, err = findUnlistedArtifact(root, cfg, identity)
		if err != nil {
			return DeleteTarget{}, err
		}
	}
	return DeleteTarget{Identity: identity, Listed: listed, Filename: artifact.filename}, nil
}

// Delete first verifies and, when necessary, atomically publishes an unlisted
// repository generation. Only after that publication succeeds is the .deb
// removed from the live repository tree.
func Delete(cfg Config, repository string, identity PackageIdentity) (DeleteResult, error) {
	if err := validatePackageIdentity(identity); err != nil {
		return DeleteResult{}, err
	}
	var result DeleteResult
	err := withRepositoryLock(repository, func() error {
		root, cfg, err := loadVerifiedLifecycleRepository(cfg, repository)
		if err != nil {
			return err
		}
		artifact, listed, err := findListedArtifact(root, cfg, identity)
		if err != nil {
			return err
		}
		if listed {
			poolArtifact, err := findUnlistedArtifact(root, cfg, identity)
			if err != nil {
				return err
			}
			if poolArtifact != artifact {
				return fmt.Errorf("indexed artifact for %s does not match its unique pool artifact", identity)
			}
			exceptIndex, err := lifecyclePackagesPath(root, cfg, identity.Architecture)
			if err != nil {
				return err
			}
			if err := ensureArtifactReferencesSafe(root, artifact.filename, identity, exceptIndex); err != nil {
				return err
			}
			artifact, err = publishUnlistedCandidate(root, cfg, identity, artifact)
			if err != nil {
				return err
			}
			result.WasListed = true
		} else {
			artifact, err = findUnlistedArtifact(root, cfg, identity)
			if err != nil {
				return err
			}
		}
		cleanupError := func(err error) error {
			if result.WasListed {
				return &ArtifactCleanupError{Identity: identity, Err: err}
			}
			return err
		}
		if err := ensureArtifactReferencesSafe(root, artifact.filename, identity, ""); err != nil {
			return cleanupError(err)
		}
		path, err := safeRepositoryPath(root, artifact.filename)
		if err != nil {
			return cleanupError(fmt.Errorf("resolve package artifact: %w", err))
		}
		if err := verifyFile(path, artifact.size, artifact.sha256); err != nil {
			return cleanupError(fmt.Errorf("verify package artifact before deletion: %w", err))
		}
		control, err := ParsePackage(path)
		if err != nil {
			return cleanupError(fmt.Errorf("inspect package artifact before deletion: %w", err))
		}
		if packageIdentity(control) != identity {
			return cleanupError(fmt.Errorf("artifact %s does not match requested package %s", artifact.filename, identity))
		}
		if err := removeLifecycleArtifact(path); err != nil {
			return cleanupError(fmt.Errorf("remove package artifact %s: %w", artifact.filename, err))
		}
		return nil
	})
	return result, err
}

func loadVerifiedLifecycleRepository(cfg Config, repository string) (string, Config, error) {
	if repository == "" {
		return "", cfg, fmt.Errorf("repository path is empty")
	}
	root, err := filepath.Abs(repository)
	if err != nil {
		return "", cfg, fmt.Errorf("resolve repository path: %w", err)
	}
	if root == string(filepath.Separator) {
		return "", cfg, fmt.Errorf("refusing to mutate filesystem root")
	}
	if err := validateRepositoryTree(root); err != nil {
		return "", cfg, err
	}
	component, codename, err := lifecycleLayout(cfg)
	if err != nil {
		return "", cfg, err
	}
	cfg.Repo.Components = component
	cfg.Repo.Codename = codename
	cfg.OutDir = root
	distDir := filepath.Join(root, "dists", codename)
	if _, err := os.Lstat(filepath.Join(distDir, "Release.gpg")); err == nil {
		return "", cfg, fmt.Errorf("detached Release.gpg signatures are unsupported for lifecycle mutations")
	} else if !os.IsNotExist(err) {
		return "", cfg, fmt.Errorf("inspect repository Release.gpg: %w", err)
	}
	_, statErr := os.Lstat(filepath.Join(distDir, "InRelease"))
	signed := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", cfg, fmt.Errorf("inspect repository InRelease: %w", statErr)
	}
	if signed && cfg.GPG == "" {
		return "", cfg, fmt.Errorf("repository is signed; supply -gpg with its signing key")
	}
	if !signed && cfg.GPG != "" {
		return "", cfg, fmt.Errorf("repository is unsigned; omit -gpg")
	}
	if err := verifyRepository(cfg); err != nil {
		return "", cfg, fmt.Errorf("verify repository before lifecycle operation: %w", err)
	}
	return root, cfg, nil
}

func lifecycleLayout(cfg Config) (component, codename string, err error) {
	component, codename = cfg.Repo.Components, cfg.Repo.Codename
	if component == "" {
		component = "main"
	}
	if codename == "" {
		codename = "stable"
	}
	if !safeLifecycleSegment(component) {
		return "", "", fmt.Errorf("invalid repository component %q", component)
	}
	if !safeLifecycleSegment(codename) {
		return "", "", fmt.Errorf("invalid repository codename %q", codename)
	}
	return component, codename, nil
}

func safeLifecycleSegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func validatePackageIdentity(identity PackageIdentity) error {
	if !safeLifecycleSegment(identity.Package) || !safeLifecycleSegment(identity.Version) || !safeLifecycleSegment(identity.Architecture) {
		return fmt.Errorf("package, version, and architecture must be non-empty Debian identity values without whitespace or path separators")
	}
	return nil
}

func packageIdentity(control Control) PackageIdentity {
	return PackageIdentity{Package: control.Name, Version: control.Version, Architecture: control.Architecture}
}

func validateRepositoryTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect repository: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repository root must be a real directory")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("repository contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("repository contains non-regular file %s", path)
		}
		return nil
	})
}

func lifecyclePackagesPath(root string, cfg Config, architecture string) (string, error) {
	component, codename, err := lifecycleLayout(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "dists", codename, component, "binary-"+architecture, "Packages"), nil
}

func findListedArtifact(root string, cfg Config, identity PackageIdentity) (lifecycleArtifact, bool, error) {
	packagesPath, err := lifecyclePackagesPath(root, cfg, identity.Architecture)
	if err != nil {
		return lifecycleArtifact{}, false, err
	}
	data, err := os.ReadFile(packagesPath)
	if os.IsNotExist(err) {
		return lifecycleArtifact{}, false, nil
	}
	if err != nil {
		return lifecycleArtifact{}, false, err
	}
	stanzas, err := parseRawControlStanzas(data)
	if err != nil {
		return lifecycleArtifact{}, false, fmt.Errorf("parse package index: %w", err)
	}
	var found []lifecycleArtifact
	for _, stanza := range stanzas {
		if stanza.fields["package"] != identity.Package || stanza.fields["version"] != identity.Version || stanza.fields["architecture"] != identity.Architecture {
			continue
		}
		filename := stanza.fields["filename"]
		size, sizeErr := strconv.ParseInt(stanza.fields["size"], 10, 64)
		sha := stanza.fields["sha256"]
		if filename == "" || sizeErr != nil || size < 0 || !validSHA256(sha) {
			return lifecycleArtifact{}, false, fmt.Errorf("package %s has invalid artifact metadata", identity)
		}
		if _, pathErr := safeRepositoryPath(root, filename); pathErr != nil {
			return lifecycleArtifact{}, false, fmt.Errorf("package %s has invalid Filename: %w", identity, pathErr)
		}
		found = append(found, lifecycleArtifact{filename: filename, size: size, sha256: strings.ToLower(sha)})
	}
	if len(found) > 1 {
		return lifecycleArtifact{}, false, fmt.Errorf("package identity %s occurs more than once in Packages", identity)
	}
	if len(found) == 0 {
		return lifecycleArtifact{}, false, nil
	}
	path, _ := safeRepositoryPath(root, found[0].filename)
	control, err := ParsePackage(path)
	if err != nil {
		return lifecycleArtifact{}, false, fmt.Errorf("inspect indexed artifact for %s: %w", identity, err)
	}
	if packageIdentity(control) != identity {
		return lifecycleArtifact{}, false, fmt.Errorf("indexed artifact %s does not match package identity %s", found[0].filename, identity)
	}
	return found[0], true, nil
}

func findUnlistedArtifact(root string, cfg Config, identity PackageIdentity) (lifecycleArtifact, error) {
	component, _, err := lifecycleLayout(cfg)
	if err != nil {
		return lifecycleArtifact{}, err
	}
	if !safeLifecycleSegment(identity.Package) {
		return lifecycleArtifact{}, fmt.Errorf("invalid package name %q", identity.Package)
	}
	packageDir := filepath.Join(root, "pool", component, identity.Package[:1], identity.Package)
	entries, err := os.ReadDir(packageDir)
	if os.IsNotExist(err) {
		return lifecycleArtifact{}, fmt.Errorf("package artifact not found: %s", identity)
	}
	if err != nil {
		return lifecycleArtifact{}, fmt.Errorf("read package artifact directory: %w", err)
	}
	var matches []lifecycleArtifact
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".deb") {
			continue
		}
		path := filepath.Join(packageDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return lifecycleArtifact{}, err
		}
		if !info.Mode().IsRegular() {
			return lifecycleArtifact{}, fmt.Errorf("package artifact is not a regular file: %s", path)
		}
		control, err := ParsePackage(path)
		if err != nil {
			return lifecycleArtifact{}, fmt.Errorf("inspect package artifact %s: %w", path, err)
		}
		if packageIdentity(control) != identity {
			continue
		}
		hash, err := getHash(path)
		if err != nil {
			return lifecycleArtifact{}, err
		}
		matches = append(matches, lifecycleArtifact{
			filename: filepath.ToSlash(filepath.Join("pool", component, identity.Package[:1], identity.Package, entry.Name())),
			size:     info.Size(),
			sha256:   hash,
		})
	}
	if len(matches) == 0 {
		return lifecycleArtifact{}, fmt.Errorf("package artifact not found: %s", identity)
	}
	if len(matches) > 1 {
		return lifecycleArtifact{}, fmt.Errorf("multiple package artifacts found for identity %s", identity)
	}
	return matches[0], nil
}

func ensureArtifactReferencesSafe(root, filename string, identity PackageIdentity, exceptIndex string) error {
	if filename == "" {
		return fmt.Errorf("package artifact filename is empty")
	}
	distsDir := filepath.Join(root, "dists")
	exceptPath := filepath.Clean(exceptIndex)
	skippedTarget := false
	err := filepath.WalkDir(distsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "Packages" || !startsWithBinary(filepath.Base(filepath.Dir(path))) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		stanzas, err := parseRawControlStanzas(data)
		if err != nil {
			return fmt.Errorf("parse package index %s: %w", path, err)
		}
		for _, stanza := range stanzas {
			if stanza.fields["filename"] != filename {
				continue
			}
			if !skippedTarget && exceptIndex != "" && filepath.Clean(path) == exceptPath &&
				stanza.fields["package"] == identity.Package && stanza.fields["version"] == identity.Version && stanza.fields["architecture"] == identity.Architecture {
				skippedTarget = true
				continue
			}
			other := PackageIdentity{Package: stanza.fields["package"], Version: stanza.fields["version"], Architecture: stanza.fields["architecture"]}
			return fmt.Errorf("artifact %s is still referenced by Packages identity %s", filename, other)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if exceptIndex != "" && !skippedTarget {
		return fmt.Errorf("package %s stanza does not reference expected artifact %s", identity, filename)
	}
	return nil
}

func parseRawControlStanzas(data []byte) ([]rawControlStanza, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	stanzas := make([]rawControlStanza, 0)
	fields := make(map[string]string)
	var raw bytes.Buffer
	lastField := ""
	flush := func() {
		if len(fields) == 0 {
			return
		}
		stanzaBytes := bytes.TrimRight(raw.Bytes(), "\n")
		stanzas = append(stanzas, rawControlStanza{fields: fields, raw: append(append([]byte(nil), stanzaBytes...), '\n')})
		fields = make(map[string]string)
		raw.Reset()
		lastField = ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasSuffix(line, "\r") {
			return nil, fmt.Errorf("CRLF control index is not canonical")
		}
		if line == "" {
			flush()
			continue
		}
		raw.WriteString(line)
		raw.WriteByte('\n')
		if line[0] == ' ' || line[0] == '\t' {
			if lastField == "" {
				return nil, fmt.Errorf("control continuation without a field")
			}
			fields[lastField] += "\n" + strings.TrimLeft(line, " \t")
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 || strings.ContainsAny(line[:colon], " \t") {
			return nil, fmt.Errorf("invalid control field %q", line)
		}
		lastField = strings.ToLower(line[:colon])
		if _, exists := fields[lastField]; exists {
			return nil, fmt.Errorf("duplicate control field %s", line[:colon])
		}
		fields[lastField] = strings.TrimSpace(line[colon+1:])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return stanzas, nil
}

func removeIdentityFromIndex(packagesPath string, identity PackageIdentity) (lifecycleArtifact, error) {
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		return lifecycleArtifact{}, err
	}
	stanzas, err := parseRawControlStanzas(data)
	if err != nil {
		return lifecycleArtifact{}, err
	}
	var target lifecycleArtifact
	matches := 0
	var output bytes.Buffer
	for _, stanza := range stanzas {
		if stanza.fields["package"] == identity.Package && stanza.fields["version"] == identity.Version && stanza.fields["architecture"] == identity.Architecture {
			matches++
			if matches > 1 {
				return lifecycleArtifact{}, fmt.Errorf("package identity %s occurs more than once in Packages", identity)
			}
			size, sizeErr := strconv.ParseInt(stanza.fields["size"], 10, 64)
			target = lifecycleArtifact{filename: stanza.fields["filename"], size: size, sha256: strings.ToLower(stanza.fields["sha256"])}
			if target.filename == "" || sizeErr != nil || size < 0 || !validSHA256(target.sha256) {
				return lifecycleArtifact{}, fmt.Errorf("package %s has invalid artifact metadata", identity)
			}
			continue
		}
		output.Write(stanza.raw)
		output.WriteByte('\n')
	}
	if matches == 0 {
		return lifecycleArtifact{}, fmt.Errorf("package %s is not listed", identity)
	}
	if err := os.WriteFile(packagesPath, output.Bytes(), 0o644); err != nil {
		return lifecycleArtifact{}, err
	}
	return target, nil
}

func publishUnlistedCandidate(root string, cfg Config, identity PackageIdentity, expected lifecycleArtifact) (lifecycleArtifact, error) {
	parent := filepath.Dir(root)
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(root)+".tpa-lifecycle-")
	if err != nil {
		return lifecycleArtifact{}, fmt.Errorf("create lifecycle staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := copyRepositoryTree(root, staging); err != nil {
		return lifecycleArtifact{}, fmt.Errorf("copy repository to lifecycle staging: %w", err)
	}
	component, codename, err := lifecycleLayout(cfg)
	if err != nil {
		return lifecycleArtifact{}, err
	}
	packagesPath := filepath.Join(staging, "dists", codename, component, "binary-"+identity.Architecture, "Packages")
	artifact, err := removeIdentityFromIndex(packagesPath, identity)
	if err != nil {
		return lifecycleArtifact{}, err
	}
	if artifact != expected {
		return lifecycleArtifact{}, fmt.Errorf("package %s changed while preparing lifecycle generation", identity)
	}
	if err := gzipFile(packagesPath); err != nil {
		return lifecycleArtifact{}, err
	}
	if err := rewriteLifecycleRelease(cfg, staging); err != nil {
		return lifecycleArtifact{}, err
	}
	if err := writeRepositoryBrowserFiles(staging, cfg.Repo); err != nil {
		return lifecycleArtifact{}, err
	}
	if err := signLifecycleCandidate(cfg, staging, codename); err != nil {
		return lifecycleArtifact{}, err
	}
	if err := prepareRepositoryPermissions(staging); err != nil {
		return lifecycleArtifact{}, fmt.Errorf("prepare lifecycle generation permissions: %w", err)
	}
	cfg.OutDir = staging
	if err := verifyRepository(cfg); err != nil {
		return lifecycleArtifact{}, fmt.Errorf("verify lifecycle generation: %w", err)
	}
	if err := beforeLifecyclePublish(staging); err != nil {
		return lifecycleArtifact{}, fmt.Errorf("publish lifecycle generation: %w", err)
	}
	if err := publishDirectory(staging, root); err != nil {
		return lifecycleArtifact{}, fmt.Errorf("publish lifecycle generation: %w", err)
	}
	return artifact, nil
}

func signLifecycleRelease(cfg Config, root, codename string) error {
	if cfg.GPG == "" {
		return nil
	}
	fingerprint, err := signingFingerprint(cfg.GPG)
	if err != nil {
		return err
	}
	releasePath := filepath.Join(root, "dists", codename, "Release")
	inReleasePath := filepath.Join(root, "dists", codename, "InRelease")
	if output, err := exec.Command("gpg", "--batch", "--yes", "--clearsign", "-u", fingerprint, "-o", inReleasePath, releasePath).CombinedOutput(); err != nil {
		return fmt.Errorf("sign updated Release: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyRepositoryTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("repository contains symlink %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("repository contains non-regular file %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			input.Close()
			return err
		}
		copyErr := copyLifecycleFile(input, output)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return outputCloseErr
	})
}

func rewriteLifecycleRelease(cfg Config, root string) error {
	_, codename, err := lifecycleLayout(cfg)
	if err != nil {
		return err
	}
	distDir := filepath.Join(root, "dists", codename)
	releasePath := filepath.Join(distDir, "Release")
	data, err := os.ReadFile(releasePath)
	if err != nil {
		return err
	}
	checksums, err := parseReleaseChecksums(data)
	if err != nil {
		return err
	}
	marker := []byte("\nSHA256:\n")
	markerAt := bytes.Index(data, marker)
	if markerAt < 0 {
		return fmt.Errorf("Release is missing its SHA256 section")
	}
	header := data[:markerAt+1]
	var updatedHeader bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(header))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	dateCount, architectureCount := 0, 0
	lastField := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if lastField == "date" || lastField == "architectures" {
				return fmt.Errorf("Release field %s unexpectedly has continuations", lastField)
			}
			updatedHeader.WriteString(line)
			updatedHeader.WriteByte('\n')
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return fmt.Errorf("invalid Release field %q", line)
		}
		lastField = strings.ToLower(line[:colon])
		switch lastField {
		case "date":
			dateCount++
			updatedHeader.WriteString("Date: ")
			updatedHeader.WriteString(time.Now().UTC().Format(time.RFC1123Z))
			updatedHeader.WriteByte('\n')
		case "architectures":
			architectureCount++
			updatedHeader.WriteString(line)
			updatedHeader.WriteByte('\n')
		default:
			updatedHeader.WriteString(line)
			updatedHeader.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if dateCount != 1 || architectureCount != 1 {
		return fmt.Errorf("Release must contain exactly one Date and Architectures field")
	}

	paths := make([]string, 0, len(checksums))
	for path := range checksums {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var output bytes.Buffer
	output.Write(updatedHeader.Bytes())
	output.WriteString("SHA256:\n")
	for _, rel := range paths {
		path, err := safeRepositoryPath(distDir, rel)
		if err != nil {
			return fmt.Errorf("invalid Release path %q: %w", rel, err)
		}
		hash, err := getHash(path)
		if err != nil {
			return fmt.Errorf("hash Release entry %s: %w", rel, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Release entry %s is not a regular file", rel)
		}
		fmt.Fprintf(&output, " %s %d %s\n", hash, info.Size(), rel)
	}
	return os.WriteFile(releasePath, output.Bytes(), 0o644)
}
