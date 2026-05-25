package chanlunv2

import (
	"encoding/json"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"nofx/strategy/chanlun"
	"os"
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
	if marker.DisplayCategory != "structure_background" || marker.SourceLayer != "parent_structure" || marker.Status != "background" {
		t.Fatalf("marker应作为父结构背景展示: %+v", marker)
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
		Statuses: []string{"background"},
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

func TestDowngradeExpiredChanlunV2SignalMarksDiagnosticWithoutOpenRejection(t *testing.T) {
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

	downgraded, diagnostic := engine.downgradeExpiredChanlunV2Signal(ctx, d, "1h")
	if !downgraded || !strings.Contains(diagnostic, "已降级为过期诊断") {
		t.Fatalf("硬过期信号应前置降级为诊断: downgraded=%v diagnostic=%q", downgraded, diagnostic)
	}
	report, ok := engine.LatestSignalsWithOptions(ctx.TraderID, "BNBUSDT", chanlun.SignalReportOptions{})
	if !ok || len(report.SignalMarkers) != 1 {
		t.Fatalf("应保留信号marker: ok=%v report=%+v", ok, report)
	}
	marker := report.SignalMarkers[0]
	if marker.Status != "rejected" || marker.ReasonCode != "freshness_gate.signal_expired" {
		t.Fatalf("前置降级应把marker标记为过期拒绝: %+v", marker)
	}
	if marker.SignalCloseTime != signalClose || marker.DisplayCloseTime != evaluationClose || marker.AgeCandles != 3 {
		t.Fatalf("marker时序和age应来自freshness评估: %+v", marker)
	}

	downgraded, diagnostic = engine.downgradeExpiredChanlunV2Signal(ctx, d, "1h")
	if !downgraded || !strings.Contains(diagnostic, "重复过期信号已静默") {
		t.Fatalf("重复硬过期信号应复用静默诊断: downgraded=%v diagnostic=%q", downgraded, diagnostic)
	}
}

func TestDowngradeExpiredChanlunV2SignalKeepsFreshSignalExecutable(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	signalClose := int64(1710000000000)
	fresh := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", signalClose, signalClose, 90, 95, 120)
	downgraded, diagnostic := engine.downgradeExpiredChanlunV2Signal(ctx, fresh, "1h")
	if downgraded || diagnostic != "" {
		t.Fatalf("fresh信号不应被前置降级: downgraded=%v diagnostic=%q", downgraded, diagnostic)
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

func TestChanlunV2161StaleFixtureDoesNotCreateOpen(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Symbol                   string `json:"symbol"`
			SignalType               string `json:"signal_type"`
			Direction                string `json:"direction"`
			ParentSignalCloseTime    int64  `json:"parent_signal_close_time"`
			FirstEvaluationCloseTime int64  `json:"first_evaluation_close_time"`
			AgeTradeCandles          int    `json:"age_trade_candles"`
			Expected                 string `json:"expected"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/entry_timing_161_stale_signals.json")
	if err != nil {
		t.Fatalf("读取161脱敏fixture失败: %v", err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("解析161脱敏fixture失败: %v", err)
	}
	seenAges := map[int]bool{}
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	for _, item := range fixture.Cases {
		seenAges[item.AgeTradeCandles] = true
		sig := Signal{
			SignalType: item.SignalType,
			Direction:  item.Direction,
			Price:      100,
			StopLoss:   95,
			TakeProfit: 120,
			Confidence: 90,
			Timestamp:  item.ParentSignalCloseTime,
		}
		evaluation := engine.evaluateParentStructureEntry(ctx, item.Symbol, sig, nil, map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"}, item.FirstEvaluationCloseTime)
		if evaluation.Decision.Action != "" {
			t.Fatalf("%s age=%d旧父结构不应直接生成开仓: %+v", item.Symbol, item.AgeTradeCandles, evaluation.Decision)
		}
		if item.Expected == "terminal_expired" && !evaluation.Terminal {
			t.Fatalf("%s age=%d应进入终态过期: %+v", item.Symbol, item.AgeTradeCandles, evaluation)
		}
	}
	for _, age := range []int{3, 8, 19, 27, 58} {
		if !seenAges[age] {
			t.Fatalf("fixture缺少%d根trade candle过期样本", age)
		}
	}
}

func TestEntryTriggerUsesFreshTriggerCloseTimeAndParentLineage(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	base := int64(1710000000000)
	centerID := 3
	sig := Signal{
		SignalType: "buy3",
		Direction:  "long",
		Price:      101,
		StopLoss:   97,
		TakeProfit: 116,
		Confidence: 90,
		CenterID:   &centerID,
		Timestamp:  base,
	}
	mr := chanlunV2QualityResult(base, centerID, "long")
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2EntryMarketData("BNBUSDT", []market.Kline{
		v2TestKline(base-int64(45*time.Minute/time.Millisecond), 100.2, 100.8, 101.6, 99.0),
		v2TestKline(base+int64(15*time.Minute/time.Millisecond), 101.0, 100.9, 101.1, 100.8),
		v2TestKline(base+int64(30*time.Minute/time.Millisecond), 100.9, 101.2, 101.4, 100.8),
	})
	evaluation := engine.evaluateParentStructureEntry(ctx, "BNBUSDT", sig, mr, map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"}, base+int64(30*time.Minute/time.Millisecond))
	if evaluation.Decision.Action != "open_long" || !evaluation.TriggerReady {
		t.Fatalf("fresh pullback trigger应生成开仓候选: %+v", evaluation)
	}
	d := evaluation.Decision
	if d.SignalID == "" || d.SignalID == v2SignalID("BNBUSDT", "1h", sig) {
		t.Fatalf("可执行signal_id应来自entry trigger: %+v", d)
	}
	if metadataString(d.StrategyMetadata, "parent_signal_id") != v2SignalID("BNBUSDT", "1h", sig) {
		t.Fatalf("应保留parent_signal_id: %+v", d.StrategyMetadata)
	}
	if got := metadataInt64(d.StrategyMetadata, "signal_close_time"); got != base+int64(30*time.Minute/time.Millisecond) {
		t.Fatalf("signal_close_time应使用entry trigger close: %d", got)
	}
	if got := metadataInt64(d.StrategyMetadata, "parent_signal_close_time"); got != base {
		t.Fatalf("parent_signal_close_time应保留父结构时间: %d", got)
	}
	valid, rejections, _ := engine.applyChanlunV2FreshnessGuard(ctx, []decision.Decision{d}, map[string]string{"trade": "1h"})
	if len(rejections) != 0 || len(valid) != 1 {
		t.Fatalf("父结构旧但trigger新鲜时应通过freshness: valid=%+v rejections=%+v", valid, rejections)
	}
	if metadataString(valid[0].StrategyMetadata, "freshness_state") != "fresh" || metadataInt(valid[0].StrategyMetadata, "age_candles") != 0 {
		t.Fatalf("freshness应基于trigger close time: %+v", valid[0].StrategyMetadata)
	}
	report, ok := engine.LatestSignalsWithOptions(ctx.TraderID, "BNBUSDT", chanlun.SignalReportOptions{View: "audit"})
	if !ok || len(report.SignalMarkers) == 0 {
		t.Fatalf("应写入entry trigger marker: ok=%v report=%+v", ok, report)
	}
	var triggerMarker *chanlun.SignalMarker
	for i := range report.SignalMarkers {
		if report.SignalMarkers[i].SignalID == d.SignalID {
			triggerMarker = &report.SignalMarkers[i]
		}
	}
	if triggerMarker == nil || triggerMarker.EntryTriggerID != d.SignalID || triggerMarker.ThirdPointQualityCategory != "strong_third_buy" {
		t.Fatalf("entry trigger marker应携带lineage和质量指标: %+v", report.SignalMarkers)
	}
	if triggerMarker.StructureToTriggerLatencyCandles != 2 {
		t.Fatalf("entry trigger marker应记录结构到触发延迟: %+v", triggerMarker)
	}
}

func TestThirdPointQualityRejectsInvalidCases(t *testing.T) {
	tests := []struct {
		name       string
		mutateData func([]market.Kline) []market.Kline
		mutateCfg  func(*config.ChanlunV2StrategyConfig)
		wantReason string
	}{
		{
			name: "reentered center",
			mutateData: func(klines []market.Kline) []market.Kline {
				klines[1].Low = 99.8
				return klines
			},
			wantReason: "third_point.reentered_center",
		},
		{
			name: "gap atr too far",
			mutateCfg: func(cfg *config.ChanlunV2StrategyConfig) {
				cfg.EntryTiming.ThirdPointQuality.MaxSupportGapATR = 0.1
			},
			wantReason: "entry_zone_chased",
		},
		{
			name: "deep retracement",
			mutateCfg: func(cfg *config.ChanlunV2StrategyConfig) {
				cfg.EntryTiming.ThirdPointQuality.MaxRetracementRatio = 0.2
			},
			wantReason: "third_point.deep_retracement",
		},
		{
			name: "too many pullback candles",
			mutateCfg: func(cfg *config.ChanlunV2StrategyConfig) {
				cfg.EntryTiming.ThirdPointQuality.MaxPullbackCandles = 1
			},
			wantReason: "third_point.range_after_breakout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ChanlunV2StrategyConfig{}
			if tt.mutateCfg != nil {
				tt.mutateCfg(&cfg)
			}
			engine, err := NewEngine(cfg)
			if err != nil {
				t.Fatalf("创建缠论V2引擎失败: %v", err)
			}
			base := int64(1710000000000)
			centerID := 3
			sig := Signal{SignalType: "buy3", Direction: "long", Price: 101, StopLoss: 97, TakeProfit: 116, Confidence: 90, CenterID: &centerID, Timestamp: base}
			klines := []market.Kline{
				v2TestKline(base-int64(45*time.Minute/time.Millisecond), 100.2, 100.8, 101.6, 99.0),
				v2TestKline(base+int64(15*time.Minute/time.Millisecond), 101.0, 100.9, 101.1, 100.8),
				v2TestKline(base+int64(30*time.Minute/time.Millisecond), 100.9, 101.2, 101.4, 100.8),
			}
			if tt.mutateData != nil {
				klines = tt.mutateData(klines)
			}
			ctx := chanlunV2ValidationContext(1000)
			ctx.MarketDataMap["BNBUSDT"] = chanlunV2EntryMarketData("BNBUSDT", klines)
			evaluation := engine.evaluateParentStructureEntry(ctx, "BNBUSDT", sig, chanlunV2QualityResult(base, centerID, "long"), map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"}, base+int64(30*time.Minute/time.Millisecond))
			if !evaluation.TriggerRejected || evaluation.ReasonCode != tt.wantReason {
				t.Fatalf("应按%s拒绝: %+v", tt.wantReason, evaluation)
			}
		})
	}
}

func TestThirdPointQualitySupportsShortAndMissingCenterFallback(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{
		EntryTiming: config.ChanlunV2EntryTimingConfig{
			ThirdPointQuality: config.ChanlunV2ThirdPointQualityConfig{MaxSupportGapATR: 2.0},
		},
	})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	base := int64(1710000000000)
	centerID := 4
	shortSig := Signal{SignalType: "sell3", Direction: "short", Price: 99, StopLoss: 103, TakeProfit: 84, Confidence: 90, CenterID: &centerID, Timestamp: base}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2EntryMarketData("BNBUSDT", []market.Kline{
		v2TestKline(base-int64(45*time.Minute/time.Millisecond), 99.8, 99.2, 101.0, 98.4),
		v2TestKline(base+int64(15*time.Minute/time.Millisecond), 99.0, 99.1, 99.2, 98.7),
		v2TestKline(base+int64(30*time.Minute/time.Millisecond), 99.1, 98.8, 99.1, 98.4),
	})
	evaluation := engine.evaluateParentStructureEntry(ctx, "BNBUSDT", shortSig, chanlunV2QualityResult(base, centerID, "short"), map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"}, base+int64(30*time.Minute/time.Millisecond))
	if evaluation.Decision.Action != "open_short" || metadataString(evaluation.Decision.StrategyMetadata, "third_point_quality_category") != "strong_third_sell" {
		t.Fatalf("强三卖应生成short trigger: %+v", evaluation)
	}

	noCenter := Signal{SignalType: "buy3", Direction: "long", Price: 100, StopLoss: 97, TakeProfit: 116, Confidence: 90, Timestamp: base}
	ctx.MarketDataMap["ETHUSDT"] = chanlunV2EntryMarketData("ETHUSDT", []market.Kline{
		v2TestKline(base-int64(45*time.Minute/time.Millisecond), 100.2, 100.8, 101.6, 99.0),
		v2TestKline(base+int64(15*time.Minute/time.Millisecond), 101.0, 100.9, 101.1, 100.8),
		v2TestKline(base+int64(30*time.Minute/time.Millisecond), 100.9, 101.2, 101.4, 100.8),
	})
	evaluation = engine.evaluateParentStructureEntry(ctx, "ETHUSDT", noCenter, nil, map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"}, base+int64(30*time.Minute/time.Millisecond))
	if evaluation.Decision.Action != "open_long" || metadataString(evaluation.Decision.StrategyMetadata, "third_point_quality_diagnostic") == "" {
		t.Fatalf("缺少center时应使用fallback诊断且仍可评估: %+v", evaluation)
	}
}

func TestValidateChanlunV2DecisionsRejectsZeroQuantitySizing(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["ZEROUSDT"] = &market.Data{Symbol: "ZEROUSDT", CurrentPrice: 100}
	d := decision.Decision{
		Symbol:          "ZEROUSDT",
		Action:          "open_long",
		Leverage:        5,
		StopLoss:        95,
		TakeProfit:      120,
		Confidence:      90,
		StrategyMode:    "chanlun_v2",
		StrategyName:    "chanlun_v2",
		StrategyVersion: "v0.1",
		ConfigHash:      engine.configHash,
		SignalID:        "zero-quantity",
		SignalType:      "buy2",
		SignalTimeframe: "15m",
		StrategyMetadata: map[string]any{
			"layer":                    "entry_trigger",
			"entry_trigger_close_time": int64(1710000000000),
			"trade_intent":             "open_long",
		},
	}
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, nil)
	if len(valid) != 0 || len(rejections) != 1 {
		t.Fatalf("zero quantity sizing应拒绝: valid=%+v rejections=%+v", valid, rejections)
	}
	if len(rejections[0].GateReasons) == 0 || rejections[0].GateReasons[0] != "position_sizing.zero_quantity" {
		t.Fatalf("应使用position_sizing.zero_quantity原因码: %+v", rejections[0])
	}
}

func TestChanlunV2SymbolFilterAndUniverseSkipsNonCryptoAndFilteredCandidates(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	for _, symbol := range []string{"CLUSDT", "XAUUSDT", "XAGUSDT"} {
		if isChanlunV2TradableCryptoSymbol(symbol) {
			t.Fatalf("%s 不应作为V2可开仓加密标的", symbol)
		}
	}
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} {
		if !isChanlunV2TradableCryptoSymbol(symbol) {
			t.Fatalf("%s 应作为V2可开仓加密标的", symbol)
		}
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.CandidateCoins = []decision.CandidateCoin{
		{Symbol: "CLUSDT", IncludedInPrompt: true},
		{Symbol: "XAUUSDT", IncludedInPrompt: true},
		{Symbol: "BTCUSDT", IncludedInPrompt: true},
		{Symbol: "DOGEUSDT", FilterReason: "cooldown", IncludedInPrompt: true},
		{Symbol: "SOLUSDT", IncludedInPrompt: true},
	}
	universe := engine.resolveSymbolUniverse(ctx)
	var symbols []string
	for _, item := range universe {
		symbols = append(symbols, item.Symbol)
	}
	got := strings.Join(symbols, ",")
	if got != "BTCUSDT,SOLUSDT" {
		t.Fatalf("V2候选过滤结果错误: %s", got)
	}
}

func TestBuildInputUsesStandardMACDHistogram(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	base := int64(1710000000000)
	klines := make([]market.Kline, 60)
	for i := range klines {
		close := 100 + float64(i)*0.7 + float64((i%5)*(i%5))*0.05
		klines[i] = v2TestKline(base+int64(i)*int64(time.Hour/time.Millisecond), close-0.2, close, close+0.5, close-0.8)
	}
	input := engine.buildInput(klines, "1h")
	if len(input.MACDHist) != len(klines) {
		t.Fatalf("macd_hist长度应与K线一致: %d vs %d", len(input.MACDHist), len(klines))
	}
	last := len(klines) - 1
	priceDiff := klines[last].Close - klines[last-1].Close
	if input.MACDHist[last] == 0 || input.MACDHist[last] == priceDiff {
		t.Fatalf("macd_hist应为标准MACD柱而不是收盘价差: hist=%.8f diff=%.8f", input.MACDHist[last], priceDiff)
	}
}

func TestApplyV2StopTakeProfitFallbackKeepsRustAndUsesATR(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	ctx.MarketDataMap["BNBUSDT"] = chanlunV2EntryMarketData("BNBUSDT", []market.Kline{
		v2TestKline(1710000000000, 99, 100, 101, 98),
		v2TestKline(1710000900000, 100, 100, 102, 99),
	})
	valid := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", 1710000000000, 1710000000000, 90, 95, 120)
	got := engine.applyV2StopTakeProfitFallback(ctx, valid, Signal{SignalType: "buy2", Direction: "long", StopLoss: 95, TakeProfit: 120}, nil, "15m")
	if got.StopLoss != 95 || got.TakeProfit != 120 || metadataString(got.StrategyMetadata, "sl_tp_source") != "rust" {
		t.Fatalf("Rust有效SL/TP应保持不变: %+v", got)
	}
	missing := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", 1710000000000, 1710000000000, 90, 0, 0)
	got = engine.applyV2StopTakeProfitFallback(ctx, missing, Signal{SignalType: "buy2", Direction: "long"}, nil, "15m")
	if got.StopLoss <= 0 || got.TakeProfit <= 0 || metadataString(got.StrategyMetadata, "sl_tp_source") != "atr" {
		t.Fatalf("缺失SL/TP应使用ATR兜底: %+v", got)
	}
	noDataCtx := chanlunV2ValidationContext(1000)
	delete(noDataCtx.MarketDataMap, "BNBUSDT")
	got = engine.applyV2StopTakeProfitFallback(noDataCtx, missing, Signal{SignalType: "buy2", Direction: "long"}, nil, "15m")
	if metadataString(got.StrategyMetadata, "sl_tp_source") != "invalid" {
		t.Fatalf("无行情/无ATR时应标记invalid: %+v", got)
	}
}

func TestValidateChanlunV2DecisionsRejectsInvalidStopTakeProfitFallback(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	d := chanlunV2DecisionFixture(engine, ctx, "BNBUSDT", "buy2", "open_long", 1710000000000, 1710000000000, 90, 0, 0)
	d.StrategyMetadata["sl_tp_source"] = "invalid"
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, nil)
	if len(valid) != 0 || len(rejections) != 1 || rejections[0].GateReasons[0] != "chanlun_v2.sl_tp_invalid" {
		t.Fatalf("invalid SL/TP应转为open_rejected: valid=%+v rejections=%+v", valid, rejections)
	}
}

func TestMultiLevelJudgmentSuppressesHigherTimeframeCountertrend(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	sig := Signal{SignalType: "buy2", Direction: "long", StopLoss: 95, TakeProfit: 120, Confidence: 90, Timestamp: 1710000000000}
	mr := &multiLevelResult{
		Symbol: "BNBUSDT",
		Results: map[string]*AnalysisResult{
			"higher": {Trend: "down_trend"},
			"trade":  {Signals: []Signal{sig}},
		},
		LastClosedByLevel: map[string]int64{"trade": 1710000000000},
	}
	signals, diagnostics := engine.multiLevelJudgment(ctx, mr, map[string]string{"trade": "1h"})
	if len(signals) != 0 || len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "countertrend") && !strings.Contains(diagnostics[0], "高级别趋势压制") {
		t.Fatalf("高级别下跌应压制多单: signals=%+v diagnostics=%+v", signals, diagnostics)
	}
	if !engine.hasTerminalSignal(ctx.TraderID, "BNBUSDT", v2SignalID("BNBUSDT", "1h", sig)) {
		t.Fatalf("逆势压制应写入V2 terminal状态")
	}

	shortSig := Signal{SignalType: "sell2", Direction: "short", StopLoss: 105, TakeProfit: 80, Confidence: 90, Timestamp: 1710003600000}
	mr.Results["higher"] = &AnalysisResult{Trend: "up_trend"}
	mr.Results["trade"] = &AnalysisResult{Signals: []Signal{shortSig}}
	signals, diagnostics = engine.multiLevelJudgment(ctx, mr, map[string]string{"trade": "1h"})
	if len(signals) != 0 || len(diagnostics) != 1 {
		t.Fatalf("高级别上涨应压制空单: signals=%+v diagnostics=%+v", signals, diagnostics)
	}

	mr.Results["higher"] = &AnalysisResult{Trend: "consolidation"}
	mr.Results["trade"] = &AnalysisResult{Signals: []Signal{{SignalType: "buy2", Direction: "long", StopLoss: 95, TakeProfit: 120, Confidence: 90, Timestamp: 1710007200000}}}
	signals, diagnostics = engine.multiLevelJudgment(ctx, mr, map[string]string{"trade": "1h"})
	if len(signals) != 1 || len(diagnostics) != 0 {
		t.Fatalf("盘整高级别不应压制: signals=%+v diagnostics=%+v", signals, diagnostics)
	}
}

func TestV2PositionManagementBreakevenPartialStructureAndDrawdown(t *testing.T) {
	disabled := false
	tests := []struct {
		name   string
		cfg    config.ChanlunV2StrategyConfig
		ctx    *decision.Context
		warmup func(*Engine, *decision.Context)
		want   string
	}{
		{
			name: "breakeven",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				PartialTakeProfitEnabled: &disabled,
				StructureBreakEnabled:    &disabled,
				FloatingDrawdownEnabled:  &disabled,
			}},
			ctx:  chanlunV2PositionContext("BNBUSDT", "long", 100, 106, 95, nil),
			want: "update_stop_loss",
		},
		{
			name: "partial take profit",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:        &disabled,
				StructureBreakEnabled:   &disabled,
				FloatingDrawdownEnabled: &disabled,
			}},
			ctx:  chanlunV2PositionContext("BNBUSDT", "long", 100, 108, 95, nil),
			want: "partial_close",
		},
		{
			name: "structure break",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				FloatingDrawdownEnabled:  &disabled,
			}},
			ctx: chanlunV2PositionContext("BNBUSDT", "long", 100, 94, 95, []market.Kline{
				v2TestKline(1710000000000, 96, 94.5, 96.2, 94.2),
				v2TestKline(1710000900000, 94.8, 94.2, 95.0, 94.0),
			}),
			want: "partial_close",
		},
		{
			name: "floating drawdown",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				StructureBreakEnabled:    &disabled,
				FloatingDrawdownPct:      30,
			}},
			ctx: chanlunV2PositionContext("BNBUSDT", "long", 100, 106, 95, nil),
			warmup: func(engine *Engine, ctx *decision.Context) {
				warm := chanlunV2PositionContext("BNBUSDT", "long", 100, 110, 95, nil)
				_ = engine.evaluateV2PositionManagement(warm, warm.Positions[0], map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"})
			},
			want: "partial_close",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewEngine(tt.cfg)
			if err != nil {
				t.Fatalf("创建缠论V2引擎失败: %v", err)
			}
			if tt.warmup != nil {
				tt.warmup(engine, tt.ctx)
			}
			d := engine.evaluateV2PositionManagement(tt.ctx, tt.ctx.Positions[0], map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"})
			if d.Action != tt.want {
				t.Fatalf("持仓管理动作错误: want=%s got=%+v", tt.want, d)
			}
			valid, rejections := engine.validateChanlunV2Decisions(tt.ctx, []decision.Decision{d}, nil)
			if len(valid) != 1 || len(rejections) != 0 {
				t.Fatalf("risk-reducing动作应通过专用验证: valid=%+v rejections=%+v", valid, rejections)
			}
		})
	}
}

func TestV2PositionManagementFullCloseRiskPaths(t *testing.T) {
	disabled := false
	enabled := true
	now := time.Now().UnixMilli()
	tests := []struct {
		name  string
		cfg   config.ChanlunV2StrategyConfig
		ctx   *decision.Context
		want  string
		rule  string
		setup func(*decision.Context)
	}{
		{
			name: "long atr hard stop",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				StructureBreakEnabled:    &disabled,
				FloatingDrawdownEnabled:  &disabled,
				HardStopATRMultiplier:    2,
			}},
			ctx:  chanlunV2PositionContext("BNBUSDT", "long", 100, 95, 90, nil),
			want: "close_long",
			rule: "atr_hard_stop",
			setup: func(ctx *decision.Context) {
				ctx.MarketDataMap["BNBUSDT"].MidTermSeries15m = &market.MidTermData15m{ATRValues: []float64{2}}
			},
		},
		{
			name: "short atr hard stop",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				StructureBreakEnabled:    &disabled,
				FloatingDrawdownEnabled:  &disabled,
				HardStopATRMultiplier:    2,
			}},
			ctx:  chanlunV2PositionContext("BNBUSDT", "short", 100, 105, 110, nil),
			want: "close_short",
			rule: "atr_hard_stop",
			setup: func(ctx *decision.Context) {
				ctx.MarketDataMap["BNBUSDT"].MidTermSeries15m = &market.MidTermData15m{ATRValues: []float64{2}}
			},
		},
		{
			name: "timeout unprofitable",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				StructureBreakEnabled:    &disabled,
				FloatingDrawdownEnabled:  &disabled,
				HardStopATREnabled:       &disabled,
				MaxHoldEnabled:           &enabled,
				MaxHoldCandles:           1,
				MaxHoldTimeframe:         "15m",
			}},
			ctx:  chanlunV2PositionContext("BNBUSDT", "long", 100, 99, 95, nil),
			want: "close_long",
			rule: "max_hold_unprofitable",
			setup: func(ctx *decision.Context) {
				ctx.Positions[0].UpdateTime = now - int64(45*time.Minute/time.Millisecond)
			},
		},
		{
			name: "structure break full close",
			cfg: config.ChanlunV2StrategyConfig{PositionManagement: config.ChanlunV2PositionManagementConfig{
				BreakevenEnabled:         &disabled,
				PartialTakeProfitEnabled: &disabled,
				FloatingDrawdownEnabled:  &disabled,
				HardStopATREnabled:       &disabled,
				FullCloseOnBreak:         &enabled,
			}},
			ctx: chanlunV2PositionContext("BNBUSDT", "long", 100, 94, 95, []market.Kline{
				v2TestKline(1710000000000, 96, 94.5, 96.2, 94.2),
				v2TestKline(1710000900000, 94.8, 94.2, 95.0, 94.0),
			}),
			want: "close_long",
			rule: "structure_break",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewEngine(tt.cfg)
			if err != nil {
				t.Fatalf("创建缠论V2引擎失败: %v", err)
			}
			if tt.setup != nil {
				tt.setup(tt.ctx)
			}
			d := engine.evaluateV2PositionManagement(tt.ctx, tt.ctx.Positions[0], map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"})
			if d.Action != tt.want || metadataString(d.StrategyMetadata, "rule") != tt.rule {
				t.Fatalf("V2 full close动作错误: want=%s/%s got=%+v", tt.want, tt.rule, d)
			}
			valid, rejections := engine.validateChanlunV2Decisions(tt.ctx, []decision.Decision{d}, nil)
			if len(valid) != 1 || len(rejections) != 0 {
				t.Fatalf("full close应通过risk-reducing验证: valid=%+v rejections=%+v", valid, rejections)
			}
		})
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

func v2TestKline(closeTime int64, open, close, high, low float64) market.Kline {
	return market.Kline{
		OpenTime:  closeTime - int64(15*time.Minute/time.Millisecond) + 1,
		CloseTime: closeTime,
		Open:      open,
		Close:     close,
		High:      high,
		Low:       low,
		Volume:    1000,
	}
}

func chanlunV2EntryMarketData(symbol string, klines []market.Kline) *market.Data {
	price := 0.0
	if len(klines) > 0 {
		price = klines[len(klines)-1].Close
	}
	data := chanlunV2ValidationMarketData(symbol, price)
	data.MidTermSeries15m = &market.MidTermData15m{ATRValues: []float64{2}}
	data.Klines = map[string][]market.Kline{
		"15m": klines,
		"1h":  klines,
		"3m":  klines,
	}
	return data
}

func chanlunV2QualityResult(base int64, centerID int, direction string) *multiLevelResult {
	center := Center{
		ID:        centerID,
		ZG:        100,
		ZD:        100,
		High:      103,
		Low:       97,
		StartTime: base - int64(2*time.Hour/time.Millisecond),
		EndTime:   base - int64(time.Hour/time.Millisecond),
	}
	if direction == "short" {
		center.ZG = 102
		center.ZD = 100
	}
	return &multiLevelResult{
		Symbol: "BNBUSDT",
		Results: map[string]*AnalysisResult{
			"trade": {
				Centers: []Center{center},
			},
		},
		LastClosedByLevel: map[string]int64{"trade": base},
	}
}

func chanlunV2PositionContext(symbol, side string, entry, current, stop float64, structureKlines []market.Kline) *decision.Context {
	ctx := chanlunV2ValidationContext(1000)
	ctx.Positions = []decision.PositionInfo{{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: entry,
		MarkPrice:  current,
		StopLoss:   stop,
		Quantity:   1,
		Leverage:   5,
	}}
	data := chanlunV2ValidationMarketData(symbol, current)
	if len(structureKlines) > 0 {
		data.Klines = map[string][]market.Kline{"15m": structureKlines, "1h": structureKlines, "3m": structureKlines}
	} else {
		now := int64(1710000000000)
		data.Klines = map[string][]market.Kline{"15m": []market.Kline{
			v2TestKline(now, current, current, current*1.01, current*0.99),
			v2TestKline(now+int64(15*time.Minute/time.Millisecond), current, current, current*1.01, current*0.99),
		}}
	}
	ctx.MarketDataMap[market.Normalize(symbol)] = data
	return ctx
}
