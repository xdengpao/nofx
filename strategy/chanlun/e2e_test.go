package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"path/filepath"
	"testing"
	"time"
)

func TestE2EDirectStructurePositionManagementAndDiagnostics(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	policy := testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json"))
	policy.DefectFixPackEnabled = true
	policy.SignalFreshness = decision.ProgrammaticSignalFreshnessPolicy{Enabled: true, SoftAgeCandles: 2, MaxLifetimeCandles: 4, MinRemainingNetRR: 1.2}
	policy.EntryTiming.Enabled = true
	policy.EntryTiming.DirectStructureOpen = true
	policy.EntryTiming.DirectStructureMinConfidence = 70
	policy.EntryTiming.EntryZone = decision.ProgrammaticEntryZonePolicy{
		MaxChaseRatio:         0.5,
		MaxChaseATRMultiplier: 1.0,
		MinRemainingNetRR:     1.2,
	}
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	engine.Clock = func() time.Time { return now }
	ctx := &decision.Context{
		TraderID:        "t1",
		AltcoinLeverage: 5,
		Account:         decision.AccountInfo{TotalEquity: 1000, AvailableBalance: 1000},
		CandidateCoins:  []decision.CandidateCoin{{Symbol: "SOLUSDT", IncludedInPrompt: true}},
		FrequencyPolicy: &decision.FrequencyPolicy{Mode: "balanced", EffectiveMode: "balanced"},
		FrequencyState:  &decision.FrequencyState{},
		MarketDataMap:   map[string]*market.Data{"SOLUSDT": {Symbol: "SOLUSDT", CurrentPrice: 100}},
	}
	signal := testSignal(SignalBuy2, SideLong, now.UnixMilli(), now.UnixMilli(), 90, 130, 100)
	signal.SignalID = "e2e-open"
	signal.StructureKey = "e2e-structure"
	signal.Confidence = 80
	signal.SourceLayer = "structure"

	openable, diagnostics := engine.prepareDirectStructureEntry(ctx, &signal, ctx.MarketDataMap["SOLUSDT"], now)
	if !openable || signal.EntryPath != EntryPathDirectStructure {
		t.Fatalf("direct_structure应进入ready: signal=%+v diagnostics=%+v", signal, diagnostics)
	}
	openDecision := engine.signalToMainDecision(ctx, signal)
	guarded, rejection, guardDiagnostics := engine.applyProgrammaticSignalGuard(ctx, signal, openDecision, ctx.MarketDataMap["SOLUSDT"], now)
	if rejection != nil || guarded.Action != "open_long" {
		t.Fatalf("direct_structure开仓候选应通过guard: d=%+v rejection=%+v diagnostics=%+v", guarded, rejection, guardDiagnostics)
	}
	engine.setLatestSignals("t1", "SOLUSDT", []ChanlunSignal{signal}, []string{"mock signal"})
	full := &decision.FullDecision{Decisions: []decision.Decision{guarded}, Timestamp: now}
	engine.applyDecisionMetadata(ctx, full, []string{"SOLUSDT 识别到1个信号"}, nil, AccountSizeDecision{})
	if len(full.StrategyDiagnostics["per_candidate"].([]map[string]any)) != 1 ||
		full.StrategyDiagnostics["risk_state"] == nil || full.StrategyDiagnostics["account_state"] == nil {
		t.Fatalf("策略诊断字段不完整: %+v", full.StrategyDiagnostics)
	}

	pos := decision.PositionInfo{
		Symbol:           "SOLUSDT",
		Side:             SideLong,
		EntryPrice:       100,
		MarkPrice:        103,
		StopLoss:         95,
		Quantity:         1,
		UnrealizedPnLPct: 3,
		UpdateTime:       now.UnixMilli(),
	}
	manageData := &market.Data{Symbol: "SOLUSDT", CurrentPrice: 103, Klines: map[string][]market.Kline{}}
	breakeven, _ := engine.evaluateBreakeven(ctx, pos, manageData, now)
	if breakeven.Action != "update_stop_loss" || breakeven.NewStopLoss <= pos.EntryPrice {
		t.Fatalf("浮盈后应触发breakeven: %+v", breakeven)
	}

	engine.Policy.PositionManagement.Breakeven.Enabled = false
	engine.Policy.PositionManagement.FloatingDrawdown.Enabled = true
	drawdownPos := pos
	drawdownPos.MarkPrice = 104
	drawdownPos.UnrealizedPnLPct = 2
	state := ProgrammaticPositionState{Side: SideLong, PeakPrice: 110, PeakR: 2}
	partial, _ := engine.evaluateFloatingDrawdown(ctx, drawdownPos, &market.Data{Symbol: "SOLUSDT", CurrentPrice: 104}, state, now)
	if partial.Action != "partial_close" || partial.ClosePercentage <= 0 {
		t.Fatalf("回撤后应触发partial_close: %+v", partial)
	}

	engine.Policy.PositionManagement.FloatingDrawdown.Enabled = false
	engine.Policy.PositionManagement.StructureBreak.Enabled = true
	engine.Policy.PositionManagement.StructureBreak.Action = "close"
	breakData := &market.Data{Symbol: "SOLUSDT", CurrentPrice: 94, Klines: map[string][]market.Kline{
		"15m": append(testKlines(25, 100, 0), market.Kline{OpenTime: now.UnixMilli() + 1, CloseTime: now.Add(15 * time.Minute).UnixMilli(), Open: 100, High: 101, Low: 90, Close: 94}),
	}}
	closeDecision, _ := engine.evaluateStructureBreak(ctx, pos, breakData, ProgrammaticPositionState{Side: SideLong}, now)
	if closeDecision.Action != "close_long" {
		t.Fatalf("结构破坏应触发close_long: %+v", closeDecision)
	}
}

