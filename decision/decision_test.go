package decision

// Feature: quant-trading-system
// 任务 15.1: 决策引擎核心逻辑测试覆盖
// 覆盖需求: 3.1, 3.2, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10

import (
	"nofx/market"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

// newTestContext 构造一个基础测试用 Context
func newTestContext() *Context {
	return &Context{
		Account: AccountInfo{
			TotalEquity:      10000.0,
			AvailableBalance: 8000.0,
			PositionCount:    0,
		},
		Positions:           []PositionInfo{},
		MarketDataMap:       map[string]*market.Data{},
		CorrelationMap:      map[string]*CorrelationData{},
		BTCETHLeverage:      10,
		AltcoinLeverage:     5,
		MaxRiskPerTrade:     0.02,
		TotalRiskBudget:     0.08,
		AnalysisIntervalMin: 3,
		LastAnalysisTime:    time.Time{},
	}
}

// newTestMarketData 构造测试用市场数据
func newTestMarketData(price float64) *market.Data {
	return &market.Data{
		CurrentPrice:   price,
		CurrentADX:     30.0,
		CurrentRSI14:   50.0,
		CurrentEMA20:   price * 1.01,
		CurrentEMA50:   price * 0.99,
		CurrentDIPlus:  25.0,
		CurrentDIMinus: 15.0,
		LongerTermContext: &market.LongerTermData{
			ATR14: price * 0.02,
			EMA20: price * 1.01,
			EMA50: price * 0.99,
		},
	}
}

// newOpenLongDecision 构造一个合法的做多开仓决策
func newOpenLongDecision(symbol string, price float64) *Decision {
	return &Decision{
		Symbol:          symbol,
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000.0,
		StopLoss:        price * 0.94,
		TakeProfit:      price * 1.20,
		Confidence:      80,
		MinHoldMinutes:  30,
	}
}

// ============================================================================
// 需求 3.5: validateOpenDecision — 风险回报比验证
// ============================================================================

func TestValidateOpenDecision_ValidDecision_Passes(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := newOpenLongDecision("BTCUSDT", price)
	if err := validateOpenDecision(d, ctx); err != nil {
		t.Errorf("合法决策应通过验证, 错误: %v", err)
	}
}

func TestValidateOpenDecision_LowRiskReward_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	// 止损 -5%, 止盈 +5% → 净回报 4.8% / 5% ≈ 0.96:1 < 2.5:1
	d := &Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        price * 0.95,
		TakeProfit:      price * 1.05,
	}
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("风险回报比不足时应返回错误")
	}
}

func TestValidateOpenDecision_StopLossAbovePrice_Long_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := &Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        price * 1.01,
		TakeProfit:      price * 1.20,
	}
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("做多止损高于当前价时应返回错误")
	}
}

func TestValidateOpenDecision_TakeProfitBelowPrice_Long_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := &Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        price * 0.94,
		TakeProfit:      price * 0.99,
	}
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("做多止盈低于当前价时应返回错误")
	}
}

func TestValidateOpenDecision_DuplicatePosition_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)
	ctx.Positions = []PositionInfo{{Symbol: "BTCUSDT", Side: "BUY"}}

	d := newOpenLongDecision("BTCUSDT", price)
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("已有持仓时重复开仓应返回错误")
	}
}

func TestValidateOpenDecision_InvalidLeverage_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := newOpenLongDecision("BTCUSDT", price)
	d.Leverage = 0
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("杠杆为0时应返回错误")
	}
}

func TestValidateOpenDecision_ZeroPositionSize_Fails(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := newOpenLongDecision("BTCUSDT", price)
	d.PositionSizeUSD = 0
	if err := validateOpenDecision(d, ctx); err == nil {
		t.Error("仓位为0时应返回错误")
	}
}

func TestValidateOpenDecision_Short_ValidDecision_Passes(t *testing.T) {
	ctx := newTestContext()
	price := 50000.0
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(price)

	d := &Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_short",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        price * 1.06,
		TakeProfit:      price * 0.80,
	}
	if err := validateOpenDecision(d, ctx); err != nil {
		t.Errorf("合法做空决策应通过验证, 错误: %v", err)
	}
}

// ============================================================================
// 需求 3.6: shouldCallAIForNewOpportunities — 频率控制和条件判断
// ============================================================================

func TestShouldCallAI_NoPriorAnalysis_ReturnsTrue(t *testing.T) {
	ctx := newTestContext()
	if !shouldCallAIForNewOpportunities(ctx) {
		t.Error("首次分析（无历史时间）应返回 true")
	}
}

func TestShouldCallAI_RecentAnalysis_ReturnsFalse(t *testing.T) {
	ctx := newTestContext()
	ctx.AnalysisIntervalMin = 3
	ctx.LastAnalysisTime = time.Now().Add(-1 * time.Minute)
	if shouldCallAIForNewOpportunities(ctx) {
		t.Error("距上次分析不足间隔时应返回 false")
	}
}

func TestShouldCallAI_IntervalElapsed_ReturnsTrue(t *testing.T) {
	ctx := newTestContext()
	ctx.AnalysisIntervalMin = 3
	ctx.LastAnalysisTime = time.Now().Add(-5 * time.Minute)
	if !shouldCallAIForNewOpportunities(ctx) {
		t.Error("超过分析间隔后应返回 true")
	}
}

func TestShouldCallAI_FullPositions_ReturnsFalse(t *testing.T) {
	ctx := newTestContext()
	ctx.Account.PositionCount = 3
	if shouldCallAIForNewOpportunities(ctx) {
		t.Error("持仓已满(3)时应返回 false")
	}
}

func TestShouldCallAI_InsufficientBudget_ReturnsFalse(t *testing.T) {
	ctx := newTestContext()
	ctx.TotalRiskBudget = 0.0
	if shouldCallAIForNewOpportunities(ctx) {
		t.Error("风险预算不足时应返回 false")
	}
}

// ============================================================================
// 需求 3.7: mergeDecisions — 优先级合并逻辑
// ============================================================================

