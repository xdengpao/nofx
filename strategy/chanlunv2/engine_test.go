package chanlunv2

import (
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"nofx/strategy/chanlun"
	"strings"
	"testing"
	"time"
)

func TestLatestSignalsWithOptionsReturnsV2ReportMarkers(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{
		Timeframes: map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"},
	})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	centerID := 7
	signal := Signal{
		SignalType:         "buy1",
		Direction:          "long",
		Price:              100,
		StopLoss:           95,
		TakeProfit:         115,
		Confidence:         88,
		CenterID:           &centerID,
		DivergenceStrength: 0.82,
		Timestamp:          1710000000000,
	}
	mr := &multiLevelResult{
		Symbol: "ETHUSDT",
		Results: map[string]*AnalysisResult{
			"trade": {
				Trend:   "down_trend",
				Signals: []Signal{signal},
			},
		},
	}

	engine.setLatestReport("t1", "ETHUSDT", mr, []Signal{signal}, []string{"ETHUSDT buy1 置信度88"})
	report, ok := engine.LatestSignalsWithOptions("t1", "ETHUSDT", chanlun.SignalReportOptions{})
	if !ok {
		t.Fatal("应返回最新信号报告")
	}
	if report.DecisionMode != "chanlun_v2" || report.TradeTimeframe != "1h" || report.ComponentTimeframe != "15m" {
		t.Fatalf("报告元数据不符合预期: %+v", report)
	}
	if len(report.Signals) != 1 || report.Signals[0].ActionHint != "open_long" {
		t.Fatalf("信号转换不符合预期: %+v", report.Signals)
	}
	if len(report.SignalMarkers) != 1 {
		t.Fatalf("应生成一个marker: %+v", report.SignalMarkers)
	}
	marker := report.SignalMarkers[0]
	if marker.DisplayCategory != "trade_action" || marker.TradeIntent != "open_long" || marker.Status != "ready" {
		t.Fatalf("marker无法被前端识别为交易动作: %+v", marker)
	}
}

func TestLatestSignalsWithOptionsFiltersV2Markers(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	buy := Signal{SignalType: "buy2", Direction: "long", Confidence: 80, Timestamp: 1710000000000}
	sell := Signal{SignalType: "sell1", Direction: "short", Confidence: 80, Timestamp: 1710003600000}
	engine.setLatestReport("t1", "BTCUSDT", nil, []Signal{buy, sell}, nil)

	report, ok := engine.LatestSignalsWithOptions("t1", "BTCUSDT", chanlun.SignalReportOptions{
		View:     "audit",
		Statuses: []string{"ready"},
		Limit:    1,
	})
	if !ok {
		t.Fatal("应返回最新信号报告")
	}
	if report.View != "audit" || report.Filters.Limit != 1 {
		t.Fatalf("应保留audit过滤参数: %+v", report)
	}
	if len(report.SignalMarkers) != 1 || report.MarkerSummary.TotalRaw != 2 || report.MarkerSummary.TotalReturned != 1 {
		t.Fatalf("limit过滤摘要不符合预期: markers=%+v summary=%+v", report.SignalMarkers, report.MarkerSummary)
	}
}

func TestValidateChanlunV2DecisionsEnrichesOpenSizing(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	sig := Signal{
		SignalType:         "buy2",
		Direction:          "long",
		Price:              100,
		StopLoss:           95,
		TakeProfit:         120,
		Confidence:         90,
		DivergenceStrength: 0.7,
		Timestamp:          1710000000000,
	}
	d := engine.signalToDecision(ctx, "SOLUSDT", sig, "1h")
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, nil)
	if len(rejections) != 0 {
		t.Fatalf("开仓信号不应被拒绝: %+v", rejections)
	}
	if len(valid) != 1 {
		t.Fatalf("应保留一个有效决策: %+v", valid)
	}
	got := valid[0]
	if got.PositionSizeUSD <= 0 || got.RequestedPositionSizeUSD <= 0 || got.AdjustedPositionSizeUSD <= 0 || got.RiskUSD <= 0 {
		t.Fatalf("V2开仓决策应补全仓位和风险字段: %+v", got)
	}
}

func TestValidateChanlunV2DecisionsRiskBlockedRejectsOpen(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	d := engine.signalToDecision(ctx, "SOLUSDT", Signal{
		SignalType: "buy2",
		Direction:  "long",
		StopLoss:   95,
		TakeProfit: 120,
		Confidence: 90,
		Timestamp:  1710000000000,
	}, "1h")
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, &decision.CyclePreparation{
		RiskIncreaseBlocked: true,
		StopReason:          "测试阻断",
	})
	if len(valid) != 0 {
		t.Fatalf("风险阻断时不应保留开仓决策: %+v", valid)
	}
	if len(rejections) != 1 || !strings.Contains(rejections[0].Reason, "测试阻断") {
		t.Fatalf("应记录风险阻断拒绝原因: %+v", rejections)
	}
}

