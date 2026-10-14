package aptpackage

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func readRepositoryTreeFile(filePath string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("repository path is not a regular file: %s", filePath)
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("repository file exceeds %d bytes: %s", maximum, filePath)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("repository file exceeds %d bytes: %s", maximum, filePath)
	}
	return data, nil
}

func validateTPARepositoryLayout(cfg Config) error {
	root := cfg.OutDir
	component, codename := cfg.Repo.Components, cfg.Repo.Codename
	if component == "" {
		component = "main"
	}
	if codename == "" {
		codename = "stable"
	}
	if !validRepositoryNameSegment(component) || !validRepositoryNameSegment(codename) {
		return fmt.Errorf("invalid repository component or codename")
	}
	if err := requireTreeEntries(root, map[string]bool{
		"dists": true, "pool": true, "index.html": false, "repository.json": false,
	}); err != nil {
		return err
	}
	distsRoot := filepath.Join(root, "dists")
	if err := requireTreeEntries(distsRoot, map[string]bool{codename: true}); err != nil {
		return err
	}
	distRoot := filepath.Join(distsRoot, codename)
	releaseData, err := readRepositoryTreeFile(filepath.Join(distRoot, "Release"), maxRepositoryReleaseBytes)
	if err != nil {
		return fmt.Errorf("read repository Release: %w", err)
	}
	fields, checksums, err := parseRepositoryRelease(releaseData)
	if err != nil {
		return fmt.Errorf("parse repository Release: %w", err)
	}
	if fields["codename"] != codename {
		return fmt.Errorf("Release Codename does not match repository path")
	}
	components := strings.Fields(fields["components"])
	if len(components) != 1 || components[0] != component || fields["components"] != component {
		return fmt.Errorf("TPA repository v1 requires exactly its configured component")
	}
	for _, name := range []string{"origin", "label", "suite", "codename", "components", "date", "description"} {
		if err := validateReleaseField(name, fields[name]); err != nil {
			return err
		}
	}
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
	for _, expected := range [][2]string{{"origin", origin}, {"label", label}, {"suite", suite}, {"description", description}} {
		if fields[expected[0]] != expected[1] {
			return fmt.Errorf("Release %s does not match configured repository metadata", expected[0])
		}
	}
	if _, err := time.Parse(time.RFC1123Z, fields["date"]); err != nil {
		return fmt.Errorf("invalid Release Date %q", fields["date"])
	}
	architectures := strings.Fields(fields["architectures"])
	if len(architectures) == 0 || strings.Join(architectures, " ") != fields["architectures"] {
		return fmt.Errorf("Release Architectures is empty or non-canonical")
	}
	for index, architecture := range architectures {
		if !validRepositoryArchitecture(architecture) || (index > 0 && architectures[index-1] >= architecture) {
			return fmt.Errorf("Release Architectures must be sorted, unique, and valid")
		}
	}
	if len(cfg.emptyRepositoryArchitectures) > 0 {
		expectedArchitectures := append([]string(nil), cfg.emptyRepositoryArchitectures...)
		sort.Strings(expectedArchitectures)
		if strings.Join(expectedArchitectures, " ") != fields["architectures"] {
			return fmt.Errorf("Release Architectures do not match the requested empty-repository architectures")
		}
	}

	inRelease := false
	if info, err := os.Lstat(filepath.Join(distRoot, "InRelease")); err == nil {
		if !info.Mode().IsRegular() || info.Size() > maxRepositorySignatureSize {
			return fmt.Errorf("InRelease has an invalid type or exceeds %d bytes", maxRepositorySignatureSize)
		}
		inRelease = true
	} else if !os.IsNotExist(err) {
		return err
	}
	distEntries := map[string]bool{"Release": false, component: true}
	if inRelease {
		distEntries["InRelease"] = false
	}
	if err := requireTreeEntries(distRoot, distEntries); err != nil {
		return err
	}
	componentRoot := filepath.Join(distRoot, component)
	indexDirectories := make(map[string]bool, len(architectures))
	checksummedPaths := make(map[string]bool, len(architectures)*2)
	indexedIdentities := make(map[PackageIdentity]bool)
	for _, architecture := range architectures {
		directory := "binary-" + architecture
		indexDirectories[directory] = true
		indexRoot := filepath.Join(componentRoot, directory)
		if err := requireTreeEntries(indexRoot, map[string]bool{"Packages": false, "Packages.gz": false}); err != nil {
			return err
		}
		for _, name := range []string{"Packages", "Packages.gz"} {
			relative := path.Join(component, directory, name)
			if _, exists := checksums[relative]; !exists {
				return fmt.Errorf("Release is missing checksum for %s", relative)
			}
			checksummedPaths[relative] = true
		}
		packagesPath := filepath.Join(indexRoot, "Packages")
		data, err := readRepositoryTreeFile(packagesPath, maxRepositoryIndexBytes)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("%s contains invalid UTF-8 control data", directory)
		}
		entries, err := parseRepositoryPackages(data, architecture)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filepath.ToSlash(filepath.Join(component, directory, "Packages")), err)
		}
		for index, entry := range entries {
			if !validRepositoryPackageName(entry.Package) || !safeLifecycleSegment(entry.Version) || !validRepositoryArchitecture(entry.Architecture) || entry.Architecture != architecture {
				return fmt.Errorf("%s stanza %d has an invalid package identity", directory, index+1)
			}
			identity := PackageIdentity{Package: entry.Package, Version: entry.Version, Architecture: entry.Architecture}
			if indexedIdentities[identity] {
				return fmt.Errorf("repository repeats package identity %s", identity)
			}
			if len(indexedIdentities) >= maxRepositoryPackageCount {
				return fmt.Errorf("repository exceeds %d package identities", maxRepositoryPackageCount)
			}
			indexedIdentities[identity] = true
			basename := path.Base(entry.Filename)
			expected := path.Join("pool", component, entry.Package[:1], entry.Package, basename)
			if entry.Filename != expected || !validRepositoryArtifactBasename(basename) {
				return fmt.Errorf("%s stanza %d has a non-canonical artifact path", directory, index+1)
			}
			sha, _ := getRepositoryField(entry.Control, "SHA256")
			size, _ := getRepositoryField(entry.Control, "Size")
			if sha != entry.SHA256 || size != strconv.FormatInt(entry.Size, 10) {
				return fmt.Errorf("%s stanza %d has non-canonical artifact metadata", directory, index+1)
			}
		}
	}
	if err := requireTreeEntries(componentRoot, indexDirectories); err != nil {
		return err
	}
	if len(checksums) != len(checksummedPaths) {
		return fmt.Errorf("Release contains paths outside the TPA repository v1 index set")
	}
	for _, checksum := range checksums {
		if checksum.Size < 0 || checksum.Size > maxRepositoryIndexBytes {
			return fmt.Errorf("Release index size exceeds %d bytes", maxRepositoryIndexBytes)
		}
	}
	for _, line := range strings.Split(fields["sha256"], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 || parts[0] != strings.ToLower(parts[0]) {
			return fmt.Errorf("Release SHA256 entries are not canonical")
		}
		size, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || size < 0 || strconv.FormatInt(size, 10) != parts[1] {
			return fmt.Errorf("Release SHA256 entry has non-canonical size")
		}
	}
	return validateTPAPool(root, component, architectures)
}

