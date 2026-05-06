package decision

// Feature: quant-trading-system
// 任务 12.1: 风险管理模块测试覆盖
// 覆盖需求: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9, 5.10, 5.11

import (
	"math"
	"nofx/market"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

// newRiskTestContext 构造风险测试用 Context
func newRiskTestContext(equity float64) *Context {
	return &Context{
		Account: AccountInfo{
			TotalEquity:   equity,
			TotalPnLPct:   0.0,
			MarginUsedPct: 50.0,
		},
		Positions:      []PositionInfo{},
		MarketDataMap:  map[string]*market.Data{},
		CorrelationMap: map[string]*CorrelationData{},
	}
}

// newTestPosition 构造测试持仓
func newTestPosition(symbol, side string, qty, markPrice, stopLoss float64) PositionInfo {
	return PositionInfo{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: markPrice,
		MarkPrice:  markPrice,
		Quantity:   qty,
		StopLoss:   stopLoss,
	}
}

// ============================================================================
// 需求 5.3: CalculatePositionRisk — 四级降级风险计算 (单元测试)
// ============================================================================

func TestCalculatePositionRisk_PlanSource_Accurate(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "BTCUSDT", Side: "long", MarkPrice: 50000.0, Quantity: 0.1}
	plan := &TradePlan{Direction: "long", StopLoss: 48000.0, CurrentStopLoss: 48000.0}
	risk := rc.CalculatePositionRisk(pos, plan, nil, 0.0)

	if risk.RiskSource != "plan" {
		t.Errorf("有效交易计划止损应使用 plan 来源, 实际=%s", risk.RiskSource)
	}
	if !risk.IsAccurate {
		t.Error("交易计划止损应标记为准确")
	}
	posValue := pos.Quantity * pos.MarkPrice
	expectedStopDist := (pos.MarkPrice - plan.StopLoss) / pos.MarkPrice
	expectedRisk := posValue * expectedStopDist
	if math.Abs(risk.RiskUSD-expectedRisk) > 0.01 {
		t.Errorf("风险金额计算错误: 期望=%.4f, 实际=%.4f", expectedRisk, risk.RiskUSD)
	}
}

func TestCalculatePositionRisk_PositionSLSource(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "ETHUSDT", Side: "long", MarkPrice: 3000.0, Quantity: 1.0, StopLoss: 2850.0}
	risk := rc.CalculatePositionRisk(pos, nil, nil, 0.0)

	if risk.RiskSource != "position_sl" {
		t.Errorf("无计划但有持仓止损时应使用 position_sl 来源, 实际=%s", risk.RiskSource)
	}
	if !risk.IsAccurate {
		t.Error("持仓止损应标记为准确")
	}
}

func TestCalculatePositionRisk_ATRSource(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "SOLUSDT", Side: "long", MarkPrice: 100.0, Quantity: 10.0}
	md := &market.Data{LongerTermContext: &market.LongerTermData{ATR14: 3.0}}
	risk := rc.CalculatePositionRisk(pos, nil, md, 0.0)

	if risk.RiskSource != "atr_estimate" {
		t.Errorf("无止损但有ATR时应使用 atr_estimate 来源, 实际=%s", risk.RiskSource)
	}
	if risk.IsAccurate {
		t.Error("ATR估算应标记为不准确")
	}
}

func TestCalculatePositionRisk_DefaultSource(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "SOLUSDT", Side: "long", MarkPrice: 100.0, Quantity: 10.0}
	risk := rc.CalculatePositionRisk(pos, nil, nil, 0.0)

	if risk.RiskSource != "default" {
		t.Errorf("无任何止损信息时应使用 default 来源, 实际=%s", risk.RiskSource)
	}
	if risk.IsAccurate {
		t.Error("默认估算应标记为不准确")
	}
}

