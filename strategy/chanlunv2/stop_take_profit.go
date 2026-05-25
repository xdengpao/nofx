package chanlunv2

import (
	"math"
	"nofx/decision"
	"nofx/market"
	"strings"
)

func (e *Engine) applyV2StopTakeProfitFallback(ctx *decision.Context, d decision.Decision, sig Signal, mr *multiLevelResult, tradeTF string) decision.Decision {
	if !decision.IsOpenLikeAction(d.Action) {
		return d
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	if d.RequestedStopLoss <= 0 {
		d.RequestedStopLoss = d.StopLoss
	}
	if d.RequestedTakeProfit <= 0 {
		d.RequestedTakeProfit = d.TakeProfit
	}
	currentPrice := currentPriceForV2Guard(ctx, d.Symbol, tradeTF)
	if currentPrice <= 0 && sig.Price > 0 {
		currentPrice = sig.Price
	}
	if currentPrice <= 0 {
		d.StrategyMetadata["sl_tp_source"] = "invalid"
		d.StrategyMetadata["sl_tp_invalid_reason"] = "missing_current_price"
		return d
	}
	if validV2StopTakeProfit(d.Action, currentPrice, d.StopLoss, d.TakeProfit) {
		d.EffectiveStopLoss = d.StopLoss
		d.EffectiveTakeProfit = d.TakeProfit
		d.StrategyMetadata["sl_tp_source"] = "rust"
		return d
	}
	if stop, take, ok := centerV2StopTakeProfit(d.Action, currentPrice, sig, mr); ok {
		d.StopLoss = stop
		d.TakeProfit = take
		d.EffectiveStopLoss = stop
		d.EffectiveTakeProfit = take
		d.StructureTarget = take
		d.StrategyMetadata["sl_tp_source"] = "center"
		return d
	}
	if stop, take, atr, ok := atrV2StopTakeProfit(ctx, d.Symbol, d.Action, currentPrice, tradeTF); ok {
		d.StopLoss = stop
		d.TakeProfit = take
		d.EffectiveStopLoss = stop
		d.EffectiveTakeProfit = take
		d.StructureTarget = take
		d.StrategyMetadata["sl_tp_source"] = "atr"
		d.StrategyMetadata["sl_tp_atr"] = atr
		return d
	}
	d.StrategyMetadata["sl_tp_source"] = "invalid"
	d.StrategyMetadata["sl_tp_invalid_reason"] = "missing_center_and_atr"
	return d
}

func validV2StopTakeProfit(action string, currentPrice, stopLoss, takeProfit float64) bool {
	if currentPrice <= 0 || stopLoss <= 0 || takeProfit <= 0 {
		return false
	}
	return !invalidStopTakeProfit(action, currentPrice, stopLoss, takeProfit)
}

func centerV2StopTakeProfit(action string, currentPrice float64, sig Signal, mr *multiLevelResult) (float64, float64, bool) {
	center, ok := findV2SignalCenter(sig, mr)
	if !ok {
		return 0, 0, false
	}
	direction := decision.DecisionDirection(action)
	riskReward := 2.5
	switch direction {
	case "long":
		stop := firstPositiveFloat(sig.StopLoss, center.ZD, center.Low)
		if stop <= 0 || stop >= currentPrice {
			return 0, 0, false
		}
		take := sig.TakeProfit
		if take <= currentPrice {
			take = currentPrice + (currentPrice-stop)*riskReward
		}
		if validV2StopTakeProfit(action, currentPrice, stop, take) {
			return stop, take, true
		}
	case "short":
		stop := firstPositiveFloat(sig.StopLoss, center.ZG, center.High)
		if stop <= 0 || stop <= currentPrice {
			return 0, 0, false
		}
		take := sig.TakeProfit
		if take <= 0 || take >= currentPrice {
			take = currentPrice - (stop-currentPrice)*riskReward
		}
		if validV2StopTakeProfit(action, currentPrice, stop, take) {
			return stop, take, true
		}
	}
	return 0, 0, false
}

func findV2SignalCenter(sig Signal, mr *multiLevelResult) (Center, bool) {
	if sig.CenterID == nil || mr == nil {
		return Center{}, false
	}
	tradeResult := mr.Results["trade"]
	if tradeResult == nil {
		return Center{}, false
	}
	for _, center := range tradeResult.Centers {
		if center.ID == *sig.CenterID {
			return center, true
		}
	}
	return Center{}, false
}

func atrV2StopTakeProfit(ctx *decision.Context, symbol, action string, currentPrice float64, tradeTF string) (float64, float64, float64, bool) {
	data := marketDataForV2(ctx, symbol)
	if data == nil {
		return 0, 0, 0, false
	}
	atr := market.GetATR(data, tradeTF)
	if atr <= 0 {
		klines := normalizeMarketKlines(data.Klines[tradeTF])
		atr = calculateSimpleATR(klines, 14)
	}
	if atr <= 0 || currentPrice <= 0 {
		return 0, 0, 0, false
	}
	stopMultiplier := 2.0
	takeMultiplier := 3.0
	switch strings.ToLower(decision.DecisionDirection(action)) {
	case "long":
		stop := currentPrice - atr*stopMultiplier
		take := currentPrice + atr*takeMultiplier
		if validV2StopTakeProfit(action, currentPrice, stop, take) {
			return stop, take, atr, true
		}
	case "short":
		stop := currentPrice + atr*stopMultiplier
		take := currentPrice - atr*takeMultiplier
		if validV2StopTakeProfit(action, currentPrice, stop, take) {
			return stop, take, atr, true
		}
	}
	return 0, 0, 0, false
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value
		}
	}
	return 0
}