func TestChanlunV2FreshnessConfigChangesHash(t *testing.T) {
	base, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建默认引擎失败: %v", err)
	}
	custom, err := NewEngine(config.ChanlunV2StrategyConfig{
		SignalFreshness: config.ChanlunV2SignalFreshnessConfig{SoftAgeCandles: 3},
	})
	if err != nil {
		t.Fatalf("创建自定义引擎失败: %v", err)
	}
	if base.configHash == custom.configHash {
		t.Fatalf("signal_freshness变化应改变config_hash: %s", base.configHash)
	}
}

func TestSignalToDecisionUsesEvaluationCloseTimeButStableSignalID(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	signalClose := int64(1710000000000)
	evaluationClose := signalClose + int64(3*time.Hour/time.Millisecond)
	sig := Signal{
		SignalType: "buy2",
		Direction:  "long",
		StopLoss:   95,
		TakeProfit: 120,
		Confidence: 90,
		Timestamp:  signalClose,
	}
	d := engine.signalToDecision(ctx, "BNBUSDT", sig, "1h", evaluationClose)
	if d.SignalID != "chanlun_v2:BNBUSDT:1h:buy2:1710000000000" {
		t.Fatalf("signal_id应继续基于结构信号时间: %s", d.SignalID)
	}
	if got := metadataInt64(d.StrategyMetadata, "signal_close_time"); got != signalClose {
		t.Fatalf("signal_close_time应保持结构时间: %d", got)
	}
	if got := metadataInt64(d.StrategyMetadata, "decision_close_time"); got != evaluationClose {
		t.Fatalf("decision_close_time应使用评估K线: %d", got)
	}
	if got := metadataInt64(d.StrategyMetadata, "evaluation_close_time"); got != evaluationClose {
		t.Fatalf("evaluation_close_time应使用评估K线: %d", got)
	}
}

func TestEvaluationCloseTimeUsesLatestClosedKline(t *testing.T) {
	closeTimeSeconds := int64(1710003600)
	got := latestKlineCloseMillis([]market.Kline{{CloseTime: closeTimeSeconds}})
	if got != closeTimeSeconds*1000 {
		t.Fatalf("latestKlineCloseMillis应归一化为毫秒: %d", got)
	}
	mr := &multiLevelResult{LastClosedByLevel: map[string]int64{"trade": closeTimeSeconds}}
	if eval := evaluationCloseTime(mr, "trade"); eval != closeTimeSeconds*1000 {
		t.Fatalf("evaluationCloseTime应读取当前分析结果的最新闭合K线: %d", eval)
	}
}

func TestApplyChanlunV2FreshnessGuardFreshAndAged(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	signalClose := int64(1710000000000)
	fresh := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose, 90, 95, 120)
	aged := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose+int64(2*time.Hour/time.Millisecond), 90, 95, 120)

	valid, rejections, suppressed := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{fresh, aged}, map[string]string{"trade": "1h"})
	if len(rejections) != 0 || len(valid) != 2 {
		t.Fatalf("fresh/soft-aged信号不应直接拒绝: valid=%+v rejections=%+v", valid, rejections)
	}
	if len(suppressed) != 0 {
		t.Fatalf("fresh/soft-aged信号不应被静默: %+v", suppressed)
	}
	if state := metadataString(valid[0].StrategyMetadata, "freshness_state"); state != "fresh" {
		t.Fatalf("首个信号应为fresh: %s", state)
	}
	if state := metadataString(valid[1].StrategyMetadata, "freshness_state"); state != "aged" {
		t.Fatalf("第二个信号应为aged: %s", state)
	}
	if valid[1].Confidence != 80 {
		t.Fatalf("soft-aged应按默认每根10衰减置信度: %d", valid[1].Confidence)
	}
}

func TestApplyChanlunV2FreshnessGuardRejectsExpired(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	signalClose := int64(1710000000000)
	expired := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose+int64(3*time.Hour/time.Millisecond), 90, 95, 120)
	valid, rejections, suppressed := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{expired}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 1 {
		t.Fatalf("hard-expired信号应被freshness gate拒绝: valid=%+v rejections=%+v", valid, rejections)
	}
	if len(suppressed) != 0 {
		t.Fatalf("首次过期拒绝不应被静默: %+v", suppressed)
	}
	if rejections[0].FreshnessState != "expired" || rejections[0].AgeCandles != 3 {
		t.Fatalf("拒绝元数据不符合预期: %+v", rejections[0])
	}
	if len(rejections[0].GateReasons) != 1 || rejections[0].GateReasons[0] != "freshness_gate.signal_expired" {
		t.Fatalf("应使用freshness_gate原因码: %+v", rejections[0].GateReasons)
	}
}

