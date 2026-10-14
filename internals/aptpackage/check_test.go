package aptpackage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlDiagnosticsCollectIndependentErrors(t *testing.T) {
	c := Control{Name: "UpperName", Version: "banana", Architecture: "x86_64", Priority: "urgent", Essential: "maybe", MultiArch: "sometimes", Depends: "libc6 => 2", Provides: "feature (>= 1)"}
	err := ValidateControl(c)
	ds := ErrorDiagnostics(err)
	if len(ds) < 9 {
		t.Fatalf("expected independent errors, got %d: %v", len(ds), err)
	}
	for _, d := range ds {
		if d.Field == "" || !strings.HasPrefix(d.URL, "https://www.debian.org/") {
			t.Fatalf("unhelpful diagnostic: %+v", d)
		}
	}
	for _, run := range []struct {
		name string
		call func() error
	}{
		{"init", func() error { return InitPackage(Config{Control: c, OutDir: filepath.Join(t.TempDir(), "package")}) }},
		{"json", func() error { return JSONBuild(Config{Control: c, OutDir: filepath.Join(t.TempDir(), "package")}) }},
	} {
		t.Run(run.name, func(t *testing.T) {
			if got := ErrorDiagnostics(run.call()); len(got) != len(ds) {
				t.Fatalf("different coverage: %+v", got)
			}
		})
	}
}

func TestDebianVersionValidation(t *testing.T) {
	for _, v := range []string{"1", "1.2.3~rc1", "2:1.0-3+b1", "0.7.1~dev.20261014", "1:2:3-1", "1.0-beta-2"} {
		if !validDebianVersion(v) {
			t.Errorf("rejected %q", v)
		}
	}
	for _, v := range []string{"", "dev", "1.0-", "a:1.0", "1:abc", "1/2", "1 2", "1.0:3", "1-2:3"} {
		if validDebianVersion(v) {
			t.Errorf("accepted %q", v)
		}
	}
}

func TestControlParserRejectsDuplicatesAndMalformedSyntax(t *testing.T) {
	base := basicControl("fixture", "1.0", "all")
	for _, bad := range []string{base + "package: second\n", base + "not a field\n", base + "\nDescription: extra stanza\n"} {
		if _, err := ParseControl([]byte(bad)); err == nil || !strings.Contains(err.Error(), controlPolicyURL) {
			t.Fatalf("accepted malformed control or missing help: %v", err)
		}
	}
	c, err := ParseControl([]byte(strings.ReplaceAll(base, ": ", ":")))
	if err != nil || c.Name != "fixture" {
		t.Fatalf("valid no-space fields: %+v %v", c, err)
	}
	if _, err := ParseControl(nil); len(ErrorDiagnostics(err)) < 5 {
		t.Fatalf("missing required fields not collected: %v", err)
	}
}

func TestCheckAndBuildRejectEditedMetadataWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "package")
	cfg := Config{OutDir: root, Control: Control{Name: "fixture", Version: "1.0", Architecture: "all", Maintainer: "Test", Description: "Fixture"}}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	control := []byte("Package: WrongName\nVersion: nope\nArchitecture: x86_64\nMaintainer:\nDescription:\n")
	if err := os.WriteFile(filepath.Join(root, "DEBIAN", "control"), control, 0644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "DEBIAN", "postinst")
	if err := os.Chmod(script, 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshotLifecycleTree(t, root)
	report, err := CheckPackage(root)
	if err == nil || report.Valid || len(report.Diagnostics) < 5 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	out := filepath.Join(t.TempDir(), "output.deb")
	if err := Build(Config{InDir: root, OutDir: out}); err == nil {
		t.Fatal("build accepted edited metadata")
	}
	assertLifecycleTreeEqual(t, root, before)
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("invalid build created archive")
	}
}

func TestCheckLeavesAutomaticScriptModesAlone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "package")
	cfg := Config{OutDir: root, Control: Control{Name: "fixture", Version: "1.0", Architecture: "all", Maintainer: "Test", Description: "Fixture"}}
	if err := InitPackage(cfg); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "DEBIAN", "postinst")
	if err := os.Chmod(script, 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshotLifecycleTree(t, root)
	if report, err := CheckPackage(root); err != nil || !report.Valid {
		t.Fatalf("automatic mode treated as error: %+v %v", report, err)
	}
	assertLifecycleTreeEqual(t, root, before)
	info, err := os.Stat(script)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatal("check repaired script mode")
	}
}

