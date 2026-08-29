package aptpackage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicPackLeavesLiveRepositoryOnBuildFailure(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "repo")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(live, "previous-release")
	if err := os.WriteFile(marker, []byte("complete"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := AtomicPack(Config{InDir: filepath.Join(root, "missing"), Repo: RepoConfig{Codename: "stable", Components: "main"}}, live)
	if err == nil {
		t.Fatal("expected build failure")
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil || string(data) != "complete" {
		t.Fatalf("live repository changed after failure: %v", readErr)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= 10 && entry.Name()[:10] == ".repo.tpa-" {
			t.Errorf("staging directory was not cleaned: %s", entry.Name())
		}
	}
}
