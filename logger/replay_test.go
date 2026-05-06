package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildReplayReport_OpenRejectionAndPnL(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp: now,
			Decisions: []DecisionAction{
				{Action: "open_long", Symbol: "BTCUSDT", Quantity: 1, Price: 100, Leverage: 5, RiskUSD: 20, Success: true, Timestamp: now},
				{Action: "open_rejected", Symbol: "ETHUSDT", Success: false, GateState: "block", GateReasons: []string{"BTC闪崩"}, Timestamp: now},
			},
		},
		{
			Timestamp: now.Add(time.Hour),
			Decisions: []DecisionAction{
				{Action: "auto_close_long", Symbol: "BTCUSDT", Price: 110, Success: true, Timestamp: now.Add(time.Hour)},
			},
		},
	}

	report := BuildReplayReport(records, true, true)
	if report.OpenAttempts != 1 {
		t.Fatalf("开仓次数错误: %+v", report)
	}
	if report.RejectedOpenCount != 1 || report.RejectionReasons["BTC闪崩"] != 1 {
		t.Fatalf("拒绝原因统计错误: %+v", report)
	}
	if report.TheoreticalPnL != 10 {
		t.Fatalf("理论PnL错误: %.4f", report.TheoreticalPnL)
	}
	if !report.ReportOnly || !report.DryRun {
		t.Fatalf("replay模式标记错误: %+v", report)
	}
}

func TestLoadDecisionRecordsRecursive(t *testing.T) {
	dir := t.TempDir()
	traderDir := filepath.Join(dir, "trader-a")
	if err := os.MkdirAll(traderDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	content := `{"timestamp":"2026-05-06T14:00:00Z","success":true,"decisions":[]}`
	if err := os.WriteFile(filepath.Join(traderDir, "decision_20260506_140000_cycle1.json"), []byte(content), 0644); err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(traderDir, "ignore.json"), []byte(content), 0644); err != nil {
		t.Fatalf("写入忽略文件失败: %v", err)
	}

	records, err := LoadDecisionRecordsRecursive(dir)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("应只读取decision日志，实际=%d", len(records))
	}
}
