package chanlunv2

import (
	"fmt"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"strings"
	"time"
)

func (e *Engine) evaluateV2PositionManagement(ctx *decision.Context, pos decision.PositionInfo, timeframes map[string]string) decision.Decision {
	if ctx == nil {
		return decision.Decision{}
	}
	pm := config.NormalizeChanlunV2PositionManagement(e.Config.PositionManagement)
	if pm.Enabled != nil && !*pm.Enabled {
		return decision.Decision{}
	}
	data := marketDataForV2(ctx, pos.Symbol)
	if data == nil {
		return decision.Decision{}
	}
	currentPrice := currentPositionPrice(pos, data)
	currentR, ok := currentFavorableR(pos, currentPrice)
	if !ok {
		return e.evaluateV2ReverseSignalClose(ctx, pos, timeframes, pm)
	}
	state := e.updateV2PositionPeak(ctx, pos, currentR)
	now := time.Now()
	if pm.StructureBreakEnabled == nil || *pm.StructureBreakEnabled {
		if d := e.evaluateV2StructureBreak(ctx, pos, data, pm, now); d.Action != "" {
			return d
		}
	}
	if pm.FloatingDrawdownEnabled == nil || *pm.FloatingDrawdownEnabled {
		if d := e.evaluateV2FloatingDrawdown(ctx, pos, pm, state, currentR, now); d.Action != "" {
			return d
		}
	}
	if pm.PartialTakeProfitEnabled == nil || *pm.PartialTakeProfitEnabled {
		if currentR >= pm.PartialTakeProfitR {
			if d := e.buildV2PartialCloseDecision(ctx, pos, pm, "partial_take_profit", fmt.Sprintf("缠论V2达到%.2fR，执行分批止盈", currentR), now); d.Action != "" {
				return d
			}
		}
	}
	if pm.BreakevenEnabled == nil || *pm.BreakevenEnabled {
		if d := e.evaluateV2Breakeven(ctx, pos, data, pm, currentR, now); d.Action != "" {
			return d
		}
	}
	return e.evaluateV2ReverseSignalClose(ctx, pos, timeframes, pm)
}

func (e *Engine) evaluateV2Breakeven(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, pm config.ChanlunV2PositionManagementConfig, currentR float64, now time.Time) decision.Decision {
	if currentR < pm.BreakevenTriggerR || pos.EntryPrice <= 0 {
		return decision.Decision{}
	}
	side := strings.ToLower(pos.Side)
	buffer := pm.BreakevenBufferPct / 100
	newStop := pos.EntryPrice
	if side == "long" {
		newStop = pos.EntryPrice * (1 + buffer)
		if pos.StopLoss > 0 && newStop <= pos.StopLoss {
			return decision.Decision{}
		}
		if currentPositionPrice(pos, data) > 0 && newStop >= currentPositionPrice(pos, data) {
			return decision.Decision{}
		}
	} else if side == "short" {
		newStop = pos.EntryPrice * (1 - buffer)
		if pos.StopLoss > 0 && newStop >= pos.StopLoss {
			return decision.Decision{}
		}
		if currentPositionPrice(pos, data) > 0 && newStop <= currentPositionPrice(pos, data) {
			return decision.Decision{}
		}
	} else {
		return decision.Decision{}
	}
	d := e.baseV2PositionDecision(ctx, pos, "update_stop_loss", "breakeven", "chanlun_v2_breakeven", now)
	d.NewStopLoss = newStop
	d.Reasoning = fmt.Sprintf("缠论V2 %.2fR后移动止损到保本 %.6f", currentR, newStop)
	return d
}

func (e *Engine) evaluateV2StructureBreak(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, pm config.ChanlunV2PositionManagementConfig, now time.Time) decision.Decision {
	tf := firstNonEmptyString(pm.StructureBreakTimeframe, "15m")
	klines := normalizeMarketKlines(data.Klines[tf])
	if len(klines) == 0 || pm.StructureBreakConfirmBars <= 0 || len(klines) < pm.StructureBreakConfirmBars {
		return decision.Decision{}
	}
	level := pos.StopLoss
	if level <= 0 {
		level = pos.EntryPrice
	}
	if level <= 0 {
		return decision.Decision{}
	}
	broken := true
	recent := klines[len(klines)-pm.StructureBreakConfirmBars:]
	side := strings.ToLower(pos.Side)
	for _, k := range recent {
		switch side {
		case "long":
			if k.Close > level {
				broken = false
			}
		case "short":
			if k.Close < level {
				broken = false
			}
		default:
			return decision.Decision{}
		}
	}
	if !broken {
		return decision.Decision{}
	}
	return e.buildV2PartialCloseDecision(ctx, pos, pm, "structure_break", fmt.Sprintf("缠论V2 %s 连续%d根K线破坏结构位 %.6f", tf, pm.StructureBreakConfirmBars, level), now)
}

