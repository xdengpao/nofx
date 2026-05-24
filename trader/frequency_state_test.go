package trader

import (
	"nofx/logger"
	"testing"
	"time"
)

func TestCountSignals24hHonorsExplicitSignalCount(t *testing.T) {
	now := time.Now()
	records := []*logger.DecisionRecord{
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"signal_count": 0},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}, {Symbol: "SOLUSDT"}},
		},
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"signal_count": float64(2)},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BNBUSDT"}, {Symbol: "XRPUSDT"}, {Symbol: "DOGEUSDT"}},
		},
		{
			Timestamp:        now,
			CandidateDetails: []logger.CandidateSnapshot{{Symbol: "ADAUSDT"}},
		},
	}
	if got := countSignals24h(records, now.Add(-time.Hour)); got != 3 {
		t.Fatalf("显式signal_count应优先于候选币fallback: got=%d want=3", got)
	}
}

func TestCountSignals24hUsesPerCandidateBeforeFallback(t *testing.T) {
	now := time.Now()
	records := []*logger.DecisionRecord{
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"per_candidate": []any{}},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}},
		},
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"per_candidate": []any{map[string]any{"symbol": "SOLUSDT"}}},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "SOLUSDT"}, {Symbol: "BNBUSDT"}},
		},
	}
	if got := countSignals24h(records, now.Add(-time.Hour)); got != 1 {
		t.Fatalf("per_candidate空数组也应阻止候选币fallback: got=%d want=1", got)
	}
}
