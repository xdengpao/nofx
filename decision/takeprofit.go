package decision

import (
	"fmt"
	"log"
	"math"
	"nofx/market"
	"strings"
	"time"
)

//止盈止损管理

// ============================================================================
// 止盈配置
// ============================================================================

// TakeProfitEngineConfig 止盈引擎配置
type TakeProfitEngineConfig struct {
	EnableDynamicTP      bool    `json:"enable_dynamic_tp"`
	EnableScaledExit     bool    `json:"enable_scaled_exit"`
	EnableATRTrailing    bool    `json:"enable_atr_trailing"`
	EnableProfitProtect  bool    `json:"enable_profit_protect"`
	PriorityMode         string  `json:"priority_mode"`
	MinimumProfitLock    float64 `json:"minimum_profit_lock"`
	ATRTrailingMult      float64 `json:"atr_trailing_mult"`
	ProfitProtectTrigger float64 `json:"profit_protect_trigger"`
	ProfitProtectRatio   float64 `json:"profit_protect_ratio"`
}

// TrailingStopConfig 移动止损配置
type TrailingStopConfig struct {
	BreakevenThreshold    float64
	LockProfitThresholds  []TrailingStopLevel
	TriggerGuardMinPct    float64
	TriggerGuardATRMult   float64
	TriggerGuardMaxPct    float64
	TrendToleranceATRMult float64
	TrendToleranceMinPct  float64
	MinProfitBuffer       float64
}

// TrailingStopLevel 移动止损档位
type TrailingStopLevel struct {
	PnLThreshold    float64
	LockPercent     float64
	RequireADXAbove float64
}

const (
	softStopLossPnLPct         = -5.0
	noMomentumMFEThresholdPct  = 1.5
	noMomentumLossPnLPct       = -1.5
	noMomentumHoldMinutes      = 45
	mfeGivebackTriggerPct      = 3.0
	mfeGivebackCloseRatio      = 0.70
	highPeakProfitProtectPct   = 12.0
	highPeakProfitProtectRatio = 0.65
	basePeakProfitProtectRatio = 0.60
)

// DynamicExitTranche 动态出场档位
type DynamicExitTranche struct {
	BaseRR           float64
	ClosePercent     float64
	MoveStopTo       string
	RequiresMomentum bool
	MinADX           float64
	MaxHoldHours     float64
}

// 默认配置
var defaultTPConfig = &TakeProfitEngineConfig{
	EnableDynamicTP:      true,
	EnableScaledExit:     true,
	EnableATRTrailing:    true,
	EnableProfitProtect:  true,
	PriorityMode:         "balanced",
	MinimumProfitLock:    5.0,
	ATRTrailingMult:      2.5,
	ProfitProtectTrigger: 8.0,
	ProfitProtectRatio:   basePeakProfitProtectRatio,
}

var defaultTrailingConfig = &TrailingStopConfig{
	BreakevenThreshold: 2.0,
	LockProfitThresholds: []TrailingStopLevel{
		{PnLThreshold: 10.0, LockPercent: 0.25, RequireADXAbove: 20},
		{PnLThreshold: 12.0, LockPercent: 0.40, RequireADXAbove: 20},
		{PnLThreshold: 20.0, LockPercent: 0.60, RequireADXAbove: 15},
	},
	TriggerGuardMinPct:    0.005,
	TriggerGuardATRMult:   0.25,
	TriggerGuardMaxPct:    0.012,
	TrendToleranceATRMult: 1.2,
	TrendToleranceMinPct:  0.012,
	MinProfitBuffer:       2.0,
}

var defaultExitTranches = []DynamicExitTranche{
	{BaseRR: 2.5, ClosePercent: 20, MoveStopTo: "breakeven", RequiresMomentum: false, MinADX: 20, MaxHoldHours: 48},
	{BaseRR: 4.0, ClosePercent: 30, MoveStopTo: "lock_1r", RequiresMomentum: false, MinADX: 22, MaxHoldHours: 72},
	{BaseRR: 6.0, ClosePercent: 30, MoveStopTo: "lock_2r", RequiresMomentum: true, MinADX: 25, MaxHoldHours: 96},
	{BaseRR: 10.0, ClosePercent: 20, MoveStopTo: "lock_4r", RequiresMomentum: true, MinADX: 28, MaxHoldHours: 0},
}

// GetTakeProfitConfig 获取止盈配置
func GetTakeProfitConfig() *TakeProfitEngineConfig {
	return defaultTPConfig
}

// ============================================================================
// 持仓评估器
// ============================================================================

// PositionEvaluator 持仓评估器
type PositionEvaluator struct {
	Position      *PositionInfo
	Plan          *TradePlan
	MarketData    *market.Data
	BTCMarketData *market.Data
	Symbol        string
	Exchange      string
}