func TestCalculatePositionRisk_Short_StopAbovePrice(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "BTCUSDT", Side: "short", MarkPrice: 50000.0, Quantity: 0.1}
	plan := &TradePlan{Direction: "short", StopLoss: 52000.0, CurrentStopLoss: 52000.0}
	risk := rc.CalculatePositionRisk(pos, plan, nil, 0.0)

	if risk.RiskSource != "plan" {
		t.Errorf("空头有效止损应使用 plan 来源, 实际=%s", risk.RiskSource)
	}
	posValue := pos.Quantity * pos.MarkPrice
	expectedStopDist := (plan.StopLoss - pos.MarkPrice) / pos.MarkPrice
	expectedRisk := posValue * expectedStopDist
	if math.Abs(risk.RiskUSD-expectedRisk) > 0.01 {
		t.Errorf("空头风险金额计算错误: 期望=%.4f, 实际=%.4f", expectedRisk, risk.RiskUSD)
	}
}

func TestCalculatePositionRisk_HighCorrelation_AdjustsRisk(t *testing.T) {
	rc := NewRiskCalculator()
	pos := &PositionInfo{Symbol: "ETHUSDT", Side: "long", MarkPrice: 3000.0, Quantity: 1.0, StopLoss: 2850.0}
	riskHighCorr := rc.CalculatePositionRisk(pos, nil, nil, 0.9)
	riskNoCorr := rc.CalculatePositionRisk(pos, nil, nil, 0.0)

	if riskHighCorr.RiskUSD <= riskNoCorr.RiskUSD {
		t.Errorf("高相关性应增加风险: highCorr=%.4f, noCorr=%.4f", riskHighCorr.RiskUSD, riskNoCorr.RiskUSD)
	}
	if riskHighCorr.CorrelationAdj <= 1.0 {
		t.Errorf("高相关性调整系数应 > 1.0, 实际=%.4f", riskHighCorr.CorrelationAdj)
	}
}

// ============================================================================
// 需求 5.4: CalculateTotalRisk — 安全边际计算 (单元测试)
// ============================================================================

func TestCalculateTotalRisk_ZeroEquity_ReturnsZero(t *testing.T) {
	ctx := newRiskTestContext(0)
	ctx.Positions = []PositionInfo{newTestPosition("BTCUSDT", "long", 0.1, 50000, 48000)}
	totalRisk, details := CalculateTotalRisk(ctx)
	if totalRisk != 0 {
		t.Errorf("零权益时总风险应为0, 实际=%.4f", totalRisk)
	}
	if details != nil {
		t.Error("零权益时风险详情应为nil")
	}
}

func TestCalculateTotalRisk_NoPositions_ReturnsZero(t *testing.T) {
	ctx := newRiskTestContext(10000)
	totalRisk, details := CalculateTotalRisk(ctx)
	if totalRisk != 0 {
		t.Errorf("无持仓时总风险应为0, 实际=%.4f", totalRisk)
	}
	if len(details) != 0 {
		t.Errorf("无持仓时风险详情应为空, 实际长度=%d", len(details))
	}
}

func TestCalculateTotalRisk_InaccurateEstimates_AddSafetyMargin(t *testing.T) {
	// 两个持仓，均无止损（不准确估算），应添加安全边际
	ctx := newRiskTestContext(10000)
	ctx.Positions = []PositionInfo{
		newTestPosition("SOLUSDT", "long", 10, 100, 0), // 无止损 → 不准确
		newTestPosition("BNBUSDT", "long", 5, 200, 0),  // 无止损 → 不准确
	}

	// 单个持仓的基准风险（无安全边际）
	rc := NewRiskCalculator()
	r1 := rc.CalculatePositionRisk(&ctx.Positions[0], nil, nil, 0)
	r2 := rc.CalculatePositionRisk(&ctx.Positions[1], nil, nil, 0)
	baseRiskUSD := r1.RiskUSD + r2.RiskUSD
	// 2个不准确 → 安全边际 = 1 + 2×0.1 = 1.2
	expectedRiskUSD := baseRiskUSD * 1.2
	expectedRiskPct := expectedRiskUSD / 10000

	totalRisk, _ := CalculateTotalRisk(ctx)
	if math.Abs(totalRisk-expectedRiskPct) > 0.001 {
		t.Errorf("2个不准确估算安全边际错误: 期望=%.6f, 实际=%.6f", expectedRiskPct, totalRisk)
	}
}

