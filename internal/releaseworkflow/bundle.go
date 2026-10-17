package releaseworkflow

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ReleaseManifestSchemaVersion = 1
const MaxGitHubReleaseAssetSize int64 = 2 << 30

type SourceArchive struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type ReleaseArtifact struct {
	Filename             string            `json:"filename"`
	Package              string            `json:"package"`
	Version              string            `json:"version"`
	Architecture         string            `json:"architecture"`
	Size                 int64             `json:"size"`
	SHA256               string            `json:"sha256"`
	RuntimeQualification string            `json:"runtime_qualification"`
	PackageQualification string            `json:"package_qualification"`
	InstallQualification string            `json:"install_qualification"`
	UpgradeQualification string            `json:"upgrade_qualification"`
	ControlFields        map[string]string `json:"control_fields,omitempty"`
}

type BuildIdentity struct {
	WorkflowRef     string   `json:"workflow_ref,omitempty"`
	WorkflowSHA     string   `json:"workflow_sha,omitempty"`
	RunID           string   `json:"run_id,omitempty"`
	RunAttempt      string   `json:"run_attempt,omitempty"`
	GoVersion       string   `json:"go_version,omitempty"`
	TPAVersion      string   `json:"tpa_version"`
	TPACommit       string   `json:"tpa_commit"`
	BuildCommand    string   `json:"build_command"`
	TestCommand     string   `json:"test_command"`
	ProjectTests    string   `json:"project_tests"`
	BuildFlags      []string `json:"build_flags,omitempty"`
	SourceDateEpoch int64    `json:"source_date_epoch"`
	Reproducible    bool     `json:"reproducible"`
	RebuildHashes   []string `json:"rebuild_hashes"`
}

type ReleaseManifest struct {
	SchemaVersion      int               `json:"schema_version"`
	Project            string            `json:"project"`
	Package            string            `json:"package"`
	Version            string            `json:"version"`
	Tag                string            `json:"tag,omitempty"`
	SourceRepo         string            `json:"source_repository,omitempty"`
	SourceCommit       string            `json:"source_commit"`
	ConfigSHA256       string            `json:"config_sha256"`
	SourceInputs       map[string]string `json:"source_inputs"`
	SourceArchive      SourceArchive     `json:"source_archive"`
	ReleaseNotesSHA256 string            `json:"release_notes_sha256"`
	Build              BuildIdentity     `json:"build"`
	Artifacts          []ReleaseArtifact `json:"artifacts"`
}

type Provenance struct {
	SchemaVersion      int               `json:"schema_version"`
	PredicateType      string            `json:"predicate_type"`
	Project            string            `json:"project"`
	Package            string            `json:"package"`
	Version            string            `json:"version"`
	Tag                string            `json:"tag,omitempty"`
	SourceRepo         string            `json:"source_repository,omitempty"`
	SourceCommit       string            `json:"source_commit"`
	ConfigSHA256       string            `json:"config_sha256"`
	SourceInputs       map[string]string `json:"source_inputs"`
	SourceArchive      SourceArchive     `json:"source_archive"`
	ReleaseNotesSHA256 string            `json:"release_notes_sha256"`
	Builder            BuildIdentity     `json:"builder"`
	Artifacts          []ReleaseArtifact `json:"artifacts"`
}

type BundleOptions struct {
	Workspace, ArtifactsRoot, OutputDir string
	Plan                                Plan
	TPACommit, TPAVersion, GoVersion    string
	WorkflowRef, WorkflowSHA            string
	RunID, RunAttempt, SourceRepository string
}