// Evaluate 评估持仓
func (e *PositionEvaluator) Evaluate() *EvaluationResult {
	result := &EvaluationResult{
		Action:           "hold",
		Reason:           "继续持有",
		ShouldUpdatePeak: true, // 默认需要更新峰值
		TrancheIndex:     -1,
	}

	if e.Position == nil || e.MarketData == nil {
		return result
	}

	currentPrice := e.MarketData.CurrentPrice
	holdingMinutes := e.getHoldingMinutes()
	tpConfig := GetTakeProfitConfig()

	// 第一优先级：硬性止损检查
	if e.Plan != nil {
		effectiveSL := e.getEffectiveStopLoss()

		if e.Plan.Direction == "long" && currentPrice <= effectiveSL {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🛑 触发止损: 当前价%.4f <= 止损价%.4f", currentPrice, effectiveSL),
				IsHardStop: true,
			}
		}
		if e.Plan.Direction == "short" && currentPrice >= effectiveSL {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🛑 触发止损: 当前价%.4f >= 止损价%.4f", currentPrice, effectiveSL),
				IsHardStop: true,
			}
		}
	}

	// 第二优先级：固定止盈检查
	if e.Plan != nil && e.Plan.TakeProfit > 0 {
		if e.Plan.Direction == "long" && currentPrice >= e.Plan.TakeProfit {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🎯 触发止盈: 当前价%.4f >= 止盈价%.4f", currentPrice, e.Plan.TakeProfit),
				IsHardStop: true,
			}
		}
		if e.Plan.Direction == "short" && currentPrice <= e.Plan.TakeProfit {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🎯 触发止盈: 当前价%.4f <= 止盈价%.4f", currentPrice, e.Plan.TakeProfit),
				IsHardStop: true,
			}
		}
	}

	// 第三优先级：最小持仓时间保护
	minHoldMinutes := 30
	if e.Plan != nil && e.Plan.MinHoldMinutes > 0 {
		minHoldMinutes = e.Plan.MinHoldMinutes
	}

	if holdingMinutes < int64(minHoldMinutes) {
		// 保护期内只有极端亏损才平仓
		if e.Position.UnrealizedPnLPct < -3.0 {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("⚠️ 保护期内极端亏损(%.2f%% < -3%%)，紧急平仓", e.Position.UnrealizedPnLPct),
				IsHardStop: true,
			}
		}
		result.Reason = fmt.Sprintf("📋 持仓保护期(%d/%d分钟)，继续持有", holdingMinutes, minHoldMinutes)
		return result
	}

	// 第四优先级：保护期后的软止损
	if softStopResult := e.evaluateSoftStop(holdingMinutes); softStopResult != nil {
		return softStopResult
	}

	// 第五优先级：利润保护机制
	if tpConfig.EnableProfitProtect && e.Plan != nil {
		if protectResult := e.checkProfitProtection(tpConfig); protectResult != nil {
			return protectResult
		}
	}

	// 第六优先级：ATR跟踪止盈
	if tpConfig.EnableATRTrailing && e.Position.UnrealizedPnLPct > 5.0 && e.Plan != nil {
		if atrResult := e.evaluateATRTrailingTakeProfit(tpConfig); atrResult != nil {
			return atrResult
		}
	}

	// 第七优先级：智能分批止盈
	if tpConfig.EnableScaledExit && e.Plan != nil && e.Position.UnrealizedPnLPct > 0 {
		if scaledResult := e.evaluateAdaptiveScaledExit(); scaledResult != nil {
			return scaledResult
		}
	}

	// 第八优先级：移动止损
	if e.Position.UnrealizedPnLPct > 0 && e.Plan != nil {
		if trailingResult := e.evaluateTrailingStop(); trailingResult != nil {
			return trailingResult
		}
	}

	// 第九优先级：动态止盈调整
	if tpConfig.EnableDynamicTP && e.Plan != nil {
		if newTP := e.calculateDynamicTakeProfit(tpConfig); newTP > 0 {
			result.NewTakeProfit = newTP
		}
	}

	// 第十优先级：计划失效条件检查
	if holdingMinutes >= 60 && e.Plan != nil {
		if invalidated, reason := e.checkPlanInvalidation(); invalidated {
			return &EvaluationResult{
				Action:            "close",
				Reason:            reason,
				IsPlanInvalidated: true,
			}
		}
	}

	return result
}

// ============================================================================
// 辅助方法
// ============================================================================

func (e *PositionEvaluator) getHoldingMinutes() int64 {
	if e.Position.UpdateTime <= 0 {
		return 0
	}
	return (time.Now().UnixMilli() - e.Position.UpdateTime) / (1000 * 60)
}

func (e *PositionEvaluator) getEffectiveStopLoss() float64 {
	if e.Plan == nil {
		return 0
	}
	if e.Plan.CurrentStopLoss > 0 {
		return e.Plan.CurrentStopLoss
	}
	return e.Plan.StopLoss
}

func (e *PositionEvaluator) calculateCurrentRR() float64 {
	if e.Plan == nil {
		return 0
	}

	riskDistance := math.Abs(e.Plan.EntryPrice - e.Plan.StopLoss)
	if riskDistance == 0 {
		return 0
	}

	currentDistance := math.Abs(e.MarketData.CurrentPrice - e.Plan.EntryPrice)
	return currentDistance / riskDistance
}

func (e *PositionEvaluator) getATR() float64 {
	if e.MarketData.LongerTermContext != nil && e.MarketData.LongerTermContext.ATR14 > 0 {
		return e.MarketData.LongerTermContext.ATR14
	}
	return e.MarketData.CurrentPrice * 0.02
}

// ============================================================================
// 保护期后的软止损
// ============================================================================

