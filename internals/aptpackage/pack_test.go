package aptpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackPreservesControlMetadataAndCreatesUnsignedRepository(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	control := `Package: fixture-tool
Version: 1.2.3
Architecture: all
Maintainer: Test <test@example.invalid>
Description: repository fixture
 long description line
Section: utils
Priority: optional
Homepage: https://example.invalid/fixture
Depends: dep-one (= 1.0)
Pre-Depends: pre-one
Recommends: recommended-one
Suggests: suggested-one
Provides: fixture-virtual (= 1.2.3)
Conflicts: conflicting-one
Breaks: broken-one
Replaces: replaced-one
Multi-Arch: foreign
Built-Using: fixture-source (= 1.2.3)
`
	buildTestDeb(t, input, "fixture-tool_1.2.3_all.deb", control, "fixture")

	out := filepath.Join(root, "repo")
	cfg := Config{InDir: input, OutDir: out, Repo: RepoConfig{
		Origin: "Fixture", Label: "Fixture", Suite: "stable",
		Codename: "bookworm", Components: "main", Description: "fixture repo",
	}}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}

	packagesPath := filepath.Join(out, "dists", "bookworm", "main", "binary-all", "Packages")
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, field := range []string{
		"Package: fixture-tool\n",
		"Version: 1.2.3\n",
		"Architecture: all\n",
		"Maintainer: Test <test@example.invalid>\n",
		"Description: repository fixture\n long description line\n",
		"Section: utils\n",
		"Priority: optional\n",
		"Homepage: https://example.invalid/fixture\n",
		"Depends: dep-one (= 1.0)\n",
		"Pre-Depends: pre-one\n",
		"Recommends: recommended-one\n",
		"Suggests: suggested-one\n",
		"Provides: fixture-virtual (= 1.2.3)\n",
		"Conflicts: conflicting-one\n",
		"Breaks: broken-one\n",
		"Replaces: replaced-one\n",
		"Multi-Arch: foreign\n",
		"Built-Using: fixture-source (= 1.2.3)\n",
		"Filename: pool/main/f/fixture-tool/fixture-tool_1.2.3_all.deb\n",
		"Size: ",
		"SHA256: ",
	} {
		if !strings.Contains(text, field) {
			t.Errorf("Packages is missing %q:\n%s", field, text)
		}
	}
	for _, name := range []string{"Filename", "Size", "SHA256"} {
		if count := strings.Count(text, name+": "); count != 1 {
			t.Errorf("Packages contains %d %s fields, want 1:\n%s", count, name, text)
		}
	}
	for _, path := range []string{
		packagesPath + ".gz",
		filepath.Join(out, "dists", "bookworm", "Release"),
		filepath.Join(out, "pool", "main", "f", "fixture-tool", "fixture-tool_1.2.3_all.deb"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing repository artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "dists", "bookworm", "InRelease")); !os.IsNotExist(err) {
		t.Errorf("unsigned repository unexpectedly has InRelease: %v", err)
	}
}

func TestParseControlUsesHyphenatedDebianFieldNames(t *testing.T) {
	control, err := ParseControl([]byte(basicControl("fixture", "1.0", "all") +
		"Pre-Depends: pre-base\nBuilt-Using: source (= 1.0)\nMulti-Arch: foreign\n"))
	if err != nil {
		t.Fatal(err)
	}
	if control.PreDepends != "pre-base" || control.BuiltUsing != "source (= 1.0)" || control.MultiArch != "foreign" {
		t.Fatalf("hyphenated fields were not parsed: %+v", control)
	}
}

func TestPackAcceptsByteIdenticalDuplicateIdentityOnce(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	first := buildTestDeb(t, input, "fixture-a.deb", basicControl("fixture", "1.0", "all"), "same")
	second := filepath.Join(input, "fixture-b.deb")
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "repo")
	if err := Pack(Config{InDir: input, OutDir: out}); err != nil {
		t.Fatal(err)
	}
	packages, err := os.ReadFile(filepath.Join(out, "dists", "stable", "main", "binary-all", "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(packages), "Package: fixture\n"); count != 1 {
		t.Fatalf("got %d entries for identical retry, want 1:\n%s", count, packages)
	}
}

func TestPackRejectsConflictingDuplicateIdentity(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	control := basicControl("fixture", "1.0", "all")
	buildTestDeb(t, input, "fixture-a.deb", control, "first")
	buildTestDeb(t, input, "fixture-b.deb", control, "second")

	err := Pack(Config{InDir: input, OutDir: filepath.Join(root, "repo")})
	if err == nil || !strings.Contains(err.Error(), "conflicting package identity fixture 1.0 all") {
		t.Fatalf("expected conflicting identity error, got %v", err)
	}
}

func TestVerifyRepositoryRejectsTamperedPackage(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "original")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo")}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(cfg.OutDir, "pool", "main", "f", "fixture", "fixture_1.0_all.deb")
	if err := os.WriteFile(artifact, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "artifact") {
		t.Fatalf("expected package verification failure, got %v", err)
	}
}

func TestVerifyRepositoryRejectsTamperedIndex(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "original")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo")}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	packages := filepath.Join(cfg.OutDir, "dists", "stable", "main", "binary-all", "Packages")
	file, err := os.OpenFile(packages, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("tampered\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "Release entry") {
		t.Fatalf("expected Release verification failure, got %v", err)
	}
}

func TestVerifyRepositoryRejectsTamperedCompressedIndex(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "original")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo")}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	packagesGzip := filepath.Join(cfg.OutDir, "dists", "stable", "main", "binary-all", "Packages.gz")
	file, err := os.OpenFile(packagesGzip, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("tampered"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "Packages.gz") {
		t.Fatalf("expected compressed index verification failure, got %v", err)
	}
}
