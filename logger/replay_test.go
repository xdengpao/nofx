package logger

import (
	"os"
	"path/filepath"
	"strings"
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

func TestBuildReplayReport_AccountSemanticsMixedOldNewLogs(t *testing.T) {
	now := time.Now()
	records := []*DecisionRecord{
		{
			Timestamp:    now.Add(-time.Hour),
			AccountState: AccountSnapshot{TotalBalance: 100},
		},
		{
			Timestamp: now,
			AccountState: AccountSnapshot{
				TotalBalance:           110,
				CostBasis:              108,
				StrategyBaseline:       108,
				BaselineSource:         "trade_logs_plus_unrealized",
				EquitySource:           "exchange_balance",
				AllocationEnabled:      true,
				AllocatedBalance:       50,
				AllocatedAvailable:     40,
				AllocatedUsedMargin:    10,
				SizingEquity:           50,
				SizingAvailableBalance: 40,
				SizingEquitySource:     "allocated_balance",
				RiskDenominator:        50,
				RiskDenominatorSource:  "allocated_balance",
			},
		},
	}

	report := BuildReplayReport(records, false, false)
	if report.AccountSemantics == nil {
		t.Fatal("应输出account_semantics摘要")
	}
	if report.AccountSemantics.LastStrategyBaseline != 108 ||
		report.AccountSemantics.BaselineSource != "trade_logs_plus_unrealized" ||
		report.AccountSemantics.EquitySource != "exchange_balance" {
		t.Fatalf("baseline/equity语义摘要错误: %+v", report.AccountSemantics)
	}
	if !report.AccountSemantics.AllocationEnabled ||
		report.AccountSemantics.AllocatedBalance != 50 ||
		report.AccountSemantics.SizingEquitySource != "allocated_balance" {
		t.Fatalf("allocation语义摘要错误: %+v", report.AccountSemantics)
	}
}

func TestBuildOpenRejectionDailyReport(t *testing.T) {
	now := time.Date(2026, 5, 19, 17, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp:   now,
		CycleNumber: 7,
		Decisions: []DecisionAction{{
			Action:      "open_rejected",
			Symbol:      "BTCUSDT",
			Success:     false,
			Error:       "BTCUSDT open_short 被拒: 剩余净RR 2.30低于阈值2.50，当前价77015.800000 止损77384.600000 止盈76014.200000",
			GateReasons: []string{"remaining_net_rr_too_low"},
			GateDiagnostics: map[string]any{
				"remaining_net_rr": 2.3,
			},
			StrategyMetadata: map[string]any{
				"min_remaining_net_rr": 2.5,
			},
			Timestamp: now,
		}},
		StrategyDiagnostics: map[string]any{
			"messages": []any{
				"BNBUSDT sell2 结构信号不进入开仓: 入场追价比例0.37超过阈值0.35",
				"ETHUSDT sell2 作为结构背景保留，direct_structure_open关闭，等待15m fresh entry trigger",
			},
		},
	}}

	report := BuildOpenRejectionDailyReport(records, 10)
	if report.RejectedOpenCount != 1 || report.DiagnosticCount != 2 {
		t.Fatalf("日报计数错误: %+v", report)
	}
	if report.ByReason["remaining_net_rr_too_low"] != 1 || report.ByReason["entry_chase_ratio_too_high"] != 1 ||
		report.ByReason["structure_background_only"] != 1 {
		t.Fatalf("reason聚合错误: %+v", report.ByReason)
	}
	if report.BySymbol["BTCUSDT"] != 1 || report.BySymbol["BNBUSDT"] != 1 || report.BySymbol["ETHUSDT"] != 1 {
		t.Fatalf("symbol聚合错误: %+v", report.BySymbol)
	}
	if len(report.NearMisses) != 2 || report.NearMisses[0].Metric != "entry_chase_ratio" || report.NearMisses[0].Gap < 0.019 ||
		report.NearMisses[0].Gap > 0.021 {
		t.Fatalf("near miss排序/解析错误: %+v", report.NearMisses)
	}
}

