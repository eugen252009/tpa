package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eugen252009/tpa/internal/version"
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
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("version exit code = %d, stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), version.Version+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--version"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 || stdout.String() != "tpa "+version.Version+"\n" {
		t.Fatalf("--version exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-version"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 || stdout.String() != "tpa "+version.Version+"\n" {
		t.Fatalf("-version exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestBuildScriptUsesOneInjectedVersionForBinaryAndPackage(t *testing.T) {
	script, err := os.ReadFile("build.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, fragment := range []string{
		"VERSION=${TPA_VERSION:-0.0.0~dev}",
		"LDFLAGS=\"-X github.com/eugen252009/tpa/internal/version.Version=$VERSION\"",
		"go build -ldflags \"$LDFLAGS\" -o tpa .",
		"go build -ldflags \"$LDFLAGS\" -o \"$work_dir/usr/local/bin/tpa\" .",
		"-ver=\"$VERSION\"",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("build.sh is missing version-coupling fragment %q", fragment)
		}
	}
}

func TestUnlistCommandRemovesMetadataButKeepsArtifact(t *testing.T) {
	_, repo, artifact := makeCLILifecycleRepository(t)
	args := []string{"unlist", "-in=" + repo, "-package=fixture", "-ver=1.0", "-arch=all"}
	var stdout, stderr bytes.Buffer
	if code := runWithTerminal(args, bytes.NewReader(nil), &stdout, &stderr, false); code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("unlist removed artifact: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(repo, "dists", "stable", "main", "binary-all", "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(index), "Package: fixture\n") {
		t.Fatal("unlisted package remains in Packages")
	}
}

func TestDeleteCancellationAndNonInteractiveConfirmationAreSafe(t *testing.T) {
	for _, test := range []struct {
		name        string
		interactive bool
		input       string
		wantCode    int
	}{
		{name: "interactive no", interactive: true, input: "n\n", wantCode: 0},
		{name: "noninteractive without yes", interactive: false, input: "yes\n", wantCode: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, repo, artifact := makeCLILifecycleRepository(t)
			before := snapshotCLILifecycleTree(t, repo)
			args := []string{"delete", "-in=" + repo, "-package=fixture", "-ver=1.0", "-arch=all"}
			var stdout, stderr bytes.Buffer
			if code := runWithTerminal(args, bytes.NewBufferString(test.input), &stdout, &stderr, test.interactive); code != test.wantCode {
				t.Fatalf("exit=%d, want %d; stdout=%q stderr=%q", code, test.wantCode, stdout.String(), stderr.String())
			}
			if test.interactive && !strings.Contains(stdout.String(), "Deletion cancelled") {
				t.Fatalf("missing cancellation message: %q", stdout.String())
			}
			if !test.interactive && !strings.Contains(stderr.String(), "requires an interactive terminal or --yes") {
				t.Fatalf("missing non-interactive refusal: %q", stderr.String())
			}
			assertCLILifecycleTreeEqual(t, repo, before)
			if _, err := os.Stat(artifact); err != nil {
				t.Fatalf("artifact changed after refusal: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".repo.tpa.lock")); !os.IsNotExist(err) {
				t.Fatalf("declined delete created mutation lock: %v", err)
			}
		})
	}
}

func TestDeleteAcceptsInteractiveYesAndNoninteractiveYesFlag(t *testing.T) {
	for _, test := range []struct {
		name        string
		interactive bool
		input       string
		flag        bool
	}{
		{name: "interactive yes", interactive: true, input: "YeS\n"},
		{name: "noninteractive --yes", interactive: false, flag: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, repo, artifact := makeCLILifecycleRepository(t)
			args := []string{"delete", "-in=" + repo, "-package=fixture", "-ver=1.0", "-arch=all"}
			if test.flag {
				args = append(args, "--yes")
			}
			var stdout, stderr bytes.Buffer
			if code := runWithTerminal(args, bytes.NewBufferString(test.input), &stdout, &stderr, test.interactive); code != 0 {
				t.Fatalf("exit=%d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if _, err := os.Stat(artifact); !os.IsNotExist(err) {
				t.Fatalf("artifact was not removed: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(repo, "dists", "stable", "main", "binary-all", "Packages"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "Package: fixture\n") {
				t.Fatal("deleted package remains in Packages")
			}
		})
	}
}

func makeCLILifecycleRepository(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	packageRoot := filepath.Join(root, "fixture-tree")
	archive := filepath.Join(root, "artifacts", "fixture_1.0_all.deb")
	repo := filepath.Join(root, "repo")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "-name=fixture", "-ver=1.0", "-arch=all", "-maintainer=Fixture", "-desc=Fixture", "-out=" + packageRoot}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("init exit=%d stderr=%q", code, stderr.String())
	}
	if code := run([]string{"build", "-in=" + packageRoot, "-out=" + archive}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("build exit=%d stderr=%q", code, stderr.String())
	}
	if code := run([]string{"pack", "-in=" + filepath.Dir(archive), "-out=" + repo}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("pack exit=%d stderr=%q", code, stderr.String())
	}
	artifact := filepath.Join(repo, "pool", "main", "f", "fixture", filepath.Base(archive))
	return root, repo, artifact
}

func snapshotCLILifecycleTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[rel] = data
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func assertCLILifecycleTreeEqual(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	actual := snapshotCLILifecycleTree(t, root)
	if len(actual) != len(expected) {
		t.Fatalf("repository file count changed: got %d, want %d", len(actual), len(expected))
	}
	for path, want := range expected {
		if got, ok := actual[path]; !ok || !bytes.Equal(got, want) {
			t.Errorf("repository file %s changed", path)
		}
	}
}

func TestHelpNoCommandUnknownCommandAndFlagErrorUX(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("%v exit=%d stderr=%q", args, code, stderr.String())
		}
		text := stdout.String()
		for _, expected := range []string{version.ProductName + " " + version.Version, version.ProductDescription, version.ProjectURL, version.SupportEmail, "Usage:", "init", "build", "parse", "pack", "inspect", "verify", "unlist", "delete", "json", "schema", "version"} {
			if !strings.Contains(text, expected) {
				t.Errorf("%v help is missing %q:\n%s", args, expected, text)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, bytes.NewReader(nil), &stdout, &stderr); code != 2 || !strings.Contains(stdout.String(), version.SupportEmail) {
		t.Fatalf("no-command exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"foobar"}, bytes.NewReader(nil), &stdout, &stderr); code != 2 {
		t.Fatalf("unknown command exit=%d", code)
	}
	for _, expected := range []string{`unknown command "foobar"`, version.Version, version.ProjectURL, version.SupportEmail, "tpa --help"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Errorf("unknown command error missing %q: %s", expected, stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"build", "--definitely-invalid"}, bytes.NewReader(nil), &stdout, &stderr); code != 2 {
		t.Fatalf("invalid flag exit=%d", code)
	}
	for _, expected := range []string{version.Version, version.SupportEmail, "Run 'tpa --help'"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Errorf("flag error missing %q: %s", expected, stderr.String())
		}
	}
	if len(stderr.String()) > 500 {
		t.Fatalf("flag error printed an unexpectedly large help dump: %s", stderr.String())
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
