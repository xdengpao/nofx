package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"testing"
	"time"
)

func TestDecideEntryPathAndDirectStructure(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	engine := &Engine{
		Policy: decision.ProgrammaticStrategyPolicy{
			DefectFixPackEnabled: true,
			AllowLong:            true,
			Timeframes:           decision.ProgrammaticTimeframesPolicy{Trade: "1h", Sub: "15m"},
			EntryTiming: decision.ProgrammaticEntryTimingPolicy{
				Enabled:                      true,
				DirectStructureOpen:          true,
				DirectStructureMinConfidence: 70,
				EntryZone: decision.ProgrammaticEntryZonePolicy{
					MaxChaseRatio:                0.35,
					MaxChaseATRMultiplier:        0.6,
					MinRemainingNetRR:            1.5,
					TheoreticalRRUnreachableSkip: true,
				},
			},
			SignalFreshness: decision.ProgrammaticSignalFreshnessPolicy{MaxLifetimeCandles: 4},
		},
		StateStore: &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()},
	}
	signal := ChanlunSignal{
		SignalID:          "sig1",
		StructureKey:      "struct1",
		Symbol:            "SOLUSDT",
		Direction:         SideLong,
		SignalType:        SignalBuy2,
		AnalysisTF:        "1h",
		TriggerTF:         "15m",
		Price:             100,
		StopLoss:          95,
		TakeProfit:        115,
		Confidence:        75,
		SignalCloseTime:   now.UnixMilli(),
		DecisionCloseTime: now.UnixMilli(),
		SourceLayer:       "structure",
	}
	if got := engine.decideEntryPath(signal); got != EntryPathDirectStructure {
		t.Fatalf("高置信结构应走direct路径: %s", got)
	}
	signal.Confidence = 65
	if got := engine.decideEntryPath(signal); got != EntryPathPreviewThenTrigger {
		t.Fatalf("低置信结构应走preview路径: %s", got)
	}
	signal.Confidence = 75
	ctx := &decision.Context{TraderID: "t1", AltcoinLeverage: 5}
	openable, diagnostics := engine.prepareDirectStructureEntry(ctx, &signal, &market.Data{Symbol: "SOLUSDT", CurrentPrice: 100}, now)
	if !openable || signal.EntryPath != EntryPathDirectStructure || signal.EntryWindowState != "direct_structure_ready" {
		t.Fatalf("direct structure应直接进入ready: signal=%+v diagnostics=%+v", signal, diagnostics)
	}
}