func TestE2EHoldOnlySkipsOpenEvaluation(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	policy := testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json"))
	policy.DefectFixPackEnabled = true
	policy.MaxPilotNotionalPct = 0.6
	policy.MinPilotNotionalUSD = 30
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	engine.Clock = func() time.Time { return now }
	engine.MarketDataProvider = func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error) {
		return &market.Data{Symbol: symbol, CurrentPrice: 100, Klines: map[string][]market.Kline{}}, nil
	}
	engine.DisableOITopFetch = true
	full, err := engine.GetFullDecision(&decision.Context{
		TraderID:        "t1",
		RuntimeMinutes:  60,
		AltcoinLeverage: 5,
		Account:         decision.AccountInfo{TotalEquity: 5, AvailableBalance: 5},
		FrequencyPolicy: &decision.FrequencyPolicy{Mode: "balanced", EffectiveMode: "balanced"},
		FrequencyState:  &decision.FrequencyState{},
	})
	if err != nil {
		t.Fatalf("hold_only周期不应失败: %v", err)
	}
	if len(full.OpenRejections) != 0 || full.StrategyDiagnostics == nil {
		t.Fatalf("hold_only应跳过开仓评估并保留诊断: %+v", full)
	}
	riskState := full.StrategyDiagnostics["risk_state"].(map[string]any)
	if riskState["account_too_small"] != true {
		t.Fatalf("hold_only应标记account_too_small: %+v", riskState)
	}
}

func TestE2ELoosenModeAfterInactivity(t *testing.T) {
	now := time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC)
	policy := testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json"))
	policy.DefectFixPackEnabled = true
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	engine.Clock = func() time.Time { return now }
	ctx := &decision.Context{
		TraderID:       "t1",
		RuntimeMinutes: 13 * 60,
		FrequencyPolicy: &decision.FrequencyPolicy{
			Mode:          "balanced",
			EffectiveMode: "balanced",
			LoosenMode: decision.LoosenModePolicy{
				Enabled:                 true,
				InactivityWindowMinutes: 720,
				MaxDurationHours:        24,
			},
		},
		FrequencyState: &decision.FrequencyState{},
	}
	if got := engine.loosenModeController(ctx, false); got != "loosen" {
		t.Fatalf("第13小时应进入loosen并允许后续开仓评估: %s", got)
	}
	riskState := engine.buildStrategyRiskDiagnostics(ctx, AccountSizeDecision{})
	if riskState["active_mode"] != "loosen" {
		t.Fatalf("loosen应写入risk_state: %+v", riskState)
	}
}
