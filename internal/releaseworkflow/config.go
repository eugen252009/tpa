package releaseworkflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const ConfigSchemaVersion = 1

type Config struct {
	SchemaVersion   int           `yaml:"schema_version" json:"schema_version"`
	Project         ProjectConfig `yaml:"project" json:"project"`
	Build           BuildConfig   `yaml:"build" json:"build"`
	Package         PackageConfig `yaml:"package" json:"package"`
	Release         ReleaseConfig `yaml:"release" json:"release"`
	Reproducibility bool          `yaml:"reproducibility" json:"reproducibility"`
}

type ProjectConfig struct {
	Name               string       `yaml:"name" json:"name"`
	TestCommand        string       `yaml:"test_command" json:"test_command"`
	SourceVersionCheck VersionCheck `yaml:"source_version_check" json:"source_version_check"`
}

type VersionCheck struct {
	File    string `yaml:"file" json:"file"`
	Pattern string `yaml:"pattern" json:"pattern"`
}

type BuildConfig struct {
	Command string   `yaml:"command" json:"command"`
	Flags   []string `yaml:"flags" json:"flags"`
}

type PackageConfig struct {
	Name          string                        `yaml:"name" json:"name"`
	Description   string                        `yaml:"description" json:"description"`
	Maintainer    string                        `yaml:"maintainer" json:"maintainer"`
	Homepage      string                        `yaml:"homepage" json:"homepage"`
	Depends       string                        `yaml:"depends" json:"depends"`
	Recommends    string                        `yaml:"recommends" json:"recommends"`
	PreDepends    string                        `yaml:"pre_depends" json:"pre_depends"`
	Suggests      string                        `yaml:"suggests" json:"suggests"`
	Breaks        string                        `yaml:"breaks" json:"breaks"`
	Conflicts     string                        `yaml:"conflicts" json:"conflicts"`
	Replaces      string                        `yaml:"replaces" json:"replaces"`
	Provides      string                        `yaml:"provides" json:"provides"`
	BuiltUsing    string                        `yaml:"built_using" json:"built_using"`
	Essential     string                        `yaml:"essential" json:"essential"`
	MultiArch     string                        `yaml:"multi_arch" json:"multi_arch"`
	Section       string                        `yaml:"section" json:"section"`
	Priority      string                        `yaml:"priority" json:"priority"`
	StageRoot     string                        `yaml:"stage_root" json:"stage_root"`
	MetadataFile  string                        `yaml:"metadata_file" json:"metadata_file"`
	ControlFields map[string]any                `yaml:"control_fields" json:"control_fields"`
	Scripts       map[string]string             `yaml:"scripts" json:"scripts"`
	Conffiles     []string                      `yaml:"conffiles" json:"conffiles"`
	RequiredFiles []string                      `yaml:"required_files" json:"required_files"`
	Architectures map[string]ArchitectureConfig `yaml:"architectures" json:"architectures"`
	Qualification QualificationConfig           `yaml:"qualification" json:"qualification"`
}

type ArchitectureConfig struct {
	Runner  string `yaml:"runner" json:"runner"`
	Runtime string `yaml:"runtime" json:"runtime"`
}

type QualificationConfig struct {
	Command        string `yaml:"command" json:"command"`
	InstallCommand string `yaml:"install_command" json:"install_command"`
	UpgradeCommand string `yaml:"upgrade_command" json:"upgrade_command"`
}

type ReleaseConfig struct {
	NotesFile string `yaml:"notes_file" json:"notes_file"`
}

var (
	packageNameRE         = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	archNameRE            = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	fieldNameRE           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)
	packagePathRE         = regexp.MustCompile(`^[A-Za-z0-9._+@/-]+$`)
	reservedControlFields = map[string]bool{"package": true, "version": true, "architecture": true, "maintainer": true, "description": true,
		"depends": true, "homepage": true, "section": true, "priority": true, "predepends": true, "recommends": true,
		"suggests": true, "breaks": true, "conflicts": true, "replaces": true, "provides": true, "builtusing": true,
		"essential": true, "multiarch": true, "filename": true, "size": true, "sha256": true}
)

func LoadConfig(path, workspace string) (Config, string, error) {
	if filepath.IsAbs(path) {
		return Config{}, "", fmt.Errorf("config path must be relative to the project checkout")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return Config{}, "", fmt.Errorf("config path escapes the project checkout")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return Config{}, "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Config{}, "", err
	}
	full := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return Config{}, "", fmt.Errorf("resolve release config: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Config{}, "", fmt.Errorf("config path resolves outside the project checkout")
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return Config{}, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Config{}, "", fmt.Errorf("release config must be a regular file no larger than 1 MiB")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return Config{}, "", err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, "", fmt.Errorf("decode release config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, "", fmt.Errorf("release config must contain exactly one YAML document")
		}
		return Config{}, "", err
	}
	if err := cfg.Validate(root); err != nil {
		return Config{}, "", err
	}
	return cfg, resolved, nil
}

