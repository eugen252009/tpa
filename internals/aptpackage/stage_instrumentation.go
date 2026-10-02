//go:build tpa_bench

package aptpackage

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var stageNanos [stageCount]atomic.Int64
var stageCalls [stageCount]atomic.Int64
var directControlReads atomic.Int64
var controlFallbackReasons sync.Map

func startStage(id stageID) func() {
	started := time.Now()
	return func() {
		stageNanos[id].Add(time.Since(started).Nanoseconds())
		stageCalls[id].Add(1)
	}
}

func recordControlDirectRead() { directControlReads.Add(1) }

func recordControlFallback(reason string) {
	value, _ := controlFallbackReasons.LoadOrStore(reason, new(atomic.Int64))
	value.(*atomic.Int64).Add(1)
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
	if attempts := stageCalls[stageControlDirect].Load(); attempts > 0 {
		result["control_reader_in_process"] = map[string]int64{
			"duration_ns": stageNanos[stageControlDirect].Load(),
			"calls":       directControlReads.Load(),
			"attempts":    attempts,
		}
	}
	controlFallbackReasons.Range(func(key, value any) bool {
		result["control_fallback_reason:"+key.(string)] = map[string]int64{"calls": value.(*atomic.Int64).Load()}
		return true
	})
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
