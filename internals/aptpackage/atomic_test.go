package aptpackage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestAtomicPackSuccessfullyReplacesAndCleansLiveRepository(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("successful atomic publication requires Linux")
	}
	root := t.TempDir()
	live := filepath.Join(root, "repo")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(live, "previous-release")
	if err := os.WriteFile(marker, []byte("complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "packages")
	buildTestDeb(t, input, "fixture_1.0_all.deb", basicControl("fixture", "1.0", "all"), "new repository")

	stop := make(chan struct{})
	observationErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := os.Stat(marker); err != nil {
				if !os.IsNotExist(err) {
					select {
					case observationErrors <- err:
					default:
					}
					return
				}
				release := filepath.Join(live, "dists", "stable", "Release")
				if _, releaseErr := os.Stat(release); releaseErr != nil {
					select {
					case observationErrors <- fmt.Errorf("live tree was neither complete old nor complete new repository: %w", releaseErr):
					default:
					}
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	err := AtomicPack(Config{InDir: input, Repo: RepoConfig{Codename: "stable", Components: "main"}}, live)
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-observationErrors:
		t.Fatal(err)
	default:
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("old live marker remains after replacement: %v", err)
	}
	packages := filepath.Join(live, "dists", "stable", "main", "binary-all", "Packages")
	if _, err := os.Stat(packages); err != nil {
		t.Fatalf("new repository is not live: %v", err)
	}
	for path, wantMode := range map[string]os.FileMode{live: 0o755, packages: 0o644} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != wantMode {
			t.Errorf("%s mode is %o, want %o", path, mode, wantMode)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".repo.tpa-staging-") {
			t.Errorf("replaced staging tree was not cleaned: %s", entry.Name())
		}
	}
}