func CreateBundle(cfg Config, options BundleOptions) (ReleaseManifest, error) {
	if err := ValidatePlanInputs(cfg, options.Workspace, options.Plan); err != nil {
		return ReleaseManifest{}, err
	}
	if err := ValidateWorkspaceChanges(cfg, options.Workspace); err != nil {
		return ReleaseManifest{}, err
	}
	if err := validateBuildArtifacts(options.ArtifactsRoot, options.Plan); err != nil {
		return ReleaseManifest{}, err
	}
	if filepath.IsAbs(options.OutputDir) == false {
		return ReleaseManifest{}, fmt.Errorf("bundle output directory must be absolute")
	}
	if _, err := os.Lstat(options.OutputDir); err == nil {
		return ReleaseManifest{}, fmt.Errorf("bundle output directory already exists: %s", options.OutputDir)
	} else if !os.IsNotExist(err) {
		return ReleaseManifest{}, err
	}
	if err := os.MkdirAll(options.OutputDir, 0755); err != nil {
		return ReleaseManifest{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(options.OutputDir)
		}
	}()

	artifacts := make([]ReleaseArtifact, 0, len(options.Plan.Architectures))
	rebuildHashes := make([]string, 0, len(options.Plan.Architectures))
	for _, arch := range options.Plan.Architectures {
		var first PackageRecord
		for rebuild := 1; ; rebuild++ {
			folder := filepath.Join(options.ArtifactsRoot, artifactName(arch, rebuild))
			folderInfo, err := os.Lstat(folder)
			if err != nil || !folderInfo.IsDir() || folderInfo.Mode()&os.ModeSymlink != 0 {
				return ReleaseManifest{}, fmt.Errorf("build artifact %s is not a real directory", folder)
			}
			entries, err := os.ReadDir(folder)
			if err != nil {
				return ReleaseManifest{}, fmt.Errorf("missing build artifact %s: %w", artifactName(arch, rebuild), err)
			}
			var debPath, recordPath, buildRecordPath string
			var metadataPath string
			for _, entry := range entries {
				if entry.IsDir() {
					return ReleaseManifest{}, fmt.Errorf("unexpected directory in build artifact %s", folder)
				}
				if strings.HasSuffix(entry.Name(), ".deb") {
					if debPath != "" {
						return ReleaseManifest{}, fmt.Errorf("multiple Debian packages in %s", folder)
					}
					debPath = filepath.Join(folder, entry.Name())
				} else if entry.Name() == "qualification.json" {
					recordPath = filepath.Join(folder, entry.Name())
				} else if entry.Name() == "build-record.json" {
					buildRecordPath = filepath.Join(folder, entry.Name())
				} else if entry.Name() == "package-metadata.json" {
					metadataPath = filepath.Join(folder, entry.Name())
				} else {
					return ReleaseManifest{}, fmt.Errorf("unexpected build artifact file %q", entry.Name())
				}
			}
			if debPath == "" || recordPath == "" || buildRecordPath == "" || metadataPath == "" {
				return ReleaseManifest{}, fmt.Errorf("build artifact %s lacks a package, metadata, build record, or qualification record", folder)
			}
			var record PackageRecord
			data, err := readRegularFile(recordPath)
			if err != nil {
				return ReleaseManifest{}, err
			}
			if err := json.Unmarshal(data, &record); err != nil {
				return ReleaseManifest{}, fmt.Errorf("decode qualification record: %w", err)
			}
			buildRecordData, err := readRegularFile(buildRecordPath)
			if err != nil {
				return ReleaseManifest{}, err
			}
			var buildRecord PackageRecord
			if err := json.Unmarshal(buildRecordData, &buildRecord); err != nil {
				return ReleaseManifest{}, fmt.Errorf("decode package build record: %w", err)
			}
			expectedName := fmt.Sprintf("%s_%s_%s.deb", cfg.Package.Name, options.Plan.Version, arch)
			metadataBytes, err := readRegularFile(metadataPath)
			if err != nil {
				return ReleaseManifest{}, err
			}
			var metadata map[string]string
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				return ReleaseManifest{}, err
			}
			if !equalStringMap(record.ControlFields, metadata) || !equalStringMap(buildRecord.ControlFields, metadata) {
				return ReleaseManifest{}, fmt.Errorf("package metadata record differs from metadata sidecar for %s", arch)
			}
			if buildRecord.BuildNumber != rebuild || buildRecord.Filename != expectedName || buildRecord.Package != cfg.Package.Name || buildRecord.Version != options.Plan.Version || buildRecord.Architecture != arch || buildRecord.PackageQualification != "pending" || buildRecord.InstallQualification != "pending" || buildRecord.UpgradeQualification != "pending" ||
				record.BuildNumber != buildRecord.BuildNumber || record.Filename != buildRecord.Filename || record.Package != buildRecord.Package || record.Version != buildRecord.Version || record.Architecture != buildRecord.Architecture || record.Size != buildRecord.Size || record.SHA256 != buildRecord.SHA256 || record.RuntimeQualification != buildRecord.RuntimeQualification {
				return ReleaseManifest{}, fmt.Errorf("qualification record differs from original package build record for %s", arch)
			}
			if record.BuildNumber != rebuild || record.Filename != expectedName || filepath.Base(debPath) != expectedName || record.Package != cfg.Package.Name || record.Version != options.Plan.Version || record.Architecture != arch {
				return ReleaseManifest{}, fmt.Errorf("build record does not match release identity for %s", arch)
			}
			actualHash, size, err := fileDigest(debPath)
			if err != nil {
				return ReleaseManifest{}, err
			}
			if err := VerifyPackage(cfg, options.Plan, arch, debPath, options.TPAVersion, options.TPACommit, record.ControlFields); err != nil {
				return ReleaseManifest{}, err
			}
			if size > MaxGitHubReleaseAssetSize {
				return ReleaseManifest{}, fmt.Errorf("package %s exceeds GitHub's 2 GiB release asset limit", expectedName)
			}
			if actualHash != record.SHA256 || size != record.Size || actualHash != buildRecord.SHA256 || size != buildRecord.Size {
				return ReleaseManifest{}, fmt.Errorf("build artifact record hash/size mismatch for %s build %d", arch, rebuild)
			}
			rebuildHashes = append(rebuildHashes, actualHash)
			if rebuild == 1 {
				first = record
				if err := copyRegularFile(debPath, filepath.Join(options.OutputDir, expectedName)); err != nil {
					return ReleaseManifest{}, err
				}
			} else {
				if record.RuntimeQualification != first.RuntimeQualification || record.PackageQualification != first.PackageQualification || record.InstallQualification != first.InstallQualification || record.UpgradeQualification != first.UpgradeQualification || !equalStringMap(record.ControlFields, first.ControlFields) {
					return ReleaseManifest{}, fmt.Errorf("independent builds have inconsistent qualification or metadata for %s", arch)
				}
				if actualHash != first.SHA256 {
					return ReleaseManifest{}, fmt.Errorf("reproducibility failure for %s: build 1 %s differs from build %d %s", arch, first.SHA256, rebuild, actualHash)
				}
			}
			if !options.Plan.Reproducibility || rebuild == 2 {
				break
			}
		}
		artifacts = append(artifacts, ReleaseArtifact{Filename: first.Filename, Package: first.Package, Version: first.Version,
			Architecture: first.Architecture, Size: first.Size, SHA256: first.SHA256,
			RuntimeQualification: first.RuntimeQualification, PackageQualification: first.PackageQualification,
			InstallQualification: first.InstallQualification, UpgradeQualification: first.UpgradeQualification, ControlFields: first.ControlFields})
	}

	sourceName := fmt.Sprintf("%s_%s_source.tar.gz", cfg.Project.Name, options.Plan.Version)
	sourcePath := filepath.Join(options.OutputDir, sourceName)
	if err := writeSourceArchive(options.Workspace, options.Plan.SourceCommit, options.Plan.SourceDateEpoch, cfg.Project.Name, options.Plan.Version, sourcePath); err != nil {
		return ReleaseManifest{}, err
	}
	sourceHash, sourceSize, err := fileDigest(sourcePath)
	if err != nil {
		return ReleaseManifest{}, err
	}
	if sourceSize > MaxGitHubReleaseAssetSize {
		return ReleaseManifest{}, fmt.Errorf("source archive exceeds GitHub's 2 GiB release asset limit")
	}
	source := SourceArchive{Filename: sourceName, Size: sourceSize, SHA256: sourceHash}
	notesRelative := filepath.Clean(filepath.FromSlash(cfg.Release.NotesFile))
	notes, err := readGitFile(options.Workspace, options.Plan.SourceCommit, notesRelative)
	if err != nil {
		return ReleaseManifest{}, fmt.Errorf("read committed release notes: %w", err)
	}
	notesHash := sha256.Sum256(notes)
	if hex.EncodeToString(notesHash[:]) != options.Plan.ReleaseNotesSHA256 {
		return ReleaseManifest{}, fmt.Errorf("release notes differ from the validated source plan")
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "release-notes.md"), notes, 0644); err != nil {
		return ReleaseManifest{}, err
	}

	checksums := make([]struct{ name, digest string }, 0, len(artifacts)+1)
	for _, artifact := range artifacts {
		checksums = append(checksums, struct{ name, digest string }{artifact.Filename, artifact.SHA256})
	}
	checksums = append(checksums, struct{ name, digest string }{source.Filename, source.SHA256})
	sort.Slice(checksums, func(i, j int) bool { return checksums[i].name < checksums[j].name })
	var sumText strings.Builder
	for _, item := range checksums {
		fmt.Fprintf(&sumText, "%s  %s\n", item.digest, item.name)
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "SHA256SUMS.txt"), []byte(sumText.String()), 0644); err != nil {
		return ReleaseManifest{}, err
	}
	githubChecksums := make([]struct{ name, digest string }, 0, len(checksums))
	githubNames := make(map[string]bool, len(checksums))
	for _, item := range checksums {
		name := githubReleaseAssetName(item.name)
		if githubNames[name] {
			return ReleaseManifest{}, fmt.Errorf("release assets collide as GitHub filename %q", name)
		}
		githubNames[name] = true
		githubChecksums = append(githubChecksums, struct{ name, digest string }{name, item.digest})
	}
	sort.Slice(githubChecksums, func(i, j int) bool { return githubChecksums[i].name < githubChecksums[j].name })
	var githubSumText strings.Builder
	for _, item := range githubChecksums {
		fmt.Fprintf(&githubSumText, "%s  %s\n", item.digest, item.name)
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "SHA256SUMS-GITHUB.txt"), []byte(githubSumText.String()), 0644); err != nil {
		return ReleaseManifest{}, err
	}

	buildIdentity := BuildIdentity{WorkflowRef: options.WorkflowRef, WorkflowSHA: options.WorkflowSHA, RunID: options.RunID,
		RunAttempt: options.RunAttempt, GoVersion: options.GoVersion, TPAVersion: options.TPAVersion, TPACommit: options.TPACommit,
		BuildCommand: cfg.Build.Command, TestCommand: cfg.Project.TestCommand, ProjectTests: "passed", BuildFlags: append([]string(nil), cfg.Build.Flags...), SourceDateEpoch: options.Plan.SourceDateEpoch,
		Reproducible: options.Plan.Reproducibility, RebuildHashes: rebuildHashes}
	manifest := ReleaseManifest{SchemaVersion: ReleaseManifestSchemaVersion, Project: cfg.Project.Name, Package: cfg.Package.Name,
		Version: options.Plan.Version, Tag: options.Plan.Tag, SourceRepo: options.SourceRepository, SourceCommit: options.Plan.SourceCommit,
		ConfigSHA256: options.Plan.ConfigSHA256, SourceInputs: options.Plan.SourceInputs, SourceArchive: source,
		ReleaseNotesSHA256: hex.EncodeToString(notesHash[:]), Build: buildIdentity, Artifacts: artifacts}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ReleaseManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "release-manifest.json"), append(manifestData, '\n'), 0644); err != nil {
		return ReleaseManifest{}, err
	}
	provenance := Provenance{SchemaVersion: 1, PredicateType: "urn:tpa:release-provenance:v1", Project: manifest.Project, Package: manifest.Package,
		Version: manifest.Version, Tag: manifest.Tag, SourceRepo: manifest.SourceRepo, SourceCommit: manifest.SourceCommit,
		ConfigSHA256: manifest.ConfigSHA256, SourceInputs: manifest.SourceInputs, SourceArchive: source,
		ReleaseNotesSHA256: manifest.ReleaseNotesSHA256, Builder: buildIdentity, Artifacts: artifacts}
	provenanceData, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return ReleaseManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "provenance.json"), append(provenanceData, '\n'), 0644); err != nil {
		return ReleaseManifest{}, err
	}
	if err := VerifyBundle(cfg, options.Plan, options.OutputDir, options.TPAVersion, options.TPACommit); err != nil {
		return ReleaseManifest{}, err
	}
	complete = true
	return manifest, nil
}

