package optimize

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"nofx/backtest"
	"nofx/decision"
	"nofx/logger"
)

func TestLoadOptimizationConfigDefaultsAndOverridePolicy(t *testing.T) {
	cfg, err := LoadOptimizationConfig("")
	if err != nil {
		t.Fatalf("默认配置不应失败: %v", err)
	}
	if cfg.MaxDrawdownRelativeTolerance != DefaultMaxDrawdownRelativeTolerance ||
		cfg.ProfitFactorRelativeFloor != DefaultProfitFactorRelativeFloor ||
		cfg.BootstrapIterations != DefaultBootstrapIterations {
		t.Fatalf("默认阈值错误: %+v", cfg)
	}

	path := filepath.Join(t.TempDir(), "optimization.json")
	if err := os.WriteFile(path, []byte(`{"max_drawdown_relative_tolerance":1.2}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOptimizationConfig(path); !errors.Is(err, ErrUncommittedPolicyOverride) {
		t.Fatalf("覆盖阈值缺少policy应失败，实际=%v", err)
	}

	if err := os.WriteFile(path, []byte(`{"max_drawdown_relative_tolerance":1.2,"policy_ref":"spec://gate","policy_commit":"abc123"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadOptimizationConfig(path)
	if err != nil {
		t.Fatalf("带policy覆盖应通过: %v", err)
	}
	if cfg.PolicyRef != "spec://gate" || cfg.PolicyCommit != "abc123" {
		t.Fatalf("policy元数据未保留: %+v", cfg)
	}
}

func TestLoadRunArtifactsDerivesMetricsAndKeepsArtifactSource(t *testing.T) {
	dir := makeRunFixture(t, "run-artifact", "hash-a", true)
	artifacts, err := LoadRunArtifacts(dir)
	if err != nil {
		t.Fatalf("加载run artifacts失败: %v", err)
	}
	if artifacts.Metrics == nil {
		t.Fatal("应生成metrics")
	}
	if artifacts.Metrics.MetricsSource != "artifact" {
		t.Fatalf("metrics_source错误: %s", artifacts.Metrics.MetricsSource)
	}
	if artifacts.Metrics.DataHash != "hash-a" || artifacts.Metrics.TraderID != "t1" || artifacts.Metrics.Exchange != "binance" {
		t.Fatalf("上下文字段错误: %+v", artifacts.Metrics)
	}
	if artifacts.Metrics.MinNotionalRejectionRate != 1 {
		t.Fatalf("最小名义额拒绝率应为1，实际=%.4f", artifacts.Metrics.MinNotionalRejectionRate)
	}
	if artifacts.Metrics.BootstrapCIs["net_pnl"].Status != "ok" {
		t.Fatalf("bootstrap应可用: %+v", artifacts.Metrics.BootstrapCIs)
	}

	dir = makeRunFixture(t, "run-derived", "hash-b", false)
	artifacts, err = LoadRunArtifacts(dir)
	if err != nil {
		t.Fatalf("加载缺metrics旧run应派生: %v", err)
	}
	if artifacts.Metrics.MetricsSource != "derived" {
		t.Fatalf("旧run metrics_source应为derived，实际=%s", artifacts.Metrics.MetricsSource)
	}
}

func TestLoadRunArtifactsMissingStructuresIsCheckable(t *testing.T) {
	dir := makeRunFixture(t, "run-no-structures", "hash-a", true)
	if err := os.Remove(filepath.Join(dir, "structures.json")); err != nil {
		t.Fatal(err)
	}
	artifacts, err := LoadRunArtifacts(dir)
	if !errors.Is(err, ErrMissingStructureSnapshots) {
		t.Fatalf("缺structures应返回可检查错误，实际=%v", err)
	}
	if artifacts == nil || artifacts.Report.RunID != "run-no-structures" {
		t.Fatalf("应保留已加载artifact上下文: %+v", artifacts)
	}
}

func TestResolveCanonicalLogFieldsFallbackAndMissing(t *testing.T) {
	action := logger.DecisionAction{
		SignalID:        "top-signal",
		SignalType:      "buy2",
		SignalTimeframe: "1h",
		StructureTarget: 100,
		ConfigHash:      "cfg",
		StrategyVersion: "v1",
		StrategyMetadata: map[string]any{
			"structure_key":       "sk",
			"parent_signal_id":    "parent",
			"entry_trigger_id":    "trigger",
			"source_layer":        "main",
			"trigger_timeframe":   "15m",
			"signal_close_time":   int64(1000),
			"decision_close_time": int64(2000),
			"age_candles":         1,
			"freshness_state":     "fresh",
			"reason_code":         "ok",
		},
	}
	fields, errs := ResolveCanonicalLogFields(action)
	if len(errs) != 0 {
		t.Fatalf("字段完整时不应报错: %+v", errs)
	}
	if fields.SignalID != "top-signal" || fields.StructureKey != "sk" || fields.TriggerTimeframe != "15m" {
		t.Fatalf("字段解析优先级错误: %+v", fields)
	}

	_, errs = ResolveCanonicalLogFields(logger.DecisionAction{})
	if len(errs) == 0 {
		t.Fatal("缺必填字段应返回结构化错误")
	}
}

func TestBuildDefectCatalogAggregatesReplayAndBacktest(t *testing.T) {
	dir := makeRunFixture(t, "run-defect", "hash-a", true)
	artifacts, err := LoadRunArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := BuildDefectCatalog([]ReplayReportInput{{
		Report: logger.ReplayReport{StrategyDisease: logger.StrategyDiseaseReport{MicroStopCount: 2}},
		Path:   "decision_logs/t1/replay.json", TraderID: "t1", Exchange: "binance", RunID: "replay-1",
	}}, []BacktestRunInput{{
		Artifacts: artifacts, TraderID: "t1", Exchange: "binance",
	}}, DefaultOptimizationConfig())
	if err != nil {
		t.Fatalf("构建缺陷目录失败: %v", err)
	}
	if catalog.Summary.TotalDefects < 2 {
		t.Fatalf("应包含replay和backtest缺陷: %+v", catalog.Summary)
	}
	if catalog.Summary.ByTraderExchange["t1|binance"] == 0 {
		t.Fatalf("trader/exchange聚合缺失: %+v", catalog.Summary)
	}
}

func TestBuildDefectCatalogRejectsMissingContext(t *testing.T) {
	catalog, err := BuildDefectCatalog([]ReplayReportInput{{
		Report: logger.ReplayReport{StrategyDisease: logger.StrategyDiseaseReport{MicroStopCount: 1}},
		Path:   "replay.json",
	}}, nil, DefaultOptimizationConfig())
	if !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("缺context应导致证据不足，实际=%v", err)
	}
	if catalog.Summary.RejectedNoEvidenceCount != 1 {
		t.Fatalf("应记录拒绝数量: %+v", catalog.Summary)
	}
}

func TestEvaluateGateBoundariesAndCoverage(t *testing.T) {
	base := comparableMetrics("base", "hash-a")
	candidate := comparableMetrics("candidate", "hash-a")
	candidate.MaxDrawdownPct = 11
	candidate.ProfitFactor = 1.9
	candidate.MinNotionalRejectionRate = 0.05
	result, err := EvaluateGate(GateInput{Baseline: base, Candidate: candidate, Config: DefaultOptimizationConfig()})
	if err != nil {
		t.Fatalf("边界值不应失败: %v", err)
	}
	if result.Verdict != "approved" {
		t.Fatalf("边界值应通过: %+v", result)
	}

	candidate = comparableMetrics("candidate", "hash-b")
	result, err = EvaluateGate(GateInput{Baseline: base, Candidate: candidate, Config: DefaultOptimizationConfig()})
	if !errors.Is(err, ErrIncomparableRuns) || result.Verdict != "invalid_input" {
		t.Fatalf("data_hash不一致应不可比: result=%+v err=%v", result, err)
	}

	cfg := DefaultOptimizationConfig()
	cfg.MaxDrawdownRelativeTolerance = 1.2
	result, err = EvaluateGate(GateInput{Baseline: base, Candidate: comparableMetrics("candidate", "hash-a"), Config: cfg})
	if !errors.Is(err, ErrUncommittedPolicyOverride) || result.Verdict != "invalid_input" {
		t.Fatalf("未提交policy覆盖应失败: result=%+v err=%v", result, err)
	}

	coverage := RequirementCoverageSummary{Items: []RequirementCoverageItem{{Requirement: "R12", Status: "missing", Severity: "high"}}}
	result, err = EvaluateGate(GateInput{Baseline: base, Candidate: comparableMetrics("candidate", "hash-a"), Config: DefaultOptimizationConfig(), RequirementCoverage: &coverage})
	if err != nil || result.Verdict != "invalid_input" {
		t.Fatalf("高严重度coverage缺失应阻断approved: result=%+v err=%v", result, err)
	}
}

func TestSignalQualityEquityConsistencyAndProposalRolloutReport(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	signalReport, err := BuildSignalQualityReport("run-1", SignalQualityInput{
		Signals: []backtest.SignalOutcome{{SignalID: "sig-1", Reached1R: true}},
		Structures: []StructureSnapshot{{
			SnapshotID: "s1", SignalID: "sig-1", SignalType: "buy2", SourceLayer: "main", Timeframe: "1h",
			SegmentEndMS: now.Add(-time.Hour).UnixMilli(), ConfirmCloseMS: now.UnixMilli(), ATRProfile: "medium", ADXRange: "20-30",
		}},
		Trades: []backtest.TradeLifecycle{{SignalID: "sig-1", RealizedPnL: 20, RMultiple: 2, FinalRMultiple: 2}},
	})
	if err != nil {
		t.Fatalf("信号质量报告失败: %v", err)
	}
	if len(signalReport.Metrics) != 1 || signalReport.Metrics[0].OneRHitRate != 1 || signalReport.Metrics[0].FinalRMultiple != 2 {
		t.Fatalf("信号质量指标错误: %+v", signalReport)
	}

	equity := AnalyzeEquityCurve("run-1", []backtest.EquityPoint{
		{Timestamp: now, Equity: 100, RealizedPnL: 0},
		{Timestamp: now.Add(24 * time.Hour), Equity: 90, RealizedPnL: -10, DrawdownPct: 10},
	}, []backtest.TradeLifecycle{{LifecycleID: "lc-1", SignalType: "buy2", ExitTime: now.Add(24 * time.Hour), RealizedPnL: -10}})
	if len(equity.DailyEquity) != 2 || len(equity.DrawdownIntervals) != 1 || equity.DrawdownIntervals[0].LifecycleID != "lc-1" {
		t.Fatalf("资金曲线分析错误: %+v", equity)
	}

	consistency := CompareReplayBacktest("replay", "backtest", comparableMetrics("r", "hash-a"), comparableMetrics("b", "hash-a"), 0.05)
	if !consistency.Comparable || consistency.Verdict != "ok" {
		t.Fatalf("一致性比较错误: %+v", consistency)
	}

	rollout := DefaultGradualRolloutConfig()
	rollout.RejectionRateThreshold = 0.1
	shouldRollback, reason := CheckRollbackCondition(&RunMetrics{RejectionRate: 0.2}, &RunMetrics{}, &rollout)
	if !shouldRollback || reason == "" {
		t.Fatalf("拒绝率超限应触发回滚")
	}

	coverage := BuildRequirementCoverage([]string{"R1"}, map[string][]EvidenceRef{
		"R1": []EvidenceRef{{Source: "test", Path: "fixture", TraderID: "t1"}},
	})
	gate := GateResult{Verdict: "approved"}
	proposal := NewProposalChecklist("proposal-1", []DefectEntry{{DefectCode: "MIN_NOTIONAL", EvidenceRefs: []EvidenceRef{{Source: "test", Path: "fixture", TraderID: "t1"}}}}, comparableMetrics("base", "hash-a"), comparableMetrics("candidate", "hash-a"), gate, rollout)
	if reasons := ValidateProposalChecklist(proposal, coverage); len(reasons) != 0 {
		t.Fatalf("proposal checklist应完整: %v", reasons)
	}

	outDir := t.TempDir()
	if err := WriteOptimizationReport(outDir, OptimizationReport{SignalQualityReport: &signalReport, EquityCurveAnalysis: &equity, ProposalChecklist: &proposal, RequirementCoverage: &coverage}); err != nil {
		t.Fatalf("写优化报告失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "optimization_report.json")); err != nil {
		t.Fatalf("报告文件缺失: %v", err)
	}
}

func TestPropertyGateThresholds(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 50
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 1: Optimization_Gate 阈值判定", prop.ForAll(
		func(ddRatio, pfRatio, minDelta, rejDelta, cbDelta float64) bool {
			cfg := DefaultOptimizationConfig()
			base := comparableMetrics("base", "hash-a")
			candidate := comparableMetrics("candidate", "hash-a")
			candidate.MaxDrawdownPct = base.MaxDrawdownPct * ddRatio
			candidate.ProfitFactor = base.ProfitFactor * pfRatio
			candidate.MinNotionalRejectionRate = base.MinNotionalRejectionRate + minDelta
			candidate.RejectionRate = base.RejectionRate + rejDelta
			candidate.CircuitBreakerFrequencyPerDay = base.CircuitBreakerFrequencyPerDay + cbDelta
			result, err := EvaluateGate(GateInput{Baseline: base, Candidate: candidate, Config: cfg})
			if err != nil {
				return false
			}
			shouldReject := ddRatio > cfg.MaxDrawdownRelativeTolerance ||
				pfRatio < cfg.ProfitFactorRelativeFloor ||
				minDelta > cfg.MinNotionalRejectionTolerance
			shouldReview := !shouldReject && (rejDelta > cfg.RejectionRateAbsoluteTolerance || cbDelta > cfg.CircuitBreakerFrequencyTolerance)
			if shouldReject {
				return result.Verdict == "rejected"
			}
			if shouldReview {
				return result.Verdict == "manual_review"
			}
			return result.Verdict == "approved"
		},
		gen.Float64Range(0.8, 1.4),
		gen.Float64Range(0.8, 1.2),
		gen.Float64Range(0, 0.1),
		gen.Float64Range(0, 0.2),
		gen.Float64Range(0, 0.1),
	))
	properties.TestingRun(t)
}

func TestPropertyDefectCatalogRejectsNoEvidence(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 30
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 8: DefectCatalog 拒绝无证据缺陷", prop.ForAll(
		func(path, traderID, exchange string) bool {
			catalog, err := BuildDefectCatalog([]ReplayReportInput{{
				Report:   logger.ReplayReport{StrategyDisease: logger.StrategyDiseaseReport{MicroStopCount: 1}},
				Path:     path,
				TraderID: traderID,
				Exchange: exchange,
			}}, nil, DefaultOptimizationConfig())
			if path == "" || traderID == "" || exchange == "" {
				return errors.Is(err, ErrInsufficientEvidence) && catalog.Summary.RejectedNoEvidenceCount == 1
			}
			return err == nil && len(catalog.Defects) == 1 && len(catalog.Defects[0].EvidenceRefs) == 1
		},
		gen.OneConstOf("", "replay.json"),
		gen.OneConstOf("", "t1"),
		gen.OneConstOf("", "binance"),
	))
	properties.TestingRun(t)
}

func TestPropertyDefectCatalogOutputCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 30
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 9: DefectCatalog 输出完整性", prop.ForAll(
		func(count int) bool {
			cfg := DefaultOptimizationConfig()
			cfg.ReplayFrom = "2026-05-01"
			cfg.ReplayTo = "2026-05-20"
			cfg.Timezone = "Asia/Shanghai"
			catalog, err := BuildDefectCatalog([]ReplayReportInput{{
				Report:   logger.ReplayReport{StrategyDisease: logger.StrategyDiseaseReport{MicroStopCount: count}},
				Path:     "decision_logs/t1/replay.json",
				TraderID: "t1",
				Exchange: "binance",
			}}, nil, cfg)
			if count <= 0 {
				return errors.Is(err, ErrInsufficientEvidence)
			}
			if err != nil || catalog.DiagnosisInterval.ReplayFrom != cfg.ReplayFrom || len(catalog.Defects) == 0 {
				return false
			}
			defect := catalog.Defects[0]
			return defect.PrimaryMetric != "" &&
				len(defect.AffectedTraders) > 0 &&
				len(defect.AffectedExchanges) > 0 &&
				len(defect.EvidenceRefs) > 0 &&
				defect.SampleCountByTraderExchange["t1|binance"] == count
		},
		gen.IntRange(0, 5),
	))
	properties.TestingRun(t)
}