func (e *PositionEvaluator) evaluateSoftStop(holdingMinutes int64) *EvaluationResult {
	pnlPct := e.Position.UnrealizedPnLPct
	if pnlPct <= softStopLossPnLPct {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("⚠️ 软止损触发: 持仓%d分钟，当前亏损%.2f%% <= %.2f%%",
				holdingMinutes, pnlPct, softStopLossPnLPct),
			IsHardStop: false,
		}
	}

	if e.Plan == nil {
		return nil
	}

	peakPnL := math.Max(e.Plan.PeakPnLPercent, pnlPct)
	noMomentumFailure := peakPnL < noMomentumMFEThresholdPct && pnlPct <= noMomentumLossPnLPct
	lateSevereFailure := peakPnL < 3.0 && pnlPct <= -3.0
	if holdingMinutes >= noMomentumHoldMinutes && (noMomentumFailure || lateSevereFailure) {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("⚠️ 动量失败软止损: 持仓%d分钟，MFE %.2f%% < %.2f%%，当前%.2f%%",
				holdingMinutes, peakPnL, noMomentumMFEThresholdPct, pnlPct),
			IsHardStop: false,
		}
	}

	if e.checkLostBreakevenWithWeakMomentum(peakPnL) {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("⚠️ 浮盈回吐软止损: 峰值盈利%.2f%%，当前%.2f%%，短周期动量转弱",
				peakPnL, pnlPct),
			IsHardStop: false,
		}
	}

	if e.checkMFEGivebackWithWeakMomentum(peakPnL) {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("⚠️ 浮盈大幅回吐软止损: 峰值盈利%.2f%%，当前%.2f%%，短周期动量转弱",
				peakPnL, pnlPct),
			IsHardStop: false,
		}
	}

	return nil
}

func (e *PositionEvaluator) checkLostBreakevenWithWeakMomentum(peakPnL float64) bool {
	if peakPnL < noMomentumMFEThresholdPct || e.Position.UnrealizedPnLPct > 0 {
		return false
	}
	return hasWeakShortTermMomentum(e.MarketData, e.Plan.Direction) ||
		hasWeakShortTermMomentum(e.BTCMarketData, e.Plan.Direction)
}

func (e *PositionEvaluator) checkMFEGivebackWithWeakMomentum(peakPnL float64) bool {
	if peakPnL < mfeGivebackTriggerPct || e.Position.UnrealizedPnLPct < 0 {
		return false
	}
	giveback := peakPnL - e.Position.UnrealizedPnLPct
	if giveback < peakPnL*mfeGivebackCloseRatio {
		return false
	}
	return hasWeakShortTermMomentum(e.MarketData, e.Plan.Direction) ||
		hasWeakShortTermMomentum(e.BTCMarketData, e.Plan.Direction)
}

func hasWeakShortTermMomentum(data *market.Data, direction string) bool {
	if data == nil {
		return false
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	macdHist := 0.0
	if data.MidTermSeries15m != nil {
		macdHist = market.GetLastValue(data.MidTermSeries15m.MACDHist)
		ema20 := market.GetLastValue(data.MidTermSeries15m.EMA20Values)
		ema50 := market.GetLastValue(data.MidTermSeries15m.EMA50Values)
		if direction == "long" && ema20 > 0 && ema50 > 0 && ema20 < ema50 {
			return true
		}
		if direction == "short" && ema20 > 0 && ema50 > 0 && ema20 > ema50 {
			return true
		}
	}
	if data.IntradaySeries != nil && macdHist == 0 {
		macdHist = market.GetLastValue(data.IntradaySeries.MACDHist)
	}
	if direction == "long" {
		return macdHist < 0 || data.PriceChange1h < -0.5 || data.CurrentDIPlus < data.CurrentDIMinus
	}
	if direction == "short" {
		return macdHist > 0 || data.PriceChange1h > 0.5 || data.CurrentDIPlus > data.CurrentDIMinus
	}
	return false
}

// ============================================================================
// 利润保护
// ============================================================================

func (e *PositionEvaluator) checkProfitProtection(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	pnlPct := e.Position.UnrealizedPnLPct
	peakPnL := e.Plan.PeakPnLPercent

	if pnlPct > peakPnL {
		peakPnL = pnlPct
	}

	if peakPnL < config.ProfitProtectTrigger {
		return nil
	}

	protectLine := peakPnL * profitProtectRatioForPeak(peakPnL, config)

	if pnlPct < protectLine {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 峰值盈利%.2f%%, 当前%.2f%%, 保护线%.2f%%",
				peakPnL, pnlPct, protectLine),
			ShouldUpdatePeak: false,
		}
	}

	if peakPnL >= 10.0 && pnlPct < 2.0 {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 曾盈利%.2f%%，现仅剩%.2f%%",
				peakPnL, pnlPct),
			ShouldUpdatePeak: false,
		}
	}

	return nil
}

func profitProtectRatioForPeak(peakPnL float64, config *TakeProfitEngineConfig) float64 {
	ratio := basePeakProfitProtectRatio
	if config != nil && config.ProfitProtectRatio > ratio {
		ratio = config.ProfitProtectRatio
	}
	if peakPnL >= highPeakProfitProtectPct && ratio < highPeakProfitProtectRatio {
		ratio = highPeakProfitProtectRatio
	}
	return ratio
}

// ============================================================================
// ATR跟踪止盈
// ============================================================================

func (e *PositionEvaluator) evaluateATRTrailingTakeProfit(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentPrice := e.MarketData.CurrentPrice
	atr := e.getATR()
	trailingDistance := atr * config.ATRTrailingMult

	peakPrice := e.Plan.PeakPrice

	if e.Plan.Direction == "long" {
		if peakPrice == 0 || currentPrice > peakPrice {
			peakPrice = currentPrice
		}

		trailingTP := peakPrice - trailingDistance

		if trailingTP > e.Plan.EntryPrice && currentPrice <= trailingTP {
			profit := (trailingTP - e.Plan.EntryPrice) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📈 ATR跟踪止盈: 从高点%.4f回落%.4f, 锁定利润%.2f%%",
					peakPrice, trailingDistance, profit),
				ShouldUpdatePeak: false,
			}
		}
	} else {
		if peakPrice == 0 || currentPrice < peakPrice {
			peakPrice = currentPrice
		}

		trailingTP := peakPrice + trailingDistance

		if trailingTP < e.Plan.EntryPrice && currentPrice >= trailingTP {
			profit := (e.Plan.EntryPrice - trailingTP) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📉 ATR跟踪止盈: 从低点%.4f反弹%.4f, 锁定利润%.2f%%",
					peakPrice, trailingDistance, profit),
				ShouldUpdatePeak: false,
			}
		}
	}

	return nil
}