func VerifyBundle(cfg Config, plan Plan, bundleDir, builderVersion, builderCommit string) error {
	bundleInfo, err := os.Lstat(bundleDir)
	if err != nil || !bundleInfo.IsDir() || bundleInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("release bundle is not a real directory")
	}
	manifestPath := filepath.Join(bundleDir, "release-manifest.json")
	data, err := readRegularFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest ReleaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	if manifest.SchemaVersion != ReleaseManifestSchemaVersion || len(manifest.ReleaseNotesSHA256) != 64 || !isLowerHex(manifest.ReleaseNotesSHA256) || manifest.Project != cfg.Project.Name || manifest.Package != cfg.Package.Name || manifest.Version != plan.Version || manifest.Tag != plan.Tag || manifest.SourceCommit != plan.SourceCommit || manifest.ConfigSHA256 != plan.ConfigSHA256 || !equalStringMap(manifest.SourceInputs, plan.SourceInputs) || manifest.ReleaseNotesSHA256 != plan.ReleaseNotesSHA256 || manifest.Build.ProjectTests != "passed" || manifest.Build.SourceDateEpoch != plan.SourceDateEpoch || manifest.Build.Reproducible != plan.Reproducibility || manifest.Build.TPAVersion != builderVersion || manifest.Build.TPACommit != builderCommit || manifest.Build.BuildCommand != cfg.Build.Command || manifest.Build.TestCommand != cfg.Project.TestCommand || !equalStringSlice(manifest.Build.BuildFlags, cfg.Build.Flags) {
		return fmt.Errorf("release manifest identity does not match the qualified source plan")
	}
	if len(manifest.Artifacts) != len(plan.Architectures) {
		return fmt.Errorf("release manifest architecture set is incomplete")
	}
	seen := make(map[string]bool)
	artifactsByArch := make(map[string]ReleaseArtifact, len(manifest.Artifacts))
	checksumExpected := make(map[string]string)
	for _, artifact := range manifest.Artifacts {
		if seen[artifact.Architecture] {
			return fmt.Errorf("duplicate architecture in release manifest: %s", artifact.Architecture)
		}
		seen[artifact.Architecture] = true
		artifactsByArch[artifact.Architecture] = artifact
		if _, ok := cfg.Package.Architectures[artifact.Architecture]; !ok {
			return fmt.Errorf("unexpected architecture in release manifest: %s", artifact.Architecture)
		}
		if artifact.Package != cfg.Package.Name || artifact.Version != plan.Version || artifact.Filename != fmt.Sprintf("%s_%s_%s.deb", cfg.Package.Name, plan.Version, artifact.Architecture) {
			return fmt.Errorf("invalid release artifact identity %q", artifact.Filename)
		}
		spec := cfg.Package.Architectures[artifact.Architecture]
		installState, upgradeState := "not-runtime-qualified", "not-configured"
		if spec.Runtime == "native" || spec.Runtime == "emulated" {
			installState = "passed"
		}
		if spec.Runtime == "build-only" {
			upgradeState = "not-applicable"
		}
		if commandConfigured(cfg.Package.Qualification.UpgradeCommand) {
			upgradeState = "passed"
		}
		packageState := "not-configured"
		if commandConfigured(cfg.Package.Qualification.Command) {
			packageState = "passed"
		}
		if artifact.RuntimeQualification != spec.Runtime || artifact.PackageQualification != packageState || artifact.InstallQualification != installState || artifact.UpgradeQualification != upgradeState {
			return fmt.Errorf("qualification record does not match configuration for %s", artifact.Architecture)
		}
		if artifact.Size > MaxGitHubReleaseAssetSize {
			return fmt.Errorf("release artifact %s exceeds GitHub's 2 GiB size limit", artifact.Filename)
		}
		path := filepath.Join(bundleDir, artifact.Filename)
		digest, size, err := fileDigest(path)
		if err != nil {
			return err
		}
		if err := VerifyPackage(cfg, plan, artifact.Architecture, path, builderVersion, builderCommit, artifact.ControlFields); err != nil {
			return err
		}
		if digest != artifact.SHA256 || size != artifact.Size {
			return fmt.Errorf("release artifact hash/size mismatch: %s", artifact.Filename)
		}
		checksumExpected[artifact.Filename] = digest
	}
	expectedRebuildHashes := make([]string, 0, len(plan.Architectures))
	for _, arch := range plan.Architectures {
		if !seen[arch] {
			return fmt.Errorf("release manifest is missing architecture %s", arch)
		}
		expectedRebuildHashes = append(expectedRebuildHashes, artifactsByArch[arch].SHA256)
		if plan.Reproducibility {
			expectedRebuildHashes = append(expectedRebuildHashes, artifactsByArch[arch].SHA256)
		}
	}
	if !equalStringSlice(manifest.Build.RebuildHashes, expectedRebuildHashes) {
		return fmt.Errorf("release build hashes do not match the qualified package set")
	}
	if manifest.SourceArchive.Size > MaxGitHubReleaseAssetSize {
		return fmt.Errorf("source archive exceeds GitHub's 2 GiB size limit")
	}
	sourcePath := filepath.Join(bundleDir, manifest.SourceArchive.Filename)
	sourceHash, sourceSize, err := fileDigest(sourcePath)
	if err != nil {
		return err
	}
	if sourceHash != manifest.SourceArchive.SHA256 || sourceSize != manifest.SourceArchive.Size || manifest.SourceArchive.Filename != fmt.Sprintf("%s_%s_source.tar.gz", cfg.Project.Name, plan.Version) {
		return fmt.Errorf("source archive metadata mismatch")
	}
	checksumExpected[manifest.SourceArchive.Filename] = sourceHash
	notesBytes, err := readRegularFile(filepath.Join(bundleDir, "release-notes.md"))
	if err != nil {
		return fmt.Errorf("release notes are unavailable: %w", err)
	}
	notesHash := sha256.Sum256(notesBytes)
	if hex.EncodeToString(notesHash[:]) != manifest.ReleaseNotesSHA256 {
		return fmt.Errorf("release notes differ from the source manifest")
	}
	if err := verifyChecksums(filepath.Join(bundleDir, "SHA256SUMS.txt"), checksumExpected); err != nil {
		return err
	}
	githubChecksumExpected := make(map[string]string, len(checksumExpected))
	for name, digest := range checksumExpected {
		githubName := githubReleaseAssetName(name)
		if _, duplicate := githubChecksumExpected[githubName]; duplicate {
			return fmt.Errorf("release assets collide as GitHub checksum filename %q", githubName)
		}
		githubChecksumExpected[githubName] = digest
	}
	if err := verifyChecksums(filepath.Join(bundleDir, "SHA256SUMS-GITHUB.txt"), githubChecksumExpected); err != nil {
		return fmt.Errorf("verify GitHub release checksums: %w", err)
	}
	var provenance Provenance
	provenanceBytes, err := readRegularFile(filepath.Join(bundleDir, "provenance.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(provenanceBytes, &provenance); err != nil {
		return fmt.Errorf("decode provenance: %w", err)
	}
	if provenance.SchemaVersion != 1 || provenance.PredicateType != "urn:tpa:release-provenance:v1" || provenance.Project != manifest.Project || provenance.Package != manifest.Package || provenance.Version != manifest.Version || provenance.Tag != manifest.Tag || provenance.SourceRepo != manifest.SourceRepo || provenance.SourceCommit != manifest.SourceCommit || provenance.ConfigSHA256 != manifest.ConfigSHA256 || !equalStringMap(provenance.SourceInputs, manifest.SourceInputs) || provenance.SourceArchive.SHA256 != manifest.SourceArchive.SHA256 || provenance.ReleaseNotesSHA256 != manifest.ReleaseNotesSHA256 || !sameBuildIdentity(provenance.Builder, manifest.Build) || len(provenance.Artifacts) != len(manifest.Artifacts) {
		return fmt.Errorf("provenance does not describe the release manifest")
	}
	for i := range manifest.Artifacts {
		if !sameArtifact(manifest.Artifacts[i], provenance.Artifacts[i]) {
			return fmt.Errorf("provenance artifact set differs from release manifest")
		}
	}
	allowed := map[string]bool{"SHA256SUMS.txt": true, "SHA256SUMS-GITHUB.txt": true, "release-manifest.json": true, "provenance.json": true, manifest.SourceArchive.Filename: true}
	for _, artifact := range manifest.Artifacts {
		allowed[artifact.Filename] = true
	}
	if _, err := os.Lstat(filepath.Join(bundleDir, "release-notes.md")); err == nil {
		allowed["release-notes.md"] = true
	}
	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !allowed[entry.Name()] {
			return fmt.Errorf("unexpected release bundle entry %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release bundle entry %q is not a regular file", entry.Name())
		}
	}
	return nil
}

