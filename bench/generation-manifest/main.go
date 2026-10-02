// Command generation-manifest measures TPA's complete-generation inventory
// API against an already-built repository tree. It is benchmark tooling only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

type result struct {
	Files        int                         `json:"files"`
	Bytes        int64                       `json:"manifest_bytes"`
	CreateNS     int64                       `json:"create_duration_ns"`
	WriteNS      int64                       `json:"write_duration_ns"`
	VerifyNS     int64                       `json:"verify_duration_ns"`
	StageMetrics map[string]map[string]int64 `json:"stages,omitempty"`
}

func main() {
	root := flag.String("root", "", "completed repository tree to inventory")
	manifestPath := flag.String("out", "", "manifest output path outside the repository tree")
	repositoryID := flag.String("repository-id", "bench-repo", "repository identity")
	generationID := flag.String("generation-id", "bench-generation", "generation identity")
	stagePath := flag.String("stage-metrics", "", "optional benchmark-only stage metrics output path")
	flag.Parse()
	if *root == "" || *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: generation-manifest --root DIR --out FILE [--stage-metrics FILE]")
		os.Exit(2)
	}

	started := time.Now()
	manifest, err := aptpackage.CreateGenerationManifest(*root, *repositoryID, *generationID, "")
	if err != nil {
		fatal(err)
	}
	result := result{Files: len(manifest.Files), CreateNS: time.Since(started).Nanoseconds()}

	started = time.Now()
	if err := aptpackage.WriteGenerationManifest(*manifestPath, manifest); err != nil {
		fatal(err)
	}
	result.WriteNS = time.Since(started).Nanoseconds()
	info, err := os.Stat(*manifestPath)
	if err != nil {
		fatal(err)
	}
	result.Bytes = info.Size()

	started = time.Now()
	if err := aptpackage.VerifyGenerationManifest(*root, manifest, *repositoryID); err != nil {
		fatal(err)
	}
	result.VerifyNS = time.Since(started).Nanoseconds()
	if *stagePath != "" {
		if err := aptpackage.WriteStageMetrics(*stagePath); err != nil {
			fatal(err)
		}
		result.StageMetrics = aptpackage.StageMetricsSnapshot()
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
