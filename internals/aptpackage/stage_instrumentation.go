//go:build tpa_bench

package aptpackage

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"time"
)

var stageNanos [stageCount]atomic.Int64
var stageCalls [stageCount]atomic.Int64

func startStage(id stageID) func() {
	started := time.Now()
	return func() {
		stageNanos[id].Add(time.Since(started).Nanoseconds())
		stageCalls[id].Add(1)
	}
}

// StageMetricsSnapshot returns benchmark-only aggregate duration and call data.
func StageMetricsSnapshot() map[string]map[string]int64 {
	result := make(map[string]map[string]int64, stageCount)
	for i, name := range stageNames {
		nanos := stageNanos[i].Load()
		calls := stageCalls[i].Load()
		if calls == 0 {
			continue
		}
		result[name] = map[string]int64{"duration_ns": nanos, "calls": calls}
	}
	return result
}

// WriteStageMetrics writes optional benchmark-only measurements as JSON.
func WriteStageMetrics(path string) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(StageMetricsSnapshot(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
