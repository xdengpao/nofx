package chanlunv2

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"nofx/strategy/chanlun"
	"strings"
	"time"
)

const (
	v2LayerParentStructure = "parent_structure"
	v2LayerEntryTrigger    = "entry_trigger"
)

type ThirdPointQualityMetrics struct {
	P0               float64 `json:"p0"`
	P1               float64 `json:"p1"`
	BreakoutExtreme  float64 `json:"breakout_extreme"`
	SupportGapPct    float64 `json:"support_gap_pct"`
	SupportGapATR    float64 `json:"support_gap_atr"`
	RetracementRatio float64 `json:"retracement_ratio"`
	PullbackCandles  int     `json:"pullback_candles"`
	QualityTimeframe string  `json:"quality_timeframe"`
	RemainingNetRR   float64 `json:"remaining_net_rr"`
}

type EntryTrigger struct {
	ID                    string
	Symbol                string
	Type                  string
	Timeframe             string
	CloseTime             int64
	ParentSignalID        string
	ParentSignalType      string
	ParentSignalCloseTime int64
	Direction             string
	Confidence            int
	EntryPrice            float64
	StopLoss              float64
	TakeProfit            float64
	QualityCategory       string
	QualityMetrics        *ThirdPointQualityMetrics
	Diagnostics           map[string]any
}

type entryTriggerRejection struct {
	ReasonCode  string
	Reason      string
	Diagnostics map[string]any
}

type parentEntryEvaluation struct {
	Decision        decision.Decision
	Diagnostics     []string
	ParentSeen      bool
	TriggerReady    bool
	TriggerRejected bool
	Terminal        bool
	ReasonCode      string
}

func stableV2EntryTriggerID(traderID, symbol, parentSignalID, triggerType, triggerTF string, triggerCloseTime int64, configHash string) string {
	value := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s",
		strings.TrimSpace(traderID), market.Normalize(symbol), strings.TrimSpace(parentSignalID),
		strings.TrimSpace(triggerType), strings.TrimSpace(triggerTF), triggerCloseTime, strings.TrimSpace(configHash))
	sum := sha1.Sum([]byte(value))
	return "chanlun_v2_entry:" + hex.EncodeToString(sum[:])[:20]
}