func (e *Engine) evaluateV2FloatingDrawdown(ctx *decision.Context, pos decision.PositionInfo, pm config.ChanlunV2PositionManagementConfig, state PositionManagementState, currentR float64, now time.Time) decision.Decision {
	if state.PeakFavorableR < pm.FloatingDrawdownActivationR || state.PeakFavorableR <= 0 {
		return decision.Decision{}
	}
	drawdown := (state.PeakFavorableR - currentR) / state.PeakFavorableR * 100
	if drawdown < pm.FloatingDrawdownPct {
		return decision.Decision{}
	}
	return e.buildV2PartialCloseDecision(ctx, pos, pm, "floating_drawdown", fmt.Sprintf("缠论V2浮盈回撤 %.1f%%，峰值%.2fR 当前%.2fR", drawdown, state.PeakFavorableR, currentR), now)
}

func (e *Engine) buildV2PartialCloseDecision(ctx *decision.Context, pos decision.PositionInfo, pm config.ChanlunV2PositionManagementConfig, rule, reason string, now time.Time) decision.Decision {
	state := e.positionState(ctx, pos)
	if pm.PartialCloseCooldownMinutes > 0 && state.LastPartialCloseAt > 0 {
		until := time.UnixMilli(state.LastPartialCloseAt).Add(time.Duration(pm.PartialCloseCooldownMinutes) * time.Minute)
		if now.Before(until) {
			return decision.Decision{}
		}
	}
	if pm.MaxPartialCloseCountPerPosition > 0 && state.PartialCloseCount >= pm.MaxPartialCloseCountPerPosition {
		return decision.Decision{}
	}
	closePct := pm.PartialTakeProfitPct
	if closePct <= 0 {
		closePct = 50
	}
	if pm.MaxTotalPartialClosePct > 0 {
		remaining := pm.MaxTotalPartialClosePct - state.TotalPartialClosePct
		if remaining <= 0 {
			return decision.Decision{}
		}
		if closePct > remaining {
			closePct = remaining
		}
	}
	d := e.baseV2PositionDecision(ctx, pos, "partial_close", rule, "chanlun_v2_"+rule, now)
	d.ClosePercentage = closePct
	d.Reasoning = reason
	e.recordV2PartialCloseSignal(ctx, pos, closePct, now)
	return d
}

func (e *Engine) evaluateV2ReverseSignalClose(ctx *decision.Context, pos decision.PositionInfo, timeframes map[string]string, pm config.ChanlunV2PositionManagementConfig) decision.Decision {
	if pm.ReverseSignalCloseEnabled != nil && !*pm.ReverseSignalCloseEnabled {
		return decision.Decision{}
	}
	if ctx == nil {
		return decision.Decision{}
	}
	mr := e.analyzeSymbolFromContext(ctx, pos.Symbol, timeframes)
	if mr == nil {
		return decision.Decision{}
	}
	decisionCloseTime := evaluationCloseTime(mr, "trade")
	tradeResult, ok := mr.Results["trade"]
	if !ok {
		return decision.Decision{}
	}
	minConfidence := pm.ReverseSignalMinConfidence
	if minConfidence <= 0 {
		minConfidence = 60
	}
	for _, sig := range tradeResult.Signals {
		if (strings.EqualFold(pos.Side, "long") && sig.Direction == "short" && sig.Confidence >= minConfidence) ||
			(strings.EqualFold(pos.Side, "short") && sig.Direction == "long" && sig.Confidence >= minConfidence) {
			closeAction := "close_long"
			if strings.EqualFold(pos.Side, "short") {
				closeAction = "close_short"
			}
			signalCloseTime := normalizeV2EpochMillis(sig.Timestamp)
			if decisionCloseTime > 0 && signalCloseTime > 0 && decisionCloseTime < signalCloseTime {
				decisionCloseTime = signalCloseTime
			}
			d := e.baseV2PositionDecision(ctx, pos, closeAction, "reverse_signal", "chanlun_v2_reverse_signal", time.Now())
			d.SignalID = v2SignalID(pos.Symbol, timeframes["trade"], sig)
			d.SignalType = sig.SignalType
			d.SignalTimeframe = timeframes["trade"]
			d.Reasoning = fmt.Sprintf("缠论V2反向信号 %s 置信度%d", sig.SignalType, sig.Confidence)
			d.StrategyMetadata["timeframe"] = timeframes["trade"]
			d.StrategyMetadata["signal_close_time"] = signalCloseTime
			d.StrategyMetadata["decision_close_time"] = decisionCloseTime
			d.StrategyMetadata["evaluation_close_time"] = decisionCloseTime
			return d
		}
	}
	return decision.Decision{}
}

