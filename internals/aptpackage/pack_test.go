package aptpackage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackPreservesControlMetadataAndCreatesUnsignedRepository(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	longDescription := strings.Repeat("long & <description> ☃ ", 128)
	control := `Package: fixture-tool
Version: 1.2.3
Architecture: all
Maintainer: Test <test@example.invalid>
Description: repository fixture
 long description line
Section: utils
Priority: optional
Homepage: https://example.invalid/fixture
X-HTML-Test: <script>alert("owned")</script> & 'snowman ☃'
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
	control = strings.Replace(control, "Section: utils\n", " "+longDescription+"\nSection: utils\n", 1)
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

	browserPath := filepath.Join(out, "repository.json")
	browserData, err := os.ReadFile(browserPath)
	if err != nil {
		t.Fatalf("read root package JSON: %v", err)
	}
	var browser struct {
		Format   string `json:"format"`
		Version  int    `json:"version"`
		Packages []struct {
			Metadata map[string]string `json:"metadata"`
			Artifact struct {
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
				SHA256   string `json:"sha256"`
			} `json:"artifact"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(browserData, &browser); err != nil {
		t.Fatalf("decode root package JSON: %v", err)
	}
	if browser.Format != "tpa-repository-index" || browser.Version != 1 || len(browser.Packages) != 1 {
		t.Fatalf("unexpected root package index: %+v", browser)
	}
	metadata := browser.Packages[0].Metadata
	for key, want := range map[string]string{
		"Package": "fixture-tool", "Version": "1.2.3", "Architecture": "all",
		"Description": "repository fixture\nlong description line\n" + longDescription,
		"Depends":     "dep-one (= 1.0)",
		"Homepage":    "https://example.invalid/fixture",
		"X-HTML-Test": `<script>alert("owned")</script> & 'snowman ☃'`,
	} {
		if metadata[key] != want {
			t.Errorf("JSON metadata %s = %q, want %q", key, metadata[key], want)
		}
	}
	artifact := browser.Packages[0].Artifact
	if artifact.Filename != "pool/main/f/fixture-tool/fixture-tool_1.2.3_all.deb" || artifact.Size <= 0 || len(artifact.SHA256) != 64 {
		t.Errorf("JSON artifact metadata is incomplete: %+v", artifact)
	}
	artifactPath := filepath.Join(out, filepath.FromSlash(artifact.Filename))
	artifactInfo, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifactHash, err := getHash(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Size != artifactInfo.Size() || artifact.SHA256 != artifactHash {
		t.Errorf("JSON artifact metadata does not match published package: %+v", artifact)
	}
	if _, exists := metadata["Filename"]; exists {
		t.Errorf("artifact filename should be represented separately from package metadata")
	}
	htmlPath := filepath.Join(out, "index.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read root browser page: %v", err)
	}
	htmlText := string(html)
	if !strings.Contains(htmlText, `href="repository.json"`) || !strings.Contains(htmlText, `href="pool/main/f/fixture-tool/fixture-tool_1.2.3_all.deb"`) {
		t.Errorf("root browser page is missing repository-relative links")
	}
	if strings.Contains(htmlText, `<script>alert("owned")</script>`) || !strings.Contains(htmlText, "&lt;script&gt;") || !strings.Contains(htmlText, "&lt;description&gt;") {
		t.Errorf("hostile package metadata was not safely HTML-escaped")
	}
	if strings.Contains(htmlText, "<script") {
		t.Errorf("static repository browser unexpectedly contains executable script")
	}
	firstJSON, firstHTML := append([]byte(nil), browserData...), append([]byte(nil), html...)
	staleIndex := filepath.Join(out, "dists", "old", "main", "binary-all", "Packages")
	if err := os.MkdirAll(filepath.Dir(staleIndex), 0o755); err != nil {
		t.Fatal(err)
	}
	staleStanza := "Package: stale\nVersion: 9.9\nArchitecture: all\nFilename: pool/main/s/stale/stale.deb\nSize: 1\nSHA256: " + strings.Repeat("0", 64) + "\n\n"
	if err := os.WriteFile(staleIndex, []byte(staleStanza), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeRepositoryBrowserFiles(out, cfg.Repo); err != nil {
		t.Fatalf("regenerate repository browser files: %v", err)
	}
	secondJSON, err := os.ReadFile(browserPath)
	if err != nil {
		t.Fatal(err)
	}
	secondHTML, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) || !bytes.Equal(firstHTML, secondHTML) {
		t.Fatal("browser files changed when regenerated from unchanged repository state")
	}
}

