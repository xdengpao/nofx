package decision

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"strings"
	"time"
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
	Allowed         bool     `json:"allowed"`
	State           string   `json:"state"` // allow, penalize, block
	EffectiveRisk   float64  `json:"effective_risk"`
	MinConfidence   int      `json:"min_confidence,omitempty"`
	AdjustedSizeUSD float64  `json:"adjusted_size_usd,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// EvaluateOpenGate 汇总 rolling、市场状态、相关性、执行质量和 AI backoff gate。
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

	applyRollingPerformanceGate(&result, input.Decision, ctx)
	applyBTCMarketGate(&result, ctx)
	applyCorrelationConcentrationGate(&result, input.Decision, ctx)
	applyShortSideGate(&result, input.Decision)
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
}

func applyBTCMarketGate(result *OpenGateResult, ctx *Context) {
	btcData := ctx.MarketDataMap["BTCUSDT"]
	if btcData == nil {
		return
	}
	if btcData.PriceChange1h <= -5 {
		result.block(fmt.Sprintf("BTC 1小时跌幅 %.2f%%，禁止新开仓", btcData.PriceChange1h))
		return
	}
	if btcData.PriceChange1h <= -3 || btcData.PriceChange4h <= -7 || btcData.BollingerWidth >= 0.12 {
		result.penalize("BTC波动或跌幅偏高，新开仓降权")
		if result.MinConfidence < 85 {
			result.MinConfidence = 85
		}
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
		if pos.Side != side {
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

func applyShortSideGate(result *OpenGateResult, d *Decision) {
	if d.Action != "open_short" {
		return
	}
	result.penalize("short侧默认更严格，要求更高置信度")
	if result.MinConfidence < 90 {
		result.MinConfidence = 90
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

func (result *OpenGateResult) penalize(reason string) {
	if result.State == "" || result.State == "allow" {
		result.State = "penalize"
	}
	if reason != "" {
		result.Reasons = appendUniqueReason(result.Reasons, reason)
	}
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