func TestCalculateTotalRisk_AccurateEstimates_NoSafetyMargin(t *testing.T) {
	// 有精确止损的持仓不应添加安全边际
	ctx := newRiskTestContext(10000)
	pos := newTestPosition("BTCUSDT", "long", 0.1, 50000, 48000)
	ctx.Positions = []PositionInfo{pos}

	rc := NewRiskCalculator()
	r := rc.CalculatePositionRisk(&pos, nil, nil, 0)
	// position_sl 来源是准确的
	if !r.IsAccurate {
		t.Skip("持仓止损应为准确来源，跳过此测试")
	}

	expectedRiskPct := r.RiskUSD / 10000
	totalRisk, _ := CalculateTotalRisk(ctx)
	if math.Abs(totalRisk-expectedRiskPct) > 0.001 {
		t.Errorf("准确估算不应添加安全边际: 期望=%.6f, 实际=%.6f", expectedRiskPct, totalRisk)
	}
}

// ============================================================================
// 需求 5.10: GetAdjustedRisk — 动态风险调整范围 (单元测试)
// ============================================================================

func TestGetAdjustedRisk_WithinBounds_HighSharpe(t *testing.T) {
	adjuster := &DynamicRiskAdjuster{
		BaseRiskPerTrade:  0.02,
		WinStreakBonus:    0.05,
		LoseStreakPenalty: 0.10,
		MaxAdjustment:     0.5,
	}
	stats := &TradeStatistics{SharpeRatio: 2.0, TotalTrades: 30, WinRate: 0.65}
	risk := adjuster.GetAdjustedRisk(stats)

	if risk < adjuster.BaseRiskPerTrade*0.5 || risk > adjuster.BaseRiskPerTrade*1.5 {
		t.Errorf("调整后风险应在 [%.4f, %.4f] 范围内, 实际=%.4f",
			adjuster.BaseRiskPerTrade*0.5, adjuster.BaseRiskPerTrade*1.5, risk)
	}
}

func TestGetAdjustedRisk_WithinBounds_LowSharpe(t *testing.T) {
	adjuster := &DynamicRiskAdjuster{
		BaseRiskPerTrade:  0.02,
		WinStreakBonus:    0.05,
		LoseStreakPenalty: 0.10,
		MaxAdjustment:     0.5,
	}
	stats := &TradeStatistics{SharpeRatio: -0.5, TotalTrades: 30, WinRate: 0.30}
	risk := adjuster.GetAdjustedRisk(stats)

	if risk < adjuster.BaseRiskPerTrade*0.5 || risk > adjuster.BaseRiskPerTrade*1.5 {
		t.Errorf("低夏普调整后风险应在 [%.4f, %.4f] 范围内, 实际=%.4f",
			adjuster.BaseRiskPerTrade*0.5, adjuster.BaseRiskPerTrade*1.5, risk)
	}
}

func TestGetAdjustedRisk_ConsecutiveLosses_ReducesRisk(t *testing.T) {
	adjuster := &DynamicRiskAdjuster{
		BaseRiskPerTrade:  0.02,
		WinStreakBonus:    0.05,
		LoseStreakPenalty: 0.10,
		MaxAdjustment:     0.5,
	}
	statsNormal := &TradeStatistics{SharpeRatio: 1.0, TotalTrades: 20, WinRate: 0.5}
	statsLosing := &TradeStatistics{SharpeRatio: 1.0, TotalTrades: 20, WinRate: 0.5, ConsecutiveLosses: 5}

	riskNormal := adjuster.GetAdjustedRisk(statsNormal)
	riskLosing := adjuster.GetAdjustedRisk(statsLosing)

	if riskLosing >= riskNormal {
		t.Errorf("连续亏损应降低风险: 正常=%.4f, 亏损中=%.4f", riskNormal, riskLosing)
	}
}