func (e *Engine) evaluateParentStructureEntry(ctx *decision.Context, symbol string, sig Signal, mr *multiLevelResult, timeframes map[string]string, decisionCloseTime int64) parentEntryEvaluation {
	action := signalActionHint(sig)
	if action == "" {
		return parentEntryEvaluation{}
	}
	timing := config.NormalizeChanlunV2EntryTiming(e.Config.EntryTiming)
	if timing.Enabled != nil && !*timing.Enabled {
		return parentEntryEvaluation{Decision: e.signalToDecision(ctx, symbol, sig, timeframes["trade"], decisionCloseTime), ParentSeen: true, TriggerReady: true}
	}
	if ctx == nil {
		return parentEntryEvaluation{ParentSeen: true, Diagnostics: []string{fmt.Sprintf("%s %s 缺少交易上下文，无法跟踪entry trigger", symbol, sig.SignalType)}}
	}
	tradeTF := firstNonEmptyString(timeframes["trade"], "1h")
	parentID := v2SignalID(symbol, tradeTF, sig)
	parentClose := normalizeV2EpochMillis(sig.Timestamp)
	if decisionCloseTime == 0 {
		decisionCloseTime = parentClose
	}
	if decisionCloseTime > 0 && parentClose > 0 && decisionCloseTime < parentClose {
		decisionCloseTime = parentClose
	}
	if e.isTerminalParentLifecycle(ctx.TraderID, symbol, parentID) {
		return parentEntryEvaluation{
			ParentSeen:  true,
			Terminal:    true,
			ReasonCode:  "lifecycle.terminal",
			Diagnostics: []string{fmt.Sprintf("%s %s 父结构已处于终态，跳过entry trigger", market.Normalize(symbol), sig.SignalType)},
		}
	}

	state := SignalLifecycleState{
		TraderID:              ctx.TraderID,
		Symbol:                market.Normalize(symbol),
		ParentSignalID:        parentID,
		ParentSignalType:      sig.SignalType,
		ParentSignalCloseTime: parentClose,
		Direction:             sig.Direction,
		Status:                "watching_entry",
		LastEvaluationTime:    decisionCloseTime,
		UpdatedAt:             time.Now().UnixMilli(),
	}
	currentPrice := currentPriceForV2Guard(ctx, symbol, tradeTF)
	terminal := func(status, reasonCode, reason string) parentEntryEvaluation {
		state.Status = status
		state.TerminalReasonCode = reasonCode
		e.upsertLifecycle(state)
		return parentEntryEvaluation{
			ParentSeen:  true,
			Terminal:    true,
			ReasonCode:  reasonCode,
			Diagnostics: []string{reason},
		}
	}
	if currentPrice > 0 && targetCrossed(action, currentPrice, sig.TakeProfit) {
		return terminal("terminal_target_crossed", "entry_parent.target_crossed",
			fmt.Sprintf("%s %s 父结构终止: 当前价%.6f已穿越目标%.6f", market.Normalize(symbol), sig.SignalType, currentPrice, sig.TakeProfit))
	}
	if currentPrice > 0 {
		if rr, ok := remainingNetRRForV2Decision(action, currentPrice, sig.StopLoss, sig.TakeProfit, v2TradingCostPct(ctx)); ok {
			state.RemainingNetRR = rr
			if rr < timing.EntryZone.MinRemainingNetRR {
				return terminal("terminal_rr_invalid", "entry_rr_invalid",
					fmt.Sprintf("%s %s 父结构终止: 剩余净RR %.2f低于阈值%.2f", market.Normalize(symbol), sig.SignalType, rr, timing.EntryZone.MinRemainingNetRR))
			}
		}
	}
	if invalidStopTakeProfit(action, currentPrice, sig.StopLoss, sig.TakeProfit) {
		return terminal("terminal_invalidated", "entry_parent.invalid_structure",
			fmt.Sprintf("%s %s 父结构终止: 止损/止盈结构无效", market.Normalize(symbol), sig.SignalType))
	}

	watchAge := signalAgeCandles(parentClose, decisionCloseTime, timing.WatchTimeframe)
	if timing.WatchMaxCandles > 0 && watchAge > timing.WatchMaxCandles {
		return terminal("terminal_expired", "entry_parent.watch_window_expired",
			fmt.Sprintf("%s %s 父结构观察窗口过期: 年龄%d根%s超过%d根", market.Normalize(symbol), sig.SignalType, watchAge, timing.WatchTimeframe, timing.WatchMaxCandles))
	}

	tradeAge := signalAgeCandles(parentClose, decisionCloseTime, tradeTF)
	if timing.DirectStructureOpen && tradeAge <= timing.DirectOpenMaxAgeCandles {
		d := e.signalToDecision(ctx, symbol, sig, tradeTF, decisionCloseTime)
		if d.StrategyMetadata == nil {
			d.StrategyMetadata = map[string]any{}
		}
		d.StrategyMetadata["layer"] = "direct_structure"
		d.StrategyMetadata["parent_signal_id"] = parentID
		d.StrategyMetadata["parent_signal_close_time"] = parentClose
		d.StrategyMetadata["reason_code"] = "chanlun_v2_direct_structure"
		state.Status = "entry_trigger_ready"
		e.upsertLifecycle(state)
		return parentEntryEvaluation{Decision: d, ParentSeen: true, TriggerReady: true, Diagnostics: []string{fmt.Sprintf("%s %s direct_structure fresh", market.Normalize(symbol), sig.SignalType)}}
	}

	data := marketDataForV2(ctx, symbol)
	trigger, rejection := e.detectV2EntryTrigger(ctx, symbol, sig, mr, data, timeframes, decisionCloseTime)
	if rejection.ReasonCode != "" {
		state.Status = "watching_entry"
		e.upsertLifecycle(state)
		return parentEntryEvaluation{
			ParentSeen:      true,
			TriggerRejected: true,
			ReasonCode:      rejection.ReasonCode,
			Diagnostics:     []string{rejection.Reason},
		}
	}
	if trigger == nil {
		state.Status = "watching_entry"
		e.upsertLifecycle(state)
		return parentEntryEvaluation{
			ParentSeen:  true,
			ReasonCode:  "waiting_for_fresh_entry_trigger",
			Diagnostics: []string{fmt.Sprintf("%s %s 作为父结构背景保留，等待%s fresh entry trigger", market.Normalize(symbol), sig.SignalType, timing.TriggerTimeframe)},
		}
	}
	state.Status = "entry_trigger_ready"
	state.EntryTriggerID = trigger.ID
	state.EntryTriggerType = trigger.Type
	state.EntryTriggerCloseTime = trigger.CloseTime
	if trigger.QualityMetrics != nil {
		state.RemainingNetRR = trigger.QualityMetrics.RemainingNetRR
	}
	e.upsertLifecycle(state)
	e.appendEntryTriggerMarker(ctx.TraderID, *trigger, "ready", fmt.Sprintf("%s %s %s ready", market.Normalize(symbol), sig.SignalType, trigger.Type))
	return parentEntryEvaluation{
		Decision:     e.entryTriggerToDecision(ctx, symbol, sig, *trigger),
		ParentSeen:   true,
		TriggerReady: true,
		Diagnostics:  []string{fmt.Sprintf("%s %s fresh entry trigger ready: %s", market.Normalize(symbol), sig.SignalType, trigger.ID)},
	}
}

func (e *Engine) detectV2EntryTrigger(ctx *decision.Context, symbol string, sig Signal, mr *multiLevelResult, data *market.Data, timeframes map[string]string, decisionCloseTime int64) (*EntryTrigger, entryTriggerRejection) {
	if ctx == nil || data == nil {
		return nil, entryTriggerRejection{}
	}
	timing := config.NormalizeChanlunV2EntryTiming(e.Config.EntryTiming)
	if timing.MinTriggerConfidence > 0 && sig.Confidence < timing.MinTriggerConfidence {
		return nil, entryTriggerRejection{
			ReasonCode: "entry_trigger_low_confidence",
			Reason:     fmt.Sprintf("%s %s entry trigger等待: 置信度%d低于%d", market.Normalize(symbol), sig.SignalType, sig.Confidence, timing.MinTriggerConfidence),
		}
	}
	if v2TriggerTypeAllowed(timing, "pullback_retest_resume") {
		if trigger, rejection := e.detectPullbackRetestResumeTrigger(ctx, symbol, sig, mr, data, timeframes, decisionCloseTime, timing); trigger != nil || rejection.ReasonCode != "" {
			return trigger, rejection
		}
	}
	if v2TriggerTypeAllowed(timing, "breakout_continuation") {
		if trigger, rejection := e.detectContinuationTrigger(ctx, symbol, sig, data, timeframes, decisionCloseTime, timing); trigger != nil || rejection.ReasonCode != "" {
			return trigger, rejection
		}
	}
	if v2TriggerTypeAllowed(timing, "micro_reversal_confirm") {
		if trigger, rejection := e.detectMicroReversalTrigger(ctx, symbol, sig, data, timeframes, decisionCloseTime, timing); trigger != nil || rejection.ReasonCode != "" {
			return trigger, rejection
		}
	}
	return nil, entryTriggerRejection{}
}