func TestApplyChanlunV2FreshnessGuardSuppressesRepeatedTerminalStaleSignal(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	signalClose := int64(1710000000000)
	decisionClose := signalClose + int64(3*time.Hour/time.Millisecond)
	expired := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, decisionClose, 90, 95, 120)

	valid, rejections, suppressed := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{expired}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 1 || len(suppressed) != 0 {
		t.Fatalf("首次终态过期应记录拒绝: valid=%+v rejections=%+v suppressed=%+v", valid, rejections, suppressed)
	}

	valid, rejections, suppressed = engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{expired}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 0 || len(suppressed) != 1 {
		t.Fatalf("重复终态过期应静默: valid=%+v rejections=%+v suppressed=%+v", valid, rejections, suppressed)
	}
	if !strings.Contains(suppressed[0], "重复过期信号已静默") || !strings.Contains(suppressed[0], "freshness_gate.signal_expired") {
		t.Fatalf("静默诊断应说明原因: %+v", suppressed)
	}

	newSignal := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose+int64(time.Hour/time.Millisecond), decisionClose+int64(time.Hour/time.Millisecond), 90, 95, 120)
	valid, rejections, suppressed = engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{newSignal}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 1 || len(suppressed) != 0 {
		t.Fatalf("新signal_id应重新评估并记录首次拒绝: valid=%+v rejections=%+v suppressed=%+v", valid, rejections, suppressed)
	}
}

func TestApplyChanlunV2FreshnessGuardSuppressesTerminalLifecycleBeforeReevaluation(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 121)
	signalClose := int64(1710000000000)
	crossed := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose, 90, 95, 120)

	valid, rejections, suppressed := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{crossed}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 1 || rejections[0].FreshnessState != "target_crossed" || len(suppressed) != 0 {
		t.Fatalf("首次目标穿越应记录终态拒绝: valid=%+v rejections=%+v suppressed=%+v", valid, rejections, suppressed)
	}

	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	valid, rejections, suppressed = engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{crossed}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 0 || len(suppressed) != 1 {
		t.Fatalf("同一signal_id终态后即使价格回落也应静默: valid=%+v rejections=%+v suppressed=%+v", valid, rejections, suppressed)
	}
}

func TestApplyChanlunV2FreshnessGuardRejectsTargetCrossedAndRRInvalid(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 121)
	signalClose := int64(1710000000000)
	crossed := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose, 90, 95, 120)
	_, rejections, _ := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{crossed}, map[string]string{"trade": "1h"})
	if len(rejections) != 1 || rejections[0].FreshnessState != "target_crossed" {
		t.Fatalf("目标穿越应被拒绝: %+v", rejections)
	}

	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 119)
	rrInvalidSignalClose := signalClose + int64(time.Hour/time.Millisecond)
	rrInvalid := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", rrInvalidSignalClose, rrInvalidSignalClose, 90, 95, 120)
	_, rejections, _ = engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{rrInvalid}, map[string]string{"trade": "1h"})
	if len(rejections) != 1 || rejections[0].FreshnessState != "rr_invalid" {
		t.Fatalf("剩余RR不足应被拒绝: %+v", rejections)
	}

	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 79)
	shortCrossedSignalClose := signalClose + int64(2*time.Hour/time.Millisecond)
	shortCrossed := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "sell2", "open_short", shortCrossedSignalClose, shortCrossedSignalClose, 90, 105, 80)
	_, rejections, _ = engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{shortCrossed}, map[string]string{"trade": "1h"})
	if len(rejections) != 1 || rejections[0].FreshnessState != "target_crossed" {
		t.Fatalf("空单目标穿越应被拒绝: %+v", rejections)
	}
}

