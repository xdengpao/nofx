package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteDailySummaryAggregatesTraderRecords(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)

	writeDecisionRecord(t, dir, "decision_20260520_010000_cycle1.json", DecisionRecord{
		Timestamp:         day.Add(time.Hour),
		WaitReasonSummary: "pilot_below_threshold",
		AccountState:      AccountSnapshot{TotalBalance: 100},
		CandidateDetails:  []CandidateSnapshot{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}},
		RiskState: &RiskStateSnapshot{
			TraderID:   "trader-a",
			ActiveMode: "normal",
			Suppressions: map[string]any{
				"total_active": float64(1),
			},
		},
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "BTCUSDT", Success: true, Timestamp: day.Add(time.Hour)},
			{Action: "open_rejected", Symbol: "ETHUSDT", Success: false, Timestamp: day.Add(time.Hour)},
		},
	})
	writeDecisionRecord(t, dir, "decision_20260520_020000_cycle2.json", DecisionRecord{
		Timestamp:         day.Add(2 * time.Hour),
		WaitReasonSummary: "no_signal",
		AccountState:      AccountSnapshot{TotalBalance: 110},
		CandidateDetails:  []CandidateSnapshot{{Symbol: "SOLUSDT"}},
		RiskState:         &RiskStateSnapshot{TraderID: "trader-a", ActiveMode: "loosen"},
	})
	writeDecisionRecord(t, dir, "decision_20260520_030000_cycle3.json", DecisionRecord{
		Timestamp:    day.Add(3 * time.Hour),
		AccountState: AccountSnapshot{TotalBalance: 210},
		RiskState:    &RiskStateSnapshot{TraderID: "trader-b", ActiveMode: "loosen"},
		Decisions:    []DecisionAction{{Action: "open_long", Symbol: "BNBUSDT", Success: true, Timestamp: day.Add(3 * time.Hour)}},
	})
	if err := os.WriteFile(filepath.Join(dir, "decision_20260520_bad.json"), []byte("{bad json"), 0644); err != nil {
		t.Fatalf("写入坏日志失败: %v", err)
	}
	writeDecisionRecord(t, dir, "decision_20260521_010000_cycle1.json", DecisionRecord{
		Timestamp:    day.AddDate(0, 0, 1).Add(time.Hour),
		AccountState: AccountSnapshot{TotalBalance: 999},
		RiskState:    &RiskStateSnapshot{TraderID: "trader-a"},
	})

	if err := WriteDailySummary("trader-a", day, dir); err != nil {
		t.Fatalf("生成daily summary失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "daily_summary_20260520.json"))
	if err != nil {
		t.Fatalf("读取daily summary失败: %v", err)
	}
	var summary DailySummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("解析daily summary失败: %v", err)
	}
	if summary.CycleCount != 2 || summary.OpenCount != 1 || summary.OpenRejected != 1 || summary.SignalCount != 3 {
		t.Fatalf("聚合计数错误: %+v", summary)
	}
	if summary.WaitReasonHist["pilot_below_threshold"] != 1 || summary.WaitReasonHist["no_signal"] != 1 {
		t.Fatalf("wait reason聚合错误: %+v", summary.WaitReasonHist)
	}
	if summary.AccountStartEnd[0] != 100 || summary.AccountStartEnd[1] != 110 {
		t.Fatalf("余额首尾错误: %+v", summary.AccountStartEnd)
	}
	if summary.LoosenModeEnters != 1 {
		t.Fatalf("loosen进入次数错误: %+v", summary)
	}
	if summary.SuppressionFinal == nil {
		t.Fatalf("应保留最后一次suppression快照: %+v", summary)
	}
}

func writeDecisionRecord(t *testing.T, dir, name string, record DecisionRecord) {
	t.Helper()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("序列化fixture失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatalf("写入fixture失败: %v", err)
	}
}