func verifyChecksums(path string, expected map[string]string) error {
	data, err := readRegularFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != len(expected) {
		return fmt.Errorf("SHA256SUMS.txt contains %d entries, expected %d", len(lines), len(expected))
	}
	seen := make(map[string]bool)
	for _, line := range lines {
		if len(line) < 67 || line[64:66] != "  " {
			return fmt.Errorf("invalid sha256sum line %q", line)
		}
		digest, name := line[:64], line[66:]
		if len(digest) != 64 || !isLowerHex(digest) || filepath.Base(name) != name || seen[name] {
			return fmt.Errorf("invalid or duplicate checksum entry %q", name)
		}
		if expected[name] != digest {
			return fmt.Errorf("checksum entry differs from release manifest: %s", name)
		}
		seen[name] = true
	}
	return nil
}

func validateBuildArtifacts(root string, plan Plan) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("build artifacts root is not a real directory")
	}
	builds := 1
	if plan.Reproducibility {
		builds = 2
	}
	expected := make(map[string]bool, len(plan.Architectures)*builds)
	for _, arch := range plan.Architectures {
		for n := 1; n <= builds; n++ {
			expected[artifactName(arch, n)] = true
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("build artifact set has %d entries, expected %d", len(entries), len(expected))
	}
	for _, entry := range entries {
		if !expected[entry.Name()] || !entry.IsDir() {
			return fmt.Errorf("unexpected build artifact entry %q", entry.Name())
		}
	}
	return nil
}