func TestRepositoryBrowserCoversVersionsAndArchitectures(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	identities := []PackageIdentity{
		{Package: "foo", Version: "1.0", Architecture: "amd64"},
		{Package: "foo", Version: "2.0", Architecture: "amd64"},
		{Package: "bar", Version: "1.0", Architecture: "arm64"},
		{Package: "common", Version: "1.0", Architecture: "all"},
	}
	for i, identity := range identities {
		buildTestDeb(t, input, "fixture-"+string(rune('a'+i))+".deb", basicControl(identity.Package, identity.Version, identity.Architecture), identity.Package)
	}
	out := filepath.Join(root, "repository")
	if err := Pack(Config{InDir: input, OutDir: out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "repository.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index repositoryBrowserIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Packages) != len(identities) {
		t.Fatalf("JSON lists %d packages, want %d", len(index.Packages), len(identities))
	}
	architectures := make(map[string]bool)
	html, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range index.Packages {
		identity := PackageIdentity{
			Package: entry.Metadata["Package"], Version: entry.Metadata["Version"],
			Architecture: entry.Metadata["Architecture"],
		}
		architectures[identity.Architecture] = true
		if !strings.Contains(string(html), identity.Package) || !strings.Contains(string(html), identity.Version) || !strings.Contains(string(html), identity.Architecture) {
			t.Errorf("HTML does not expose JSON package identity %s", identity)
		}
	}
	for _, architecture := range []string{"amd64", "arm64", "all"} {
		if !architectures[architecture] {
			t.Errorf("repository JSON omitted architecture %s", architecture)
		}
	}
}

func TestGzipFileIsDeterministicAcrossInputModificationTimes(t *testing.T) {
	requireDebTools(t)
	root := t.TempDir()
	path := filepath.Join(root, "Packages")
	if err := os.WriteFile(path, []byte("Package: stable\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, firstTime, firstTime); err != nil {
		t.Fatal(err)
	}
	if err := gzipFile(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".gz"); err != nil {
		t.Fatal(err)
	}
	secondTime := firstTime.Add(24 * time.Hour)
	if err := os.Chtimes(path, secondTime, secondTime); err != nil {
		t.Fatal(err)
	}
	if err := gzipFile(path); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("gzip output changed with input modification time")
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

func TestVerifyRepositoryIgnoresConvenienceIndex(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "original")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo")}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.OutDir, "repository.json"), []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepository(cfg); err != nil {
		t.Fatalf("convenience JSON unexpectedly affected repository verification: %v", err)
	}
}

func TestSafeRepositoryArtifactLinkRejectsTraversal(t *testing.T) {
	for _, filename := range []string{"../escape.deb", "pool/../escape.deb", "/pool/package.deb", "pool\\package.deb", "dists/stable/Release"} {
		if link, err := safeRepositoryArtifactLink(filename); err == nil {
			t.Errorf("unsafe filename %q produced link %q", filename, link)
		}
	}
	link, err := safeRepositoryArtifactLink("pool/main/pkg/file%3Fname.deb")
	if err != nil {
		t.Fatal(err)
	}
	if link != "pool/main/pkg/file%253Fname.deb" {
		t.Errorf("URL-significant filename was not escaped: %q", link)
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
