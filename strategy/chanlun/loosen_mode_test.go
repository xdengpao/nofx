package chanlun

import (
	"nofx/decision"
	"testing"
	"time"
)

func TestLoosenModeTriggersAndAppliesThresholdAdjustments(t *testing.T) {
	now := time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC)
	store := &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()}
	engine := &Engine{
		Policy: decision.ProgrammaticStrategyPolicy{
			DefectFixPackEnabled: true,
			PreviewSignals: decision.ProgrammaticPreviewSignalsPolicy{
				PilotMinConfidence: 65,
			},
			EntryTiming: decision.ProgrammaticEntryTimingPolicy{EntryZone: decision.ProgrammaticEntryZonePolicy{
				MaxChaseRatio:     0.35,
				MinRemainingNetRR: 1.6,
			}},
		},
		StateStore: store,
		Clock:      func() time.Time { return now },
	}
	ctx := &decision.Context{
		TraderID:       "t1",
		RuntimeMinutes: 13 * 60,
		FrequencyPolicy: &decision.FrequencyPolicy{
			Mode:          "balanced",
			EffectiveMode: "balanced",
			LoosenMode: decision.LoosenModePolicy{
				Enabled:                  true,
				InactivityWindowMinutes:  720,
				PilotConfidenceDrop:      20,
				HardFloorPilotConfidence: 60,
				MinNetRRDelta:            -0.4,
				MaxChaseRatioBump:        0.05,
				MaxDurationHours:         24,
			},
		},
		FrequencyState: &decision.FrequencyState{},
	}
	if got := engine.loosenModeController(ctx, false); got != "loosen" {
		t.Fatalf("12h无开仓后应进入loosen: %s", got)
	}
	if state := store.LoosenState("t1"); !state.Active || state.ExpiresAt.IsZero() {
		t.Fatalf("loosen状态未持久化: %+v", state)
	}
	if got := engine.effectivePilotMinConfidence(ctx, SignalBuy2); got != 60 {
		t.Fatalf("loosen应降低pilot阈值且受hard floor限制: %d", got)
	}
	if got := engine.minRemainingNetRRForSignal(SignalBuy2, "1h", "SOLUSDT", ""); got < 1.19 || got > 1.21 {
		t.Fatalf("loosen应降低min RR: %.2f", got)
	}
	if got := engine.effectiveMaxChaseRatio("SOLUSDT", "", 1); got < 0.39 || got > 0.41 {
		t.Fatalf("loosen应放宽chase ratio: %.2f", got)
	}
}

func TestLoosenModeExitsAfterOpenAndIsMutuallyExclusive(t *testing.T) {
	now := time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC)
	store := &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()}
	store.SetLoosenState("t1", LoosenState{Active: true, EnteredAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)})
	engine := &Engine{
		Policy:     decision.ProgrammaticStrategyPolicy{DefectFixPackEnabled: true},
		StateStore: store,
		Clock:      func() time.Time { return now },
	}
	ctx := &decision.Context{
		TraderID: "t1",
		FrequencyPolicy: &decision.FrequencyPolicy{
			Mode:          "balanced",
			EffectiveMode: "balanced",
			LoosenMode:    decision.LoosenModePolicy{Enabled: true, InactivityWindowMinutes: 720, MaxDurationHours: 24},
		},
		FrequencyState: &decision.FrequencyState{OpenCount24h: 1},
	}
	if got := engine.loosenModeController(ctx, false); got != "balanced" {
		t.Fatalf("成功开仓后应退出loosen并恢复原档位: %s", got)
	}
	if state := store.LoosenState("t1"); state.Active {
		t.Fatalf("loosen状态应清空: %+v", state)
	}

	store.SetLoosenState("t1", LoosenState{Active: true, EnteredAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)})
	ctx.FrequencyState.OpenCount24h = 0
	ctx.FrequencyPolicy.EffectiveMode = "safe"
	if got := engine.loosenModeController(ctx, false); got != "safe" {
		t.Fatalf("safe模式应与loosen互斥: %s", got)
	}
	if state := store.LoosenState("t1"); state.Active {
		t.Fatalf("safe模式下应退出loosen: %+v", state)
	}

	store.SetLoosenState("t1", LoosenState{Active: true, EnteredAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)})
	ctx.FrequencyPolicy.EffectiveMode = "balanced"
	ctx.LossMode = &decision.LossModeState{Active: true}
	if got := engine.loosenModeController(ctx, false); got != "loss" {
		t.Fatalf("loss mode应优先于loosen: %s", got)
	}
}

func TestLoosenModeExpiresAndGateEffectivenessDiagnostics(t *testing.T) {
	now := time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC)
	store := &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()}
	store.SetLoosenState("t1", LoosenState{Active: true, EnteredAt: now.Add(-25 * time.Hour), ExpiresAt: now.Add(-time.Hour)})
	engine := &Engine{
		Policy:     decision.ProgrammaticStrategyPolicy{DefectFixPackEnabled: true},
		StateStore: store,
		Clock:      func() time.Time { return now },
	}
	ctx := &decision.Context{
		TraderID:       "t1",
		RuntimeMinutes: 60,
		FrequencyPolicy: &decision.FrequencyPolicy{
			Mode:                        "balanced",
			EffectiveMode:               "balanced",
			GateEffectivenessReportOnly: true,
			LoosenMode:                  decision.LoosenModePolicy{Enabled: true, InactivityWindowMinutes: 720, MaxDurationHours: 24},
		},
		FrequencyState: &decision.FrequencyState{OpenRejected24h: 12},
	}
	if got := engine.loosenModeController(ctx, false); got != "balanced" {
		t.Fatalf("过期loosen不应继续生效: %s", got)
	}
	if state := store.LoosenState("t1"); state.Active {
		t.Fatalf("过期loosen应清空: %+v", state)
	}
	risk := engine.buildStrategyRiskDiagnostics(ctx, AccountSizeDecision{})
	gate, ok := risk["gate_effectiveness"].(map[string]any)
	if !ok || gate["report_only"] != true {
		t.Fatalf("report-only gate effectiveness诊断缺失: %+v", risk)
	}
	if warnings, ok := risk["warnings"].(map[string]bool); !ok || !warnings["runaway_rejection_loop"] {
		t.Fatalf("open_rejected空转告警缺失: %+v", risk)
	}
}
