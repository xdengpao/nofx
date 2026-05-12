package decision

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"strings"
	"time"
)

const (
	highBetaLongMaxSameSidePositions = 2
	losingSameSideBlockPnLPct        = -4.0
	extremeADX                       = 60.0
	elevatedADX                      = 50.0
	highADXRiskMultiplier            = 0.5
	longBaseMinConfidence            = 78
	shortBaseMinConfidence           = 82
	rangeLongMinConfidence           = 82
	rangeShortMinConfidence          = 85
	counterTrendMinConfidence        = 88
	btcVolatilityMinConfidence       = 85
	highADXMinConfidence             = 90
	btcConflictMinConfidence         = 88
	btcHighVolatilityBollingerPct    = 12.0
)

// OpenGateInput 是开仓准入评估的输入。
type OpenGateInput struct {
	Decision         *Decision
	Context          *Context
	MarketData       *market.Data
	ExistingRisk     float64
	ExecutionQuality *logger.ExecutionQualityStats
}

// OpenGateResult 是结构化开仓准入结果。
type OpenGateResult struct {
	Allowed         bool           `json:"allowed"`
	State           string         `json:"state"` // allow, penalize, block
	EffectiveRisk   float64        `json:"effective_risk"`
	MinConfidence   int            `json:"min_confidence,omitempty"`
	AdjustedSizeUSD float64        `json:"adjusted_size_usd,omitempty"`
	Reasons         []string       `json:"reasons,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
	Diagnostics     map[string]any `json:"diagnostics,omitempty"`
}

// EvaluateOpenGate 汇总当前行情、相关性、执行质量和 AI backoff gate。
func EvaluateOpenGate(input OpenGateInput) OpenGateResult {
	result := OpenGateResult{
		Allowed:       true,
		State:         "allow",
		EffectiveRisk: 0.02,
	}
	if input.Decision == nil {
		result.block("缺少开仓决策")
		return result
	}
	ctx := input.Context
	if ctx == nil {
		result.block("缺少交易上下文")
		return result
	}

	result.EffectiveRisk = baseOpenGateRisk(ctx)
	result.AdjustedSizeUSD = input.Decision.PositionSizeUSD

	if !ctx.AIBackoffUntil.IsZero() && time.Now().Before(ctx.AIBackoffUntil) {
		result.block(fmt.Sprintf("AI调用退避中，直到 %s", ctx.AIBackoffUntil.Format(time.RFC3339)))
	}

	applyDirectionalConfidenceGate(&result, input.Decision, input.MarketData)
	applyBTCMarketGate(&result, ctx)
	applyBTCMultiTimeframeGate(&result, input.Decision, ctx)
	applySameSideExposureGate(&result, input.Decision, ctx)
	applyCorrelationConcentrationGate(&result, input.Decision, ctx)
	applyHighADXChaseGate(&result, input.Decision, input.MarketData)
	applyExecutionQualityGate(&result, input.ExecutionQuality)

	if result.AdjustedSizeUSD <= 0 {
		result.AdjustedSizeUSD = input.Decision.PositionSizeUSD
	}
	return result
}

func baseOpenGateRisk(ctx *Context) float64 {
	maxRisk := ctx.MaxRiskPerTrade
	if ctx.EffectiveMaxRiskPerTrade > 0 && (maxRisk == 0 || ctx.EffectiveMaxRiskPerTrade < maxRisk) {
		maxRisk = ctx.EffectiveMaxRiskPerTrade
	}
	if maxRisk <= 0 {
		maxRisk = 0.02
	}
	return maxRisk
}

func applyDirectionalConfidenceGate(result *OpenGateResult, d *Decision, data *market.Data) {
	if d == nil {
		return
	}
	switch d.Action {
	case "open_long":
		result.requireMinConfidence(longBaseMinConfidence, "多单基础置信度要求")
	case "open_short":
		result.requireMinConfidence(shortBaseMinConfidence, "空单基础置信度要求")
	default:
		return
	}
	if data == nil {
		return
	}

	state, _ := market.GetMarketState(data)
	switch d.Action {
	case "open_long":
		switch state {
		case "RANGING", "SQUEEZE":
			result.penalize("标的处于震荡/波动收缩，多单需更高置信度")
			result.requireMinConfidence(rangeLongMinConfidence, "震荡区间多单置信度要求")
		case "WEAK_DOWNTREND", "STRONG_DOWNTREND":
			result.penalize("标的处于下行结构，多单属于逆势")
			result.requireMinConfidence(counterTrendMinConfidence, "逆势多单置信度要求")
		}
	case "open_short":
		switch state {
		case "RANGING", "SQUEEZE":
			result.penalize("标的处于震荡/波动收缩，空单需更高置信度")
			result.requireMinConfidence(rangeShortMinConfidence, "震荡区间空单置信度要求")
		case "WEAK_UPTREND", "STRONG_UPTREND":
			result.penalize("标的处于上行结构，空单属于逆势")
			result.requireMinConfidence(counterTrendMinConfidence, "逆势空单置信度要求")
		}
	}
}

func applyRollingPerformanceGate(result *OpenGateResult, d *Decision, ctx *Context) {
	if ctx.PerformanceGates == nil {
		return
	}
	side := "long"
	if d.Action == "open_short" {
		side = "short"
	}
	applyGate := func(g logger.PerformanceGate) {
		if g.State == "" || g.State == "allow" {
			return
		}
		reason := g.Reason
		if reason == "" {
			reason = fmt.Sprintf("%s rolling performance gate", g.Scope)
		}
		result.addDiagnostics("rolling_"+g.Scope+"_"+g.Key, map[string]any{
			"scope":           g.Scope,
			"key":             g.Key,
			"state":           g.State,
			"trade_count":     g.TradeCount,
			"total_pn_l":      g.TotalPnL,
			"win_rate":        g.WinRate,
			"profit_factor":   g.ProfitFactor,
			"min_confidence":  g.MinConfidence,
			"risk_multiplier": g.RiskMultiplier,
			"cooldown_until":  g.CooldownUntil,
			"reason":          reason,
		})
		if g.State == "block" && (g.CooldownUntil.IsZero() || time.Now().Before(g.CooldownUntil)) {
			result.block(reason)
			return
		}
		result.penalize(reason)
		if g.MinConfidence > result.MinConfidence {
			result.MinConfidence = g.MinConfidence
		}
		if g.RiskMultiplier > 0 && g.RiskMultiplier < 1 {
			result.EffectiveRisk *= g.RiskMultiplier
		}
	}

	if g, ok := ctx.PerformanceGates.SymbolGates[d.Symbol]; ok {
		applyGate(g)
	}
	if g, ok := ctx.PerformanceGates.SideGates[side]; ok {
		applyGate(g)
	}
	applyGate(ctx.PerformanceGates.GlobalGate)
}

func applyBTCMarketGate(result *OpenGateResult, ctx *Context) {
	if ctx.MarketDataMap == nil {
		return
	}
	btcData := ctx.MarketDataMap["BTCUSDT"]
	if btcData == nil {
		return
	}
	if btcData.PriceChange1h <= -5 {
		result.block(fmt.Sprintf("BTC 1小时跌幅 %.2f%%，禁止新开仓", btcData.PriceChange1h))
		return
	}
	if btcData.PriceChange1h <= -3 || btcData.PriceChange4h <= -7 || btcData.BollingerWidth >= btcHighVolatilityBollingerPct {
		result.penalize("BTC波动或跌幅偏高，新开仓降权")
		result.requireMinConfidence(btcVolatilityMinConfidence, "BTC波动环境置信度要求")
		result.EffectiveRisk *= 0.5
	}
}

func applyBTCMultiTimeframeGate(result *OpenGateResult, d *Decision, ctx *Context) {
	if d.Action != "open_long" || !isHighBetaAltcoin(d.Symbol) || ctx.MarketDataMap == nil {
		return
	}
	btcData := ctx.MarketDataMap["BTCUSDT"]
	if btcData == nil {
		return
	}
	diagnostics := buildBTCGateDiagnostics(btcData)
	if isConfirmedBTCBearishStructure(btcData) {
		result.blockWithDiagnostics("BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单", "btc", diagnostics)
		return
	}
	if isBearishStructure(btcData) {
		result.penalizeWithDiagnostics("BTC 1h/4h 存在转弱信号，高 beta 山寨多单降权", "btc", diagnostics)
		result.requireMinConfidence(btcConflictMinConfidence, "BTC转弱时高 beta 多单置信度要求")
		result.EffectiveRisk *= 0.5
		return
	}
	if hasBTCMultiTimeframeConflict(btcData) {
		result.penalizeWithDiagnostics("BTC 15m 与 1h/4h 趋势冲突，高 beta 山寨多单降权", "btc", diagnostics)
		result.requireMinConfidence(btcConflictMinConfidence, "BTC多周期冲突时高 beta 多单置信度要求")
		result.EffectiveRisk *= 0.5
	}
}

func applySameSideExposureGate(result *OpenGateResult, d *Decision, ctx *Context) {
	side := decisionSide(d.Action)
	if side == "" {
		return
	}
	sameSidePositions := 0
	sameSideHighBetaPositions := 0
	for _, pos := range ctx.Positions {
		posSide := normalizePositionSide(pos.Side)
		if posSide != side {
			continue
		}
		if pos.UnrealizedPnLPct <= losingSameSideBlockPnLPct {
			result.block(fmt.Sprintf("已有同向持仓 %s 浮亏 %.2f%%，禁止继续加同向仓", pos.Symbol, pos.UnrealizedPnLPct))
			return
		}
		sameSidePositions++
		if isHighBetaAltcoin(pos.Symbol) {
			sameSideHighBetaPositions++
		}
	}

	if d.Action != "open_long" || !isHighBetaAltcoin(d.Symbol) {
		return
	}
	if sameSidePositions >= highBetaLongMaxSameSidePositions {
		result.block("已有2个及以上同向多单，禁止继续叠加高 beta 多单")
		return
	}
	if sameSideHighBetaPositions >= 1 {
		result.penalize("已有同向高 beta 多单，新开仓风险减半")
		result.EffectiveRisk *= 0.5
	}
}

func applyCorrelationConcentrationGate(result *OpenGateResult, d *Decision, ctx *Context) {
	targetCorr, ok := ctx.CorrelationMap[d.Symbol]
	if !ok || !targetCorr.IsHighCorr {
		return
	}
	side := "long"
	if d.Action == "open_short" {
		side = "short"
	}
	sameSideHighCorr := 0
	for _, pos := range ctx.Positions {
		if normalizePositionSide(pos.Side) != side {
			continue
		}
		if corr, ok := ctx.CorrelationMap[pos.Symbol]; ok && corr.IsHighCorr {
			sameSideHighCorr++
		}
	}
	if sameSideHighCorr >= 2 {
		result.block("已有同向高相关持仓集中，禁止继续叠加风险")
		return
	}
	if sameSideHighCorr == 1 {
		result.penalize("已有同向高相关持仓，新开仓降权")
		result.EffectiveRisk *= 0.5
	}
}

func applyHighADXChaseGate(result *OpenGateResult, d *Decision, data *market.Data) {
	if d.Action != "open_long" || !isHighBetaAltcoin(d.Symbol) || data == nil {
		return
	}
	if data.CurrentADX > extremeADX {
		isExtended := data.PriceChange1h >= 1.5 || data.PriceChange4h >= 4
		if isExtended && !hasPullbackConfirmationForLong(data) {
			result.block(fmt.Sprintf("%s ADX %.1f 且短期涨幅过大，缺少回踩确认，拒绝追高", d.Symbol, data.CurrentADX))
			return
		}
	}
	if data.CurrentADX > elevatedADX {
		result.penalize(fmt.Sprintf("%s ADX %.1f 偏高，按趋势末端追入风险降权", d.Symbol, data.CurrentADX))
		result.requireMinConfidence(highADXMinConfidence, "高ADX追入置信度要求")
		result.EffectiveRisk *= highADXRiskMultiplier
	}
}

func applyExecutionQualityGate(result *OpenGateResult, quality *logger.ExecutionQualityStats) {
	if quality == nil {
		return
	}
	if quality.HighRiskExecutionFailures > 0 || quality.ProtectionOrderFailures > 0 {
		result.block("近期存在保护单或高危执行失败，暂停新开仓")
		return
	}
	if quality.PartialCloseFailureRate >= 50 && quality.PartialCloseAttempts >= 3 {
		result.penalize("partial_close失败率偏高，新开仓降权")
		result.EffectiveRisk *= 0.5
	}
	if quality.AIFailureCount >= 3 {
		result.penalize("AI失败次数偏高，新开仓降权")
		result.EffectiveRisk *= 0.5
	}
}

func (result *OpenGateResult) block(reason string) {
	result.Allowed = false
	result.State = "block"
	if reason != "" {
		result.Reasons = appendUniqueReason(result.Reasons, reason)
	}
}

func (result *OpenGateResult) blockWithDiagnostics(reason, key string, diagnostics map[string]any) {
	result.block(reason)
	result.addDiagnostics(key, diagnostics)
}

func (result *OpenGateResult) penalize(reason string) {
	if result.State == "" || result.State == "allow" {
		result.State = "penalize"
	}
	if reason != "" {
		result.Reasons = appendUniqueReason(result.Reasons, reason)
	}
}

func (result *OpenGateResult) penalizeWithDiagnostics(reason, key string, diagnostics map[string]any) {
	result.penalize(reason)
	result.addDiagnostics(key, diagnostics)
}

func (result *OpenGateResult) requireMinConfidence(min int, reason string) {
	if min > result.MinConfidence {
		result.MinConfidence = min
	}
	if reason != "" {
		result.Warnings = appendUniqueReason(result.Warnings, reason)
	}
}

func (result *OpenGateResult) addDiagnostics(key string, diagnostics map[string]any) {
	if key == "" || len(diagnostics) == 0 {
		return
	}
	if result.Diagnostics == nil {
		result.Diagnostics = make(map[string]any)
	}
	result.Diagnostics[key] = diagnostics
}

func appendUniqueReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func isHighBetaAltcoin(symbol string) bool {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	return symbol != "" && symbol != "BTCUSDT" && symbol != "ETHUSDT"
}

func normalizePositionSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long", "buy":
		return "long"
	case "short", "sell":
		return "short"
	default:
		return strings.ToLower(strings.TrimSpace(side))
	}
}

func decisionSide(action string) string {
	switch action {
	case "open_long":
		return "long"
	case "open_short":
		return "short"
	default:
		return ""
	}
}

func isBearishStructure(data *market.Data) bool {
	if data == nil {
		return false
	}
	if data.CurrentDIPlus > 0 && data.CurrentDIMinus > 0 && data.CurrentDIPlus < data.CurrentDIMinus &&
		data.LongerTermContext != nil && data.LongerTermContext.EMA20 > 0 && data.CurrentPrice < data.LongerTermContext.EMA20 {
		return true
	}
	if data.LongerTermContext != nil {
		macdHist := lastFloat(data.LongerTermContext.MACDHist)
		if macdHist < 0 && data.LongerTermContext.EMA50 > 0 && data.CurrentPrice < data.LongerTermContext.EMA50 {
			return true
		}
	}
	if data.MidTermSeries1h != nil {
		ema20 := lastFloat(data.MidTermSeries1h.EMA20Values)
		ema50 := lastFloat(data.MidTermSeries1h.EMA50Values)
		if ema20 > 0 && ema50 > 0 && ema20 < ema50 {
			return true
		}
		macdHist := lastFloat(data.MidTermSeries1h.MACDHist)
		if macdHist < 0 && data.PriceChange1h < 0 {
			return true
		}
	}
	return false
}

func isConfirmedBTCBearishStructure(data *market.Data) bool {
	if data == nil {
		return false
	}
	fourHBearish := isBTCFourHourBearish(data)
	oneHBearish := isBTCOneHourBearish(data)
	deepFourHBreak := false
	if data.LongerTermContext != nil {
		longMACDHist := lastFloat(data.LongerTermContext.MACDHist)
		deepFourHBreak = data.LongerTermContext.EMA50 > 0 &&
			data.CurrentPrice < data.LongerTermContext.EMA50 &&
			longMACDHist < 0
	}
	directionalBearish := data.CurrentDIPlus > 0 && data.CurrentDIMinus > 0 && data.CurrentDIPlus < data.CurrentDIMinus
	priceBreakdown := data.PriceChange1h <= -1.5 || data.PriceChange4h <= -3

	return (fourHBearish && oneHBearish) || (deepFourHBreak && (oneHBearish || directionalBearish || priceBreakdown))
}

func isBTCFourHourBearish(data *market.Data) bool {
	if data == nil || data.LongerTermContext == nil {
		return false
	}
	priceBelowEMA20 := data.LongerTermContext.EMA20 > 0 && data.CurrentPrice < data.LongerTermContext.EMA20
	priceBelowEMA50 := data.LongerTermContext.EMA50 > 0 && data.CurrentPrice < data.LongerTermContext.EMA50
	diBearish := data.CurrentDIPlus > 0 && data.CurrentDIMinus > 0 && data.CurrentDIPlus < data.CurrentDIMinus
	macdBearish := lastFloat(data.LongerTermContext.MACDHist) < 0
	return (priceBelowEMA20 && diBearish) || (priceBelowEMA50 && macdBearish)
}

func isBTCOneHourBearish(data *market.Data) bool {
	if data == nil || data.MidTermSeries1h == nil {
		return false
	}
	ema20 := lastFloat(data.MidTermSeries1h.EMA20Values)
	ema50 := lastFloat(data.MidTermSeries1h.EMA50Values)
	macdHist := lastFloat(data.MidTermSeries1h.MACDHist)
	emaBearish := ema20 > 0 && ema50 > 0 && ema20 < ema50
	macdPriceBearish := macdHist < 0 && data.PriceChange1h < 0
	return emaBearish || macdPriceBearish
}

func buildBTCGateDiagnostics(data *market.Data) map[string]any {
	if data == nil {
		return nil
	}
	diagnostics := map[string]any{
		"symbol":                   data.Symbol,
		"current_price":            data.CurrentPrice,
		"price_change_1h_pct":      data.PriceChange1h,
		"price_change_4h_pct":      data.PriceChange4h,
		"bollinger_width":          data.BollingerWidth,
		"current_di_plus":          data.CurrentDIPlus,
		"current_di_minus":         data.CurrentDIMinus,
		"current_adx":              data.CurrentADX,
		"confirmed_bearish":        isConfirmedBTCBearishStructure(data),
		"four_hour_bearish":        isBTCFourHourBearish(data),
		"one_hour_bearish":         isBTCOneHourBearish(data),
		"mild_bearish_signal":      isBearishStructure(data),
		"multi_timeframe_conflict": hasBTCMultiTimeframeConflict(data),
	}
	if data.LongerTermContext != nil {
		diagnostics["four_hour_ema20"] = data.LongerTermContext.EMA20
		diagnostics["four_hour_ema50"] = data.LongerTermContext.EMA50
		diagnostics["four_hour_macd_hist"] = lastFloat(data.LongerTermContext.MACDHist)
		diagnostics["price_below_four_hour_ema20"] = data.LongerTermContext.EMA20 > 0 && data.CurrentPrice < data.LongerTermContext.EMA20
		diagnostics["price_below_four_hour_ema50"] = data.LongerTermContext.EMA50 > 0 && data.CurrentPrice < data.LongerTermContext.EMA50
	}
	if data.MidTermSeries1h != nil {
		ema20 := lastFloat(data.MidTermSeries1h.EMA20Values)
		ema50 := lastFloat(data.MidTermSeries1h.EMA50Values)
		macdHist := lastFloat(data.MidTermSeries1h.MACDHist)
		diagnostics["one_hour_ema20"] = ema20
		diagnostics["one_hour_ema50"] = ema50
		diagnostics["one_hour_macd_hist"] = macdHist
		diagnostics["one_hour_ema_bearish"] = ema20 > 0 && ema50 > 0 && ema20 < ema50
		diagnostics["one_hour_macd_price_bearish"] = macdHist < 0 && data.PriceChange1h < 0
	}
	if data.MidTermSeries15m != nil {
		diagnostics["fifteen_min_ema20"] = lastFloat(data.MidTermSeries15m.EMA20Values)
		diagnostics["fifteen_min_ema50"] = lastFloat(data.MidTermSeries15m.EMA50Values)
		diagnostics["fifteen_min_macd_hist"] = lastFloat(data.MidTermSeries15m.MACDHist)
	}
	return diagnostics
}

func hasBTCMultiTimeframeConflict(data *market.Data) bool {
	if data == nil || data.MidTermSeries15m == nil {
		return false
	}
	shortBearish := isSeriesBearish(data.MidTermSeries15m.EMA20Values, data.MidTermSeries15m.EMA50Values, data.MidTermSeries15m.MACDHist)
	shortBullish := isSeriesBullish(data.MidTermSeries15m.EMA20Values, data.MidTermSeries15m.EMA50Values, data.MidTermSeries15m.MACDHist)
	if !shortBearish && !shortBullish {
		return false
	}

	midBullish := false
	midBearish := false
	if data.MidTermSeries1h != nil {
		midBullish = isSeriesBullish(data.MidTermSeries1h.EMA20Values, data.MidTermSeries1h.EMA50Values, data.MidTermSeries1h.MACDHist)
		midBearish = isSeriesBearish(data.MidTermSeries1h.EMA20Values, data.MidTermSeries1h.EMA50Values, data.MidTermSeries1h.MACDHist)
	}
	longBullish := data.LongerTermContext != nil && data.LongerTermContext.EMA20 > 0 && data.CurrentPrice > data.LongerTermContext.EMA20 && data.CurrentDIPlus >= data.CurrentDIMinus
	longBearish := data.LongerTermContext != nil && data.LongerTermContext.EMA20 > 0 && data.CurrentPrice < data.LongerTermContext.EMA20 && data.CurrentDIPlus < data.CurrentDIMinus

	return (shortBearish && (midBullish || longBullish)) || (shortBullish && (midBearish || longBearish))
}

func hasPullbackConfirmationForLong(data *market.Data) bool {
	if data == nil {
		return false
	}
	if data.CurrentEMA20 > 0 && data.CurrentPrice <= data.CurrentEMA20*1.01 {
		return true
	}
	if data.MidTermSeries15m != nil {
		rsi := lastFloat(data.MidTermSeries15m.RSI14Values)
		hist := data.MidTermSeries15m.MACDHist
		if rsi > 0 && rsi < 60 && len(hist) >= 2 && hist[len(hist)-1] > hist[len(hist)-2] {
			return true
		}
	}
	return false
}

func isSeriesBullish(ema20Values, ema50Values, macdHistValues []float64) bool {
	ema20 := lastFloat(ema20Values)
	ema50 := lastFloat(ema50Values)
	macdHist := lastFloat(macdHistValues)
	return ema20 > 0 && ema50 > 0 && ema20 >= ema50 && macdHist >= 0
}

func isSeriesBearish(ema20Values, ema50Values, macdHistValues []float64) bool {
	ema20 := lastFloat(ema20Values)
	ema50 := lastFloat(ema50Values)
	macdHist := lastFloat(macdHistValues)
	return ema20 > 0 && ema50 > 0 && ema20 < ema50 && macdHist < 0
}

func lastFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}