func TestMergeDecisions_PositionDecisionTakesPriority(t *testing.T) {
	// hold/wait 持仓评估决策不会被 AI 开仓决策覆盖
	posDecisions := []Decision{
		{Symbol: "BTCUSDT", Action: "hold", Reasoning: "持仓中"},
	}
	aiDecisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long", Reasoning: "AI 看多"},
	}
	merged := mergeDecisions(posDecisions, aiDecisions)

	var btcDecision *Decision
	for i := range merged {
		if merged[i].Symbol == "BTCUSDT" {
			btcDecision = &merged[i]
			break
		}
	}
	if btcDecision == nil {
		t.Fatal("合并后应包含 BTCUSDT 决策")
	}
	// hold 时 AI 的 open_long 应被跳过
	if btcDecision.Action == "open_long" {
		t.Errorf("持仓评估为 hold 时 AI 开仓应被跳过, 实际=%s", btcDecision.Action)
	}
}

func TestMergeDecisions_DifferentSymbols_BothKept(t *testing.T) {
	posDecisions := []Decision{
		{Symbol: "ETHUSDT", Action: "hold"},
	}
	aiDecisions := []Decision{
		{Symbol: "SOLUSDT", Action: "open_long"},
	}
	merged := mergeDecisions(posDecisions, aiDecisions)

	hasSOL := false
	for _, d := range merged {
		if d.Symbol == "SOLUSDT" {
			hasSOL = true
		}
	}
	if !hasSOL {
		t.Error("不同币种的 AI 开仓决策应被保留")
	}
}

func TestMergeDecisions_SameSymbol_HoldThenAI_AISkipped(t *testing.T) {
	posDecisions := []Decision{
		{Symbol: "BTCUSDT", Action: "hold"},
	}
	aiDecisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
	}
	merged := mergeDecisions(posDecisions, aiDecisions)

	for _, d := range merged {
		if d.Symbol == "BTCUSDT" && d.Action == "open_long" {
			t.Error("同币种持仓评估为 hold 时，AI 开仓决策应被跳过")
		}
	}
}

func TestMergeDecisions_EmptyPositionDecisions_AIDecisionsKept(t *testing.T) {
	posDecisions := []Decision{}
	aiDecisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "open_short"},
	}
	merged := mergeDecisions(posDecisions, aiDecisions)
	if len(merged) != 2 {
		t.Errorf("无持仓评估时 AI 决策应全部保留, 实际=%d", len(merged))
	}
}

// ============================================================================
// 需求 3.9: validateFinalDecisions — 持仓数量限制
// ============================================================================

func TestValidateFinalDecisions_WithinLimit_Passes(t *testing.T) {
	ctx := newTestContext()
	ctx.Account.PositionCount = 1

	decisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "open_short"},
	}
	if err := validateFinalDecisions(decisions, ctx); err != nil {
		t.Errorf("总持仓3个应通过验证, 错误: %v", err)
	}
}

func TestValidateFinalDecisions_ExceedsLimit_Fails(t *testing.T) {
	ctx := newTestContext()
	ctx.Account.PositionCount = 2

	decisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "open_short"},
	}
	if err := validateFinalDecisions(decisions, ctx); err == nil {
		t.Error("总持仓超过3个时应返回错误")
	}
}

func TestValidateFinalDecisions_CloseDecisionsNotCounted(t *testing.T) {
	ctx := newTestContext()
	ctx.Account.PositionCount = 2

	decisions := []Decision{
		{Symbol: "BTCUSDT", Action: "close"},
		{Symbol: "ETHUSDT", Action: "open_long"},
	}
	if err := validateFinalDecisions(decisions, ctx); err != nil {
		t.Errorf("平仓决策不应计入持仓数量, 错误: %v", err)
	}
}

func TestValidateFinalDecisions_NoOpenDecisions_Passes(t *testing.T) {
	ctx := newTestContext()
	ctx.Account.PositionCount = 3

	decisions := []Decision{
		{Symbol: "BTCUSDT", Action: "close"},
		{Symbol: "ETHUSDT", Action: "hold"},
	}
	if err := validateFinalDecisions(decisions, ctx); err != nil {
		t.Errorf("无新开仓决策时应通过验证, 错误: %v", err)
	}
}

// ============================================================================
// 需求 3.8: CheckPreOpenInvalidation — 开仓前失效条件预检查
// ============================================================================

func TestCheckPreOpenInvalidation_NilMarketData_ReturnsFalse(t *testing.T) {
	d := newOpenLongDecision("BTCUSDT", 50000)
	invalidated, reason := CheckPreOpenInvalidation(d, nil)
	if invalidated {
		t.Errorf("nil 市场数据不应触发失效, reason=%s", reason)
	}
}

func TestCheckPreOpenInvalidation_NoConditions_ReturnsFalse(t *testing.T) {
	d := newOpenLongDecision("BTCUSDT", 50000)
	md := newTestMarketData(50000)
	invalidated, reason := CheckPreOpenInvalidation(d, md)
	if invalidated {
		t.Errorf("无失效条件时不应触发失效, reason=%s", reason)
	}
}

func TestCheckPreOpenInvalidation_InvalidationPrice_Long_Triggered(t *testing.T) {
	price := 50000.0
	d := newOpenLongDecision("BTCUSDT", price)
	d.InvalidationPrice = price * 1.01
	md := newTestMarketData(price)
	invalidated, reason := CheckPreOpenInvalidation(d, md)
	if !invalidated {
		t.Error("当前价低于失效价时多单应触发失效")
	}
	if reason == "" {
		t.Error("失效原因不应为空")
	}
}

func TestCheckPreOpenInvalidation_InvalidationPrice_Long_NotTriggered(t *testing.T) {
	price := 50000.0
	d := newOpenLongDecision("BTCUSDT", price)
	d.InvalidationPrice = price * 0.95
	md := newTestMarketData(price)
	invalidated, _ := CheckPreOpenInvalidation(d, md)
	if invalidated {
		t.Error("当前价高于失效价时多单不应触发失效")
	}
}

