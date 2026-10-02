//go:build !tpa_bench

package aptpackage

func startStage(stageID) func() { return func() {} }

func recordControlDirectRead() {}

func recordControlFallback(string) {}

func StageMetricsSnapshot() map[string]map[string]int64 { return nil }

func WriteStageMetrics(string) error { return nil }
