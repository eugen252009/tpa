package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerationManifestIsDeterministicAndCoversTree(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string]string{
		"dists/stable/Release":   "release",
		"dists/stable/InRelease": "signed release",
		"pool/main/p/pkg.deb":    "package bytes",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := CreateGenerationManifest(root, "repo-id", "gen-1", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateGenerationManifest(root, "repo-id", "gen-1", "")
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := GenerationManifestJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := GenerationManifestJSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("manifest encoding is not deterministic:\n%s\n%s", firstJSON, secondJSON)
	}
	if err := VerifyGenerationManifest(root, first, "repo-id"); err != nil {
		t.Fatal(err)
	}
	if got := len(first.Files); got != 3 {
		t.Fatalf("manifest file count=%d, want 3", got)
	}
}

func TestVerifyGenerationManifestRejectsInventoryMismatch(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "dists", "stable", "Release")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateGenerationManifest(root, "repo-id", "gen-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerationManifest(root, manifest, "repo-id"); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("expected undeclared-file rejection, got %v", err)
	}
	_ = os.Remove(filepath.Join(root, "extra"))
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerationManifest(root, manifest, "repo-id"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-file rejection, got %v", err)
	}
}

func TestVerifyGenerationManifestRejectsInvalidPathsIdentityAndHash(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := CreateGenerationManifest(root, "repo-id", "gen-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerationManifest(root, manifest, "other-repo"); err == nil {
		t.Fatal("wrong repository identity accepted")
	}
	cases := []struct {
		name string
		edit func(*GenerationManifest)
	}{
		{"traversal", func(m *GenerationManifest) { m.Files[0].Path = "../escape" }},
		{"absolute", func(m *GenerationManifest) { m.Files[0].Path = "/etc/passwd" }},
		{"long-path", func(m *GenerationManifest) { m.Files[0].Path = strings.Repeat("a", maxGenerationPathBytes+1) }},
		{"deep-path", func(m *GenerationManifest) { m.Files[0].Path = strings.Repeat("a/", maxGenerationPathDepth) + "a" }},
		{"too-many-files", func(m *GenerationManifest) { m.Files = append(m.Files, make([]GenerationFile, maxGenerationFiles)...) }},
		{"too-many-directories", func(m *GenerationManifest) {
			m.Files = nil
			for branch := 0; branch < 1042; branch++ {
				path := fmt.Sprintf("branch-%04d", branch)
				for depth := 0; depth < 62; depth++ {
					path += fmt.Sprintf("/d-%04d-%02d", branch, depth)
				}
				m.Files = append(m.Files, GenerationFile{Path: path + "/payload"})
			}
		}},
		{"hash", func(m *GenerationManifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) }},
		{"version", func(m *GenerationManifest) { m.Version = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := manifest
			copy.Files = append([]GenerationFile(nil), manifest.Files...)
			tc.edit(&copy)
			if err := VerifyGenerationManifest(root, copy, "repo-id"); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestGenerationManifestRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateGenerationManifest(root, "repo-id", "gen-1", ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}