func (c Config) Validate(workspace string) error {
	if c.SchemaVersion != ConfigSchemaVersion {
		return fmt.Errorf("schema_version must be %d", ConfigSchemaVersion)
	}
	if !packageNameRE.MatchString(c.Project.Name) {
		return fmt.Errorf("project.name must be a valid Debian package-style identifier")
	}
	if strings.TrimSpace(c.Project.TestCommand) == "" {
		return fmt.Errorf("project.test_command is required")
	}
	if c.Project.SourceVersionCheck.File != "" {
		if c.Project.SourceVersionCheck.Pattern == "" {
			return fmt.Errorf("project.source_version_check.pattern is required when a version file is configured")
		}
		if _, err := safeWorkspacePath(workspace, c.Project.SourceVersionCheck.File); err != nil {
			return fmt.Errorf("project.source_version_check.file: %w", err)
		}
		pattern, err := regexp.Compile(c.Project.SourceVersionCheck.Pattern)
		if err != nil || len(pattern.SubexpNames()) != 2 {
			return fmt.Errorf("project.source_version_check.pattern must be a valid regexp with one capture group")
		}
	}
	if strings.TrimSpace(c.Build.Command) == "" {
		return fmt.Errorf("build.command is required")
	}
	if !packageNameRE.MatchString(c.Package.Name) {
		return fmt.Errorf("package.name must be a valid Debian package name")
	}
	if strings.TrimSpace(c.Package.Description) == "" || strings.ContainsAny(c.Package.Description, "\r\n") {
		return fmt.Errorf("package.description must be a non-empty single line")
	}
	if strings.TrimSpace(c.Package.Maintainer) == "" || strings.TrimSpace(c.Package.StageRoot) == "" {
		return fmt.Errorf("package.maintainer and package.stage_root are required")
	}
	if _, err := safeWorkspacePath(workspace, c.Package.StageRoot); err != nil {
		return fmt.Errorf("package.stage_root: %w", err)
	}
	if c.Package.MetadataFile != "" {
		if _, err := safeWorkspacePath(workspace, c.Package.MetadataFile); err != nil {
			return fmt.Errorf("package.metadata_file: %w", err)
		}
	}
	if len(c.Package.Architectures) == 0 || len(c.Package.Architectures) > 32 {
		return fmt.Errorf("package.architectures must contain between 1 and 32 Debian architectures")
	}
	for arch, spec := range c.Package.Architectures {
		if !archNameRE.MatchString(arch) {
			return fmt.Errorf("invalid package.architectures entry %q", arch)
		}
		if spec.Runner != "ubuntu-24.04" && spec.Runner != "ubuntu-24.04-arm" {
			return fmt.Errorf("architecture %s must use a supported GitHub-hosted Ubuntu runner", arch)
		}
		switch spec.Runtime {
		case "native", "emulated":
			if strings.TrimSpace(c.Package.Qualification.InstallCommand) == "" {
				return fmt.Errorf("package.architectures.%s requires qualification.install_command for runtime=%s", arch, spec.Runtime)
			}
			if spec.Runtime == "native" && !((spec.Runner == "ubuntu-24.04" && arch == "amd64") || (spec.Runner == "ubuntu-24.04-arm" && arch == "arm64")) {
				return fmt.Errorf("architecture %s is not native on runner %s", arch, spec.Runner)
			}
			if spec.Runtime == "emulated" && ((arch != "arm64" && arch != "riscv64") || (spec.Runner == "ubuntu-24.04-arm" && arch != "riscv64")) {
				return fmt.Errorf("architecture %s has no configured emulation path on runner %s", arch, spec.Runner)
			}
		case "build-only":
		default:
			return fmt.Errorf("package.architectures.%s.runtime must be native, emulated, or build-only", arch)
		}
	}
	if commandConfigured(c.Package.Qualification.UpgradeCommand) {
		for arch, spec := range c.Package.Architectures {
			if spec.Runtime == "build-only" {
				return fmt.Errorf("upgrade qualification is configured but architecture %s is build-only", arch)
			}
		}
	}
	metadataPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(c.Package.MetadataFile)))
	if metadataPath != "." {
		if metadataPath == filepath.ToSlash(filepath.Clean(filepath.FromSlash(c.Release.NotesFile))) ||
			metadataPath == filepath.ToSlash(filepath.Clean(filepath.FromSlash(c.Project.SourceVersionCheck.File))) {
			return fmt.Errorf("package.metadata_file must not overwrite release notes or the source version input")
		}
	}
	for name, value := range c.Package.Scripts {
		if name != "preinst" && name != "postinst" && name != "prerm" && name != "postrm" {
			return fmt.Errorf("unsupported maintainer script %q", name)
		}
		if _, err := safeWorkspacePath(workspace, value); err != nil {
			return fmt.Errorf("package.scripts.%s: %w", name, err)
		}
		if metadataPath != "." && metadataPath == filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))) {
			return fmt.Errorf("package.metadata_file must not overwrite maintainer script %q", name)
		}
	}
	for _, name := range c.Package.RequiredFiles {
		if err := validatePackagePath(name); err != nil {
			return fmt.Errorf("invalid required package path %q: %w", name, err)
		}
	}
	for _, name := range c.Package.Conffiles {
		if err := validatePackagePath(name); err != nil {
			return fmt.Errorf("invalid conffiles path %q: %w", name, err)
		}
	}
	seenControlFields := make(map[string]string, len(c.Package.ControlFields))
	for name, value := range c.Package.ControlFields {
		if !fieldNameRE.MatchString(name) {
			return fmt.Errorf("invalid additional Debian control field name %q", name)
		}
		normalized := normalizeField(name)
		if reservedControlFields[normalized] {
			return fmt.Errorf("additional control field %q is reserved", name)
		}
		if previous, exists := seenControlFields[normalized]; exists {
			return fmt.Errorf("additional control fields %q and %q normalize to the same name", previous, name)
		}
		seenControlFields[normalized] = name
		if !isScalar(value) {
			return fmt.Errorf("additional control field %q must be a string, number, or boolean", name)
		}
	}
	if strings.TrimSpace(c.Release.NotesFile) == "" {
		return fmt.Errorf("release.notes_file is required for a tagged release")
	}
	notesPath, err := safeWorkspacePath(workspace, c.Release.NotesFile)
	if err != nil {
		return fmt.Errorf("release.notes_file: %w", err)
	}
	notesInfo, err := os.Lstat(notesPath)
	if err != nil {
		return fmt.Errorf("release.notes_file: %w", err)
	}
	if !notesInfo.Mode().IsRegular() || notesInfo.Size() > 100_000 {
		return fmt.Errorf("release.notes_file must be a regular file no larger than 100000 bytes")
	}
	notes, err := readRegularFile(notesPath)
	if err != nil {
		return fmt.Errorf("release.notes_file: %w", err)
	}
	if len(notes) == 0 || len(notes) > 100_000 || strings.TrimSpace(string(notes)) == "" {
		return fmt.Errorf("release.notes_file must contain 1 to 100000 bytes of release notes")
	}
	return nil
}

