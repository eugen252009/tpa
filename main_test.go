package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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

func TestSchemaReturnsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"schema"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("schema exit code = %d, stderr=%q", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("schema produced no output")
	}
}