func TestBuildOpenRejectionDailyReportVersionDiagnosticHistoricalOnly(t *testing.T) {
	now := time.Date(2026, 5, 31, 8, 0, 0, 0, time.UTC)
	currentStart := now.Add(time.Hour)
	records := []*DecisionRecord{
		{
			Timestamp:    now,
			CycleNumber:  88,
			DecisionMode: "chanlun_v2",
		},
		{
			Timestamp:    currentStart,
			CycleNumber:  1,
			DecisionMode: "chanlun_v2",
			StrategyDiagnostics: map[string]any{
				"active_mode":            "balanced",
				"effective_entry_timing": map[string]any{"min_remaining_net_rr": 1.5},
			},
		},
	}

	report := BuildOpenRejectionDailyReport(records, 10)

	if !report.VersionDiagnosticMissing || report.VersionDiagnosticMissingCount != 1 {
		t.Fatalf("历史旧格式应保持全窗口missing语义: %+v", report)
	}
	if report.CurrentVersionDiagnosticMissing || report.CurrentVersionDiagnosticMissingCount != 0 {
		t.Fatalf("当前窗口新格式不应误报当前进程缺字段: %+v", report)
	}
	if report.CurrentVersionWindowStart != currentStart.Format(time.RFC3339) {
		t.Fatalf("当前重启窗口起点错误: got=%q want=%q", report.CurrentVersionWindowStart, currentStart.Format(time.RFC3339))
	}
	if !containsText(report.Notes, "historical_only") {
		t.Fatalf("应提示仅历史旧格式: %+v", report.Notes)
	}
}

func TestBuildOpenRejectionDailyReportVersionDiagnosticCurrentWindowMissing(t *testing.T) {
	now := time.Date(2026, 5, 31, 8, 0, 0, 0, time.UTC)
	currentStart := now.Add(time.Hour)
	records := []*DecisionRecord{
		{
			Timestamp:    now,
			CycleNumber:  12,
			DecisionMode: "chanlun_v2",
			StrategyDiagnostics: map[string]any{
				"active_mode":            "balanced",
				"effective_entry_timing": map[string]any{"min_remaining_net_rr": 1.5},
			},
		},
		{
			Timestamp:    currentStart,
			CycleNumber:  1,
			DecisionMode: "chanlun_v2",
			StrategyDiagnostics: map[string]any{
				"messages": []any{"SOLUSDT sell2 结构信号不进入开仓: 入场追价比例0.37超过阈值0.35"},
			},
		},
	}

	report := BuildOpenRejectionDailyReport(records, 10)

	if !report.VersionDiagnosticMissing || report.VersionDiagnosticMissingCount != 1 {
		t.Fatalf("当前窗口缺字段也应保持全窗口missing语义: %+v", report)
	}
	if !report.CurrentVersionDiagnosticMissing || report.CurrentVersionDiagnosticMissingCount != 1 {
		t.Fatalf("当前窗口缺字段应输出当前进程missing: %+v", report)
	}
	if report.CurrentVersionWindowStart != currentStart.Format(time.RFC3339) {
		t.Fatalf("当前重启窗口起点错误: got=%q want=%q", report.CurrentVersionWindowStart, currentStart.Format(time.RFC3339))
	}
	if !containsText(report.Notes, "建议确认运行进程已重启到当前HEAD") {
		t.Fatalf("应提示当前运行进程可能未部署HEAD: %+v", report.Notes)
	}
}

