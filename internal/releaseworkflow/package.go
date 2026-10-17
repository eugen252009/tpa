package releaseworkflow

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type PackageBuild struct {
	Workspace    string
	StageRoot    string
	OutputDir    string
	Architecture string
	TPABinary    string
	TPACommit    string
	BuildNumber  int
}

type PackageRecord struct {
	BuildNumber          int               `json:"build_number"`
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

type tpaJSONConfig struct {
	Control    map[string]any    `json:"control"`
	Scripts    map[string]string `json:"scripts,omitempty"`
	Provenance bool              `json:"provenance"`
	Repo       map[string]string `json:"repo"`
	InDir      string            `json:"indir"`
	OutDir     string            `json:"outdir"`
	GPG        string            `json:"gpg"`
}

func BuildPackage(cfg Config, plan Plan, build PackageBuild) (PackageRecord, error) {
	workspace, err := filepath.Abs(build.Workspace)
	if err != nil {
		return PackageRecord{}, err
	}
	if err := ValidatePlanInputs(cfg, workspace, plan); err != nil {
		return PackageRecord{}, err
	}
	if err := ValidateWorkspaceChanges(cfg, workspace); err != nil {
		return PackageRecord{}, err
	}
	if _, ok := cfg.Package.Architectures[build.Architecture]; !ok {
		return PackageRecord{}, fmt.Errorf("unsupported architecture %q", build.Architecture)
	}
	stageRoot, err := cfg.PackageStageRoot(workspace, build.Architecture)
	if err != nil {
		return PackageRecord{}, err
	}
	if build.StageRoot != "" && filepath.Clean(build.StageRoot) != filepath.Clean(stageRoot) {
		return PackageRecord{}, fmt.Errorf("stage root does not match the validated project configuration")
	}
	stageInfo, err := os.Lstat(stageRoot)
	if err != nil || !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
		return PackageRecord{}, fmt.Errorf("project build did not create a real package stage directory %s", stageRoot)
	}
	if _, err := os.Lstat(filepath.Join(stageRoot, "DEBIAN")); err == nil {
		return PackageRecord{}, fmt.Errorf("project build must not provide a DEBIAN directory; TPA owns package metadata and maintainer scripts")
	} else if !os.IsNotExist(err) {
		return PackageRecord{}, err
	}
	if build.TPABinary == "" {
		return PackageRecord{}, fmt.Errorf("TPA package builder path is required")
	}
	if _, err := os.Stat(build.TPABinary); err != nil {
		return PackageRecord{}, fmt.Errorf("TPA package builder is unavailable: %w", err)
	}
	if len(build.TPACommit) != 40 || !isLowerHex(build.TPACommit) {
		return PackageRecord{}, fmt.Errorf("TPA builder commit must be a full lowercase 40-character SHA-1")
	}
	if build.BuildNumber < 1 {
		return PackageRecord{}, fmt.Errorf("build number must be positive")
	}

	builderVersion, err := commandOutput(build.TPABinary, "version")
	if err != nil || strings.TrimSpace(builderVersion) == "" {
		return PackageRecord{}, fmt.Errorf("read TPA package-builder version: %w", err)
	}
	control := map[string]any{
		"name": cfg.Package.Name, "version": plan.Version, "architecture": build.Architecture,
		"maintainer": cfg.Package.Maintainer, "description": cfg.Package.Description,
		"depends": cfg.Package.Depends, "recommends": cfg.Package.Recommends, "preDepends": cfg.Package.PreDepends,
		"suggests": cfg.Package.Suggests, "breaks": cfg.Package.Breaks, "conflicts": cfg.Package.Conflicts,
		"replaces": cfg.Package.Replaces, "provides": cfg.Package.Provides, "builtUsing": cfg.Package.BuiltUsing,
		"essential": cfg.Package.Essential, "multiArch": cfg.Package.MultiArch,
		"homepage": cfg.Package.Homepage, "section": cfg.Package.Section, "priority": cfg.Package.Priority,
	}
	for key, value := range cfg.Package.ControlFields {
		expanded, expandErr := expandValue(value, plan, build.Architecture, strings.TrimSpace(builderVersion), build.TPACommit)
		if expandErr != nil {
			return PackageRecord{}, fmt.Errorf("control field %s: %w", key, expandErr)
		}
		control[key] = expanded
	}
	if cfg.Package.MetadataFile != "" {
		metadataPath, pathErr := safeWorkspacePath(workspace, cfg.Package.MetadataFile)
		if pathErr != nil {
			return PackageRecord{}, pathErr
		}
		metadata, loadErr := readMetadataFile(metadataPath)
		if loadErr != nil {
			return PackageRecord{}, fmt.Errorf("project control metadata: %w", loadErr)
		}
		for key, value := range metadata {
			if !fieldNameRE.MatchString(key) || !isScalar(value) || isReservedControlField(key) {
				return PackageRecord{}, fmt.Errorf("invalid or reserved project control metadata field %q", key)
			}
			expanded, expandErr := expandValue(value, plan, build.Architecture, strings.TrimSpace(builderVersion), build.TPACommit)
			if expandErr != nil {
				return PackageRecord{}, fmt.Errorf("control field %s: %w", key, expandErr)
			}
			if err := mergeControlField(control, key, expanded); err != nil {
				return PackageRecord{}, err
			}
		}
	}
	for name, value := range map[string]any{
		"TPA-Source-Commit":     plan.SourceCommit,
		"TPA-Release-Tag":       releaseIdentityTag(plan),
		"TPA-Source-Date-Epoch": plan.SourceDateEpoch,
		"TPA-Builder-Commit":    build.TPACommit,
		"Created-At":            time.Unix(plan.SourceDateEpoch, 0).UTC().Format(time.RFC3339),
	} {
		if err := mergeControlField(control, name, value); err != nil {
			return PackageRecord{}, err
		}
	}
	if !hasControlField(control, "TPA-Version") {
		control["TPA-Version"] = strings.TrimSpace(builderVersion)
	}

	scripts := make(map[string]string, len(cfg.Package.Scripts))
	for name, relative := range cfg.Package.Scripts {
		body, readErr := readGitFile(workspace, plan.SourceCommit, relative)
		if readErr != nil {
			return PackageRecord{}, fmt.Errorf("read maintainer script %s: %w", name, readErr)
		}
		scripts[name] = string(body)
	}
	jsonConfig := tpaJSONConfig{Control: control, Scripts: scripts, Provenance: false,
		Repo: map[string]string{}, InDir: stageRoot, OutDir: stageRoot, GPG: ""}
	encoded, err := json.Marshal(jsonConfig)
	if err != nil {
		return PackageRecord{}, err
	}
	if err := runCommand(build.TPABinary, []string{"json"}, workspace, encoded); err != nil {
		return PackageRecord{}, fmt.Errorf("TPA package initialization failed: %w", err)
	}
	for _, name := range []string{"preinst", "postinst", "prerm", "postrm"} {
		if _, configured := scripts[name]; !configured {
			if err := os.Remove(filepath.Join(stageRoot, "DEBIAN", name)); err != nil && !os.IsNotExist(err) {
				return PackageRecord{}, err
			}
		}
	}
	if len(cfg.Package.Conffiles) > 0 {
		conffilesPath := filepath.Join(stageRoot, "DEBIAN", "conffiles")
		var contents strings.Builder
		for _, filename := range cfg.Package.Conffiles {
			fmt.Fprintf(&contents, "/%s\n", filename)
		}
		if err := os.WriteFile(conffilesPath, []byte(contents.String()), 0644); err != nil {
			return PackageRecord{}, err
		}
	}
	build.OutputDir, err = filepath.Abs(build.OutputDir)
	if err != nil {
		return PackageRecord{}, err
	}
	if err := os.MkdirAll(build.OutputDir, 0755); err != nil {
		return PackageRecord{}, err
	}
	outputInfo, err := os.Lstat(build.OutputDir)
	if err != nil || !outputInfo.IsDir() || outputInfo.Mode()&os.ModeSymlink != 0 {
		return PackageRecord{}, fmt.Errorf("package output must be a real directory")
	}
	outputEntries, err := os.ReadDir(build.OutputDir)
	if err != nil {
		return PackageRecord{}, err
	}
	if len(outputEntries) != 0 {
		return PackageRecord{}, fmt.Errorf("package output directory must be empty")
	}
	filename := fmt.Sprintf("%s_%s_%s.deb", cfg.Package.Name, plan.Version, build.Architecture)
	output := filepath.Join(build.OutputDir, filename)
	if _, err := os.Lstat(output); err == nil {
		return PackageRecord{}, fmt.Errorf("package output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return PackageRecord{}, err
	}
	if err := runCommandWithEnv(build.TPABinary, []string{"build", "-in=" + stageRoot, "-out=" + output}, workspace, nil,
		map[string]string{"SOURCE_DATE_EPOCH": fmt.Sprint(plan.SourceDateEpoch), "DEB_HOST_ARCH": build.Architecture}); err != nil {
		return PackageRecord{}, fmt.Errorf("TPA package build failed: %w", err)
	}
	if err := VerifyPackage(cfg, plan, build.Architecture, output, strings.TrimSpace(builderVersion), build.TPACommit, nil); err != nil {
		return PackageRecord{}, err
	}
	customFields, err := readCustomControlFields(output, control)
	if err != nil {
		return PackageRecord{}, err
	}
	metadataBytes, err := json.MarshalIndent(customFields, "", "  ")
	if err != nil {
		return PackageRecord{}, err
	}
	if err := writeNewFile(filepath.Join(build.OutputDir, "package-metadata.json"), append(metadataBytes, '\n'), 0644); err != nil {
		return PackageRecord{}, err
	}
	digest, size, err := fileDigest(output)
	if err != nil {
		return PackageRecord{}, err
	}
	record := PackageRecord{BuildNumber: build.BuildNumber, Filename: filename, Package: cfg.Package.Name, Version: plan.Version, Architecture: build.Architecture,
		Size: size, SHA256: digest, RuntimeQualification: cfg.Package.Architectures[build.Architecture].Runtime,
		PackageQualification: "pending", InstallQualification: "pending", UpgradeQualification: "pending", ControlFields: customFields}
	recordBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return PackageRecord{}, err
	}
	if err := writeNewFile(filepath.Join(build.OutputDir, "build-record.json"), append(recordBytes, '\n'), 0644); err != nil {
		return PackageRecord{}, err
	}
	return record, nil
}

