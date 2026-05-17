package chanlun

import (
	"nofx/decision"
	"nofx/market"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testProgrammaticPolicy(statePath string) decision.ProgrammaticStrategyPolicy {
	pm := defaultPositionManagementPolicy(30)
	pm.StructureBreak.Enabled = false
	pm.FloatingDrawdown.Enabled = false
	pm.ShortTrade.Enabled = false
	return decision.ProgrammaticStrategyPolicy{
		DecisionMode:       "programmatic",
		StrategyName:       "chanlun_programmatic",
		StrategyVersion:    "v1",
		ConfigHash:         "hash",
		AllowLong:          true,
		AllowShort:         true,
		Timeframes:         decision.ProgrammaticTimeframesPolicy{Higher: "4h", Trade: "1h", Sub: "15m", Micro: "3m"},
		HistoryDepth:       decision.ProgrammaticHistoryDepth{M3: 60, M15: 60, H1: 60, H4: 60},
		Position:           decision.ProgrammaticPositionPolicy{PartialClosePct: 30, AddSizeMultiplier: 0.5},
		PositionManagement: pm,
		State:              decision.ProgrammaticStatePolicy{Path: statePath},
	}
}

func TestPositionManagementBreakevenStop(t *testing.T) {
	dir := t.TempDir()
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(dir, "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	ctx := &decision.Context{
		TraderID: "t1",
		Positions: []decision.PositionInfo{{
			Symbol:           "BTCUSDT",
			Side:             "long",
			EntryPrice:       100,
			MarkPrice:        102,
			StopLoss:         95,
			Quantity:         1,
			UnrealizedPnLPct: 2,
			UpdateTime:       time.Now().UnixMilli(),
		}},
	}
	data := &market.Data{Symbol: "BTCUSDT", CurrentPrice: 102, Klines: map[string][]market.Kline{}}
	d, diag := engine.analyzePosition(ctx, ctx.Positions[0], data, time.Now())
	if d.Action != "update_stop_loss" {
		t.Fatalf("达到保本阈值应输出update_stop_loss: d=%+v diag=%+v", d, diag)
	}
	if d.NewStopLoss <= 100 || d.NewStopLoss >= 102 {
		t.Fatalf("保本止损应高于入场且低于当前价: %.4f", d.NewStopLoss)
	}
	if d.SignalID == "" || d.StrategyMetadata["rule"] != "breakeven" {
		t.Fatalf("应写入确定性signal和规则元数据: %+v", d)
	}
}

func TestPositionSignalIDStable(t *testing.T) {
	dir := t.TempDir()
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(dir, "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	pos := decision.PositionInfo{Symbol: "BTCUSDT", Side: "long", EntryPrice: 100}
	first := engine.positionSignalID("t1", pos, "breakeven", "position", 123, "a")
	second := engine.positionSignalID("t1", pos, "breakeven", "position", 123, "a")
	third := engine.positionSignalID("t1", pos, "breakeven", "position", 124, "a")
	if first == "" || first != second {
		t.Fatalf("相同输入应生成稳定signal id: %s/%s", first, second)
	}
	if first == third {
		t.Fatalf("触发时间变化应生成不同signal id")
	}
	if !engine.StateStore.MarkPositionSignal("t1", "BTCUSDT", "long", "breakeven", first) {
		t.Fatal("首次mark应成功")
	}
	if engine.StateStore.MarkPositionSignal("t1", "BTCUSDT", "long", "breakeven", first) {
		t.Fatal("重复signal id不应再次mark")
	}
}

func TestMainSignalNoNewClosedKlineSkipsOpenOnly(t *testing.T) {
	dir := t.TempDir()
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(dir, "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	data := &market.Data{Symbol: "BTCUSDT", CurrentPrice: 100, Klines: map[string][]market.Kline{
		"1h": testKlines(40, 100, 1),
	}}
	lastClosed := data.Klines["1h"][len(data.Klines["1h"])-1].CloseTime
	engine.StateStore.SetLastAnalyzedClosedKline("t1", "BTCUSDT", "1h", lastClosed)
	signals, diag := engine.analyzeMainSignal("t1", "BTCUSDT", data, time.Now())
	if len(signals) != 0 {
		t.Fatalf("无新闭合K线不应输出主信号: %+v", signals)
	}
	if !containsText(diag, "无新闭合K线") {
		t.Fatalf("应记录无新闭合K线诊断: %+v", diag)
	}
}

func TestValidateProgrammaticDecisionsRiskIncreaseBlocked(t *testing.T) {
	dir := t.TempDir()
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(dir, "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	ctx := &decision.Context{
		TraderID: "t1",
		Positions: []decision.PositionInfo{{
			Symbol:     "BTCUSDT",
			Side:       "long",
			EntryPrice: 100,
			MarkPrice:  110,
			StopLoss:   95,
			Quantity:   1,
		}},
		MarketDataMap: map[string]*market.Data{"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 110}},
	}
	riskDecision := engine.positionDecision(ctx, ctx.Positions[0], "breakeven", "update_stop_loss", "pm-risk", "保本", nil, func(d *decision.Decision) {
		d.NewStopLoss = 101
	})
	openDecision := decision.Decision{Symbol: "ETHUSDT", Action: "open_long", Reasoning: "开仓"}
	valid, rejected := engine.validateProgrammaticDecisions(ctx, []decision.Decision{riskDecision, openDecision}, &decision.CyclePreparation{
		RiskIncreaseBlocked: true,
		StopReason:          "熔断中",
	})
	if len(valid) != 1 || valid[0].Action != "update_stop_loss" {
		t.Fatalf("risk blocked时应保留风险降低动作: valid=%+v", valid)
	}
	if len(rejected) != 1 || rejected[0].Action != "open_long" {
		t.Fatalf("risk blocked时应拒绝open/add: rejected=%+v", rejected)
	}
}

func TestFloatingDrawdownAndStructureBreak(t *testing.T) {
	dir := t.TempDir()
	policy := testProgrammaticPolicy(filepath.Join(dir, "state.json"))
	policy.PositionManagement.Breakeven.Enabled = false
	policy.PositionManagement.StructureBreak.Enabled = true
	policy.PositionManagement.FloatingDrawdown.Enabled = true
	engine, err := NewEngine(policy)
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	ctx := &decision.Context{TraderID: "t1"}
	pos := decision.PositionInfo{
		Symbol:           "BTCUSDT",
		Side:             "long",
		EntryPrice:       100,
		MarkPrice:        105,
		StopLoss:         95,
		Quantity:         1,
		UnrealizedPnLPct: 2,
		UpdateTime:       123,
	}
	data := &market.Data{Symbol: "BTCUSDT", CurrentPrice: 105, Klines: map[string][]market.Kline{}}
	state := ProgrammaticPositionState{Side: "long", PeakPrice: 110, PeakR: 2, LastManagedAt: time.Now()}
	d, _ := engine.evaluateFloatingDrawdown(ctx, pos, data, state, time.Now())
	if d.Action != "partial_close" {
		t.Fatalf("达到浮盈回撤阈值应输出partial_close: %+v", d)
	}

	structureKlines := append(testKlines(25, 100, 0), market.Kline{OpenTime: 26_000, CloseTime: 26_999, Open: 100, High: 101, Low: 90, Close: 94})
	level, _, ok := structureBreakLevel(structureKlines, "long")
	if !ok || level <= 0 {
		t.Fatalf("应能计算结构位: %.4f", level)
	}
	if !structureLevelBroken(structureKlines, "long", level) {
		t.Fatalf("最后一根闭合K线跌破结构位应触发结构破坏")
	}
}

func testKlines(count int, base, step float64) []market.Kline {
	klines := make([]market.Kline, 0, count)
	for i := 0; i < count; i++ {
		close := base + float64(i)*step
		klines = append(klines, market.Kline{
			OpenTime:  int64(i * 1000),
			CloseTime: int64(i*1000 + 999),
			Open:      close,
			High:      close + 1,
			Low:       close - 1,
			Close:     close,
			Volume:    1,
		})
	}
	return klines
}

func containsText(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