func TestPropertyCanonicalLogFieldsRequiredCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 30
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 6: Canonical_Log_Field 必填字段完整", prop.ForAll(
		func(placeTop bool) bool {
			action := completeDecisionAction()
			if !placeTop {
				action.SignalID = ""
				action.SignalType = ""
				action.SignalTimeframe = ""
				action.StructureTarget = 0
				action.SignalCloseTime = 0
				action.DecisionCloseTime = 0
				action.ConfigHash = ""
				action.StrategyVersion = ""
				action.StrategyMetadata["signal_id"] = "meta-signal"
				action.StrategyMetadata["signal_type"] = "sell2"
				action.StrategyMetadata["analysis_timeframe"] = "4h"
				action.StrategyMetadata["structure_target"] = 99.5
				action.StrategyMetadata["signal_close_time"] = int64(10)
				action.StrategyMetadata["decision_close_time"] = int64(20)
				action.StrategyMetadata["config_hash"] = "meta-cfg"
				action.StrategyMetadata["strategy_version"] = "meta-v"
			}
			fields, errs := ResolveCanonicalLogFields(action)
			return len(errs) == 0 &&
				fields.SignalID != "" &&
				fields.StructureKey != "" &&
				fields.EntryTriggerID != "" &&
				fields.ConfigHash != "" &&
				fields.StrategyVersion != ""
		},
		gen.Bool(),
	))
	properties.TestingRun(t)
}