func containsText(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestBuildOpenRejectionDailyReport_ChanlunV2NoOpen(t *testing.T) {
	now := time.Date(2026, 5, 29, 13, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp:   now,
		SourcePath:  "/var/log/nofx/decision_20260529.json",
		CycleNumber: 17,
		Decisions: []DecisionAction{
			{Action: "wait", FinalAction: "wait", TradeIntent: "wait", Timestamp: now},
			{
				Action:      "open_rejected",
				FinalAction: "open_rejected",
				TradeIntent: "open_long",
				Symbol:      "BNBUSDT",
				Success:     false,
				Error:       "BNBUSDT open_long 被拒: BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单",
				GateReasons: []string{"btc"},
				GateDiagnostics: map[string]any{
					"reason_code": "btc",
					"min_confidence_override_applied": map[string]any{
						"rule":              "range_long",
						"from":              82,
						"to":                60,
						"actual_confidence": 70,
					},
					"btc": map[string]any{
						"confirmed_bearish": true,
						"four_hour_bearish": true,
						"one_hour_bearish":  true,
					},
				},
				Timestamp: now,
			},
		},
		StrategyDiagnostics: map[string]any{
			"messages": []any{
				"DOGEUSDT sell2 父结构终止: 剩余净RR 1.41低于阈值1.50",
				"ETHUSDT sell3 父结构观察窗口过期: 年龄17根15m超过16根",
				"SOLUSDT sell2 终态信号已静默: entry_rr_invalid",
				"BNBUSDT buy2 fresh entry trigger ready: chanlun_v2_entry:abc",
				"ADAUSDT sell2 作为父结构背景保留，等待15m fresh entry trigger",
			},
		},
	}}

	report := BuildOpenRejectionDailyReport(records, 10)
	noOpen := report.ChanlunV2NoOpen
	if noOpen == nil {
		t.Fatal("应输出Chanlun V2 no-open摘要")
	}
	if noOpen.ActionDistribution["wait"] != 1 || noOpen.FinalActionDistribution["open_rejected"] != 1 ||
		noOpen.TradeIntentDistribution["open_long"] != 1 {
		t.Fatalf("action/final_action/trade_intent分布错误: %+v", noOpen)
	}
	if noOpen.DirectTerminalCount != 2 || noOpen.DirectTerminalByReason["entry_rr_invalid"] != 1 ||
		noOpen.DirectTerminalByReason["entry_parent.watch_window_expired"] != 1 {
		t.Fatalf("直接终态统计错误: %+v", noOpen.DirectTerminalByReason)
	}
	if noOpen.SuppressedTerminalCount != 1 || noOpen.SuppressedTerminalByReason["entry_rr_invalid"] != 1 {
		t.Fatalf("静默终态统计错误: %+v", noOpen.SuppressedTerminalByReason)
	}
	if noOpen.RRDirectTerminalCount != 1 || noOpen.RRDirectBySymbol["DOGEUSDT"] != 1 ||
		noOpen.RRDirectBySignalType["sell2"] != 1 || noOpen.RRDirectByThreshold["sell2@1.50"] != 1 {
		t.Fatalf("RR直接终态统计错误: %+v", noOpen)
	}
	if len(noOpen.RRDirectSamples) != 1 || noOpen.RRDirectSamples[0].SourceFile == "" ||
		noOpen.RRDirectSamples[0].RemainingRR != 1.41 || noOpen.RRDirectSamples[0].Threshold != 1.5 {
		t.Fatalf("RR样本错误: %+v", noOpen.RRDirectSamples)
	}
	if noOpen.TriggerReadyCount != 1 || noOpen.TriggerReadyBySymbol["BNBUSDT"] != 1 ||
		noOpen.WaitingForTriggerCount != 1 {
		t.Fatalf("trigger统计错误: %+v", noOpen)
	}
	if noOpen.OpenGateRejectionCount != 1 || noOpen.OpenGateByReason["btc"] != 1 ||
		noOpen.BTCGateRejectionCount != 1 || len(noOpen.BTCGateDiagnostics) != 1 {
		t.Fatalf("BTC gate统计错误: %+v", noOpen)
	}
	if confirmed, _ := noOpen.BTCGateDiagnostics[0].BTC["confirmed_bearish"].(bool); !confirmed {
		t.Fatalf("BTC diagnostics应展开confirmed_bearish: %+v", noOpen.BTCGateDiagnostics[0])
	}
	if noOpen.ConfidenceOverrideCount != 1 || len(noOpen.ConfidenceOverrides) != 1 ||
		noOpen.ConfidenceOverrides[0].Rule != "range_long" || noOpen.ConfidenceOverrides[0].To != 60 {
		t.Fatalf("confidence override诊断统计错误: %+v", noOpen.ConfidenceOverrides)
	}
}

