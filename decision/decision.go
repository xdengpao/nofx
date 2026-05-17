package decision

import (
	"fmt"
	"log"
	"math"
	"nofx/logger"
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
	CurrentTime              string                             `json:"current_time"`
	TraderID                 string                             `json:"trader_id,omitempty"`
	Exchange                 string                             `json:"exchange,omitempty"`
	RuntimeMinutes           int                                `json:"runtime_minutes"`
	CallCount                int                                `json:"call_count"`
	Account                  AccountInfo                        `json:"account"`
	Positions                []PositionInfo                     `json:"positions"`
	CandidateCoins           []CandidateCoin                    `json:"candidate_coins"`
	MarketDataMap            map[string]*market.Data            `json:"-"`
	OITopDataMap             map[string]*OITopData              `json:"-"`
	CorrelationMap           map[string]*CorrelationData        `json:"-"`
	CircuitBreaker           *CircuitBreakerState               `json:"-"`
	Performance              interface{}                        `json:"-"`
	BTCETHLeverage           int                                `json:"-"`
	AltcoinLeverage          int                                `json:"-"`
	MaxRiskPerTrade          float64                            `json:"-"`
	EffectiveMaxRiskPerTrade float64                            `json:"-"`
	PerformanceGates         *logger.RollingPerformanceSnapshot `json:"-"`
	ExecutionQuality         *logger.ExecutionQualityStats      `json:"-"`
	TotalRiskBudget          float64                            `json:"-"`
	MaxDailyLossPct          float64                            `json:"-"`
	MaxAccountDrawdownPct    float64                            `json:"-"`
	LastAnalysisTime         time.Time                          `json:"-"`
	LastAIAttemptTime        time.Time                          `json:"-"`
	LastAISuccessTime        time.Time                          `json:"-"`
	AIBackoffUntil           time.Time                          `json:"-"`
	LastAIError              string                             `json:"-"`
	ConsecutiveAIFails       int                                `json:"-"`
	AnalysisIntervalMin      int                                `json:"-"`
	FrequencyPolicy          *FrequencyPolicy                   `json:"-"`
	FrequencyState           *FrequencyState                    `json:"-"`
	LossMode                 *LossModeState                     `json:"-"`
	StrategyRiskPolicy       *StrategyRiskPolicy                `json:"-"`
}

const defaultMaxAccountDrawdownPct = 20.0

var configuredMaxAccountDrawdownPct = defaultMaxAccountDrawdownPct

// ============================================================================
// 核心决策函数
// ============================================================================

// GetFullDecision 获取AI的完整交易决策
func GetFullDecision(ctx *Context, mcpClient *mcp.Client) (*FullDecision, error) {
	preparation, err := PrepareCycleContext(ctx, CyclePreparationOptions{})
	if err != nil {
		return nil, err
	}
	if preparation.HaltDecision != nil {
		return preparation.HaltDecision, nil
	}
	positionDecisions := preparation.PositionDecisions

	// 判断是否需要调用AI
	shouldCallAI := shouldCallAIForNewOpportunities(ctx)

	var aiDecisions []Decision
	var cotTrace string
	var userPrompt string
	var openRejections []OpenRejection

	if shouldCallAI {
		remainingBudget := calculateRemainingRiskBudget(ctx)
		if remainingBudget <= 0 {
			reason := fmt.Sprintf("风险预算已用尽(剩余%.2f%%)，跳过本周期新机会搜索", remainingBudget*100)
			log.Printf("⚠️ %s", reason)
			cotTrace = reason
			aiDecisions = []Decision{waitDecision(reason)}
		} else {
			systemPrompt := buildSystemPrompt(ctx)
			userPrompt = buildUserPrompt(ctx, remainingBudget)

			aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
			if err != nil {
				trace := fmt.Sprintf("AI API调用失败，已跳过本周期新开仓: %v", err)
				return &FullDecision{
					UserPrompt:      userPrompt,
					CoTTrace:        trace,
					AICallAttempted: true,
					AICallSucceeded: false,
					AIFailureReason: trace,
					Decisions: []Decision{{
						Symbol:    "ALL",
						Action:    "wait",
						Reasoning: trace,
					}},
					Timestamp: time.Now(),
				}, fmt.Errorf("AI API调用失败: %w", err)
			} else {
				parsedDecisions, parsedTrace, parseErr := ExtractDecisionsRobust(aiResponse)
				if parseErr != nil {
					trace := fmt.Sprintf("AI响应解析失败，已跳过本周期新开仓: %v\n响应摘要: %s",
						parseErr, truncateForDecisionLog(aiResponse, 500))
					return &FullDecision{
						UserPrompt:      userPrompt,
						CoTTrace:        trace,
						AICallAttempted: true,
						AICallSucceeded: false,
						AIFailureReason: trace,
						Decisions: []Decision{{
							Symbol:    "ALL",
							Action:    "wait",
							Reasoning: trace,
						}},
						Timestamp: time.Now(),
					}, fmt.Errorf("AI响应解析失败: %w", parseErr)
				}
				aiDecisions = parsedDecisions
				cotTrace = parsedTrace

				var validDecisions []Decision
				var rejectedReasons []string
				for _, d := range aiDecisions {
					if d.Action == "open_long" || d.Action == "open_short" {
						// 补充缺失参数
						if err := ValidateAndEnrichDecision(&d, ctx); err != nil {
							reason := fmt.Sprintf("%s %s 参数补充失败: %v", d.Symbol, d.Action, err)
							log.Printf("⚠️ 决策%s", reason)
							rejectedReasons = append(rejectedReasons, reason)
							openRejections = append(openRejections, OpenRejection{
								Symbol: d.Symbol,
								Action: d.Action,
								Reason: reason,
							})
							continue
						}

						if err := validateOpenDecision(&d, ctx); err != nil {
							reason := fmt.Sprintf("%s %s 被风控过滤: %v", d.Symbol, d.Action, err)
							log.Printf("⚠️ 开仓决策验证失败: %v", err)
							rejectedReasons = append(rejectedReasons, reason)
							openRejections = append(openRejections, buildOpenRejection(d, ctx, reason))
							continue
						}
						validDecisions = append(validDecisions, d)
					} else if d.Action == "wait" {
						validDecisions = append(validDecisions, d)
					}
				}
				if len(validDecisions) == 0 {
					reason := "AI未给出可执行交易决策，等待更高质量机会"
					if len(rejectedReasons) > 0 {
						reason = "AI开仓建议已全部被风控过滤，等待更高质量机会: " + strings.Join(rejectedReasons, "; ")
					}
					validDecisions = append(validDecisions, waitDecision(reason))
					if strings.TrimSpace(cotTrace) == "" {
						cotTrace = reason
					}
				}
				aiDecisions = validDecisions
			}
		}

		ctx.LastAnalysisTime = time.Now()
	} else {
		reason := describeAISkipReason(ctx)
		cotTrace = reason
		aiDecisions = []Decision{waitDecision(reason)}
	}

	allDecisions := mergeDecisions(positionDecisions, aiDecisions)
	var finalRejections []OpenRejection
	allDecisions, finalRejections = enforceFinalDecisionLimits(allDecisions, ctx)
	if len(finalRejections) > 0 {
		openRejections = append(openRejections, finalRejections...)
		reason := "最终风控拦截: " + strings.Join(openRejectionReasons(finalRejections), "; ")
		log.Printf("⚠️ %s", reason)
		if strings.TrimSpace(cotTrace) == "" {
			cotTrace = reason
		} else {
			cotTrace += "\n" + reason
		}
	}

	if err := validateFinalDecisions(allDecisions, ctx); err != nil {
		log.Printf("⚠️ 决策验证警告: %v", err)
	}

	// 统一生成完整的 CoTTrace（包含所有决策来源）
	finalCoTTrace := buildFinalCoTTrace(cotTrace, positionDecisions, aiDecisions, allDecisions)

	return &FullDecision{
		UserPrompt:      userPrompt,
		CoTTrace:        finalCoTTrace,
		Decisions:       allDecisions,
		Timestamp:       time.Now(),
		AICallAttempted: shouldCallAI && userPrompt != "",
		AICallSucceeded: shouldCallAI && userPrompt != "",
		OpenRejections:  openRejections,
	}, nil
}

type CyclePreparationOptions struct {
	MarketSymbols           []string
	MarketHistoryDepth      map[string]int
	ClosedKlinesOnly        bool
	IncludeMicroADX         bool
	AllowRiskReducingOnHalt bool
}

type CyclePreparation struct {
	PositionDecisions   []Decision
	WaitDecision        *Decision
	StopReason          string
	HaltDecision        *FullDecision
	RiskIncreaseBlocked bool
	FullStop            bool
}

