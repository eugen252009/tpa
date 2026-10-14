package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestComposeCLI(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "package.json")
	if err := os.WriteFile(input, []byte("{\"control\":{\"name\":\"cli-direct\",\"version\":\"1.0\",\"architecture\":\"all\",\"maintainer\":\"Fixture\",\"description\":\"Metadata only\"}}"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"compose", "-in", input, "-out", filepath.Join(root, "repo"), "-workers", "2", "-memory-mib", "16", "-epoch", "123"}, bytes.NewReader(nil), &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
}
