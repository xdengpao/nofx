package chanlun

import (
	"encoding/json"
	"nofx/decision"
	"nofx/market"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEntryTimingStaleStructureBecomesBackgroundOnly(t *testing.T) {
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	signal := testSignal(SignalSell2, SideShort, base, base+int64(5*time.Hour/time.Millisecond), 110, 70, 100)
	ctx, data := testGuardContext(100)

	openable, diagnostics := engine.prepareStructureEntry(ctx, &signal, data, time.UnixMilli(signal.DecisionCloseTime))
	if openable {
		t.Fatalf("默认entry timing下陈旧结构不应直接开仓: %+v", signal)
	}
	if signal.Status != "background" || signal.EntryWindowState != "structure_background_only" {
		t.Fatalf("陈旧结构应标记为background: signal=%+v diagnostics=%+v", signal, diagnostics)
	}
	if engine.StateStore.HasExecutedSignal("t1", "SOLUSDT", signal.SignalID) {
		t.Fatal("background-only结构不应写入executed_signals")
	}
	if got, ok := engine.StateStore.SuppressedSignalForAction("t1", "SOLUSDT", signal.SignalID, "open_short"); !ok || got.ReasonCode != "structure_background_only" {
		t.Fatalf("background结构应写入独立suppression用于避免重放open: %+v ok=%v", got, ok)
	}
	markers := engine.StateStore.RecentSignalMarkers("t1", "SOLUSDT", 10)
	if len(markers) != 1 || markers[0].Status != "background" || markers[0].EntryWindowState != "structure_background_only" {
		t.Fatalf("应写入background marker: %+v", markers)
	}
}

func TestEntryTimingTargetCrossedInvalidatesStructureBeforeDecision(t *testing.T) {
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	signal := testSignal(SignalSell2, SideShort, base, base+int64(time.Hour/time.Millisecond), 110, 70, 100)
	ctx, data := testGuardContext(65)

	openable, _ := engine.prepareStructureEntry(ctx, &signal, data, time.UnixMilli(signal.DecisionCloseTime))
	if openable {
		t.Fatalf("越过目标的结构不应进入开仓决策: %+v", signal)
	}
	if signal.Status != "invalidated" || !signal.EntryInvalidated || signal.EntryInvalidReason != "target_already_crossed" {
		t.Fatalf("应标记target_already_crossed invalidated: %+v", signal)
	}
	if got, ok := engine.StateStore.SuppressedSignalForAction("t1", "SOLUSDT", signal.SignalID, "open_short"); !ok || got.ReasonCode != "target_already_crossed" {
		t.Fatalf("target-crossed结构应写入suppression: %+v ok=%v", got, ok)
	}
}

func TestEntryTimingDirectFreshStructureBuildsEntryTrigger(t *testing.T) {
	policy := testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json"))
	policy.TakeProfit.MinNetRR = 2.5
	policy.EntryTiming.Enabled = true
	policy.EntryTiming.DirectStructureOpen = true
	policy.EntryTiming.RequireFreshTrigger = true
	policy.EntryTiming.DirectOpenMaxAgeCandles = 0
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	closeTime := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	signal := testSignal(SignalBuy2, SideLong, closeTime, closeTime, 90, 140, 100)
	ctx, data := testGuardContext(100)

	openable, diagnostics := engine.prepareStructureEntry(ctx, &signal, data, time.UnixMilli(closeTime))
	if !openable {
		t.Fatalf("新闭合结构在direct开启且窗口有效时应生成entry trigger: signal=%+v diagnostics=%+v", signal, diagnostics)
	}
	if signal.SourceLayer != "entry_trigger" || signal.EntryTriggerID == "" || signal.ParentSignalID == "" ||
		signal.EntryTriggerType != "new_structure_segment" || signal.EntryWindowState != "entry_trigger_ready" {
		t.Fatalf("entry trigger元数据不完整: %+v", signal)
	}
	d := engine.signalToMainDecision(&decision.Context{TraderID: "t1", AltcoinLeverage: 5}, signal)
	if d.SignalID != signal.EntryTriggerID || metadataString(d.StrategyMetadata, "parent_signal_id") != signal.ParentSignalID ||
		metadataString(d.StrategyMetadata, "layer") != "entry_trigger" {
		t.Fatalf("开仓决策应以entry_trigger_id去重并保留parent lineage: %+v", d)
	}
}

func TestEntryTimingStructureSuppressionDoesNotBlockNewTriggerID(t *testing.T) {
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	parent := testSignal(SignalSell2, SideShort, base, base+int64(5*time.Hour/time.Millisecond), 110, 70, 100)
	ctx, data := testGuardContext(100)
	openable, _ := engine.prepareStructureEntry(ctx, &parent, data, time.UnixMilli(parent.DecisionCloseTime))
	if openable {
		t.Fatal("默认配置下父结构应只背景化")
	}

	fresh := testSignal(SignalSell2, SideShort, base, base, 110, 70, 100)
	fresh.ParentSignalID = parent.SignalID
	fresh.EntryTriggerID = StableEntryTriggerID("t1", fresh.Symbol, parent.SignalID, "pullback_retest_resume", "15m", base, engine.Policy.ConfigHash)
	fresh.EntryTriggerType = "pullback_retest_resume"
	fresh.EntryTriggerTF = "15m"
	fresh.EntryTriggerClose = base
	fresh.SourceLayer = "entry_trigger"
	d := engine.signalToMainDecision(ctx, fresh)
	if d.SignalID != fresh.EntryTriggerID {
		t.Fatalf("fresh trigger应使用独立id: %+v", d)
	}
	if msg, suppressed := engine.suppressedSignalDiagnostic(ctx, d); suppressed {
		t.Fatalf("父结构suppression不应阻塞新的entry trigger: %s", msg)
	}
}

func TestEntryTimingPullbackRetestResumeBuildsValidOpenCandidate(t *testing.T) {
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	tradeClose := base + int64(2*time.Hour/time.Millisecond)
	signal := testSignal(SignalBuy2, SideLong, base, tradeClose, 90, 150, 100)
	ctx, data := testGuardContext(105)
	data.Klines = map[string][]market.Kline{
		"1h": {{
			OpenTime:  tradeClose - int64(time.Hour/time.Millisecond) + 1,
			CloseTime: tradeClose,
			Open:      102,
			High:      108,
			Low:       99,
			Close:     104,
			Volume:    10,
		}},
		"15m": {
			{OpenTime: tradeClose + 1, CloseTime: tradeClose + int64(15*time.Minute/time.Millisecond), Open: 106, High: 107, Low: 103, Close: 104, Volume: 5},
			{OpenTime: tradeClose + int64(15*time.Minute/time.Millisecond) + 1, CloseTime: tradeClose + int64(30*time.Minute/time.Millisecond), Open: 104, High: 106, Low: 104, Close: 105, Volume: 6},
		},
	}

	openable, diagnostics := engine.prepareStructureEntry(ctx, &signal, data, time.UnixMilli(tradeClose))
	if !openable {
		t.Fatalf("闭合15m pullback/retest/resume应生成有效入场触发: signal=%+v diagnostics=%+v", signal, diagnostics)
	}
	if signal.SourceLayer != "entry_trigger" || signal.EntryTriggerType != "pullback_retest_resume" ||
		signal.EntryTriggerID == "" || signal.ParentSignalID == "" || signal.EntryTriggerClose != data.Klines["15m"][1].CloseTime {
		t.Fatalf("pullback trigger元数据错误: %+v", signal)
	}
	d := engine.signalToMainDecision(ctx, signal)
	if d.Action != "open_long" || d.SignalID != signal.EntryTriggerID {
		t.Fatalf("pullback trigger应转换为独立open candidate: %+v", d)
	}
	guarded, rejection, guardDiagnostics := engine.applyProgrammaticSignalGuard(ctx, signal, d, data, time.UnixMilli(signal.EntryTriggerClose))
	if rejection != nil || guarded.Action != "open_long" || metadataString(guarded.StrategyMetadata, "freshness_state") != "fresh" {
		t.Fatalf("pullback trigger应通过时效和窗口guard: guarded=%+v rejection=%+v diagnostics=%+v", guarded, rejection, guardDiagnostics)
	}
	markers := engine.StateStore.RecentSignalMarkers("t1", "SOLUSDT", 10)
	if len(markers) != 1 || markers[0].SourceLayer != "entry_trigger" || markers[0].SignalID != signal.EntryTriggerID ||
		markers[0].ParentSignalID != signal.ParentSignalID || markers[0].EntryTriggerType != "pullback_retest_resume" {
		t.Fatalf("应写入entry_trigger ready marker: %+v", markers)
	}
}

func TestEntryTimingReplayFixturesOld161SignalsDoNotOpen(t *testing.T) {
	type staleFixture struct {
		Symbol         string  `json:"symbol"`
		SignalType     string  `json:"signal_type"`
		Direction      string  `json:"direction"`
		AgeCandles     int     `json:"age_candles"`
		Price          float64 `json:"price"`
		StopLoss       float64 `json:"stop_loss"`
		TakeProfit     float64 `json:"take_profit"`
		CurrentPrice   float64 `json:"current_price"`
		ExpectedStatus string  `json:"expected_status"`
		ExpectedReason string  `json:"expected_reason"`
	}
	raw, err := os.ReadFile("testdata/entry_timing_161_stale_signals.json")
	if err != nil {
		t.Fatalf("读取161 stale fixture失败: %v", err)
	}
	var fixtures []staleFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("解析161 stale fixture失败: %v", err)
	}
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 17, 20, 59, 59, 0, time.UTC).UnixMilli()
	for _, fixture := range fixtures {
		t.Run(fixture.Symbol, func(t *testing.T) {
			signalClose := base
			decisionClose := signalClose + int64(fixture.AgeCandles)*int64(time.Hour/time.Millisecond)
			signal := ChanlunSignal{
				SignalID:          "fixture-" + fixture.Symbol,
				Symbol:            fixture.Symbol,
				Direction:         fixture.Direction,
				SignalType:        fixture.SignalType,
				ActionHint:        "open",
				AnalysisTF:        "1h",
				TriggerTF:         "15m",
				Level:             "1h",
				Price:             fixture.Price,
				StopLoss:          fixture.StopLoss,
				TakeProfit:        fixture.TakeProfit,
				StructureTarget:   fixture.TakeProfit,
				Confidence:        90,
				SignalCloseTime:   signalClose,
				TriggerCloseTime:  signalClose,
				DecisionCloseTime: decisionClose,
				SegmentStartTime:  signalClose - int64(time.Hour/time.Millisecond),
				SegmentEndTime:    signalClose,
				SourceLayer:       "structure",
				Status:            "detected",
			}
			ctx := &decision.Context{
				TraderID:        "t1",
				BTCETHLeverage:  5,
				AltcoinLeverage: 5,
				MarketDataMap:   map[string]*market.Data{fixture.Symbol: {Symbol: fixture.Symbol, CurrentPrice: fixture.CurrentPrice}},
			}
			data := ctx.MarketDataMap[fixture.Symbol]
			openable, diagnostics := engine.prepareStructureEntry(ctx, &signal, data, time.UnixMilli(decisionClose))
			if openable {
				t.Fatalf("161旧信号不应生成open candidate: fixture=%+v signal=%+v diagnostics=%+v", fixture, signal, diagnostics)
			}
			if signal.Status != fixture.ExpectedStatus {
				t.Fatalf("fixture状态错误: expected=%s signal=%+v diagnostics=%+v", fixture.ExpectedStatus, signal, diagnostics)
			}
			gotReason := signal.EntryWindowState
			if signal.EntryInvalidated {
				gotReason = signal.EntryInvalidReason
			}
			if gotReason != fixture.ExpectedReason {
				t.Fatalf("fixture拒绝/背景原因错误: expected=%s got=%s signal=%+v", fixture.ExpectedReason, gotReason, signal)
			}
			action := "open_long"
			if fixture.Direction == SideShort {
				action = "open_short"
			}
			if got, ok := engine.StateStore.SuppressedSignalForAction("t1", fixture.Symbol, signal.SignalID, action); !ok || got.ReasonCode != fixture.ExpectedReason {
				t.Fatalf("fixture应写入suppression: got=%+v ok=%v", got, ok)
			}
		})
	}
}