func TestBuildOpenRejectionDailyReport_FreshnessCompatibilityAudit(t *testing.T) {
	start := time.Date(2026, 5, 29, 17, 25, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	diagnostics := map[string]any{
		"active_mode":            "balanced",
		"effective_entry_timing": map[string]any{"min_remaining_net_rr": 2.0},
		"messages":               []any{},
	}
	records := []*DecisionRecord{
		{
			Timestamp:           start,
			DecisionMode:        "chanlun_v2",
			StrategyDiagnostics: diagnostics,
			Decisions:           []DecisionAction{{Action: "wait", FinalAction: "wait", TradeIntent: "wait", Success: true, Timestamp: start}},
		},
		{
			Timestamp:           end,
			SourcePath:          "decision_logs/aster_chanlun_v2/decision_20260531_023014_cycle561.json",
			CycleNumber:         561,
			DecisionMode:        "chanlun_v2",
			StrategyDiagnostics: diagnostics,
			Decisions: []DecisionAction{{
				Action:      "open_rejected",
				TradeIntent: "open_short",
				Symbol:      "DOGEUSDT",
				Success:     false,
				Error:       "DOGEUSDT open_short 被信号新鲜度门控拒绝: 剩余净RR 1.90低于阈值2.50",
				GateReasons: []string{"freshness_gate.rr_invalid"},
				GateDiagnostics: map[string]any{
					"reason_code":          "freshness_gate.rr_invalid",
					"remaining_net_rr":     1.90,
					"min_remaining_net_rr": 2.50,
				},
				StrategyMetadata: map[string]any{
					"parent_signal_type": "sell2",
				},
				Timestamp: end,
			}},
		},
	}

	report := BuildOpenRejectionDailyReportWithOptions(records, 10, OpenRejectionDailyOptions{
		ConfigSource: "config.json",
		SignalTypeMinRR: map[string]float64{
			"sell2": 1.1,
		},
	})
	if report.NoSuccessfulOpenHours < 47.99 || report.NoSuccessfulOpenHours > 48.01 {
		t.Fatalf("无成功开仓时长应覆盖整个周期: %.4f", report.NoSuccessfulOpenHours)
	}
	if report.VersionDiagnosticMissing {
		t.Fatal("已有active_mode/effective_entry_timing时不应提示版本诊断缺失")
	}
	if len(report.TopNoOpenBuckets) == 0 || report.TopNoOpenBuckets[0].Bucket != "freshness_rr" {
		t.Fatalf("应输出按计数排序的top bucket: %+v", report.TopNoOpenBuckets)
	}
	audit := report.FreshnessCompatibility
	if audit == nil || audit.CheckedCount != 1 || audit.WouldPassSignalTypeRR != 1 {
		t.Fatalf("应识别旧2.5阈值拒绝但sell2=1.1可通过的样本: %+v", audit)
	}
	if audit.UsedDefaultSignalTypeMinRR || !audit.ConfigProvided || audit.ConfigSource != "config.json" {
		t.Fatalf("应记录配置来源而不是默认阈值: %+v", audit)
	}
	if audit.BySymbol["DOGEUSDT"] != 1 || audit.BySignalType["sell2"] != 1 {
		t.Fatalf("兼容审计聚合错误: %+v", audit)
	}
	if len(audit.Samples) != 1 || audit.Samples[0].SignalTypeThreshold != 1.1 || audit.Samples[0].OldThreshold != 2.5 {
		t.Fatalf("兼容审计样本错误: %+v", audit.Samples)
	}
	if !audit.Samples[0].FreshnessWouldPassSignalTypeRR || audit.Samples[0].FinalRRPass || audit.Samples[0].FinalRRThreshold != 2.5 {
		t.Fatalf("兼容审计应说明entry通过不代表final RR通过: %+v", audit.Samples[0])
	}
}

func TestBuildOpenRejectionDailyReport_EnhancedNoOrderBucketsAndServiceLog(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp:   now,
		CycleNumber: 91,
		Decisions: []DecisionAction{
			{
				Action:      "open_rejected",
				FinalAction: "open_rejected",
				TradeIntent: "open_long",
				Symbol:      "ASTERUSDT",
				Success:     false,
				Error:       "ASTERUSDT open_long 被拒: ASTERUSDT 1h ADX 15.3低于profile阈值25.0，禁止趋势开仓（report-only）; BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单",
				GateReasons: []string{"btc"},
				GateDiagnostics: map[string]any{
					"reason_code": "btc",
					"btc": map[string]any{
						"confirmed_bearish": true,
					},
				},
				Timestamp: now,
			},
			{
				Action:      "open_rejected",
				FinalAction: "open_rejected",
				TradeIntent: "open_short",
				Symbol:      "DOGEUSDT",
				Success:     false,
				Error:       "DOGEUSDT open_short 被拒: 风险回报比过低(1.45:1 < 2.5:1)",
				StrategyMetadata: map[string]any{
					"parent_signal_type":   "sell2",
					"remaining_net_rr":     1.45,
					"min_remaining_net_rr": 1.1,
				},
				Timestamp: now.Add(time.Minute),
			},
		},
		StrategyDiagnostics: map[string]any{
			"active_mode":            "loosen",
			"effective_entry_timing": map[string]any{"min_remaining_net_rr": 1.1},
			"messages": []any{
				"ASTERUSDT 1h ADX 15.3低于profile阈值25.0，禁止趋势开仓（report-only）",
				"SOLUSDT sell2 终态信号已静默: entry_rr_invalid",
				"ETHUSDT sell3 父结构终止: 剩余净RR 0.58低于阈值1.00",
			},
		},
	}}

	report := BuildOpenRejectionDailyReportWithOptions(records, 10, OpenRejectionDailyOptions{
		EnabledTraders: 1,
		SignalTypeMinRR: map[string]float64{
			"sell2": 1.1,
		},
		ServiceLogSource: "nofx.log",
		ServiceLogText: strings.Join([]string{
			"创建订单追踪器: Aster Chanlun V2 Trader",
			"无自动成交订单",
		}, "\n"),
	})

	if report.EnabledTraderCount != 1 || report.RealExchangeOrderCount != 0 {
		t.Fatalf("执行路径统计错误: %+v", report)
	}
	if !report.ServiceLogProvided || report.TrackerOnlyOrderMentions != 1 || report.ServiceLogExchangeOrderMentions != 0 {
		t.Fatalf("service log订单文案审计错误: %+v", report)
	}
	if report.ByBucket["btc_hard_veto"] != 1 || report.ByBucket["adx_report_only"] != 1 ||
		report.ByBucket["final_rr"] != 1 || report.ByBucket["terminal_suppressed"] != 2 || report.ByBucket["rr"] != 0 {
		t.Fatalf("enhanced no-order bucket错误: %+v", report.ByBucket)
	}
	if !containsText(report.RemainingHardBlocks, "btc_hard_veto") || !containsText(report.RemainingHardBlocks, "final_rr_2.5") {
		t.Fatalf("应输出loosen后仍保留的硬阻断: %+v", report.RemainingHardBlocks)
	}
	if report.FinalValidationRRRejectionCount != 1 || report.FreshnessPassButFinalRRFailCount != 1 ||
		!report.FinalRRKept || report.FinalRRThreshold != 2.5 {
		t.Fatalf("final RR诊断错误: %+v", report)
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

func TestBuildReplayReport_StrategyDiseaseBucketsAndRMultiple(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp: now,
			Decisions: []DecisionAction{{
				Action:                 "open_long",
				Symbol:                 "BTCUSDT",
				Quantity:               1,
				Price:                  100,
				Success:                true,
				Timestamp:              now,
				ProfileName:            "btc_eth",
				RequestedStopLoss:      99.97,
				EffectiveStopLoss:      98,
				RequestedTakeProfit:    100.07,
				EffectiveTakeProfit:    104,
				ExchangeFullTakeProfit: 106,
				ExchangeFullTPMode:     "algorithmic_full",
				StopDistanceRatio:      0.02,
				TakeProfitRatio:        0.06,
				RiskNormalization: map[string]any{
					"rewritten_stop_loss":        true,
					"rewritten_take_profit":      true,
					"rewritten_exchange_full_tp": true,
				},
				GateDiagnostics: map[string]any{
					"adx_regime": map[string]any{
						"gate": "low_adx",
						"adx":  14.5,
					},
					"correlation_concentration": map[string]any{
						"same_side_high_corr": 1,
					},
				},
			}},
		},
		{
			Timestamp: now.Add(time.Hour),
			Decisions: []DecisionAction{{
				Action:      "close_long",
				Symbol:      "BTCUSDT",
				Quantity:    1,
				Price:       104,
				Success:     true,
				Timestamp:   now.Add(time.Hour),
				CloseSource: "take_profit",
				Reasoning:   "止盈成交",
			}},
		},
	}

	report := BuildReplayReport(records, true, true)
	disease := report.StrategyDisease
	if disease.MicroStopCount != 1 || disease.MicroTPCount != 1 {
		t.Fatalf("微止损/微止盈诊断错误: %+v", disease)
	}
	if disease.RewrittenStopCount != 1 || disease.RewrittenTPCount != 1 || disease.RewrittenExchangeFullTPCount != 1 {
		t.Fatalf("重写计数错误: %+v", disease)
	}
	if disease.LowADXEntryCount != 1 || disease.SameSideCorrelationCount != 1 {
		t.Fatalf("ADX/相关性病因计数错误: %+v", disease)
	}
	if disease.PrematureFullTPCount != 1 {
		t.Fatalf("应识别未到algorithmic full TP的提前整仓止盈: %+v", disease)
	}
	if disease.RMultipleStats.Count != 1 || disease.RMultipleStats.Average < 1.99 || disease.RMultipleStats.Average > 2.01 {
		t.Fatalf("R multiple统计错误: %+v", disease.RMultipleStats)
	}
	if disease.ByProfile["btc_eth"].MicroStopCount != 1 || disease.ByCloseReason["take_profit"].PrematureFullTPCount != 1 {
		t.Fatalf("分组病因统计错误: by_profile=%+v by_close=%+v", disease.ByProfile, disease.ByCloseReason)
	}
}