func VerifyPackage(cfg Config, plan Plan, architecture, path, builderVersion, builderCommit string, expectedControlFields map[string]string) error {
	if _, ok := cfg.Package.Architectures[architecture]; !ok {
		return fmt.Errorf("unsupported package architecture %q", architecture)
	}
	if _, _, err := fileDigest(path); err != nil {
		return err
	}
	fields := map[string]string{
		"Package": cfg.Package.Name, "Version": plan.Version, "Architecture": architecture,
		"Maintainer": cfg.Package.Maintainer, "Description": cfg.Package.Description,
	}
	if cfg.Package.Homepage != "" {
		fields["Homepage"] = cfg.Package.Homepage
	}
	if cfg.Package.Depends != "" {
		fields["Depends"] = cfg.Package.Depends
	}
	if cfg.Package.Recommends != "" {
		fields["Recommends"] = cfg.Package.Recommends
	}
	if cfg.Package.PreDepends != "" {
		fields["Pre-Depends"] = cfg.Package.PreDepends
	}
	if cfg.Package.Suggests != "" {
		fields["Suggests"] = cfg.Package.Suggests
	}
	if cfg.Package.Breaks != "" {
		fields["Breaks"] = cfg.Package.Breaks
	}
	if cfg.Package.Conflicts != "" {
		fields["Conflicts"] = cfg.Package.Conflicts
	}
	if cfg.Package.Replaces != "" {
		fields["Replaces"] = cfg.Package.Replaces
	}
	if cfg.Package.Provides != "" {
		fields["Provides"] = cfg.Package.Provides
	}
	if cfg.Package.BuiltUsing != "" {
		fields["Built-Using"] = cfg.Package.BuiltUsing
	}
	if cfg.Package.Essential != "" {
		fields["Essential"] = cfg.Package.Essential
	}
	if cfg.Package.MultiArch != "" {
		fields["Multi-Arch"] = cfg.Package.MultiArch
	}
	if cfg.Package.Section != "" {
		fields["Section"] = cfg.Package.Section
	}
	if cfg.Package.Priority != "" {
		fields["Priority"] = cfg.Package.Priority
	}
	for field, want := range fields {
		got, err := commandOutput("dpkg-deb", "-f", path, field)
		if err != nil || got != want {
			return fmt.Errorf("%s: Debian field %s is %q, expected %q (dpkg-deb error: %v)", filepath.Base(path), field, got, want, err)
		}
	}
	wantControl := map[string]string{
		"TPA-Source-Commit":     plan.SourceCommit,
		"TPA-Release-Tag":       releaseIdentityTag(plan),
		"TPA-Source-Date-Epoch": fmt.Sprint(plan.SourceDateEpoch),
		"TPA-Builder-Commit":    builderCommit,
		"Created-At":            time.Unix(plan.SourceDateEpoch, 0).UTC().Format(time.RFC3339),
	}
	for field, want := range wantControl {
		if want == "" {
			continue
		}
		got, err := commandOutput("dpkg-deb", "-f", path, field)
		if err != nil || got != want {
			return fmt.Errorf("%s: provenance field %s is %q, expected %q", filepath.Base(path), field, got, want)
		}
	}
	if configured, ok := controlField(cfg.Package.ControlFields, "TPA-Version"); ok {
		expected, err := expandValue(configured, plan, architecture, builderVersion, builderCommit)
		if err != nil {
			return err
		}
		got, err := commandOutput("dpkg-deb", "-f", path, "TPA-Version")
		if err != nil || fmt.Sprint(expected) != got {
			return fmt.Errorf("%s: TPA-Version is %q, expected %q", filepath.Base(path), got, expected)
		}
	} else {
		got, err := commandOutput("dpkg-deb", "-f", path, "TPA-Version")
		if err != nil || got != builderVersion {
			return fmt.Errorf("%s: TPA-Version is %q, expected builder version %q", filepath.Base(path), got, builderVersion)
		}
	}
	for field, expected := range expectedControlFields {
		got, err := commandOutput("dpkg-deb", "-f", path, debianFieldName(field))
		if err != nil || got != expected {
			return fmt.Errorf("%s: control field %s is %q, expected %q", filepath.Base(path), field, got, expected)
		}
	}
	for field, value := range cfg.Package.ControlFields {
		if normalizeField(field) == normalizeField("TPA-Version") {
			continue
		}
		expanded, err := expandValue(value, plan, architecture, builderVersion, builderCommit)
		if err != nil {
			return err
		}
		got, err := commandOutput("dpkg-deb", "-f", path, debianFieldName(field))
		if err != nil || fmt.Sprint(expanded) != got {
			return fmt.Errorf("%s: configured control field %s is %q, expected %q", filepath.Base(path), field, got, expanded)
		}
	}
	for _, value := range cfg.Package.RequiredFiles {
		if err := validatePackagePath(value); err != nil {
			return err
		}
	}
	if len(cfg.Package.RequiredFiles) > 0 {
		if err := verifyRequiredFiles(path, cfg.Package.RequiredFiles); err != nil {
			return err
		}
	}
	return nil
}

