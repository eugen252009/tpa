package aptpackage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackCreatesUnsignedRepositoryFixture(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is required for the repository fixture")
	}
	root := t.TempDir()
	pkgRoot := filepath.Join(root, "pkg")
	if err := os.MkdirAll(filepath.Join(pkgRoot, "DEBIAN"), 0o755); err != nil {
		t.Fatal(err)
	}
	control := "Package: fixture-tool\nVersion: 1.2.3\nArchitecture: all\nMaintainer: Test <test@example.invalid>\nDescription: repository fixture\n"
	if err := os.WriteFile(filepath.Join(pkgRoot, "DEBIAN", "control"), []byte(control), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgRoot, "usr", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgRoot, "usr", "bin", "fixture-tool"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(root, "packages")
	if err := os.Mkdir(in, 0o755); err != nil {
		t.Fatal(err)
	}
	deb := filepath.Join(in, "fixture-tool_1.2.3_all.deb")
	cmd := exec.Command("dpkg-deb", "--build", pkgRoot, deb)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v: %s", err, output)
	}

	out := filepath.Join(root, "repo")
	cfg := Config{InDir: in, OutDir: out, Repo: RepoConfig{
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
	if !strings.Contains(text, "Package: fixture-tool\n") || !strings.Contains(text, "Filename: pool/main/f/fixture-tool/fixture-tool_1.2.3_all.deb\n") {
		t.Fatalf("invalid Packages fixture:\n%s", text)
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
