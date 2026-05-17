package chanlun

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"nofx/decision"
	"nofx/market"
	"strings"
	"time"
)

func defaultPositionManagementPolicy(partialClosePct float64) decision.ProgrammaticPositionManagementPolicy {
	if partialClosePct <= 0 {
		partialClosePct = 30
	}
	return decision.ProgrammaticPositionManagementPolicy{
		Enabled: true,
		Timeframes: decision.ProgrammaticManagementTFPolicy{
			Structure: "15m",
			Micro:     "3m",
		},
		Breakeven: decision.ProgrammaticBreakevenPolicy{
			Enabled:          true,
			TriggerProfitPct: 1.0,
			TriggerR:         1.0,
			BufferRatio:      0.0005,
		},
		FloatingDrawdown: decision.ProgrammaticFloatingDrawdownPolicy{
			Enabled:             true,
			ActivationProfitPct: 2.0,
			ActivationR:         1.5,
			DrawdownRatio:       0.35,
			Action:              "partial_close",
		},
		StructureBreak: decision.ProgrammaticStructureBreakPolicy{
			Enabled:                 true,
			ConfirmBars:             2,
			Action:                  "partial_close",
			PartialCloseGuardAction: "bypass_cooldown_clip_budget",
		},
		ShortTrade: decision.ProgrammaticShortTradePolicy{
			Enabled:         true,
			PartialClosePct: partialClosePct,
		},
		PartialCloseGuard: decision.ProgrammaticPartialCloseGuardPolicy{
			CooldownMinutes:     15,
			MaxCountPerPosition: 2,
			MaxTotalRatio:       0.5,
			CooldownEnabled:     true,
		},
	}
}

func (e *Engine) evaluatePositionManagement(ctx *decision.Context, now time.Time) ([]decision.Decision, []string) {
	if !e.Policy.PositionManagement.Enabled {
		return nil, []string{"持仓管理层已关闭"}
	}
	if len(ctx.Positions) == 0 {
		e.StateStore.ResetPositionGuardIfMissing(ctx.TraderID, nil)
		return nil, []string{"持仓管理层无持仓"}
	}
	e.StateStore.ResetPositionGuardIfMissing(ctx.TraderID, ctx.Positions)
	var decisions []decision.Decision
	var diagnostics []string
	evaluated := 0
	for _, pos := range ctx.Positions {
		symbol := market.Normalize(pos.Symbol)
		var data *market.Data
		if ctx.MarketDataMap != nil {
			data = ctx.MarketDataMap[symbol]
		}
		if data == nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 持仓管理跳过: 缺少行情数据", symbol))
			continue
		}
		evaluated++
		d, diag := e.analyzePosition(ctx, pos, data, now)
		diagnostics = append(diagnostics, diag...)
		if d.Action == "" {
			continue
		}
		rule, _ := d.StrategyMetadata["rule"].(string)
		if e.StateStore.HasPositionSignal(ctx.TraderID, symbol, pos.Side, rule, d.SignalID) {
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s 已处理过signal_id=%s", symbol, rule, d.SignalID))
			if marker, ok := e.decisionToMarker(d, "deduped"); ok {
				marker.Reason = "已处理过signal_id"
				e.StateStore.StoreSignalMarker(ctx.TraderID, symbol, marker)
			}
			continue
		}
		if d.Action == "partial_close" {
			guarded, guardDiag := e.applyPartialCloseGuard(ctx, pos, d, now)
			diagnostics = append(diagnostics, guardDiag...)
			if guarded.Action == "" {
				if marker, ok := e.decisionToMarker(d, "rejected"); ok {
					marker.Reason = strings.Join(guardDiag, "; ")
					e.StateStore.StoreSignalMarker(ctx.TraderID, symbol, marker)
				}
				continue
			}
			d = guarded
		}
		if marker, ok := e.decisionToMarker(d, "detected"); ok {
			e.StateStore.StoreSignalMarker(ctx.TraderID, symbol, marker)
		}
		decisions = append(decisions, d)
	}
	diagnostics = append([]string{fmt.Sprintf("持仓管理已评估%d个持仓，触发%d个动作", evaluated, len(decisions))}, diagnostics...)
	return decisions, diagnostics
}

