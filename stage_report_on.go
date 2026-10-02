//go:build tpa_bench

package main

import (
	"fmt"
	"os"

	"github.com/eugen252009/tpa/internals/aptpackage"
)

func writeStageMetrics() {
	if path := os.Getenv("TPA_STAGE_METRICS"); path != "" {
		if err := aptpackage.WriteStageMetrics(path); err != nil {
			fmt.Fprintf(os.Stderr, "tpa benchmark metrics: %v\n", err)
		}
	}
}