func TestCheckPreOpenInvalidation_InvalidationPrice_Short_Triggered(t *testing.T) {
	price := 50000.0
	d := &Decision{
		Symbol:            "BTCUSDT",
		Action:            "open_short",
		Leverage:          5,
		PositionSizeUSD:   1000,
		StopLoss:          price * 1.06,
		TakeProfit:        price * 0.80,
		InvalidationPrice: price * 0.99,
	}
	md := newTestMarketData(price)
	invalidated, reason := CheckPreOpenInvalidation(d, md)
	if !invalidated {
		t.Error("当前价高于失效价时空单应触发失效")
	}
	if reason == "" {
		t.Error("失效原因不应为空")
	}
}

func TestCheckPreOpenInvalidation_InvalidationPrice_Short_NotTriggered(t *testing.T) {
	price := 50000.0
	d := &Decision{
		Symbol:            "BTCUSDT",
		Action:            "open_short",
		Leverage:          5,
		PositionSizeUSD:   1000,
		StopLoss:          price * 1.06,
		TakeProfit:        price * 0.80,
		InvalidationPrice: price * 1.05,
	}
	md := newTestMarketData(price)
	invalidated, _ := CheckPreOpenInvalidation(d, md)
	if invalidated {
		t.Error("当前价低于失效价时空单不应触发失效")
	}
}

// ============================================================================
// 需求 3.4: ValidateAndEnrichDecision — 决策参数自动补充 - 属性基测试 (Property 9)
// Feature: quant-trading-system, Property 9: 决策参数自动补充
// Validates: Requirements 3.4
// ============================================================================

