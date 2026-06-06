package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"testing"
)

func TestEntryWindowDualTrackChaseAndOverrides(t *testing.T) {
	engine := &Engine{Policy: decision.ProgrammaticStrategyPolicy{
		Timeframes: decision.ProgrammaticTimeframesPolicy{Trade: "1h", Sub: "15m"},
		EntryTiming: decision.ProgrammaticEntryTimingPolicy{TriggerTimeframe: "15m", EntryZone: decision.ProgrammaticEntryZonePolicy{
			MaxChaseRatio:         0.2,
			MaxChaseATRMultiplier: 0.6,
			FreshAgeChaseRelax:    0.10,
			MinRemainingNetRR:     1.2,
			TierOverrides: map[string]decision.ProgrammaticEntryZoneOverridePolicy{
				"core": {MaxChaseRatio: 0.25},
			},
		}},
	}}
	signal := ChanlunSignal{
		Symbol:     "BTCUSDT",
		Direction:  SideLong,
		SignalType: SignalBuy2,
		Price:      100,
		StopLoss:   90,
		TakeProfit: 140,
		Tier:       "core",
	}
	data := &market.Data{
		Symbol:       "BTCUSDT",
		CurrentPrice: 103,
		MidTermSeries15m: &market.MidTermData15m{
			ATRValues: []float64{10},
		},
	}
	window := engine.evaluateStructureEntryWindow(&decision.Context{}, signal, data, 1)
	if !window.Valid || window.ChaseRatio <= 0.25 || window.ChaseATR > 0.6 {
		t.Fatalf("ATR轨通过时应放行: %+v", window)
	}
	if got := engine.effectiveMaxChaseRatio("BTCUSDT", "core", 0); got != 0.35 {
		t.Fatalf("fresh age应在tier阈值上放宽0.10: %.2f", got)
	}
}
