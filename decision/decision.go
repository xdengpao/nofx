package decision

import (
	"fmt"
	"log"
	"nofx/market"
	"nofx/mcp"
	"nofx/pool"
	"strings"
	"time"
)

// ============================================================================
// Context 交易上下文
// ============================================================================

// Context 交易上下文
type Context struct {
	CurrentTime         string                      `json:"current_time"`
	RuntimeMinutes      int                         `json:"runtime_minutes"`
	CallCount           int                         `json:"call_count"`
	Account             AccountInfo                 `json:"account"`
	Positions           []PositionInfo              `json:"positions"`
	CandidateCoins      []CandidateCoin             `json:"candidate_coins"`
	MarketDataMap       map[string]*market.Data     `json:"-"`
	OITopDataMap        map[string]*OITopData       `json:"-"`
	CorrelationMap      map[string]*CorrelationData `json:"-"`
	CircuitBreaker      *CircuitBreakerState        `json:"-"`
	Performance         interface{}                 `json:"-"`
	BTCETHLeverage      int                         `json:"-"`
	AltcoinLeverage     int                         `json:"-"`
	MaxRiskPerTrade     float64                     `json:"-"`
	TotalRiskBudget     float64                     `json:"-"`
	LastAnalysisTime    time.Time                   `json:"-"`
	AnalysisIntervalMin int                         `json:"-"`
}

// ============================================================================
// 核心决策函数
// ============================================================================

// GetFullDecision 获取AI的完整交易决策
func GetFullDecision(ctx *Context, mcpClient *mcp.Client) (*FullDecision, error) {
	initializeDefaults(ctx)

	// 检查熔断状态
	if result := checkCircuitBreakerState(ctx); result != nil {
		return result, nil
	}

	// 获取市场数据
	if err := fetchMarketDataForContext(ctx); err != nil {
		return nil, fmt.Errorf("获取市场数据失败: %w", err)
	}

	// 检查是否触发熔断
	stats := GetStatistics()
	cb := CheckCircuitBreaker(ctx, stats)
	if cb.IsTriggered {
		// CheckCircuitBreaker 内部已调用 SetCircuitBreakerState，
		// 此处同步到 ctx 以便日志输出
		ctx.CircuitBreaker = cb
		return &FullDecision{
			CoTTrace: "🛑 触发熔断保护，暂停交易",
			Decisions: []Decision{{
				Symbol:    "ALL",
				Action:    "wait",
				Reasoning: cb.TriggerReason,
			}},
			Timestamp: time.Now(),
		}, nil
	}

	// 计算相关性矩阵
	CalculateCorrelationMatrix(ctx)

	// 评估现有持仓
	positionDecisions := evaluateExistingPositions(ctx)

	// 判断是否需要调用AI
	shouldCallAI := shouldCallAIForNewOpportunities(ctx)

	var aiDecisions []Decision
	var cotTrace string

	if shouldCallAI {
		remainingBudget := calculateRemainingRiskBudget(ctx)
		if remainingBudget <= 0 {
			log.Printf("⚠️ 风险预算已用尽(剩余%.2f%%)，跳过新机会搜索", remainingBudget*100)
		} else {
			systemPrompt := buildSystemPrompt(ctx)
			userPrompt := buildUserPrompt(ctx, remainingBudget)

			aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
			if err != nil {
				log.Printf("⚠️ 调用AI API失败: %v", err)
			} else {
				aiDecisions, cotTrace, _ = ExtractDecisionsRobust(aiResponse)

				var validDecisions []Decision
				for _, d := range aiDecisions {
					if d.Action == "open_long" || d.Action == "open_short" {
						// 补充缺失参数
						if err := ValidateAndEnrichDecision(&d, ctx); err != nil {
							log.Printf("⚠️ 决策参数补充失败: %v", err)
						}

						if err := validateOpenDecision(&d, ctx); err != nil {
							log.Printf("⚠️ 开仓决策验证失败: %v", err)
							continue
						}
						validDecisions = append(validDecisions, d)
					} else if d.Action == "wait" {
						validDecisions = append(validDecisions, d)
					}
				}
				aiDecisions = validDecisions
			}
		}

		ctx.LastAnalysisTime = time.Now()
	}

	allDecisions := mergeDecisions(positionDecisions, aiDecisions)

	if err := validateFinalDecisions(allDecisions, ctx); err != nil {
		log.Printf("⚠️ 决策验证警告: %v", err)
	}

	// 统一生成完整的 CoTTrace（包含所有决策来源）
	finalCoTTrace := buildFinalCoTTrace(cotTrace, positionDecisions, aiDecisions, allDecisions)

	return &FullDecision{
		CoTTrace:  finalCoTTrace,
		Decisions: allDecisions,
		Timestamp: time.Now(),
	}, nil
}

// ============================================================================
// 初始化和检查
// ============================================================================

func initializeDefaults(ctx *Context) {
	if ctx.MaxRiskPerTrade == 0 {
		ctx.MaxRiskPerTrade = 0.02
	}
	if ctx.TotalRiskBudget == 0 {
		ctx.TotalRiskBudget = 0.08
	}
	if ctx.AnalysisIntervalMin == 0 {
		ctx.AnalysisIntervalMin = 15
	}
}

func checkCircuitBreakerState(ctx *Context) *FullDecision {
	// 从全局状态读取熔断信息，不再依赖 ctx.CircuitBreaker
	cbState := GetCircuitBreakerState()
	if cbState != nil && cbState.IsTriggered {
		cooldownEnd := cbState.TriggerTime.Add(
			time.Duration(cbState.CooldownMinutes) * time.Minute)
		if time.Now().Before(cooldownEnd) {
			remainingMinutes := int(cooldownEnd.Sub(time.Now()).Minutes())
			return &FullDecision{
				CoTTrace: fmt.Sprintf("⚠️ 熔断中: %s | 剩余冷却时间: %d分钟",
					cbState.TriggerReason, remainingMinutes),
				Decisions: []Decision{{
					Symbol:    "ALL",
					Action:    "wait",
					Reasoning: fmt.Sprintf("熔断保护触发: %s", cbState.TriggerReason),
				}},
				Timestamp: time.Now(),
			}
		}
		// 冷却已过期，清除全局状态
		SetCircuitBreakerState(nil)
	}
	return nil
}

// ============================================================================
// 持仓评估
// ============================================================================

func evaluateExistingPositions(ctx *Context) []Decision {
	var decisions []Decision

	for _, pos := range ctx.Positions {
		plan := planManager.GetPlan(pos.Symbol)
		marketData := ctx.MarketDataMap[pos.Symbol]

		evaluator := &PositionEvaluator{
			Position:   &pos,
			Plan:       plan,
			MarketData: marketData,
			Symbol:     pos.Symbol,
		}

		result := evaluator.Evaluate()

		// 更新峰值数据
		if result.ShouldUpdatePeak && marketData != nil {
			planManager.UpdatePlanPeakData(pos.Symbol, marketData.CurrentPrice, pos.UnrealizedPnLPct)
		}

		// 更新入场ATR
		if plan != nil && plan.EntryATR == 0 && marketData != nil {
			atr := 0.0
			if marketData.LongerTermContext != nil {
				atr = marketData.LongerTermContext.ATR14
			}
			if atr > 0 {
				planManager.UpdatePlanEntryATR(pos.Symbol, atr)
			}
		}

		// 更新动态止盈
		if result.NewTakeProfit > 0 {
			planManager.UpdatePlanTakeProfit(pos.Symbol, result.NewTakeProfit)
		}

		switch result.Action {
		case "close":
			action := "close_long"
			if pos.Side == "short" {
				action = "close_short"
			}
			decisions = append(decisions, Decision{
				Symbol:    pos.Symbol,
				Action:    action,
				Reasoning: result.Reason,
			})
			if result.IsPlanInvalidated && plan != nil {
				planManager.UpdatePlan(pos.Symbol, func(p *TradePlan) {
					p.Status = "INVALIDATED"
				})
			}

		case "partial_close":
			decisions = append(decisions, Decision{
				Symbol:          pos.Symbol,
				Action:          "partial_close",
				ClosePercentage: result.ClosePercentage,
				NewStopLoss:     result.NewStopLoss,
				TrancheIndex:    result.TrancheIndex,
				Reasoning:       result.Reason,
			})

		case "update_stop_loss":
			decisions = append(decisions, Decision{
				Symbol:      pos.Symbol,
				Action:      "update_stop_loss",
				NewStopLoss: result.NewStopLoss,
				Reasoning:   result.Reason,
			})

		case "hold":
			decisions = append(decisions, Decision{
				Symbol:    pos.Symbol,
				Action:    "hold",
				Reasoning: result.Reason,
			})
		}
	}

	return decisions
}

