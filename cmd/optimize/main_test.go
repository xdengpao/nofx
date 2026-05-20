package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nofx/backtest"
	"nofx/decision"
	"nofx/optimize"
)

func TestCLICommandsWithFixtureRuns(t *testing.T) {
	baseline := makeCLIRunFixture(t, "baseline", "same-hash", 10, 2.0, 0)
	candidate := makeCLIRunFixture(t, "candidate", "same-hash", 10.5, 2.0, 0)

	outDir := t.TempDir()
	diagnoseOut := filepath.Join(outDir, "defect_catalog.json")
	runDiagnose([]string{"-backtest-run", candidate, "-trader", "t1", "-exchange", "binance", "-output", diagnoseOut})
	var catalog optimize.DefectCatalog
	readJSONFile(t, diagnoseOut, &catalog)
	if catalog.Summary.TotalDefects == 0 {
		t.Fatalf("diagnose应输出缺陷目录: %+v", catalog)
	}

	compareOut := filepath.Join(outDir, "gate_result.json")
	runCompare([]string{"-baseline-run", baseline, "-candidate-run", candidate, "-output", compareOut})
	var gate optimize.GateResult
	readJSONFile(t, compareOut, &gate)
	if gate.Verdict != "approved" {
		t.Fatalf("compare应通过门控: %+v", gate)
	}

	reportOut := filepath.Join(outDir, "report")
	runReport([]string{"-baseline-run", baseline, "-candidate-run", candidate, "-output-dir", reportOut})
	if _, err := os.Stat(filepath.Join(reportOut, "optimization_report.json")); err != nil {
		t.Fatalf("report应写optimization_report.json: %v", err)
	}
}

func makeCLIRunFixture(t *testing.T, runID, dataHash string, maxDD, pf float64, extraRejects int) string {
	t.Helper()
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), runID)
	rejections := []decision.OpenRejection{{Symbol: "DOGEUSDT", Action: "open_long", Reason: "min_notional 过低", SignalID: "sig-r", SignalType: "buy3"}}
	for i := 0; i < extraRejects; i++ {
		rejections = append(rejections, decision.OpenRejection{Symbol: "SOLUSDT", Action: "open_long", Reason: "open gate阻止开仓"})
	}
	report := backtest.Report{
		RunID:           runID,
		GeneratedAt:     now,
		ConfigHash:      "cfg",
		DataHash:        dataHash,
		TraderID:        "t1",
		Exchange:        "binance",
		Timezone:        "UTC",
		BacktestFrom:    now.Add(-24 * time.Hour),
		BacktestTo:      now,
		ExecutionModel:  "next_open",
		FundingMode:     "disabled",
		LiquidationMode: "disabled",
		ConfigSnapshot:  map[string]any{"costs": map[string]any{"taker_fee_bps": 4}},
		Summary: backtest.SummaryStats{
			InitialEquity: 1000, FinalEquity: 1010, NetPnL: 10, NetReturnPct: 1,
			MaxDrawdownPct: maxDD, WinRate: 50, ProfitFactor: pf, AverageR: 0.5,
			TradeCount: 2, SignalCount: 100, RejectionCount: len(rejections),
		},
		BySymbol:           map[string]backtest.SymbolStats{"BTCUSDT": {TradeCount: 2, NetPnL: 10, WinRate: 50}},
		BySignalType:       map[string]backtest.SignalStats{"buy2": {Count: 2, Executed: 2}},
		RejectionBuckets:   map[string]int{"MIN_NOTIONAL_REJECTION": 1},
		MinNotionalRejects: 1,
	}
	if err := backtest.WriteArtifacts(dir, backtest.ReportArtifacts{
		Report: report,
		Trades: []backtest.TradeLifecycle{
			{LifecycleID: "lc-1", Symbol: "BTCUSDT", Side: "long", EntryTime: now.Add(-2 * time.Hour), ExitTime: now.Add(-time.Hour), EntryPrice: 100, ExitPrice: 110, RealizedPnL: 10, RMultiple: 1, DurationMinutes: 60, SignalID: "sig-1", SignalType: "buy2", Closed: true},
			{LifecycleID: "lc-2", Symbol: "BTCUSDT", Side: "long", EntryTime: now.Add(-time.Hour), ExitTime: now, EntryPrice: 100, ExitPrice: 100, RealizedPnL: 0, DurationMinutes: 60, SignalID: "sig-2", SignalType: "buy2", Closed: true},
		},
		Signals:    []backtest.SignalOutcome{{SignalID: "sig-1", Symbol: "BTCUSDT", SignalType: "buy2"}},
		Rejections: rejections,
		Equity:     []backtest.EquityPoint{{Timestamp: now, Equity: 1010}},
		Structures: []backtest.StructureSnapshot{{SnapshotID: "s1", TraderID: "t1", Exchange: "binance", Symbol: "BTCUSDT", Timeframe: "1h", StructureKey: "sk", SignalID: "sig-1", SignalType: "buy2"}},
	}); err != nil {
		t.Fatalf("写CLI fixture失败: %v", err)
	}
	return dir
}

func readJSONFile(t *testing.T, path string, dest any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatal(err)
	}
}
