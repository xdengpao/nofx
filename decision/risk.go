package decision

import (
	"fmt"
	"log"
	"math"
	"nofx/market"
	"sync"
	"time"
)

// ============================================================================
// 风险计算器
// ============================================================================

// RiskCalculator 风险计算器
type RiskCalculator struct {
	DefaultStopPct float64
	MaxSingleRisk  float64
	CorrelationAdj bool
}

// PositionRisk 持仓风险详情
type PositionRisk struct {
	Symbol         string
	PositionValue  float64
	RiskUSD        float64
	RiskPercent    float64
	StopDistance   float64
	RiskSource     string // "plan", "position_sl", "atr_estimate", "default"
	IsAccurate     bool
	CorrelationAdj float64
}

// NewRiskCalculator 创建风险计算器
func NewRiskCalculator() *RiskCalculator {
	return &RiskCalculator{
		DefaultStopPct: 0.05,
		MaxSingleRisk:  0.02,
		CorrelationAdj: true,
	}
}

// CalculatePositionRisk 计算单个持仓的精确风险
func (rc *RiskCalculator) CalculatePositionRisk(
	pos *PositionInfo,
	plan *TradePlan,
	marketData *market.Data,
	btcCorrelation float64,
) *PositionRisk {

	result := &PositionRisk{
		Symbol:         pos.Symbol,
		PositionValue:  pos.Quantity * pos.MarkPrice,
		RiskSource:     "default",
		IsAccurate:     false,
		CorrelationAdj: 1.0,
	}

	var stopDistancePct float64

	// 方法1: 从交易计划获取（最准确）
	if plan != nil {
		effectiveSL := plan.CurrentStopLoss
		if effectiveSL == 0 {
			effectiveSL = plan.StopLoss
		}

		if effectiveSL > 0 {
			if plan.Direction == "long" {
				stopDistancePct = (pos.MarkPrice - effectiveSL) / pos.MarkPrice
			} else {
				stopDistancePct = (effectiveSL - pos.MarkPrice) / pos.MarkPrice
			}

			// 验证止损距离的合理性
			if stopDistancePct > 0 && stopDistancePct < 0.20 {
				result.RiskSource = "plan"
				result.IsAccurate = true
			}
		}
	}

	// 方法2: 从持仓的止损单获取
	if !result.IsAccurate && pos.StopLoss > 0 {
		if pos.Side == "long" {
			stopDistancePct = (pos.MarkPrice - pos.StopLoss) / pos.MarkPrice
		} else {
			stopDistancePct = (pos.StopLoss - pos.MarkPrice) / pos.MarkPrice
		}

		if stopDistancePct > 0 && stopDistancePct < 0.20 {
			result.RiskSource = "position_sl"
			result.IsAccurate = true
		}
	}

	// 方法3: 基于ATR估算
	if !result.IsAccurate && marketData != nil {
		atr := 0.0
		if marketData.LongerTermContext != nil {
			atr = marketData.LongerTermContext.ATR14
		}

		if atr > 0 {
			isAltcoin := pos.Symbol != "BTCUSDT" && pos.Symbol != "ETHUSDT"
			multiplier := 1.8
			if isAltcoin {
				multiplier = 2.5
			}
			stopDistancePct = (atr * multiplier) / pos.MarkPrice
			result.RiskSource = "atr_estimate"
			result.IsAccurate = false
		}
	}

	// 方法4: 使用默认值
	if stopDistancePct <= 0 {
		if pos.Symbol == "BTCUSDT" || pos.Symbol == "ETHUSDT" {
			stopDistancePct = 0.03
		} else {
			stopDistancePct = 0.05
		}
		result.RiskSource = "default"
		result.IsAccurate = false
	}

	// 确保止损距离在合理范围
	stopDistancePct = math.Max(0.005, math.Min(0.15, stopDistancePct))

	// 计算风险金额
	result.StopDistance = stopDistancePct
	result.RiskUSD = result.PositionValue * stopDistancePct

	// 相关性调整
	if rc.CorrelationAdj && math.Abs(btcCorrelation) > 0.7 {
		result.CorrelationAdj = 1.0 + (math.Abs(btcCorrelation)-0.7)*0.5
		result.RiskUSD *= result.CorrelationAdj
	}

	return result
}