// ============================================================================
// AI调用判断
// ============================================================================

func shouldCallAIForNewOpportunities(ctx *Context) bool {
	if !ctx.LastAnalysisTime.IsZero() {
		elapsed := time.Since(ctx.LastAnalysisTime).Minutes()
		if elapsed < float64(ctx.AnalysisIntervalMin) {
			log.Printf("📊 距离上次分析%.1f分钟，跳过AI调用(间隔%d分钟)", elapsed, ctx.AnalysisIntervalMin)
			return false
		}
	}

	if ctx.Account.PositionCount >= 3 {
		log.Printf("📊 持仓已满(%d/3)，跳过新机会搜索", ctx.Account.PositionCount)
		return false
	}

	remainingBudget := calculateRemainingRiskBudget(ctx)
	if remainingBudget <= 0.01 {
		log.Printf("📊 风险预算不足(剩余%.2f%%)，跳过新机会搜索", remainingBudget*100)
		return false
	}

	return true
}

func calculateRemainingRiskBudget(ctx *Context) float64 {
	usedRisk, _ := CalculateTotalRisk(ctx)
	return ctx.TotalRiskBudget - usedRisk
}

// ============================================================================
// 决策合并与验证
// ============================================================================

func mergeDecisions(positionDecisions, aiDecisions []Decision) []Decision {
	decisionMap := make(map[string]Decision)

	for _, d := range positionDecisions {
		decisionMap[d.Symbol] = d
	}

	for _, d := range aiDecisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			if existing, exists := decisionMap[d.Symbol]; exists {
				// 持仓评估决策（任何非 wait 的决策）优先于 AI 新开仓决策
				if existing.Action != "wait" {
					continue
				}
			}
			decisionMap[d.Symbol] = d
		} else if d.Action == "wait" && len(positionDecisions) == 0 {
			decisionMap[d.Symbol] = d
		}
	}

	var result []Decision
	for _, d := range decisionMap {
		result = append(result, d)
	}

	return result
}

func validateOpenDecision(d *Decision, ctx *Context) error {
	// ========== 开仓前失效条件预检查 ==========
	marketData, ok := ctx.MarketDataMap[d.Symbol]
	if !ok {
		return fmt.Errorf("缺少 %s 市场数据", d.Symbol)
	}

	// 开仓前失效条件检查
	if invalidated, reason := CheckPreOpenInvalidation(d, marketData); invalidated {
		return fmt.Errorf("开仓前失效条件检查失败: %s", reason)
	}

	// 检查是否已有持仓
	for _, pos := range ctx.Positions {
		if pos.Symbol == d.Symbol {
			return fmt.Errorf("%s 已有持仓，不能重复开仓", d.Symbol)
		}
	}

	// 检查风险预算
	remainingBudget := calculateRemainingRiskBudget(ctx)
	estimatedRisk := d.RiskUSD / ctx.Account.TotalEquity
	if estimatedRisk > remainingBudget {
		return fmt.Errorf("风险预算不足: 需要%.2f%%, 剩余%.2f%%", estimatedRisk*100, remainingBudget*100)
	}

	// 检查杠杆
	maxLeverage := ctx.AltcoinLeverage
	if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
		maxLeverage = ctx.BTCETHLeverage
	}
	if d.Leverage <= 0 || d.Leverage > maxLeverage {
		return fmt.Errorf("杠杆必须在1-%d之间: %d", maxLeverage, d.Leverage)
	}

	// 检查仓位
	if d.PositionSizeUSD <= 0 {
		return fmt.Errorf("仓位大小必须>0")
	}

	maxPositionValue := ctx.Account.AvailableBalance * float64(maxLeverage) * 0.9
	if d.PositionSizeUSD > maxPositionValue {
		log.Printf("⚠️ 自动调整仓位: %.0f → %.0f USD", d.PositionSizeUSD, maxPositionValue*0.9)
		d.PositionSizeUSD = maxPositionValue * 0.9
	}

	// 相关性调整
	if corr, ok := ctx.CorrelationMap[d.Symbol]; ok && corr.IsHighCorr {
		adjustedSize := d.PositionSizeUSD * corr.RiskWeight
		log.Printf("⚠️ 高相关性调整: %.0f → %.0f USD", d.PositionSizeUSD, adjustedSize)
		d.PositionSizeUSD = adjustedSize
	}

	// 检查止损止盈
	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return fmt.Errorf("止损止盈必须>0")
	}

	currentPrice := marketData.CurrentPrice
	var riskPct, rewardPct float64
	if d.Action == "open_long" {
		if d.StopLoss >= currentPrice || d.TakeProfit <= currentPrice {
			return fmt.Errorf("做多止损必须<当前价<止盈")
		}
		riskPct = (currentPrice - d.StopLoss) / currentPrice * 100
		rewardPct = (d.TakeProfit - currentPrice) / currentPrice * 100
	} else {
		if d.StopLoss <= currentPrice || d.TakeProfit >= currentPrice {
			return fmt.Errorf("做空止损必须>当前价>止盈")
		}
		riskPct = (d.StopLoss - currentPrice) / currentPrice * 100
		rewardPct = (currentPrice - d.TakeProfit) / currentPrice * 100
	}

	// 检查风险回报比
	tradingCost := 0.2
	netRewardPct := rewardPct - tradingCost
	riskRewardRatio := netRewardPct / riskPct

	if riskRewardRatio < 2.5 {
		return fmt.Errorf("风险回报比过低(%.2f:1 < 2.5:1)", riskRewardRatio)
	}

	// 检查单笔风险
	positionRiskUSD := d.PositionSizeUSD * (riskPct / 100)
	maxRiskUSD := ctx.Account.TotalEquity * ctx.MaxRiskPerTrade
	if positionRiskUSD > maxRiskUSD*1.01 {
		return fmt.Errorf("单笔风险(%.2f USD)超过上限(%.2f USD)", positionRiskUSD, maxRiskUSD)
	}

	d.RiskUSD = positionRiskUSD
	return nil
}

func validateFinalDecisions(decisions []Decision, ctx *Context) error {
	newPositions := 0
	for _, d := range decisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			newPositions++
		}
	}

	totalPositions := ctx.Account.PositionCount + newPositions
	if totalPositions > 3 {
		return fmt.Errorf("总持仓数量(%d)超过上限(3)", totalPositions)
	}

	return nil
}

// ValidateAndEnrichDecision 验证并补充决策参数
func ValidateAndEnrichDecision(d *Decision, ctx *Context) error {
	marketData, ok := ctx.MarketDataMap[d.Symbol]
	if !ok {
		return fmt.Errorf("缺少 %s 市场数据", d.Symbol)
	}

	currentPrice := marketData.CurrentPrice

	// 补充默认杠杆
	if d.Leverage <= 0 {
		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			d.Leverage = ctx.BTCETHLeverage
		} else {
			d.Leverage = ctx.AltcoinLeverage
		}
	}

	// 补充默认仓位
	if d.PositionSizeUSD <= 0 {
		isAltcoin := d.Symbol != "BTCUSDT" && d.Symbol != "ETHUSDT"
		atr := 0.0
		if marketData.LongerTermContext != nil {
			atr = marketData.LongerTermContext.ATR14
		}

		suggestedSize, _ := market.CalculateAdaptivePositionSize(
			ctx.Account.TotalEquity,
			atr,
			currentPrice,
			ctx.MaxRiskPerTrade,
			isAltcoin,
		)
		d.PositionSizeUSD = suggestedSize
	}

	// 补充止损止盈
	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		atr := currentPrice * 0.02
		if marketData.LongerTermContext != nil && marketData.LongerTermContext.ATR14 > 0 {
			atr = marketData.LongerTermContext.ATR14
		}

		isAltcoin := d.Symbol != "BTCUSDT" && d.Symbol != "ETHUSDT"
		multiplier := 1.8
		if isAltcoin {
			multiplier = 2.5
		}

		stopDistance := atr * multiplier

		if d.Action == "open_long" {
			if d.StopLoss <= 0 {
				d.StopLoss = currentPrice - stopDistance
			}
			if d.TakeProfit <= 0 {
				d.TakeProfit = currentPrice + stopDistance*3.5
			}
		} else {
			if d.StopLoss <= 0 {
				d.StopLoss = currentPrice + stopDistance
			}
			if d.TakeProfit <= 0 {
				d.TakeProfit = currentPrice - stopDistance*3.5
			}
		}
	}

	// 补充置信度
	if d.Confidence <= 0 {
		d.Confidence = 75
	}

	// 补充最小持仓时间
	if d.MinHoldMinutes <= 0 {
		d.MinHoldMinutes = 30
	}

	return nil
}