func (e *Engine) baseV2PositionDecision(ctx *decision.Context, pos decision.PositionInfo, action, rule, reasonCode string, now time.Time) decision.Decision {
	traderID := ""
	if ctx != nil {
		traderID = ctx.TraderID
	}
	symbol := market.Normalize(pos.Symbol)
	side := strings.ToLower(pos.Side)
	signalID := fmt.Sprintf("chanlun_v2_pm:%s:%s:%s:%s:%d", traderID, symbol, side, rule, now.UnixMilli())
	return decision.Decision{
		Symbol:          symbol,
		Action:          action,
		Reasoning:       "缠论V2持仓管理",
		StrategyMode:    "chanlun_v2",
		StrategyName:    "chanlun_v2",
		StrategyVersion: "v0.1",
		ConfigHash:      e.configHash,
		SignalID:        signalID,
		SignalType:      rule,
		SignalTimeframe: "position",
		StrategyMetadata: map[string]any{
			"layer":                 "position_management",
			"rule":                  rule,
			"reason_code":           reasonCode,
			"timeframe":             "position",
			"trade_intent":          action,
			"position_side":         side,
			"signal_close_time":     now.UnixMilli(),
			"decision_close_time":   now.UnixMilli(),
			"evaluation_close_time": now.UnixMilli(),
			"action_timestamp":      now.UnixMilli(),
		},
	}
}

func (e *Engine) updateV2PositionPeak(ctx *decision.Context, pos decision.PositionInfo, currentR float64) PositionManagementState {
	key := positionManagementKey(ctx.TraderID, pos.Symbol, pos.Side)
	now := time.Now().UnixMilli()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.positionStates == nil {
		e.positionStates = map[string]PositionManagementState{}
	}
	state := e.positionStates[key]
	state.TraderID = ctx.TraderID
	state.Symbol = market.Normalize(pos.Symbol)
	state.Side = strings.ToLower(pos.Side)
	if currentR > state.PeakFavorableR {
		state.PeakFavorableR = currentR
	}
	state.UpdatedAt = now
	e.positionStates[key] = state
	return state
}

func (e *Engine) positionState(ctx *decision.Context, pos decision.PositionInfo) PositionManagementState {
	if e == nil || ctx == nil {
		return PositionManagementState{}
	}
	key := positionManagementKey(ctx.TraderID, pos.Symbol, pos.Side)
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.positionStates[key]
}

func (e *Engine) recordV2PartialCloseSignal(ctx *decision.Context, pos decision.PositionInfo, closePct float64, now time.Time) {
	if e == nil || ctx == nil {
		return
	}
	key := positionManagementKey(ctx.TraderID, pos.Symbol, pos.Side)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.positionStates == nil {
		e.positionStates = map[string]PositionManagementState{}
	}
	state := e.positionStates[key]
	state.TraderID = ctx.TraderID
	state.Symbol = market.Normalize(pos.Symbol)
	state.Side = strings.ToLower(pos.Side)
	state.LastPartialCloseAt = now.UnixMilli()
	state.PartialCloseCount++
	state.TotalPartialClosePct += closePct
	state.UpdatedAt = now.UnixMilli()
	e.positionStates[key] = state
}

func currentPositionPrice(pos decision.PositionInfo, data *market.Data) float64 {
	if data != nil && data.CurrentPrice > 0 {
		return data.CurrentPrice
	}
	if pos.MarkPrice > 0 {
		return pos.MarkPrice
	}
	return pos.EntryPrice
}

func currentFavorableR(pos decision.PositionInfo, currentPrice float64) (float64, bool) {
	if pos.EntryPrice <= 0 || currentPrice <= 0 || pos.StopLoss <= 0 {
		return 0, false
	}
	side := strings.ToLower(pos.Side)
	switch side {
	case "long":
		risk := pos.EntryPrice - pos.StopLoss
		if risk <= 0 {
			return 0, false
		}
		return (currentPrice - pos.EntryPrice) / risk, true
	case "short":
		risk := pos.StopLoss - pos.EntryPrice
		if risk <= 0 {
			return 0, false
		}
		return (pos.EntryPrice - currentPrice) / risk, true
	default:
		return 0, false
	}
}