// CalculateTotalRisk 计算总风险
func CalculateTotalRisk(ctx *Context) (totalRisk float64, riskDetails []*PositionRisk) {
	if ctx.Account.TotalEquity <= 0 {
		return 0, nil
	}

	rc := NewRiskCalculator()
	var totalRiskUSD float64
	var inaccurateCount int

	for _, pos := range ctx.Positions {
		plan := planManager.GetPlan(pos.Symbol)
		marketData := ctx.MarketDataMap[pos.Symbol]

		btcCorr := 0.0
		if corr, ok := ctx.CorrelationMap[pos.Symbol]; ok {
			btcCorr = corr.BTCCorr
		}

		risk := rc.CalculatePositionRisk(&pos, plan, marketData, btcCorr)
		riskDetails = append(riskDetails, risk)
		totalRiskUSD += risk.RiskUSD

		if !risk.IsAccurate {
			inaccurateCount++
		}

		log.Printf("📊 %s 风险: %.2f USD (%.2f%%) [来源: %s]",
			risk.Symbol, risk.RiskUSD, risk.StopDistance*100, risk.RiskSource)
	}

	// 如果有不准确的风险估算，增加安全边际
	if inaccurateCount > 0 {
		safetyMargin := 1.0 + float64(inaccurateCount)*0.1
		totalRiskUSD *= safetyMargin
		log.Printf("⚠️ 有 %d 个持仓风险估算不准确，添加 %.0f%% 安全边际",
			inaccurateCount, (safetyMargin-1)*100)
	}

	totalRisk = totalRiskUSD / ctx.Account.TotalEquity
	return totalRisk, riskDetails
}

// ============================================================================
// 动态风险调整器
// ============================================================================

// DynamicRiskAdjuster 动态风险调整器
type DynamicRiskAdjuster struct {
	BaseRiskPerTrade  float64
	WinStreakBonus    float64
	LoseStreakPenalty float64
	MaxAdjustment     float64
	mu                sync.RWMutex
}

var globalRiskAdjuster = &DynamicRiskAdjuster{
	BaseRiskPerTrade:  0.02,
	WinStreakBonus:    0.05,
	LoseStreakPenalty: 0.10,
	MaxAdjustment:     0.5,
}

// GetAdjustedRisk 获取调整后的风险
func (d *DynamicRiskAdjuster) GetAdjustedRisk(stats *TradeStatistics) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	adjustment := 1.0

	// 根据夏普比率调整
	if stats.SharpeRatio > 1.5 {
		adjustment *= 1.15
	} else if stats.SharpeRatio > 1.0 {
		adjustment *= 1.05
	} else if stats.SharpeRatio < 0 {
		adjustment *= 0.7
	} else if stats.SharpeRatio < 0.5 {
		adjustment *= 0.85
	}

	// 根据胜率调整
	if stats.TotalTrades >= 20 {
		if stats.WinRate > 0.6 {
			adjustment *= 1.1
		} else if stats.WinRate < 0.35 {
			adjustment *= 0.75
		}
	}

	// 根据连续亏损调整
	if stats.ConsecutiveLosses >= 3 {
		penalty := 1.0 - float64(stats.ConsecutiveLosses)*d.LoseStreakPenalty
		adjustment *= math.Max(0.5, penalty)
	}

	// 根据连续盈利调整
	if stats.ConsecutiveWins >= 3 {
		bonus := 1.0 + float64(stats.ConsecutiveWins-2)*d.WinStreakBonus
		adjustment *= math.Min(1.3, bonus)
	}

	// 限制调整幅度
	adjustment = math.Max(1-d.MaxAdjustment, math.Min(1+d.MaxAdjustment, adjustment))

	return d.BaseRiskPerTrade * adjustment
}