func PrepareCycleContext(ctx *Context, opts CyclePreparationOptions) (*CyclePreparation, error) {
	initializeDefaults(ctx)

	preparation := &CyclePreparation{}
	if result := checkCircuitBreakerState(ctx); result != nil {
		if !opts.AllowRiskReducingOnHalt {
			return &CyclePreparation{HaltDecision: result, FullStop: true}, nil
		}
		preparation.HaltDecision = result
		preparation.RiskIncreaseBlocked = true
		preparation.StopReason = haltReason(result)
		preparation.WaitDecision = waitDecisionPtr(preparation.StopReason)
	}

	if err := fetchMarketDataForContextWithOptions(ctx, opts); err != nil {
		return nil, fmt.Errorf("获取市场数据失败: %w", err)
	}

	stats := GetStatistics()
	cb := CheckCircuitBreaker(ctx, stats)
	if cb.IsTriggered {
		ctx.CircuitBreaker = cb
		halt := &FullDecision{
			CoTTrace: "🛑 触发熔断保护，暂停交易",
			Decisions: []Decision{{
				Symbol:    "ALL",
				Action:    "wait",
				Reasoning: cb.TriggerReason,
			}},
			Timestamp: time.Now(),
		}
		if !opts.AllowRiskReducingOnHalt {
			return &CyclePreparation{
				StopReason: cb.TriggerReason,
				WaitDecision: &Decision{
					Symbol:    "ALL",
					Action:    "wait",
					Reasoning: cb.TriggerReason,
				},
				HaltDecision: halt,
				FullStop:     true,
			}, nil
		}
		preparation.StopReason = cb.TriggerReason
		preparation.WaitDecision = waitDecisionPtr(cb.TriggerReason)
		preparation.HaltDecision = halt
		preparation.RiskIncreaseBlocked = true
	}

	CalculateCorrelationMatrix(ctx)
	evaluateCandidateQuality(ctx)

	positionDecisions := evaluateExistingPositions(ctx)
	preparation.PositionDecisions = positionDecisions
	if isAccountDrawdownHardStopped(ctx) {
		reason := fmt.Sprintf("账户总回撤 %.2f%% 已达到最大回撤阈值 %.2f%%，停止搜索新开仓机会",
			ctx.Account.TotalPnLPct, ctx.MaxAccountDrawdownPct)
		waitDecision := Decision{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: reason,
		}
		strategyDecisions := []Decision{waitDecision}
		allDecisions := mergeDecisions(positionDecisions, strategyDecisions)
		halt := &FullDecision{
			CoTTrace:  buildFinalCoTTrace(reason, positionDecisions, strategyDecisions, allDecisions),
			Decisions: allDecisions,
			Timestamp: time.Now(),
		}
		if !opts.AllowRiskReducingOnHalt {
			return &CyclePreparation{
				PositionDecisions: positionDecisions,
				WaitDecision:      &waitDecision,
				StopReason:        reason,
				HaltDecision:      halt,
				FullStop:          true,
			}, nil
		}
		preparation.WaitDecision = &waitDecision
		preparation.StopReason = reason
		preparation.HaltDecision = halt
		preparation.RiskIncreaseBlocked = true
	}

	return preparation, nil
}

func waitDecisionPtr(reason string) *Decision {
	if strings.TrimSpace(reason) == "" {
		reason = "暂停新开仓"
	}
	return &Decision{Symbol: "ALL", Action: "wait", Reasoning: reason}
}

func haltReason(full *FullDecision) string {
	if full == nil {
		return ""
	}
	if len(full.Decisions) > 0 && strings.TrimSpace(full.Decisions[0].Reasoning) != "" {
		return full.Decisions[0].Reasoning
	}
	return strings.TrimSpace(full.CoTTrace)
}

func openRejectionReasons(rejections []OpenRejection) []string {
	reasons := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		reason := strings.TrimSpace(rejection.Reason)
		if reason == "" {
			reason = strings.Join(rejection.GateReasons, "; ")
		}
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func buildOpenRejection(d Decision, ctx *Context, reason string) OpenRejection {
	rejection := OpenRejection{
		Symbol: d.Symbol,
		Action: d.Action,
		Reason: reason,
	}
	if ctx == nil || ctx.MarketDataMap == nil {
		return rejection
	}
	marketData := ctx.MarketDataMap[d.Symbol]
	if marketData == nil {
		return rejection
	}
	var strategyProfile InstrumentProfile
	if ctx.StrategyRiskPolicy != nil && !ctx.StrategyRiskPolicy.Legacy && ctx.StrategyRiskPolicy.Enabled {
		strategyProfile = ResolveInstrumentProfile(d.Symbol, ctx.StrategyRiskPolicy)
	}
	gate := EvaluateOpenGate(OpenGateInput{
		Decision:         &d,
		Context:          ctx,
		MarketData:       marketData,
		ExecutionQuality: ctx.ExecutionQuality,
		StrategyPolicy:   ctx.StrategyRiskPolicy,
		StrategyProfile:  strategyProfile,
	})
	rejection.GateState = gate.State
	rejection.GateReasons = append(rejection.GateReasons, gate.Reasons...)
	rejection.GateDiagnostics = copyDiagnostics(gate.Diagnostics)
	rejection.Simulations = buildOpenFrequencySimulations(d, ctx, marketData, gate, reason)
	if len(rejection.GateReasons) == 0 && strings.TrimSpace(reason) != "" {
		rejection.GateReasons = append(rejection.GateReasons, reason)
	}
	return rejection
}

type StrategyValidationOptions struct {
	Source              string
	AllowAdd            bool
	AllowTPRRFallback   bool
	PreserveStructureTP bool
}

func ValidateStrategyDecisions(ctx *Context, decisions []Decision, opts StrategyValidationOptions) ([]Decision, []OpenRejection) {
	var validDecisions []Decision
	var openRejections []OpenRejection

	for _, d := range decisions {
		if IsOpenLikeAction(d.Action) {
			if IsAddAction(d.Action) && !opts.AllowAdd {
				reason := fmt.Sprintf("%s %s 被拒绝: 当前策略不允许加仓", d.Symbol, d.Action)
				openRejections = append(openRejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: reason})
				continue
			}
			if err := ValidateAndEnrichDecision(&d, ctx); err != nil {
				reason := fmt.Sprintf("%s %s 参数补充失败: %v", d.Symbol, d.Action, err)
				openRejections = append(openRejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: reason})
				continue
			}
			validationOpts := openValidationOptions{}
			if IsAddAction(d.Action) {
				validationOpts.Intent = "add"
			}
			if err := validateOpenDecisionWithOptions(&d, ctx, validationOpts); err != nil {
				reason := fmt.Sprintf("%s %s 被风控过滤: %v", d.Symbol, d.Action, err)
				openRejections = append(openRejections, buildOpenRejection(d, ctx, reason))
				continue
			}
			validDecisions = append(validDecisions, d)
			continue
		}
		validDecisions = append(validDecisions, d)
	}

	var finalRejections []OpenRejection
	validDecisions, finalRejections = enforceFinalDecisionLimits(validDecisions, ctx)
	openRejections = append(openRejections, finalRejections...)
	return validDecisions, openRejections
}

type RiskReducingValidationOptions struct {
	Source string
}

func ValidateRiskReducingStrategyDecisions(ctx *Context, decisions []Decision, opts RiskReducingValidationOptions) ([]Decision, []OpenRejection) {
	var valid []Decision
	var rejections []OpenRejection
	seen := map[string]bool{}

	for _, d := range decisions {
		if IsOpenLikeAction(d.Action) || d.Action == "wait" || d.Action == "hold" {
			valid = append(valid, d)
			continue
		}
		if !isRiskReducingStrategyAction(d.Action) {
			rejections = append(rejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: fmt.Sprintf("%s %s 不是允许的程序化持仓管理动作", d.Symbol, d.Action)})
			continue
		}
		pos, ok := findPositionForRiskDecision(ctx, d)
		if !ok {
			rejections = append(rejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: fmt.Sprintf("%s %s 被拒绝: 未找到已有持仓", d.Symbol, d.Action)})
			continue
		}
		if err := validateRiskDecisionForPosition(ctx, d, pos); err != nil {
			rejections = append(rejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: err.Error()})
			continue
		}
		key := market.Normalize(d.Symbol) + "|" + strings.ToLower(pos.Side)
		if seen[key] {
			rejections = append(rejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: fmt.Sprintf("%s %s 被拒绝: 同一持仓本周期已有程序化管理动作", d.Symbol, d.Action)})
			continue
		}
		if err := validateProgrammaticMetadata(d); err != nil {
			rejections = append(rejections, OpenRejection{Symbol: d.Symbol, Action: d.Action, Reason: err.Error()})
			continue
		}
		seen[key] = true
		valid = append(valid, d)
	}
	return valid, rejections
}

func isRiskReducingStrategyAction(action string) bool {
	switch action {
	case "close_long", "close_short", "partial_close", "update_stop_loss":
		return true
	default:
		return false
	}
}

func findPositionForRiskDecision(ctx *Context, d Decision) (PositionInfo, bool) {
	if ctx == nil {
		return PositionInfo{}, false
	}
	symbol := market.Normalize(d.Symbol)
	for _, pos := range ctx.Positions {
		if market.Normalize(pos.Symbol) != symbol {
			continue
		}
		side := strings.ToLower(pos.Side)
		switch d.Action {
		case "close_long":
			if side != "long" {
				continue
			}
		case "close_short":
			if side != "short" {
				continue
			}
		}
		return pos, true
	}
	return PositionInfo{}, false
}