func (e *Engine) analyzePosition(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, now time.Time) (decision.Decision, []string) {
	symbol := market.Normalize(pos.Symbol)
	side := strings.ToLower(pos.Side)
	if side != SideLong && side != SideShort {
		return decision.Decision{}, []string{fmt.Sprintf("%s 持仓管理跳过: 未知方向%s", symbol, pos.Side)}
	}
	state := e.syncPositionPeakState(ctx, pos, data)
	if d, diag := e.evaluateStructureBreak(ctx, pos, data, state, now); d.Action != "" {
		return d, diag
	} else if len(diag) > 0 {
		return decision.Decision{}, diag
	}
	if d, diag := e.evaluateFloatingDrawdown(ctx, pos, data, state, now); d.Action != "" {
		return d, diag
	}
	if d, diag := e.evaluateShortTrade(ctx, pos, data, now); d.Action != "" {
		return d, diag
	}
	if d, diag := e.evaluateBreakeven(ctx, pos, data, now); d.Action != "" {
		return d, diag
	}
	return decision.Decision{}, []string{fmt.Sprintf("%s 持仓管理无动作", symbol)}
}

type partialCloseGuardResult struct {
	Allowed            bool
	RequestedClosePct  float64
	ClosePercentage    float64
	BlockedReason      string
	CooldownRemaining  time.Duration
	BudgetRemainingPct float64
	CountRemaining     int
	Clipped            bool
	UpgradeToClose     bool
	Estimated          bool
}

func (e *Engine) applyPartialCloseGuard(ctx *decision.Context, pos decision.PositionInfo, d decision.Decision, now time.Time) (decision.Decision, []string) {
	rule, _ := d.StrategyMetadata["rule"].(string)
	symbol := market.Normalize(pos.Symbol)
	side := strings.ToLower(pos.Side)
	policy := effectivePartialCloseGuardPolicy(e.Policy.PositionManagement.PartialCloseGuard)
	structureAction := strings.TrimSpace(e.Policy.PositionManagement.StructureBreak.PartialCloseGuardAction)
	if structureAction == "" {
		structureAction = "bypass_cooldown_clip_budget"
	}
	if rule == "structure_break" && closeActionForPolicy(pos, e.Policy.PositionManagement.StructureBreak.Action) != "partial_close" {
		return d, nil
	}
	state := e.StateStore.UpdatePositionState(ctx.TraderID, symbol, side, func(state *ProgrammaticPositionState) {
		guard := state.PartialCloseGuard
		if guard.InitialTrackedQuantity <= 0 && pos.Quantity > 0 {
			guard.InitialTrackedQuantity = math.Abs(pos.Quantity)
			price := currentPriceForPosition(pos, nil)
			if price > 0 {
				guard.InitialTrackedValueUSD = guard.InitialTrackedQuantity * price
			}
		}
		if pos.Quantity > 0 {
			guard.LastKnownQuantity = math.Abs(pos.Quantity)
		}
		state.PartialCloseGuard = guard
	})
	guard := state.PartialCloseGuard

	result := partialCloseGuardResult{
		Allowed:            true,
		RequestedClosePct:  d.ClosePercentage,
		ClosePercentage:    d.ClosePercentage,
		BudgetRemainingPct: math.Max(0, policy.MaxTotalRatio*100-guard.TotalPartialClosePct),
		CountRemaining:     policy.MaxCountPerPosition - guard.PartialCloseCount,
	}
	ignoreCooldown := rule == "structure_break" && (structureAction == "bypass_cooldown_clip_budget" || structureAction == "close_on_budget_exhausted")
	budgetExhausted := false
	if policy.CooldownEnabled && !ignoreCooldown && !guard.LastPartialCloseAt.IsZero() {
		until := guard.LastPartialCloseAt.Add(time.Duration(policy.CooldownMinutes) * time.Minute)
		if now.Before(until) {
			result.Allowed = false
			result.CooldownRemaining = until.Sub(now)
			result.BlockedReason = "partial_close冷却中"
		}
	}
	if result.Allowed && guard.PartialCloseCount >= policy.MaxCountPerPosition {
		budgetExhausted = true
		result.Allowed = false
		result.BlockedReason = "partial_close次数预算耗尽"
	}
	initialQty := guard.InitialTrackedQuantity
	if initialQty <= 0 && pos.Quantity > 0 {
		initialQty = math.Abs(pos.Quantity)
		result.Estimated = true
	}
	maxQty := initialQty * policy.MaxTotalRatio
	remainingQtyBudget := maxQty - guard.TotalPartialCloseQuantity
	if result.Allowed && initialQty > 0 && remainingQtyBudget <= 0 {
		budgetExhausted = true
		result.Allowed = false
		result.BlockedReason = "partial_close累计比例预算耗尽"
	}
	currentQty := math.Abs(pos.Quantity)
	if result.Allowed && currentQty > 0 && remainingQtyBudget > 0 {
		maxPctForCurrent := remainingQtyBudget / currentQty * 100
		result.BudgetRemainingPct = maxPctForCurrent
		if d.ClosePercentage > maxPctForCurrent {
			result.ClosePercentage = math.Max(0, maxPctForCurrent)
			result.Clipped = true
		}
	}
	if result.Allowed && result.ClosePercentage <= 0 {
		result.Allowed = false
		result.BlockedReason = "partial_close剩余预算过小"
	}
	if rule == "structure_break" && structureAction == "close_on_budget_exhausted" && budgetExhausted {
		result.Allowed = true
		result.UpgradeToClose = true
		result.ClosePercentage = 100
		d.Action = closeActionForPolicy(pos, "close")
		d.ClosePercentage = 0
		if d.Explanation != nil {
			d.Explanation.Summary = strings.Replace(d.Explanation.Summary, "部分平仓", "全平", 1)
		}
		appendGuardMetadata(&d, result, policy, guard)
		return d, []string{fmt.Sprintf("%s 结构破坏partial_close预算耗尽，已升级为%s", symbol, d.Action)}
	}
	if !result.Allowed {
		if rule == "structure_break" && structureAction == "respect_guard" {
			return decision.Decision{}, []string{formatPartialCloseGuardBlock(symbol, rule, result, policy, guard)}
		}
		return decision.Decision{}, []string{formatPartialCloseGuardBlock(symbol, rule, result, policy, guard)}
	}
	if result.Clipped {
		oldPct := d.ClosePercentage
		d.ClosePercentage = result.ClosePercentage
		appendGuardMetadata(&d, result, policy, guard)
		return d, []string{fmt.Sprintf("%s %s partial_close预算裁剪: %.2f%% -> %.2f%%", symbol, rule, oldPct, d.ClosePercentage)}
	}
	appendGuardMetadata(&d, result, policy, guard)
	return d, nil
}

