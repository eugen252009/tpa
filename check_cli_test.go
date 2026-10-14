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

func TestCheckCLIReportsAllMetadataErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "DEBIAN"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DEBIAN", "control"), []byte("Package: WrongName\nVersion: nope\nArchitecture: x86_64\nMaintainer:\nDescription:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"check", "--json", root}, {"check", "-in", root, "--json"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, bytes.NewReader(nil), &stdout, &stderr); code != 1 {
			t.Fatalf("exit=%d %s", code, stderr.String())
		}
		var report aptpackage.CheckReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.Valid || len(report.Diagnostics) < 5 || stderr.Len() != 0 {
			t.Fatalf("report=%+v stderr=%s", report, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", root}, bytes.NewReader(nil), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "uppercase") || !strings.Contains(stderr.String(), "https://www.debian.org/") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestCheckCLIValidPackageAndMissingTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "package")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "-out", root}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("init %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", root}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("check %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", "--json", filepath.Join(root, "missing")}, bytes.NewReader(nil), &stdout, &stderr); code != 1 {
		t.Fatalf("missing %d", code)
	}
	var report aptpackage.CheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Valid || len(report.Diagnostics) == 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