func validateRiskDecisionForPosition(ctx *Context, d Decision, pos PositionInfo) error {
	side := strings.ToLower(pos.Side)
	switch d.Action {
	case "close_long":
		if side != "long" {
			return fmt.Errorf("%s close_long 被拒绝: 持仓方向是%s", d.Symbol, pos.Side)
		}
	case "close_short":
		if side != "short" {
			return fmt.Errorf("%s close_short 被拒绝: 持仓方向是%s", d.Symbol, pos.Side)
		}
	case "partial_close":
		if d.ClosePercentage <= 0 || d.ClosePercentage > 100 {
			return fmt.Errorf("%s partial_close 被拒绝: close_percentage必须在0-100之间: %.2f", d.Symbol, d.ClosePercentage)
		}
	case "update_stop_loss":
		if d.NewStopLoss <= 0 {
			return fmt.Errorf("%s update_stop_loss 被拒绝: new_stop_loss无效", d.Symbol)
		}
		currentPrice := pos.MarkPrice
		if currentPrice <= 0 {
			currentPrice = pos.EntryPrice
		}
		if ctx != nil && ctx.MarketDataMap != nil {
			if data := ctx.MarketDataMap[market.Normalize(d.Symbol)]; data != nil && data.CurrentPrice > 0 {
				currentPrice = data.CurrentPrice
			}
		}
		existingStop := effectiveStopForPosition(ctx, pos)
		switch side {
		case "long":
			if currentPrice > 0 && d.NewStopLoss >= currentPrice {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 多头止损 %.4f 不得高于或等于当前价 %.4f", d.Symbol, d.NewStopLoss, currentPrice)
			}
			if existingStop > 0 && d.NewStopLoss <= existingStop {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 新止损 %.4f 未改善现有保护 %.4f", d.Symbol, d.NewStopLoss, existingStop)
			}
			if existingStop <= 0 && pos.EntryPrice > 0 && d.NewStopLoss < pos.EntryPrice {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 新止损 %.4f 会扩大入场风险", d.Symbol, d.NewStopLoss)
			}
		case "short":
			if currentPrice > 0 && d.NewStopLoss <= currentPrice {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 空头止损 %.4f 不得低于或等于当前价 %.4f", d.Symbol, d.NewStopLoss, currentPrice)
			}
			if existingStop > 0 && d.NewStopLoss >= existingStop {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 新止损 %.4f 未改善现有保护 %.4f", d.Symbol, d.NewStopLoss, existingStop)
			}
			if existingStop <= 0 && pos.EntryPrice > 0 && d.NewStopLoss > pos.EntryPrice {
				return fmt.Errorf("%s update_stop_loss 被拒绝: 新止损 %.4f 会扩大入场风险", d.Symbol, d.NewStopLoss)
			}
		default:
			return fmt.Errorf("%s update_stop_loss 被拒绝: 未知持仓方向%s", d.Symbol, pos.Side)
		}
	}
	return nil
}

func effectiveStopForPosition(ctx *Context, pos PositionInfo) float64 {
	stop := pos.StopLoss
	if ctx != nil {
		if plan := GetPlanByScope(ctx.TraderID, pos.Symbol, pos.Side); plan != nil {
			if plan.CurrentStopLoss > 0 {
				stop = plan.CurrentStopLoss
			} else if plan.StopLoss > 0 {
				stop = plan.StopLoss
			}
		}
	}
	return stop
}

func validateProgrammaticMetadata(d Decision) error {
	if d.StrategyMode == "programmatic" {
		if strings.TrimSpace(d.StrategyName) == "" || strings.TrimSpace(d.StrategyVersion) == "" || strings.TrimSpace(d.ConfigHash) == "" {
			return fmt.Errorf("%s %s 被拒绝: 缺少策略版本或配置hash", d.Symbol, d.Action)
		}
		if strings.TrimSpace(d.SignalID) == "" {
			return fmt.Errorf("%s %s 被拒绝: 缺少signal_id", d.Symbol, d.Action)
		}
		layer, _ := d.StrategyMetadata["layer"].(string)
		rule, _ := d.StrategyMetadata["rule"].(string)
		if d.StrategyMetadata == nil || strings.TrimSpace(layer) == "" || strings.TrimSpace(rule) == "" {
			return fmt.Errorf("%s %s 被拒绝: 缺少strategy_metadata.layer/rule", d.Symbol, d.Action)
		}
	}
	return nil
}

func MergePublicAndStrategyDecisions(publicDecisions, strategyDecisions []Decision) []Decision {
	return MergePublicAndStrategyDecisionsWithContext(nil, publicDecisions, strategyDecisions)
}

func MergePublicAndStrategyDecisionsWithContext(ctx *Context, publicDecisions, strategyDecisions []Decision) []Decision {
	if len(publicDecisions) == 0 {
		return strategyDecisions
	}
	blockOpenLikeBySymbol := make(map[string]bool)
	suppressRiskBySymbol := make(map[string]bool)
	publicStopBySymbol := make(map[string]Decision)
	publicStopIndex := make(map[string]int)
	for _, d := range publicDecisions {
		if d.Symbol == "" || d.Symbol == "ALL" {
			continue
		}
		symbol := market.Normalize(d.Symbol)
		switch d.Action {
		case "close_long", "close_short", "partial_close", "update_stop_loss", "update_take_profit":
			blockOpenLikeBySymbol[symbol] = true
		}
		switch d.Action {
		case "close_long", "close_short", "partial_close":
			suppressRiskBySymbol[symbol] = true
		case "update_stop_loss":
			publicStopBySymbol[symbol] = d
		}
	}

	merged := append([]Decision(nil), publicDecisions...)
	for idx, d := range merged {
		if d.Action == "update_stop_loss" && d.Symbol != "" && d.Symbol != "ALL" {
			publicStopIndex[market.Normalize(d.Symbol)] = idx
		}
	}
	for _, d := range strategyDecisions {
		symbol := market.Normalize(d.Symbol)
		if IsOpenLikeAction(d.Action) && blockOpenLikeBySymbol[symbol] {
			continue
		}
		if isRedundantWaitOrHold(d, publicDecisions) {
			continue
		}
		if isRiskReducingStrategyAction(d.Action) {
			if suppressRiskBySymbol[symbol] {
				continue
			}
			if d.Action == "update_stop_loss" {
				if publicStop, ok := publicStopBySymbol[symbol]; ok {
					if shouldReplacePublicStop(ctx, publicStop, d) {
						merged[publicStopIndex[symbol]] = d
					}
					continue
				}
			}
		}
		merged = append(merged, d)
	}
	return merged
}

func shouldReplacePublicStop(ctx *Context, publicStop, strategyStop Decision) bool {
	if ctx == nil || strategyStop.NewStopLoss <= 0 || publicStop.NewStopLoss <= 0 {
		return false
	}
	pos, ok := findPositionForRiskDecision(ctx, strategyStop)
	if !ok {
		return false
	}
	switch strings.ToLower(pos.Side) {
	case "long":
		return strategyStop.NewStopLoss > publicStop.NewStopLoss
	case "short":
		return strategyStop.NewStopLoss < publicStop.NewStopLoss
	default:
		return false
	}
}

func isRedundantWaitOrHold(d Decision, existing []Decision) bool {
	if d.Action != "wait" && d.Action != "hold" {
		return false
	}
	for _, current := range existing {
		if current.Action == "wait" || current.Action == "hold" {
			return true
		}
	}
	return false
}

func buildOpenFrequencySimulations(d Decision, ctx *Context, marketData *market.Data, gate OpenGateResult, reason string) []OpenFrequencySimulation {
	if ctx == nil || ctx.FrequencyPolicy == nil {
		return nil
	}
	policy := ctx.FrequencyPolicy
	var simulations []OpenFrequencySimulation

	if policy.HighADXReportOnly && marketData != nil && d.Action == "open_long" && isHighBetaAltcoin(d.Symbol) &&
		marketData.CurrentADX > elevatedADX && marketData.CurrentADX <= extremeADX {
		hasHardBTCBlock := reasonContainsAny(gate.Reasons, "BTC 1h/4h 明显转弱", "禁止新开高 beta")
		wouldAllow := d.Confidence >= 85 && !hasHardBTCBlock
		simulations = append(simulations, OpenFrequencySimulation{
			Scenario:        "high_adx_active_candidate",
			Source:          "structured",
			WouldAllow:      wouldAllow,
			Reason:          fmt.Sprintf("%s ADX %.1f active report-only", d.Symbol, marketData.CurrentADX),
			OriginalState:   gate.State,
			SimulatedState:  boolState(wouldAllow),
			MinConfidence:   85,
			EffectiveRisk:   gate.EffectiveRisk * highADXRiskMultiplier,
			AdjustedSizeUSD: d.PositionSizeUSD * highADXRiskMultiplier,
			Diagnostics: map[string]any{
				"adx":        marketData.CurrentADX,
				"confidence": d.Confidence,
			},
		})
	}

	if policy.RRReportOnly && marketData != nil {
		if rr, ok := calculateNetRR(d, marketData); ok && rr >= 2.0 && rr < 2.5 {
			wouldAllow := gate.Allowed && !reasonContainsAny(gate.Reasons, "风险回报比")
			simulations = append(simulations, OpenFrequencySimulation{
				Scenario:        "rr_threshold_candidate",
				Source:          "structured",
				WouldAllow:      wouldAllow,
				Reason:          fmt.Sprintf("净RR %.2f 位于 report-only 区间[2.0,2.5)", rr),
				OriginalState:   gate.State,
				SimulatedState:  boolState(wouldAllow),
				EffectiveRisk:   gate.EffectiveRisk,
				AdjustedSizeUSD: gate.AdjustedSizeUSD,
				Diagnostics: map[string]any{
					"net_rr":    rr,
					"threshold": 2.0,
				},
			})
		}
	}

	if policy.RollingGateReportOnly && hasRollingGateSignal(gate, reason) {
		wouldAllow := gate.State != "block"
		diagnostics := copyDiagnostics(gate.Diagnostics)
		if diagnostics == nil {
			diagnostics = map[string]any{}
		}
		diagnostics["rolling_sample_policy"] = "sample_insufficient_uses_risk_only"
		simulations = append(simulations, OpenFrequencySimulation{
			Scenario:        "rolling_risk_only_candidate",
			Source:          "structured",
			WouldAllow:      wouldAllow,
			Reason:          "rolling gate report-only: 只降仓，不额外提高置信度",
			OriginalState:   gate.State,
			SimulatedState:  boolState(wouldAllow),
			EffectiveRisk:   gate.EffectiveRisk,
			AdjustedSizeUSD: gate.AdjustedSizeUSD,
			Diagnostics:     diagnostics,
		})
	}

	return simulations
}