func makeRunFixture(t *testing.T, runID, dataHash string, keepMetrics bool) string {
	t.Helper()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), runID)
	trades := []backtest.TradeLifecycle{
		{LifecycleID: "lc-1", Symbol: "BTCUSDT", Side: "long", EntryTime: now.Add(-2 * time.Hour), ExitTime: now.Add(-time.Hour), EntryPrice: 100, ExitPrice: 120, RealizedPnL: 20, RMultiple: 2, FinalRMultiple: 2, DurationMinutes: 60, SignalID: "sig-1", SignalType: "buy2", Closed: true},
		{LifecycleID: "lc-2", Symbol: "ETHUSDT", Side: "short", EntryTime: now.Add(-time.Hour), ExitTime: now, EntryPrice: 100, ExitPrice: 90, RealizedPnL: -5, RMultiple: -0.5, FinalRMultiple: -0.5, DurationMinutes: 60, SignalID: "sig-2", SignalType: "sell2", Closed: true},
	}
	rejections := []decision.OpenRejection{{Symbol: "DOGEUSDT", Action: "open_long", Reason: "min_notional 过低", SignalID: "sig-r", SignalType: "buy3"}}
	report := backtest.Report{
		RunID:              runID,
		GeneratedAt:        now,
		ConfigHash:         "cfg",
		DataHash:           dataHash,
		TraderID:           "t1",
		Exchange:           "binance",
		Timezone:           "UTC",
		BacktestFrom:       now.Add(-24 * time.Hour),
		BacktestTo:         now,
		ExecutionModel:     "next_open",
		FundingMode:        "disabled",
		LiquidationMode:    "disabled",
		ConfigSnapshot:     map[string]any{"costs": map[string]any{"taker_fee_bps": 4}},
		Summary:            backtest.SummaryStats{InitialEquity: 1000, FinalEquity: 1015, NetPnL: 15, NetReturnPct: 1.5, MaxDrawdownPct: 10, WinRate: 50, ProfitFactor: 4, AverageR: 0.75, TradeCount: 2, SignalCount: 2, RejectionCount: 1, TotalFees: 1, TotalSlippage: 1},
		BySymbol:           map[string]backtest.SymbolStats{"BTCUSDT": {TradeCount: 1, NetPnL: 20, WinRate: 100}, "ETHUSDT": {TradeCount: 1, NetPnL: -5, WinRate: 0}},
		BySignalType:       map[string]backtest.SignalStats{"buy2": {Count: 1, Executed: 1}, "buy3": {Count: 1, Rejected: 1}},
		BySide:             map[string]backtest.BucketStats{"long": {TradeCount: 1, NetPnL: 20, WinRate: 100, ProfitFactor: 0}, "short": {TradeCount: 1, NetPnL: -5, WinRate: 0}},
		BySymbolCategory:   map[string]backtest.BucketStats{"BTC": {TradeCount: 1, NetPnL: 20, WinRate: 100}, "ETH": {TradeCount: 1, NetPnL: -5, WinRate: 0}},
		RejectionBuckets:   map[string]int{"MIN_NOTIONAL_REJECTION": 1},
		MinNotionalRejects: 1,
	}
	if err := backtest.WriteArtifacts(dir, backtest.ReportArtifacts{
		Report: report,
		Trades: trades,
		Signals: []backtest.SignalOutcome{
			{SignalID: "sig-1", Symbol: "BTCUSDT", SignalType: "buy2", Reached1R: true},
			{SignalID: "sig-2", Symbol: "ETHUSDT", SignalType: "sell2"},
		},
		Rejections: rejections,
		Equity: []backtest.EquityPoint{
			{Timestamp: now.Add(-2 * time.Hour), Equity: 1000},
			{Timestamp: now, Equity: 1015, RealizedPnL: 15},
		},
		Structures: []backtest.StructureSnapshot{{SnapshotID: "s1", TraderID: "t1", Exchange: "binance", Symbol: "BTCUSDT", Timeframe: "1h", StructureKey: "sk", SignalID: "sig-1", SignalType: "buy2", ConfirmCloseMS: now.UnixMilli()}},
	}); err != nil {
		t.Fatalf("写fixture失败: %v", err)
	}
	if !keepMetrics {
		if err := os.Remove(filepath.Join(dir, "metrics.json")); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func comparableMetrics(runID, dataHash string) *RunMetrics {
	return &RunMetrics{
		RunID:                         runID,
		TraderID:                      "t1",
		Exchange:                      "binance",
		ConfigHash:                    "cfg",
		DataHash:                      dataHash,
		Timezone:                      "UTC",
		SymbolSetHash:                 "symbols",
		InitialEquity:                 1000,
		FeeModelHash:                  "fee",
		SlippageModelHash:             "slip",
		FundingMode:                   "disabled",
		LiquidationMode:               "disabled",
		ExecutionModelHash:            "exec",
		WinRate:                       50,
		ProfitFactor:                  2,
		NetPnL:                        100,
		MaxDrawdownPct:                10,
		MinNotionalRejectionRate:      0,
		RejectionRate:                 0.1,
		CircuitBreakerFrequencyPerDay: 0,
		BootstrapCIs:                  map[string]BootstrapCI{"profit_factor": {Metric: "profit_factor", Status: "ok", Interval: [2]float64{1.8, 2.2}}},
		CIStatus:                      "ok",
	}
}

func completeDecisionAction() logger.DecisionAction {
	return logger.DecisionAction{
		SignalID:          "sig",
		SignalType:        "buy2",
		SignalTimeframe:   "1h",
		StructureTarget:   100,
		SignalCloseTime:   10,
		DecisionCloseTime: 20,
		ConfigHash:        "cfg",
		StrategyVersion:   "v1",
		StrategyMetadata: map[string]any{
			"structure_key":     "sk",
			"parent_signal_id":  "parent",
			"entry_trigger_id":  "trigger",
			"source_layer":      "main",
			"trigger_timeframe": "15m",
			"age_candles":       1,
			"freshness_state":   "fresh",
			"reason_code":       "ok",
		},
	}
}

func TestBootstrapIntervalsDeterministicWithSeed(t *testing.T) {
	trades := []backtest.TradeLifecycle{{RealizedPnL: 10, RMultiple: 1}, {RealizedPnL: -5, RMultiple: -0.5}, {RealizedPnL: 20, RMultiple: 2}}
	cfg := DefaultOptimizationConfig()
	cfg.BootstrapIterations = 50
	cfg.BootstrapSeed = 99
	a := BootstrapIntervals(trades, cfg)
	b := BootstrapIntervals(trades, cfg)
	if !reflect.DeepEqual(a, b) {
		aj, _ := json.Marshal(a)
		bj, _ := json.Marshal(b)
		t.Fatalf("固定seed应确定: %s != %s", aj, bj)
	}
	if a["net_pnl"].Status != "ok" {
		t.Fatalf("样本充足时CI应为ok: %+v", a)
	}
	if got := BootstrapIntervals(trades[:1], cfg)["net_pnl"].Status; got != "insufficient_samples" {
		t.Fatalf("样本不足状态错误: %s", got)
	}
}