// SetBaseRisk 设置基础风险
func (d *DynamicRiskAdjuster) SetBaseRisk(risk float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.BaseRiskPerTrade = risk
}

// ============================================================================
// 全局熔断状态管理器
// ============================================================================

var (
	globalCircuitBreakerState *CircuitBreakerState
	cbStateLock               sync.RWMutex
	// 回撤基准线：冷却结束后记录当时的回撤水平，只有在此基础上再跌 MaxDailyLoss% 才重新触发
	drawdownBaseline    float64
	hasDrawdownBaseline bool
	drawdownBaselineMu  sync.RWMutex
)

// GetCircuitBreakerState 返回当前全局熔断状态的深拷贝（线程安全）
func GetCircuitBreakerState() *CircuitBreakerState {
	cbStateLock.RLock()
	defer cbStateLock.RUnlock()

	if globalCircuitBreakerState == nil {
		return nil
	}

	copy := *globalCircuitBreakerState
	return &copy
}

// SetCircuitBreakerState 更新全局熔断状态（线程安全）
func SetCircuitBreakerState(state *CircuitBreakerState) {
	cbStateLock.Lock()
	defer cbStateLock.Unlock()

	if state == nil {
		globalCircuitBreakerState = nil
		return
	}

	copy := *state
	globalCircuitBreakerState = &copy
}

// ResetDrawdownBaseline 重置回撤基准线（用于测试和系统重置）
func ResetDrawdownBaseline() {
	drawdownBaselineMu.Lock()
	defer drawdownBaselineMu.Unlock()
	drawdownBaseline = 0
	hasDrawdownBaseline = false
}

// ============================================================================
// 熔断机制
// ============================================================================

// CircuitBreakerConfig 熔断配置
type CircuitBreakerConfig struct {
	MaxDailyLoss         float64 // 最大日亏损百分比
	MaxConsecutiveLosses int     // 最大连续亏损次数
	BTCCrashThreshold    float64 // BTC闪崩阈值
	MarginUsageThreshold float64 // 保证金使用率阈值
	DefaultCooldownMin   int     // 默认冷却时间
	SevereCooldownMin    int     // 严重情况冷却时间
}

var defaultCircuitConfig = &CircuitBreakerConfig{
	MaxDailyLoss:         10.0,
	MaxConsecutiveLosses: 5,
	BTCCrashThreshold:    -5.0,
	MarginUsageThreshold: 90.0,
	DefaultCooldownMin:   30,
	SevereCooldownMin:    120,
}