func calculateNetRR(d Decision, marketData *market.Data) (float64, bool) {
	if marketData == nil || marketData.CurrentPrice <= 0 || d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return 0, false
	}
	currentPrice := marketData.CurrentPrice
	var riskPct, rewardPct float64
	switch DecisionDirection(d.Action) {
	case "long":
		if d.StopLoss >= currentPrice || d.TakeProfit <= currentPrice {
			return 0, false
		}
		riskPct = (currentPrice - d.StopLoss) / currentPrice * 100
		rewardPct = (d.TakeProfit - currentPrice) / currentPrice * 100
	case "short":
		if d.StopLoss <= currentPrice || d.TakeProfit >= currentPrice {
			return 0, false
		}
		riskPct = (d.StopLoss - currentPrice) / currentPrice * 100
		rewardPct = (currentPrice - d.TakeProfit) / currentPrice * 100
	default:
		return 0, false
	}
	if riskPct <= 0 {
		return 0, false
	}
	return (rewardPct - 0.2) / riskPct, true
}

func hasRollingGateSignal(gate OpenGateResult, reason string) bool {
	if reasonContainsAny(gate.Reasons, "rolling", "历史滚动", "最近", "PF") {
		return true
	}
	return strings.Contains(strings.ToLower(reason), "rolling") ||
		strings.Contains(reason, "历史滚动") ||
		strings.Contains(reason, "最近") ||
		strings.Contains(reason, "PF")
}

