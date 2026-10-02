// Command metapackages creates deterministic, valid Debian meta-packages with
// TPA's package initialization and build APIs. It is benchmark tooling only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

const maxWorkers = 32

type packageMetrics struct {
	tree   time.Duration
	render time.Duration
	write  time.Duration
	build  time.Duration
	bytes  int64
}

type result struct {
	Packages       int                         `json:"packages"`
	Version        string                      `json:"version"`
	Output         string                      `json:"output"`
	Workers        int                         `json:"workers"`
	TreeDuration   time.Duration               `json:"tree_duration_ns_total"`
	RenderDuration time.Duration               `json:"render_duration_ns_total"`
	WriteDuration  time.Duration               `json:"control_write_duration_ns_total"`
	BuildDuration  time.Duration               `json:"build_duration_ns_total"`
	BuildStages    map[string]map[string]int64 `json:"build_stages,omitempty"`
	Bytes          int64                       `json:"bytes"`
}

func main() {
	out := flag.String("out", "", "output directory for .deb files")
	count := flag.Int("count", 10000, "number of packages")
	version := flag.String("version", "1.0.0", "Debian package version")
	metrics := flag.String("metrics", "", "optional JSON metrics output path")
	stageMetrics := flag.String("stage-metrics", "", "optional benchmark-only stage metrics JSON path (build with -tags=tpa_bench)")
	first := flag.Int("first", 0, "first package index")
	workers := flag.Int("workers", 1, "bounded concurrent package builds (1-32)")
	flag.Parse()
	if *out == "" || *count < 1 || *first < 0 || *first+*count > 100000 || *workers < 1 || *workers > maxWorkers {
		fmt.Fprintln(os.Stderr, "usage: metapackages --out DIR [--count N] [--version VERSION] [--first INDEX] [--workers 1-32]")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		fatal(err)
	}
	perPackage := make([]packageMetrics, *count)
	if err := runJobs(*count, *workers, func(offset int) error {
		metrics, err := buildPackage(*out, *version, *first+offset)
		if err != nil {
			return err
		}
		perPackage[offset] = metrics
		return nil
	}); err != nil {
		fatal(err)
	}

	result := result{Packages: *count, Version: *version, Output: *out, Workers: *workers}
	for _, metrics := range perPackage {
		result.TreeDuration += metrics.tree
		result.RenderDuration += metrics.render
		result.WriteDuration += metrics.write
		result.BuildDuration += metrics.build
		result.Bytes += metrics.bytes
	}
	// The generator's own JSON is written after the benchmarked operation; it
	// does not inspect or transform package payloads.
	if *stageMetrics != "" {
		if err := aptpackage.WriteStageMetrics(*stageMetrics); err != nil {
			fatal(fmt.Errorf("write stage metrics: %w", err))
		}
		result.BuildStages = aptpackage.StageMetricsSnapshot()
	}
	if *metrics != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*metrics, append(data, '\n'), 0644); err != nil {
			fatal(err)
		}
	} else if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal(err)
	}
}

func buildPackage(out, version string, index int) (packageMetrics, error) {
	name := fmt.Sprintf("tpa-meta-%05d", index)
	tree := filepath.Join(out, ".build", name)
	deb := filepath.Join(out, fmt.Sprintf("%s_%s_all.deb", name, version))
	control := aptpackage.Control{
		Name: name, Version: version, Architecture: "all",
		Maintainer:  "TPA benchmark <benchmark@example.invalid>",
		Description: fmt.Sprintf("Deterministic synthetic meta-package %05d\n Lightweight benchmark package with control metadata only.", index),
		Section:     "misc", Priority: "optional",
		Provides: fmt.Sprintf("tpa-meta-capability-%02d", index%100),
	}
	if index > 0 {
		control.Depends = fmt.Sprintf("tpa-meta-%05d (>= 1.0.0)", index-1)
	}

	var metrics packageMetrics
	started := time.Now()
	if err := os.MkdirAll(filepath.Join(tree, "DEBIAN"), 0755); err != nil {
		return metrics, fmt.Errorf("initialize %s: %w", name, err)
	}
	metrics.tree = time.Since(started)
	started = time.Now()
	stanza, err := control.Render()
	if err != nil {
		return metrics, fmt.Errorf("render %s: %w", name, err)
	}
	metrics.render = time.Since(started)
	started = time.Now()
	if err := os.WriteFile(filepath.Join(tree, "DEBIAN", "control"), []byte(stanza), 0644); err != nil {
		return metrics, fmt.Errorf("write %s control: %w", name, err)
	}
	metrics.write = time.Since(started)
	started = time.Now()
	if err := aptpackage.Build(aptpackage.Config{InDir: tree, OutDir: deb}); err != nil {
		return metrics, fmt.Errorf("build %s: %w", name, err)
	}
	metrics.build = time.Since(started)
	info, err := os.Stat(deb)
	if err != nil {
		return metrics, err
	}
	metrics.bytes = info.Size()
	if err := os.RemoveAll(tree); err != nil {
		return metrics, fmt.Errorf("remove build tree for %s: %w", name, err)
	}
	return metrics, nil
}

func runJobs(count, workers int, job func(index int) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := make(chan int, workers*2)
	type result struct {
		index int
		err   error
	}
	results := make(chan result, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok || ctx.Err() != nil {
						return
					}
					err := job(index)
					results <- result{index: index, err: err}
					if err != nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := 0; index < count; index++ {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case jobs <- index:
			}
		}
	}()
	go func() {
		group.Wait()
		close(results)
	}()
	completed := 0
	firstErrorIndex := count
	var firstError error
	for completedResult := range results {
		completed++
		if completedResult.err != nil && completedResult.index < firstErrorIndex {
			firstErrorIndex = completedResult.index
			firstError = completedResult.err
		}
	}
	if firstError != nil {
		return firstError
	}
	if completed != count {
		return fmt.Errorf("package generation canceled after %d of %d jobs", completed, count)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
