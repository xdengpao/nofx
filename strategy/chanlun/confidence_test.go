package chanlun

import (
	"nofx/decision"
	"testing"
	"time"
)

func TestEffectivePilotMinConfidenceP75AndModes(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	store := &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()}
	engine := &Engine{
		Policy: decision.ProgrammaticStrategyPolicy{
			PreviewSignals: decision.ProgrammaticPreviewSignalsPolicy{
				PilotMinConfidence:       70,
				PilotMinConfidenceUseP75: true,
				P75Floor:                 65,
				P75Ceiling:               85,
			},
		},
		StateStore: store,
	}
	ctx := &decision.Context{TraderID: "t1"}

	for i := 0; i < 40; i++ {
		store.StoreConfidenceSample("t1", "BTCUSDT", SignalBuy2, 50+i, now.Add(time.Duration(i)*time.Minute))
	}
	if got := engine.effectivePilotMinConfidence(ctx, SignalBuy2); got != 80 {
		t.Fatalf("P75阈值错误: got=%d want=80", got)
	}
	if got := engine.effectivePilotMinConfidence(ctx, SignalSell2); got != 70 {
		t.Fatalf("样本不足应回退基础阈值: got=%d want=70", got)
	}
	ctx.FrequencyPolicy = &decision.FrequencyPolicy{EffectiveMode: "safe"}
	if got := engine.effectivePilotMinConfidence(ctx, SignalBuy2); got != 90 {
		t.Fatalf("safe模式应提高10点且封顶95: got=%d want=90", got)
	}
	ctx.FrequencyPolicy = &decision.FrequencyPolicy{EffectiveMode: "loosen"}
	if got := engine.effectivePilotMinConfidence(ctx, SignalBuy2); got != 70 {
		t.Fatalf("loosen模式应降低10点且floor 60: got=%d want=70", got)
	}
}