// ============================================================================
// 开仓前失效条件检查
// ============================================================================

// PreOpenInvalidationChecker 开仓前失效条件检查器
type PreOpenInvalidationChecker struct {
	Symbol       string
	Direction    string
	MarketData   *market.Data
	CurrentPrice float64
}

// CheckPreOpenInvalidation 开仓前综合检查失效条件
func CheckPreOpenInvalidation(d *Decision, marketData *market.Data) (bool, string) {
	if marketData == nil {
		return false, ""
	}

	direction := "long"
	if d.Action == "open_short" {
		direction = "short"
	}

	checker := &PreOpenInvalidationChecker{
		Symbol:       d.Symbol,
		Direction:    direction,
		MarketData:   marketData,
		CurrentPrice: marketData.CurrentPrice,
	}

	// 1. 检查失效价格
	if invalidated, reason := checker.CheckInvalidationPrice(d.InvalidationPrice); invalidated {
		return true, reason
	}

	// 2. 检查失效条件
	if invalidated, reason := checker.CheckInvalidationCondition(d.InvalidationCondition); invalidated {
		return true, reason
	}

	return false, ""
}

// CheckInvalidationPrice 检查失效价格是否已触发
func (c *PreOpenInvalidationChecker) CheckInvalidationPrice(invalidationPrice float64) (bool, string) {
	if invalidationPrice <= 0 {
		return false, ""
	}

	if c.Direction == "long" {
		// 做多时，如果当前价格已经低于失效价格，则不应开仓
		if c.CurrentPrice < invalidationPrice {
			return true, fmt.Sprintf("当前价格(%.4f)已低于失效价格(%.4f)，多单计划已失效",
				c.CurrentPrice, invalidationPrice)
		}
	} else {
		// 做空时，如果当前价格已经高于失效价格，则不应开仓
		if c.CurrentPrice > invalidationPrice {
			return true, fmt.Sprintf("当前价格(%.4f)已高于失效价格(%.4f)，空单计划已失效",
				c.CurrentPrice, invalidationPrice)
		}
	}

	return false, ""
}

// CheckInvalidationCondition 检查失效条件是否已触发
func (c *PreOpenInvalidationChecker) CheckInvalidationCondition(conditionStr string) (bool, string) {
	if conditionStr == "" {
		return false, ""
	}

	// 解析失效条件
	parsed := ParseInvalidationCondition(conditionStr)
	if !parsed.IsValid {
		log.Printf("⚠️ 开仓前检查: 失效条件解析失败: %s - %s", conditionStr, parsed.ParseError)
		return false, "" // 解析失败不阻止开仓，但记录警告
	}

	// 获取对应时间框架的上下文
	ctx := c.getContextForTimeframe(parsed.Timeframe)
	if ctx == nil {
		log.Printf("⚠️ 开仓前检查: 无法获取 %s 时间框架数据", parsed.Timeframe)
		return false, ""
	}

	// 根据条件类型检查
	switch parsed.Type {
	case ICT_EMA_CROSS_DOWN:
		return c.checkEMACrossDown(ctx, parsed)
	case ICT_EMA_CROSS_UP:
		return c.checkEMACrossUp(ctx, parsed)
	case ICT_PRICE_BELOW:
		return c.checkPriceBelow(ctx, parsed)
	case ICT_PRICE_ABOVE:
		return c.checkPriceAbove(ctx, parsed)
	case ICT_RSI_ABOVE:
		return c.checkRSIAbove(ctx, parsed)
	case ICT_RSI_BELOW:
		return c.checkRSIBelow(ctx, parsed)
	case ICT_ADX_BELOW:
		return c.checkADXBelow(ctx, parsed)
	case ICT_MACD_CROSS:
		return c.checkMACDCross(ctx, parsed)
	case ICT_TREND_REVERSAL:
		return c.checkTrendReversal(ctx, parsed)
	default:
		return false, ""
	}
}

