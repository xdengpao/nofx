package decision

import (
	"fmt"
	"math"
	"nofx/market"
	"strings"
)

const (
	ExchangeFullTPModeAlgorithmicFull = "algorithmic_full"
	ExchangeFullTPModeLegacyAI        = "legacy_ai"
	ExchangeFullTPModeFinalRTarget    = "final_r_target"

	defaultStrategyRiskFeeSlippage = 0.002
	defaultStrategyRiskMinNetRR    = 2.5
	defaultStrategyRiskMinStop     = 0.01
)

// StrategyRiskActive 判断新策略硬约束是否参与实盘验证。
func StrategyRiskActive(policy *StrategyRiskPolicy) bool {
	return policy != nil && !policy.Legacy && policy.Enabled && !policy.RollbackLegacyValidation
}

// ResolveInstrumentProfile 为 symbol 选择最保守可用 profile。
func ResolveInstrumentProfile(symbol string, policy *StrategyRiskPolicy) InstrumentProfile {
	profiles := defaultDecisionStrategyProfiles()
	if policy != nil && len(policy.Profiles) > 0 {
		profiles = policy.Profiles
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	var fallback InstrumentProfile
	for _, profile := range profiles {
		if profile.Name == "default" {
			fallback = profile
		}
		for _, s := range profile.Symbols {
			if strings.EqualFold(strings.TrimSpace(s), symbol) {
				return normalizeInstrumentProfileDefaults(profile, policy)
			}
		}
	}
	for _, profile := range profiles {
		if profile.Name == "default" {
			continue
		}
		if profileMatchesSymbol(profile, symbol) {
			return normalizeInstrumentProfileDefaults(profile, policy)
		}
	}
	if fallback.Name == "" && len(profiles) > 0 {
		fallback = profiles[len(profiles)-1]
	}
	return normalizeInstrumentProfileDefaults(fallback, policy)
}

func profileMatchesSymbol(profile InstrumentProfile, symbol string) bool {
	if profile.MatchQuote != "" && strings.HasSuffix(symbol, strings.ToUpper(strings.TrimSpace(profile.MatchQuote))) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(profile.MatchType)) {
	case "btc_eth":
		return symbol == "BTCUSDT" || symbol == "ETHUSDT"
	case "major_alt":
		switch symbol {
		case "SOLUSDT", "BNBUSDT", "BCHUSDT", "XRPUSDT", "LTCUSDT", "ADAUSDT":
			return true
		}
	case "high_beta_alt":
		if symbol == "BTCUSDT" || symbol == "ETHUSDT" {
			return false
		}
		if strings.HasSuffix(symbol, "USDT") {
			switch symbol {
			case "SOLUSDT", "BNBUSDT", "BCHUSDT", "XRPUSDT", "LTCUSDT", "ADAUSDT":
				return false
			default:
				return true
			}
		}
	case "non_crypto":
		return strings.HasPrefix(symbol, "XAG")
	}
	return false
}