func verifyRequiredFiles(packagePath string, required []string) error {
	wanted := make(map[string]bool, len(required))
	for _, name := range required {
		wanted[name] = false
	}
	cmd := exec.Command("dpkg-deb", "--fsys-tarfile", packagePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	limited := &io.LimitedReader{R: stdout, N: 8 << 30}
	tarReader := tar.NewReader(limited)
	seen := make(map[string]bool, len(wanted))
	var scanErr error
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			scanErr = nextErr
			break
		}
		name := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if _, ok := wanted[name]; !ok {
			continue
		}
		if seen[name] {
			scanErr = fmt.Errorf("duplicate package payload path %s", name)
			break
		}
		seen[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			scanErr = fmt.Errorf("required package path %s is not a regular file or directory", name)
			break
		}
		wanted[name] = true
	}
	if scanErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if scanErr != nil {
		if limited.N == 0 {
			return fmt.Errorf("package payload exceeds the 8 GiB inspection budget")
		}
		return fmt.Errorf("inspect package payload: %w", scanErr)
	}
	if waitErr != nil {
		return fmt.Errorf("dpkg-deb --fsys-tarfile: %s: %w", strings.TrimSpace(stderr.String()), waitErr)
	}
	for name, found := range wanted {
		if !found {
			return fmt.Errorf("required package path %s is missing", name)
		}
	}
	return nil
}

