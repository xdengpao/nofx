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
				{Action: "open_rejected", Symbol: "ETHUSDT", Success: false, GateState: "block", GateReasons: []string{"ADX偏高"}, Timestamp: now},
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
	if report.RejectedOpenCount != 1 || report.RejectionReasons["ADX偏高"] != 1 {
		t.Fatalf("拒绝原因统计错误: %+v", report)
	}
	if report.TheoreticalPnL != 10 {
		t.Fatalf("理论PnL错误: %.4f", report.TheoreticalPnL)
	}
	if !report.ReportOnly || !report.DryRun {
		t.Fatalf("replay模式标记错误: %+v", report)
	}
	if report.ReportOnlySimulationSources["text_inferred"] != 1 {
		t.Fatalf("旧日志拒绝应标注text_inferred模拟来源: %+v", report.ReportOnlySimulationSources)
	}
	if report.ReportOnlySimulationSymbols["ETHUSDT"] != 1 {
		t.Fatalf("应统计report-only symbol分布: %+v", report.ReportOnlySimulationSymbols)
	}
}

func TestBuildReplayReport_UsesStructuredSimulationSource(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: now,
		Decisions: []DecisionAction{{
			Action:    "open_rejected",
			Symbol:    "BCHUSDT",
			Success:   false,
			Error:     "ADX偏高",
			Timestamp: now,
			Simulations: []OpenFrequencySimulationSnapshot{{
				Scenario:   "high_adx_active_candidate",
				Source:     "structured",
				WouldAllow: true,
			}},
		}},
	}}

	report := BuildReplayReport(records, true, true)
	if report.ReportOnlySimulationCount != 1 {
		t.Fatalf("应统计结构化simulation: %+v", report)
	}
	if report.ReportOnlySimulationSources["structured"] != 1 {
		t.Fatalf("结构化来源统计错误: %+v", report.ReportOnlySimulationSources)
	}
	if report.ReportOnlySimulationSymbols["BCHUSDT"] != 1 {
		t.Fatalf("结构化symbol分布统计错误: %+v", report.ReportOnlySimulationSymbols)
	}
}

func TestBuildReplayReport_DetectsDuplicateCloseAndBalanceDelta(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp:    now,
			AccountState: AccountSnapshot{TotalBalance: 200},
			RiskState:    &RiskStateSnapshot{TraderID: "trader-a"},
			Decisions: []DecisionAction{
				{Action: "open_long", Symbol: "BTCUSDT", Quantity: 1, Price: 100, Leverage: 5, Success: true, Timestamp: now},
			},
		},
		{
			Timestamp:    now.Add(10 * time.Minute),
			AccountState: AccountSnapshot{TotalBalance: 198.5},
			RiskState:    &RiskStateSnapshot{TraderID: "trader-a"},
			Decisions: []DecisionAction{
				{Action: "close_long", Symbol: "BTCUSDT", Quantity: 1, Price: 98, Success: true, Timestamp: now.Add(10 * time.Minute)},
			},
		},
		{
			Timestamp:    now.Add(13 * time.Minute),
			AccountState: AccountSnapshot{TotalBalance: 197.8},
			RiskState:    &RiskStateSnapshot{TraderID: "trader-a"},
			Decisions: []DecisionAction{
				{Action: "auto_close_long", Symbol: "BTCUSDT", Quantity: 1, Price: 98, Success: true, CloseSource: "snapshot", Timestamp: now.Add(13 * time.Minute)},
			},
		},
	}

	report := BuildReplayReport(records, true, true)
	if report.RawCloseActions != 2 || report.DeduplicatedCloseActions != 1 || report.DuplicateCloseCount != 1 {
		t.Fatalf("重复close统计错误: %+v", report)
	}
	if report.DuplicateCloseGroups["BTCUSDT_long"] != 1 {
		t.Fatalf("重复close分组错误: %+v", report.DuplicateCloseGroups)
	}
	if report.FirstBalance != 200 || report.LastBalance != 197.8 || report.BalanceDelta > -2.19 || report.BalanceDelta < -2.21 {
		t.Fatalf("余额delta错误: first=%.4f last=%.4f delta=%.4f", report.FirstBalance, report.LastBalance, report.BalanceDelta)
	}
}

