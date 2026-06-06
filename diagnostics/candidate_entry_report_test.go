package diagnostics

import (
	"testing"
	"time"

	"nofx/logger"
	"nofx/pool"
)

func TestBuildCandidateEntryEvaluationReportAggregatesFactsAndFunnel(t *testing.T) {
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	records := []*logger.DecisionRecord{
		{
			Timestamp:      now.Add(-time.Hour),
			CandidateCoins: []string{"BTCUSDT", "ETHUSDT"},
			Decisions: []logger.DecisionAction{
				{Action: "wait", FinalAction: "wait"},
				{Action: "open_rejected", Symbol: "ETHUSDT", TradeIntent: "open_long", GateReasons: []string{"remaining_net_rr_too_low"}},
			},
			StrategyDiagnostics: map[string]any{
				"entry_trigger_funnel": map[string]any{
					"raw_signal_count":           float64(3),
					"parent_structure_count":     float64(2),
					"waiting_for_trigger_count":  float64(1),
					"trigger_ready_count":        float64(1),
					"trigger_ready_by_type":      map[string]any{"pullback_retest_resume": float64(1)},
					"parent_terminal_by_reason":  map[string]any{"entry_rr_invalid": float64(1)},
					"terminal_suppressed_count":  float64(1),
					"trigger_rejected_by_reason": map[string]any{"entry_trigger_low_confidence": float64(1)},
					"per_symbol": map[string]any{
						"ETHUSDT": map[string]any{
							"raw_signal_count":          float64(2),
							"parent_structure_count":    float64(1),
							"waiting_for_trigger_count": float64(1),
						},
					},
				},
			},
		},
	}

	report := BuildCandidateEntryEvaluationReport(records, CandidateEntryEvaluationOptions{
		TraderID:           "t1",
		DryRun:             true,
		StaticSymbols:      []string{"SOLUSDT"},
		EnabledTraderCount: 2,
		GeneratedAt:        now,
	})

	if report.Window.RecordCount != 1 || report.RuntimeFacts.EnabledTraderCount != 2 {
		t.Fatalf("运行事实聚合不符合预期: %+v", report.RuntimeFacts)
	}
	if report.RuntimeFacts.OpenLikeCandidateCount != 1 || report.RuntimeFacts.OpenRejectedCount != 1 || report.RuntimeFacts.WaitCount != 1 {
		t.Fatalf("动作分布聚合不符合预期: %+v", report.RuntimeFacts)
	}
	if report.StaticPool.SymbolCount != 3 {
		t.Fatalf("静态候选池应合并配置和日志候选: %+v", report.StaticPool)
	}
	if report.EntryTriggerFunnel.TriggerReadyCount != 1 || report.EntryTriggerFunnel.OpenRejectionByReason["final_rr"] != 1 {
		t.Fatalf("entry trigger漏斗或拒绝分类不符合预期: %+v", report.EntryTriggerFunnel)
	}
	if report.EntryTriggerFunnel.PerSymbol["ETHUSDT"].WaitingForTriggerCount != 1 {
		t.Fatalf("per_symbol漏斗合并不符合预期: %+v", report.EntryTriggerFunnel.PerSymbol)
	}
	if report.OpenProbability.Qualitative == "" || len(report.OpenProbability.Notes) == 0 {
		t.Fatalf("开仓概率分层说明缺失: %+v", report.OpenProbability)
	}
}

func TestBuildEntryTriggerFunnelReportFallsBackToLegacyDiagnostics(t *testing.T) {
	records := []*logger.DecisionRecord{{
		Timestamp: time.Now(),
		StrategyDiagnostics: map[string]any{
			"messages": []any{
				"ETHUSDT buy3 父结构终止: 剩余净RR 1.20低于阈值2.50",
				"SOLUSDT sell3 作为父结构背景保留，等待15m fresh entry trigger",
				"BNBUSDT buy2 fresh entry trigger ready: trigger-1",
			},
		},
	}}

	report := BuildEntryTriggerFunnelReport(records)

	if report.FieldMissingCount != 1 {
		t.Fatalf("旧日志缺结构化字段时应记录缺字段计数: %+v", report)
	}
	if report.ParentTerminalByReason["entry_rr_invalid"] != 1 || report.WaitingForTriggerCount != 1 || report.TriggerReadyCount != 1 {
		t.Fatalf("旧日志文案解析不符合预期: %+v", report)
	}
}

func TestBuildCandidateEntryEvaluationReportIncludesShortSidePreview(t *testing.T) {
	snapshot := &pool.DynamicCandidatePool{
		MarketRegime: "risk_off",
		ShortSideSummary: pool.ShortSideSummary{
			Enabled:        true,
			ReportOnly:     true,
			BTCWeak:        true,
			CandidateCount: 1,
			PromptCount:    1,
			Symbols:        []string{"ETHUSDT"},
		},
		Symbols: []pool.DynamicCandidate{{
			Symbol: "ETHUSDT",
			Tier:   "satellite",
			Score:  72,
			SideProfile: pool.CandidateSideProfile{
				Bias:       "short",
				ShortScore: 74,
				Reasons:    []string{"ETHUSDT short-side候选"},
				ReportOnly: true,
			},
		}},
	}
	merged := &pool.MergedCoinPool{AllSymbols: []string{"ETHUSDT"}}

	report := BuildCandidateEntryEvaluationReport(nil, CandidateEntryEvaluationOptions{
		DryRun:            true,
		DynamicSnapshot:   snapshot,
		DynamicMergedPool: merged,
	})

	if !report.ShortSideCoverage.Enabled || !report.ShortSideCoverage.ReportOnly || !report.ShortSideCoverage.BTCWeak {
		t.Fatalf("short-side覆盖报告缺失: %+v", report.ShortSideCoverage)
	}
	if report.DynamicPoolPreview.ShortSideCandidateCount != 1 || report.DynamicPoolPreview.ShortSidePromptCount != 1 {
		t.Fatalf("动态池short-side摘要不符合预期: %+v", report.DynamicPoolPreview)
	}
}
