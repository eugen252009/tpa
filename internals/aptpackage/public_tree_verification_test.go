package aptpackage

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVerifyFinishedTreeWithPublicSignerOnly(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg required")
	}
	root := t.TempDir()
	signer := filepath.Join(root, "signer")
	os.Mkdir(signer, 0700)
	t.Setenv("GNUPGHOME", signer)
	fp := generateTestSigningKey(t, "Public verification fixture <public@example.invalid>")
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "signed")
	cfg := Config{InDir: input, OutDir: filepath.Join(root, "repo"), GPG: fp}
	if err := Pack(cfg); err != nil {
		t.Fatal(err)
	}
	pub, err := exec.Command("gpg", "--batch", "--export", fp).Output()
	if err != nil {
		t.Fatal(err)
	}
	publicHome := filepath.Join(root, "public")
	os.Mkdir(publicHome, 0700)
	t.Setenv("GNUPGHOME", publicHome)
	cmd := exec.Command("gpg", "--batch", "--import")
	cmd.Stdin = bytes.NewReader(pub)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("public import: %v %s", err, out)
	}
	if _, err := signingFingerprint(fp); err == nil {
		t.Fatal("secret signing lookup unexpectedly succeeded")
	}
	if err := VerifyTPARepositoryTree(cfg); err != nil {
		t.Fatal("finished-tree verification incorrectly required private key", err)
	}
	cfg.GPG = "0000000000000000000000000000000000000000"
	if err := VerifyTPARepositoryTree(cfg); err == nil {
		t.Fatal("wrong public signer accepted")
	}
}
