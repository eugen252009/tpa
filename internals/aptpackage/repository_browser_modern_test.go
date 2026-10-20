package aptpackage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModernAndLegacyCanonicalSidecarsRemainStrict(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	buildTestDeb(t, input, "fixture.deb", basicControl("fixture", "1.0", "all"), "payload")
	out := filepath.Join(root, "repo")
	cfg := Config{InDir: input, OutDir: out}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	modernJSON, modernHTML, err := renderRepositoryBrowserFiles(out, cfg.Repo)
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, legacyHTML, err := renderRepositoryBrowserFilesStyle(out, cfg.Repo, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(modernJSON, legacyJSON) || bytes.Equal(modernHTML, legacyHTML) {
		t.Fatal("canonical inventory changed, or no UI integration")
	}
	previousJSON, previousHTML, err := renderRepositoryBrowserFilesStyle(out, cfg.Repo, false, true)
	if err != nil || !bytes.Equal(previousJSON, modernJSON) || bytes.Equal(previousHTML, modernHTML) {
		t.Fatal("previous qualified modern v1 rendering changed inventory", err)
	}
	for _, html := range [][]byte{modernHTML, legacyHTML, previousHTML} {
		if err := os.WriteFile(filepath.Join(out, "index.html"), html, 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyRepositoryBrowserFiles(out, cfg.Repo, true); err != nil {
			t.Fatal("known canonical rendering rejected", err)
		}
		if err := os.WriteFile(filepath.Join(out, "index.html"), append(append([]byte(nil), html...), []byte("<script>bad()</script>")...), 0644); err != nil {
			t.Fatal(err)
		}
		if err := verifyRepositoryBrowserFiles(out, cfg.Repo, true); err == nil {
			t.Fatal("tampered known rendering accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), append(legacyHTML, []byte("<script>bad()</script>")...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepositoryBrowserFiles(out, cfg.Repo, true); err == nil {
		t.Fatal("arbitrary/tampered HTML accepted as a legacy rendering")
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), modernHTML, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "repository.json"), bytes.Replace(modernJSON, []byte("fixture"), []byte("wrong"), 1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepositoryBrowserFiles(out, cfg.Repo, true); err == nil {
		t.Fatal("UI bypassed canonical APT inventory checks")
	}
}
func TestBrowserMetadataUsesSharedParagraphReaderWithoutV1Drift(t *testing.T) {
	fields, err := parseRepositoryBrowserMetadata([]byte("Package: fixture  \nDescription: first  \n second  \n .\n third\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fields["Package"] != "fixture" || fields["Description"] != "first\nsecond  \n.\nthird" {
		t.Fatal(fields)
	}
	for _, raw := range []string{"Package: one\npackage: two\n", "Description: a\rbroken\n", " first\n"} {
		if _, err := parseRepositoryBrowserMetadata([]byte(raw)); err == nil {
			t.Fatalf("malformed duplicate/control metadata accepted %q", raw)
		}
	}
	html, err := os.ReadFile(filepath.Join("..", "..", "pkg", "repositoryweb", "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "/_tpa/") {
		t.Fatal("experimental utility backend dependency integrated")
	}
}
