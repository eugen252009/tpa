package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func TestCommandExitSemantics(t *testing.T) {
	root := t.TempDir()
	blockingFile := filepath.Join(root, "file")
	if err := os.WriteFile(blockingFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "no command", args: nil},
		{name: "unknown command", args: []string{"unknown"}},
		{name: "init", args: []string{"init", "-out", filepath.Join(blockingFile, "package")}},
		{name: "build", args: []string{"build", "-in", missing, "-out", filepath.Join(root, "package.deb")}},
		{name: "parse", args: []string{"parse", "-in", missing + ".deb"}},
		{name: "json", args: []string{"json"}, stdin: "{invalid"},
		{name: "pack", args: []string{"pack", "-in", missing, "-out", filepath.Join(root, "repo")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, bytes.NewBufferString(test.stdin), &stdout, &stderr); code == 0 {
				t.Fatalf("failure returned exit code 0; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestNoProvenanceFlagDisablesAutomaticMetadata(t *testing.T) {
	root := t.TempDir()
	input := `{"control":{"name":"example","version":"1.0.0","architecture":"all","maintainer":"Example","description":"Example","memaType":"backup"},"outdir":"` + root + `"}`
	var stdout, stderr bytes.Buffer
	if code := run([]string{"json", "--no-provenance"}, bytes.NewBufferString(input), &stdout, &stderr); code != 0 {
		t.Fatalf("json --no-provenance exit code = %d, stderr=%q", code, stderr.String())
	}
	control, err := os.ReadFile(filepath.Join(root, "DEBIAN", "control"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(control)
	if strings.Contains(text, "TPA-Version:") || strings.Contains(text, "Created-At:") {
		t.Fatalf("automatic provenance was not disabled:\n%s", text)
	}
	if !strings.Contains(text, "Mema-Type: backup\n") {
		t.Fatalf("custom metadata was disabled with provenance:\n%s", text)
	}

	persistentRoot := t.TempDir()
	persistentInput := `{"control":{"name":"example","version":"1.0.0","architecture":"all","maintainer":"Example","description":"Example"},"provenance":false,"outdir":"` + persistentRoot + `"}`
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"json"}, bytes.NewBufferString(persistentInput), &stdout, &stderr); code != 0 {
		t.Fatalf("persistent provenance=false exit code = %d, stderr=%q", code, stderr.String())
	}
	persistentControl, err := os.ReadFile(filepath.Join(persistentRoot, "DEBIAN", "control"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(persistentControl); strings.Contains(text, "TPA-Version:") || strings.Contains(text, "Created-At:") {
		t.Fatalf("persistent provenance=false did not disable automatic metadata:\n%s", text)
	}
}

func TestVersionCommandReturnsProgramVersion(t *testing.T) {
	const wantVersion = "0.5.0"
	if aptpackage.TPAVersion != wantVersion {
		t.Fatalf("program version = %q, want %q", aptpackage.TPAVersion, wantVersion)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("version exit code = %d, stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), wantVersion+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestBuildPackageDefaultMatchesProgramVersion(t *testing.T) {
	script, err := os.ReadFile("build.sh")
	if err != nil {
		t.Fatal(err)
	}
	want := "VERSION=${TPA_VERSION:-" + aptpackage.TPAVersion + "}"
	if !strings.Contains(string(script), want+"\n") {
		t.Fatalf("build.sh does not default Debian package version to program version %q", aptpackage.TPAVersion)
	}
}

func TestVersionFlagIsNotVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-version"}, bytes.NewReader(nil), &stdout, &stderr); code == 0 {
		t.Fatalf("-version unexpectedly succeeded as version command: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSchemaReturnsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"schema"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("schema exit code = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("schema produced no output")
	}
}

func TestPackWritesVerifiedGenerationInventory(t *testing.T) {
	root := t.TempDir()
	packageRoot := filepath.Join(root, "package")
	archive := filepath.Join(root, "fixture.deb")
	repository := filepath.Join(root, "repository")
	manifestPath := filepath.Join(root, "generation.json")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "-name=fixture", "-ver=1.0", "-arch=all", "-maintainer=Fixture", "-desc=Fixture", "-out=" + packageRoot}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"build", "-in=" + packageRoot, "-out=" + archive}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("build exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"pack", "-in=" + root, "-out=" + repository, "-generation-manifest=" + manifestPath, "-repository-id=repo", "-generation-id=0123456789abcdef0123456789abcdef"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("pack exit=%d stderr=%q", code, stderr.String())
	}
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest aptpackage.GenerationManifest
	if err = json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RepositoryID != "repo" || manifest.GenerationID != "0123456789abcdef0123456789abcdef" || len(manifest.Files) == 0 {
		t.Fatalf("unexpected generation manifest: %+v", manifest)
	}
	if err := aptpackage.VerifyGenerationManifest(repository, manifest, "repo"); err != nil {
		t.Fatalf("generated inventory does not verify: %v", err)
	}
}