// checkEMACrossDown 检查EMA死叉（开仓前）
func (c *PreOpenInvalidationChecker) checkEMACrossDown(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := c.getEMAValue(ctx, cond.Indicator)
	ema2 := c.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 对于多单，EMA死叉是失效信号
	if c.Direction == "long" && ema1 < ema2 {
		return true, fmt.Sprintf("%s EMA已死叉: %s(%.4f) < %s(%.4f)，多单计划已失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

// checkEMACrossUp 检查EMA金叉（开仓前）
func (c *PreOpenInvalidationChecker) checkEMACrossUp(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := c.getEMAValue(ctx, cond.Indicator)
	ema2 := c.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 对于空单，EMA金叉是失效信号
	if c.Direction == "short" && ema1 > ema2 {
		return true, fmt.Sprintf("%s EMA已金叉: %s(%.4f) > %s(%.4f)，空单计划已失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

// checkPriceBelow 检查价格跌破（开仓前）
func (c *PreOpenInvalidationChecker) checkPriceBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = c.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	// 对于多单，价格跌破是失效信号
	if c.Direction == "long" && ctx.CurrentPrice < targetPrice {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)已跌破%s(%.4f)，多单计划已失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

// checkPriceAbove 检查价格突破（开仓前）
func (c *PreOpenInvalidationChecker) checkPriceAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = c.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	// 对于空单，价格突破是失效信号
	if c.Direction == "short" && ctx.CurrentPrice > targetPrice {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)已突破%s(%.4f)，空单计划已失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

// checkRSIAbove 检查RSI超过阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkRSIAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 > cond.Threshold {
		// RSI超买：对于多单，极度超买(>80)是获利了结信号
		if c.Direction == "long" && cond.Threshold >= 80 {
			return true, fmt.Sprintf("%s RSI(%.1f) > %.0f 极度超买，多单计划已失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 注意：RSI超买对空单是有利的，不是失效信号
	}

	return false, ""
}

// checkRSIBelow 检查RSI低于阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkRSIBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 < cond.Threshold {
		// RSI超卖：对于空单，极度超卖(<20)是获利了结信号
		if c.Direction == "short" && cond.Threshold <= 20 {
			return true, fmt.Sprintf("%s RSI(%.1f) < %.0f 极度超卖，空单计划已失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 注意：RSI超卖对多单是有利的，不是失效信号
	}

	return false, ""
}

// checkADXBelow 检查ADX低于阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkADXBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.ADX14 < cond.Threshold {
		return true, fmt.Sprintf("%s ADX(%.1f) < %.0f 趋势过弱，计划已失效",
			cond.Timeframe, ctx.ADX14, cond.Threshold)
	}

	return false, ""
}

// checkMACDCross 检查MACD交叉（开仓前）
func (c *PreOpenInvalidationChecker) checkMACDCross(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.MACD == 0 && ctx.MACDSignal == 0 {
		return false, ""
	}

	if cond.Direction == "DOWN" {
		// MACD死叉
		if ctx.MACD < ctx.MACDSignal && ctx.MACDHist < 0 {
			if c.Direction == "long" {
				return true, fmt.Sprintf("%s MACD已死叉(MACD=%.4f < Signal=%.4f)，多单计划已失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	} else if cond.Direction == "UP" {
		// MACD金叉
		if ctx.MACD > ctx.MACDSignal && ctx.MACDHist > 0 {
			if c.Direction == "short" {
				return true, fmt.Sprintf("%s MACD已金叉(MACD=%.4f > Signal=%.4f)，空单计划已失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	}

	return false, ""
}

// checkTrendReversal 检查趋势反转（开仓前）
func (c *PreOpenInvalidationChecker) checkTrendReversal(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 {
		return false, ""
	}

	// ADX > 25 表示有明确趋势
	if ctx.ADX14 > 25 {
		if c.Direction == "long" && ctx.DIMinus > ctx.DIPlus {
			return true, fmt.Sprintf("%s 趋势已反转: DI-(%.1f) > DI+(%.1f)，多单计划已失效",
				cond.Timeframe, ctx.DIMinus, ctx.DIPlus)
		}
		if c.Direction == "short" && ctx.DIPlus > ctx.DIMinus {
			return true, fmt.Sprintf("%s 趋势已反转: DI+(%.1f) > DI-(%.1f)，空单计划已失效",
				cond.Timeframe, ctx.DIPlus, ctx.DIMinus)
		}
	}

	return false, ""
}

// getEMAValue 获取EMA值（开仓前检查器）
func (c *PreOpenInvalidationChecker) getEMAValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	default:
		return 0
	}
}

// getIndicatorValue 获取指标值（开仓前检查器）
func (c *PreOpenInvalidationChecker) getIndicatorValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
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

// getContextForTimeframe 获取指定时间框架的上下文
func (c *PreOpenInvalidationChecker) getContextForTimeframe(timeframe string) *InvalidationCheckContext {
	if c.MarketData == nil {
		return nil
	}

	ctx := &InvalidationCheckContext{
		CurrentPrice: c.CurrentPrice,
		Timeframe:    timeframe,
	}

	switch strings.ToUpper(timeframe) {
	case "4H":
		if c.MarketData.LongerTermContext != nil {
			ltc := c.MarketData.LongerTermContext
			ctx.EMA20 = ltc.EMA20
			ctx.EMA50 = ltc.EMA50
			ctx.BBUpper = ltc.BollingerUpper
			ctx.BBLower = ltc.BollingerLower
			ctx.RSI14 = market.GetLastValue(ltc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(ltc.ADXValues)
			ctx.DIPlus = market.GetLastValue(ltc.DIPlus)
			ctx.DIMinus = market.GetLastValue(ltc.DIMinus)
			ctx.MACD = market.GetLastValue(ltc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(ltc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(ltc.MACDHist)
		}

	case "1H":
		if c.MarketData.MidTermSeries1h != nil {
			mtc := c.MarketData.MidTermSeries1h
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
		} else {
			ctx.EMA20 = c.MarketData.CurrentEMA20
			ctx.EMA50 = c.MarketData.CurrentEMA50
			ctx.RSI14 = c.MarketData.CurrentRSI14
			ctx.ADX14 = c.MarketData.CurrentADX
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
			ctx.MACD = c.MarketData.CurrentMACD
		}

	case "15M", "30M":
		if c.MarketData.MidTermSeries15m != nil {
			mtc := c.MarketData.MidTermSeries15m
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
		} else {
			ctx.EMA20 = c.MarketData.CurrentEMA20
			ctx.EMA50 = c.MarketData.CurrentEMA50
			ctx.RSI14 = c.MarketData.CurrentRSI14
			ctx.ADX14 = c.MarketData.CurrentADX
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
			ctx.MACD = c.MarketData.CurrentMACD
		}

	default:
		ctx.EMA20 = c.MarketData.CurrentEMA20
		ctx.EMA50 = c.MarketData.CurrentEMA50
		ctx.RSI14 = c.MarketData.CurrentRSI14
		ctx.ADX14 = c.MarketData.CurrentADX
		ctx.DIPlus = c.MarketData.CurrentDIPlus
		ctx.DIMinus = c.MarketData.CurrentDIMinus
		ctx.MACD = c.MarketData.CurrentMACD
		if c.MarketData.IntradaySeries != nil {
			ctx.MACDSignal = market.GetLastValue(c.MarketData.IntradaySeries.MACDSignal)
			ctx.MACDHist = market.GetLastValue(c.MarketData.IntradaySeries.MACDHist)
		}
	}

	return ctx
}

// ============================================================================
// 创建交易计划
// ============================================================================

// CreateTradePlanFromDecision 从决策创建交易计划
func CreateTradePlanFromDecision(d *Decision, actualEntryPrice float64) *TradePlan {
	direction := "long"
	if d.Action == "open_short" {
		direction = "short"
	}

	// 解析失效条件
	var parsedCondition *ParsedInvalidationCondition
	if d.InvalidationCondition != "" {
		parsedCondition = ParseInvalidationCondition(d.InvalidationCondition)
		if !parsedCondition.IsValid {
			log.Printf("⚠️ 失效条件解析失败: %s - %s", d.InvalidationCondition, parsedCondition.ParseError)
		}
	}

	// 按比例调整止损止盈
	var adjustedSL, adjustedTP float64
	if direction == "long" {
		slPct := (d.StopLoss - actualEntryPrice) / actualEntryPrice
		tpPct := (d.TakeProfit - actualEntryPrice) / actualEntryPrice
		adjustedSL = actualEntryPrice * (1 + slPct)
		adjustedTP = actualEntryPrice * (1 + tpPct)
	} else {
		slPct := (d.StopLoss - actualEntryPrice) / actualEntryPrice
		tpPct := (d.TakeProfit - actualEntryPrice) / actualEntryPrice
		adjustedSL = actualEntryPrice * (1 + slPct)
		adjustedTP = actualEntryPrice * (1 + tpPct)
	}

	// 使用原始值如果调整后的值无效
	if adjustedSL <= 0 {
		adjustedSL = d.StopLoss
	}
	if adjustedTP <= 0 {
		adjustedTP = d.TakeProfit
	}

	plan := &TradePlan{
		ID:                          fmt.Sprintf("%s_%d", d.Symbol, time.Now().UnixNano()),
		Symbol:                      d.Symbol,
		Direction:                   direction,
		EntryPrice:                  actualEntryPrice,
		ActualEntry:                 actualEntryPrice,
		StopLoss:                    adjustedSL,
		TakeProfit:                  adjustedTP,
		OriginalTakeProfit:          adjustedTP,
		CurrentStopLoss:             adjustedSL,
		PositionSizeUSD:             d.PositionSizeUSD,
		Leverage:                    d.Leverage,
		EntryReason:                 d.Reasoning,
		InvalidationCondition:       d.InvalidationCondition,
		ParsedInvalidationCondition: parsedCondition,
		InvalidationPrice:           d.InvalidationPrice,
		MinHoldMinutes:              d.MinHoldMinutes,
		CreatedAt:                   time.Now(),
		Status:                      "ACTIVE",
		Confidence:                  d.Confidence,
		RiskUSD:                     d.RiskUSD,
		ExecutedTranches:            make(map[int]bool),
	}

	if plan.MinHoldMinutes == 0 {
		plan.MinHoldMinutes = 30
	}

	planManager.SetPlan(plan)
	return plan
}

// ============================================================================
// 市场数据获取
// ============================================================================

func fetchMarketDataForContext(ctx *Context) error {
	ctx.MarketDataMap = make(map[string]*market.Data)
	ctx.OITopDataMap = make(map[string]*OITopData)

	symbolSet := make(map[string]bool)
	symbolSet["BTCUSDT"] = true

	for _, pos := range ctx.Positions {
		symbolSet[pos.Symbol] = true
	}

	for _, coin := range ctx.CandidateCoins {
		symbolSet[coin.Symbol] = true
	}

	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	for symbol := range symbolSet {
		data, err := market.Get(symbol)
		if err != nil {
			log.Printf("⚠️ 获取 %s 数据失败: %v", symbol, err)
			continue
		}

		isExistingPosition := positionSymbols[symbol]
		if !isExistingPosition && data.OIValueUSD > 0 {
			oiValueInMillions := data.OIValueUSD / 1_000_000
			if oiValueInMillions < 15 {
				log.Printf("⚠️ %s OI价值过低(%.2fM USD < 15M)，跳过", symbol, oiValueInMillions)
				continue
			}
		}

		ctx.MarketDataMap[symbol] = data
	}

	oiPositions, err := pool.GetOITopPositions()
	if err == nil {
		for _, pos := range oiPositions {
			ctx.OITopDataMap[pos.Symbol] = &OITopData{
				Rank:              pos.Rank,
				OIDeltaPercent:    pos.OIDeltaPercent,
				OIDeltaValue:      pos.OIDeltaValue,
				PriceDeltaPercent: pos.PriceDeltaPercent,
				NetLong:           pos.NetLong,
				NetShort:          pos.NetShort,
			}
		}
	}

	return nil
}

// ============================================================================
// Prompt构建
// ============================================================================

func buildFinalCoTTrace(aiCotTrace string, positionDecisions, aiDecisions, allDecisions []Decision) string {
	var sb strings.Builder

	// 1. 如果有AI分析，先添加AI的思维链
	if aiCotTrace != "" {
		sb.WriteString(aiCotTrace)
		sb.WriteString("\n\n")
	}

	// 2. 如果有持仓管理决策，添加说明
	if len(positionDecisions) > 0 {
		hasNonHold := false
		for _, d := range positionDecisions {
			if d.Action != "hold" {
				hasNonHold = true
				break
			}
		}

		if hasNonHold {
			sb.WriteString("**📊 持仓管理决策**:\n")
			for _, d := range positionDecisions {
				if d.Action == "hold" {
					continue
				}
				sb.WriteString(fmt.Sprintf("- %s: %s - %s\n", d.Symbol, d.Action, d.Reasoning))
			}
			sb.WriteString("\n")
		}
	}

	// 3. 添加最终决策的JSON格式
	if len(allDecisions) > 0 {
		sb.WriteString("**📋 决策汇总**:\n")
		for _, d := range allDecisions {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", d.Symbol, d.Action))
		}
	}

	result := sb.String()
	if result == "" {
		return "无决策输出"
	}

	return result
}

func buildSystemPrompt(ctx *Context) string {
	var sb strings.Builder

	availableBalance := ctx.Account.AvailableBalance
	btcEthLeverage := ctx.BTCETHLeverage
	altcoinLeverage := ctx.AltcoinLeverage

	// 获取当前夏普比率
	sharpeRatio := CalculateSharpeRatio()

	sb.WriteString("你是专业的加密货币交易AI，核心目标是**最大化夏普比率**。\n\n")

	// 添加当前夏普比率状态
	if sharpeRatio != 0 {
		sb.WriteString(fmt.Sprintf("**当前策略夏普比率**: %.2f\n", sharpeRatio))
		if sharpeRatio < 0 {
			sb.WriteString("⚠️ 夏普比率为负，需要更加保守的策略\n")
		} else if sharpeRatio > 1.5 {
			sb.WriteString("✅ 夏普比率良好，可以适当增加交易频率\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("# 🎯 你的核心职责\n\n")
	sb.WriteString("**只负责寻找新的开仓机会**。持仓管理由系统自动执行。\n\n")
	sb.WriteString("**量化标准**:\n")
	sb.WriteString("- 每天2-4笔开仓\n")
	sb.WriteString("- 只输出高置信度(≥80)的开仓决策\n")
	sb.WriteString("- 没有好机会时，输出 `wait`\n\n")

	maxPositionForAltcoin := availableBalance * float64(altcoinLeverage) * 0.9
	maxPositionForBTCETH := availableBalance * float64(btcEthLeverage) * 0.9

	sb.WriteString("# ⚖️ 硬约束\n\n")
	sb.WriteString("| 约束 | 值 |\n")
	sb.WriteString("|------|----|\n")
	sb.WriteString("| 风险回报比 | ≥ 1:3 |\n")
	sb.WriteString("| 单笔风险 | ≤ 账户净值的2% |\n")
	sb.WriteString(fmt.Sprintf("| 仓位上限 | 山寨币 %.0f USD / BTC&ETH %.0f USD |\n", maxPositionForAltcoin, maxPositionForBTCETH))
	sb.WriteString("| OI价值 | ≥ 15M USD |\n\n")

	sb.WriteString("# 📋 开仓决策流程\n\n")
	sb.WriteString("1. **评估BTC趋势** → 确定大方向\n")
	sb.WriteString("2. **筛选候选币种** → ADX>25 + 趋势方向一致\n")
	sb.WriteString("3. **多时间框架确认** → 4h/1h/15m 信号对齐\n")
	sb.WriteString("4. **计算仓位** → ATR自适应 + 相关性调整\n")
	sb.WriteString("5. **设置止损止盈** → 止损=ATR×2.5, RR≥1:3.5\n")
	sb.WriteString("6. **定义失效条件** → 什么情况下计划失效\n\n")

	// 🆕 新增：失效条件格式说明
	sb.WriteString("# 🚫 失效条件格式（重要！）\n\n")
	sb.WriteString("使用**结构化格式**定义失效条件，系统会自动监控并触发平仓。\n\n")
	sb.WriteString("**格式**: `时间框架:条件类型:参数1:参数2`\n\n")
	sb.WriteString("**支持的条件类型**:\n")
	sb.WriteString("| 类型 | 格式 | 说明 | 示例 |\n")
	sb.WriteString("|------|------|------|------|\n")
	sb.WriteString("| EMA死叉 | `TF:EMA_CROSS_DOWN:EMA短:EMA长` | 短期EMA下穿长期EMA | `4H:EMA_CROSS_DOWN:EMA20:EMA50` |\n")
	sb.WriteString("| EMA金叉 | `TF:EMA_CROSS_UP:EMA短:EMA长` | 短期EMA上穿长期EMA | `4H:EMA_CROSS_UP:EMA20:EMA50` |\n")
	sb.WriteString("| 价格跌破 | `TF:PRICE_BELOW:指标或价格` | 价格跌破指定位置 | `4H:PRICE_BELOW:EMA50` |\n")
	sb.WriteString("| 价格突破 | `TF:PRICE_ABOVE:指标或价格` | 价格突破指定位置 | `1H:PRICE_ABOVE:95000` |\n")
	sb.WriteString("| RSI超买 | `TF:RSI_ABOVE:阈值` | RSI超过阈值 | `4H:RSI_ABOVE:70` |\n")
	sb.WriteString("| RSI超卖 | `TF:RSI_BELOW:阈值` | RSI低于阈值 | `4H:RSI_BELOW:30` |\n")
	sb.WriteString("| ADX减弱 | `TF:ADX_BELOW:阈值` | ADX低于阈值 | `4H:ADX_BELOW:20` |\n")
	sb.WriteString("| MACD交叉 | `TF:MACD_CROSS:方向` | MACD交叉 | `4H:MACD_CROSS:DOWN` |\n")
	sb.WriteString("| 趋势反转 | `TF:TREND_REVERSAL` | DI反转 | `4H:TREND_REVERSAL` |\n\n")
	sb.WriteString("**时间框架**: `4H`, `1H`, `15M`, `30M`\n\n")

	sb.WriteString("# 💵 波动率自适应仓位\n\n")
	sb.WriteString("```\n")
	sb.WriteString("止损距离 = ATR14 × 倍数（山寨2.5，BTC/ETH 1.8）\n")
	sb.WriteString("仓位大小 = (账户净值 × 2%) / 止损百分比\n")
	sb.WriteString("```\n\n")

	sb.WriteString("# 📐 相关性控制\n\n")
	sb.WriteString("- 高相关(ρ>0.8)：仓位×0.7\n")
	sb.WriteString("- 中相关(0.5<ρ<0.8)：仓位×0.85\n")
	sb.WriteString("- 同方向高相关持仓不超过2个\n\n")

	sb.WriteString("# 📤 输出格式\n\n")
	sb.WriteString("**第一步**: 简短分析（3-5句话）\n")
	sb.WriteString("**第二步**: JSON决策数组\n\n")

	sb.WriteString("**开仓决策JSON**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[\n")
	sb.WriteString("  {\n")
	sb.WriteString("    \"symbol\": \"BTCUSDT\",\n")
	sb.WriteString("    \"action\": \"open_long\",\n")
	sb.WriteString(fmt.Sprintf("    \"leverage\": %d,\n", btcEthLeverage))
	sb.WriteString("    \"position_size_usd\": 100,\n")
	sb.WriteString("    \"stop_loss\": 95000,\n")
	sb.WriteString("    \"take_profit\": 105000,\n")
	sb.WriteString("    \"confidence\": 85,\n")
	sb.WriteString("    \"risk_usd\": 10,\n")
	sb.WriteString("    \"invalidation_price\": 96000,\n")
	sb.WriteString("    \"invalidation_condition\": \"4H:EMA_CROSS_DOWN:EMA20:EMA50\",\n")
	sb.WriteString("    \"min_hold_minutes\": 30,\n")
	sb.WriteString("    \"reasoning\": \"BTC强势+4H突破+资金费率中性\"\n")
	sb.WriteString("  }\n")
	sb.WriteString("]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**空单示例**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[\n")
	sb.WriteString("  {\n")
	sb.WriteString("    \"symbol\": \"ETHUSDT\",\n")
	sb.WriteString("    \"action\": \"open_short\",\n")
	sb.WriteString(fmt.Sprintf("    \"leverage\": %d,\n", btcEthLeverage))
	sb.WriteString("    \"position_size_usd\": 80,\n")
	sb.WriteString("    \"stop_loss\": 3500,\n")
	sb.WriteString("    \"take_profit\": 3200,\n")
	sb.WriteString("    \"confidence\": 82,\n")
	sb.WriteString("    \"risk_usd\": 8,\n")
	sb.WriteString("    \"invalidation_price\": 3450,\n")
	sb.WriteString("    \"invalidation_condition\": \"4H:EMA_CROSS_UP:EMA20:EMA50\",\n")
	sb.WriteString("    \"min_hold_minutes\": 30,\n")
	sb.WriteString("    \"reasoning\": \"ETH弱势+4H跌破支撑+资金费率偏高\"\n")
	sb.WriteString("  }\n")
	sb.WriteString("]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**无机会时**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[{\"symbol\": \"ALL\", \"action\": \"wait\", \"reasoning\": \"无符合条件的机会\"}]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("---\n")
	sb.WriteString("**核心原则**: 宁可错过，不可做错 | 风险回报比≥1:3 | BTC是龙头 | 失效条件必须明确\n")

	return sb.String()
}

func buildUserPrompt(ctx *Context, remainingBudget float64) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("**时间**: %s | **周期**: #%d\n\n", ctx.CurrentTime, ctx.CallCount))

	// BTC状态
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		marketState, stateConfidence := market.GetMarketState(btcData)
		sb.WriteString("## 🪙 BTC状态\n")
		sb.WriteString(fmt.Sprintf("**价格**: %.2f | **趋势**: **%s** (置信度%d%%)\n",
			btcData.CurrentPrice, marketState, stateConfidence))
		sb.WriteString(fmt.Sprintf("**ADX**: %.1f | **DI+**: %.1f | **DI-**: %.1f\n",
			btcData.CurrentADX, btcData.CurrentDIPlus, btcData.CurrentDIMinus))
		sb.WriteString(fmt.Sprintf("**MACD**: %.4f | **RSI14**: %.1f | **资金费率**: %.4f%%\n\n",
			btcData.CurrentMACD, btcData.CurrentRSI14, btcData.FundingRate*100))
	}

	// 账户状态
	sb.WriteString("## 💰 账户状态\n")
	sb.WriteString(fmt.Sprintf("**净值**: %.2f USDT | **可用**: %.2f USDT\n",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("**剩余风险预算**: **%.1f%%** (可开仓风险额度)\n",
		remainingBudget*100))
	sb.WriteString(fmt.Sprintf("**当前持仓**: %d/3\n", ctx.Account.PositionCount))

	// 显示夏普比率
	sharpeRatio := CalculateSharpeRatio()
	sortinoRatio := CalculateSortinoRatio()
	if sharpeRatio != 0 || sortinoRatio != 0 {
		sb.WriteString(fmt.Sprintf("**夏普比率**: %.2f | **索提诺比率**: %.2f\n", sharpeRatio, sortinoRatio))
	}
	sb.WriteString("\n")

	// 当前持仓
	if len(ctx.Positions) > 0 {
		sb.WriteString("## 📊 当前持仓（仅供参考）\n")
		for _, pos := range ctx.Positions {
			sb.WriteString(fmt.Sprintf("- %s %s: 盈亏 %+.2f%%\n",
				pos.Symbol, strings.ToUpper(pos.Side), pos.UnrealizedPnLPct))
		}
		sb.WriteString("\n")
	}

	// 候选币种
	sb.WriteString("## 🔍 候选币种\n\n")
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		if coin.Symbol == "BTCUSDT" {
			continue
		}
		hasPosition := false
		for _, pos := range ctx.Positions {
			if pos.Symbol == coin.Symbol {
				hasPosition = true
				break
			}
		}
		if hasPosition {
			continue
		}

		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		corrInfo := ""
		if corr, ok := ctx.CorrelationMap[coin.Symbol]; ok {
			corrInfo = fmt.Sprintf(" | BTC相关性: %.2f", corr.BTCCorr)
			if corr.IsHighCorr {
				corrInfo += "(高)"
			}
		}

		marketState, _ := market.GetMarketState(marketData)

		isAltcoin := coin.Symbol != "BTCUSDT" && coin.Symbol != "ETHUSDT"
		atr14 := 0.0
		if marketData.LongerTermContext != nil {
			atr14 = marketData.LongerTermContext.ATR14
		}
		suggestedSize, stopDist := market.CalculateAdaptivePositionSize(
			ctx.Account.TotalEquity,
			atr14,
			marketData.CurrentPrice,
			ctx.MaxRiskPerTrade,
			isAltcoin,
		)

		sb.WriteString(fmt.Sprintf("### %d. %s\n", displayedCount, coin.Symbol))
		sb.WriteString(fmt.Sprintf("**趋势**: %s%s\n", marketState, corrInfo))
		sb.WriteString(fmt.Sprintf("**建议仓位**: %.0f USD | **止损距离**: %.4f\n",
			suggestedSize, stopDist))
		sb.WriteString(market.FormatCompact(marketData))
		sb.WriteString("\n")

		if displayedCount >= 5 {
			break
		}
	}

	// 绩效指标
	stats := GetReturnsStats()
	if stats["count"] >= 5 {
		sb.WriteString("## 📈 绩效指标\n")
		sb.WriteString(fmt.Sprintf("**交易数**: %.0f | **胜率**: %.1f%%\n",
			stats["count"], stats["win_rate"]*100))
		sb.WriteString(fmt.Sprintf("**夏普比率**: %.2f | **索提诺比率**: %.2f\n\n",
			stats["sharpe_ratio"], stats["sortino_ratio"]))

		if stats["sharpe_ratio"] < -0.5 {
			sb.WriteString("⚠️ **夏普<-0.5**: 极其保守，只做置信度≥90的交易\n\n")
		} else if stats["sharpe_ratio"] < 0 {
			sb.WriteString("⚠️ **夏普<0**: 保守策略，只做置信度≥85的交易\n\n")
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("请分析并输出开仓决策（简短分析 + JSON）\n")

	return sb.String()
}

// ============================================================================
// 决策执行器
// ============================================================================

// DecisionExecutor 执行决策的接口定义
type DecisionExecutor interface {
	OpenPosition(symbol, side string, leverage int, sizeUSD, stopLoss, takeProfit float64) (*OpenPositionResult, error)
	ClosePosition(symbol string, percentage float64) error
	UpdateStopLoss(symbol string, newStopLoss float64) error
}

// ProcessDecisions 处理决策列表
func ProcessDecisions(decisions []Decision, executor DecisionExecutor, marketData map[string]*market.Data) error {
	for _, d := range decisions {
		var err error

		switch d.Action {
		case "open_long":
			// ✅ 执行开仓并获取实际成交信息
			result, execErr := executor.OpenPosition(d.Symbol, "long", d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
			if execErr == nil {
				OnPositionOpened(&d, result.EntryPrice, result.Quantity)
			}
			err = execErr

		case "open_short":
			result, execErr := executor.OpenPosition(d.Symbol, "short", d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
			if execErr == nil && result != nil {
				OnPositionOpened(&d, result.EntryPrice, result.Quantity)
			}
			err = execErr

		case "close_long", "close_short":
			err = executor.ClosePosition(d.Symbol, 100)
			if err == nil {
				// 🔧 使用增强版回调（如果有市场数据可以计算盈亏）
				if md, ok := marketData[d.Symbol]; ok {
					plan := planManager.GetPlan(d.Symbol)
					if plan != nil {
						var pnlPct float64
						if plan.Direction == "long" {
							pnlPct = (md.CurrentPrice - plan.EntryPrice) / plan.EntryPrice * 100
						} else {
							pnlPct = (plan.EntryPrice - md.CurrentPrice) / plan.EntryPrice * 100
						}
						OnPositionClosed(d.Symbol, md.CurrentPrice, pnlPct, 0, d.Reasoning)
					} else {
						OnPositionClosedSimple(d.Symbol, d.Reasoning)
					}
				} else {
					OnPositionClosedSimple(d.Symbol, d.Reasoning)
				}
			}

		case "partial_close":
			err = executor.ClosePosition(d.Symbol, d.ClosePercentage)
			if err == nil {
				// 🔧 使用档位索引
				OnPartialClose(d.Symbol, d.TrancheIndex, d.ClosePercentage, d.NewStopLoss)
			}

		case "update_stop_loss":
			err = executor.UpdateStopLoss(d.Symbol, d.NewStopLoss)
			if err == nil {
				OnStopLossUpdated(d.Symbol, d.NewStopLoss)
			}

		case "hold", "wait":
			continue
		}

		if err != nil {
			log.Printf("❌ 执行 %s %s 失败: %v", d.Symbol, d.Action, err)
		}
	}

	return nil
}

// ============================================================================
// 初始化和关闭
// ============================================================================

// Initialize 初始化决策模块
func Initialize(config *Config) error {
	if config == nil {
		config = &Config{
			MaxRiskPerTrade:     0.02,
			TotalRiskBudget:     0.08,
			AnalysisIntervalMin: 15,
			BTCETHLeverage:      10,
			AltcoinLeverage:     5,
			DataDir:             defaultDataDir,
			RiskFreeRate:        0.0,
		}
	}

	// 初始化计划管理器（带持久化）
	if err := InitPlanManager(config.DataDir); err != nil {
		return fmt.Errorf("初始化计划管理器失败: %w", err)
	}

	// 设置夏普比率配置
	SetSharpeConfig(SharpeConfig{
		RiskFreeRate:     config.RiskFreeRate,
		AnnualizeFactor:  252,
		MinTradesForCalc: 10,
	})

	log.Printf("📊 决策模块初始化: 单笔风险=%.1f%%, 总预算=%.1f%%, 分析间隔=%d分钟",
		config.MaxRiskPerTrade*100, config.TotalRiskBudget*100, config.AnalysisIntervalMin)

	// 输出当前统计
	stats := GetStatistics()
	if stats.TotalTrades > 0 {
		log.Printf("📊 恢复历史统计: 总交易=%d, 胜率=%.1f%%, 夏普=%.2f",
			stats.TotalTrades, stats.WinRate*100, stats.SharpeRatio)
	}

	return nil
}

// Shutdown 关闭决策模块
func Shutdown() error {
	if planManager != nil {
		if err := planManager.ForceSave(); err != nil {
			return fmt.Errorf("保存数据失败: %w", err)
		}
		log.Printf("📂 决策模块数据已保存")
	}
	return nil
}

// ============================================================================
// 计划同步
// ============================================================================

// SyncPlansFromPositions 从现有持仓同步计划
func SyncPlansFromPositions(positions []PositionInfo, marketDataMap map[string]*market.Data) {
	for _, pos := range positions {
		if planManager.GetPlan(pos.Symbol) != nil {
			continue
		}

		marketData, ok := marketDataMap[pos.Symbol]
		if !ok {
			log.Printf("⚠️ 无法为 %s 创建恢复计划：缺少市场数据", pos.Symbol)
			continue
		}

		atr := 0.0
		if marketData.LongerTermContext != nil {
			atr = marketData.LongerTermContext.ATR14
		}

		isAltcoin := pos.Symbol != "BTCUSDT" && pos.Symbol != "ETHUSDT"
		multiplier := 1.8
		if isAltcoin {
			multiplier = 2.5
		}
		stopDistance := atr * multiplier

		var stopLoss, takeProfit float64
		if pos.Side == "long" {
			stopLoss = pos.EntryPrice - stopDistance
			takeProfit = pos.EntryPrice + stopDistance*3.5
		} else {
			stopLoss = pos.EntryPrice + stopDistance
			takeProfit = pos.EntryPrice - stopDistance*3.5
		}

		if pos.StopLoss > 0 {
			stopLoss = pos.StopLoss
		}
		if pos.TakeProfit > 0 {
			takeProfit = pos.TakeProfit
		}

		plan := &TradePlan{
			ID:               fmt.Sprintf("recovered_%s_%d", pos.Symbol, time.Now().UnixNano()),
			Symbol:           pos.Symbol,
			Direction:        pos.Side,
			EntryPrice:       pos.EntryPrice,
			StopLoss:         stopLoss,
			TakeProfit:       takeProfit,
			CurrentStopLoss:  stopLoss,
			PositionSizeUSD:  pos.MarginUsed * float64(pos.Leverage),
			Leverage:         pos.Leverage,
			EntryReason:      "从现有持仓恢复",
			MinHoldMinutes:   0,
			CreatedAt:        time.UnixMilli(pos.UpdateTime),
			Status:           "ACTIVE",
			ExecutedTranches: make(map[int]bool),
		}

		planManager.SetPlan(plan)
		log.Printf("📋 从持仓恢复交易计划: %s %s @ %.4f, SL=%.4f, TP=%.4f",
			pos.Symbol, pos.Side, pos.EntryPrice, stopLoss, takeProfit)
	}
}

// ============================================================================
// 状态查询
// ============================================================================

// GetPlanStatus 获取计划状态摘要
func GetPlanStatus() string {
	plans := GetAllPlans()
	if len(plans) == 0 {
		return "无活跃交易计划"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("活跃计划: %d个\n", len(plans)))

	for _, plan := range plans {
		holdingTime := time.Since(plan.CreatedAt).Minutes()

		// 显示峰值信息
		peakInfo := ""
		if plan.PeakPnLPercent > 0 {
			peakInfo = fmt.Sprintf(", 峰值盈利=%.2f%%", plan.PeakPnLPercent)
		}

		// 显示分批止盈进度
		trancheInfo := ""
		if plan.TotalClosedPercent > 0 {
			trancheInfo = fmt.Sprintf(", 已平仓%.0f%%", plan.TotalClosedPercent)
		}

		sb.WriteString(fmt.Sprintf("  - %s %s: 持仓%.0f分钟, SL=%.4f, TP=%.4f%s%s\n",
			plan.Symbol, plan.Direction, holdingTime,
			plan.CurrentStopLoss, plan.TakeProfit,
			peakInfo, trancheInfo))

		if plan.InvalidationCondition != "" {
			formattedCond := FormatInvalidationCondition(plan.InvalidationCondition)
			sb.WriteString(fmt.Sprintf("    └─ 失效条件: %s\n", formattedCond))
		}
	}
	return sb.String()
}

// GetPlanBySymbol 根据symbol获取计划
func GetPlanBySymbol(symbol string) *TradePlan {
	return planManager.GetPlan(symbol)
}

// GetPerformanceReport 获取完整绩效报告
func GetPerformanceReport() string {
	var sb strings.Builder

	stats := GetStatistics()
	returnsStats := GetReturnsStats()

	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("           📊 绩效报告                  \n")
	sb.WriteString("═══════════════════════════════════════\n\n")

	sb.WriteString("【交易统计】\n")
	sb.WriteString(fmt.Sprintf("  总交易数: %d\n", stats.TotalTrades))
	sb.WriteString(fmt.Sprintf("  盈利交易: %d\n", stats.WinningTrades))
	sb.WriteString(fmt.Sprintf("  亏损交易: %d\n", stats.LosingTrades))
	sb.WriteString(fmt.Sprintf("  胜率: %.2f%%\n", stats.WinRate*100))
	sb.WriteString(fmt.Sprintf("  连续亏损: %d (最大: %d)\n\n", stats.ConsecutiveLosses, stats.MaxConsecLosses))

	sb.WriteString("【盈亏分析】\n")
	sb.WriteString(fmt.Sprintf("  总盈亏: %.2f%%\n", stats.TotalPnL))
	sb.WriteString(fmt.Sprintf("  平均盈利: %.2f%%\n", stats.AverageWin))
	sb.WriteString(fmt.Sprintf("  平均亏损: %.2f%%\n", stats.AverageLoss))
	sb.WriteString(fmt.Sprintf("  盈亏因子: %.2f\n\n", stats.ProfitFactor))

	sb.WriteString("【风险调整收益】\n")
	sb.WriteString(fmt.Sprintf("  夏普比率: %.2f\n", returnsStats["sharpe_ratio"]))
	sb.WriteString(fmt.Sprintf("  索提诺比率: %.2f\n", returnsStats["sortino_ratio"]))
	sb.WriteString(fmt.Sprintf("  最大回撤: %.2f%%\n\n", stats.MaxDrawdown))

	sb.WriteString("【其他指标】\n")
	sb.WriteString(fmt.Sprintf("  平均持仓时间: %.0f 分钟\n", stats.AverageHoldTime))
	sb.WriteString(fmt.Sprintf("  最后更新: %s\n", stats.LastUpdated.Format("2006-01-02 15:04:05")))

	sb.WriteString("\n═══════════════════════════════════════\n")

	sharpe := returnsStats["sharpe_ratio"]
	sb.WriteString("\n【夏普比率解读】\n")
	if sharpe > 2.0 {
		sb.WriteString("  ✅ 优秀 (>2.0): 策略表现非常好\n")
	} else if sharpe > 1.0 {
		sb.WriteString("  ✅ 良好 (1.0-2.0): 策略表现不错\n")
	} else if sharpe > 0 {
		sb.WriteString("  ⚠️ 一般 (0-1.0): 策略有改进空间\n")
	} else {
		sb.WriteString("  ❌ 较差 (<0): 策略需要调整\n")
	}

	return sb.String()
}

// DebugContext 输出调试信息
func DebugContext(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("=== 决策系统状态 ===\n")
	sb.WriteString(fmt.Sprintf("时间: %s\n", ctx.CurrentTime))
	sb.WriteString(fmt.Sprintf("运行时间: %d分钟\n", ctx.RuntimeMinutes))
	sb.WriteString(fmt.Sprintf("调用次数: %d\n", ctx.CallCount))
	sb.WriteString(fmt.Sprintf("上次分析: %s\n", ctx.LastAnalysisTime.Format("15:04:05")))
	sb.WriteString(fmt.Sprintf("分析间隔: %d分钟\n\n", ctx.AnalysisIntervalMin))

	sb.WriteString("=== 账户状态 ===\n")
	sb.WriteString(fmt.Sprintf("净值: %.2f USDT\n", ctx.Account.TotalEquity))
	sb.WriteString(fmt.Sprintf("可用: %.2f USDT\n", ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("保证金使用率: %.1f%%\n", ctx.Account.MarginUsedPct))
	sb.WriteString(fmt.Sprintf("持仓数: %d\n\n", ctx.Account.PositionCount))

	sb.WriteString("=== 风险预算 ===\n")
	usedRisk, _ := CalculateTotalRisk(ctx)
	remainingRisk := ctx.TotalRiskBudget - usedRisk
	sb.WriteString(fmt.Sprintf("总预算: %.1f%%\n", ctx.TotalRiskBudget*100))
	sb.WriteString(fmt.Sprintf("已用: %.1f%%\n", usedRisk*100))
	sb.WriteString(fmt.Sprintf("剩余: %.1f%%\n\n", remainingRisk*100))

	sb.WriteString("=== 绩效指标 ===\n")
	stats := GetStatistics()
	sb.WriteString(fmt.Sprintf("总交易: %d | 胜率: %.1f%%\n", stats.TotalTrades, stats.WinRate*100))
	sb.WriteString(fmt.Sprintf("夏普比率: %.2f | 索提诺比率: %.2f\n", stats.SharpeRatio, stats.SortinoRatio))
	sb.WriteString(fmt.Sprintf("盈亏因子: %.2f | 平均持仓: %.0f分钟\n\n", stats.ProfitFactor, stats.AverageHoldTime))

	sb.WriteString("=== 交易计划 ===\n")
	sb.WriteString(GetPlanStatus())

	if ctx.CircuitBreaker != nil && ctx.CircuitBreaker.IsTriggered {
		sb.WriteString("\n=== 熔断状态 ===\n")
		sb.WriteString(fmt.Sprintf("触发原因: %s\n", ctx.CircuitBreaker.TriggerReason))
		sb.WriteString(fmt.Sprintf("触发时间: %s\n", ctx.CircuitBreaker.TriggerTime.Format("15:04:05")))
		sb.WriteString(fmt.Sprintf("冷却时间: %d分钟\n", ctx.CircuitBreaker.CooldownMinutes))
	}

	return sb.String()
}

// ============================================================================
// 快捷分析
// ============================================================================

// QuickAnalyze 快速分析
func QuickAnalyze(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("=== 快速市场分析 ===\n\n")

	if btcData, ok := ctx.MarketDataMap["BTCUSDT"]; ok {
		state, conf := market.GetMarketState(btcData)
		sb.WriteString(fmt.Sprintf("**BTC**: %.2f | %s (%d%%)\n", btcData.CurrentPrice, state, conf))
		sb.WriteString(fmt.Sprintf("  ADX=%.1f | DI+=%.1f | DI-=%.1f | RSI=%.1f\n\n",
			btcData.CurrentADX, btcData.CurrentDIPlus, btcData.CurrentDIMinus, btcData.CurrentRSI14))
	}

	if len(ctx.Positions) > 0 {
		sb.WriteString("**持仓状态**:\n")
		for _, pos := range ctx.Positions {
			plan := planManager.GetPlan(pos.Symbol)
			planInfo := "无计划"
			if plan != nil {
				holdMin := time.Since(plan.CreatedAt).Minutes()
				planInfo = fmt.Sprintf("持仓%.0f分钟, SL=%.4f", holdMin, plan.CurrentStopLoss)
			}
			sb.WriteString(fmt.Sprintf("  %s %s: %+.2f%% | %s\n",
				pos.Symbol, pos.Side, pos.UnrealizedPnLPct, planInfo))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**候选币评分**:\n")
	for _, coin := range ctx.CandidateCoins {
		if coin.Symbol == "BTCUSDT" {
			continue
		}
		if data, ok := ctx.MarketDataMap[coin.Symbol]; ok {
			state, conf := market.GetMarketState(data)
			score := calculateCoinScore(data, ctx.CorrelationMap[coin.Symbol])
			sb.WriteString(fmt.Sprintf("  %s: %s(%d%%) | 评分=%.1f\n",
				coin.Symbol, state, conf, score))
		}
	}

	return sb.String()
}

func calculateCoinScore(data *market.Data, corr *CorrelationData) float64 {
	score := 50.0

	if data.CurrentADX > 25 {
		score += 15
	} else if data.CurrentADX > 20 {
		score += 8
	}

	if data.CurrentRSI14 > 30 && data.CurrentRSI14 < 70 {
		score += 10
	}

	if data.LongerTermContext != nil {
		pos := data.LongerTermContext.PricePosition
		if pos > 0.2 && pos < 0.8 {
			score += 10
		}
	}

	if corr != nil && corr.IsHighCorr {
		score -= 10
	}

	oiMil := data.OIValueUSD / 1_000_000
	if oiMil > 50 {
		score += 15
	} else if oiMil > 30 {
		score += 10
	} else if oiMil > 15 {
		score += 5
	}

	return score
}

// CalculateOptimalPosition 计算最优仓位
func CalculateOptimalPosition(ctx *Context, symbol string, side string) (positionSize, stopLoss, takeProfit float64, err error) {
	marketData, ok := ctx.MarketDataMap[symbol]
	if !ok {
		return 0, 0, 0, fmt.Errorf("缺少 %s 市场数据", symbol)
	}

	currentPrice := marketData.CurrentPrice
	atr14 := 0.0
	if marketData.LongerTermContext != nil {
		atr14 = marketData.LongerTermContext.ATR14
	}

	isAltcoin := symbol != "BTCUSDT" && symbol != "ETHUSDT"

	suggestedSize, stopDistance := market.CalculateAdaptivePositionSize(
		ctx.Account.TotalEquity,
		atr14,
		currentPrice,
		ctx.MaxRiskPerTrade,
		isAltcoin,
	)

	if corr, ok := ctx.CorrelationMap[symbol]; ok {
		suggestedSize *= corr.RiskWeight
	}

	if side == "long" {
		stopLoss = currentPrice - stopDistance
		takeProfit = currentPrice + stopDistance*3.5
	} else {
		stopLoss = currentPrice + stopDistance
		takeProfit = currentPrice - stopDistance*3.5
	}

	maxLeverage := ctx.AltcoinLeverage
	if !isAltcoin {
		maxLeverage = ctx.BTCETHLeverage
	}
	maxPositionValue := ctx.Account.AvailableBalance * float64(maxLeverage) * 0.9

	if suggestedSize > maxPositionValue {
		suggestedSize = maxPositionValue
	}

	return suggestedSize, stopLoss, takeProfit, nil
}

// GetTradingRecommendation 获取交易建议
func GetTradingRecommendation(ctx *Context, symbol string) string {
	marketData, ok := ctx.MarketDataMap[symbol]
	if !ok {
		return "缺少市场数据"
	}

	var recommendations []string

	state, confidence := market.GetMarketState(marketData)
	recommendations = append(recommendations, fmt.Sprintf("市场状态: %s (置信度%d%%)", state, confidence))

	if marketData.LongerTermContext != nil {
		if marketData.LongerTermContext.EMA20 > marketData.LongerTermContext.EMA50 {
			recommendations = append(recommendations, "4小时EMA: 多头排列")
		} else {
			recommendations = append(recommendations, "4小时EMA: 空头排列")
		}
	}

	fundingPct := marketData.FundingRate * 100
	if fundingPct > 0.05 {
		recommendations = append(recommendations, fmt.Sprintf("资金费率: %.4f%% (做空有利)", fundingPct))
	} else if fundingPct < -0.05 {
		recommendations = append(recommendations, fmt.Sprintf("资金费率: %.4f%% (做多有利)", fundingPct))
	}

	if corr, ok := ctx.CorrelationMap[symbol]; ok {
		if corr.IsHighCorr {
			recommendations = append(recommendations, fmt.Sprintf("BTC相关性: %.2f (高)", corr.BTCCorr))
		}
	}

	return strings.Join(recommendations, " | ")
}