func (e *Engine) detectPullbackRetestResumeTrigger(ctx *decision.Context, symbol string, sig Signal, mr *multiLevelResult, data *market.Data, timeframes map[string]string, decisionCloseTime int64, timing config.ChanlunV2EntryTimingConfig) (*EntryTrigger, entryTriggerRejection) {
	triggerTF := firstNonEmptyString(timing.TriggerTimeframe, timeframes["sub"], "15m")
	parentClose := normalizeV2EpochMillis(sig.Timestamp)
	components := klinesAfter(data.Klines[triggerTF], parentClose)
	if len(components) < 2 {
		return nil, entryTriggerRejection{}
	}
	prev := normalizeMarketKlineTime(components[len(components)-2])
	last := normalizeMarketKlineTime(components[len(components)-1])
	pattern, ok := detectV2PullbackRetestResume(sig, prev, last, timing.EntryZone.MaxChaseRatio)
	if !ok {
		return nil, entryTriggerRejection{}
	}
	if rejection := e.validateTriggerAge(symbol, sig, triggerTF, last.CloseTime, data, decisionCloseTime, timing); rejection.ReasonCode != "" {
		return nil, rejection
	}
	metrics, category, qualityDiagnostics, qualityRejection := e.evaluateThirdPointQuality(ctx, symbol, sig, mr, data, timeframes, timing, prev, last)
	if qualityRejection.ReasonCode != "" {
		return nil, qualityRejection
	}
	diagnostics := map[string]any{
		"pattern": "pullback_retest_resume",
	}
	for key, value := range pattern {
		diagnostics[key] = value
	}
	for key, value := range qualityDiagnostics {
		diagnostics[key] = value
	}
	parentID := v2SignalID(symbol, firstNonEmptyString(timeframes["trade"], "1h"), sig)
	return &EntryTrigger{
		ID:                    stableV2EntryTriggerID(ctx.TraderID, symbol, parentID, "pullback_retest_resume", triggerTF, last.CloseTime, e.configHash),
		Symbol:                market.Normalize(symbol),
		Type:                  "pullback_retest_resume",
		Timeframe:             triggerTF,
		CloseTime:             last.CloseTime,
		ParentSignalID:        parentID,
		ParentSignalType:      sig.SignalType,
		ParentSignalCloseTime: parentClose,
		Direction:             sig.Direction,
		Confidence:            sig.Confidence,
		EntryPrice:            last.Close,
		StopLoss:              sig.StopLoss,
		TakeProfit:            sig.TakeProfit,
		QualityCategory:       category,
		QualityMetrics:        metrics,
		Diagnostics:           diagnostics,
	}, entryTriggerRejection{}
}

func (e *Engine) detectContinuationTrigger(ctx *decision.Context, symbol string, sig Signal, data *market.Data, timeframes map[string]string, decisionCloseTime int64, timing config.ChanlunV2EntryTimingConfig) (*EntryTrigger, entryTriggerRejection) {
	triggerTF := firstNonEmptyString(timing.TriggerTimeframe, timeframes["sub"], "15m")
	parentClose := normalizeV2EpochMillis(sig.Timestamp)
	components := klinesAfter(data.Klines[triggerTF], parentClose)
	if len(components) < 2 {
		return nil, entryTriggerRejection{}
	}
	prev := normalizeMarketKlineTime(components[len(components)-2])
	last := normalizeMarketKlineTime(components[len(components)-1])
	direction := strings.ToLower(strings.TrimSpace(sig.Direction))
	continued := false
	if direction == "long" {
		continued = last.Close > last.Open && last.Close > prev.High
	} else if direction == "short" {
		continued = last.Close < last.Open && last.Close < prev.Low
	}
	if !continued {
		return nil, entryTriggerRejection{}
	}
	if rejection := e.validateTriggerAge(symbol, sig, triggerTF, last.CloseTime, data, decisionCloseTime, timing); rejection.ReasonCode != "" {
		return nil, rejection
	}
	if rejection := validateEntryZoneAndRR(symbol, signalActionHint(sig), last.Close, sig.StopLoss, sig.TakeProfit, sig.Price, timing, v2TradingCostPct(ctx)); rejection.ReasonCode != "" {
		return nil, rejection
	}
	parentID := v2SignalID(symbol, firstNonEmptyString(timeframes["trade"], "1h"), sig)
	return &EntryTrigger{
		ID:                    stableV2EntryTriggerID(ctx.TraderID, symbol, parentID, "breakout_continuation", triggerTF, last.CloseTime, e.configHash),
		Symbol:                market.Normalize(symbol),
		Type:                  "breakout_continuation",
		Timeframe:             triggerTF,
		CloseTime:             last.CloseTime,
		ParentSignalID:        parentID,
		ParentSignalType:      sig.SignalType,
		ParentSignalCloseTime: parentClose,
		Direction:             sig.Direction,
		Confidence:            max(0, sig.Confidence-5),
		EntryPrice:            last.Close,
		StopLoss:              sig.StopLoss,
		TakeProfit:            sig.TakeProfit,
		QualityCategory:       "breakout_continuation",
		Diagnostics: map[string]any{
			"breakout_reference": prev.High,
		},
	}, entryTriggerRejection{}
}

