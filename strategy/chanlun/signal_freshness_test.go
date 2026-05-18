package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFreshnessEngine(t *testing.T) *Engine {
	t.Helper()
	policy := testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json"))
	policy.TakeProfit.MinNetRR = 2.5
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	return engine
}

func testSignal(signalType, direction string, signalClose, decisionClose int64, stop, target, price float64) ChanlunSignal {
	return ChanlunSignal{
		SignalID:          "sig-" + signalType + "-" + direction,
		Symbol:            "SOLUSDT",
		Direction:         direction,
		SignalType:        signalType,
		AnalysisTF:        "1h",
		TriggerTF:         "15m",
		Level:             "1h",
		Price:             price,
		StopLoss:          stop,
		TakeProfit:        target,
		StructureTarget:   target,
		Confidence:        90,
		SignalCloseTime:   signalClose,
		TriggerCloseTime:  signalClose,
		DecisionCloseTime: decisionClose,
		SegmentStartTime:  signalClose - int64(time.Hour/time.Millisecond),
		SegmentEndTime:    signalClose,
		SourceLayer:       "main_signal",
		Status:            "detected",
	}
}

func testGuardContext(current float64) (*decision.Context, *market.Data) {
	data := &market.Data{Symbol: "SOLUSDT", CurrentPrice: current}
	ctx := &decision.Context{
		TraderID:        "t1",
		BTCETHLeverage:  5,
		AltcoinLeverage: 5,
		MarketDataMap:   map[string]*market.Data{"SOLUSDT": data},
	}
	return ctx, data
}

func TestProgrammaticSignalGuardRejectsTargetCrossedShortBeforeGenericShape(t *testing.T) {
	engine := testFreshnessEngine(t)
	signalClose := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	decisionClose := time.Date(2026, 5, 18, 13, 59, 59, 0, time.UTC).UnixMilli()
	signal := testSignal(SignalSell2, SideShort, signalClose, decisionClose, 86.92, 85.89, 85.9)
	ctx, data := testGuardContext(84.82)
	d := engine.signalToMainDecision(ctx, signal)

	_, rejection, _ := engine.applyProgrammaticSignalGuard(ctx, signal, d, data, time.UnixMilli(decisionClose))
	if rejection == nil {
		t.Fatal("越过空单止盈目标应被预校验拒绝")
	}
	if len(rejection.GateReasons) == 0 || rejection.GateReasons[0] != "target_already_crossed" {
		t.Fatalf("应优先标记target_already_crossed，而不是通用结构错误: %+v", rejection)
	}
	if !strings.Contains(rejection.Reason, "已越过止盈目标") {
		t.Fatalf("拒绝原因应说明错过目标: %s", rejection.Reason)
	}
	state := engine.StateStore.SymbolState("t1", "SOLUSDT")
	if len(state.SuppressedSignals) != 1 {
		t.Fatalf("target-crossed应写入suppressed_signals: %+v", state.SuppressedSignals)
	}
	if len(state.ExecutedSignals) != 0 {
		t.Fatalf("拒绝信号不应写入executed_signals: %+v", state.ExecutedSignals)
	}
	markers := engine.StateStore.RecentSignalMarkers("t1", "SOLUSDT", 10)
	if len(markers) != 1 || markers[0].Status != "rejected" || markers[0].FreshnessState == "" {
		t.Fatalf("应写入带freshness元数据的rejected marker: %+v", markers)
	}
}

