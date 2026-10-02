package aptpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GenerationManifest describes the exact regular-file inventory of a completed
// APT repository generation. The manifest is kept outside the generation tree
// to avoid a self-referential hash.
type GenerationManifest struct {
	Version          int              `json:"version"`
	RepositoryID     string           `json:"repository_id"`
	GenerationID     string           `json:"generation_id"`
	ParentGeneration string           `json:"parent_generation"`
	Files            []GenerationFile `json:"files"`
}

type GenerationFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var generationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

const (
	maxGenerationManifestBytes = 16 << 20
	maxGenerationFiles         = 65536
	maxGenerationPathBytes     = 4096
	maxGenerationPathDepth     = 64
	maxGenerationDirectories   = 65536
)

// CreateGenerationManifest inventories a repository tree in stable path order.
// The caller should first build and verify the APT repository with TPA.
func CreateGenerationManifest(root, repositoryID, generationID, parentGeneration string) (GenerationManifest, error) {
	manifestStage := startStage(stageManifestWalk)
	defer manifestStage()
	if strings.TrimSpace(repositoryID) == "" || len(repositoryID) > 256 || strings.ContainsAny(repositoryID, "\r\n\x00") {
		return GenerationManifest{}, fmt.Errorf("invalid repository identity")
	}
	if !generationIDPattern.MatchString(generationID) {
		return GenerationManifest{}, fmt.Errorf("invalid generation identity")
	}
	if parentGeneration != "" && !generationIDPattern.MatchString(parentGeneration) {
		return GenerationManifest{}, fmt.Errorf("invalid parent generation identity")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return GenerationManifest{}, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return GenerationManifest{}, fmt.Errorf("generation root must be a directory")
	}
	manifest := GenerationManifest{Version: 1, RepositoryID: repositoryID, GenerationID: generationID, ParentGeneration: parentGeneration, Files: []GenerationFile{}}
	lastPath := ""
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generation contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("generation contains non-regular file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err = validateGenerationRelativePath(rel); err != nil {
			return err
		}
		if rel <= lastPath {
			return fmt.Errorf("generation walk did not produce sorted unique paths")
		}
		lastPath = rel
		if len(manifest.Files) >= maxGenerationFiles {
			return fmt.Errorf("generation exceeds %d files", maxGenerationFiles)
		}
		sum, size, err := hashGenerationFile(path)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, GenerationFile{Path: rel, Size: size, SHA256: sum})
		return nil
	})
	if err != nil {
		return GenerationManifest{}, err
	}
	if len(manifest.Files) == 0 {
		return GenerationManifest{}, fmt.Errorf("generation contains no files")
	}
	if err = validateGenerationManifestLimits(manifest); err != nil {
		return GenerationManifest{}, err
	}
	return manifest, nil
}

