package repositoryweb_test

import (
	"fmt"
	"github.com/eugen252009/tpa/pkg/repositoryweb"
	"strings"
	"testing"
)

func TestBoundedEscapedStaticPreview(t *testing.T) {
	packages := make([]repositoryweb.Package, 2000)
	for i := range packages {
		packages[i] = repositoryweb.Package{Package: fmt.Sprintf("fixture-%04d", i), Version: "1.0", Architecture: "all", Filename: fmt.Sprintf("pool/fixture-%04d.deb", i), Size: 123, SHA256: "sha256", Control: map[string]string{"Description": "<script>bad()</script> & long text", "Custom": "\" onclick=\"bad"}}
	}
	html, err := repositoryweb.Render(packages, repositoryweb.Options{InlineAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	if strings.Count(s, `class="package-card static-card"`) != 50 || strings.Contains(s, "fixture-0050") || !strings.Contains(s, "2000 package records") {
		t.Fatal("unbounded or incomplete static preview")
	}
	// A promoted field named Package shadows the string field in templates.
	// RV-STATIC-IDENTITY: title is the exact name, never the embedded record.
	if !strings.Contains(s, "<h2>fixture-0000</h2>") || strings.Contains(s, "<h2>{") {
		t.Fatal("static title leaked the full package record")
	}
	if strings.Contains(s, "<script>bad()") || !strings.Contains(s, "&lt;script&gt;bad()&lt;/script&gt;") || strings.Count(s, "<script>") != 1 {
		t.Fatal("metadata escaping or inline first-party asset")
	}
	if strings.Contains(s, `src="assets/`) || strings.Contains(s, `href="assets/`) {
		t.Fatal("canonical output introduced protocol asset paths")
	}
	if !strings.Contains(s, "Package details") || !strings.Contains(s, "pool/fixture-0000.deb") || !strings.Contains(s, "SHA-256") {
		t.Fatal("static fallback loses details/downloads")
	}
	external, err := repositoryweb.Render(nil, repositoryweb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(external), `src="assets/app.js"`) || !strings.Contains(string(external), "No packages are currently listed") {
		t.Fatal("independent utility output/empty repository")
	}
}
func TestSharedSafeArtifactLinksAndBases(t *testing.T) {
	for _, v := range []string{"", "../pool/a", "pool/../a", "/pool/a", "pool//a", "pool/a\\b", "pool/a\x00b"} {
		if _, err := repositoryweb.ArtifactLink(v); err == nil {
			t.Fatalf("unsafe artifact accepted: %q", v)
		}
	}
	v, err := repositoryweb.ArtifactLink("pool/a?b#c%.deb")
	if err != nil || v != "pool/a%3Fb%23c%25.deb" {
		t.Fatal(v, err)
	}
	for _, v := range []string{"javascript:bad", "https://user:pass@example.invalid/", "//example.invalid/", "https://example.invalid/?secret=1", "https://example.invalid/#frag", "\\evil"} {
		if repositoryweb.ValidateBase(v) == nil {
			t.Fatalf("unsafe base %q", v)
		}
	}
}