func readCustomControlFields(packagePath string, control map[string]any) (map[string]string, error) {
	known := map[string]bool{"name": true, "version": true, "architecture": true, "maintainer": true, "description": true,
		"depends": true, "recommends": true, "predepends": true, "suggests": true, "breaks": true, "conflicts": true,
		"replaces": true, "provides": true, "builtusing": true, "essential": true, "multiarch": true,
		"homepage": true, "section": true, "priority": true}
	fields := make(map[string]string)
	for name, value := range control {
		if known[normalizeField(name)] {
			continue
		}
		got, err := commandOutput("dpkg-deb", "-f", packagePath, debianFieldName(name))
		if err != nil {
			return nil, fmt.Errorf("read generated control field %s: %w", name, err)
		}
		if got != fmt.Sprint(value) {
			return nil, fmt.Errorf("generated control field %s is %q, expected %q", name, got, value)
		}
		fields[name] = got
	}
	return fields, nil
}

func releaseIdentityTag(plan Plan) string {
	if plan.Tag != "" {
		return plan.Tag
	}
	return "qualification"
}

func mergeControlField(control map[string]any, field string, value any) error {
	normalized := normalizeField(field)
	for oldField, oldValue := range control {
		if normalizeField(oldField) == normalized {
			if fmt.Sprint(oldValue) != fmt.Sprint(value) {
				return fmt.Errorf("conflicting values for Debian control field %s", field)
			}
			return nil
		}
	}
	control[field] = value
	return nil
}

