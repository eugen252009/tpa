package aptpackage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireDebTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"dpkg-deb", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required", tool)
		}
	}
}

func buildTestDeb(t *testing.T, inputDir, filename, control, payload string) string {
	t.Helper()
	requireDebTools(t)
	root, err := os.MkdirTemp(t.TempDir(), "package-root-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "DEBIAN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DEBIAN", "control"), []byte(control), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "usr", "share", "tpa-test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "share", "tpa-test", "payload"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	deb := filepath.Join(inputDir, filename)
	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", root, deb)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v: %s", err, output)
	}
	return deb
}

func basicControl(name, version, architecture string) string {
	return "Package: " + name + "\n" +
		"Version: " + version + "\n" +
		"Architecture: " + architecture + "\n" +
		"Maintainer: Test <test@example.invalid>\n" +
		"Description: repository fixture\n"
}