func requireTreeEntries(directory string, expected map[string]bool) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read repository directory %s: %w", directory, err)
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("repository directory %s has unexpected entries", directory)
	}
	for _, entry := range entries {
		wantDirectory, exists := expected[entry.Name()]
		if !exists || entry.IsDir() != wantDirectory {
			return fmt.Errorf("repository directory %s has unexpected entry %s", directory, entry.Name())
		}
	}
	return nil
}

func validateTPAPool(root, component string, architectures []string) error {
	poolRoot := filepath.Join(root, "pool")
	if err := requireTreeEntries(poolRoot, map[string]bool{component: true}); err != nil {
		return err
	}
	componentRoot := filepath.Join(poolRoot, component)
	firstEntries, err := os.ReadDir(componentRoot)
	if err != nil {
		return err
	}
	declaredArchitectures := make(map[string]bool, len(architectures))
	for _, architecture := range architectures {
		declaredArchitectures[architecture] = true
	}
	identities := make(map[PackageIdentity]string)
	for _, firstEntry := range firstEntries {
		if !firstEntry.IsDir() || len(firstEntry.Name()) != 1 || !validRepositoryNameSegment(firstEntry.Name()) {
			return fmt.Errorf("invalid package-prefix directory in pool")
		}
		firstRoot := filepath.Join(componentRoot, firstEntry.Name())
		packageEntries, err := os.ReadDir(firstRoot)
		if err != nil {
			return err
		}
		for _, packageEntry := range packageEntries {
			packageName := packageEntry.Name()
			if !packageEntry.IsDir() || !validRepositoryPackageName(packageName) || packageName[:1] != firstEntry.Name() {
				return fmt.Errorf("invalid package directory in pool")
			}
			packageRoot := filepath.Join(firstRoot, packageName)
			artifacts, err := os.ReadDir(packageRoot)
			if err != nil {
				return err
			}
			for _, artifact := range artifacts {
				if artifact.IsDir() || !validRepositoryArtifactBasename(artifact.Name()) {
					return fmt.Errorf("invalid artifact entry in pool for %s", packageName)
				}
				artifactPath := filepath.Join(packageRoot, artifact.Name())
				info, err := artifact.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRepositoryArtifactBytes {
					return fmt.Errorf("pool artifact has an invalid type or size: %s", artifactPath)
				}
				control, err := ParsePackage(artifactPath)
				if err != nil {
					return fmt.Errorf("inspect pool artifact %s: %w", artifactPath, err)
				}
				if control.Name != packageName || !validRepositoryArchitecture(control.Architecture) ||
					!safeLifecycleSegment(control.Version) || !declaredArchitectures[control.Architecture] {
					return fmt.Errorf("pool artifact %s does not match its canonical package path or declared architecture", artifactPath)
				}
				identity := packageIdentity(control)
				if previous, exists := identities[identity]; exists {
					return fmt.Errorf("duplicate pool artifacts for package identity %s: %s and %s", identity, previous, artifactPath)
				}
				if len(identities) >= maxRepositoryPackageCount {
					return fmt.Errorf("repository pool exceeds %d package artifacts", maxRepositoryPackageCount)
				}
				identities[identity] = artifactPath
			}
		}
	}
	return nil
}
