package aptpackage

import (
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPackagesCompressionRejectsMismatchedIndex(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "Packages")
	if err := os.WriteFile(packages, []byte("Package: expected\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(packages + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte("Package: different\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackagesCompression(packages); err == nil {
		t.Fatal("mismatched Packages.gz was accepted")
	}
}

func TestVerifyRepositoryRequiresExpectedSigningFingerprint(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	root := t.TempDir()
	gpgHome := filepath.Join(root, "gnupg")
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gpgHome)
	first := generateTestSigningKey(t, "First signer <first@example.invalid>")
	second := generateTestSigningKey(t, "Second signer <second@example.invalid>")
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "signed")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo"), GPG: first}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.GPG = second
	if err := verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "expected fingerprint") {
		t.Fatalf("expected wrong-signer failure, got %v", err)
	}
}

func TestVerifyRepositoryRejectsReleaseDifferentFromSignedPayload(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	root := t.TempDir()
	gpgHome := filepath.Join(root, "gnupg")
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gpgHome)
	fingerprint := generateTestSigningKey(t, "Payload signer <payload@example.invalid>")
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "signed")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo"), GPG: fingerprint}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(cfg.OutDir, "dists", "stable", "Release")
	file, err := os.OpenFile(release, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("Changed: yes\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "payload does not match Release") {
		t.Fatalf("expected signed payload mismatch, got %v", err)
	}
}

func TestVerifyRepositoryRejectsUnsignedTrailingInReleaseData(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is required")
	}
	root := t.TempDir()
	gpgHome := filepath.Join(root, "gnupg")
	if err := os.Mkdir(gpgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", gpgHome)
	fingerprint := generateTestSigningKey(t, "Trailing data signer <trailing@example.invalid>")
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "signed")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo"), GPG: fingerprint}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	inRelease := filepath.Join(cfg.OutDir, "dists", "stable", "InRelease")
	file, err := os.OpenFile(inRelease, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("unsigned tail\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = verifyRepository(cfg); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected unsigned trailing data rejection, got %v", err)
	}
}

func generateTestSigningKey(t *testing.T, identity string) string {
	t.Helper()
	cmd := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "", "--quick-generate-key", identity, "rsa2048", "sign", "0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate GPG key: %v: %s", err, output)
	}
	fingerprint, err := signingFingerprint(identity)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}