func sameBuildIdentity(a, b BuildIdentity) bool {
	return a.WorkflowRef == b.WorkflowRef && a.WorkflowSHA == b.WorkflowSHA && a.RunID == b.RunID && a.RunAttempt == b.RunAttempt &&
		a.GoVersion == b.GoVersion && a.TPAVersion == b.TPAVersion && a.TPACommit == b.TPACommit && a.BuildCommand == b.BuildCommand &&
		a.TestCommand == b.TestCommand && a.ProjectTests == b.ProjectTests && a.SourceDateEpoch == b.SourceDateEpoch && a.Reproducible == b.Reproducible &&
		equalStringSlice(a.BuildFlags, b.BuildFlags) && equalStringSlice(a.RebuildHashes, b.RebuildHashes)
}

func sameBuildIdentityStable(a, b BuildIdentity) bool {
	a.RunID, b.RunID = "", ""
	a.RunAttempt, b.RunAttempt = "", ""
	return sameBuildIdentity(a, b)
}

func sameArtifact(a, b ReleaseArtifact) bool {
	return a.Filename == b.Filename && a.Package == b.Package && a.Version == b.Version && a.Architecture == b.Architecture &&
		a.Size == b.Size && a.SHA256 == b.SHA256 && a.RuntimeQualification == b.RuntimeQualification &&
		a.PackageQualification == b.PackageQualification && a.InstallQualification == b.InstallQualification &&
		a.UpgradeQualification == b.UpgradeQualification && equalStringMap(a.ControlFields, b.ControlFields)
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func artifactName(arch string, rebuild int) string {
	return fmt.Sprintf("tpa-release-%s-%d", arch, rebuild)
}

func copyRegularFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

type sizeLimitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *sizeLimitedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, fmt.Errorf("source archive exceeds GitHub's 2 GiB asset limit")
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	return n, err
}

func writeSourceArchive(workspace, commit string, epoch int64, project, version, output string) error {
	cmd := exec.Command("git", "archive", "--format=tar", "--prefix="+project+"_"+version+"/", commit)
	cmd.Dir = workspace
	tarPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	limited := &sizeLimitedWriter{writer: file, remaining: MaxGitHubReleaseAssetSize}
	gz := gzip.NewWriter(limited)
	gz.Name = ""
	gz.Comment = ""
	gz.ModTime = time.Unix(epoch, 0).UTC()
	gz.OS = 255
	_, copyErr := io.Copy(gz, tarPipe)
	if copyErr != nil {
		_ = cmd.Process.Kill()
	}
	gzipErr := gz.Close()
	closeErr := file.Close()
	waitErr := cmd.Wait()
	for _, candidate := range []error{copyErr, gzipErr, closeErr, waitErr} {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}
