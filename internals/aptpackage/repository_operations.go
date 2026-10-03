package aptpackage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ulikunitz/xz"
)

const (
	maxRepositoryReleaseBytes        = 32 << 20
	maxRepositoryIndexBytes          = 256 << 20
	maxRepositorySignatureSize       = 33 << 20
	MaxRepositoryKeyringSize         = 1 << 20
	maxRepositoryKeyringBytes        = MaxRepositoryKeyringSize
	maxRepositoryArtifactBytes       = int64(16) << 30
	maxRepositoryPackageCount        = 100_000
	maxRepositoryControlFields       = 256
	maxVerificationTempBytes   int64 = 2 << 30
)

var ErrRepositoryPackageNotFound = errors.New("package identity not found")

type RepositoryOptions struct {
	Codename      string
	Keyring       []byte // exported public OpenPGP keys; secret keys are rejected
	Fingerprint   string // optional expected primary fingerprint
	RequireSigned bool
	Package       string
	Version       string
	Architecture  string
	Workers       int
}

type RepositoryIndex struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type RepositoryPackage struct {
	Package      string            `json:"package"`
	Version      string            `json:"version"`
	Architecture string            `json:"architecture"`
	Filename     string            `json:"filename"`
	Size         int64             `json:"size"`
	SHA256       string            `json:"sha256"`
	Control      map[string]string `json:"control,omitempty"`
}

type RepositoryReport struct {
	Signed            bool                `json:"signed"`
	SignatureVerified bool                `json:"signatureVerified"`
	Signer            string              `json:"signer,omitempty"`
	Origin            string              `json:"origin,omitempty"`
	Label             string              `json:"label,omitempty"`
	Suite             string              `json:"suite,omitempty"`
	Codename          string              `json:"codename,omitempty"`
	Architectures     []string            `json:"architectures"`
	Components        []string            `json:"components"`
	PackageCount      int                 `json:"packageCount"`
	Indexes           []RepositoryIndex   `json:"indexes"`
	Packages          []RepositoryPackage `json:"packages"`
}

type RepositoryVerifyResult struct {
	Valid bool `json:"valid"`
	RepositoryReport
	Error string `json:"error,omitempty"`
}

// InspectRepository reports the listed state described by Release and Packages.
// It verifies the Release-to-index hashes, but never reads package archives.
func InspectRepository(ctx context.Context, reader RepositoryReader, options RepositoryOptions) (RepositoryReport, error) {
	return readRepository(ctx, reader, options, false)
}

// VerifyRepository checks repository metadata and every indexed artifact's
// size, digest, and Debian control identity.
func VerifyRepository(ctx context.Context, reader RepositoryReader, options RepositoryOptions) (RepositoryReport, error) {
	report, err := readRepository(ctx, reader, options, true)
	return report, err
}