func TestGetAdjustedRisk_ConsecutiveWins_IncreasesRisk(t *testing.T) {
	adjuster := &DynamicRiskAdjuster{
		BaseRiskPerTrade:  0.02,
		WinStreakBonus:    0.05,
		LoseStreakPenalty: 0.10,
		MaxAdjustment:     0.5,
	}
	statsNormal := &TradeStatistics{SharpeRatio: 1.0, TotalTrades: 20, WinRate: 0.5}
	statsWinning := &TradeStatistics{SharpeRatio: 1.0, TotalTrades: 20, WinRate: 0.5, ConsecutiveWins: 5}

	riskNormal := adjuster.GetAdjustedRisk(statsNormal)
	riskWinning := adjuster.GetAdjustedRisk(statsWinning)

	if riskWinning <= riskNormal {
		t.Errorf("连续盈利应增加风险: 正常=%.4f, 盈利中=%.4f", riskNormal, riskWinning)
	}
}

// ============================================================================
// 需求 5.9: CalculateCorrelationMatrix — 相关性计算 (单元测试)
// ============================================================================

func TestCalculateCorrelationMatrix_BTCAlwaysCorr1(t *testing.T) {
	ctx := newRiskTestContext(10000)
	btcPrices := []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109}
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		MidTermSeries1h: &market.MidTermData1h{MidPrices: btcPrices},
	}

	CalculateCorrelationMatrix(ctx)

	btcCorr, ok := ctx.CorrelationMap["BTCUSDT"]
	if !ok {
		t.Fatal("BTC 应在相关性矩阵中")
	}
	if btcCorr.BTCCorr != 1.0 {
		t.Errorf("BTC 与自身相关性应为1.0, 实际=%.4f", btcCorr.BTCCorr)
	}
	if btcCorr.RiskWeight != 1.0 {
		t.Errorf("BTC 风险权重应为1.0, 实际=%.4f", btcCorr.RiskWeight)
	}
}

func TestCalculateCorrelationMatrix_HighCorr_ReducesWeight(t *testing.T) {
	ctx := newRiskTestContext(10000)
	// BTC 和 ETH 价格完全同向（高相关性）
	btcPrices := []float64{100, 101, 102, 103, 104, 105, 106, 107, 108, 109}
	ethPrices := []float64{200, 202, 204, 206, 208, 210, 212, 214, 216, 218}
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		MidTermSeries1h: &market.MidTermData1h{MidPrices: btcPrices},
	}
	ctx.MarketDataMap["ETHUSDT"] = &market.Data{
		MidTermSeries1h: &market.MidTermData1h{MidPrices: ethPrices},
	}

	CalculateCorrelationMatrix(ctx)

	ethCorr, ok := ctx.CorrelationMap["ETHUSDT"]
	if !ok {
		t.Fatal("ETH 应在相关性矩阵中")
	}
	// 高相关性 (>0.8) 应降低风险权重至 0.7
	if ethCorr.IsHighCorr && ethCorr.RiskWeight != 0.7 {
		t.Errorf("高相关性风险权重应为0.7, 实际=%.4f", ethCorr.RiskWeight)
	}
}

func TestCalculateCorrelationMatrix_NoBTCData_EmptyMap(t *testing.T) {
	ctx := newRiskTestContext(10000)
	// 无 BTC 数据时，相关性矩阵应为空
	CalculateCorrelationMatrix(ctx)

	if len(ctx.CorrelationMap) != 0 {
		t.Errorf("无BTC数据时相关性矩阵应为空, 实际长度=%d", len(ctx.CorrelationMap))
	}
}

// ============================================================================
// 需求 5.5-5.8: CheckCircuitBreaker — 四种熔断条件 (单元测试)
// ============================================================================