func TestProgrammaticSignalGuardExpiredAgedAndFreshCases(t *testing.T) {
	engine := testFreshnessEngine(t)
	base := time.Date(2026, 5, 18, 5, 59, 59, 0, time.UTC).UnixMilli()
	ctx, data := testGuardContext(100)

	expired := testSignal(SignalBuy1, SideLong, base, base+int64(5*time.Hour/time.Millisecond), 90, 130, 100)
	expiredDecision := engine.signalToMainDecision(ctx, expired)
	_, rejection, _ := engine.applyProgrammaticSignalGuard(ctx, expired, expiredDecision, data, time.UnixMilli(expired.DecisionCloseTime))
	if rejection == nil || rejection.GateReasons[0] != "signal_expired" {
		t.Fatalf("超过硬生命周期应过期拒绝: %+v", rejection)
	}

	expiredShort := testSignal(SignalSell1, SideShort, base, base+int64(5*time.Hour/time.Millisecond), 110, 70, 100)
	expiredShortDecision := engine.signalToMainDecision(ctx, expiredShort)
	_, rejection, _ = engine.applyProgrammaticSignalGuard(ctx, expiredShort, expiredShortDecision, data, time.UnixMilli(expiredShort.DecisionCloseTime))
	if rejection == nil || rejection.GateReasons[0] != "signal_expired" {
		t.Fatalf("空头超过硬生命周期也应过期拒绝: %+v", rejection)
	}

	aged := testSignal(SignalBuy1, SideLong, base, base+int64(3*time.Hour/time.Millisecond), 90, 140, 100)
	agedDecision := engine.signalToMainDecision(ctx, aged)
	guarded, rejection, _ := engine.applyProgrammaticSignalGuard(ctx, aged, agedDecision, data, time.UnixMilli(aged.DecisionCloseTime))
	if rejection != nil {
		t.Fatalf("aged但结构/RR有效时不应硬拒绝: %+v", rejection)
	}
	if guarded.Confidence != 87 || metadataString(guarded.StrategyMetadata, "freshness_state") != "aged" {
		t.Fatalf("aged信号应衰减置信度并写元数据: confidence=%d metadata=%+v", guarded.Confidence, guarded.StrategyMetadata)
	}

	agedInvalidRR := testSignal(SignalBuy1, SideLong, base, base+int64(3*time.Hour/time.Millisecond), 90, 120, 100)
	agedInvalidDecision := engine.signalToMainDecision(ctx, agedInvalidRR)
	_, rejection, _ = engine.applyProgrammaticSignalGuard(ctx, agedInvalidRR, agedInvalidDecision, data, time.UnixMilli(agedInvalidRR.DecisionCloseTime))
	if rejection == nil || rejection.GateReasons[0] != "remaining_net_rr_too_low" {
		t.Fatalf("aged且剩余RR不足应拒绝: %+v", rejection)
	}

	fresh := testSignal(SignalBuy1, SideLong, base, base+int64(time.Hour/time.Millisecond), 90, 140, 100)
	freshDecision := engine.signalToMainDecision(ctx, fresh)
	guarded, rejection, _ = engine.applyProgrammaticSignalGuard(ctx, fresh, freshDecision, data, time.UnixMilli(fresh.DecisionCloseTime))
	if rejection != nil || guarded.Confidence != 90 || metadataString(guarded.StrategyMetadata, "freshness_state") != "fresh" {
		t.Fatalf("fresh有效信号应原样通过: d=%+v rejection=%+v", guarded, rejection)
	}
}

func TestPreviewTwoClosedComponentsBuildsWatchlistMarker(t *testing.T) {
	lastHourClose := time.Date(2026, 5, 18, 5, 59, 59, 999*int(time.Millisecond), time.UTC).UnixMilli()
	components := []market.Kline{
		{OpenTime: lastHourClose + 1, CloseTime: lastHourClose + int64(15*time.Minute/time.Millisecond), Open: 100, High: 103, Low: 99, Close: 102, Volume: 10},
		{OpenTime: lastHourClose + int64(15*time.Minute/time.Millisecond) + 1, CloseTime: lastHourClose + int64(30*time.Minute/time.Millisecond), Open: 102, High: 104, Low: 101, Close: 103, Volume: 12},
	}
	data := &market.Data{Klines: map[string][]market.Kline{
		"1h":  {{OpenTime: lastHourClose - int64(time.Hour/time.Millisecond) + 1, CloseTime: lastHourClose, Open: 98, High: 101, Low: 97, Close: 100}},
		"15m": components,
	}}
	got, phase, count, diagnostics := previewClosedComponents(data, "1h", "15m", 2)
	if len(diagnostics) != 0 || len(got) != 2 || phase != "preview_2x15m" || count != 2 {
		t.Fatalf("应使用2根闭合15m生成preview_2x15m: components=%+v phase=%s count=%d diag=%+v", got, phase, count, diagnostics)
	}
	synthetic := syntheticTradeKlineFromComponents(got)
	if synthetic.Open != 100 || synthetic.High != 104 || synthetic.Low != 99 || synthetic.Close != 103 || synthetic.Volume != 22 {
		t.Fatalf("合成1h预览K线错误: %+v", synthetic)
	}

	marker := signalToMarker(ChanlunSignal{
		SignalID:          "preview-sig",
		Symbol:            "SOLUSDT",
		Direction:         SideLong,
		SignalType:        SignalBuy2,
		AnalysisTF:        "1h",
		SignalCloseTime:   lastHourClose,
		DecisionCloseTime: components[1].CloseTime,
		SourceLayer:       "preview_signal",
		Status:            "watchlist",
		PreviewPhase:      phase,
		PreviewSourceTF:   "15m",
		PreviewComponents: 2,
	}, "", "", "", "")
	if marker.SourceLayer != "preview_signal" || marker.Status != "watchlist" || marker.PreviewPhase != phase || marker.DisplayCloseTime != components[1].CloseTime {
		t.Fatalf("preview marker元数据错误: %+v", marker)
	}
}

