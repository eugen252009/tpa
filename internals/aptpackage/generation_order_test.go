package aptpackage

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestGenerationManifestSortsFullPathsRatherThanWalkOrder(t *testing.T) {
	root := t.TempDir()
	paths := []string{"pool/main/b/bll/bll.deb", "pool/main/b/bll-bash/bll-bash.deb", "pool/main/b/bll-zsh/bll-zsh.deb", "index.html", "repository.json"}
	for _, path := range paths {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(path), 0644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := CreateGenerationManifest(root, "repository", "generation", "")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, file := range manifest.Files {
		got = append(got, file.Path)
	}
	sort.Strings(paths)
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("got %v want %v", got, paths)
	}
	if err := VerifyGenerationManifest(root, manifest, "repository"); err != nil {
		t.Fatal(err)
	}
}