func TestBuildReplayReport_StrategyDiseaseRejectedBuckets(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: now,
		Decisions: []DecisionAction{
			{Action: "open_rejected", Symbol: "ETHUSDT", Success: false, Error: "ETHUSDT 1h ADX 14.5低于profile阈值25.0，禁止趋势开仓", Timestamp: now},
			{Action: "open_rejected", Symbol: "BCHUSDT", Success: false, Error: "BCHUSDT 1h ADX 31.2但DI方向与开仓方向不一致", Timestamp: now},
			{Action: "open_rejected", Symbol: "SOLUSDT", Success: false, Error: "已有同向高相关持仓集中，禁止继续叠加风险", Timestamp: now},
			{Action: "open_rejected", Symbol: "XAGUSDT", Success: false, Error: "profile不允许当前品种方向", Timestamp: now},
		},
	}}

	report := BuildReplayReport(records, true, true)
	if report.StrategyDisease.RejectedByADXCount != 2 {
		t.Fatalf("ADX拒绝计数错误: %+v", report.StrategyDisease)
	}
	if report.StrategyDisease.RejectedByCorrelationCount != 1 || report.StrategyDisease.RejectedByProfileCount != 1 {
		t.Fatalf("相关性/profile拒绝计数错误: %+v", report.StrategyDisease)
	}
	if report.RejectionBuckets["adx"] != 1 || report.RejectionBuckets["counter_di"] != 1 || report.RejectionBuckets["correlation"] != 1 || report.RejectionBuckets["profile"] != 1 {
		t.Fatalf("拒绝桶分类错误: %+v", report.RejectionBuckets)
	}
}

func TestBuildReplayReport_ClassifiesExchangeFullTPCloseReason(t *testing.T) {
	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp: now,
			Decisions: []DecisionAction{{
				Action:                 "open_long",
				Symbol:                 "ETHUSDT",
				Quantity:               1,
				Price:                  100,
				Success:                true,
				ProfileName:            "btc_eth",
				EffectiveStopLoss:      98,
				ExchangeFullTakeProfit: 106,
				Timestamp:              now,
			}},
		},
		{
			Timestamp: now.Add(time.Hour),
			Decisions: []DecisionAction{{
				Action:      "auto_close_long",
				Symbol:      "ETHUSDT",
				Quantity:    1,
				Price:       106,
				Success:     true,
				CloseSource: "order_tracker",
				Reasoning:   "EXCHANGE_FULL_TP",
				Timestamp:   now.Add(time.Hour),
			}},
		},
	}

	report := BuildReplayReport(records, true, true)
	group := report.StrategyDisease.ByCloseReason["exchange_full_tp"]
	if group == nil || group.TradeCount != 1 || group.RMultipleStats.Count != 1 {
		t.Fatalf("交易所整仓TP应单独归类: %+v", report.StrategyDisease.ByCloseReason)
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
