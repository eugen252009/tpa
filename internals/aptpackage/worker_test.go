package aptpackage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPackWorkerCountLimitsAndDefault(t *testing.T) {
	for _, requested := range []int{1, MaxPackWorkers} {
		if got, err := packWorkerCount(requested); err != nil || got != requested {
			t.Fatalf("packWorkerCount(%d) = %d, %v", requested, got, err)
		}
	}
	for _, requested := range []int{-1, MaxPackWorkers + 1} {
		if _, err := packWorkerCount(requested); err == nil {
			t.Fatalf("packWorkerCount(%d) unexpectedly succeeded", requested)
		}
	}
	if got, err := packWorkerCount(0); err != nil || got < 1 || got > MaxPackWorkers {
		t.Fatalf("default packWorkerCount = %d, %v", got, err)
	}
}

func TestCopyFileAndHashUsesPublishedBytes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.deb")
	destination := filepath.Join(root, "destination.deb")
	want := []byte("artifact bytes")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, size, err := copyFileAndHash(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(want)) {
		t.Fatalf("copied size = %d, want %d", size, len(want))
	}
	sourceHash, err := getHash(source)
	if err != nil {
		t.Fatal(err)
	}
	destinationHash, err := getHash(destination)
	if err != nil {
		t.Fatal(err)
	}
	if hash != sourceHash || hash != destinationHash {
		t.Fatalf("streamed hash %s differs from source %s or destination %s", hash, sourceHash, destinationHash)
	}
}

func TestOrderedInspectionJobsBoundReorderWindow(t *testing.T) {
	const count, workers = 100, 4
	started := make(chan int, count)
	releaseFirst := make(chan struct{})
	consumed := make([]int, 0, count)
	done := make(chan error, 1)
	go func() {
		done <- runOrderedInspectionJobs(count, workers, func(index int) (orderedInspectionResult, error) {
			started <- index
			if index == 0 {
				<-releaseFirst
			}
			return orderedInspectionResult{}, nil
		}, func(index int, _ orderedInspectionResult) error {
			consumed = append(consumed, index)
			return nil
		})
	}()
	startedIndexes := make(map[int]bool)
	for len(startedIndexes) < workers*2 {
		select {
		case index := <-started:
			startedIndexes[index] = true
		case <-time.After(time.Second):
			t.Fatal("workers did not fill the bounded inspection window")
		}
	}
	time.Sleep(10 * time.Millisecond)
	select {
	case index := <-started:
		t.Fatalf("inspection window advanced to job %d while job 0 was blocked", index)
	default:
	}
	close(releaseFirst)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for index, got := range consumed {
		if got != index {
			t.Fatalf("consumed job %d at position %d", got, index)
		}
	}
}

func TestRunPackageJobsBoundedAndCompletesEveryIndex(t *testing.T) {
	const count, workers = 200, 4
	var active, maxActive atomic.Int64
	seen := make([]atomic.Int32, count)
	err := runPackageJobs(count, workers, func(index int) error {
		current := active.Add(1)
		for old := maxActive.Load(); current > old && !maxActive.CompareAndSwap(old, current); old = maxActive.Load() {
		}
		defer active.Add(-1)
		seen[index].Add(1)
		time.Sleep(time.Microsecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := maxActive.Load(); got > workers || got < 2 {
		t.Fatalf("max active workers = %d, want between 2 and %d", got, workers)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("active jobs after return = %d, want 0", got)
	}
	for index := range seen {
		if got := seen[index].Load(); got != 1 {
			t.Errorf("job %d ran %d times, want once", index, got)
		}
	}
}

func TestRunPackageJobsCancelsAfterErrorAndJoinsWorkers(t *testing.T) {
	const workers = 4
	wantErr := errors.New("injected worker failure")
	var started, active atomic.Int64
	err := runPackageJobs(10000, workers, func(index int) error {
		started.Add(1)
		active.Add(1)
		defer active.Add(-1)
		if index == 0 {
			return wantErr
		}
		time.Sleep(time.Millisecond)
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want injected error", err)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("%d jobs still active after return", got)
	}
	if got := started.Load(); got >= 10000 {
		t.Fatalf("cancellation did not stop queued jobs: started %d", got)
	}
}

func TestRunPackageJobsZeroJobs(t *testing.T) {
	if err := runPackageJobs(0, 1, func(int) error { t.Fatal("unexpected job"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestPackWorkerCountsPreserveCanonicalIndexes(t *testing.T) {
	requireDebTools(t)
	root := t.TempDir()
	input := filepath.Join(root, "packages")
	for index := 0; index < 12; index++ {
		name := fmt.Sprintf("worker-fixture-%02d", index)
		control := basicControl(name, "1.0.0", "all")
		buildTestDeb(t, input, name+"_1.0.0_all.deb", control, name)
	}
	outputs := make([][]byte, 0, 3)
	for _, workers := range []int{1, 4, 16} {
		out := filepath.Join(root, fmt.Sprintf("repo-%d", workers))
		cfg := Config{InDir: input, OutDir: out, Workers: workers}
		if err := Pack(cfg); err != nil {
			t.Fatalf("Pack workers=%d: %v", workers, err)
		}
		data, err := os.ReadFile(filepath.Join(out, "dists", "stable", "main", "binary-all", "Packages"))
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, data)
	}
	for index := 1; index < len(outputs); index++ {
		if string(outputs[index]) != string(outputs[0]) {
			t.Fatalf("Packages differs between worker counts 1 and %d", []int{4, 16}[index-1])
		}
	}
}