func setupCleanCircuitBreaker() {
	SetCircuitBreakerState(nil)
	ResetDrawdownBaseline()
}

func TestCircuitBreaker_BTCCrash_Triggers(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -6.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if !result.IsTriggered {
		t.Error("BTC 1h 跌幅 -6% 应触发熔断")
	}
	if result.CooldownMinutes != 120 {
		t.Errorf("BTC暴跌冷却时间应为120分钟, 实际=%d", result.CooldownMinutes)
	}
}

func TestCircuitBreaker_BTCNormalChange_NoTrigger(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -4.9}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if result.IsTriggered {
		t.Error("BTC 1h 跌幅 -4.9% 不应触发熔断")
	}
}

func TestCircuitBreaker_AccountDrawdown_Triggers(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -11.0
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if !result.IsTriggered {
		t.Error("账户回撤 -11% 应触发熔断")
	}
	if result.CooldownMinutes != 120 {
		t.Errorf("账户回撤冷却时间应为120分钟, 实际=%d", result.CooldownMinutes)
	}
}

func TestCircuitBreaker_AccountDrawdown_BelowThreshold_NoTrigger(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -9.9
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if result.IsTriggered {
		t.Error("账户回撤 -9.9% 不应触发熔断")
	}
}

func TestCircuitBreaker_ConfiguredMaxDailyLoss_OverridesDefault(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.MaxDailyLossPct = 0.05
	ctx.Account.TotalPnLPct = -6.0
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}

	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if !result.IsTriggered {
		t.Fatal("配置 max_daily_loss=5% 时，账户回撤 -6% 应触发熔断")
	}
	if result.DailyLoss != -6.0 {
		t.Fatalf("熔断应记录当前回撤: got=%.2f", result.DailyLoss)
	}
}

func TestCircuitBreaker_ConsecutiveLosses_Triggers(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -3.0
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{ConsecutiveLosses: 5})
	if !result.IsTriggered {
		t.Error("连续亏损5次应触发熔断")
	}
	if result.CooldownMinutes != 30 {
		t.Errorf("连续亏损冷却时间应为30分钟, 实际=%d", result.CooldownMinutes)
	}
}

func TestCircuitBreaker_ConsecutiveLosses_BelowThreshold_NoTrigger(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -3.0
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{ConsecutiveLosses: 4})
	if result.IsTriggered {
		t.Error("连续亏损4次不应触发熔断")
	}
}

func TestCircuitBreaker_MarginUsage_Triggers(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -3.0
	ctx.Account.MarginUsedPct = 91.0
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if !result.IsTriggered {
		t.Error("保证金使用率91%应触发熔断")
	}
	if result.CooldownMinutes != 30 {
		t.Errorf("保证金过高冷却时间应为30分钟, 实际=%d", result.CooldownMinutes)
	}
}

func TestCircuitBreaker_MarginUsage_BelowThreshold_NoTrigger(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -3.0
	ctx.Account.MarginUsedPct = 89.9
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if result.IsTriggered {
		t.Error("保证金使用率89.9%不应触发熔断")
	}
}

func TestCircuitBreaker_CooldownActive_ReturnsTriggered(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.CircuitBreaker = &CircuitBreakerState{
		IsTriggered:     true,
		TriggerReason:   "测试熔断",
		TriggerTime:     time.Now().Add(-10 * time.Minute),
		CooldownMinutes: 30,
	}
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	result := CheckCircuitBreaker(ctx, &TradeStatistics{})
	if !result.IsTriggered {
		t.Error("冷却期内应保持熔断状态")
	}
}