func reasonContainsAny(reasons []string, needles ...string) bool {
	for _, reason := range reasons {
		for _, needle := range needles {
			if strings.Contains(strings.ToLower(reason), strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}

func boolState(allowed bool) string {
	if allowed {
		return "allow"
	}
	return "block"
}

func copyDiagnostics(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func waitDecision(reason string) Decision {
	return Decision{
		Symbol:    "ALL",
		Action:    "wait",
		Reasoning: reason,
	}
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
	if ctx.FrequencyPolicy != nil {
		if ctx.FrequencyPolicy.AnalysisIntervalMin > 0 {
			ctx.AnalysisIntervalMin = ctx.FrequencyPolicy.AnalysisIntervalMin
		}
		if ctx.FrequencyPolicy.EffectiveMode == "" {
			ctx.FrequencyPolicy.EffectiveMode = ctx.FrequencyPolicy.Mode
		}
	}
	if ctx.MaxAccountDrawdownPct == 0 {
		ctx.MaxAccountDrawdownPct = configuredMaxAccountDrawdownPct
	}
}

func normalizeAccountDrawdownPct(value float64) float64 {
	if value <= 0 {
		return defaultMaxAccountDrawdownPct
	}
	if value <= 1 {
		return value * 100
	}
	return value
}

// NormalizeAccountDrawdownPct 将 0.2 和 20.0 统一解释为 20%。
func NormalizeAccountDrawdownPct(value float64) float64 {
	return normalizeAccountDrawdownPct(value)
}

func isAccountDrawdownHardStopped(ctx *Context) bool {
	if ctx == nil || ctx.MaxAccountDrawdownPct <= 0 {
		return false
	}
	return ctx.Account.TotalPnLPct <= -ctx.MaxAccountDrawdownPct
}

func truncateForDecisionLog(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "...(truncated)"
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
		plan := planManager.GetPlanScoped(ctx.TraderID, pos.Symbol, pos.Side)
		marketData := ctx.MarketDataMap[pos.Symbol]

		evaluator := &PositionEvaluator{
			Position:      &pos,
			Plan:          plan,
			MarketData:    marketData,
			BTCMarketData: ctx.MarketDataMap["BTCUSDT"],
			Symbol:        pos.Symbol,
			Exchange:      ctx.Exchange,
		}

		result := evaluator.Evaluate()

		// 更新峰值数据
		if result.ShouldUpdatePeak && marketData != nil {
			planManager.UpdatePlanPeakDataScoped(ctx.TraderID, pos.Symbol, pos.Side, marketData.CurrentPrice, pos.UnrealizedPnLPct)
		}

		// 更新入场ATR
		if plan != nil && plan.EntryATR == 0 && marketData != nil {
			atr := 0.0
			if marketData.LongerTermContext != nil {
				atr = marketData.LongerTermContext.ATR14
			}
			if atr > 0 {
				planManager.UpdatePlanEntryATRScoped(ctx.TraderID, pos.Symbol, pos.Side, atr)
			}
		}

		if result.Action == "hold" {
			if result.NewTakeProfit > 0 {
				decisions = append(decisions, Decision{
					Symbol:        pos.Symbol,
					Action:        "update_take_profit",
					NewTakeProfit: result.NewTakeProfit,
					Reasoning:     fmt.Sprintf("动态止盈调整: %.4f → %.4f", planTakeProfit(plan), result.NewTakeProfit),
				})
				continue
			}
			if needsTakeProfitSync(plan) {
				decisions = append(decisions, Decision{
					Symbol:        pos.Symbol,
					Action:        "update_take_profit",
					NewTakeProfit: plan.TakeProfit,
					Reasoning:     fmt.Sprintf("同步本地止盈计划到交易所: TP=%.4f", plan.TakeProfit),
				})
				continue
			}
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
				planManager.UpdatePlanScoped(ctx.TraderID, pos.Symbol, pos.Side, func(p *TradePlan) {
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

func planTakeProfit(plan *TradePlan) float64 {
	if plan == nil {
		return 0
	}
	return plan.TakeProfit
}

func needsTakeProfitSync(plan *TradePlan) bool {
	if plan == nil || plan.TakeProfit <= 0 || plan.LastTPAdjustTime.IsZero() {
		return false
	}
	return plan.LastTPSyncTime.IsZero() || plan.LastTPAdjustTime.After(plan.LastTPSyncTime)
}

// ============================================================================
// AI调用判断
// ============================================================================

func shouldCallAIForNewOpportunities(ctx *Context) bool {
	if ctx.LossMode != nil && ctx.LossMode.Active {
		if ctx.LossMode.DailyOpenLimit > 0 && ctx.FrequencyState != nil &&
			ctx.FrequencyState.OpenCount24h >= ctx.LossMode.DailyOpenLimit {
			log.Printf("📊 亏损模式24小时新增开仓已达上限(%d/%d)，跳过新机会搜索",
				ctx.FrequencyState.OpenCount24h, ctx.LossMode.DailyOpenLimit)
			return false
		}
		if ctx.LossMode.MaxPositions > 0 && ctx.Account.PositionCount >= ctx.LossMode.MaxPositions {
			log.Printf("📊 亏损模式持仓已满(%d/%d)，跳过新机会搜索", ctx.Account.PositionCount, ctx.LossMode.MaxPositions)
			return false
		}
	}

	if ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.DailyOpenLimit > 0 && ctx.FrequencyState != nil &&
		ctx.FrequencyState.OpenCount24h >= ctx.FrequencyPolicy.DailyOpenLimit {
		log.Printf("📊 24小时新增开仓已达上限(%d/%d)，跳过新机会搜索",
			ctx.FrequencyState.OpenCount24h, ctx.FrequencyPolicy.DailyOpenLimit)
		return false
	}

	if !ctx.AIBackoffUntil.IsZero() && time.Now().Before(ctx.AIBackoffUntil) {
		remaining := time.Until(ctx.AIBackoffUntil).Minutes()
		log.Printf("📊 AI调用退避中，剩余%.1f分钟，跳过新机会搜索", remaining)
		return false
	}

	if !ctx.LastAnalysisTime.IsZero() {
		elapsed := time.Since(ctx.LastAnalysisTime).Minutes()
		if elapsed < float64(ctx.AnalysisIntervalMin) {
			log.Printf("📊 距离上次分析%.1f分钟，跳过AI调用(间隔%d分钟)", elapsed, ctx.AnalysisIntervalMin)
			return false
		}
	}

	maxPositions := maxOpenPositions(ctx)
	if ctx.Account.PositionCount >= maxPositions {
		log.Printf("📊 持仓已满(%d/%d)，跳过新机会搜索", ctx.Account.PositionCount, maxPositions)
		return false
	}

	remainingBudget := calculateRemainingRiskBudget(ctx)
	if remainingBudget <= 0.01 {
		log.Printf("📊 风险预算不足(剩余%.2f%%)，跳过新机会搜索", remainingBudget*100)
		return false
	}

	return true
}

func describeAISkipReason(ctx *Context) string {
	if ctx.LossMode != nil && ctx.LossMode.Active {
		if ctx.LossMode.DailyOpenLimit > 0 && ctx.FrequencyState != nil &&
			ctx.FrequencyState.OpenCount24h >= ctx.LossMode.DailyOpenLimit {
			return fmt.Sprintf("亏损模式24小时新增开仓已达上限(%d/%d)，跳过本周期新机会搜索",
				ctx.FrequencyState.OpenCount24h, ctx.LossMode.DailyOpenLimit)
		}
		if ctx.LossMode.MaxPositions > 0 && ctx.Account.PositionCount >= ctx.LossMode.MaxPositions {
			return fmt.Sprintf("亏损模式持仓已满(%d/%d)，跳过本周期新机会搜索",
				ctx.Account.PositionCount, ctx.LossMode.MaxPositions)
		}
	}

	if ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.DailyOpenLimit > 0 && ctx.FrequencyState != nil &&
		ctx.FrequencyState.OpenCount24h >= ctx.FrequencyPolicy.DailyOpenLimit {
		return fmt.Sprintf("24小时新增开仓已达上限(%d/%d)，跳过本周期新机会搜索",
			ctx.FrequencyState.OpenCount24h, ctx.FrequencyPolicy.DailyOpenLimit)
	}

	if !ctx.AIBackoffUntil.IsZero() && time.Now().Before(ctx.AIBackoffUntil) {
		return fmt.Sprintf("AI调用退避中，剩余%.1f分钟，跳过本周期新机会搜索", time.Until(ctx.AIBackoffUntil).Minutes())
	}

	if !ctx.LastAnalysisTime.IsZero() {
		elapsed := time.Since(ctx.LastAnalysisTime).Minutes()
		if elapsed < float64(ctx.AnalysisIntervalMin) {
			return fmt.Sprintf("距离上次AI新机会分析%.1f分钟，未满%d分钟间隔，跳过本周期新机会搜索",
				elapsed, ctx.AnalysisIntervalMin)
		}
	}

	maxPositions := maxOpenPositions(ctx)
	if ctx.Account.PositionCount >= maxPositions {
		return fmt.Sprintf("持仓已满(%d/%d)，跳过本周期新机会搜索", ctx.Account.PositionCount, maxPositions)
	}

	remainingBudget := calculateRemainingRiskBudget(ctx)
	if remainingBudget <= 0.01 {
		return fmt.Sprintf("风险预算不足(剩余%.2f%%)，跳过本周期新机会搜索", remainingBudget*100)
	}

	return "未满足AI新机会搜索条件，跳过本周期新机会搜索"
}

func calculateRemainingRiskBudget(ctx *Context) float64 {
	usedRisk, _ := CalculateTotalRisk(ctx)
	return ctx.TotalRiskBudget - usedRisk
}

func maxOpenPositions(ctx *Context) int {
	limit := 3
	if ctx != nil && ctx.LossMode != nil && ctx.LossMode.Active &&
		ctx.LossMode.MaxPositions > 0 && ctx.LossMode.MaxPositions < limit {
		limit = ctx.LossMode.MaxPositions
	}
	return limit
}

// ============================================================================
// 决策合并与验证
// ============================================================================

func mergeDecisions(positionDecisions, aiDecisions []Decision) []Decision {
	var result []Decision
	indexBySymbol := make(map[string]int)

	for _, d := range positionDecisions {
		if idx, exists := indexBySymbol[d.Symbol]; exists {
			result[idx] = d
			continue
		}
		indexBySymbol[d.Symbol] = len(result)
		result = append(result, d)
	}

	for _, d := range aiDecisions {
		if IsOpenLikeAction(d.Action) {
			if idx, exists := indexBySymbol[d.Symbol]; exists {
				// 持仓评估决策（任何非 wait 的决策）优先于 AI 新开仓决策
				if result[idx].Action != "wait" {
					continue
				}
				result[idx] = d
				continue
			}
			indexBySymbol[d.Symbol] = len(result)
			result = append(result, d)
		} else if d.Action == "wait" && len(positionDecisions) == 0 {
			if idx, exists := indexBySymbol[d.Symbol]; exists {
				result[idx] = d
				continue
			}
			indexBySymbol[d.Symbol] = len(result)
			result = append(result, d)
		}
	}

	return result
}

func validateOpenDecision(d *Decision, ctx *Context) error {
	return validateOpenDecisionWithOptions(d, ctx, openValidationOptions{})
}

type openValidationOptions struct {
	Intent string
}

func validateOpenDecisionWithOptions(d *Decision, ctx *Context, opts openValidationOptions) error {
	// ========== 开仓前失效条件预检查 ==========
	marketData, ok := ctx.MarketDataMap[d.Symbol]
	if !ok {
		return fmt.Errorf("缺少 %s 市场数据", d.Symbol)
	}

	var riskNormalization *OpenRiskNormalization
	var strategyProfile InstrumentProfile
	if ctx.StrategyRiskPolicy != nil && !ctx.StrategyRiskPolicy.Legacy && ctx.StrategyRiskPolicy.Enabled {
		strategyProfile = ResolveInstrumentProfile(d.Symbol, ctx.StrategyRiskPolicy)
	}
	if StrategyRiskActive(ctx.StrategyRiskPolicy) {
		var err error
		riskNormalization, err = NormalizeOpenDecisionRisk(d, ctx, marketData)
		if err != nil {
			return fmt.Errorf("策略风险规范化失败: %w", err)
		}
	}

	gate := EvaluateOpenGate(OpenGateInput{
		Decision:          d,
		Context:           ctx,
		MarketData:        marketData,
		ExecutionQuality:  ctx.ExecutionQuality,
		StrategyProfile:   strategyProfile,
		StrategyPolicy:    ctx.StrategyRiskPolicy,
		RiskNormalization: riskNormalization,
	})
	if !gate.Allowed {
		return fmt.Errorf("open gate阻止开仓: %s", strings.Join(gate.Reasons, "; "))
	}
	if gate.MinConfidence > 0 && d.Confidence > 0 && d.Confidence < gate.MinConfidence {
		return fmt.Errorf("open gate要求更高置信度: %d < %d (%s)", d.Confidence, gate.MinConfidence, openGateConfidenceReason(gate))
	}

	// 开仓前失效条件检查
	if invalidated, reason := CheckPreOpenInvalidation(d, marketData); invalidated {
		return fmt.Errorf("开仓前失效条件检查失败: %s", reason)
	}

	direction := DecisionDirection(d.Action)

	// 检查是否已有持仓
	hasSameSidePosition := false
	for _, pos := range ctx.Positions {
		if pos.Symbol != d.Symbol {
			continue
		}
		if opts.Intent == "add" {
			if pos.Side == direction {
				hasSameSidePosition = true
				continue
			}
			return fmt.Errorf("%s 已有反向持仓，不能加仓", d.Symbol)
		} else {
			return fmt.Errorf("%s 已有持仓，不能重复开仓", d.Symbol)
		}
	}
	if opts.Intent == "add" && !hasSameSidePosition {
		return fmt.Errorf("%s 没有同向持仓，不能加仓", d.Symbol)
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
	originalRequestedSize := d.PositionSizeUSD
	if d.RequestedPositionSizeUSD <= 0 {
		d.RequestedPositionSizeUSD = originalRequestedSize
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
	if direction == "long" {
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
	effectiveRiskForSizing := gate.EffectiveRisk
	if StrategyRiskActive(ctx.StrategyRiskPolicy) && strategyProfile.MaxRiskPct > 0 &&
		(effectiveRiskForSizing == 0 || strategyProfile.MaxRiskPct < effectiveRiskForSizing) {
		effectiveRiskForSizing = strategyProfile.MaxRiskPct
	}
	feeSlippagePct := 0.0
	if ctx.StrategyRiskPolicy != nil {
		feeSlippagePct = ctx.StrategyRiskPolicy.FeeSlippagePct
	}
	minOrderValue := defaultMinOrderValueUSDT
	if StrategyRiskActive(ctx.StrategyRiskPolicy) && strategyProfile.MinOrderValueUSDT > 0 {
		minOrderValue = strategyProfile.MinOrderValueUSDT
	}
	sizing := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            ctx.Account.TotalEquity,
		AvailableBalance:         ctx.Account.AvailableBalance,
		CurrentPrice:             currentPrice,
		StopLoss:                 d.StopLoss,
		Leverage:                 d.Leverage,
		EffectiveRiskPct:         effectiveRiskForSizing,
		RemainingRiskBudgetPct:   remainingBudget,
		RequestedPositionSizeUSD: d.PositionSizeUSD,
		MinOrderValueUSDT:        minOrderValue,
		FeeSlippagePct:           feeSlippagePct,
		ProfileName:              strategyProfile.Name,
	})
	d.StopDistancePct = sizing.StopDistancePct
	d.StopDistanceRatio = sizing.StopDistanceRatio
	d.StopDistancePercent = sizing.StopDistancePercent
	d.EffectiveRiskPct = sizing.RiskPct
	d.FeeSlippageReserveUSD = sizing.FeeSlippageReserveUSD
	d.TotalRiskUSD = sizing.TotalRiskUSD
	d.TotalRiskPct = sizing.TotalRiskPct
	d.RiskCapReason = sizing.RiskCapReason
	if d.PositionSizeUSD > sizing.MaxPositionSizeUSD*1.01 && sizing.MaxPositionSizeUSD > 0 {
		if sizing.PositionSizeUSD >= minOrderValue {
			log.Printf("⚠️ 单笔风险超限，自动缩仓: %.2f → %.2f USD", d.PositionSizeUSD, sizing.PositionSizeUSD)
			d.AdjustedPositionSizeUSD = sizing.PositionSizeUSD
			d.PositionSizeUSD = sizing.PositionSizeUSD
			d.RiskUSD = sizing.RiskUSD
			d.FeeSlippageReserveUSD = sizing.FeeSlippageReserveUSD
			d.TotalRiskUSD = sizing.TotalRiskUSD
			d.TotalRiskPct = sizing.TotalRiskPct
			d.SizingAdjusted = true
			d.SizingReason = "单笔风险超限，已缩小到最大可执行仓位"
			return nil
		}
		return fmt.Errorf("单笔风险(%.2f USD)超过上限(%.2f USD)", d.PositionSizeUSD*(riskPct/100), ctx.Account.TotalEquity*effectiveRiskForSizing)
	}
	if !sizing.Executable && sizing.PositionSizeUSD < minOrderValue {
		return fmt.Errorf("仓位sizing不可执行: %s", strings.Join(sizing.Reasons, "; "))
	}

	d.RiskUSD = sizing.RiskUSD
	d.AdjustedPositionSizeUSD = sizing.PositionSizeUSD
	return nil
}

func openGateConfidenceReason(gate OpenGateResult) string {
	reasons := make([]string, 0, len(gate.Reasons)+len(gate.Warnings))
	reasons = append(reasons, gate.Reasons...)
	reasons = append(reasons, gate.Warnings...)
	if len(reasons) == 0 {
		return "当前开仓置信度低于方向/行情门槛"
	}
	return strings.Join(reasons, "; ")
}

type openGateLimit struct {
	maxRiskPerTrade float64
	minConfidence   int
	blocked         bool
	reason          string
}

func effectiveOpenGate(d *Decision, ctx *Context) openGateLimit {
	maxRisk := ctx.MaxRiskPerTrade
	if ctx.EffectiveMaxRiskPerTrade > 0 && (maxRisk == 0 || ctx.EffectiveMaxRiskPerTrade < maxRisk) {
		maxRisk = ctx.EffectiveMaxRiskPerTrade
	}
	if maxRisk <= 0 {
		maxRisk = 0.02
	}

	limit := openGateLimit{
		maxRiskPerTrade: maxRisk,
	}
	if ctx == nil || ctx.PerformanceGates == nil {
		return limit
	}

	side := DecisionDirection(d.Action)
	if side == "" {
		side = "long"
	}
	applyGate := func(g logger.PerformanceGate) {
		if g.State == "" || g.State == "allow" {
			return
		}
		if g.State == "block" {
			if g.CooldownUntil.IsZero() || time.Now().Before(g.CooldownUntil) {
				limit.blocked = true
			}
		}
		if g.MinConfidence > limit.minConfidence {
			limit.minConfidence = g.MinConfidence
		}
		if g.RiskMultiplier > 0 && g.RiskMultiplier < 1 {
			limit.maxRiskPerTrade *= g.RiskMultiplier
		}
		if limit.reason == "" {
			limit.reason = g.Reason
		}
	}

	if g, ok := ctx.PerformanceGates.SymbolGates[d.Symbol]; ok {
		applyGate(g)
	}
	if g, ok := ctx.PerformanceGates.SideGates[side]; ok {
		applyGate(g)
	}
	applyGate(ctx.PerformanceGates.GlobalGate)
	if limit.reason == "" {
		limit.reason = "rolling performance gate"
	}
	return limit
}

func validateFinalDecisions(decisions []Decision, ctx *Context) error {
	newPositions := 0
	for _, d := range decisions {
		if IsOpenAction(d.Action) {
			newPositions++
		}
	}

	totalPositions := ctx.Account.PositionCount + newPositions
	maxPositions := maxOpenPositions(ctx)
	if totalPositions > maxPositions {
		return fmt.Errorf("总持仓数量(%d)超过上限(%d)", totalPositions, maxPositions)
	}

	return nil
}

func enforceFinalDecisionLimits(decisions []Decision, ctx *Context) ([]Decision, []OpenRejection) {
	if ctx == nil {
		return decisions, nil
	}

	maxPositions := maxOpenPositions(ctx)
	availableSlots := maxPositions - ctx.Account.PositionCount
	if availableSlots < 0 {
		availableSlots = 0
	}

	var result []Decision
	var rejections []OpenRejection
	keptOpens := 0
	keptOpenLike := 0
	dailyOpenRemaining := 0
	if ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.DailyOpenLimit > 0 {
		openCount := 0
		if ctx.FrequencyState != nil {
			openCount = ctx.FrequencyState.OpenCount24h
		}
		dailyOpenRemaining = ctx.FrequencyPolicy.DailyOpenLimit - openCount
		if dailyOpenRemaining < 0 {
			dailyOpenRemaining = 0
		}
	}
	if ctx.LossMode != nil && ctx.LossMode.Active && ctx.LossMode.DailyOpenLimit > 0 {
		openCount := 0
		if ctx.FrequencyState != nil {
			openCount = ctx.FrequencyState.OpenCount24h
		}
		lossModeRemaining := ctx.LossMode.DailyOpenLimit - openCount
		if lossModeRemaining < 0 {
			lossModeRemaining = 0
		}
		if dailyOpenRemaining == 0 || lossModeRemaining < dailyOpenRemaining {
			dailyOpenRemaining = lossModeRemaining
		}
	}
	for _, d := range decisions {
		if !IsOpenLikeAction(d.Action) {
			result = append(result, d)
			continue
		}
		if IsOpenAction(d.Action) && keptOpens >= availableSlots {
			rejections = append(rejections, OpenRejection{
				Symbol: d.Symbol,
				Action: d.Action,
				Reason: fmt.Sprintf("%s %s 因持仓上限%d个被拒绝", d.Symbol, d.Action, maxPositions),
			})
			continue
		}
		if dailyOpenRemaining > 0 && keptOpenLike >= dailyOpenRemaining ||
			dailyOpenRemaining == 0 && (ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.DailyOpenLimit > 0 ||
				ctx.LossMode != nil && ctx.LossMode.Active && ctx.LossMode.DailyOpenLimit > 0) {
			openCount := 0
			if ctx.FrequencyState != nil {
				openCount = ctx.FrequencyState.OpenCount24h
			}
			limit := 0
			if ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.DailyOpenLimit > 0 {
				limit = ctx.FrequencyPolicy.DailyOpenLimit
			}
			if ctx.LossMode != nil && ctx.LossMode.Active && ctx.LossMode.DailyOpenLimit > 0 &&
				(limit == 0 || ctx.LossMode.DailyOpenLimit < limit) {
				limit = ctx.LossMode.DailyOpenLimit
			}
			rejections = append(rejections, OpenRejection{
				Symbol: d.Symbol,
				Action: d.Action,
				Reason: fmt.Sprintf("%s %s 因24小时新增开仓上限%d笔被拒绝(当前%d笔)",
					d.Symbol, d.Action, limit, openCount+keptOpenLike),
			})
			continue
		}
		result = append(result, d)
		keptOpenLike++
		if IsOpenAction(d.Action) {
			keptOpens++
		}
	}

	if len(result) == 0 && len(rejections) > 0 {
		result = append(result, waitDecision("最终风控拦截: "+strings.Join(openRejectionReasons(rejections), "; ")))
	}

	return result, rejections
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
			effectiveOpenGate(d, ctx).maxRiskPerTrade,
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

		if DecisionDirection(d.Action) == "long" {
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

	direction := DecisionDirection(d.Action)
	if direction == "" {
		direction = "long"
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
	if DecisionDirection(d.Action) == "short" {
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
		ProfileName:                 d.ProfileName,
		InitialRiskDistance:         math.Abs(actualEntryPrice - adjustedSL),
		EffectiveStopLoss:           d.EffectiveStopLoss,
		EffectiveTakeProfit:         d.EffectiveTakeProfit,
		ExchangeFullTakeProfit:      d.ExchangeFullTakeProfit,
		ExchangeFullTPMode:          d.ExchangeFullTPMode,
		StrategyMode:                d.StrategyMode,
		StrategyName:                d.StrategyName,
		StrategyVersion:             d.StrategyVersion,
		ConfigHash:                  d.ConfigHash,
		SignalID:                    d.SignalID,
		SignalType:                  d.SignalType,
		SignalTimeframe:             d.SignalTimeframe,
		StructureTarget:             d.StructureTarget,
		StrategyMetadata:            copyStringAnyMap(d.StrategyMetadata),
		StrategyDiagnosis:           copyStringAnyMap(d.StrategyDiagnosis),
	}
	if actualEntryPrice > 0 {
		plan.InitialRiskDistancePct = plan.InitialRiskDistance / actualEntryPrice
	}
	if plan.EffectiveStopLoss <= 0 {
		plan.EffectiveStopLoss = adjustedSL
	}
	if plan.EffectiveTakeProfit <= 0 {
		plan.EffectiveTakeProfit = adjustedTP
	}
	if plan.ExchangeFullTakeProfit <= 0 {
		plan.ExchangeFullTakeProfit = adjustedTP
	}
	if d.RiskNormalization != nil {
		plan.InitialATR = d.RiskNormalization.ATRValue
		plan.InitialATRTimeframe = d.RiskNormalization.ATRTimeframe
		plan.MinNetRR = d.RiskNormalization.MinNetRR
	}

	if plan.MinHoldMinutes == 0 {
		plan.MinHoldMinutes = 30
	}

	return plan
}

func copyStringAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

// ============================================================================
// 市场数据获取
// ============================================================================

func fetchMarketDataForContext(ctx *Context) error {
	return fetchMarketDataForContextWithOptions(ctx, CyclePreparationOptions{})
}

func fetchMarketDataForContextWithOptions(ctx *Context, opts CyclePreparationOptions) error {
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
	for _, symbol := range opts.MarketSymbols {
		if strings.TrimSpace(symbol) != "" {
			symbolSet[market.Normalize(symbol)] = true
		}
	}

	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	for symbol := range symbolSet {
		data, err := getMarketDataForPreparation(symbol, opts)
		if err != nil {
			log.Printf("⚠️ 获取 %s 数据失败: %v", symbol, err)
			markCandidateFiltered(ctx, symbol, fmt.Sprintf("市场数据获取失败: %v", err))
			continue
		}

		isExistingPosition := positionSymbols[symbol]
		if !isExistingPosition && data.OIValueUSD > 0 {
			oiValueInMillions := data.OIValueUSD / 1_000_000
			if oiValueInMillions < 15 {
				log.Printf("⚠️ %s OI价值过低(%.2fM USD < 15M)，跳过", symbol, oiValueInMillions)
				markCandidateFiltered(ctx, symbol, fmt.Sprintf("OI价值过低 %.2fM USD < 15M", oiValueInMillions))
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

func getMarketDataForPreparation(symbol string, opts CyclePreparationOptions) (*market.Data, error) {
	if opts.ClosedKlinesOnly || opts.IncludeMicroADX || len(opts.MarketHistoryDepth) > 0 {
		return market.GetWithHistory(symbol, market.HistoryOptions{
			Depth: market.HistoryDepth{
				M3:  opts.MarketHistoryDepth["3m"],
				M15: opts.MarketHistoryDepth["15m"],
				H1:  opts.MarketHistoryDepth["1h"],
				H4:  opts.MarketHistoryDepth["4h"],
			},
			ClosedOnly:      opts.ClosedKlinesOnly,
			IncludeMicroADX: opts.IncludeMicroADX,
		})
	}
	return market.Get(symbol)
}

func markCandidateFiltered(ctx *Context, symbol string, reason string) {
	if ctx == nil {
		return
	}
	for i := range ctx.CandidateCoins {
		if ctx.CandidateCoins[i].Symbol != symbol {
			continue
		}
		ctx.CandidateCoins[i].IncludedInPrompt = false
		ctx.CandidateCoins[i].DataQuality = "insufficient"
		ctx.CandidateCoins[i].FilterReason = reason
		return
	}
}

func evaluateCandidateQuality(ctx *Context) {
	if ctx == nil {
		return
	}
	for i := range ctx.CandidateCoins {
		coin := &ctx.CandidateCoins[i]
		coin.IncludedInPrompt = true
		if coin.DataQuality == "" {
			coin.DataQuality = "ok"
		}

		data := ctx.MarketDataMap[coin.Symbol]
		if data == nil {
			if coin.FilterReason == "" {
				coin.FilterReason = "缺少市场数据"
			}
			coin.DataQuality = "insufficient"
			coin.IncludedInPrompt = false
			continue
		}

		state, confidence := market.GetMarketState(data)
		coin.MarketState = state
		coin.StateConfidence = confidence
		coin.Score = calculateCoinScore(data, ctx.CorrelationMap[coin.Symbol])

		var warnings []string
		if ctx.Exchange != "" && ctx.Exchange != "binance" {
			warnings = append(warnings, fmt.Sprintf("行情源为Binance，执行交易所为%s，需关注价差", ctx.Exchange))
		}
		if data.CurrentPrice <= 0 {
			coin.FilterReason = "当前价格缺失"
			coin.DataQuality = "insufficient"
			coin.IncludedInPrompt = false
		}
		if data.CurrentADX <= 0 || data.CurrentRSI14 <= 0 {
			warnings = append(warnings, "ADX/RSI关键指标不足")
			if coin.DataQuality == "ok" {
				coin.DataQuality = "warn"
			}
		}
		if data.LongerTermContext == nil || data.LongerTermContext.ATR14 <= 0 {
			warnings = append(warnings, "4h ATR数据不足")
			if coin.DataQuality == "ok" {
				coin.DataQuality = "warn"
			}
		}
		if coin.FilterReason != "" {
			coin.DataQuality = "insufficient"
			coin.IncludedInPrompt = false
		}
		coin.Warnings = appendUniqueStrings(coin.Warnings, warnings...)
	}
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		exists := false
		for _, value := range values {
			if value == addition {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, addition)
		}
	}
	return values
}

func candidateIncludedInPrompt(coin CandidateCoin) bool {
	if coin.IncludedInPrompt {
		return true
	}
	return coin.FilterReason == "" && coin.DataQuality == ""
}

func btcAltLongPromptRestriction(ctx *Context) (string, []string) {
	if ctx == nil || ctx.MarketDataMap == nil {
		return "", nil
	}
	btcData := ctx.MarketDataMap["BTCUSDT"]
	if btcData == nil {
		return "", nil
	}
	if btcData.PriceChange1h <= -5 {
		return "block", []string{fmt.Sprintf("BTC 1小时跌幅 %.2f%%，禁止新开仓", btcData.PriceChange1h)}
	}
	if isConfirmedBTCBearishStructure(btcData) {
		return "block", []string{"BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单"}
	}
	var reasons []string
	if btcData.PriceChange1h <= -3 || btcData.PriceChange4h <= -7 || btcData.BollingerWidth >= btcHighVolatilityBollingerPct {
		reasons = append(reasons, "BTC波动或跌幅偏高，新开仓降权")
	}
	if isBearishStructure(btcData) {
		reasons = append(reasons, "BTC 1h/4h 存在转弱信号，高 beta 山寨多单降权")
	}
	if hasBTCMultiTimeframeConflict(btcData) {
		reasons = append(reasons, "BTC 15m 与 1h/4h 趋势冲突，高 beta 山寨多单降权")
	}
	if len(reasons) > 0 {
		return "penalize", reasons
	}
	return "", nil
}

func promptCandidateOrder(ctx *Context, prioritizeCore bool) []CandidateCoin {
	if ctx == nil || len(ctx.CandidateCoins) == 0 {
		return nil
	}
	ordered := make([]CandidateCoin, 0, len(ctx.CandidateCoins))
	used := make(map[string]bool, len(ctx.CandidateCoins))
	if prioritizeCore {
		for _, prioritySymbol := range []string{"BTCUSDT", "ETHUSDT"} {
			for _, coin := range ctx.CandidateCoins {
				if coin.Symbol != prioritySymbol || used[coin.Symbol] {
					continue
				}
				ordered = append(ordered, coin)
				used[coin.Symbol] = true
				break
			}
		}
	}
	for _, coin := range ctx.CandidateCoins {
		if used[coin.Symbol] {
			continue
		}
		ordered = append(ordered, coin)
		used[coin.Symbol] = true
	}
	return ordered
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
	sb.WriteString("| 净风险回报比 | `(止盈距离% - 0.2) / 止损距离% >= 2.5` |\n")
	sb.WriteString("| 单笔风险 | ≤ 账户净值的2% |\n")
	sb.WriteString(fmt.Sprintf("| 仓位上限 | 山寨币 %.0f USD / BTC&ETH %.0f USD |\n", maxPositionForAltcoin, maxPositionForBTCETH))
	sb.WriteString("| OI价值 | ≥ 15M USD |\n\n")
	sb.WriteString("**额外风控硬约束**:\n")
	sb.WriteString("- BTC 1h/4h 明显转弱时，禁止新开高 beta 山寨多单\n")
	sb.WriteString("- BTC 15m 与 1h/4h 方向冲突时，山寨多单必须降权且置信度更高\n")
	sb.WriteString("- 已有2个同向多单时，不要继续叠加高 beta 多单\n")
	sb.WriteString("- 任一同向持仓浮亏超过4%时，禁止继续加同向仓\n")
	sb.WriteString("- ADX 25-50 可视为趋势确认；ADX>60 代表追高风险，必须等待回踩确认\n")
	sb.WriteString("- 最近连续亏损后系统会自动降仓，AI应同步降低交易频率\n\n")

	sb.WriteString("# 📋 开仓决策流程\n\n")
	sb.WriteString("1. **评估BTC趋势** → 确定大方向\n")
	sb.WriteString("2. **筛选候选币种** → ADX 25-50 + 趋势方向一致，ADX>60 需回踩确认\n")
	sb.WriteString("3. **检查组合暴露** → 避免同向高 beta 持仓过度集中\n")
	sb.WriteString("4. **多时间框架确认** → 4h/1h/15m 信号对齐\n")
	sb.WriteString("5. **计算仓位** → ATR自适应 + 相关性调整 + 亏损后降仓\n")
	sb.WriteString("6. **设置止损止盈** → 止损=ATR×2.5，净RR公式 `(止盈距离% - 0.2) / 止损距离% >= 2.5`\n")
	sb.WriteString("7. **定义失效条件** → 什么情况下计划失效\n\n")

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
	sb.WriteString("最低止盈距离% = 止损距离% × 2.5 + 0.2\n")
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
	sb.WriteString("**核心原则**: 宁可错过，不可做错 | 净RR≥2.5 | BTC是龙头 | 失效条件必须明确\n")

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

	restrictionState, restrictionReasons := btcAltLongPromptRestriction(ctx)
	if restrictionState != "" {
		sb.WriteString("## ⚠️ 开仓路径提示\n")
		sb.WriteString(fmt.Sprintf("**高 beta 山寨多单 gate**: %s | %s\n",
			restrictionState, strings.Join(restrictionReasons, "; ")))
		sb.WriteString("优先评估 BTCUSDT、ETHUSDT 或 open_short；如仍选择山寨多单，必须有更高置信度、更小仓位和明确失效条件。\n\n")
	}

	// 候选币种
	sb.WriteString("## 🔍 候选币种\n\n")
	displayedCount := 0
	prioritizeCore := restrictionState != ""
	for _, coin := range promptCandidateOrder(ctx, prioritizeCore) {
		if coin.Symbol == "BTCUSDT" && !prioritizeCore {
			continue
		}
		if restrictionState == "block" && isHighBetaAltcoin(coin.Symbol) {
			continue
		}
		if !candidateIncludedInPrompt(coin) {
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
		riskForSizing := ctx.MaxRiskPerTrade
		if ctx.EffectiveMaxRiskPerTrade > 0 && (riskForSizing == 0 || ctx.EffectiveMaxRiskPerTrade < riskForSizing) {
			riskForSizing = ctx.EffectiveMaxRiskPerTrade
		}
		if ctx.LossMode != nil && ctx.LossMode.Active && ctx.LossMode.MaxRiskPerTrade > 0 &&
			(riskForSizing == 0 || ctx.LossMode.MaxRiskPerTrade < riskForSizing) {
			riskForSizing = ctx.LossMode.MaxRiskPerTrade
		}
		suggestedSize, stopDist := market.CalculateAdaptivePositionSize(
			ctx.Account.TotalEquity,
			atr14,
			marketData.CurrentPrice,
			riskForSizing,
			isAltcoin,
		)

		sb.WriteString(fmt.Sprintf("### %d. %s\n", displayedCount, coin.Symbol))
		sb.WriteString(fmt.Sprintf("**趋势**: %s%s | **数据质量**: %s | **评分**: %.1f\n",
			marketState, corrInfo, coin.DataQuality, coin.Score))
		if len(coin.Sources) > 0 || len(coin.Warnings) > 0 {
			sb.WriteString(fmt.Sprintf("**来源**: %s", strings.Join(coin.Sources, ",")))
			if coin.Tier != "" {
				sb.WriteString(fmt.Sprintf(" | **池层级**: %s", coin.Tier))
			}
			if coin.PoolScore > 0 {
				sb.WriteString(fmt.Sprintf(" | **池评分**: %.1f", coin.PoolScore))
			}
			if len(coin.Warnings) > 0 {
				sb.WriteString(fmt.Sprintf(" | **警告**: %s", strings.Join(coin.Warnings, "; ")))
			}
			sb.WriteString("\n")
		}
		stopPct := 0.0
		if marketData.CurrentPrice > 0 {
			stopPct = stopDist / marketData.CurrentPrice * 100
		}
		minTakeProfitPct := stopPct*2.5 + 0.2
		if ctx.StrategyRiskPolicy != nil && !ctx.StrategyRiskPolicy.Legacy && ctx.StrategyRiskPolicy.Enabled {
			profile := ResolveInstrumentProfile(coin.Symbol, ctx.StrategyRiskPolicy)
			atr := market.GetATR(marketData, profile.ATRTimeframe)
			minStopRatio := profile.MinStopPct
			if atr > 0 && marketData.CurrentPrice > 0 {
				atrRatio := atr * profile.ATRMultiplier / marketData.CurrentPrice
				if atrRatio > minStopRatio {
					minStopRatio = atrRatio
				}
			}
			minTPRatio := minStopRatio*profile.MinNetRR + ctx.StrategyRiskPolicy.FeeSlippagePct
			adxSnapshot := market.GetDirectionalSnapshot(marketData, ctx.StrategyRiskPolicy.ADXTimeframe)
			executableLong := profile.AllowLong && adxSnapshot.ADX >= profile.MinADX && adxSnapshot.DIPlus > adxSnapshot.DIMinus
			executableShort := profile.AllowShort && adxSnapshot.ADX >= profile.MinADX && adxSnapshot.DIMinus > adxSnapshot.DIPlus
			if adxSnapshot.ADX <= 0 || adxSnapshot.DIPlus <= 0 || adxSnapshot.DIMinus <= 0 {
				executableLong = false
				executableShort = false
			}
			nonExecutable := !executableLong && !executableShort
			sb.WriteString(fmt.Sprintf("**Profile**: %s | ATR(%s): %.4f | minSL: %.2f%% | minTP: %.2f%% | ADX(%s): %.1f DI+: %.1f DI-: %.1f | executable_long=%t executable_short=%t non_executable=%t\n",
				profile.Name, profile.ATRTimeframe, atr, minStopRatio*100, minTPRatio*100,
				adxSnapshot.Timeframe, adxSnapshot.ADX, adxSnapshot.DIPlus, adxSnapshot.DIMinus,
				executableLong, executableShort, nonExecutable))
		}
		sb.WriteString(fmt.Sprintf("**建议仓位**: %.0f USD | **建议止损/最大SL距离**: %.4f (%.2f%%) | **最低TP距离**: %.2f%%\n",
			suggestedSize, stopDist, stopPct, minTakeProfitPct))
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
	UpdateTakeProfit(symbol string, newTakeProfit float64) error
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

		case "update_take_profit":
			err = executor.UpdateTakeProfit(d.Symbol, d.NewTakeProfit)
			if err == nil {
				OnTakeProfitUpdated(d.Symbol, d.NewTakeProfit)
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
			MaxRiskPerTrade:       0.02,
			TotalRiskBudget:       0.08,
			MaxAccountDrawdownPct: defaultMaxAccountDrawdownPct,
			AnalysisIntervalMin:   15,
			BTCETHLeverage:        10,
			AltcoinLeverage:       5,
			DataDir:               defaultDataDir,
			RiskFreeRate:          0.0,
		}
	}

	configuredMaxAccountDrawdownPct = normalizeAccountDrawdownPct(config.MaxAccountDrawdownPct)

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

	log.Printf("📊 决策模块初始化: 单笔风险=%.1f%%, 总预算=%.1f%%, 最大账户回撤=%.1f%%, 分析间隔=%d分钟",
		config.MaxRiskPerTrade*100, config.TotalRiskBudget*100, configuredMaxAccountDrawdownPct, config.AnalysisIntervalMin)

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
	SyncPlansFromPositionsScoped("", positions, marketDataMap)
}

// SyncPlansFromPositionsScoped 从现有持仓同步 trader 作用域计划。
func SyncPlansFromPositionsScoped(traderID string, positions []PositionInfo, marketDataMap map[string]*market.Data) {
	for _, pos := range positions {
		if planManager.GetPlanScoped(traderID, pos.Symbol, pos.Side) != nil {
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
			TraderID:         traderID,
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
	if planManager == nil {
		return nil
	}
	return planManager.GetPlan(symbol)
}

// GetPlanByScope 根据 trader/symbol/side 获取计划，兼容旧 symbol 计划。
func GetPlanByScope(traderID, symbol, side string) *TradePlan {
	if planManager == nil {
		return nil
	}
	return planManager.GetPlanScoped(traderID, symbol, side)
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
			plan := planManager.GetPlanScoped(ctx.TraderID, pos.Symbol, pos.Side)
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
		if !candidateIncludedInPrompt(coin) {
			reason := coin.FilterReason
			if reason == "" {
				reason = "未进入prompt"
			}
			sb.WriteString(fmt.Sprintf("  %s: 过滤 | %s\n", coin.Symbol, reason))
			continue
		}
		if data, ok := ctx.MarketDataMap[coin.Symbol]; ok {
			state, conf := market.GetMarketState(data)
			score := calculateCoinScore(data, ctx.CorrelationMap[coin.Symbol])
			poolInfo := ""
			if coin.Tier != "" || coin.PoolScore > 0 {
				poolInfo = fmt.Sprintf(" | 池=%s/%.1f", coin.Tier, coin.PoolScore)
			}
			sb.WriteString(fmt.Sprintf("  %s: %s(%d%%) | 评分=%.1f%s | 数据=%s\n",
				coin.Symbol, state, conf, score, poolInfo, coin.DataQuality))
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