func TestProperty9_DecisionParamEnrichment(t *testing.T) {
	// Feature: quant-trading-system, Property 9: 决策参数自动补充
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 对任意缺少杠杆/仓位/止损/止盈的开仓决策，补充后所有字段应为正值
	properties.Property("开仓决策补充后所有字段应为正值", prop.ForAll(
		func(priceNorm float64, equityNorm float64, actionIdx int) bool {
			// 生成随机价格: 100 ~ 100000
			price := 100.0 + priceNorm*99900.0
			// 生成随机账户权益: 1000 ~ 100000
			equity := 1000.0 + equityNorm*99000.0

			// 交替测试 open_long 和 open_short
			actions := []string{"open_long", "open_short"}
			action := actions[actionIdx%2]

			// 构造缺少所有可选字段的决策（零值）
			d := &Decision{
				Symbol:          "BTCUSDT",
				Action:          action,
				Leverage:        0, // 缺失
				PositionSizeUSD: 0, // 缺失
				StopLoss:        0, // 缺失
				TakeProfit:      0, // 缺失
			}

			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:      equity,
					AvailableBalance: equity * 0.8,
					PositionCount:    0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": newTestMarketData(price),
				},
				CorrelationMap:      map[string]*CorrelationData{},
				BTCETHLeverage:      10,
				AltcoinLeverage:     5,
				MaxRiskPerTrade:     0.02,
				TotalRiskBudget:     0.08,
				AnalysisIntervalMin: 15,
			}

			if err := ValidateAndEnrichDecision(d, ctx); err != nil {
				return false
			}

			// 所有字段补充后应为正值
			return d.Leverage > 0 &&
				d.PositionSizeUSD > 0 &&
				d.StopLoss > 0 &&
				d.TakeProfit > 0
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 3.5: validateOpenDecision — 风险回报比验证 - 属性基测试 (Property 10)
// Feature: quant-trading-system, Property 10: 风险回报比验证
// Validates: Requirements 3.5
// ============================================================================

func TestProperty10_RiskRewardRatioValidation(t *testing.T) {
	// Feature: quant-trading-system, Property 10: 风险回报比验证
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 交易成本常量（与 validateOpenDecision 保持一致）
	const tradingCost = 0.2

	// 属性 10a: 当净风险回报比 < 2.5:1 时，validateOpenDecision 应返回错误
	properties.Property("净风险回报比<2.5时应返回错误", prop.ForAll(
		func(priceNorm float64, riskPctNorm float64, rrNorm float64, actionIdx int) bool {
			// 价格: 100 ~ 100000
			price := 100.0 + priceNorm*99900.0
			// 风险百分比: 1% ~ 10%
			riskPct := 1.0 + riskPctNorm*9.0
			// 净风险回报比: 0.1 ~ 2.499（严格小于 2.5）
			netRR := 0.1 + rrNorm*2.399
			// 反推止盈所需的 rewardPct: netRR = (rewardPct - tradingCost) / riskPct
			rewardPct := netRR*riskPct + tradingCost

			actions := []string{"open_long", "open_short"}
			action := actions[actionIdx%2]

			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:      100000.0,
					AvailableBalance: 80000.0,
					PositionCount:    0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": newTestMarketData(price),
				},
				CorrelationMap:  map[string]*CorrelationData{},
				BTCETHLeverage:  10,
				AltcoinLeverage: 5,
				MaxRiskPerTrade: 0.10, // 宽松风险上限，避免其他检查干扰
				TotalRiskBudget: 0.50,
			}

			var d *Decision
			if action == "open_long" {
				// 做多: stopLoss < price < takeProfit
				stopLoss := price * (1 - riskPct/100)
				takeProfit := price * (1 + rewardPct/100)
				d = &Decision{
					Symbol:          "BTCUSDT",
					Action:          "open_long",
					Leverage:        5,
					PositionSizeUSD: 1000.0,
					StopLoss:        stopLoss,
					TakeProfit:      takeProfit,
				}
			} else {
				// 做空: takeProfit < price < stopLoss
				stopLoss := price * (1 + riskPct/100)
				takeProfit := price * (1 - rewardPct/100)
				d = &Decision{
					Symbol:          "BTCUSDT",
					Action:          "open_short",
					Leverage:        5,
					PositionSizeUSD: 1000.0,
					StopLoss:        stopLoss,
					TakeProfit:      takeProfit,
				}
			}

			err := validateOpenDecision(d, ctx)
			// 净 RR < 2.5 时必须返回错误
			return err != nil
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
	))

	// 属性 10b: 当净风险回报比 >= 2.5:1 时，RR 检查不应阻止开仓
	properties.Property("净风险回报比>=2.5时RR检查不应返回错误", prop.ForAll(
		func(priceNorm float64, riskPctNorm float64, rrExcessNorm float64, actionIdx int) bool {
			// 价格: 100 ~ 100000
			price := 100.0 + priceNorm*99900.0
			// 风险百分比: 1% ~ 5%
			riskPct := 1.0 + riskPctNorm*4.0
			// 净风险回报比: 2.5 ~ 10.0（满足要求）
			netRR := 2.5 + rrExcessNorm*7.5
			// 反推止盈所需的 rewardPct
			rewardPct := netRR*riskPct + tradingCost

			actions := []string{"open_long", "open_short"}
			action := actions[actionIdx%2]

			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:      100000.0,
					AvailableBalance: 80000.0,
					PositionCount:    0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": newTestMarketData(price),
				},
				CorrelationMap:  map[string]*CorrelationData{},
				BTCETHLeverage:  10,
				AltcoinLeverage: 5,
				MaxRiskPerTrade: 0.10, // 宽松风险上限
				TotalRiskBudget: 0.50,
			}

			var d *Decision
			if action == "open_long" {
				stopLoss := price * (1 - riskPct/100)
				takeProfit := price * (1 + rewardPct/100)
				d = &Decision{
					Symbol:          "BTCUSDT",
					Action:          "open_long",
					Leverage:        5,
					PositionSizeUSD: 1000.0,
					StopLoss:        stopLoss,
					TakeProfit:      takeProfit,
				}
			} else {
				stopLoss := price * (1 + riskPct/100)
				takeProfit := price * (1 - rewardPct/100)
				d = &Decision{
					Symbol:          "BTCUSDT",
					Action:          "open_short",
					Leverage:        5,
					PositionSizeUSD: 1000.0,
					StopLoss:        stopLoss,
					TakeProfit:      takeProfit,
				}
			}

			err := validateOpenDecision(d, ctx)
			if err != nil {
				// 只允许非 RR 相关的错误（如单笔风险超限）
				return !strings.Contains(err.Error(), "风险回报比过低")
			}
			return true
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 3.6: shouldCallAIForNewOpportunities — 属性基测试 (Property 11)
// Feature: quant-trading-system, Property 11: 满仓或预算不足时跳过 AI 调用
// Validates: Requirements 3.6
// ============================================================================

func TestProperty11_SkipAIWhenFullOrBudgetInsufficient(t *testing.T) {
	// Feature: quant-trading-system, Property 11: 满仓或预算不足时跳过 AI 调用
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 属性 11a: 持仓 >= 3 时，shouldCallAIForNewOpportunities 应返回 false
	properties.Property("持仓>=3时应跳过AI调用", prop.ForAll(
		func(posCount int) bool {
			// posCount 范围: 3 ~ 10
			count := 3 + posCount%8
			ctx := newTestContext()
			ctx.Account.PositionCount = count
			// 确保其他条件不干扰（充足预算、超过间隔）
			ctx.TotalRiskBudget = 0.08
			ctx.LastAnalysisTime = time.Now().Add(-60 * time.Minute)
			return !shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 99),
	))

	// 属性 11b: 剩余风险预算 <= 1% 时，shouldCallAIForNewOpportunities 应返回 false
	// 通过将 TotalRiskBudget 设为 0（无预算）来模拟预算耗尽
	properties.Property("风险预算<=1%时应跳过AI调用", prop.ForAll(
		func(budgetNorm float64) bool {
			// budgetNorm 范围 [0,1)，映射到 [0, 0.01] 即 0%~1%
			budget := budgetNorm * 0.01
			ctx := newTestContext()
			ctx.Account.PositionCount = 0
			ctx.TotalRiskBudget = budget
			// 确保其他条件不干扰
			ctx.LastAnalysisTime = time.Now().Add(-60 * time.Minute)
			return !shouldCallAIForNewOpportunities(ctx)
		},
		gen.Float64Range(0, 1),
	))

	// 属性 11c: 持仓 < 3 且预算充足时，shouldCallAIForNewOpportunities 应返回 true（排除频率限制）
	properties.Property("持仓<3且预算充足时应允许AI调用", prop.ForAll(
		func(posCount int, budgetNorm float64) bool {
			// posCount: 0 ~ 2
			count := posCount % 3
			// budget: 2% ~ 8%（充足）
			budget := 0.02 + budgetNorm*0.06
			ctx := newTestContext()
			ctx.Account.PositionCount = count
			ctx.TotalRiskBudget = budget
			// 确保超过分析间隔
			ctx.LastAnalysisTime = time.Now().Add(-60 * time.Minute)
			return shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 99),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 3.7: mergeDecisions — 决策合并优先级 - 属性基测试 (Property 12)
// Feature: quant-trading-system, Property 12: 决策合并优先级
// Validates: Requirements 3.7
// ============================================================================

func TestProperty12_MergeDecisionsPriority(t *testing.T) {
	// Feature: quant-trading-system, Property 12: 决策合并优先级
	// 对任意持仓评估决策集合和 AI 新开仓决策集合，合并后对于同一币种，
	// 持仓评估决策（非 hold/wait）应优先于 AI 新开仓决策。
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 所有非 hold/wait 的持仓评估动作
	positionActions := []string{"close_long", "close_short", "partial_close", "update_stop_loss"}
	aiOpenActions := []string{"open_long", "open_short"}
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"}

	// 属性 12a: 同一币种的持仓评估决策（非 hold/wait）应优先于 AI 新开仓决策
	properties.Property("持仓评估决策(非hold/wait)应优先于AI新开仓决策", prop.ForAll(
		func(symbolIdx int, posActionIdx int, aiActionIdx int) bool {
			symbol := symbols[symbolIdx%len(symbols)]
			posAction := positionActions[posActionIdx%len(positionActions)]
			aiAction := aiOpenActions[aiActionIdx%len(aiOpenActions)]

			posDecisions := []Decision{
				{Symbol: symbol, Action: posAction, Reasoning: "持仓评估"},
			}
			aiDecisions := []Decision{
				{Symbol: symbol, Action: aiAction, Reasoning: "AI 新开仓"},
			}

			merged := mergeDecisions(posDecisions, aiDecisions)

			// 找到该币种的决策
			for _, d := range merged {
				if d.Symbol == symbol {
					// 持仓评估决策应保留，AI 开仓决策应被丢弃
					return d.Action == posAction
				}
			}
			// 如果没找到该币种决策，属性失败
			return false
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 12b: 持仓评估为 hold 时，AI 新开仓决策应被跳过（不覆盖 hold）
	properties.Property("持仓评估为hold时AI开仓决策应被跳过", prop.ForAll(
		func(symbolIdx int, aiActionIdx int) bool {
			symbol := symbols[symbolIdx%len(symbols)]
			aiAction := aiOpenActions[aiActionIdx%len(aiOpenActions)]

			posDecisions := []Decision{
				{Symbol: symbol, Action: "hold", Reasoning: "持仓中"},
			}
			aiDecisions := []Decision{
				{Symbol: symbol, Action: aiAction, Reasoning: "AI 新开仓"},
			}

			merged := mergeDecisions(posDecisions, aiDecisions)

			for _, d := range merged {
				if d.Symbol == symbol {
					// hold 决策应保留，AI 开仓不应覆盖
					return d.Action == "hold"
				}
			}
			return false
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 12c: 不同币种的决策互不干扰，均应保留
	properties.Property("不同币种的决策互不干扰", prop.ForAll(
		func(posActionIdx int, aiActionIdx int) bool {
			posAction := positionActions[posActionIdx%len(positionActions)]
			aiAction := aiOpenActions[aiActionIdx%len(aiOpenActions)]

			posDecisions := []Decision{
				{Symbol: "BTCUSDT", Action: posAction, Reasoning: "BTC 持仓评估"},
			}
			aiDecisions := []Decision{
				{Symbol: "ETHUSDT", Action: aiAction, Reasoning: "ETH AI 开仓"},
			}

			merged := mergeDecisions(posDecisions, aiDecisions)

			hasBTC, hasETH := false, false
			for _, d := range merged {
				if d.Symbol == "BTCUSDT" && d.Action == posAction {
					hasBTC = true
				}
				if d.Symbol == "ETHUSDT" && d.Action == aiAction {
					hasETH = true
				}
			}
			return hasBTC && hasETH
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 12d: 无持仓评估决策时，AI 开仓决策应全部保留
	properties.Property("无持仓评估时AI开仓决策应全部保留", prop.ForAll(
		func(countIdx int, aiActionIdx int) bool {
			// 生成 1~4 个不同币种的 AI 开仓决策
			count := 1 + countIdx%4
			aiAction := aiOpenActions[aiActionIdx%len(aiOpenActions)]

			var aiDecisions []Decision
			for i := 0; i < count; i++ {
				aiDecisions = append(aiDecisions, Decision{
					Symbol:    symbols[i],
					Action:    aiAction,
					Reasoning: "AI 新开仓",
				})
			}

			merged := mergeDecisions([]Decision{}, aiDecisions)

			if len(merged) != count {
				return false
			}
			// 验证每个 AI 决策都被保留
			mergedMap := make(map[string]string)
			for _, d := range merged {
				mergedMap[d.Symbol] = d.Action
			}
			for _, d := range aiDecisions {
				if mergedMap[d.Symbol] != d.Action {
					return false
				}
			}
			return true
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 3.8: CheckPreOpenInvalidation — 属性基测试 (Property 13)
// Feature: quant-trading-system, Property 13: 开仓前失效条件预检查
// Validates: Requirements 3.8
// ============================================================================

func TestProperty13_CheckPreOpenInvalidation(t *testing.T) {
	// Feature: quant-trading-system, Property 13: 开仓前失效条件预检查
	// 当失效条件已触发时，CheckPreOpenInvalidation 应返回 (true, 非空原因)
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 属性 13a: 多单失效价格已触发 → 返回 (true, 非空原因)
	// 当 invalidationPrice > currentPrice 时，多单失效条件已触发
	properties.Property("多单失效价格已触发时应返回true和非空原因", prop.ForAll(
		func(priceNorm float64, gapNorm float64) bool {
			// 当前价格: 100 ~ 100000
			price := 100.0 + priceNorm*99900.0
			// 失效价格高于当前价格 0.1% ~ 10%
			gap := 0.001 + gapNorm*0.099
			invalidationPrice := price * (1 + gap)

			d := &Decision{
				Symbol:            "BTCUSDT",
				Action:            "open_long",
				InvalidationPrice: invalidationPrice,
			}
			md := &market.Data{CurrentPrice: price}

			invalidated, reason := CheckPreOpenInvalidation(d, md)
			return invalidated && reason != ""
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 属性 13b: 空单失效价格已触发 → 返回 (true, 非空原因)
	// 当 invalidationPrice < currentPrice 时，空单失效条件已触发
	properties.Property("空单失效价格已触发时应返回true和非空原因", prop.ForAll(
		func(priceNorm float64, gapNorm float64) bool {
			// 当前价格: 100 ~ 100000
			price := 100.0 + priceNorm*99900.0
			// 失效价格低于当前价格 0.1% ~ 10%
			gap := 0.001 + gapNorm*0.099
			invalidationPrice := price * (1 - gap)

			d := &Decision{
				Symbol:            "BTCUSDT",
				Action:            "open_short",
				InvalidationPrice: invalidationPrice,
			}
			md := &market.Data{CurrentPrice: price}

			invalidated, reason := CheckPreOpenInvalidation(d, md)
			return invalidated && reason != ""
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 属性 13c: 多单 EMA 死叉已发生 → 返回 (true, 非空原因)
	// EMA20 < EMA50 表示死叉已发生，对多单是失效信号
	properties.Property("多单EMA死叉已发生时应返回true和非空原因", prop.ForAll(
		func(priceNorm float64, ema50Norm float64, gapNorm float64) bool {
			price := 100.0 + priceNorm*99900.0
			// EMA50 在价格附近
			ema50 := price * (0.95 + ema50Norm*0.1)
			// EMA20 低于 EMA50（死叉状态）
			gap := 0.001 + gapNorm*0.05
			ema20 := ema50 * (1 - gap)

			d := &Decision{
				Symbol:                "BTCUSDT",
				Action:                "open_long",
				InvalidationCondition: "4H:EMA_CROSS_DOWN:EMA20:EMA50",
			}
			md := &market.Data{
				CurrentPrice: price,
				LongerTermContext: &market.LongerTermData{
					EMA20: ema20,
					EMA50: ema50,
				},
			}

			invalidated, reason := CheckPreOpenInvalidation(d, md)
			return invalidated && reason != ""
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 属性 13d: 空单 EMA 金叉已发生 → 返回 (true, 非空原因)
	// EMA20 > EMA50 表示金叉已发生，对空单是失效信号
	properties.Property("空单EMA金叉已发生时应返回true和非空原因", prop.ForAll(
		func(priceNorm float64, ema50Norm float64, gapNorm float64) bool {
			price := 100.0 + priceNorm*99900.0
			ema50 := price * (0.95 + ema50Norm*0.1)
			// EMA20 高于 EMA50（金叉状态）
			gap := 0.001 + gapNorm*0.05
			ema20 := ema50 * (1 + gap)

			d := &Decision{
				Symbol:                "BTCUSDT",
				Action:                "open_short",
				InvalidationCondition: "4H:EMA_CROSS_UP:EMA20:EMA50",
			}
			md := &market.Data{
				CurrentPrice: price,
				LongerTermContext: &market.LongerTermData{
					EMA20: ema20,
					EMA50: ema50,
				},
			}

			invalidated, reason := CheckPreOpenInvalidation(d, md)
			return invalidated && reason != ""
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 属性 13e: 失效条件未触发时 → 返回 (false, _)
	// 多单且 EMA20 > EMA50（未死叉），失效条件不应触发
	properties.Property("多单EMA未死叉时失效条件不应触发", prop.ForAll(
		func(priceNorm float64, ema50Norm float64, gapNorm float64) bool {
			price := 100.0 + priceNorm*99900.0
			ema50 := price * (0.95 + ema50Norm*0.1)
			// EMA20 高于 EMA50（未死叉）
			gap := 0.001 + gapNorm*0.05
			ema20 := ema50 * (1 + gap)

			d := &Decision{
				Symbol:                "BTCUSDT",
				Action:                "open_long",
				InvalidationCondition: "4H:EMA_CROSS_DOWN:EMA20:EMA50",
			}
			md := &market.Data{
				CurrentPrice: price,
				LongerTermContext: &market.LongerTermData{
					EMA20: ema20,
					EMA50: ema50,
				},
			}

			invalidated, _ := CheckPreOpenInvalidation(d, md)
			return !invalidated
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 属性 13f: nil 市场数据时永远不触发失效（防御性）
	properties.Property("nil市场数据时不应触发失效", prop.ForAll(
		func(actionIdx int) bool {
			actions := []string{"open_long", "open_short"}
			d := &Decision{
				Symbol:                "BTCUSDT",
				Action:                actions[actionIdx%2],
				InvalidationPrice:     50000.0,
				InvalidationCondition: "4H:EMA_CROSS_DOWN:EMA20:EMA50",
			}
			invalidated, _ := CheckPreOpenInvalidation(d, nil)
			return !invalidated
		},
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 3.10: shouldCallAIForNewOpportunities — 属性基测试 (Property 14)
// Feature: quant-trading-system, Property 14: AI 调用频率控制
// Validates: Requirements 3.10
// ============================================================================

func TestProperty14_AICallFrequencyControl(t *testing.T) {
	// Feature: quant-trading-system, Property 14: AI 调用频率控制
	// 距离上次分析不足配置间隔时，shouldCallAIForNewOpportunities 应返回 false
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 属性 14a: 距上次分析时间 < 配置间隔时，应返回 false
	properties.Property("距上次分析不足间隔时应返回false", prop.ForAll(
		func(intervalMin int, elapsedFrac float64) bool {
			// 间隔: 1 ~ 60 分钟
			interval := 1 + intervalMin%60
			// 已过时间: 0 ~ (interval-1) 分钟（严格小于间隔）
			elapsedMinutes := elapsedFrac * float64(interval-1)
			if elapsedMinutes >= float64(interval) {
				elapsedMinutes = float64(interval) * 0.99
			}

			ctx := newTestContext()
			ctx.AnalysisIntervalMin = interval
			ctx.LastAnalysisTime = time.Now().Add(-time.Duration(elapsedMinutes * float64(time.Minute)))
			ctx.Account.PositionCount = 0
			ctx.TotalRiskBudget = 0.08

			return !shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 59),
		gen.Float64Range(0, 0.999),
	))

	// 属性 14b: 距上次分析时间 >= 配置间隔时，频率控制不应阻止 AI 调用
	properties.Property("超过分析间隔后频率控制不应阻止AI调用", prop.ForAll(
		func(intervalMin int, excessFrac float64) bool {
			// 间隔: 1 ~ 30 分钟
			interval := 1 + intervalMin%30
			// 已过时间: interval ~ interval*3 分钟（超过间隔）
			excessMinutes := float64(interval) * (1.0 + excessFrac*2.0)

			ctx := newTestContext()
			ctx.AnalysisIntervalMin = interval
			ctx.LastAnalysisTime = time.Now().Add(-time.Duration(excessMinutes * float64(time.Minute)))
			ctx.Account.PositionCount = 0
			ctx.TotalRiskBudget = 0.08

			return shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 29),
		gen.Float64Range(0, 1),
	))

	// 属性 14c: LastAnalysisTime 为零值时（首次运行），不受频率限制
	properties.Property("首次运行时不受频率限制", prop.ForAll(
		func(intervalMin int) bool {
			interval := 1 + intervalMin%60
			ctx := newTestContext()
			ctx.AnalysisIntervalMin = interval
			ctx.LastAnalysisTime = time.Time{} // 零值，表示从未分析过
			ctx.Account.PositionCount = 0
			ctx.TotalRiskBudget = 0.08

			return shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 59),
	))

	// 属性 14d: 频率控制与持仓数量限制相互独立
	// 即使超过分析间隔，持仓已满时仍应返回 false
	properties.Property("超过间隔但持仓已满时仍应返回false", prop.ForAll(
		func(intervalMin int) bool {
			interval := 1 + intervalMin%30
			ctx := newTestContext()
			ctx.AnalysisIntervalMin = interval
			ctx.LastAnalysisTime = time.Now().Add(-time.Duration(float64(interval)*2) * time.Minute)
			ctx.Account.PositionCount = 3
			ctx.TotalRiskBudget = 0.08

			return !shouldCallAIForNewOpportunities(ctx)
		},
		gen.IntRange(0, 29),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 5.1: validateOpenDecision — 单笔风险上限 - 属性基测试 (Property 21)
// Feature: quant-trading-system, Property 21: 单笔风险上限
// Validates: Requirements 5.1
// ============================================================================

func TestProperty21_SingleTradeRiskLimit(t *testing.T) {
	// Feature: quant-trading-system, Property 21: 单笔风险上限
	// 持仓风险超过账户净值 2% 时，validateOpenDecision 应返回错误
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 属性 21a: 当持仓风险 > 账户净值 × MaxRiskPerTrade 时，应返回错误
	// positionRiskUSD = positionSizeUSD × stopDistancePct
	// 当 positionRiskUSD > equity × 0.02 时，validateOpenDecision 应返回错误
	properties.Property("持仓风险超过账户净值2%时应返回错误", prop.ForAll(
		func(priceNorm float64, equityNorm float64, riskExcessNorm float64, actionIdx int) bool {
			// 价格: 1000 ~ 50000
			price := 1000.0 + priceNorm*49000.0
			// 账户净值: 10000 ~ 100000
			equity := 10000.0 + equityNorm*90000.0
			// 最大允许风险 USD = equity × 2%
			maxRiskUSD := equity * 0.02
			// 实际风险超出上限: 超出 1.1% ~ 50%（确保超过 1.01 倍容差）
			excessFactor := 1.011 + riskExcessNorm*0.489
			targetRiskUSD := maxRiskUSD * excessFactor

			actions := []string{"open_long", "open_short"}
			action := actions[actionIdx%2]

			// 止损距离: 2% ~ 8%（合理范围）
			stopDistancePct := 0.02 + (riskExcessNorm * 0.06)
			// 反推仓位大小: positionSizeUSD = targetRiskUSD / stopDistancePct
			positionSizeUSD := targetRiskUSD / stopDistancePct

			// 构造满足风险回报比要求的止损止盈（净 RR >= 2.5:1）
			// 使用宽松的止盈确保不被 RR 检查拦截
			var stopLoss, takeProfit float64
			if action == "open_long" {
				stopLoss = price * (1 - stopDistancePct)
				// 止盈: 确保净 RR >= 2.5（rewardPct = 2.5 × riskPct + tradingCost）
				rewardPct := 2.5*stopDistancePct + 0.002
				takeProfit = price * (1 + rewardPct)
			} else {
				stopLoss = price * (1 + stopDistancePct)
				rewardPct := 2.5*stopDistancePct + 0.002
				takeProfit = price * (1 - rewardPct)
			}

			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:      equity,
					AvailableBalance: equity * 0.9,
					PositionCount:    0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": newTestMarketData(price),
				},
				CorrelationMap:  map[string]*CorrelationData{},
				BTCETHLeverage:  10,
				AltcoinLeverage: 5,
				MaxRiskPerTrade: 0.02, // 2% 上限
				TotalRiskBudget: 0.50, // 宽松预算，避免预算检查干扰
			}

			d := &Decision{
				Symbol:          "BTCUSDT",
				Action:          action,
				Leverage:        5,
				PositionSizeUSD: positionSizeUSD,
				StopLoss:        stopLoss,
				TakeProfit:      takeProfit,
			}

			err := validateOpenDecision(d, ctx)
			// 风险超限时必须返回错误
			return err != nil
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
	))

	// 属性 21b: 当持仓风险 <= 账户净值 × MaxRiskPerTrade 时，风险上限检查不应阻止开仓
	properties.Property("持仓风险不超过账户净值2%时风险上限检查不应返回错误", prop.ForAll(
		func(priceNorm float64, equityNorm float64, riskFracNorm float64, actionIdx int) bool {
			// 价格: 1000 ~ 50000
			price := 1000.0 + priceNorm*49000.0
			// 账户净值: 10000 ~ 100000
			equity := 10000.0 + equityNorm*90000.0
			// 最大允许风险 USD = equity × 2%
			maxRiskUSD := equity * 0.02
			// 实际风险: 0% ~ 99% 的上限（严格不超过）
			targetRiskUSD := maxRiskUSD * (riskFracNorm * 0.99)

			actions := []string{"open_long", "open_short"}
			action := actions[actionIdx%2]

			// 止损距离: 2% ~ 5%
			stopDistancePct := 0.02 + (riskFracNorm * 0.03)
			// 反推仓位大小
			positionSizeUSD := targetRiskUSD / stopDistancePct
			if positionSizeUSD <= 0 {
				positionSizeUSD = 100.0
			}

			// 构造满足风险回报比要求的止损止盈
			var stopLoss, takeProfit float64
			if action == "open_long" {
				stopLoss = price * (1 - stopDistancePct)
				rewardPct := 2.5*stopDistancePct + 0.002
				takeProfit = price * (1 + rewardPct)
			} else {
				stopLoss = price * (1 + stopDistancePct)
				rewardPct := 2.5*stopDistancePct + 0.002
				takeProfit = price * (1 - rewardPct)
			}

			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:      equity,
					AvailableBalance: equity * 0.9,
					PositionCount:    0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": newTestMarketData(price),
				},
				CorrelationMap:  map[string]*CorrelationData{},
				BTCETHLeverage:  10,
				AltcoinLeverage: 5,
				MaxRiskPerTrade: 0.02,
				TotalRiskBudget: 0.50,
			}

			d := &Decision{
				Symbol:          "BTCUSDT",
				Action:          action,
				Leverage:        5,
				PositionSizeUSD: positionSizeUSD,
				StopLoss:        stopLoss,
				TakeProfit:      takeProfit,
			}

			err := validateOpenDecision(d, ctx)
			if err != nil {
				// 只允许非风险上限相关的错误
				return !strings.Contains(err.Error(), "单笔风险")
			}
			return true
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 5.2: validateFinalDecisions — 最大持仓数量限制 - 属性基测试 (Property 22)
// Feature: quant-trading-system, Property 22: 最大持仓数量限制
// Validates: Requirements 5.2
// ============================================================================

func TestProperty22_MaxPositionCountLimit(t *testing.T) {
	// Feature: quant-trading-system, Property 22: 最大持仓数量限制
	// 现有持仓 + 新开仓 > 3 时，validateFinalDecisions 应返回错误
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"}
	openActions := []string{"open_long", "open_short"}

	// 属性 22a: 现有持仓 + 新开仓 > 3 时，应返回错误
	properties.Property("现有持仓+新开仓>3时应返回错误", prop.ForAll(
		func(existingCount int, newCount int, actionIdx int) bool {
			// 现有持仓: 0 ~ 3
			existing := existingCount % 4
			// 新开仓: 1 ~ 4，确保总数 > 3
			newOpen := 1 + newCount%4
			// 只测试总数超过 3 的情况
			if existing+newOpen <= 3 {
				newOpen = 4 - existing
			}

			action := openActions[actionIdx%2]
			ctx := newTestContext()
			ctx.Account.PositionCount = existing

			var decisions []Decision
			for i := 0; i < newOpen && i < len(symbols); i++ {
				decisions = append(decisions, Decision{
					Symbol: symbols[i],
					Action: action,
				})
			}

			err := validateFinalDecisions(decisions, ctx)
			return err != nil
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 22b: 现有持仓 + 新开仓 <= 3 时，不应返回错误
	properties.Property("现有持仓+新开仓<=3时不应返回错误", prop.ForAll(
		func(existingCount int, newCount int, actionIdx int) bool {
			// 现有持仓: 0 ~ 3
			existing := existingCount % 4
			// 新开仓: 0 ~ (3 - existing)，确保总数 <= 3
			maxNew := 3 - existing
			newOpen := newCount % (maxNew + 1)

			action := openActions[actionIdx%2]
			ctx := newTestContext()
			ctx.Account.PositionCount = existing

			var decisions []Decision
			for i := 0; i < newOpen && i < len(symbols); i++ {
				decisions = append(decisions, Decision{
					Symbol: symbols[i],
					Action: action,
				})
			}

			err := validateFinalDecisions(decisions, ctx)
			return err == nil
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 22c: 平仓/持仓评估决策不计入持仓数量
	properties.Property("非开仓决策不应计入持仓数量限制", prop.ForAll(
		func(existingCount int, nonOpenCount int, actionIdx int) bool {
			// 现有持仓: 0 ~ 3
			existing := existingCount % 4
			// 非开仓决策数量: 1 ~ 5
			nonOpenNum := 1 + nonOpenCount%5
			nonOpenActions := []string{"close_long", "close_short", "hold", "update_stop_loss", "partial_close"}
			action := nonOpenActions[actionIdx%len(nonOpenActions)]

			ctx := newTestContext()
			ctx.Account.PositionCount = existing

			var decisions []Decision
			for i := 0; i < nonOpenNum && i < len(symbols); i++ {
				decisions = append(decisions, Decision{
					Symbol: symbols[i],
					Action: action,
				})
			}

			// 非开仓决策不应触发持仓数量限制
			err := validateFinalDecisions(decisions, ctx)
			return err == nil
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	// 属性 22d: 混合决策中只有开仓决策计入持仓数量
	properties.Property("混合决策中只有开仓决策计入持仓数量", prop.ForAll(
		func(existingCount int, openCount int, closeCount int, actionIdx int) bool {
			// 现有持仓: 0 ~ 2
			existing := existingCount % 3
			// 新开仓: 1 ~ 3
			newOpen := 1 + openCount%3
			// 平仓决策: 1 ~ 3（不应影响计数）
			closeNum := 1 + closeCount%3
			action := openActions[actionIdx%2]

			ctx := newTestContext()
			ctx.Account.PositionCount = existing

			var decisions []Decision
			// 添加开仓决策
			for i := 0; i < newOpen && i < len(symbols); i++ {
				decisions = append(decisions, Decision{
					Symbol: symbols[i],
					Action: action,
				})
			}
			// 添加平仓决策（使用不同的 symbol 索引避免重复）
			for i := 0; i < closeNum; i++ {
				decisions = append(decisions, Decision{
					Symbol: symbols[(newOpen+i)%len(symbols)],
					Action: "close_long",
				})
			}

			err := validateFinalDecisions(decisions, ctx)
			totalPositions := existing + newOpen
			if totalPositions > 3 {
				// 超过限制时应返回错误
				return err != nil
			}
			// 未超过限制时不应返回错误
			return err == nil
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