// VerifyGenerationManifest rejects inventory mismatches, unsafe paths, links,
// and special files. It intentionally does not replace APT semantic/signature
// verification; callers must verify the repository separately.
func VerifyGenerationManifest(root string, manifest GenerationManifest, repositoryID string) error {
	manifestStage := startStage(stageManifestWalk)
	defer manifestStage()
	if manifest.Version != 1 {
		return fmt.Errorf("unsupported generation manifest version %d", manifest.Version)
	}
	if manifest.RepositoryID != repositoryID {
		return fmt.Errorf("generation manifest repository identity mismatch")
	}
	if !generationIDPattern.MatchString(manifest.GenerationID) || (manifest.ParentGeneration != "" && !generationIDPattern.MatchString(manifest.ParentGeneration)) {
		return fmt.Errorf("invalid generation identity")
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("generation manifest contains no files")
	}
	if err := validateGenerationManifestLimits(manifest); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("generation root must be a directory")
	}
	declared := make(map[string]GenerationFile, len(manifest.Files))
	last := ""
	for _, file := range manifest.Files {
		if err := validateGenerationRelativePath(file.Path); err != nil {
			return err
		}
		if file.Path <= last {
			return fmt.Errorf("generation manifest paths must be sorted and unique")
		}
		last = file.Path
		if file.Size < 0 || len(file.SHA256) != sha256.Size*2 {
			return fmt.Errorf("invalid generation file metadata for %s", file.Path)
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil || strings.ToLower(file.SHA256) != file.SHA256 {
			return fmt.Errorf("invalid generation SHA256 for %s", file.Path)
		}
		declared[file.Path] = file
	}
	seen := make(map[string]bool, len(declared))
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generation contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("generation contains non-regular file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want, ok := declared[rel]
		if !ok {
			return fmt.Errorf("undeclared generation file %s", rel)
		}
		if seen[rel] {
			return fmt.Errorf("duplicate generation file %s", rel)
		}
		sum, size, err := hashGenerationFile(path)
		if err != nil {
			return err
		}
		if size != want.Size || sum != want.SHA256 {
			return fmt.Errorf("generation file size or SHA256 mismatch for %s", rel)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(declared) {
		for path := range declared {
			if !seen[path] {
				return fmt.Errorf("missing generation file %s", path)
			}
		}
	}
	return nil
}

func WriteGenerationManifest(path string, manifest GenerationManifest) error {
	if err := validateGenerationManifestLimits(manifest); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".tpa-generation-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o644); err == nil {
		err = writeGenerationManifestJSON(&generationManifestLimitWriter{writer: tmp, remaining: maxGenerationManifestBytes}, manifest, true)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func validateGenerationRelativePath(path string) error {
	if path == "" || len(path) > maxGenerationPathBytes || strings.Count(path, "/")+1 > maxGenerationPathDepth || strings.Contains(path, "\\") || strings.ContainsRune(path, '\x00') || strings.HasPrefix(path, "/") {
		return fmt.Errorf("invalid generation path %q", path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid generation path %q", path)
	}
	return nil
}

func validateGenerationManifestLimits(manifest GenerationManifest) error {
	if len(manifest.Files) == 0 {
		return fmt.Errorf("generation manifest contains no files")
	}
	if len(manifest.Files) > maxGenerationFiles {
		return fmt.Errorf("generation exceeds %d files", maxGenerationFiles)
	}
	directories := make(map[string]struct{})
	for _, file := range manifest.Files {
		if err := validateGenerationRelativePath(file.Path); err != nil {
			return err
		}
		for index := strings.IndexByte(file.Path, '/'); index >= 0; {
			directories[file.Path[:index]] = struct{}{}
			remaining := file.Path[index+1:]
			next := strings.IndexByte(remaining, '/')
			if next < 0 {
				break
			}
			index += next + 1
		}
		if len(directories) > maxGenerationDirectories {
			return fmt.Errorf("generation exceeds %d directories", maxGenerationDirectories)
		}
	}
	return nil
}

type generationManifestLimitWriter struct {
	writer    io.Writer
	remaining int
}

func (writer *generationManifestLimitWriter) Write(data []byte) (int, error) {
	if len(data) > writer.remaining {
		return 0, fmt.Errorf("generation manifest exceeds %d bytes", maxGenerationManifestBytes)
	}
	n, err := writer.writer.Write(data)
	writer.remaining -= n
	return n, err
}

func writeGenerationManifestJSON(writer io.Writer, manifest GenerationManifest, trailingNewline bool) error {
	write := func(text string) error { return writeGenerationManifestBytes(writer, []byte(text)) }
	writeJSONString := func(value string) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return writeGenerationManifestBytes(writer, encoded)
	}
	if err := write(`{
  "version": `); err != nil {
		return err
	}
	if err := write(strconv.Itoa(manifest.Version)); err != nil {
		return err
	}
	if err := write(`,
  "repository_id": `); err != nil {
		return err
	}
	if err := writeJSONString(manifest.RepositoryID); err != nil {
		return err
	}
	if err := write(`,
  "generation_id": `); err != nil {
		return err
	}
	if err := writeJSONString(manifest.GenerationID); err != nil {
		return err
	}
	if err := write(`,
  "parent_generation": `); err != nil {
		return err
	}
	if err := writeJSONString(manifest.ParentGeneration); err != nil {
		return err
	}
	if err := write(`,
  "files": [
`); err != nil {
		return err
	}
	for index, file := range manifest.Files {
		if index > 0 {
			if err := write(",\n"); err != nil {
				return err
			}
		}
		if err := write(`    {
      "path": `); err != nil {
			return err
		}
		if err := writeJSONString(file.Path); err != nil {
			return err
		}
		if err := write(`,
      "size": `); err != nil {
			return err
		}
		if err := write(strconv.FormatInt(file.Size, 10)); err != nil {
			return err
		}
		if err := write(`,
      "sha256": `); err != nil {
			return err
		}
		if err := writeJSONString(file.SHA256); err != nil {
			return err
		}
		if err := write(`
    }`); err != nil {
			return err
		}
	}
	if err := write(`
  ]
}`); err != nil {
		return err
	}
	if trailingNewline {
		return write("\n")
	}
	return nil
}

func writeGenerationManifestBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func hashGenerationFile(path string) (string, int64, error) {
	hashStage := startStage(stageManifestHash)
	defer hashStage()
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

// GenerationManifestJSON returns the deterministic JSON representation used by
// upload clients and tests.
func GenerationManifestJSON(manifest GenerationManifest) ([]byte, error) {
	if err := validateGenerationManifestLimits(manifest); err != nil {
		return nil, err
	}
	var data bytes.Buffer
	writer := &generationManifestLimitWriter{writer: &data, remaining: maxGenerationManifestBytes}
	if err := writeGenerationManifestJSON(writer, manifest, false); err != nil {
		return nil, err
	}
	if data.Len()+1 > maxGenerationManifestBytes {
		return nil, fmt.Errorf("generation manifest exceeds %d bytes", maxGenerationManifestBytes)
	}
	return data.Bytes(), nil
}
