package drl

import (
	"testing"
	"time"
)

func TestDiagnosticsCollectorStatusAndSnapshot(t *testing.T) {
	cfg := testEngineConfig()
	collector := NewDiagnosticsCollector(cfg)
	stats := FeatureStats{Dimension: cfg.ObservationDimension(), Window: cfg.ObservationWindow}

	collector.Record("ETHUSDT", []float64{10, 20, 30}, []float32{1, 2, 3}, stats, 0.65, "open_long", 2*time.Millisecond, nil)
	collector.Record("BTCUSDT", []float64{40, 50}, []float32{4, 5}, stats, -0.2, "open_short", 4*time.Millisecond, nil)

	status := collector.Status()
	if status.InferenceCount != 2 || status.LastSymbol != "BTCUSDT" || status.LastMappedAction != "open_short" {
		t.Fatalf("诊断状态异常: %+v", status)
	}
	if status.AverageInferenceTimeMS <= 0 || status.MaxInferenceTimeMS <= 0 {
		t.Fatalf("推理耗时统计应累积: %+v", status)
	}
	diag := collector.StrategyDiagnostics()
	if diag["model_version"] != "test-v1" || diag["mapped_action"] != "open_short" {
		t.Fatalf("StrategyDiagnostics字段异常: %+v", diag)
	}
	snapshot := collector.FeatureSnapshot()
	if snapshot["dimension"].(int) != 2 {
		t.Fatalf("特征快照应保留最近一次观测维度: %+v", snapshot)
	}
	if snapshot["raw_dimension"].(int) != 2 {
		t.Fatalf("特征快照应保留raw维度: %+v", snapshot)
	}
}