func safeWorkspacePath(workspace, value string) (string, error) {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("path must be a non-empty relative project path")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the project checkout")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve project checkout: %w", err)
	}
	full := filepath.Join(root, clean)
	probe := full
	for {
		resolved, resolveErr := filepath.EvalSymlinks(probe)
		if resolveErr == nil {
			rel, relErr := filepath.Rel(root, resolved)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("path resolves outside the project checkout")
			}
			break
		}
		if !os.IsNotExist(resolveErr) {
			return "", fmt.Errorf("resolve project path: %w", resolveErr)
		}
		if info, lstatErr := os.Lstat(probe); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path contains a dangling or unresolved symlink")
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", resolveErr
		}
		probe = parent
	}
	return full, nil
}

func validatePackagePath(value string) error {
	if value == "" || filepath.IsAbs(value) || !packagePathRE.MatchString(value) {
		return fmt.Errorf("must be a canonical relative package path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("must not contain empty or traversal path segments")
		}
	}
	return nil
}

func commandConfigured(value string) bool { return strings.TrimSpace(value) != "" }

func CommandConfigured(value string) bool { return commandConfigured(value) }

func isReservedControlField(name string) bool { return reservedControlFields[normalizeField(name)] }

func isScalar(value any) bool {
	switch value.(type) {
	case string, bool, int, int64, uint64, float64, json.Number:
		return true
	default:
		return false
	}
}

func (c Config) SortedArchitectures() []string {
	architectures := make([]string, 0, len(c.Package.Architectures))
	for architecture := range c.Package.Architectures {
		architectures = append(architectures, architecture)
	}
	sort.Strings(architectures)
	return architectures
}

func (c Config) PackageStageRoot(workspace, architecture string) (string, error) {
	if _, ok := c.Package.Architectures[architecture]; !ok {
		return "", fmt.Errorf("unsupported architecture %q", architecture)
	}
	root, err := safeWorkspacePath(workspace, c.Package.StageRoot)
	if err != nil {
		return "", err
	}
	return root, nil
}