func (e *Engine) detectMicroReversalTrigger(ctx *decision.Context, symbol string, sig Signal, data *market.Data, timeframes map[string]string, decisionCloseTime int64, timing config.ChanlunV2EntryTimingConfig) (*EntryTrigger, entryTriggerRejection) {
	triggerTF := firstNonEmptyString(timeframes["micro"], "3m")
	parentClose := normalizeV2EpochMillis(sig.Timestamp)
	components := klinesAfter(data.Klines[triggerTF], parentClose)
	if len(components) < 3 {
		return nil, entryTriggerRejection{}
	}
	prev := normalizeMarketKlineTime(components[len(components)-2])
	last := normalizeMarketKlineTime(components[len(components)-1])
	direction := strings.ToLower(strings.TrimSpace(sig.Direction))
	reversed := false
	if direction == "long" {
		reversed = prev.Close < prev.Open && last.Close > last.Open && last.Close > prev.High
	} else if direction == "short" {
		reversed = prev.Close > prev.Open && last.Close < last.Open && last.Close < prev.Low
	}
	if !reversed {
		return nil, entryTriggerRejection{}
	}
	if rejection := e.validateTriggerAge(symbol, sig, triggerTF, last.CloseTime, data, decisionCloseTime, timing); rejection.ReasonCode != "" {
		return nil, rejection
	}
	if rejection := validateEntryZoneAndRR(symbol, signalActionHint(sig), last.Close, sig.StopLoss, sig.TakeProfit, sig.Price, timing, v2TradingCostPct(ctx)); rejection.ReasonCode != "" {
		return nil, rejection
	}
	parentID := v2SignalID(symbol, firstNonEmptyString(timeframes["trade"], "1h"), sig)
	return &EntryTrigger{
		ID:                    stableV2EntryTriggerID(ctx.TraderID, symbol, parentID, "micro_reversal_confirm", triggerTF, last.CloseTime, e.configHash),
		Symbol:                market.Normalize(symbol),
		Type:                  "micro_reversal_confirm",
		Timeframe:             triggerTF,
		CloseTime:             last.CloseTime,
		ParentSignalID:        parentID,
		ParentSignalType:      sig.SignalType,
		ParentSignalCloseTime: parentClose,
		Direction:             sig.Direction,
		Confidence:            max(0, sig.Confidence-8),
		EntryPrice:            last.Close,
		StopLoss:              sig.StopLoss,
		TakeProfit:            sig.TakeProfit,
		QualityCategory:       "micro_reversal_confirm",
		Diagnostics: map[string]any{
			"micro_reversal_reference": prev.Close,
		},
	}, entryTriggerRejection{}
}

func (e *Engine) validateTriggerAge(symbol string, sig Signal, triggerTF string, triggerClose int64, data *market.Data, decisionCloseTime int64, timing config.ChanlunV2EntryTimingConfig) entryTriggerRejection {
	evaluationClose := latestKlineCloseMillis(data.Klines[triggerTF])
	if evaluationClose == 0 {
		evaluationClose = decisionCloseTime
	}
	if evaluationClose > 0 && triggerClose > 0 && evaluationClose < triggerClose {
		evaluationClose = triggerClose
	}
	age := signalAgeCandles(triggerClose, evaluationClose, triggerTF)
	if age > timing.MaxTriggerAgeCandles {
		return entryTriggerRejection{
			ReasonCode: "entry_trigger_expired",
			Reason:     fmt.Sprintf("%s %s entry trigger过期: 年龄%d根%s超过%d根", market.Normalize(symbol), sig.SignalType, age, triggerTF, timing.MaxTriggerAgeCandles),
			Diagnostics: map[string]any{
				"age_candles":             age,
				"max_trigger_age_candles": timing.MaxTriggerAgeCandles,
				"trigger_close_time":      triggerClose,
				"evaluation_close_time":   evaluationClose,
			},
		}
	}
	return entryTriggerRejection{}
}

func detectV2PullbackRetestResume(sig Signal, prev, last market.Kline, maxChaseRatio float64) (map[string]any, bool) {
	if maxChaseRatio <= 0 {
		maxChaseRatio = 0.35
	}
	width := math.Abs(sig.TakeProfit - sig.StopLoss)
	if width <= 0 || sig.Price <= 0 {
		return nil, false
	}
	switch strings.ToLower(strings.TrimSpace(sig.Direction)) {
	case "long":
		zoneUpper := sig.Price + width*maxChaseRatio
		retested := prev.Low <= zoneUpper && prev.Close <= prev.Open
		resumed := last.Close > last.Open && last.Close > prev.Close
		if !retested || !resumed {
			return nil, false
		}
		return map[string]any{
			"pullback_retest_level": prev.Low,
			"pullback_resume_close": last.Close,
			"entry_zone_upper":      zoneUpper,
		}, true
	case "short":
		zoneLower := sig.Price - width*maxChaseRatio
		retested := prev.High >= zoneLower && prev.Close >= prev.Open
		resumed := last.Close < last.Open && last.Close < prev.Close
		if !retested || !resumed {
			return nil, false
		}
		return map[string]any{
			"pullback_retest_level": prev.High,
			"pullback_resume_close": last.Close,
			"entry_zone_lower":      zoneLower,
		}, true
	default:
		return nil, false
	}
}

