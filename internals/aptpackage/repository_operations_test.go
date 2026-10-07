package aptpackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestInspectAndVerifyRepositoryLocalHTTPAndHTTPS(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "inspect-fixture_1.2_all.deb", basicControl("inspect-fixture", "1.2", "all")+"Depends: base-package\n", "inspect")
	repo := filepath.Join(root, "repo")
	cfg := Config{InDir: input, OutDir: repo, Repo: RepoConfig{Codename: "bookworm", Components: "main"}}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}

	locator, err := ParseRepositoryLocator(repo)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectRepository(context.Background(), reader, RepositoryOptions{Codename: "bookworm", Package: "inspect-fixture", Version: "1.2", Architecture: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if report.PackageCount != 1 || len(report.Packages) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Packages[0].Control["Depends"] != "base-package" {
		t.Fatalf("custom Debian metadata missing: %#v", report.Packages[0].Control)
	}
	if _, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Codename: "bookworm", Workers: 2}); err != nil {
		t.Fatal(err)
	}
	reader.Close()

	server := httptest.NewServer(http.FileServer(http.Dir(repo)))
	defer server.Close()
	remote, err := ParseRepositoryLocator(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpReader, err := OpenRepositoryReader(remote)
	if err != nil {
		t.Fatal(err)
	}
	defer httpReader.Close()
	if _, err := VerifyRepository(context.Background(), httpReader, RepositoryOptions{Codename: "bookworm", Workers: 2}); err != nil {
		t.Fatal(err)
	}

	httpsServer := httptest.NewTLSServer(http.FileServer(http.Dir(repo)))
	defer httpsServer.Close()
	httpsLocator, err := ParseRepositoryLocator(httpsServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpsReader, err := OpenRepositoryReader(httpsLocator)
	if err != nil {
		t.Fatal(err)
	}
	// Trust only this httptest server's ephemeral certificate while retaining
	// the production reader's timeout and HTTPS downgrade policy.
	httpsHTTPReader, ok := httpsReader.(*httpRepositoryReader)
	if !ok {
		t.Fatalf("HTTPS reader has type %T", httpsReader)
	}
	httpsHTTPReader.client.Transport = httpsServer.Client().Transport
	defer httpsReader.Close()
	httpsReport, err := InspectRepository(context.Background(), httpsReader, RepositoryOptions{Codename: "bookworm"})
	if err != nil || httpsReport.PackageCount != 1 {
		t.Fatalf("HTTPS inspect report=%+v error=%v", httpsReport, err)
	}
	if _, err := VerifyRepository(context.Background(), httpsReader, RepositoryOptions{Codename: "bookworm", Workers: 2}); err != nil {
		t.Fatal(err)
	}

	artifact := filepath.Join(repo, "pool", "main", "i", "inspect-fixture", "inspect-fixture_1.2_all.deb")
	if err := os.WriteFile(artifact, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRepository(context.Background(), httpReader, RepositoryOptions{Codename: "bookworm", Workers: 1}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected package artifact mismatch, got %v", err)
	}
}

func TestInspectAndVerifyUnsignedRepositoryOverSSH(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "ssh-fixture_1_all.deb", basicControl("ssh-fixture", "1", "all"), "ssh")
	repo := filepath.Join(root, "repo")
	if err := Pack(Config{InDir: input, OutDir: repo}); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeSSH := filepath.Join(bin, "ssh")
	script := `#!/bin/sh
set -eu
last=
for arg do last=$arg; done
case "$last" in
  "LC_ALL=C cat -- "*) eval "$last" ;;
  *) echo "unexpected SSH command: $last" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	locator, err := ParseRepositoryLocator("ssh://reader@example.invalid" + filepath.ToSlash(repo))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	inspected, err := InspectRepository(context.Background(), reader, RepositoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.PackageCount != 1 {
		t.Fatalf("SSH inspect package count=%d", inspected.PackageCount)
	}
	verified, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if verified.PackageCount != 1 {
		t.Fatalf("SSH verify package count=%d", verified.PackageCount)
	}
}

func TestInspectSupportsXZOnlyPackageIndexes(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "xz-fixture_1_all.deb", basicControl("xz-fixture", "1", "all"), "xz")
	repo := filepath.Join(root, "repo")
	if err := Pack(Config{InDir: input, OutDir: repo}); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(repo, "dists", "stable", "main", "binary-all", "Packages")
	plain, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	xzPath := indexPath + ".xz"
	if err := os.WriteFile(xzPath, compressed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexPath + ".gz"); err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(repo, "dists", "stable", "Release")
	release, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("SHA256:\n")
	position := bytes.Index(release, marker)
	if position < 0 {
		t.Fatal("Release has no SHA256 section")
	}
	lines := strings.Split(strings.TrimSuffix(string(release[position+len(marker):]), "\n"), "\n")
	remaining := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 3 && (parts[2] == "main/binary-all/Packages" || parts[2] == "main/binary-all/Packages.gz") {
			continue
		}
		remaining = append(remaining, line)
	}
	digest := sha256.Sum256(compressed.Bytes())
	xzLine := fmt.Sprintf(" %s %d main/binary-all/Packages.xz", hex.EncodeToString(digest[:]), compressed.Len())
	remaining = append(remaining, xzLine)
	updated := append(append([]byte(nil), release[:position+len(marker)]...), []byte(strings.Join(remaining, "\n")+"\n")...)
	if err := os.WriteFile(releasePath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	locator, _ := ParseRepositoryLocator(repo)
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	report, err := InspectRepository(context.Background(), reader, RepositoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.PackageCount != 1 || len(report.Packages) != 1 || !strings.HasSuffix(report.Indexes[0].Path, "/Packages") {
		t.Fatalf("unexpected xz index report: %+v", report)
	}
	if _, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Workers: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLocatorParsing(t *testing.T) {
	cases := []struct {
		input   string
		kind    RepositoryLocatorKind
		wantErr bool
	}{
		{"./repo", RepositoryLocal, false},
		{"/var/lib/apt/repo", RepositoryLocal, false},
		{"example.invalid/repo", RepositoryHTTPS, false},
		{"example.invalid:8443/repo", RepositoryHTTPS, false},
		{"[2001:db8::1]:8443/repo", RepositoryHTTPS, false},
		{"http://example.invalid/repo", RepositoryHTTP, false},
		{"https://example.invalid/repo", RepositoryHTTPS, false},
		{"ssh://user@example.invalid/srv/repo", RepositorySSH, false},
		{"sftp://example.invalid/repo", "", true},
		{"ssh://example.invalid/../repo", "", true},
		{"https://user:password@example.invalid/repo", "", true},
	}
	for _, test := range cases {
		got, err := ParseRepositoryLocator(test.input)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q accepted", test.input)
			}
			continue
		}
		if err != nil || got.Kind != test.kind {
			t.Errorf("ParseRepositoryLocator(%q) = %q, %v; want %q", test.input, got.Kind, err, test.kind)
		}
	}
}

func TestVerifyRepositoryUsesPublicKeyringOnly(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	root := t.TempDir()
	home := filepath.Join(root, "gnupg")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", home)
	fingerprint := generateTestSigningKey(t, "Public verifier <public@example.invalid>")
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "signed-fixture_1_all.deb", basicControl("signed-fixture", "1", "all"), "signed")
	repo := filepath.Join(root, "repo")
	cfg := Config{InDir: input, OutDir: repo, GPG: fingerprint}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	public, err := exec.Command("gpg", "--batch", "--export", fingerprint).Output()
	if err != nil {
		t.Fatal(err)
	}
	locator, _ := ParseRepositoryLocator(repo)
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	report, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Keyring: public, Fingerprint: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Signed || !report.SignatureVerified || !strings.EqualFold(report.Signer, fingerprint) {
		t.Fatalf("signature not reported as verified: %+v", report)
	}
	inspected, err := InspectRepository(context.Background(), reader, RepositoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !inspected.Signed || inspected.SignatureVerified {
		t.Fatalf("inspect should label signature unverified without keyring: %+v", inspected)
	}
	inReleasePath := filepath.Join(repo, "dists", "stable", "InRelease")
	if err := os.WriteFile(inReleasePath, append(mustReadFile(t, inReleasePath), []byte("unsigned tail\\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Keyring: public}); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("signed payload with trailing data was accepted: %v", err)
	}
	secret, err := exec.Command("gpg", "--batch", "--export-secret-keys", fingerprint).Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Keyring: secret}); err == nil || !strings.Contains(err.Error(), "public keys only") {
		t.Fatalf("secret keyring was accepted: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSSHRepositoryReaderUsesBoundedReadCommand(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nprintf 'ssh fixture\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	locator, err := ParseRepositoryLocator("ssh://reader@example.invalid/srv/apt")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	file, err := reader.Open(context.Background(), "dists/stable/Release")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != "ssh fixture\n" {
		t.Fatalf("SSH read returned %q", data)
	}
}

func TestHTTPRepositoryRejectsHTTPSDowngrade(t *testing.T) {
	locator, err := ParseRepositoryLocator("https://example.invalid/repo")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	httpReader := reader.(*httpRepositoryReader)
	request := &http.Request{URL: &url.URL{Scheme: "http"}}
	via := []*http.Request{{URL: &url.URL{Scheme: "https"}}}
	if err := httpReader.client.CheckRedirect(request, via); err == nil {
		t.Fatal("HTTPS downgrade redirect was allowed")
	}
}