// ============================================================================
// 自适应分批止盈
// ============================================================================

func GetAdaptiveExitTranches(marketData *market.Data, direction string) []DynamicExitTranche {
	baseTranches := make([]DynamicExitTranche, len(defaultExitTranches))
	copy(baseTranches, defaultExitTranches)

	if marketData == nil {
		return baseTranches
	}

	adx := marketData.CurrentADX

	// 趋势强度调整
	trendMultiplier := 1.0
	if adx > 40 {
		trendMultiplier = 1.5
	} else if adx > 30 {
		trendMultiplier = 1.3
	} else if adx < 20 {
		trendMultiplier = 0.8
	}

	// 波动率调整
	volMultiplier := 1.0
	if marketData.LongerTermContext != nil {
		atr := marketData.LongerTermContext.ATR14
		atrPct := atr / marketData.CurrentPrice * 100
		if atrPct > 5 {
			volMultiplier = 1.2
		} else if atrPct < 2 {
			volMultiplier = 0.9
		}
	}

	// 趋势方向匹配调整
	directionBonus := 1.0
	if marketData.CurrentDIPlus > 0 && marketData.CurrentDIMinus > 0 {
		if direction == "long" && marketData.CurrentDIPlus > marketData.CurrentDIMinus*1.5 {
			directionBonus = 1.2
		} else if direction == "short" && marketData.CurrentDIMinus > marketData.CurrentDIPlus*1.5 {
			directionBonus = 1.2
		}
	}

	// 应用调整
	for i := range baseTranches {
		baseTranches[i].BaseRR = baseTranches[i].BaseRR * trendMultiplier * volMultiplier * directionBonus
		if baseTranches[i].BaseRR < 2.0 {
			baseTranches[i].BaseRR = 2.0
		}
	}

	return baseTranches
}

func (e *PositionEvaluator) evaluateAdaptiveScaledExit() *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentRR := e.calculateCurrentRR()
	holdHours := time.Since(e.Plan.CreatedAt).Hours()
	adx := e.MarketData.CurrentADX

	tranches := GetAdaptiveExitTranches(e.MarketData, e.Plan.Direction)

	for i, tranche := range tranches {
		if e.Plan.ExecutedTranches != nil && e.Plan.ExecutedTranches[i] {
			continue
		}

		if adx < tranche.MinADX && tranche.RequiresMomentum {
			continue
		}

		triggered := false
		triggerReason := ""

		if currentRR >= tranche.BaseRR {
			triggered = true
			triggerReason = fmt.Sprintf("RR达到%.2f", currentRR)
		} else if tranche.MaxHoldHours > 0 && holdHours >= tranche.MaxHoldHours && currentRR >= tranche.BaseRR*0.7 {
			triggered = true
			triggerReason = fmt.Sprintf("持仓%.0f小时+RR %.2f", holdHours, currentRR)
		}

		if !triggered {
			continue
		}

		if tranche.RequiresMomentum && !e.checkMomentumConfirmation() {
			log.Printf("📊 %s: %s但动量不足，等待", e.Symbol, triggerReason)
			continue
		}

		remainingPositionUSD := remainingPositionValueUSD(e.Plan)
		minOrderValue := minScaledExitOrderValueUSDT(e.Exchange, e.Symbol)
		if !canExecuteScaledExit(remainingPositionUSD, tranche.ClosePercent, minOrderValue) {
			log.Printf("📊 %s: %s但仓位%.2f USD过小，跳过%.0f%%分批止盈",
				e.Symbol, triggerReason, remainingPositionUSD, tranche.ClosePercent)
			continue
		}

		newStopLoss := e.calculateStopLossForTranche(tranche.MoveStopTo)

		return &EvaluationResult{
			Action:          "partial_close",
			ClosePercentage: tranche.ClosePercent,
			NewStopLoss:     newStopLoss,
			TrancheIndex:    i,
			Reason: fmt.Sprintf("📊 自适应止盈第%d档: %s, 平仓%.0f%%",
				i+1, triggerReason, tranche.ClosePercent),
			ShouldUpdatePeak: false,
		}
	}

	return nil
}

func remainingPositionValueUSD(plan *TradePlan) float64 {
	if plan == nil {
		return 0
	}
	positionUSD := plan.PositionSizeUSD
	if positionUSD <= 0 && plan.ActualQuantity > 0 && plan.ActualEntry > 0 {
		positionUSD = plan.ActualQuantity * plan.ActualEntry
	}
	if positionUSD <= 0 {
		return 0
	}
	closedPct := math.Max(0, math.Min(100, plan.TotalClosedPercent))
	return positionUSD * (1 - closedPct/100)
}

func canExecuteScaledExit(positionUSD, closePct, minOrderValueUSD float64) bool {
	if positionUSD <= 0 || closePct <= 0 || closePct > 100 {
		return false
	}
	if minOrderValueUSD <= 0 {
		minOrderValueUSD = defaultMinOrderValueUSDT
	}
	closeValue := positionUSD * closePct / 100
	remainingValue := positionUSD - closeValue
	return closeValue >= minOrderValueUSD && remainingValue >= minOrderValueUSD
}

func minScaledExitOrderValueUSDT(exchange, symbol string) float64 {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if exchange == "binance" {
		switch symbol {
		case "BTCUSDT":
			return 50
		case "ETHUSDT":
			return 20
		}
	}
	return defaultMinOrderValueUSDT
}