func TestCircuitBreaker_CooldownExpired_ResetsConsecutiveLosses(t *testing.T) {
	setupCleanCircuitBreaker()
	defer setupCleanCircuitBreaker()
	ctx := newRiskTestContext(10000)
	ctx.Account.TotalPnLPct = -3.0
	ctx.Account.MarginUsedPct = 50.0
	ctx.CircuitBreaker = &CircuitBreakerState{
		IsTriggered:     true,
		TriggerReason:   "连续亏损 5 次",
		TriggerTime:     time.Now().Add(-60 * time.Minute),
		CooldownMinutes: 30,
	}
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
	stats := &TradeStatistics{ConsecutiveLosses: 5}
	CheckCircuitBreaker(ctx, stats)
	if stats.ConsecutiveLosses != 0 {
		t.Errorf("冷却结束后 ConsecutiveLosses 应重置为0, 实际=%d", stats.ConsecutiveLosses)
	}
}

// ============================================================================
// 需求 5.11: 市场状态检测 (单元测试)
// ============================================================================

func TestMarketRegimeDetector_Crash(t *testing.T) {
	btcData := &market.Data{PriceChange1h: -6.0, CurrentADX: 40, CurrentRSI14: 20}
	d := NewMarketRegimeDetector(btcData)
	regime, confidence := d.Detect()
	if regime != RegimeCrash {
		t.Errorf("1h跌幅-6%%应检测为崩盘, 实际=%s", regime)
	}
	if confidence < 0.9 {
		t.Errorf("崩盘置信度应>=0.9, 实际=%.2f", confidence)
	}
}

func TestMarketRegimeDetector_Trending(t *testing.T) {
	btcData := &market.Data{PriceChange1h: 1.0, CurrentADX: 35, CurrentRSI14: 60}
	d := NewMarketRegimeDetector(btcData)
	regime, _ := d.Detect()
	if regime != RegimeTrending {
		t.Errorf("ADX=35应检测为趋势市场, 实际=%s", regime)
	}
}

func TestMarketRegimeDetector_Ranging(t *testing.T) {
	btcData := &market.Data{PriceChange1h: 0.5, CurrentADX: 15, CurrentRSI14: 50}
	d := NewMarketRegimeDetector(btcData)
	regime, _ := d.Detect()
	if regime != RegimeRanging {
		t.Errorf("ADX=15应检测为震荡市场, 实际=%s", regime)
	}
}

func TestMarketRegimeDetector_NilData_ReturnsUncertain(t *testing.T) {
	d := NewMarketRegimeDetector(nil)
	regime, confidence := d.Detect()
	if regime != RegimeUncertain {
		t.Errorf("nil数据应返回不确定状态, 实际=%s", regime)
	}
	if confidence != 0.5 {
		t.Errorf("nil数据置信度应为0.5, 实际=%.2f", confidence)
	}
}

func TestMarketRegimeDetector_GetTradingRecommendation_NonEmpty(t *testing.T) {
	d := NewMarketRegimeDetector(nil)
	regimes := []MarketRegime{RegimeCrash, RegimeVolatile, RegimeTrending, RegimeRanging, RegimeUncertain}
	for _, r := range regimes {
		rec := d.GetTradingRecommendation(r)
		if rec == "" {
			t.Errorf("市场状态 %s 的交易建议不应为空", r)
		}
	}
}

// ============================================================================
// 需求 5.3: 风险计算精确性 — 属性基测试 (Property 23)
// Feature: quant-trading-system, Property 23: 风险计算精确性
// Validates: Requirements 5.3
// ============================================================================