func readRepository(ctx context.Context, reader RepositoryReader, options RepositoryOptions, verify bool) (RepositoryReport, error) {
	if options.Codename == "" {
		options.Codename = "stable"
	}
	if !validRepositorySegment(options.Codename) {
		return RepositoryReport{}, fmt.Errorf("invalid distribution codename %q", options.Codename)
	}
	if verify && (options.Package != "" || options.Version != "" || options.Architecture != "") {
		return RepositoryReport{}, fmt.Errorf("exact package lookup is available with inspect; verify checks the full repository")
	}
	if (options.Package != "" || options.Version != "" || options.Architecture != "") && (options.Package == "" || options.Version == "" || options.Architecture == "") {
		return RepositoryReport{}, fmt.Errorf("exact package lookup requires package, version, and architecture")
	}
	if len(options.Keyring) > maxRepositoryKeyringBytes {
		return RepositoryReport{}, fmt.Errorf("public keyring exceeds %d bytes", maxRepositoryKeyringBytes)
	}
	if options.Fingerprint != "" && !isFingerprint(strings.ToUpper(options.Fingerprint)) {
		return RepositoryReport{}, fmt.Errorf("expected fingerprint must be a full OpenPGP fingerprint")
	}

	dist := path.Join("dists", options.Codename)
	releaseData, releaseErr := readRepositoryFile(ctx, reader, path.Join(dist, "Release"), maxRepositoryReleaseBytes)
	inReleaseData, inReleaseErr := readRepositoryFile(ctx, reader, path.Join(dist, "InRelease"), maxRepositorySignatureSize)
	signed := inReleaseErr == nil
	if releaseErr != nil && !isRepositoryNotFound(releaseErr) {
		return RepositoryReport{}, fmt.Errorf("read Release: %w", releaseErr)
	}
	if inReleaseErr != nil && !isRepositoryNotFound(inReleaseErr) {
		return RepositoryReport{}, fmt.Errorf("read InRelease: %w", inReleaseErr)
	}
	if !signed && releaseErr != nil {
		return RepositoryReport{}, fmt.Errorf("repository has neither Release nor InRelease: %w", releaseErr)
	}
	if options.RequireSigned && !signed {
		return RepositoryReport{}, fmt.Errorf("repository is unsigned; a valid InRelease is required")
	}
	var signer string
	if signed {
		if verify || len(options.Keyring) != 0 || options.RequireSigned {
			if len(options.Keyring) == 0 {
				return RepositoryReport{}, fmt.Errorf("signed repository verification requires --keyring with public keys")
			}
			payload, actualSigner, err := verifyInReleasePublic(ctx, inReleaseData, options.Keyring, options.Fingerprint)
			if err != nil {
				return RepositoryReport{}, err
			}
			signer = actualSigner
			if releaseErr == nil && !bytes.Equal(payload, releaseData) {
				return RepositoryReport{}, fmt.Errorf("InRelease signed payload differs from Release")
			}
			if releaseErr != nil {
				releaseData = payload
			}
		} else if releaseErr != nil {
			return RepositoryReport{}, fmt.Errorf("repository has only InRelease; supply --keyring to inspect its signed payload")
		}
	} else {
		if _, err := reader.Open(ctx, path.Join(dist, "Release.gpg")); err == nil {
			return RepositoryReport{}, fmt.Errorf("detached Release.gpg signatures are not supported; use InRelease")
		} else if !isRepositoryNotFound(err) {
			return RepositoryReport{}, fmt.Errorf("inspect Release.gpg: %w", err)
		}
	}

	fields, checksums, err := parseRepositoryRelease(releaseData)
	if err != nil {
		return RepositoryReport{}, fmt.Errorf("parse Release: %w", err)
	}
	architectures := splitUnique(fields["architectures"])
	components := splitUnique(fields["components"])
	if len(architectures) == 0 || len(components) == 0 {
		return RepositoryReport{}, fmt.Errorf("Release must declare Architectures and Components")
	}
	for _, arch := range architectures {
		if !validRepositorySegment(arch) {
			return RepositoryReport{}, fmt.Errorf("invalid Release architecture %q", arch)
		}
	}
	for _, component := range components {
		if !validRepositorySegment(component) {
			return RepositoryReport{}, fmt.Errorf("invalid Release component %q", component)
		}
	}
	report := RepositoryReport{Signed: signed, SignatureVerified: signer != "", Signer: signer, Origin: fields["origin"], Label: fields["label"], Suite: fields["suite"], Codename: fields["codename"], Architectures: architectures, Components: components, Indexes: []RepositoryIndex{}, Packages: []RepositoryPackage{}}
	indexes, err := discoverPackageIndexes(checksums, architectures, components)
	if err != nil {
		return report, err
	}
	if len(indexes) == 0 {
		return report, fmt.Errorf("Release contains no supported Packages indexes")
	}

	packagesByIdentity := make(map[string]RepositoryPackage)
	for _, index := range indexes {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		indexBytes, indexInfo, err := readPackageIndex(ctx, reader, dist, index, checksums)
		if err != nil {
			return report, fmt.Errorf("index %s: %w", index, err)
		}
		entries, err := parseRepositoryPackages(indexBytes, index.architecture)
		if err != nil {
			return report, fmt.Errorf("parse %s: %w", indexInfo.Path, err)
		}
		report.Indexes = append(report.Indexes, indexInfo)
		for _, entry := range entries {
			identity := strings.Join([]string{entry.Package, entry.Version, entry.Architecture}, "\x00")
			if previous, exists := packagesByIdentity[identity]; exists {
				if previous.Filename != entry.Filename || previous.Size != entry.Size || previous.SHA256 != entry.SHA256 || !sameControlFields(previous.Control, entry.Control) {
					return report, fmt.Errorf("conflicting repository entries for %s %s %s", entry.Package, entry.Version, entry.Architecture)
				}
				continue
			}
			if len(packagesByIdentity) >= maxRepositoryPackageCount {
				return report, fmt.Errorf("repository exceeds the supported limit of %d unique package identities", maxRepositoryPackageCount)
			}
			packagesByIdentity[identity] = entry
		}
	}
	sort.Slice(report.Indexes, func(i, j int) bool { return report.Indexes[i].Path < report.Indexes[j].Path })
	for _, entry := range packagesByIdentity {
		report.Packages = append(report.Packages, entry)
	}
	sort.Slice(report.Packages, func(i, j int) bool {
		a, b := report.Packages[i], report.Packages[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Architecture < b.Architecture
	})
	allPackages := append([]RepositoryPackage(nil), report.Packages...)
	if options.Package != "" {
		filtered := report.Packages[:0]
		for _, entry := range report.Packages {
			if entry.Package == options.Package && entry.Version == options.Version && entry.Architecture == options.Architecture {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			return report, fmt.Errorf("%w: %s %s %s", ErrRepositoryPackageNotFound, options.Package, options.Version, options.Architecture)
		}
		report.Packages = filtered
	}
	report.PackageCount = len(packagesByIdentity)
	if verify {
		workers, err := packWorkerCount(options.Workers)
		if err != nil {
			return report, err
		}
		if err := runPackageJobs(len(allPackages), workers, func(index int) error {
			return verifyRepositoryArtifact(ctx, reader, allPackages[index])
		}); err != nil {
			return report, err
		}
	}
	return report, nil
}

type repositoryIndexLocation struct{ path, architecture string }

func discoverPackageIndexes(checksums map[string]releaseChecksum, architectures, components []string) ([]repositoryIndexLocation, error) {
	declaredArch := make(map[string]bool, len(architectures))
	for _, arch := range architectures {
		declaredArch[arch] = true
	}
	declaredComponent := make(map[string]bool, len(components))
	for _, component := range components {
		declaredComponent[component] = true
	}
	type formats struct {
		architecture    string
		plain, gzip, xz bool
	}
	available := make(map[string]formats)
	for relative := range checksums {
		base, format := relative, "plain"
		switch {
		case strings.HasSuffix(relative, ".gz"):
			base, format = strings.TrimSuffix(relative, ".gz"), "gzip"
		case strings.HasSuffix(relative, ".xz"):
			base, format = strings.TrimSuffix(relative, ".xz"), "xz"
		}
		parts := strings.Split(base, "/")
		if len(parts) != 3 || parts[2] != "Packages" || !strings.HasPrefix(parts[1], "binary-") {
			continue
		}
		arch := strings.TrimPrefix(parts[1], "binary-")
		if arch == "" || !declaredArch[arch] || !declaredComponent[parts[0]] {
			continue
		}
		state := available[base]
		state.architecture = arch
		switch format {
		case "plain":
			state.plain = true
		case "gzip":
			state.gzip = true
		case "xz":
			state.xz = true
		}
		available[base] = state
	}
	bases := make([]string, 0, len(available))
	for base := range available {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	result := make([]repositoryIndexLocation, 0, len(bases))
	for _, base := range bases {
		state := available[base]
		selected := base
		if !state.plain {
			if state.gzip {
				selected = base + ".gz"
			} else if state.xz {
				selected = base + ".xz"
			}
		}
		result = append(result, repositoryIndexLocation{path: selected, architecture: state.architecture})
	}
	return result, nil
}

func readPackageIndex(ctx context.Context, reader RepositoryReader, dist string, selected repositoryIndexLocation, checksums map[string]releaseChecksum) ([]byte, RepositoryIndex, error) {
	base := strings.TrimSuffix(strings.TrimSuffix(selected.path, ".gz"), ".xz")
	plain, hasPlain := []byte(nil), false
	if expected, exists := checksums[base]; exists {
		data, err := readRepositoryFile(ctx, reader, path.Join(dist, base), maxRepositoryIndexBytes)
		if err != nil {
			return nil, RepositoryIndex{}, err
		}
		if err = verifyBytes(data, expected); err != nil {
			return nil, RepositoryIndex{}, fmt.Errorf("%s: %w", base, err)
		}
		plain, hasPlain = data, true
	}
	for _, extension := range []string{".gz", ".xz"} {
		compressedPath := base + extension
		expected, exists := checksums[compressedPath]
		if !exists {
			continue
		}
		compressed, err := readRepositoryFile(ctx, reader, path.Join(dist, compressedPath), maxRepositoryIndexBytes)
		if err != nil {
			return nil, RepositoryIndex{}, err
		}
		if err = verifyBytes(compressed, expected); err != nil {
			return nil, RepositoryIndex{}, fmt.Errorf("%s: %w", compressedPath, err)
		}
		decompressed, err := decompressRepositoryIndex(compressed, extension)
		if err != nil {
			return nil, RepositoryIndex{}, fmt.Errorf("%s: %w", compressedPath, err)
		}
		if hasPlain && !bytes.Equal(plain, decompressed) {
			return nil, RepositoryIndex{}, fmt.Errorf("%s does not match Packages", compressedPath)
		}
		if !hasPlain {
			plain, hasPlain = decompressed, true
		}
	}
	if !hasPlain {
		return nil, RepositoryIndex{}, fmt.Errorf("Release does not reference this Packages index")
	}
	digest := sha256.Sum256(plain)
	return plain, RepositoryIndex{Path: path.Join(dist, base), Size: int64(len(plain)), SHA256: hex.EncodeToString(digest[:])}, nil
}

func decompressRepositoryIndex(data []byte, format string) ([]byte, error) {
	var reader io.Reader
	var closeReader io.Closer
	if format == ".gz" {
		gzipReader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		reader, closeReader = gzipReader, gzipReader
	} else {
		xzReader, err := xz.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		reader = xzReader
	}
	if closeReader != nil {
		defer closeReader.Close()
	}
	result, err := io.ReadAll(io.LimitReader(reader, maxRepositoryIndexBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(result)) > maxRepositoryIndexBytes {
		return nil, fmt.Errorf("decompressed package index exceeds %d bytes", maxRepositoryIndexBytes)
	}
	if closeReader != nil {
		if err := closeReader.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parseRepositoryRelease(data []byte) (map[string]string, map[string]releaseChecksum, error) {
	paragraphs, err := parseRepositoryParagraphs(data, false)
	if err != nil {
		return nil, nil, err
	}
	if len(paragraphs) != 1 {
		return nil, nil, fmt.Errorf("Release must contain exactly one control paragraph")
	}
	fields := make(map[string]string, len(paragraphs[0]))
	for key, value := range paragraphs[0] {
		fields[strings.ToLower(key)] = value
	}
	shaLines := fields["sha256"]
	if shaLines == "" {
		return nil, nil, fmt.Errorf("Release has no SHA256 entries")
	}
	checksums := make(map[string]releaseChecksum)
	for _, line := range strings.Split(shaLines, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 || !validSHA256(parts[0]) {
			return nil, nil, fmt.Errorf("invalid SHA256 entry %q", line)
		}
		size, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || size < 0 {
			return nil, nil, fmt.Errorf("invalid SHA256 size %q", parts[1])
		}
		name, err := repositoryRelativePath(parts[2])
		if err != nil {
			return nil, nil, fmt.Errorf("invalid Release path %q: %w", parts[2], err)
		}
		if _, exists := checksums[name]; exists {
			return nil, nil, fmt.Errorf("duplicate SHA256 path %s", name)
		}
		checksums[name] = releaseChecksum{SHA256: strings.ToLower(parts[0]), Size: size}
	}
	return fields, checksums, nil
}

func parseRepositoryPackages(data []byte, indexArchitecture string) ([]RepositoryPackage, error) {
	paragraphs, err := parseRepositoryParagraphs(data, false)
	if err != nil {
		return nil, err
	}
	entries := make([]RepositoryPackage, 0, len(paragraphs))
	for index, fields := range paragraphs {
		lookup := make(map[string]string, len(fields))
		for key, value := range fields {
			lookup[strings.ToLower(key)] = value
		}
		name, version, architecture := lookup["package"], lookup["version"], lookup["architecture"]
		filename, sizeText, digest := lookup["filename"], lookup["size"], strings.ToLower(lookup["sha256"])
		if !validIdentityField(name) || !validIdentityField(version) || !validIdentityField(architecture) {
			return nil, fmt.Errorf("stanza %d has an invalid package identity", index+1)
		}
		if architecture != indexArchitecture && architecture != "all" {
			return nil, fmt.Errorf("stanza %d architecture %s is invalid for binary-%s", index+1, architecture, indexArchitecture)
		}
		if !strings.HasPrefix(filename, "pool/") {
			return nil, fmt.Errorf("stanza %d Filename is outside pool", index+1)
		}
		if _, err := repositoryRelativePath(filename); err != nil {
			return nil, fmt.Errorf("stanza %d has unsafe Filename: %w", index+1, err)
		}
		size, err := strconv.ParseInt(sizeText, 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("stanza %d has invalid Size", index+1)
		}
		if !validSHA256(digest) {
			return nil, fmt.Errorf("stanza %d has invalid SHA256", index+1)
		}
		control := make(map[string]string, len(fields))
		for key, value := range fields {
			control[key] = value
		}
		entries = append(entries, RepositoryPackage{Package: name, Version: version, Architecture: architecture, Filename: filename, Size: size, SHA256: digest, Control: control})
	}
	return entries, nil
}

func parseRepositoryParagraphs(data []byte, required bool) ([]map[string]string, error) {
	if len(data) > maxRepositoryIndexBytes {
		return nil, fmt.Errorf("control data exceeds %d bytes", maxRepositoryIndexBytes)
	}
	paragraphs := make([]map[string]string, 0)
	fields := map[string]string{}
	fieldNames := map[string]string{}
	last := ""
	var parseErr error
	flush := func() {
		if len(fields) != 0 {
			if len(paragraphs) >= maxRepositoryPackageCount {
				parseErr = fmt.Errorf("control data exceeds %d paragraphs", maxRepositoryPackageCount)
				return
			}
			paragraphs = append(paragraphs, fields)
			fields = map[string]string{}
			fieldNames = map[string]string{}
			last = ""
		}
	}
	lineNumber, offset := 0, 0
	for offset < len(data) {
		lineNumber++
		end := offset + bytes.IndexByte(data[offset:], '\n')
		if end < offset {
			end = len(data)
		}
		lineBytes := data[offset:end]
		offset = end + 1
		if end == len(data) {
			offset = end
		}
		if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		if bytes.IndexByte(lineBytes, '\r') >= 0 || bytes.IndexByte(lineBytes, 0) >= 0 {
			return nil, fmt.Errorf("invalid control character on line %d", lineNumber)
		}
		line := string(lineBytes)
		if line == "" {
			flush()
			if parseErr != nil {
				return nil, parseErr
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == "" {
				return nil, fmt.Errorf("continuation without a field on line %d", lineNumber+1)
			}
			fields[last] += "\n" + strings.TrimLeft(line, " \t")
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, fmt.Errorf("malformed control field on line %d", lineNumber+1)
		}
		name := line[:colon]
		for _, r := range name {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return nil, fmt.Errorf("invalid control field name %q", name)
			}
		}
		key := strings.ToLower(name)
		if prior, duplicate := fieldNames[key]; duplicate {
			return nil, fmt.Errorf("duplicate control field %s (also %s)", name, prior)
		}
		if len(fields) >= maxRepositoryControlFields {
			return nil, fmt.Errorf("control paragraph exceeds %d fields", maxRepositoryControlFields)
		}
		value := strings.TrimLeft(line[colon+1:], " \t")
		fields[name] = value
		fieldNames[key] = name
		last = name
	}
	flush()
	if parseErr != nil {
		return nil, parseErr
	}
	if required && len(paragraphs) == 0 {
		return nil, fmt.Errorf("package index is empty")
	}
	return paragraphs, nil
}

func sameControlFields(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func validIdentityField(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00\t ")
}

func validRepositorySegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
		return false
	}
	return strings.TrimSpace(value) == value
}

func splitUnique(value string) []string {
	seen := map[string]bool{}
	var result []string
	for _, field := range strings.Fields(value) {
		if !seen[field] {
			result = append(result, field)
			seen[field] = true
		}
	}
	sort.Strings(result)
	return result
}

func readRepositoryFile(ctx context.Context, reader RepositoryReader, relative string, maximum int64) ([]byte, error) {
	file, err := reader.Open(ctx, relative)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, maximum+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("repository file %s exceeds %d bytes", relative, maximum)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func isRepositoryNotFound(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(err.Error(), "HTTP 404")
}

func verifyBytes(data []byte, expected releaseChecksum) error {
	if int64(len(data)) != expected.Size {
		return fmt.Errorf("size mismatch: got %d, want %d", len(data), expected.Size)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != strings.ToLower(expected.SHA256) {
		return fmt.Errorf("SHA256 mismatch")
	}
	return nil
}

var repositoryArtifactBudget = newVerificationDiskBudget()

func verifyRepositoryArtifact(ctx context.Context, reader RepositoryReader, entry RepositoryPackage) error {
	if entry.Size <= 0 || entry.Size > maxRepositoryArtifactBytes {
		return fmt.Errorf("artifact %s size %d is outside supported bounds", entry.Filename, entry.Size)
	}
	releaseBudget, err := repositoryArtifactBudget.acquire(ctx, entry.Size)
	if err != nil {
		return err
	}
	defer releaseBudget()
	input, err := reader.Open(ctx, entry.Filename)
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", entry.Filename, err)
	}
	defer input.Close()
	file, err := os.CreateTemp("", "tpa-verify-artifact-*.deb")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx: ctx, reader: input}, entry.Size+1))
	if copyErr != nil {
		file.Close()
		return copyErr
	}
	if n != entry.Size {
		file.Close()
		return fmt.Errorf("artifact %s size mismatch: got %d, want %d", entry.Filename, n, entry.Size)
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		file.Close()
		return fmt.Errorf("artifact %s SHA256 mismatch", entry.Filename)
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	control, err := ParsePackage(name)
	if err != nil {
		return fmt.Errorf("parse artifact %s: %w", entry.Filename, err)
	}
	if control.Name != entry.Package || control.Version != entry.Version || control.Architecture != entry.Architecture {
		return fmt.Errorf("artifact %s identity does not match Packages", entry.Filename)
	}
	return nil
}

type verificationDiskBudget struct {
	mu   sync.Mutex
	cond *sync.Cond
	used int64
}

func newVerificationDiskBudget() *verificationDiskBudget {
	budget := &verificationDiskBudget{}
	budget.cond = sync.NewCond(&budget.mu)
	return budget
}
func (b *verificationDiskBudget) acquire(ctx context.Context, size int64) (func(), error) {
	stop := context.AfterFunc(ctx, func() { b.mu.Lock(); b.cond.Broadcast(); b.mu.Unlock() })
	defer stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fits := size <= maxVerificationTempBytes && b.used+size <= maxVerificationTempBytes
		oversizeExclusive := size > maxVerificationTempBytes && b.used == 0
		if fits || oversizeExclusive {
			b.used += size
			return func() { b.mu.Lock(); b.used -= size; b.cond.Broadcast(); b.mu.Unlock() }, nil
		}
		b.cond.Wait()
	}
}

func verifyInReleasePublic(ctx context.Context, inRelease, keyring []byte, expectedFingerprint string) ([]byte, string, error) {
	if len(keyring) == 0 {
		return nil, "", fmt.Errorf("public keyring is required")
	}
	dir, err := os.MkdirTemp("", "tpa-verify-gpg-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, "", err
	}
	keyPath := filepath.Join(dir, "trusted-public-keys")
	if err = os.WriteFile(keyPath, keyring, 0600); err != nil {
		return nil, "", err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "gpg", append([]string{"--batch", "--no-options", "--homedir", dir}, args...)...)
		return cmd.CombinedOutput()
	}
	if output, importErr := run("--import", keyPath); importErr != nil {
		return nil, "", fmt.Errorf("import public verification keyring: %w: %s", importErr, strings.TrimSpace(string(output)))
	}
	secretKeys, err := run("--with-colons", "--list-secret-keys")
	if err != nil {
		return nil, "", fmt.Errorf("inspect verification keyring: %w", err)
	}
	for _, line := range strings.Split(string(secretKeys), "\n") {
		if strings.HasPrefix(line, "sec:") || strings.HasPrefix(line, "ssb:") {
			return nil, "", fmt.Errorf("verification keyring must contain public keys only")
		}
	}
	path := filepath.Join(dir, "InRelease")
	if err = os.WriteFile(path, inRelease, 0600); err != nil {
		return nil, "", err
	}
	if err = requireTerminalInReleaseSignature(path, int64(len(inRelease))); err != nil {
		return nil, "", err
	}
	status, err := run("--status-fd=1", "--verify", path)
	if err != nil {
		return nil, "", fmt.Errorf("InRelease signature verification failed: %w: %s", err, strings.TrimSpace(string(status)))
	}
	fingerprint, err := verifiedOpenPGPSigner(status)
	if err != nil {
		return nil, "", fmt.Errorf("InRelease signature status invalid: %w", err)
	}
	if expectedFingerprint != "" && !strings.EqualFold(fingerprint, expectedFingerprint) {
		return nil, "", fmt.Errorf("InRelease signer %s does not match expected fingerprint %s", fingerprint, strings.ToUpper(expectedFingerprint))
	}
	decrypt := exec.CommandContext(ctx, "gpg", "--batch", "--no-options", "--homedir", dir, "--decrypt", path)
	payload, err := decrypt.Output()
	if err != nil {
		return nil, "", fmt.Errorf("read InRelease payload: %w", err)
	}
	if len(payload) > maxRepositoryReleaseBytes {
		return nil, "", fmt.Errorf("InRelease payload exceeds %d bytes", maxRepositoryReleaseBytes)
	}
	return payload, fingerprint, nil
}
