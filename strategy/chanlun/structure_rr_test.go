package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"testing"
	"time"
)

func TestMinRemainingNetRRForSignalPriority(t *testing.T) {
	engine := &Engine{Policy: decision.ProgrammaticStrategyPolicy{
		SignalFreshness: decision.ProgrammaticSignalFreshnessPolicy{MinRemainingNetRR: 2.5},
		EntryTiming: decision.ProgrammaticEntryTimingPolicy{EntryZone: decision.ProgrammaticEntryZonePolicy{
			MinRemainingNetRR: 2.5,
			SignalTypeMinRR:   map[string]float64{"buy*@1h": 1.7, "sell2": 1.8},
			TierOverrides:     map[string]decision.ProgrammaticEntryZoneOverridePolicy{"core": {MinRemainingNetRR: 1.6}},
			SymbolOverrides:   map[string]decision.ProgrammaticEntryZoneOverridePolicy{"BTCUSDT": {MinRemainingNetRR: 1.5}},
		}},
	}}
	if got := engine.minRemainingNetRRForSignal(SignalBuy2, "1h", "ETHUSDT", "core"); got != 1.7 {
		t.Fatalf("signal_type@timeframe wildcard应优先: %.2f", got)
	}
	if got := engine.minRemainingNetRRForSignal(SignalSell2, "4h", "ETHUSDT", "core"); got != 1.8 {
		t.Fatalf("signal_type应优先于tier: %.2f", got)
	}
	if got := engine.minRemainingNetRRForSignal(SignalBuy3, "4h", "BTCUSDT", "core"); got != 1.5 {
		t.Fatalf("symbol override应优先于tier: %.2f", got)
	}
}

func TestShouldSkipBeforeEntryTheoreticalRR(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	engine := &Engine{
		Policy: decision.ProgrammaticStrategyPolicy{
			DefectFixPackEnabled: true,
			AllowLong:            true,
			Timeframes:           decision.ProgrammaticTimeframesPolicy{Trade: "1h", Sub: "15m"},
			SignalFreshness:      decision.ProgrammaticSignalFreshnessPolicy{MaxLifetimeCandles: 4},
			EntryTiming: decision.ProgrammaticEntryTimingPolicy{EntryZone: decision.ProgrammaticEntryZonePolicy{
				MinRemainingNetRR:            5.0,
				TheoreticalRRUnreachableSkip: true,
			}},
		},
		StateStore: &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()},
	}
	ctx := &decision.Context{TraderID: "t1", AltcoinLeverage: 5}
	signal := ChanlunSignal{
		SignalID:        "sig1",
		StructureKey:    "struct1",
		Symbol:          "SOLUSDT",
		Direction:       SideLong,
		SignalType:      SignalBuy2,
		AnalysisTF:      "1h",
		TriggerTF:       "15m",
		Price:           100,
		StopLoss:        95,
		TakeProfit:      110,
		Confidence:      80,
		SignalCloseTime: now.UnixMilli(),
	}
	rejection, diagnostics := engine.shouldSkipBeforeEntry(ctx, signal, &market.Data{Symbol: "SOLUSDT", CurrentPrice: 100}, now)
	if rejection == nil || rejection.GateReasons[0] != "theoretical_rr_unreachable" || len(diagnostics) == 0 {
		t.Fatalf("理论RR不可达应预过滤: rejection=%+v diagnostics=%+v", rejection, diagnostics)
	}
	if !engine.StateStore.IsLifecycleTerminated("t1", "SOLUSDT", "struct1") {
		t.Fatal("理论RR不可达应终结生命周期")
	}
}