// CheckCircuitBreaker 检查是否触发熔断
func CheckCircuitBreaker(ctx *Context, stats *TradeStatistics) *CircuitBreakerState {
	if ctx.CircuitBreaker == nil {
		ctx.CircuitBreaker = &CircuitBreakerState{}
	}

	cb := ctx.CircuitBreaker
	config := defaultCircuitConfig

	// 记录冷却结束时的回撤水平，用于判断是否进一步恶化
	var cooldownJustEnded bool

	// 检查是否在冷却中
	if cb.IsTriggered {
		cooldownEnd := cb.TriggerTime.Add(time.Duration(cb.CooldownMinutes) * time.Minute)
		if time.Now().Before(cooldownEnd) {
			return cb // 仍在冷却中
		}
		cooldownJustEnded = true
		// 冷却结束，设置回撤基准线为当前回撤水平
		drawdownBaselineMu.Lock()
		drawdownBaseline = ctx.Account.TotalPnLPct
		hasDrawdownBaseline = true
		drawdownBaselineMu.Unlock()
		// 重置
		cb.IsTriggered = false
		if stats != nil {
			stats.ConsecutiveLosses = 0
		}
		// 同步全局状态：冷却结束，清除熔断
		SetCircuitBreakerState(nil)
		log.Printf("✅ 熔断冷却结束，恢复交易（当前回撤: %.2f%%，新基准线: %.2f%%，再跌%.0f%%才重新触发）",
			ctx.Account.TotalPnLPct, ctx.Account.TotalPnLPct, config.MaxDailyLoss)
	}

	// 检查BTC闪崩
	if btcData, ok := ctx.MarketDataMap["BTCUSDT"]; ok {
		if btcData.PriceChange1h < config.BTCCrashThreshold {
			cb.IsTriggered = true
			cb.TriggerReason = fmt.Sprintf("BTC 1小时暴跌 %.2f%%", btcData.PriceChange1h)
			cb.TriggerTime = time.Now()
			cb.CooldownMinutes = config.SevereCooldownMin
			SetCircuitBreakerState(cb)
			log.Printf("🛑 熔断触发: %s", cb.TriggerReason)
			return cb
		}
	}

	// 检查账户回撤
	// 冷却结束后，以恢复时的回撤水平为基准线，在此基础上再跌 MaxDailyLoss% 才重新触发
	drawdownBaselineMu.RLock()
	hasBaseline := hasDrawdownBaseline
	baseline := drawdownBaseline
	drawdownBaselineMu.RUnlock()

	if hasBaseline {
		// 有基准线：基于基准线判断，回撤需要在基准线基础上再恶化 MaxDailyLoss% 才触发
		retriggerThreshold := baseline - config.MaxDailyLoss
		if ctx.Account.TotalPnLPct < retriggerThreshold {
			cb.IsTriggered = true
			cb.TriggerReason = fmt.Sprintf("账户回撤 %.2f%% 在基准 %.2f%% 上再跌超%.0f%%",
				ctx.Account.TotalPnLPct, baseline, config.MaxDailyLoss)
			cb.TriggerTime = time.Now()
			cb.CooldownMinutes = config.SevereCooldownMin
			cb.DailyLoss = ctx.Account.TotalPnLPct
			// 清除基准线，下次冷却结束会重新设置
			drawdownBaselineMu.Lock()
			hasDrawdownBaseline = false
			drawdownBaselineMu.Unlock()
			SetCircuitBreakerState(cb)
			log.Printf("🛑 熔断触发: %s", cb.TriggerReason)
			return cb
		}
		if !cooldownJustEnded {
			// 回撤好转时更新基准线（取较好的值），让基准线跟随恢复
			if ctx.Account.TotalPnLPct > baseline {
				drawdownBaselineMu.Lock()
				drawdownBaseline = ctx.Account.TotalPnLPct
				drawdownBaselineMu.Unlock()
			}
		}
	} else {
		// 无基准线：首次触发，使用原始阈值
		if ctx.Account.TotalPnLPct < -config.MaxDailyLoss {
			cb.IsTriggered = true
			cb.TriggerReason = fmt.Sprintf("账户回撤 %.2f%% 超过%.0f%%", ctx.Account.TotalPnLPct, config.MaxDailyLoss)
			cb.TriggerTime = time.Now()
			cb.CooldownMinutes = config.SevereCooldownMin
			cb.DailyLoss = ctx.Account.TotalPnLPct
			SetCircuitBreakerState(cb)
			log.Printf("🛑 熔断触发: %s", cb.TriggerReason)
			return cb
		}
	}

	// 检查连续亏损
	if stats != nil && stats.ConsecutiveLosses >= config.MaxConsecutiveLosses {
		cb.IsTriggered = true
		cb.TriggerReason = fmt.Sprintf("连续亏损 %d 次", stats.ConsecutiveLosses)
		cb.TriggerTime = time.Now()
		cb.CooldownMinutes = config.DefaultCooldownMin
		cb.ConsecutiveLosses = stats.ConsecutiveLosses
		SetCircuitBreakerState(cb)
		log.Printf("🛑 熔断触发: %s", cb.TriggerReason)
		return cb
	}

	// 检查保证金使用率
	if ctx.Account.MarginUsedPct > config.MarginUsageThreshold {
		cb.IsTriggered = true
		cb.TriggerReason = fmt.Sprintf("保证金使用率 %.2f%% 过高", ctx.Account.MarginUsedPct)
		cb.TriggerTime = time.Now()
		cb.CooldownMinutes = config.DefaultCooldownMin
		SetCircuitBreakerState(cb)
		log.Printf("🛑 熔断触发: %s", cb.TriggerReason)
		return cb
	}

	return cb
}