func (e *PositionEvaluator) checkMomentumConfirmation() bool {
	rsi := e.MarketData.CurrentRSI14
	if e.Plan.Direction == "long" {
		if rsi > 75 {
			return false
		}
	} else {
		if rsi < 25 {
			return false
		}
	}

	if e.MarketData.IntradaySeries != nil {
		macdHist := market.GetLastValue(e.MarketData.IntradaySeries.MACDHist)
		prevHist := e.getSecondLastMACDHist()

		if e.Plan.Direction == "long" {
			if macdHist < 0 || macdHist < prevHist {
				return false
			}
		} else {
			if macdHist > 0 || macdHist > prevHist {
				return false
			}
		}
	}

	if e.MarketData.CurrentADX < 20 {
		return false
	}

	return true
}

func (e *PositionEvaluator) getSecondLastMACDHist() float64 {
	if e.MarketData.IntradaySeries == nil {
		return 0
	}
	hist := e.MarketData.IntradaySeries.MACDHist
	if len(hist) < 2 {
		return 0
	}
	return hist[len(hist)-2]
}

func (e *PositionEvaluator) calculateStopLossForTranche(moveStopTo string) float64 {
	if e.Plan == nil {
		return 0
	}

	entryPrice := e.Plan.EntryPrice
	riskDistance := math.Abs(entryPrice - e.Plan.StopLoss)

	switch moveStopTo {
	case "breakeven":
		return entryPrice

	case "lock_1r":
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance
		}
		return entryPrice - riskDistance

	case "lock_2r":
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance*2
		}
		return entryPrice - riskDistance*2

	case "lock_3r":
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance*3
		}
		return entryPrice - riskDistance*3

	case "lock_4r":
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance*4
		}
		return entryPrice - riskDistance*4

	default:
		return e.getEffectiveStopLoss()
	}
}

// ============================================================================
// 改进的移动止损（续）
// ============================================================================

func (e *PositionEvaluator) evaluateTrailingStop() *EvaluationResult {
	newSL := e.calculateTrailingStopImproved(defaultTrailingConfig)

	if newSL <= 0 {
		return nil
	}

	effectiveSL := e.getEffectiveStopLoss()
	currentPrice := e.MarketData.CurrentPrice

	shouldUpdate := false

	if e.Plan.Direction == "long" {
		if newSL > effectiveSL && newSL < currentPrice {
			shouldUpdate = true
		}
	} else {
		if newSL < effectiveSL && newSL > currentPrice {
			shouldUpdate = true
		}
	}

	if shouldUpdate {
		return &EvaluationResult{
			Action:           "update_stop_loss",
			NewStopLoss:      newSL,
			Reason:           fmt.Sprintf("📈 移动止损: %.4f → %.4f (盈利%.2f%%)", effectiveSL, newSL, e.Position.UnrealizedPnLPct),
			ShouldUpdatePeak: true,
		}
	}

	return nil
}

func (e *PositionEvaluator) calculateTrailingStopImproved(config *TrailingStopConfig) float64 {
	if e.Plan == nil || e.MarketData == nil {
		return 0
	}
	if config == nil {
		config = defaultTrailingConfig
	}

	pnlPct := e.Position.UnrealizedPnLPct
	entryPrice := e.Plan.EntryPrice
	currentPrice := e.MarketData.CurrentPrice
	adx := e.MarketData.CurrentADX

	// 获取ATR用于动态计算
	atr := e.getATR()
	triggerGuard := e.calculateTriggerGuardDistance(config, atr, currentPrice)

	var newSL float64
	var reason string

	// 检查是否达到保本阈值
	currentRR := e.calculateCurrentRR()
	if pnlPct < config.BreakevenThreshold && currentRR < 1.0 {
		return 0 // 盈利不足，不移动止损
	}

	// 检查趋势强度
	if adx < 20 {
		log.Printf("⚠️ %s: ADX=%.1f 趋势较弱，暂不移动止损", e.Plan.Symbol, adx)
		return 0
	}

	// 遍历锁定利润阈值
	for i := len(config.LockProfitThresholds) - 1; i >= 0; i-- {
		level := config.LockProfitThresholds[i]
		if pnlPct >= level.PnLThreshold && adx >= level.RequireADXAbove {
			// 计算应锁定的利润
			profitToLock := pnlPct * level.LockPercent
			profitToLock = math.Max(profitToLock, config.MinProfitBuffer)

			if e.Plan.Direction == "long" {
				newSL = entryPrice * (1 + profitToLock/100)
			} else {
				newSL = entryPrice * (1 - profitToLock/100)
			}
			reason = fmt.Sprintf("锁定%.0f%%利润(盈利%.1f%%)", level.LockPercent*100, pnlPct)
			break
		}
	}

	// 如果没有匹配任何档位，检查是否应该保本
	if newSL == 0 && (pnlPct >= config.BreakevenThreshold || currentRR >= 1.0) {
		if e.Plan.Direction == "long" {
			newSL = entryPrice * 1.002 // 0.2%覆盖手续费
		} else {
			newSL = entryPrice * 0.998
		}
		reason = "移动到保本"
	}

	if newSL == 0 {
		return 0
	}
	targetSL := newSL

	// 触发保护距离只防止止损贴近当前价，不再用趋势容忍距离压制保本/锁利润止损。
	if e.Plan.Direction == "long" {
		maxAllowedSL := currentPrice - triggerGuard
		if newSL > maxAllowedSL {
			log.Printf("⚠️ %s: %s目标止损%.4f距离当前价过近，触发保护线%.4f(triggerGuard=%.4f)，调整为%.4f",
				e.Plan.Symbol, reason, newSL, maxAllowedSL, triggerGuard, maxAllowedSL)
			newSL = maxAllowedSL
		}
	} else {
		minAllowedSL := currentPrice + triggerGuard
		if newSL < minAllowedSL {
			log.Printf("⚠️ %s: %s目标止损%.4f距离当前价过近，触发保护线%.4f(triggerGuard=%.4f)，调整为%.4f",
				e.Plan.Symbol, reason, newSL, minAllowedSL, triggerGuard, minAllowedSL)
			newSL = minAllowedSL
		}
	}

	// 最终验证
	effectiveSL := e.getEffectiveStopLoss()
	if e.Plan.Direction == "long" {
		if newSL <= effectiveSL {
			log.Printf("⚠️ %s: 拒绝移动止损，目标%.4f/调整后%.4f 未高于当前止损%.4f (triggerGuard=%.4f)",
				e.Plan.Symbol, targetSL, newSL, effectiveSL, triggerGuard)
			return 0
		}
	} else {
		if newSL >= effectiveSL {
			log.Printf("⚠️ %s: 拒绝移动止损，目标%.4f/调整后%.4f 未低于当前止损%.4f (triggerGuard=%.4f)",
				e.Plan.Symbol, targetSL, newSL, effectiveSL, triggerGuard)
			return 0
		}
	}

	log.Printf("📈 %s: %s，目标止损=%.4f，新止损=%.4f (ATR=%.4f, triggerGuard=%.4f, ADX=%.1f)",
		e.Plan.Symbol, reason, targetSL, newSL, atr, triggerGuard, adx)

	return newSL
}