func debianFieldName(value string) string {
	var words []string
	var word []rune
	runes := []rune(value)
	flush := func() {
		if len(word) == 0 {
			return
		}
		text := strings.ToLower(string(word))
		first := []rune(text)
		first[0] = unicode.ToUpper(first[0])
		words = append(words, string(first))
		word = nil
	}
	for i, current := range runes {
		if current == '-' {
			flush()
			continue
		}
		if len(word) > 0 && unicode.IsUpper(current) {
			previous := runes[i-1]
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.IsLower(previous) || (unicode.IsUpper(previous) && unicode.IsLower(next)) {
				flush()
			}
		}
		word = append(word, current)
	}
	flush()
	field := strings.Join(words, "-")
	if strings.EqualFold(field, "TPA-Version") {
		return "TPA-Version"
	}
	return field
}

func writeNewFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func normalizeField(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "-", ""))
}

func hasControlField(fields map[string]any, name string) bool {
	_, ok := controlField(fields, name)
	return ok
}

func controlField(fields map[string]any, name string) (any, bool) {
	for field, value := range fields {
		if normalizeField(field) == normalizeField(name) {
			return value, true
		}
	}
	return nil, false
}

func expandValue(value any, plan Plan, arch, tpaVersion, builderCommit string) (any, error) {
	text, ok := value.(string)
	if !ok {
		return value, nil
	}
	replacements := map[string]string{
		"${version}": plan.Version, "${tag}": releaseIdentityTag(plan), "${commit}": plan.SourceCommit,
		"${architecture}": arch, "${source_date_epoch}": fmt.Sprint(plan.SourceDateEpoch),
		"${tpa_version}": tpaVersion, "${builder_commit}": builderCommit,
	}
	for key, replacement := range replacements {
		text = strings.ReplaceAll(text, key, replacement)
	}
	if strings.Contains(text, "${") {
		return nil, fmt.Errorf("unsupported or unresolved release placeholder in %q", text)
	}
	return text, nil
}

func readMetadataFile(path string) (map[string]any, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("metadata file must be a regular file no larger than 1 MiB")
	}
	data, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	opening, err := dec.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, fmt.Errorf("metadata must be a JSON object")
	}
	out := make(map[string]any)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("metadata field name is not a string")
		}
		if _, duplicate := out[key]; duplicate {
			return nil, fmt.Errorf("duplicate metadata field %q", key)
		}
		var item any
		if err := dec.Decode(&item); err != nil {
			return nil, err
		}
		if !isScalar(item) {
			return nil, fmt.Errorf("metadata field %q must be a scalar", key)
		}
		if number, ok := item.(json.Number); ok {
			if integer, parseErr := number.Int64(); parseErr == nil {
				item = integer
			} else {
				floating, parseErr := number.Float64()
				if parseErr != nil {
					return nil, parseErr
				}
				item = floating
			}
		}
		out[key] = item
	}
	closing, err := dec.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, fmt.Errorf("metadata JSON object is incomplete")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("metadata file contains trailing JSON")
	}
	return out, nil
}

func commandOutput(name string, args ...string) (string, error) {
	return commandOutputLimited(name, 1<<20, args...)
}

func commandOutputLimited(name string, limit int64, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if int64(len(output)) > limit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", fmt.Errorf("%s output exceeds %d bytes", name, limit)
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		return strings.TrimSpace(string(output)), fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), message, waitErr)
	}
	return strings.TrimSpace(string(output)), nil
}

func runCommand(name string, args []string, dir string, stdin []byte) error {
	return runCommandWithEnv(name, args, dir, stdin, nil)
}

func runCommandWithEnv(name string, args []string, dir string, stdin []byte, values map[string]string) error {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if len(values) > 0 {
		cmd.Env = overlayEnvironment(os.Environ(), values)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func fileDigest(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("artifact %s is not a regular file", path)
	}
	if info.Size() > MaxGitHubReleaseAssetSize {
		return "", 0, fmt.Errorf("artifact %s exceeds GitHub's 2 GiB release asset limit", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