func effectivePartialCloseGuardPolicy(policy decision.ProgrammaticPartialCloseGuardPolicy) decision.ProgrammaticPartialCloseGuardPolicy {
	if policy.CooldownMinutes == 0 && !policy.CooldownEnabled && policy.MaxCountPerPosition == 0 && policy.MaxTotalRatio == 0 {
		return decision.ProgrammaticPartialCloseGuardPolicy{
			CooldownMinutes:     15,
			MaxCountPerPosition: 2,
			MaxTotalRatio:       0.5,
			CooldownEnabled:     true,
		}
	}
	if policy.CooldownMinutes < 0 || policy.CooldownMinutes > 1440 {
		policy.CooldownMinutes = 15
	}
	if policy.MaxCountPerPosition <= 0 {
		policy.MaxCountPerPosition = 2
	}
	if policy.MaxTotalRatio <= 0 {
		policy.MaxTotalRatio = 0.5
	}
	if policy.CooldownMinutes == 0 {
		policy.CooldownEnabled = false
	} else {
		policy.CooldownEnabled = true
	}
	return policy
}

func formatPartialCloseGuardBlock(symbol, rule string, result partialCloseGuardResult, policy decision.ProgrammaticPartialCloseGuardPolicy, guard PartialCloseGuardState) string {
	if result.CooldownRemaining > 0 {
		return fmt.Sprintf("%s %s partial_close冷却中: 剩余%.0fm, 上次规则=%s, signal_id=%s",
			symbol, rule, result.CooldownRemaining.Minutes(), guard.LastPartialCloseRule, guard.LastPartialCloseSignalID)
	}
	return fmt.Sprintf("%s %s partial_close预算阻断: %s count=%d/%d total=%.2f%%/%.2f%% signal_id=%s",
		symbol, rule, result.BlockedReason, guard.PartialCloseCount, policy.MaxCountPerPosition,
		guard.TotalPartialClosePct, policy.MaxTotalRatio*100, guard.LastPartialCloseSignalID)
}