// ============================================================================
// 相关性计算
// ============================================================================

// CalculateCorrelationMatrix 计算相关性矩阵
func CalculateCorrelationMatrix(ctx *Context) {
	ctx.CorrelationMap = make(map[string]*CorrelationData)

	btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]
	if !hasBTC || btcData.MidTermSeries1h == nil {
		return
	}
	btcPrices := btcData.MidTermSeries1h.MidPrices

	for symbol, data := range ctx.MarketDataMap {
		if symbol == "BTCUSDT" {
			ctx.CorrelationMap[symbol] = &CorrelationData{
				Symbol:     symbol,
				BTCCorr:    1.0,
				IsHighCorr: true,
				RiskWeight: 1.0,
			}
			continue
		}

		if data.MidTermSeries1h == nil {
			continue
		}

		prices := data.MidTermSeries1h.MidPrices
		corr := market.CalculateCorrelation(btcPrices, prices)

		isHighCorr := math.Abs(corr) > 0.8
		riskWeight := 1.0
		if isHighCorr {
			riskWeight = 0.7
		} else if math.Abs(corr) < 0.5 {
			riskWeight = 1.0
		} else {
			riskWeight = 0.85
		}

		ctx.CorrelationMap[symbol] = &CorrelationData{
			Symbol:     symbol,
			BTCCorr:    corr,
			IsHighCorr: isHighCorr,
			RiskWeight: riskWeight,
		}
	}
}

// ============================================================================
// 市场状态检测
// ============================================================================

// MarketRegimeDetector 市场状态检测器
type MarketRegimeDetector struct {
	btcData    *market.Data
	totalOI    float64
	fundingAvg float64
}

// NewMarketRegimeDetector 创建市场状态检测器
func NewMarketRegimeDetector(btcData *market.Data) *MarketRegimeDetector {
	return &MarketRegimeDetector{
		btcData: btcData,
	}
}

// Detect 检测市场状态
func (d *MarketRegimeDetector) Detect() (MarketRegime, float64) {
	if d.btcData == nil {
		return RegimeUncertain, 0.5
	}

	adx := d.btcData.CurrentADX
	priceChange := d.btcData.PriceChange1h
	rsi := d.btcData.CurrentRSI14

	// 崩盘检测
	if priceChange < -5 {
		return RegimeCrash, 0.95
	}

	// 极端波动检测
	if math.Abs(priceChange) > 3 || rsi > 85 || rsi < 15 {
		return RegimeVolatile, 0.85
	}

	// 趋势检测
	if adx > 30 {
		return RegimeTrending, math.Min(0.95, adx/40)
	}

	// 震荡检测
	if adx < 20 {
		return RegimeRanging, math.Min(0.9, (25-adx)/10)
	}

	return RegimeUncertain, 0.5
}

// GetTradingRecommendation 获取交易建议
func (d *MarketRegimeDetector) GetTradingRecommendation(regime MarketRegime) string {
	switch regime {
	case RegimeCrash:
		return "🛑 市场崩盘，停止交易，等待稳定"
	case RegimeVolatile:
		return "⚠️ 高波动市场，减少仓位，收紧止损"
	case RegimeTrending:
		return "📈 趋势市场，顺势交易，扩大止盈目标"
	case RegimeRanging:
		return "↔️ 震荡市场，区间交易，收紧止盈目标"
	default:
		return "❓ 不确定市场，保守交易"
	}
}