func (e *PositionEvaluator) calculateTriggerGuardDistance(config *TrailingStopConfig, atr, currentPrice float64) float64 {
	if config == nil {
		config = defaultTrailingConfig
	}

	minPct := config.TriggerGuardMinPct
	if minPct <= 0 {
		minPct = 0.005
	}
	atrMult := config.TriggerGuardATRMult
	if atrMult <= 0 {
		atrMult = 0.25
	}
	maxPct := config.TriggerGuardMaxPct
	if maxPct <= 0 {
		maxPct = 0.012
	}

	return math.Max(currentPrice*minPct, math.Min(atr*atrMult, currentPrice*maxPct))
}

func (e *PositionEvaluator) calculateTrendToleranceDistance(config *TrailingStopConfig, atr, currentPrice float64) float64 {
	if config == nil {
		config = defaultTrailingConfig
	}

	atrMult := config.TrendToleranceATRMult
	if atrMult <= 0 {
		atrMult = 1.2
	}
	minPct := config.TrendToleranceMinPct
	if minPct <= 0 {
		minPct = 0.012
	}

	return math.Max(atr*atrMult, currentPrice*minPct)
}

// ============================================================================
// 动态止盈调整
// ============================================================================

func (e *PositionEvaluator) calculateDynamicTakeProfit(config *TakeProfitEngineConfig) float64 {
	if e.Plan == nil {
		return 0
	}

	baseTP := e.Plan.OriginalTakeProfit
	if baseTP == 0 {
		baseTP = e.Plan.TakeProfit
	}
	entryPrice := e.Plan.EntryPrice

	// 获取市场状态调整因子
	marketState, confidence := market.GetMarketState(e.MarketData)
	stateMultiplier := e.getStateMultiplier(marketState, confidence)

	// 波动率调整因子
	currentATR := e.getATR()
	entryATR := e.Plan.EntryATR
	if entryATR == 0 {
		entryATR = currentATR
	}

	volAdjustment := 1.0
	if entryATR > 0 {
		volatilityRatio := currentATR / entryATR
		if volatilityRatio > 1.3 {
			volAdjustment = 1.2
		} else if volatilityRatio < 0.7 {
			volAdjustment = 0.85
		}
	}

	// 时间衰减因子
	timeAdjustment := 1.0
	holdHours := time.Since(e.Plan.CreatedAt).Hours()
	maxHoldHours := 72.0

	if holdHours > maxHoldHours {
		decayFactor := 1.0 - (holdHours-maxHoldHours)/maxHoldHours*0.3
		timeAdjustment = math.Max(0.7, decayFactor)
	}

	// 计算调整后的止盈价
	originalDistance := math.Abs(baseTP - entryPrice)
	adjustedDistance := originalDistance * stateMultiplier * volAdjustment * timeAdjustment

	var newTP float64
	if e.Plan.Direction == "long" {
		newTP = entryPrice + adjustedDistance
	} else {
		newTP = entryPrice - adjustedDistance
	}

	// 只有变化超过0.5%才返回新值
	if math.Abs(newTP-e.Plan.TakeProfit)/e.Plan.TakeProfit > 0.005 {
		return newTP
	}

	return 0
}

func (e *PositionEvaluator) getStateMultiplier(state string, confidence int) float64 {
	switch state {
	case "STRONG_UPTREND":
		if e.Plan.Direction == "long" && confidence >= 80 {
			return 1.5
		}
		return 1.2
	case "STRONG_DOWNTREND":
		if e.Plan.Direction == "short" && confidence >= 80 {
			return 1.5
		}
		return 1.2
	case "UPTREND":
		if e.Plan.Direction == "long" {
			return 1.3
		}
		return 0.9
	case "DOWNTREND":
		if e.Plan.Direction == "short" {
			return 1.3
		}
		return 0.9
	case "RANGING", "CONSOLIDATION":
		return 0.8
	default:
		return 1.0
	}
}

// ============================================================================
// 计划失效条件检查
// ============================================================================