func TestPreviewMarkerReconcilesOnlySameTradeBucket(t *testing.T) {
	engine := testFreshnessEngine(t)
	confirmedClose := time.Date(2026, 5, 18, 6, 59, 59, 0, time.UTC).UnixMilli()
	sameBucketPreviewClose := confirmedClose - int64(30*time.Minute/time.Millisecond)
	oldPreviewClose := confirmedClose - int64(90*time.Minute/time.Millisecond)
	engine.StateStore.StoreSignalMarker("t1", "SOLUSDT", SignalMarker{
		Symbol:            "SOLUSDT",
		Timeframe:         "1h",
		CloseTime:         sameBucketPreviewClose,
		SignalCloseTime:   sameBucketPreviewClose,
		DecisionCloseTime: sameBucketPreviewClose,
		DisplayCloseTime:  sameBucketPreviewClose,
		SignalType:        SignalSell2,
		Direction:         SideShort,
		Level:             "1h",
		SourceLayer:       "preview_signal",
		Status:            "watchlist",
		SignalID:          "preview-current",
		PreviewPhase:      "preview_2x15m",
		PreviewSourceTF:   "15m",
		PreviewComponents: 2,
	})
	engine.StateStore.StoreSignalMarker("t1", "SOLUSDT", SignalMarker{
		Symbol:            "SOLUSDT",
		Timeframe:         "1h",
		CloseTime:         oldPreviewClose,
		SignalCloseTime:   oldPreviewClose,
		DecisionCloseTime: oldPreviewClose,
		DisplayCloseTime:  oldPreviewClose,
		SignalType:        SignalSell2,
		Direction:         SideShort,
		Level:             "1h",
		SourceLayer:       "preview_signal",
		Status:            "watchlist",
		SignalID:          "preview-old",
		PreviewPhase:      "preview_2x15m",
		PreviewSourceTF:   "15m",
		PreviewComponents: 2,
	})

	engine.reconcilePreviewMarkers("t1", "SOLUSDT", ChanlunSignal{
		SignalID:          "confirmed",
		Symbol:            "SOLUSDT",
		SignalType:        SignalSell2,
		Direction:         SideShort,
		DecisionCloseTime: confirmedClose,
	})

	markers := engine.StateStore.RecentSignalMarkers("t1", "SOLUSDT", 10)
	statusByID := map[string]SignalMarker{}
	for _, marker := range markers {
		statusByID[marker.SignalID] = marker
	}
	if !statusByID["preview-current"].PreviewConfirmed || statusByID["preview-current"].Status != "confirmed" {
		t.Fatalf("同一小时桶preview应被confirmed_1h确认: %+v", statusByID["preview-current"])
	}
	if statusByID["preview-old"].PreviewConfirmed || statusByID["preview-old"].Status != "watchlist" {
		t.Fatalf("旧小时桶preview不应被新1h信号确认: %+v", statusByID["preview-old"])
	}
}

func TestPreviewPilotSizingUsesCappedRiskFraction(t *testing.T) {
	engine := testFreshnessEngine(t)
	engine.Policy.PreviewSignals.PilotRiskFraction = 0.25
	ctx := &decision.Context{
		Account:         decision.AccountInfo{TotalEquity: 10000, AvailableBalance: 10000},
		MaxRiskPerTrade: 0.02,
	}
	data := &market.Data{CurrentPrice: 100}
	d := decision.Decision{
		Symbol:           "SOLUSDT",
		Action:           "open_long",
		Leverage:         5,
		StopLoss:         90,
		StrategyMetadata: map[string]any{},
	}
	engine.applyPreviewPilotSizing(ctx, data, &d)
	if d.PositionSizeUSD <= 0 {
		t.Fatalf("preview pilot应生成受限仓位: %+v", d)
	}
	if got := metadataFloat64(d.StrategyMetadata, "pilot_effective_risk_pct"); got != 0.005 {
		t.Fatalf("pilot风险应为max risk的25%%: %.6f metadata=%+v", got, d.StrategyMetadata)
	}
	if d.PositionSizeUSD > 500 {
		t.Fatalf("pilot仓位应按0.5%%风险约束，而不是满风险仓位: %.4f", d.PositionSizeUSD)
	}
}