func appendGuardMetadata(d *decision.Decision, result partialCloseGuardResult, policy decision.ProgrammaticPartialCloseGuardPolicy, guard PartialCloseGuardState) {
	if d == nil {
		return
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	cooldownStatus := map[string]any{
		"enabled":           policy.CooldownEnabled,
		"cooldown_minutes":  policy.CooldownMinutes,
		"remaining_seconds": int(result.CooldownRemaining.Seconds()),
		"last_rule":         guard.LastPartialCloseRule,
		"last_signal_id":    guard.LastPartialCloseSignalID,
	}
	budgetStatus := map[string]any{
		"requested_pct":          result.RequestedClosePct,
		"final_pct":              result.ClosePercentage,
		"clipped":                result.Clipped,
		"upgrade_to_close":       result.UpgradeToClose,
		"count":                  guard.PartialCloseCount,
		"max_count":              policy.MaxCountPerPosition,
		"total_pct":              guard.TotalPartialClosePct,
		"max_total_pct":          policy.MaxTotalRatio * 100,
		"remaining_pct":          result.BudgetRemainingPct,
		"quantity_estimated":     guard.QuantityEstimated || result.Estimated,
		"initial_tracked_qty":    guard.InitialTrackedQuantity,
		"partial_close_quantity": guard.TotalPartialCloseQuantity,
	}
	d.StrategyMetadata["cooldown_status"] = cooldownStatus
	d.StrategyMetadata["budget_status"] = budgetStatus
	ensureDecisionExplanation(d)
	d.Explanation.CooldownStatus = cooldownStatus
	d.Explanation.BudgetStatus = budgetStatus
}

func (e *Engine) syncPositionPeakState(ctx *decision.Context, pos decision.PositionInfo, data *market.Data) ProgrammaticPositionState {
	symbol := market.Normalize(pos.Symbol)
	side := strings.ToLower(pos.Side)
	currentPrice := currentPriceForPosition(pos, data)
	currentR := currentRForPosition(ctx, pos, currentPrice)
	return e.StateStore.UpdatePositionState(ctx.TraderID, symbol, side, func(state *ProgrammaticPositionState) {
		if state.Side == "" {
			state.Side = side
		}
		newPeak := false
		switch side {
		case SideLong:
			if currentPrice > state.PeakPrice || state.PeakPrice == 0 {
				state.PeakPrice = currentPrice
				state.LastDrawdownSignalID = ""
				newPeak = true
			}
		case SideShort:
			if currentPrice < state.PeakPrice || state.PeakPrice == 0 {
				state.PeakPrice = currentPrice
				state.LastDrawdownSignalID = ""
				newPeak = true
			}
		}
		if currentR > state.PeakR {
			state.PeakR = currentR
			state.LastDrawdownSignalID = ""
			newPeak = true
		}
		if newPeak {
			state.PartialCloseGuard.RequireNewPeakForDrawdown = false
		}
	})
}

func (e *Engine) evaluateBreakeven(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, now time.Time) (decision.Decision, []string) {
	policy := e.Policy.PositionManagement.Breakeven
	if !policy.Enabled {
		return decision.Decision{}, nil
	}
	currentPrice := currentPriceForPosition(pos, data)
	currentR := currentRForPosition(ctx, pos, currentPrice)
	if pos.UnrealizedPnLPct < policy.TriggerProfitPct && currentR < policy.TriggerR {
		return decision.Decision{}, []string{fmt.Sprintf("%s 保本未触发: 浮盈%.2f%% R=%.2f", market.Normalize(pos.Symbol), pos.UnrealizedPnLPct, currentR)}
	}
	existingStop := effectiveStopForPosition(ctx, pos)
	candidate := 0.0
	if strings.ToLower(pos.Side) == SideShort {
		candidate = pos.EntryPrice * (1 - policy.BufferRatio)
		if existingStop > 0 && existingStop < candidate {
			candidate = existingStop
		}
		if currentPrice > 0 && candidate <= currentPrice {
			return decision.Decision{}, []string{fmt.Sprintf("%s 保本止损跳过: 空头候选止损%.4f低于当前价%.4f", market.Normalize(pos.Symbol), candidate, currentPrice)}
		}
	} else {
		candidate = pos.EntryPrice * (1 + policy.BufferRatio)
		if existingStop > 0 && existingStop > candidate {
			candidate = existingStop
		}
		if currentPrice > 0 && candidate >= currentPrice {
			return decision.Decision{}, []string{fmt.Sprintf("%s 保本止损跳过: 多头候选止损%.4f高于当前价%.4f", market.Normalize(pos.Symbol), candidate, currentPrice)}
		}
	}
	if !stopImprovesProtection(pos, existingStop, candidate) {
		return decision.Decision{}, []string{fmt.Sprintf("%s 保本止损未改善保护", market.Normalize(pos.Symbol))}
	}
	signalID := e.positionSignalID(ctx.TraderID, pos, "breakeven", "position", positionTriggerTime(pos, now),
		fmt.Sprintf("%.8f|%.8f|%.8f|%.4f|%.4f", pos.EntryPrice, existingStop, candidate, policy.TriggerProfitPct, policy.TriggerR))
	return e.positionDecision(ctx, pos, "breakeven", "update_stop_loss", signalID, fmt.Sprintf("程序化保本止损: %.4f", candidate), map[string]any{
		"current_r":          currentR,
		"current_price":      currentPrice,
		"existing_stop":      existingStop,
		"candidate_stop":     candidate,
		"timeframe":          "position",
		"trigger_close_time": positionTriggerTime(pos, now),
	}, func(d *decision.Decision) {
		d.NewStopLoss = candidate
	}), nil
}

func (e *Engine) evaluateFloatingDrawdown(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, state ProgrammaticPositionState, now time.Time) (decision.Decision, []string) {
	policy := e.Policy.PositionManagement.FloatingDrawdown
	if !policy.Enabled {
		return decision.Decision{}, nil
	}
	peakPnL := peakPnLPercent(ctx, pos, state)
	currentR := currentRForPosition(ctx, pos, currentPriceForPosition(pos, data))
	peakR := math.Max(state.PeakR, currentR)
	if peakPnL < policy.ActivationProfitPct && peakR < policy.ActivationR {
		return decision.Decision{}, nil
	}
	if peakPnL <= 0 {
		return decision.Decision{}, nil
	}
	drawdown := (peakPnL - pos.UnrealizedPnLPct) / peakPnL
	if drawdown < policy.DrawdownRatio {
		return decision.Decision{}, nil
	}
	if state.PartialCloseGuard.RequireNewPeakForDrawdown && policy.Action == "partial_close" {
		return decision.Decision{}, []string{fmt.Sprintf("%s 浮盈回撤已处理且未出现新peak: 上次signal_id=%s 上次peak=%.4f 当前peak=%.4f",
			market.Normalize(pos.Symbol), state.LastDrawdownSignalID, state.PartialCloseGuard.LastDrawdownPeakPrice, state.PeakPrice)}
	}
	signalID := e.positionSignalID(ctx.TraderID, pos, "floating_drawdown", "position", positionTriggerTime(pos, now),
		fmt.Sprintf("%.8f|%.4f|%.4f|%.4f", state.PeakPrice, peakPnL, pos.UnrealizedPnLPct, policy.DrawdownRatio))
	if state.LastDrawdownSignalID != "" {
		return decision.Decision{}, []string{fmt.Sprintf("%s 浮盈回撤已处理", market.Normalize(pos.Symbol))}
	}
	action := closeActionForPolicy(pos, policy.Action)
	reason := fmt.Sprintf("程序化浮盈回撤保护: 峰值%.2f%% 当前%.2f%% 回撤%.1f%%", peakPnL, pos.UnrealizedPnLPct, drawdown*100)
	return e.positionDecision(ctx, pos, "floating_drawdown", action, signalID, reason, map[string]any{
		"peak_pnl_pct":       peakPnL,
		"current_pnl_pct":    pos.UnrealizedPnLPct,
		"drawdown_ratio":     drawdown,
		"peak_r":             peakR,
		"current_r":          currentR,
		"timeframe":          "position",
		"trigger_close_time": positionTriggerTime(pos, now),
	}, func(d *decision.Decision) {
		if action == "partial_close" {
			d.ClosePercentage = e.Policy.Position.PartialClosePct
		}
	}), nil
}

func (e *Engine) evaluateStructureBreak(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, state ProgrammaticPositionState, now time.Time) (decision.Decision, []string) {
	policy := e.Policy.PositionManagement.StructureBreak
	if !policy.Enabled {
		return decision.Decision{}, nil
	}
	structureTF := e.Policy.PositionManagement.Timeframes.Structure
	microTF := e.Policy.PositionManagement.Timeframes.Micro
	level, triggerTime, ok := structureBreakLevel(data.Klines[structureTF], strings.ToLower(pos.Side))
	if !ok {
		return decision.Decision{}, nil
	}
	broken := structureLevelBroken(data.Klines[structureTF], strings.ToLower(pos.Side), level)
	if !broken && policy.ConfirmBars > 0 {
		broken = microStructureBroken(data.Klines[microTF], strings.ToLower(pos.Side), level, policy.ConfirmBars)
		if broken && len(data.Klines[microTF]) > 0 {
			triggerTime = data.Klines[microTF][len(data.Klines[microTF])-1].CloseTime
		}
	}
	if !broken {
		return decision.Decision{}, nil
	}
	signalID := e.positionSignalID(ctx.TraderID, pos, "structure_break", structureTF, triggerTime,
		fmt.Sprintf("%.8f|%s|%s", level, structureTF, policy.Action))
	if state.LastStructureSignalID == signalID {
		return decision.Decision{}, []string{fmt.Sprintf("%s 结构破坏已处理", market.Normalize(pos.Symbol))}
	}
	action := closeActionForPolicy(pos, policy.Action)
	reason := fmt.Sprintf("程序化结构破坏: %s %.4f", structureTF, level)
	return e.positionDecision(ctx, pos, "structure_break", action, signalID, reason, map[string]any{
		"structure_timeframe": structureTF,
		"micro_timeframe":     microTF,
		"structure_level":     level,
		"trigger_close_time":  triggerTime,
	}, func(d *decision.Decision) {
		if action == "partial_close" {
			d.ClosePercentage = e.Policy.Position.PartialClosePct
		}
	}), nil
}

func (e *Engine) evaluateShortTrade(ctx *decision.Context, pos decision.PositionInfo, data *market.Data, now time.Time) (decision.Decision, []string) {
	policy := e.Policy.PositionManagement.ShortTrade
	if !policy.Enabled {
		return decision.Decision{}, nil
	}
	tf := e.Policy.PositionManagement.Timeframes.Micro
	signals := e.detectPositionSignals(ctx.TraderID, market.Normalize(pos.Symbol), tf, data, now)
	for _, signal := range signals {
		if strings.ToLower(pos.Side) == SideLong && (signal.SignalType == SignalSell2 || signal.SignalType == SignalSell3) ||
			strings.ToLower(pos.Side) == SideShort && (signal.SignalType == SignalBuy2 || signal.SignalType == SignalBuy3) {
			signalID := signal.SignalID
			reason := fmt.Sprintf("程序化短差减仓: %s %s", signal.SignalType, tf)
			return e.positionDecision(ctx, pos, "short_trade", "partial_close", signalID, reason, map[string]any{
				"signal_type":        signal.SignalType,
				"timeframe":          tf,
				"trigger_close_time": signal.TriggerCloseTime,
			}, func(d *decision.Decision) {
				d.ClosePercentage = policy.PartialClosePct
				d.SignalType = signal.SignalType
				d.SignalTimeframe = tf
			}), nil
		}
	}
	return decision.Decision{}, nil
}

func (e *Engine) detectPositionSignals(traderID, symbol, tf string, data *market.Data, now time.Time) []ChanlunSignal {
	klines := data.Klines[tf]
	if len(klines) < 30 {
		return nil
	}
	candles := marketKlinesToCandles(tf, klines)
	normalized := NormalizeInclusion(candles)
	fractals := FindFractals(normalized, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
	strokes := BuildStrokes(fractals, normalized, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
	segments := BuildSegments(strokes, e.Policy.Structure.Strictness)
	if len(segments) < 3 {
		return nil
	}
	centers := BuildCenters(segments, tf)
	return DetectSignals(SignalInput{
		TraderID:          traderID,
		Symbol:            symbol,
		AnalysisTF:        tf,
		TriggerTF:         tf,
		Centers:           centers,
		Segments:          segments,
		MACDHist:          macdHistForTF(data, tf),
		ConfigHash:        e.Policy.ConfigHash,
		Now:               now,
		EnabledSignal:     map[string]bool{SignalBuy2: true, SignalBuy3: true, SignalSell2: true, SignalSell3: true},
		DivergenceRatio:   e.Policy.Divergence.Ratio,
		PriceTolerancePct: e.Policy.Divergence.PriceTolerancePct,
		RequireBZeroAxis:  e.Policy.Divergence.RequireBZeroAxis,
	})
}

func (e *Engine) positionDecision(ctx *decision.Context, pos decision.PositionInfo, rule, action, signalID, reason string, metadata map[string]any, mutate func(*decision.Decision)) decision.Decision {
	symbol := market.Normalize(pos.Symbol)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["layer"] = "position_management"
	metadata["rule"] = rule
	metadata["side"] = strings.ToLower(pos.Side)
	if triggerClose, ok := metadataInt64(metadata, "trigger_close_time"); ok && triggerClose > 0 {
		if _, exists := metadata["signal_close_time"]; !exists {
			metadata["signal_close_time"] = triggerClose
		}
		if _, exists := metadata["display_close_time"]; !exists {
			metadata["display_close_time"] = triggerClose
		}
	}
	d := decision.Decision{
		Symbol:           symbol,
		Action:           action,
		Leverage:         leverageForSymbol(ctx, symbol),
		Reasoning:        reason,
		StrategyMode:     "programmatic",
		StrategyName:     e.Policy.StrategyName,
		StrategyVersion:  e.Policy.StrategyVersion,
		ConfigHash:       e.Policy.ConfigHash,
		SignalID:         signalID,
		StrategyMetadata: metadata,
	}
	d.Explanation = newProgrammaticExplanation(d, reasonCodeForPositionRule(rule), reason)
	if action == "partial_close" && d.ClosePercentage <= 0 {
		d.ClosePercentage = e.Policy.Position.PartialClosePct
	}
	if mutate != nil {
		mutate(&d)
	}
	return d
}

func newProgrammaticExplanation(d decision.Decision, reasonCode, summary string) *decision.DecisionExplanation {
	layer, _ := d.StrategyMetadata["layer"].(string)
	rule, _ := d.StrategyMetadata["rule"].(string)
	timeframe := d.SignalTimeframe
	if timeframe == "" {
		if value, _ := d.StrategyMetadata["timeframe"].(string); value != "" {
			timeframe = value
		} else if value, _ := d.StrategyMetadata["structure_timeframe"].(string); value != "" {
			timeframe = value
		}
	}
	triggerPrice := d.StopLoss
	if value, ok := d.StrategyMetadata["current_price"].(float64); ok && value > 0 {
		triggerPrice = value
	}
	details := copyMetadata(d.StrategyMetadata)
	return &decision.DecisionExplanation{
		Summary:      summary,
		Layer:        layer,
		Rule:         rule,
		ReasonCode:   reasonCode,
		Timeframe:    timeframe,
		SignalType:   d.SignalType,
		SignalID:     d.SignalID,
		TriggerPrice: triggerPrice,
		Details:      details,
	}
}

func ensureDecisionExplanation(d *decision.Decision) {
	if d == nil {
		return
	}
	if d.Explanation == nil {
		d.Explanation = newProgrammaticExplanation(*d, reasonCodeForPositionRule(metadataString(d.StrategyMetadata, "rule")), d.Reasoning)
	}
}

func reasonCodeForPositionRule(rule string) string {
	switch rule {
	case "breakeven":
		return "breakeven_reached"
	case "floating_drawdown":
		return "peak_drawdown_reached"
	case "structure_break":
		return "structure_level_broken"
	case "short_trade":
		return "reverse_micro_signal"
	default:
		return "programmatic_position_management"
	}
}

func metadataString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func copyMetadata(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func (e *Engine) positionSignalID(traderID string, pos decision.PositionInfo, rule, timeframe string, triggerCloseTime int64, triggerHash string) string {
	value := fmt.Sprintf("pm:%s:%s:%s:%s:%s:%d:%s:%s",
		traderID, market.Normalize(pos.Symbol), strings.ToLower(pos.Side), rule, timeframe, triggerCloseTime, triggerHash, e.Policy.ConfigHash)
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func structureBreakLevel(klines []market.Kline, side string) (float64, int64, bool) {
	if len(klines) < 6 {
		return 0, 0, false
	}
	trigger := klines[len(klines)-1]
	prior := klines[:len(klines)-1]
	start := len(prior) - 20
	if start < 0 {
		start = 0
	}
	level := 0.0
	for i := start; i < len(prior); i++ {
		k := prior[i]
		if side == SideShort {
			if level == 0 || k.High > level {
				level = k.High
			}
			continue
		}
		if level == 0 || k.Low < level {
			level = k.Low
		}
	}
	return level, trigger.CloseTime, level > 0
}

func structureLevelBroken(klines []market.Kline, side string, level float64) bool {
	if len(klines) == 0 || level <= 0 {
		return false
	}
	close := klines[len(klines)-1].Close
	if side == SideShort {
		return close > level
	}
	return close < level
}

func microStructureBroken(klines []market.Kline, side string, level float64, bars int) bool {
	if len(klines) < bars || bars <= 0 || level <= 0 {
		return false
	}
	for _, k := range klines[len(klines)-bars:] {
		if side == SideShort {
			if k.Close <= level {
				return false
			}
			continue
		}
		if k.Close >= level {
			return false
		}
	}
	return true
}

func currentPriceForPosition(pos decision.PositionInfo, data *market.Data) float64 {
	if data != nil && data.CurrentPrice > 0 {
		return data.CurrentPrice
	}
	if pos.MarkPrice > 0 {
		return pos.MarkPrice
	}
	return pos.EntryPrice
}

func effectiveStopForPosition(ctx *decision.Context, pos decision.PositionInfo) float64 {
	stop := pos.StopLoss
	if ctx == nil {
		return stop
	}
	if plan := decision.GetPlanByScope(ctx.TraderID, pos.Symbol, pos.Side); plan != nil {
		if plan.CurrentStopLoss > 0 {
			stop = plan.CurrentStopLoss
		} else if plan.StopLoss > 0 {
			stop = plan.StopLoss
		}
	}
	return stop
}

func currentRForPosition(ctx *decision.Context, pos decision.PositionInfo, currentPrice float64) float64 {
	risk := initialRiskDistance(ctx, pos)
	if risk <= 0 || currentPrice <= 0 {
		return 0
	}
	if strings.ToLower(pos.Side) == SideShort {
		return math.Max(0, (pos.EntryPrice-currentPrice)/risk)
	}
	return math.Max(0, (currentPrice-pos.EntryPrice)/risk)
}

func initialRiskDistance(ctx *decision.Context, pos decision.PositionInfo) float64 {
	if ctx != nil {
		if plan := decision.GetPlanByScope(ctx.TraderID, pos.Symbol, pos.Side); plan != nil {
			if plan.InitialRiskDistance > 0 {
				return plan.InitialRiskDistance
			}
			if plan.StopLoss > 0 {
				return math.Abs(pos.EntryPrice - plan.StopLoss)
			}
		}
	}
	if pos.StopLoss > 0 {
		return math.Abs(pos.EntryPrice - pos.StopLoss)
	}
	return 0
}

func peakPnLPercent(ctx *decision.Context, pos decision.PositionInfo, state ProgrammaticPositionState) float64 {
	if ctx != nil {
		if plan := decision.GetPlanByScope(ctx.TraderID, pos.Symbol, pos.Side); plan != nil && plan.PeakPnLPercent > 0 {
			return plan.PeakPnLPercent
		}
	}
	if state.PeakPrice > 0 && pos.EntryPrice > 0 {
		if strings.ToLower(pos.Side) == SideShort {
			return (pos.EntryPrice - state.PeakPrice) / pos.EntryPrice * 100
		}
		return (state.PeakPrice - pos.EntryPrice) / pos.EntryPrice * 100
	}
	return math.Max(0, pos.UnrealizedPnLPct)
}

func stopImprovesProtection(pos decision.PositionInfo, existingStop, candidate float64) bool {
	if candidate <= 0 {
		return false
	}
	if strings.ToLower(pos.Side) == SideShort {
		if existingStop > 0 {
			return candidate < existingStop
		}
		return candidate <= pos.EntryPrice
	}
	if existingStop > 0 {
		return candidate > existingStop
	}
	return candidate >= pos.EntryPrice
}

func closeActionForPolicy(pos decision.PositionInfo, action string) string {
	if action == "close" {
		if strings.ToLower(pos.Side) == SideShort {
			return "close_short"
		}
		return "close_long"
	}
	return "partial_close"
}

func positionTriggerTime(pos decision.PositionInfo, now time.Time) int64 {
	if pos.UpdateTime > 0 {
		return pos.UpdateTime
	}
	return now.UnixMilli()
}