// NormalizeOpenDecisionRisk 将 AI 开仓参数改写到 profile 允许的 SL/TP/整仓 TP 距离。
func NormalizeOpenDecisionRisk(d *Decision, ctx *Context, data *market.Data) (*OpenRiskNormalization, error) {
	if d == nil {
		return nil, fmt.Errorf("缺少开仓决策")
	}
	if data == nil || data.CurrentPrice <= 0 {
		return nil, fmt.Errorf("缺少有效市场价格")
	}
	if ctx == nil || !StrategyRiskActive(ctx.StrategyRiskPolicy) {
		return nil, nil
	}
	if !IsOpenLikeAction(d.Action) {
		return nil, nil
	}
	direction := DecisionDirection(d.Action)

	policy := ctx.StrategyRiskPolicy
	profile := ResolveInstrumentProfile(d.Symbol, policy)
	if profile.Name == "" {
		return nil, fmt.Errorf("%s 未匹配到有效策略profile", d.Symbol)
	}
	if direction == "long" && !profile.AllowLong {
		return nil, fmt.Errorf("%s profile %s 禁止做多", d.Symbol, profile.Name)
	}
	if direction == "short" && !profile.AllowShort {
		return nil, fmt.Errorf("%s profile %s 禁止做空", d.Symbol, profile.Name)
	}

	currentPrice := data.CurrentPrice
	atr := strategyATR(data, profile.ATRTimeframe)
	minStopDistance := currentPrice * profile.MinStopPct
	degradedATR := false
	if atr <= 0 || profile.ATRMultiplier <= 0 {
		minStopDistance = currentPrice * profile.FallbackStopPct
		degradedATR = true
	} else {
		minStopDistance = math.Max(atr*profile.ATRMultiplier, minStopDistance)
	}
	if minStopDistance <= 0 {
		minStopDistance = currentPrice * defaultStrategyRiskMinStop
		degradedATR = true
	}

	requestedSL := d.StopLoss
	requestedTP := d.TakeProfit
	effectiveSL := requestedSL
	rewrittenSL := false
	if direction == "long" {
		minSL := currentPrice - minStopDistance
		if effectiveSL <= 0 || effectiveSL >= currentPrice || effectiveSL > minSL {
			effectiveSL = minSL
			rewrittenSL = true
		}
		if effectiveSL <= 0 || effectiveSL >= currentPrice {
			return nil, fmt.Errorf("%s 规范化后做多止损无效: %.8f", d.Symbol, effectiveSL)
		}
	} else {
		minSL := currentPrice + minStopDistance
		if effectiveSL <= 0 || effectiveSL <= currentPrice || effectiveSL < minSL {
			effectiveSL = minSL
			rewrittenSL = true
		}
		if effectiveSL <= currentPrice {
			return nil, fmt.Errorf("%s 规范化后做空止损无效: %.8f", d.Symbol, effectiveSL)
		}
	}

	stopDistanceRatio := math.Abs(currentPrice-effectiveSL) / currentPrice
	minNetRR := profile.MinNetRR
	if minNetRR <= 0 {
		minNetRR = policy.DefaultMinNetRR
	}
	if minNetRR <= 0 {
		minNetRR = defaultStrategyRiskMinNetRR
	}
	feeSlippage := policy.FeeSlippagePct
	if feeSlippage <= 0 {
		feeSlippage = defaultStrategyRiskFeeSlippage
	}
	minTPRatio := stopDistanceRatio*minNetRR + feeSlippage
	effectiveTP := requestedTP
	rewrittenTP := false
	if direction == "long" {
		minTP := currentPrice * (1 + minTPRatio)
		if effectiveTP <= currentPrice || effectiveTP < minTP {
			effectiveTP = minTP
			rewrittenTP = true
		}
	} else {
		minTP := currentPrice * (1 - minTPRatio)
		if effectiveTP <= 0 || effectiveTP >= currentPrice || effectiveTP > minTP {
			effectiveTP = minTP
			rewrittenTP = true
		}
	}
	if effectiveTP <= 0 {
		return nil, fmt.Errorf("%s 规范化后止盈无效: %.8f", d.Symbol, effectiveTP)
	}

	tpRatio := math.Abs(effectiveTP-currentPrice) / currentPrice
	fullTPMinRR := profile.ExchangeFullTPMinRR
	if fullTPMinRR < minNetRR {
		fullTPMinRR = minNetRR
	}
	exchangeFullRatio := math.Max(tpRatio, stopDistanceRatio*fullTPMinRR+feeSlippage)
	exchangeFullTP := effectiveTP
	if direction == "long" {
		exchangeFullTP = currentPrice * (1 + exchangeFullRatio)
	} else {
		exchangeFullTP = currentPrice * (1 - exchangeFullRatio)
	}
	rewrittenFullTP := math.Abs(exchangeFullTP-requestedTP) > currentPrice*0.0000001
	finalTPRatio := math.Abs(exchangeFullTP-currentPrice) / currentPrice
	netRR := (finalTPRatio - feeSlippage) / stopDistanceRatio

	norm := &OpenRiskNormalization{
		ProfileName:             profile.Name,
		ATRTimeframe:            profile.ATRTimeframe,
		ATRValue:                atr,
		RequestedStopLoss:       requestedSL,
		RequestedTakeProfit:     requestedTP,
		EffectiveStopLoss:       effectiveSL,
		EffectiveTakeProfit:     effectiveTP,
		ExchangeFullTakeProfit:  exchangeFullTP,
		ExchangeFullTPMode:      profile.ExchangeFullTPMode,
		StopDistanceRatio:       stopDistanceRatio,
		StopDistancePct:         stopDistanceRatio,
		StopDistancePercent:     stopDistanceRatio * 100,
		MinStopDistanceRatio:    minStopDistance / currentPrice,
		MinTakeProfitRatio:      minTPRatio,
		MinNetRR:                minNetRR,
		TakeProfitRatio:         finalTPRatio,
		TakeProfitPercent:       finalTPRatio * 100,
		NetRR:                   netRR,
		RewrittenStop:           rewrittenSL,
		RewrittenTakeProfit:     rewrittenTP,
		RewrittenExchangeFullTP: rewrittenFullTP,
		DegradedATR:             degradedATR,
	}
	if rewrittenSL {
		norm.Reasons = append(norm.Reasons, "止损已按ATR/硬地板重写")
	}
	if rewrittenTP {
		norm.Reasons = append(norm.Reasons, "止盈已按最小净RR重写")
	}
	if rewrittenFullTP {
		norm.Reasons = append(norm.Reasons, "整仓交易所TP已按algorithmic_full重写")
	}
	if degradedATR {
		norm.Reasons = append(norm.Reasons, "ATR缺失，使用profile fallback止损")
	}

	d.RiskNormalization = norm
	d.ProfileName = profile.Name
	d.RequestedStopLoss = requestedSL
	d.RequestedTakeProfit = requestedTP
	d.StopLoss = effectiveSL
	d.EffectiveStopLoss = effectiveSL
	d.EffectiveTakeProfit = effectiveTP
	d.ExchangeFullTakeProfit = exchangeFullTP
	d.ExchangeFullTPMode = profile.ExchangeFullTPMode
	d.TakeProfit = exchangeFullTP
	d.StopDistancePct = stopDistanceRatio
	d.StopDistanceRatio = stopDistanceRatio
	d.StopDistancePercent = stopDistanceRatio * 100
	d.TakeProfitRatio = finalTPRatio
	d.TakeProfitPercent = finalTPRatio * 100
	d.NetRR = netRR
	return norm, nil
}