func TestDescriptionSerializationRoundTrip(t *testing.T) {
	c := Control{Name: "fixture", Version: "1.0", Architecture: "all", Maintainer: "Test", Description: "Summary\nLong description\n\nFinal line"}
	rendered, err := c.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "Summary\n Long description\n .\n Final line\n") {
		t.Fatalf("bad description: %s", rendered)
	}
	if _, err := ParseControl([]byte(rendered)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCollectsArtifactErrorsAndSkipsOnUntrustedIndex(t *testing.T) {
	requireDebTools(t)
	root := t.TempDir()
	input := filepath.Join(root, "input")
	repo := filepath.Join(root, "repo")
	buildTestDeb(t, input, "first_1.0_all.deb", basicControl("first", "1.0", "all"), "one")
	buildTestDeb(t, input, "second_1.0_all.deb", basicControl("second", "1.0", "all"), "two")
	if err := Pack(Config{InDir: input, OutDir: repo}); err != nil {
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
	defer reader.Close()
	pristine, err := VerifyRepository(context.Background(), reader, RepositoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pristine.Packages {
		if err := os.WriteFile(filepath.Join(repo, p.Filename), []byte("tampered"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := VerifyRepository(context.Background(), reader, RepositoryOptions{Workers: 2})
	if err == nil || len(report.Diagnostics) != 2 || report.ArtifactsChecked != 2 || len(report.Skipped) != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	for _, p := range pristine.Packages {
		if !strings.Contains(err.Error(), p.Filename) {
			t.Errorf("missing artifact %s", p.Filename)
		}
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dists", "stable", "main", "binary-all", "Packages"), []byte("untrusted"), 0644); err != nil {
		t.Fatal(err)
	}
	report, err = VerifyRepository(context.Background(), reader, RepositoryOptions{})
	if err == nil || report.ArtifactsChecked != 0 || len(report.Skipped) == 0 {
		t.Fatalf("untrusted index used: %+v %v", report, err)
	}
}

func TestVerifyCollectsIndependentIndexErrors(t *testing.T) {
	requireDebTools(t)
	root := t.TempDir()
	input, repo := filepath.Join(root, "input"), filepath.Join(root, "repo")
	for _, arch := range []string{"amd64", "arm64"} {
		buildTestDeb(t, input, "fixture_1.0_"+arch+".deb", basicControl("fixture", "1.0", arch), arch)
	}
	if err := Pack(Config{InDir: input, OutDir: repo}); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if err := os.WriteFile(filepath.Join(repo, "dists", "stable", "main", "binary-"+arch, "Packages"), []byte("corrupt"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	locator, _ := ParseRepositoryLocator(repo)
	reader, err := OpenRepositoryReader(locator)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	report, err := VerifyRepository(context.Background(), reader, RepositoryOptions{})
	if err == nil || len(report.Diagnostics) != 2 || report.ArtifactsChecked != 0 || len(report.Skipped) == 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyRepository(ctx, reader, RepositoryOptions{}); err == nil {
		t.Fatal("cancelled verification succeeded")
	}
}

func TestBinaryRelationships(t *testing.T) {
	c := Control{Name: "fixture", Version: "1.0", Architecture: "amd64", Maintainer: "Test", Description: "Fixture"}
	for _, deps := range []string{"libc6 (>= 2.36), other:any | alternative:native", "lib-one (<< 1:2.0-1)", "ab"} {
		c.Depends = deps
		if err := ValidateControl(c); err != nil {
			t.Errorf("valid %q: %v", deps, err)
		}
	}
	for _, deps := range []string{"UPPER", "ab (=> 2)", "ab,", "ab |", "${shlibs:Depends}", "ab\n"} {
		c.Depends = deps
		if err := ValidateControl(c); err == nil {
			t.Errorf("accepted %q", deps)
		}
	}
}