func (e *PositionEvaluator) checkPlanInvalidation() (bool, string) {
	if e.MarketData == nil {
		return false, ""
	}

	// 检查结构化失效条件
	if e.Plan.ParsedInvalidationCondition != nil && e.Plan.ParsedInvalidationCondition.IsValid {
		if invalidated, reason := e.checkParsedInvalidationCondition(); invalidated {
			return true, reason
		}
	} else if e.Plan.InvalidationCondition != "" {
		parsed := ParseInvalidationCondition(e.Plan.InvalidationCondition)
		if parsed.IsValid {
			e.Plan.ParsedInvalidationCondition = parsed
			if invalidated, reason := e.checkParsedInvalidationCondition(); invalidated {
				return true, reason
			}
		}
	}

	// 默认的趋势反转检查
	if market.Is4HTrendReversed(e.MarketData, e.Plan.Direction) {
		adx, diPlus, diMinus := market.GetTrendInfo(e.MarketData)

		if e.Plan.Direction == "long" {
			return true, fmt.Sprintf("4H趋势反转(ADX=%.1f, DI-=%.1f > DI+=%.1f)，计划失效",
				adx, diMinus, diPlus)
		} else {
			return true, fmt.Sprintf("4H趋势反转(ADX=%.1f, DI+=%.1f > DI-=%.1f)，计划失效",
				adx, diPlus, diMinus)
		}
	}

	// EMA交叉检查
	if e.MarketData.LongerTermContext != nil {
		ctx := e.MarketData.LongerTermContext
		if e.Plan.Direction == "long" && ctx.EMA20 < ctx.EMA50 {
			return true, fmt.Sprintf("4H EMA死叉(EMA20=%.2f < EMA50=%.2f)，计划失效",
				ctx.EMA20, ctx.EMA50)
		}
		if e.Plan.Direction == "short" && ctx.EMA20 > ctx.EMA50 {
			return true, fmt.Sprintf("4H EMA金叉(EMA20=%.2f > EMA50=%.2f)，计划失效",
				ctx.EMA20, ctx.EMA50)
		}
	}

	// 价格失效线检查
	if e.Plan.InvalidationPrice > 0 {
		currentPrice := e.MarketData.CurrentPrice
		if e.Plan.Direction == "long" && currentPrice < e.Plan.InvalidationPrice {
			return true, fmt.Sprintf("价格跌破失效线(%.4f < %.4f)，计划失效",
				currentPrice, e.Plan.InvalidationPrice)
		}
		if e.Plan.Direction == "short" && currentPrice > e.Plan.InvalidationPrice {
			return true, fmt.Sprintf("价格突破失效线(%.4f > %.4f)，计划失效",
				currentPrice, e.Plan.InvalidationPrice)
		}
	}

	return false, ""
}

func (e *PositionEvaluator) checkParsedInvalidationCondition() (bool, string) {
	cond := e.Plan.ParsedInvalidationCondition
	if cond == nil || !cond.IsValid {
		return false, ""
	}

	ctx := e.getContextForTimeframe(cond.Timeframe)
	if ctx == nil {
		log.Printf("⚠️ 无法获取 %s 时间框架数据", cond.Timeframe)
		return false, ""
	}

	switch cond.Type {
	case ICT_EMA_CROSS_DOWN:
		return e.checkEMACrossDown(ctx, cond)
	case ICT_EMA_CROSS_UP:
		return e.checkEMACrossUp(ctx, cond)
	case ICT_PRICE_BELOW:
		return e.checkPriceBelow(ctx, cond)
	case ICT_PRICE_ABOVE:
		return e.checkPriceAbove(ctx, cond)
	case ICT_RSI_ABOVE:
		return e.checkRSIAbove(ctx, cond)
	case ICT_RSI_BELOW:
		return e.checkRSIBelow(ctx, cond)
	case ICT_ADX_BELOW:
		return e.checkADXBelow(ctx, cond)
	case ICT_MACD_CROSS:
		return e.checkMACDCross(ctx, cond)
	case ICT_TREND_REVERSAL:
		return e.checkTrendReversal(ctx, cond)
	default:
		return false, ""
	}
}