func normalizeInstrumentProfileDefaults(profile InstrumentProfile, policy *StrategyRiskPolicy) InstrumentProfile {
	if profile.Name == "" {
		profile = defaultDecisionStrategyProfiles()[len(defaultDecisionStrategyProfiles())-1]
	}
	if profile.MinStopPct <= 0 {
		profile.MinStopPct = defaultStrategyRiskMinStop
	}
	if profile.FallbackStopPct < profile.MinStopPct {
		profile.FallbackStopPct = profile.MinStopPct
	}
	if profile.ATRMultiplier <= 0 {
		profile.ATRMultiplier = 2
	}
	if profile.ATRTimeframe == "" {
		profile.ATRTimeframe = "1h"
	}
	if profile.MinNetRR <= 0 {
		profile.MinNetRR = defaultStrategyRiskMinNetRR
		if policy != nil && policy.DefaultMinNetRR > 0 {
			profile.MinNetRR = policy.DefaultMinNetRR
		}
	}
	if profile.MaxRiskPct <= 0 {
		profile.MaxRiskPct = 0.005
	}
	if profile.MinADX <= 0 {
		profile.MinADX = 25
	}
	if profile.MaxSameSideHighCorr <= 0 {
		profile.MaxSameSideHighCorr = 1
	}
	if profile.MinOrderValueUSDT <= 0 {
		profile.MinOrderValueUSDT = 10
	}
	if profile.ExchangeFullTPMode == "" {
		profile.ExchangeFullTPMode = ExchangeFullTPModeAlgorithmicFull
	}
	if profile.ExchangeFullTPMinRR < profile.MinNetRR {
		profile.ExchangeFullTPMinRR = profile.MinNetRR
	}
	return profile
}

func defaultDecisionStrategyProfiles() []InstrumentProfile {
	return []InstrumentProfile{
		{Name: "default", MatchType: "default", MinStopPct: 0.02, FallbackStopPct: 0.025, ATRMultiplier: 2.5, ATRTimeframe: "1h", MinNetRR: defaultStrategyRiskMinNetRR, MaxRiskPct: 0.005, MinADX: 25, AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 1, MaxSameSideLossPct: 0.02, MinOrderValueUSDT: 10, ExchangeFullTPMode: ExchangeFullTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultStrategyRiskMinNetRR},
	}
}

func strategyATR(data *market.Data, timeframe string) float64 {
	return market.GetATR(data, timeframe)
}