func TestMarkRejectedOpenMarkersMovesMarkerToDecisionClose(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	signalClose := int64(1710000000000)
	evaluationClose := signalClose + int64(3*time.Hour/time.Millisecond)
	sig := Signal{SignalType: "buy2", Direction: "long", Price: 100, StopLoss: 95, TakeProfit: 120, Confidence: 90, Timestamp: signalClose}
	mr := &multiLevelResult{LastClosedByLevel: map[string]int64{"trade": evaluationClose}}
	engine.setLatestReport(ctx.TraderID, "BNBUSDT", mr, []Signal{sig}, nil)
	d := engine.signalToDecision(ctx, "BNBUSDT", sig, "1h", evaluationClose)
	valid, rejections, _ := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{d}, map[string]string{"trade": "1h"})
	if len(valid) != 0 || len(rejections) != 1 {
		t.Fatalf("应构造一个过期拒绝: valid=%+v rejections=%+v", valid, rejections)
	}
	engine.markRejectedOpenMarkers(ctx, []decision.Decision{d}, nil, rejections)
	report, ok := engine.LatestSignalsWithOptions(ctx.TraderID, "BNBUSDT", chanlun.SignalReportOptions{})
	if !ok || len(report.SignalMarkers) != 1 {
		t.Fatalf("应返回已更新marker: ok=%v report=%+v", ok, report)
	}
	marker := report.SignalMarkers[0]
	if marker.Status != "rejected" || marker.SignalCloseTime != signalClose || marker.CloseTime != signalClose {
		t.Fatalf("拒绝marker应保留结构时间: %+v", marker)
	}
	if marker.DisplayCloseTime != evaluationClose || marker.DecisionCloseTime != evaluationClose || marker.EvaluationCloseTime != evaluationClose {
		t.Fatalf("拒绝marker应显示在评估K线: %+v", marker)
	}
	if marker.FreshnessState != "expired" || marker.AgeCandles != 3 || !strings.HasPrefix(marker.ReasonCode, "freshness_gate.") {
		t.Fatalf("拒绝marker应携带freshness元数据: %+v", marker)
	}
}

func TestSetLatestReportPreservesRejectedLifecycleMarker(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2ValidationMarketData("BNBUSDT", 100)
	signalClose := int64(1710000000000)
	evaluationClose := signalClose + int64(3*time.Hour/time.Millisecond)
	sig := Signal{SignalType: "buy2", Direction: "long", Price: 100, StopLoss: 95, TakeProfit: 120, Confidence: 90, Timestamp: signalClose}
	mr := &multiLevelResult{LastClosedByLevel: map[string]int64{"trade": evaluationClose}}
	engine.setLatestReport(ctx.TraderID, "BNBUSDT", mr, []Signal{sig}, nil)
	d := engine.signalToDecision(ctx, "BNBUSDT", sig, "1h", evaluationClose)
	_, rejections, _ := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{d}, map[string]string{"trade": "1h"})
	engine.markRejectedOpenMarkers(ctx, []decision.Decision{d}, nil, rejections)

	engine.setLatestReport(ctx.TraderID, "BNBUSDT", mr, []Signal{sig}, nil)
	report, _ := engine.LatestSignalsWithOptions(ctx.TraderID, "BNBUSDT", chanlun.SignalReportOptions{})
	marker := report.SignalMarkers[0]
	if marker.Status != "rejected" || marker.DisplayCloseTime != evaluationClose {
		t.Fatalf("后续ready报告不应覆盖同一生命周期的rejected状态: %+v", marker)
	}
}

func chanlunV2ValidationContext(equity float64) *decision.Context {
	return &decision.Context{
		TraderID:        "t1",
		BTCETHLeverage:  5,
		AltcoinLeverage: 5,
		MaxRiskPerTrade: 0.02,
		TotalRiskBudget: 0.08,
		Account:         decision.AccountInfo{TotalEquity: equity, AvailableBalance: equity},
		FrequencyPolicy: &decision.FrequencyPolicy{DailyOpenLimit: 10},
		FrequencyState:  &decision.FrequencyState{},
		CorrelationMap:  map[string]*decision.CorrelationData{},
		MarketDataMap: map[string]*market.Data{
			"SOLUSDT": chanlunV2ValidationMarketData("SOLUSDT", 100),
			"BTCUSDT": chanlunV2ValidationMarketData("BTCUSDT", 100000),
		},
	}
}

func chanlunV2DecisionFixture(engine *Engine, ctx *decision.Context, symbol, signalType, action string, signalClose, decisionClose int64, confidence int, stopLoss, takeProfit float64) decision.Decision {
	direction := "long"
	if strings.Contains(action, "short") {
		direction = "short"
	}
	d := engine.signalToDecision(ctx, symbol, Signal{
		SignalType: signalType,
		Direction:  direction,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Confidence: confidence,
		Timestamp:  signalClose,
	}, "1h", decisionClose)
	return d
}

func chanlunV2ValidationMarketData(symbol string, price float64) *market.Data {
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   price,
		CurrentEMA20:   price * 0.98,
		CurrentEMA50:   price * 0.95,
		CurrentADX:     35,
		CurrentDIPlus:  30,
		CurrentDIMinus: 10,
		LongerTermContext: &market.LongerTermData{
			EMA20:     price * 0.98,
			EMA50:     price * 0.95,
			ATR14:     price * 0.02,
			MACDHist:  []float64{1},
			ADXValues: []float64{35},
			DIPlus:    []float64{30},
			DIMinus:   []float64{10},
		},
	}
}