// getContextForTimeframe 获取指定时间框架的上下文
func (e *PositionEvaluator) getContextForTimeframe(timeframe string) *InvalidationCheckContext {
	if e.MarketData == nil {
		return nil
	}

	ctx := &InvalidationCheckContext{
		CurrentPrice: e.MarketData.CurrentPrice,
		Timeframe:    timeframe,
	}

	// 根据时间框架选择数据源
	switch strings.ToUpper(timeframe) {
	case "4H":
		if e.MarketData.LongerTermContext != nil {
			ltc := e.MarketData.LongerTermContext
			ctx.EMA20 = ltc.EMA20
			ctx.EMA50 = ltc.EMA50
			ctx.BBUpper = ltc.BollingerUpper
			ctx.BBLower = ltc.BollingerLower

			// 从切片获取最新值
			ctx.RSI14 = market.GetLastValue(ltc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(ltc.ADXValues)
			ctx.DIPlus = market.GetLastValue(ltc.DIPlus)
			ctx.DIMinus = market.GetLastValue(ltc.DIMinus)
			ctx.MACD = market.GetLastValue(ltc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(ltc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(ltc.MACDHist)
		}

	case "1H":
		if e.MarketData.MidTermSeries1h != nil {
			mtc := e.MarketData.MidTermSeries1h
			// 从切片获取最新值
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			// 注意：MidTermData1h 没有 DIPlus/DIMinus，使用顶层数据
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
		} else {
			// 回退到顶层 Current* 数据
			ctx.EMA20 = e.MarketData.CurrentEMA20
			ctx.EMA50 = e.MarketData.CurrentEMA50
			ctx.RSI14 = e.MarketData.CurrentRSI14
			ctx.ADX14 = e.MarketData.CurrentADX
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
			ctx.MACD = e.MarketData.CurrentMACD
		}

	case "15M", "30M":
		if e.MarketData.MidTermSeries15m != nil {
			mtc := e.MarketData.MidTermSeries15m
			// 从切片获取最新值
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			// MidTermData15m 没有 DIPlus/DIMinus，使用顶层数据
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
		} else {
			// 回退到顶层数据
			ctx.EMA20 = e.MarketData.CurrentEMA20
			ctx.EMA50 = e.MarketData.CurrentEMA50
			ctx.RSI14 = e.MarketData.CurrentRSI14
			ctx.ADX14 = e.MarketData.CurrentADX
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
			ctx.MACD = e.MarketData.CurrentMACD
		}

	default:
		// 默认使用顶层 Current* 数据（基于3分钟最新数据计算）
		ctx.EMA20 = e.MarketData.CurrentEMA20
		ctx.EMA50 = e.MarketData.CurrentEMA50
		ctx.RSI14 = e.MarketData.CurrentRSI14
		ctx.ADX14 = e.MarketData.CurrentADX
		ctx.DIPlus = e.MarketData.CurrentDIPlus
		ctx.DIMinus = e.MarketData.CurrentDIMinus
		ctx.MACD = e.MarketData.CurrentMACD
		// 尝试从 IntradaySeries 获取 MACD 信号线和柱状图
		if e.MarketData.IntradaySeries != nil {
			ctx.MACDSignal = market.GetLastValue(e.MarketData.IntradaySeries.MACDSignal)
			ctx.MACDHist = market.GetLastValue(e.MarketData.IntradaySeries.MACDHist)
		}
	}

	return ctx
}

// 失效条件检查方法
func (e *PositionEvaluator) checkEMACrossDown(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := e.getEMAValue(ctx, cond.Indicator)
	ema2 := e.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	if ema1 < ema2 && e.Plan.Direction == "long" {
		return true, fmt.Sprintf("%s EMA死叉: %s(%.4f) < %s(%.4f)，计划失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

func (e *PositionEvaluator) checkEMACrossUp(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := e.getEMAValue(ctx, cond.Indicator)
	ema2 := e.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	if ema1 > ema2 && e.Plan.Direction == "short" {
		return true, fmt.Sprintf("%s EMA金叉: %s(%.4f) > %s(%.4f)，计划失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

func (e *PositionEvaluator) checkPriceBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = e.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	if ctx.CurrentPrice < targetPrice && e.Plan.Direction == "long" {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)跌破%s(%.4f)，计划失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

func (e *PositionEvaluator) checkPriceAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = e.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	if ctx.CurrentPrice > targetPrice && e.Plan.Direction == "short" {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)突破%s(%.4f)，计划失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

func (e *PositionEvaluator) checkRSIAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 > cond.Threshold {
		if e.Plan.Direction == "long" && cond.Threshold >= 80 {
			return true, fmt.Sprintf("%s RSI(%.1f) > %.0f 极度超买，计划失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
	}

	return false, ""
}

func (e *PositionEvaluator) checkRSIBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 < cond.Threshold {
		if e.Plan.Direction == "short" && cond.Threshold <= 20 {
			return true, fmt.Sprintf("%s RSI(%.1f) < %.0f 极度超卖，计划失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
	}

	return false, ""
}

func (e *PositionEvaluator) checkADXBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.ADX14 < cond.Threshold {
		return true, fmt.Sprintf("%s ADX(%.1f) < %.0f 趋势减弱，计划失效",
			cond.Timeframe, ctx.ADX14, cond.Threshold)
	}

	return false, ""
}

func (e *PositionEvaluator) checkMACDCross(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.MACD == 0 && ctx.MACDSignal == 0 {
		return false, ""
	}

	if cond.Direction == "DOWN" {
		if ctx.MACD < ctx.MACDSignal && ctx.MACDHist < 0 {
			if e.Plan.Direction == "long" {
				return true, fmt.Sprintf("%s MACD死叉，计划失效", cond.Timeframe)
			}
		}
	} else if cond.Direction == "UP" {
		if ctx.MACD > ctx.MACDSignal && ctx.MACDHist > 0 {
			if e.Plan.Direction == "short" {
				return true, fmt.Sprintf("%s MACD金叉，计划失效", cond.Timeframe)
			}
		}
	}

	return false, ""
}

func (e *PositionEvaluator) checkTrendReversal(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 {
		return false, ""
	}

	if ctx.ADX14 > 25 {
		if e.Plan.Direction == "long" && ctx.DIMinus > ctx.DIPlus {
			return true, fmt.Sprintf("%s 趋势反转: DI-(%.1f) > DI+(%.1f)，计划失效",
				cond.Timeframe, ctx.DIMinus, ctx.DIPlus)
		}
		if e.Plan.Direction == "short" && ctx.DIPlus > ctx.DIMinus {
			return true, fmt.Sprintf("%s 趋势反转: DI+(%.1f) > DI-(%.1f)，计划失效",
				cond.Timeframe, ctx.DIPlus, ctx.DIMinus)
		}
	}

	return false, ""
}

func (e *PositionEvaluator) getEMAValue(ctx *InvalidationCheckContext, indicator string) float64 {
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	default:
		return 0
	}
}

func (e *PositionEvaluator) getIndicatorValue(ctx *InvalidationCheckContext, indicator string) float64 {
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	case "VWAP":
		return ctx.VWAP
	case "BB_UPPER":
		return ctx.BBUpper
	case "BB_LOWER":
		return ctx.BBLower
	default:
		return 0
	}
}