func (e *Engine) evaluateThirdPointQuality(ctx *decision.Context, symbol string, sig Signal, mr *multiLevelResult, data *market.Data, timeframes map[string]string, timing config.ChanlunV2EntryTimingConfig, pullbackKline, triggerKline market.Kline) (*ThirdPointQualityMetrics, string, map[string]any, entryTriggerRejection) {
	qualityCfg := config.NormalizeChanlunV2ThirdPointQuality(timing.ThirdPointQuality)
	if qualityCfg.Enabled != nil && !*qualityCfg.Enabled {
		return nil, "", map[string]any{"third_point_quality": "disabled"}, entryTriggerRejection{}
	}
	action := signalActionHint(sig)
	qualityTF := resolveThirdPointQualityTimeframe(qualityCfg.QualityTimeframe, timing, timeframes)
	klines := normalizeMarketKlines(data.Klines[qualityTF])
	if len(klines) == 0 {
		return nil, "", map[string]any{"third_point_quality_missing": "missing_klines"}, entryTriggerRejection{}
	}
	p0, centerEnd, centerDiagnostic := resolveThirdPointBoundary(sig, mr)
	if p0 <= 0 {
		return nil, "", map[string]any{"third_point_quality_missing": "missing_p0"}, entryTriggerRejection{}
	}
	triggerClose := normalizeV2EpochMillis(triggerKline.CloseTime)
	breakoutIdx := findBreakoutKlineIndex(klines, p0, sig.Direction, centerEnd, triggerClose)
	if breakoutIdx < 0 {
		return nil, "", map[string]any{"third_point_quality_missing": "missing_breakout"}, entryTriggerRejection{}
	}
	pullbackClose := normalizeV2EpochMillis(pullbackKline.CloseTime)
	p1Idx := findKlineIndexByCloseTime(klines, pullbackClose)
	if p1Idx < breakoutIdx {
		p1Idx = breakoutIdx
	}
	if p1Idx >= len(klines) {
		p1Idx = len(klines) - 1
	}
	p1 := pullbackKline.Low
	breakoutExtreme := klines[breakoutIdx].High
	if strings.EqualFold(sig.Direction, "short") {
		p1 = pullbackKline.High
		breakoutExtreme = klines[breakoutIdx].Low
	}
	for i := breakoutIdx; i <= p1Idx && i < len(klines); i++ {
		k := klines[i]
		if strings.EqualFold(sig.Direction, "long") {
			if i > breakoutIdx && k.Low < p1 {
				p1 = k.Low
			}
			if k.High > breakoutExtreme {
				breakoutExtreme = k.High
			}
		} else {
			if i > breakoutIdx && k.High > p1 {
				p1 = k.High
			}
			if k.Low < breakoutExtreme {
				breakoutExtreme = k.Low
			}
		}
	}
	supportGapPct := 0.0
	retracementRatio := 1.0
	if strings.EqualFold(sig.Direction, "long") {
		supportGapPct = (p1 - p0) / p0 * 100
		if breakoutExtreme > p0 {
			retracementRatio = (breakoutExtreme - p1) / (breakoutExtreme - p0)
		}
	} else {
		supportGapPct = (p0 - p1) / p0 * 100
		if p0 > breakoutExtreme {
			retracementRatio = (p1 - breakoutExtreme) / (p0 - breakoutExtreme)
		}
	}
	if retracementRatio < 0 {
		retracementRatio = 0
	}
	atr := market.GetATR(data, qualityTF)
	if atr <= 0 {
		atr = calculateSimpleATR(klines, 14)
	}
	supportGapATR := 0.0
	if atr > 0 {
		supportGapATR = math.Abs(p1-p0) / atr
	}
	remainingRR := 0.0
	if rr, ok := remainingNetRRForV2Decision(action, triggerKline.Close, sig.StopLoss, sig.TakeProfit, v2TradingCostPct(ctx)); ok {
		remainingRR = rr
	}
	metrics := &ThirdPointQualityMetrics{
		P0:               p0,
		P1:               p1,
		BreakoutExtreme:  breakoutExtreme,
		SupportGapPct:    supportGapPct,
		SupportGapATR:    supportGapATR,
		RetracementRatio: retracementRatio,
		PullbackCandles:  max(1, p1Idx-breakoutIdx+1),
		QualityTimeframe: qualityTF,
		RemainingNetRR:   remainingRR,
	}
	diagnostics := map[string]any{
		"third_point_quality_category": "",
	}
	if centerDiagnostic != "" {
		diagnostics["third_point_quality_diagnostic"] = centerDiagnostic
	}
	if supportGapPct <= 0 {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "third_point.reentered_center",
			Reason:      fmt.Sprintf("%s %s 三买/三卖质量失效: 回踩重新进入中枢边界 G=%.4f%%", market.Normalize(symbol), sig.SignalType, supportGapPct),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	useATR := qualityCfg.UseATRNormalization == nil || *qualityCfg.UseATRNormalization
	if useATR && atr > 0 && supportGapATR > qualityCfg.MaxSupportGapATR {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "entry_zone_chased",
			Reason:      fmt.Sprintf("%s %s entry zone拒绝: G_ATR %.2f超过%.2f", market.Normalize(symbol), sig.SignalType, supportGapATR, qualityCfg.MaxSupportGapATR),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	if (!useATR || atr <= 0) && qualityCfg.MaxSupportGapPct > 0 && supportGapPct > qualityCfg.MaxSupportGapPct {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "entry_zone_chased",
			Reason:      fmt.Sprintf("%s %s entry zone拒绝: G %.2f%%超过%.2f%%", market.Normalize(symbol), sig.SignalType, supportGapPct, qualityCfg.MaxSupportGapPct),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	if retracementRatio > qualityCfg.MaxRetracementRatio {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "third_point.deep_retracement",
			Reason:      fmt.Sprintf("%s %s 三买/三卖回撤过深: R %.2f超过%.2f", market.Normalize(symbol), sig.SignalType, retracementRatio, qualityCfg.MaxRetracementRatio),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	if metrics.PullbackCandles > qualityCfg.MaxPullbackCandles {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "third_point.range_after_breakout",
			Reason:      fmt.Sprintf("%s %s 突破后回调耗时过长: N=%d超过%d", market.Normalize(symbol), sig.SignalType, metrics.PullbackCandles, qualityCfg.MaxPullbackCandles),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	if remainingRR > 0 && remainingRR < timing.EntryZone.MinRemainingNetRR {
		return metrics, "", diagnostics, entryTriggerRejection{
			ReasonCode:  "entry_rr_invalid",
			Reason:      fmt.Sprintf("%s %s entry trigger剩余净RR %.2f低于阈值%.2f", market.Normalize(symbol), sig.SignalType, remainingRR, timing.EntryZone.MinRemainingNetRR),
			Diagnostics: thirdPointDiagnostics(metrics, diagnostics),
		}
	}
	category := "strong_third_buy"
	if strings.EqualFold(sig.Direction, "short") {
		category = "strong_third_sell"
	}
	diagnostics["third_point_quality_category"] = category
	return metrics, category, diagnostics, entryTriggerRejection{}
}

func thirdPointDiagnostics(metrics *ThirdPointQualityMetrics, base map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	if metrics == nil {
		return out
	}
	out["p0"] = metrics.P0
	out["p1"] = metrics.P1
	out["breakout_extreme"] = metrics.BreakoutExtreme
	out["support_gap_pct"] = metrics.SupportGapPct
	out["support_gap_atr"] = metrics.SupportGapATR
	out["retracement_ratio"] = metrics.RetracementRatio
	out["pullback_candles"] = metrics.PullbackCandles
	out["quality_timeframe"] = metrics.QualityTimeframe
	out["remaining_net_rr"] = metrics.RemainingNetRR
	return out
}

func resolveThirdPointQualityTimeframe(value string, timing config.ChanlunV2EntryTimingConfig, timeframes map[string]string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "trade":
		return firstNonEmptyString(timeframes["trade"], "1h")
	case "trigger":
		return firstNonEmptyString(timing.TriggerTimeframe, timeframes["sub"], "15m")
	case "watch", "":
		return firstNonEmptyString(timing.WatchTimeframe, timeframes["sub"], "15m")
	default:
		return value
	}
}

func resolveThirdPointBoundary(sig Signal, mr *multiLevelResult) (float64, int64, string) {
	if sig.CenterID != nil && mr != nil {
		if result := mr.Results["trade"]; result != nil {
			for _, center := range result.Centers {
				if center.ID != *sig.CenterID {
					continue
				}
				if strings.EqualFold(sig.Direction, "short") {
					return center.ZD, normalizeV2EpochMillis(center.EndTime), ""
				}
				return center.ZG, normalizeV2EpochMillis(center.EndTime), ""
			}
		}
		return sig.Price, 0, "center_id_missing_fallback_to_signal_price"
	}
	return sig.Price, 0, "missing_center_fallback_to_signal_price"
}

func findBreakoutKlineIndex(klines []market.Kline, p0 float64, direction string, centerEnd, triggerClose int64) int {
	for i, k := range klines {
		closeTime := normalizeV2EpochMillis(k.CloseTime)
		if centerEnd > 0 && closeTime <= centerEnd {
			continue
		}
		if triggerClose > 0 && closeTime > triggerClose {
			break
		}
		if strings.EqualFold(direction, "short") {
			if k.Close < p0 {
				return i
			}
			continue
		}
		if k.Close > p0 {
			return i
		}
	}
	return -1
}

func findKlineIndexByCloseTime(klines []market.Kline, closeTime int64) int {
	if closeTime <= 0 {
		return len(klines) - 1
	}
	for i, k := range klines {
		if normalizeV2EpochMillis(k.CloseTime) == closeTime {
			return i
		}
	}
	return len(klines) - 1
}

func calculateSimpleATR(klines []market.Kline, period int) float64 {
	if len(klines) < 2 {
		return 0
	}
	if period <= 0 || period > len(klines)-1 {
		period = len(klines) - 1
	}
	start := len(klines) - period
	total := 0.0
	count := 0
	for i := start; i < len(klines); i++ {
		prevClose := klines[i-1].Close
		tr := math.Max(klines[i].High-klines[i].Low, math.Max(math.Abs(klines[i].High-prevClose), math.Abs(klines[i].Low-prevClose)))
		total += tr
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func validateEntryZoneAndRR(symbol, action string, entryPrice, stopLoss, takeProfit, structurePrice float64, timing config.ChanlunV2EntryTimingConfig, costPct float64) entryTriggerRejection {
	if entryPrice <= 0 || stopLoss <= 0 || takeProfit <= 0 {
		return entryTriggerRejection{ReasonCode: "entry_rr_invalid", Reason: fmt.Sprintf("%s entry trigger价格结构无效", market.Normalize(symbol))}
	}
	if rr, ok := remainingNetRRForV2Decision(action, entryPrice, stopLoss, takeProfit, costPct); ok && rr < timing.EntryZone.MinRemainingNetRR {
		return entryTriggerRejection{ReasonCode: "entry_rr_invalid", Reason: fmt.Sprintf("%s entry trigger剩余净RR %.2f低于阈值%.2f", market.Normalize(symbol), rr, timing.EntryZone.MinRemainingNetRR)}
	}
	width := math.Abs(takeProfit - stopLoss)
	if width > 0 && structurePrice > 0 {
		chase := 0.0
		switch decision.DecisionDirection(action) {
		case "long":
			chase = entryPrice - structurePrice
		case "short":
			chase = structurePrice - entryPrice
		}
		if chase > width*timing.EntryZone.MaxChaseRatio {
			return entryTriggerRejection{ReasonCode: "entry_zone_chased", Reason: fmt.Sprintf("%s entry trigger追价距离%.6f超过结构风险预算", market.Normalize(symbol), chase)}
		}
	}
	return entryTriggerRejection{}
}

func (e *Engine) entryTriggerToDecision(ctx *decision.Context, symbol string, sig Signal, trigger EntryTrigger) decision.Decision {
	action := "open_long"
	if strings.EqualFold(trigger.Direction, "short") {
		action = "open_short"
	}
	leverage := 0
	if ctx != nil {
		leverage = ctx.AltcoinLeverage
		if market.Normalize(symbol) == "BTCUSDT" || market.Normalize(symbol) == "ETHUSDT" {
			leverage = ctx.BTCETHLeverage
		}
	}
	metadata := map[string]any{
		"layer":                                v2LayerEntryTrigger,
		"timeframe":                            trigger.Timeframe,
		"trade_intent":                         action,
		"signal_close_time":                    trigger.CloseTime,
		"trigger_close_time":                   trigger.CloseTime,
		"entry_trigger_close_time":             trigger.CloseTime,
		"entry_trigger_timeframe":              trigger.Timeframe,
		"decision_close_time":                  trigger.CloseTime,
		"evaluation_close_time":                trigger.CloseTime,
		"parent_signal_id":                     trigger.ParentSignalID,
		"parent_signal_type":                   trigger.ParentSignalType,
		"parent_signal_close_time":             trigger.ParentSignalCloseTime,
		"entry_trigger_id":                     trigger.ID,
		"entry_trigger_type":                   trigger.Type,
		"structure_to_trigger_latency_candles": signalAgeCandles(trigger.ParentSignalCloseTime, trigger.CloseTime, trigger.Timeframe),
		"divergence_strength":                  sig.DivergenceStrength,
		"center_id":                            signalCenterID(sig),
		"reason_code":                          "entry_trigger." + trigger.Type,
	}
	for key, value := range trigger.Diagnostics {
		metadata[key] = value
	}
	if trigger.QualityMetrics != nil {
		copyThirdPointMetricsToMetadata(metadata, trigger.QualityCategory, trigger.QualityMetrics)
	}
	return decision.Decision{
		Symbol:           market.Normalize(symbol),
		Action:           action,
		Leverage:         leverage,
		StopLoss:         trigger.StopLoss,
		TakeProfit:       trigger.TakeProfit,
		Confidence:       trigger.Confidence,
		Reasoning:        fmt.Sprintf("缠论V2 %s 基于父结构%s触发，置信度%d", trigger.Type, sig.SignalType, trigger.Confidence),
		StrategyMode:     "chanlun_v2",
		StrategyName:     "chanlun_v2",
		StrategyVersion:  "v0.1",
		ConfigHash:       e.configHash,
		SignalID:         trigger.ID,
		SignalType:       sig.SignalType,
		SignalTimeframe:  trigger.Timeframe,
		StructureTarget:  trigger.TakeProfit,
		StrategyMetadata: metadata,
	}
}

func copyThirdPointMetricsToMetadata(metadata map[string]any, category string, metrics *ThirdPointQualityMetrics) {
	if metadata == nil || metrics == nil {
		return
	}
	metadata["third_point_quality_category"] = category
	metadata["p0"] = metrics.P0
	metadata["p1"] = metrics.P1
	metadata["breakout_extreme"] = metrics.BreakoutExtreme
	metadata["support_gap_pct"] = metrics.SupportGapPct
	metadata["support_gap_atr"] = metrics.SupportGapATR
	metadata["retracement_ratio"] = metrics.RetracementRatio
	metadata["pullback_candles"] = metrics.PullbackCandles
	metadata["quality_timeframe"] = metrics.QualityTimeframe
	metadata["remaining_net_rr"] = metrics.RemainingNetRR
}

func (e *Engine) appendEntryTriggerMarker(traderID string, trigger EntryTrigger, status, reason string) {
	if trigger.ID == "" {
		return
	}
	marker := chanlun.SignalMarker{
		Symbol:                           market.Normalize(trigger.Symbol),
		Timeframe:                        trigger.Timeframe,
		CloseTime:                        trigger.CloseTime,
		SignalCloseTime:                  trigger.CloseTime,
		DecisionCloseTime:                trigger.CloseTime,
		DisplayCloseTime:                 trigger.CloseTime,
		EvaluationCloseTime:              trigger.CloseTime,
		SignalType:                       trigger.ParentSignalType,
		Direction:                        trigger.Direction,
		Level:                            "entry",
		SourceLayer:                      v2LayerEntryTrigger,
		Status:                           status,
		SignalID:                         trigger.ID,
		LifecycleKey:                     "entry_trigger:" + trigger.ID,
		ReasonCode:                       "entry_trigger." + trigger.Type,
		DisplayCategory:                  "entry_trigger",
		DisplayPriority:                  95,
		Action:                           signalActionForDirection(trigger.Direction),
		FinalAction:                      signalActionForDirection(trigger.Direction),
		TradeIntent:                      signalActionForDirection(trigger.Direction),
		Price:                            trigger.EntryPrice,
		Reason:                           reason,
		ParentSignalID:                   trigger.ParentSignalID,
		EntryTriggerID:                   trigger.ID,
		EntryTriggerType:                 trigger.Type,
		EntryTriggerTF:                   trigger.Timeframe,
		EntryTriggerClose:                trigger.CloseTime,
		StructureToTriggerLatencyCandles: signalAgeCandles(trigger.ParentSignalCloseTime, trigger.CloseTime, trigger.Timeframe),
		EntryWindowState:                 "entry_trigger_ready",
		EntryReference:                   trigger.EntryPrice,
		LastUpdatedAt:                    time.Now().UnixMilli(),
	}
	if trigger.QualityMetrics != nil {
		marker.ThirdPointQualityCategory = trigger.QualityCategory
		marker.SupportGapPct = trigger.QualityMetrics.SupportGapPct
		marker.SupportGapATR = trigger.QualityMetrics.SupportGapATR
		marker.RetracementRatio = trigger.QualityMetrics.RetracementRatio
		marker.PullbackCandles = trigger.QualityMetrics.PullbackCandles
		marker.RemainingNetRR = trigger.QualityMetrics.RemainingNetRR
	}
	symbol := market.Normalize(trigger.Symbol)
	if symbol == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := traderID + "|" + symbol
	report := e.latestSignals[key]
	if report == nil {
		report = e.baseSignalReport(traderID, symbol)
	}
	report.SignalMarkers = append(report.SignalMarkers, marker)
	if len(report.SignalMarkers) > maxChanlunV2ReportMarkers {
		report.SignalMarkers = report.SignalMarkers[len(report.SignalMarkers)-maxChanlunV2ReportMarkers:]
	}
	report.MarkerSummary = buildChanlunV2MarkerSummary(report.SignalMarkers, report.SignalMarkers)
	e.latestSignals[key] = report
}

func signalActionForDirection(direction string) string {
	if strings.EqualFold(direction, "short") {
		return "open_short"
	}
	return "open_long"
}

func v2TriggerTypeAllowed(timing config.ChanlunV2EntryTimingConfig, triggerType string) bool {
	if triggerType == "" {
		return false
	}
	for _, allowed := range timing.AllowedTriggerTypes {
		if strings.EqualFold(strings.TrimSpace(allowed), triggerType) {
			return true
		}
	}
	return false
}

func marketDataForV2(ctx *decision.Context, symbol string) *market.Data {
	if ctx == nil || ctx.MarketDataMap == nil {
		return nil
	}
	normalized := market.Normalize(symbol)
	if data := ctx.MarketDataMap[normalized]; data != nil {
		return data
	}
	return ctx.MarketDataMap[symbol]
}

func invalidStopTakeProfit(action string, currentPrice, stopLoss, takeProfit float64) bool {
	if stopLoss <= 0 || takeProfit <= 0 {
		return true
	}
	if currentPrice <= 0 {
		return false
	}
	switch decision.DecisionDirection(action) {
	case "long":
		return !(stopLoss < currentPrice && currentPrice < takeProfit)
	case "short":
		return !(takeProfit < currentPrice && currentPrice < stopLoss)
	default:
		return true
	}
}

func klinesAfter(klines []market.Kline, closeTime int64) []market.Kline {
	if len(klines) == 0 {
		return nil
	}
	closeTime = normalizeV2EpochMillis(closeTime)
	out := make([]market.Kline, 0, len(klines))
	for _, k := range klines {
		k = normalizeMarketKlineTime(k)
		if closeTime > 0 && k.CloseTime <= closeTime {
			continue
		}
		out = append(out, k)
	}
	return out
}

func normalizeMarketKlines(klines []market.Kline) []market.Kline {
	out := make([]market.Kline, len(klines))
	for i, k := range klines {
		out[i] = normalizeMarketKlineTime(k)
	}
	return out
}

func normalizeMarketKlineTime(k market.Kline) market.Kline {
	k.OpenTime = normalizeV2EpochMillis(k.OpenTime)
	k.CloseTime = normalizeV2EpochMillis(k.CloseTime)
	return k
}