func TestBuildReplayReport_RejectionBuckets(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: now,
		Decisions: []DecisionAction{
			{Action: "open_rejected", Symbol: "ETHUSDT", Success: false, GateReasons: []string{"风险回报比不足: net RR < 2.5"}, Timestamp: now},
			{Action: "open_rejected", Symbol: "XAGUSDT", Success: false, GateReasons: []string{"BTC 1h/4h bearish，高beta多头禁止"}, Timestamp: now},
			{Action: "open_rejected", Symbol: "BNBUSDT", Success: false, GateReasons: []string{"置信度低于90"}, Timestamp: now},
		},
	}}

	report := BuildReplayReport(records, true, true)
	if report.RejectionBuckets["rr"] != 1 || report.RejectionBuckets["btc_gate"] != 1 || report.RejectionBuckets["confidence"] != 1 {
		t.Fatalf("拒绝原因桶错误: %+v", report.RejectionBuckets)
	}
	if report.RRRejectionRate < 33 || report.RRRejectionRate > 34 {
		t.Fatalf("RR拒绝率错误: %.4f", report.RRRejectionRate)
	}
}

func TestAttachExchangeCloseSnapshots(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: now,
		RiskState: &RiskStateSnapshot{TraderID: "trader-a"},
		Decisions: []DecisionAction{
			{Action: "close_short", Symbol: "ETHUSDT", OrderID: 7, Success: true, Timestamp: now},
			{Action: "auto_close_long", Symbol: "BTCUSDT", OrderID: 8, Success: true, Timestamp: now.Add(time.Minute)},
		},
	}}
	report := BuildReplayReport(records, true, true)
	AttachExchangeCloseSnapshots(&report, records, []ExchangeCloseSnapshot{
		{TraderID: "trader-a", Symbol: "ETHUSDT", Side: "short", OrderID: 7, CloseTime: now},
		{TraderID: "trader-a", Symbol: "SOLUSDT", Side: "long", OrderID: 9, CloseTime: now},
	})

	if report.ExchangeReconciliation == nil {
		t.Fatal("应生成交易所对账结果")
	}
	got := report.ExchangeReconciliation
	if got.MatchedCloseCount != 1 || got.MissingInLogsCount != 1 || got.MissingInExchangeCount != 1 {
		t.Fatalf("交易所对账统计错误: %+v", got)
	}
}

func TestFrequencyHelpers(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: now,
		RiskState: &RiskStateSnapshot{TraderID: "trader-a"},
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "BTCUSDT", Success: true, Timestamp: now.Add(-time.Hour)},
			{Action: "open_short", Symbol: "ETHUSDT", Success: false, Timestamp: now.Add(-time.Hour)},
		},
	}}

	if got := CountSuccessfulOpens(records, now.Add(-2*time.Hour), "trader-a"); got != 1 {
		t.Fatalf("成功开仓计数错误: %d", got)
	}

	stats := BuildRecentClosedTradeStats([]TradeOutcome{
		{Symbol: "BTCUSDT", PnL: 2, CloseTime: now.Add(-90 * time.Minute)},
		{Symbol: "ETHUSDT", PnL: -4, CloseTime: now.Add(-30 * time.Minute)},
	}, now.Add(-2*time.Hour))
	if stats.ClosedTrades != 2 || stats.ProfitFactor != 0.5 || stats.MaxDrawdownUSD != 4 {
		t.Fatalf("闭合交易统计错误: %+v", stats)
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
	if records[0].SourcePath == "" {
		t.Fatalf("replay应保留源文件路径")
	}
}

func TestFilterReplayRecords_ExcludesBackupsAndFiltersTrader(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{Timestamp: now, SourcePath: "/logs/trader-a/decision_1.json"},
		{Timestamp: now, SourcePath: "/logs/trader-a.bak/decision_2.json"},
		{Timestamp: now, SourcePath: "/logs/trader-b/decision_3.json"},
	}

	filtered := FilterReplayRecords(records, ReplayFilter{TraderID: "trader-a"})
	if len(filtered) != 1 || filtered[0].SourcePath != "/logs/trader-a/decision_1.json" {
		t.Fatalf("应排除备份目录并按trader过滤: %+v", filtered)
	}

	withBackups := FilterReplayRecords(records, ReplayFilter{TraderID: "trader-a", IncludeBackups: true})
	if len(withBackups) != 1 {
		t.Fatalf(".bak目录名不应被误识别为trader-a: %+v", withBackups)
	}
}

func TestBuildRollingPerformance_GlobalBlockAfterPoorRecent20(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	outcomes := make([]TradeOutcome, 20)
	for i := range outcomes {
		outcomes[i] = TradeOutcome{
			Symbol:    "BTCUSDT",
			Side:      "long",
			PnL:       -1,
			CloseTime: now.Add(-time.Duration(20-i) * time.Minute),
		}
	}

	snapshot := BuildRollingPerformance(outcomes, now)
	if snapshot.GlobalGate.State != "block" {
		t.Fatalf("最近20笔表现过差应全局暂停开仓: %+v", snapshot.GlobalGate)
	}
	if snapshot.GlobalGate.RiskMultiplier != 0 {
		t.Fatalf("全局暂停时风险倍率应为0: %+v", snapshot.GlobalGate)
	}
}