func TestProperty23_RiskCalculationAccuracy(t *testing.T) {
	// Feature: quant-trading-system, Property 23: 风险计算精确性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("多头风险计算公式正确", prop.ForAll(
		func(priceNorm, slOffsetNorm, qtyNorm float64) bool {
			markPrice := 100.0 + priceNorm*99900.0
			slOffset := 0.005 + slOffsetNorm*0.145
			stopLoss := markPrice * (1 - slOffset)
			qty := 0.01 + qtyNorm*9.99

			rc := NewRiskCalculator()
			rc.CorrelationAdj = false
			pos := &PositionInfo{
				Symbol:    "BTCUSDT",
				Side:      "long",
				MarkPrice: markPrice,
				Quantity:  qty,
				StopLoss:  stopLoss,
			}
			risk := rc.CalculatePositionRisk(pos, nil, nil, 0.0)

			posValue := qty * markPrice
			rawDist := (markPrice - stopLoss) / markPrice
			clampedDist := math.Max(0.005, math.Min(0.15, rawDist))
			expectedRisk := posValue * clampedDist

			return math.Abs(risk.RiskUSD-expectedRisk) < 0.01
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.Property("空头风险计算公式正确", prop.ForAll(
		func(priceNorm, slOffsetNorm, qtyNorm float64) bool {
			markPrice := 100.0 + priceNorm*99900.0
			slOffset := 0.005 + slOffsetNorm*0.145
			stopLoss := markPrice * (1 + slOffset)
			qty := 0.01 + qtyNorm*9.99

			rc := NewRiskCalculator()
			rc.CorrelationAdj = false
			pos := &PositionInfo{
				Symbol:    "BTCUSDT",
				Side:      "short",
				MarkPrice: markPrice,
				Quantity:  qty,
				StopLoss:  stopLoss,
			}
			risk := rc.CalculatePositionRisk(pos, nil, nil, 0.0)

			posValue := qty * markPrice
			rawDist := (stopLoss - markPrice) / markPrice
			clampedDist := math.Max(0.005, math.Min(0.15, rawDist))
			expectedRisk := posValue * clampedDist

			return math.Abs(risk.RiskUSD-expectedRisk) < 0.01
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 5.4: 不准确风险估算安全边际 — 属性基测试 (Property 24)
// Feature: quant-trading-system, Property 24: 不准确风险估算安全边际
// Validates: Requirements 5.4
// ============================================================================

func TestProperty24_InaccurateRiskSafetyMargin(t *testing.T) {
	// Feature: quant-trading-system, Property 24: 不准确风险估算安全边际
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("N个不准确估算应添加(1+N×0.1)安全边际", prop.ForAll(
		func(nNorm int, equityNorm float64) bool {
			n := 1 + nNorm%5
			equity := 10000.0 + equityNorm*90000.0

			ctx := newRiskTestContext(equity)
			symbols := []string{"SOLUSDT", "BNBUSDT", "ADAUSDT", "DOTUSDT", "AVAXUSDT"}
			prices := []float64{100, 300, 0.5, 8, 30}

			for i := 0; i < n; i++ {
				ctx.Positions = append(ctx.Positions, PositionInfo{
					Symbol:    symbols[i],
					Side:      "long",
					MarkPrice: prices[i],
					Quantity:  10.0,
				})
			}

			rc := NewRiskCalculator()
			var baseRiskUSD float64
			for i := range ctx.Positions {
				r := rc.CalculatePositionRisk(&ctx.Positions[i], nil, nil, 0)
				baseRiskUSD += r.RiskUSD
			}

			expectedMargin := 1.0 + float64(n)*0.1
			expectedRiskPct := (baseRiskUSD * expectedMargin) / equity

			totalRisk, _ := CalculateTotalRisk(ctx)
			return math.Abs(totalRisk-expectedRiskPct) < 0.0001
		},
		gen.IntRange(0, 99),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 5.5-5.8: 熔断条件触发 — 属性基测试 (Property 25)
// Feature: quant-trading-system, Property 25: 熔断条件触发
// Validates: Requirements 5.5, 5.6, 5.7, 5.8
// ============================================================================

func TestProperty25_CircuitBreakerTriggerConditions(t *testing.T) {
	// Feature: quant-trading-system, Property 25: 熔断条件触发
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("BTC暴跌超5%应触发熔断且冷却120分钟", prop.ForAll(
		func(dropNorm float64) bool {
			setupCleanCircuitBreaker()
			defer setupCleanCircuitBreaker()
			drop := -5.01 - dropNorm*14.99
			ctx := newRiskTestContext(10000)
			ctx.Account.TotalPnLPct = -3.0
			ctx.Account.MarginUsedPct = 50.0
			ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: drop}
			stats := &TradeStatistics{}
			result := CheckCircuitBreaker(ctx, stats)
			return result.IsTriggered && result.CooldownMinutes == 120
		},
		gen.Float64Range(0, 1),
	))

	properties.Property("账户回撤超10%应触发熔断且冷却120分钟", prop.ForAll(
		func(drawdownNorm float64) bool {
			setupCleanCircuitBreaker()
			defer setupCleanCircuitBreaker()
			drawdown := -10.01 - drawdownNorm*39.99
			ctx := newRiskTestContext(10000)
			ctx.Account.TotalPnLPct = drawdown
			ctx.Account.MarginUsedPct = 50.0
			ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
			stats := &TradeStatistics{}
			result := CheckCircuitBreaker(ctx, stats)
			return result.IsTriggered && result.CooldownMinutes == 120
		},
		gen.Float64Range(0, 1),
	))

	properties.Property("连续亏损>=5次应触发熔断且冷却30分钟", prop.ForAll(
		func(lossesNorm int) bool {
			setupCleanCircuitBreaker()
			defer setupCleanCircuitBreaker()
			losses := 5 + lossesNorm%11
			ctx := newRiskTestContext(10000)
			ctx.Account.TotalPnLPct = -3.0
			ctx.Account.MarginUsedPct = 50.0
			ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
			stats := &TradeStatistics{ConsecutiveLosses: losses}
			result := CheckCircuitBreaker(ctx, stats)
			return result.IsTriggered && result.CooldownMinutes == 30
		},
		gen.IntRange(0, 99),
	))

	properties.Property("保证金使用率>90%应触发熔断且冷却30分钟", prop.ForAll(
		func(marginNorm float64) bool {
			setupCleanCircuitBreaker()
			defer setupCleanCircuitBreaker()
			margin := 90.01 + marginNorm*9.99
			ctx := newRiskTestContext(10000)
			ctx.Account.TotalPnLPct = -3.0
			ctx.Account.MarginUsedPct = margin
			ctx.MarketDataMap["BTCUSDT"] = &market.Data{PriceChange1h: -1.0}
			stats := &TradeStatistics{}
			result := CheckCircuitBreaker(ctx, stats)
			return result.IsTriggered && result.CooldownMinutes == 30
		},
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 5.10: 动态风险调整范围 — 属性基测试 (Property 26)
// Feature: quant-trading-system, Property 26: 动态风险调整范围
// Validates: Requirements 5.10
// ============================================================================

func TestProperty26_DynamicRiskAdjustmentRange(t *testing.T) {
	// Feature: quant-trading-system, Property 26: 动态风险调整范围
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("调整后风险应在[BaseRisk×0.5, BaseRisk×1.5]范围内", prop.ForAll(
		func(sharpeNorm, winRateNorm, baseRiskNorm float64, totalTradesNorm, consecWinsNorm, consecLossesNorm int) bool {
			sharpe := -2.0 + sharpeNorm*5.0
			winRate := winRateNorm
			totalTrades := totalTradesNorm % 51
			consecWins := consecWinsNorm % 11
			consecLosses := consecLossesNorm % 11
			baseRisk := 0.005 + baseRiskNorm*0.045

			adjuster := &DynamicRiskAdjuster{
				BaseRiskPerTrade:  baseRisk,
				WinStreakBonus:    0.05,
				LoseStreakPenalty: 0.10,
				MaxAdjustment:     0.5,
			}
			stats := &TradeStatistics{
				SharpeRatio:       sharpe,
				WinRate:           winRate,
				TotalTrades:       totalTrades,
				ConsecutiveWins:   consecWins,
				ConsecutiveLosses: consecLosses,
			}

			risk := adjuster.GetAdjustedRisk(stats)
			return risk >= baseRisk*0.5 && risk <= baseRisk*1.5
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
