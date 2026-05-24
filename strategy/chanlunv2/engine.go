package chanlunv2

import (
	"fmt"
	"log"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"strings"
	"sync"
	"time"
)

// Engine 缠论V2策略引擎
type Engine struct {
	Config            config.ChanlunV2StrategyConfig
	mu                sync.RWMutex
	latestSignals     map[string]*chanlunSignalReport
	symbolUniverse    map[string][]chanlunStrategySymbol
	staleSuppressions map[string]chanlunV2StaleSuppression
	lifecycleStates   map[string]SignalLifecycleState
	lifecycleLoaded   map[string]bool
	positionStates    map[string]PositionManagementState
	configHash        string
}

type chanlunV2StaleSuppression struct {
	TraderID            string
	Symbol              string
	SignalID            string
	ReasonCode          string
	FreshnessState      string
	SignalCloseTime     int64
	DecisionCloseTime   int64
	EvaluationCloseTime int64
	FirstSeenAt         int64
	LastSeenAt          int64
	SuppressedCount     int
	LastReason          string
	DedupeKeys          map[string]bool
}

type chanlunV2FreshnessEvaluation struct {
	TradeTimeframe      string
	SignalCloseTime     int64
	DecisionCloseTime   int64
	EvaluationCloseTime int64
	AgeCandles          int
	SoftAgeCandles      int
	MaxLifetimeCandles  int
	FreshnessState      string
	CurrentPrice        float64
	MinRemainingNetRR   float64
}

// NewEngine 创建缠论V2引擎
func NewEngine(cfg config.ChanlunV2StrategyConfig) (*Engine, error) {
	cfg = config.NormalizeChanlunV2StrategyConfig(cfg)
	return &Engine{
		Config:            cfg,
		latestSignals:     map[string]*chanlunSignalReport{},
		symbolUniverse:    map[string][]chanlunStrategySymbol{},
		staleSuppressions: map[string]chanlunV2StaleSuppression{},
		lifecycleStates:   map[string]SignalLifecycleState{},
		lifecycleLoaded:   map[string]bool{},
		positionStates:    map[string]PositionManagementState{},
		configHash:        hashChanlunV2Config(cfg),
	}, nil
}

// GetFullDecision 实现 ChanlunV2EngineInterface
func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("缺少交易上下文")
	}
	now := time.Now()

	timeframes := e.resolveTimeframes()
	universe := e.resolveSymbolUniverse(ctx)
	e.setUniverse(ctx.TraderID, universe)
	symbols := strategySymbolNames(universe)

	prep, err := decision.PrepareCycleContext(ctx, decision.CyclePreparationOptions{
		MarketSymbols:           symbols,
		MarketHistoryDepth:      e.marketHistoryDepth(timeframes),
		ClosedKlinesOnly:        true,
		AllowRiskReducingOnHalt: true,
	})
	if err != nil {
		return nil, err
	}

	var allDecisions []decision.Decision
	var diagnostics []string
	var downgradedStaleSignals []string
	rawSignalCount := 0
	activeSignalCount := 0
	parentStructureCount := 0
	entryTriggerCount := 0
	triggerRejectionReasons := map[string]int{}

	// 持仓管理优先产出风险降低动作，后续开仓候选不能阻塞这些动作。
	posDecisions := e.managePositions(ctx, timeframes)
	for _, d := range posDecisions {
		e.appendDecisionMarker(ctx.TraderID, d)
	}
	allDecisions = append(allDecisions, posDecisions...)

	// 对每个标的进行多级别分析
	for _, symbol := range symbols {
		symbolDiagnostics := []string{}
		multiResult := e.analyzeSymbolFromContext(ctx, symbol, timeframes)
		if multiResult == nil {
			symbolDiagnostics = append(symbolDiagnostics, fmt.Sprintf("%s 数据不足", symbol))
			diagnostics = append(diagnostics, symbolDiagnostics...)
			e.setLatestReport(ctx.TraderID, symbol, nil, nil, symbolDiagnostics)
			continue
		}

		// 级别联立产出信号
		signals := e.multiLevelJudgment(multiResult, timeframes)
		if len(signals) == 0 {
			symbolDiagnostics = append(symbolDiagnostics, fmt.Sprintf("%s 无买卖点信号", symbol))
			diagnostics = append(diagnostics, symbolDiagnostics...)
			e.setLatestReport(ctx.TraderID, symbol, multiResult, nil, symbolDiagnostics)
			continue
		}
		rawSignalCount += len(signals)
		e.setLatestReport(ctx.TraderID, symbol, multiResult, signals, symbolDiagnostics)

		// 父结构先入生命周期；可执行开仓只来自direct结构窗口或fresh entry trigger。
		decisionCloseTime := evaluationCloseTime(multiResult, "trade")
		for _, sig := range signals {
			evaluation := e.evaluateParentStructureEntry(ctx, symbol, sig, multiResult, timeframes, decisionCloseTime)
			if evaluation.ParentSeen {
				parentStructureCount++
			}
			if len(evaluation.Diagnostics) > 0 {
				diagnostics = append(diagnostics, evaluation.Diagnostics...)
			}
			if evaluation.TriggerRejected && evaluation.ReasonCode != "" {
				triggerRejectionReasons[evaluation.ReasonCode]++
			}
			d := evaluation.Decision
			if d.Action == "" {
				continue
			}
			if downgraded, diagnostic := e.downgradeExpiredChanlunV2Signal(ctx, d, timeframes["trade"]); downgraded {
				if diagnostic != "" {
					diagnostics = append(diagnostics, diagnostic)
					downgradedStaleSignals = append(downgradedStaleSignals, diagnostic)
				}
				continue
			}
			allDecisions = append(allDecisions, d)
			activeSignalCount++
			if evaluation.TriggerReady && metadataString(d.StrategyMetadata, "layer") == v2LayerEntryTrigger {
				entryTriggerCount++
			}
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s 置信度%d", symbol, sig.SignalType, sig.Confidence))
		}
	}

	rawDecisions := append([]decision.Decision(nil), allDecisions...)
	allDecisions, freshnessRejections, freshnessSuppressed := e.applyChanlunV2FreshnessGuard(ctx, allDecisions, timeframes)
	allDecisions, validationRejections := e.validateChanlunV2Decisions(ctx, allDecisions, prep)
	openRejections := append(freshnessRejections, validationRejections...)
	e.markRejectedOpenMarkers(ctx, rawDecisions, allDecisions, openRejections)

	if len(allDecisions) == 0 {
		reason := "缠论V2策略未发现可执行信号"
		if len(openRejections) > 0 {
			reason = "缠论V2开仓信号已全部被过滤: " + strings.Join(chanlunV2OpenRejectionReasons(openRejections), "; ")
		} else if len(downgradedStaleSignals) > 0 {
			reason = "缠论V2过期信号已降级为诊断: " + strings.Join(downgradedStaleSignals, "; ")
		} else if len(freshnessSuppressed) > 0 {
			reason = "缠论V2重复过期信号已静默: " + strings.Join(freshnessSuppressed, "; ")
		}
		allDecisions = []decision.Decision{{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: reason,
		}}
	}

	summary := "缠论V2策略周期完成"
	if len(diagnostics) > 0 {
		summary += ": " + strings.Join(diagnostics, "; ")
	}
	if prep != nil && prep.RiskIncreaseBlocked && strings.TrimSpace(prep.StopReason) != "" {
		summary += "; 风险增加已阻断: " + prep.StopReason
	}
	if len(freshnessRejections) > 0 {
		summary += "; 信号新鲜度拒绝 " + strings.Join(chanlunV2OpenRejectionReasons(freshnessRejections), "; ")
	}
	if len(freshnessSuppressed) > 0 {
		summary += "; 重复过期信号已静默 " + strings.Join(freshnessSuppressed, "; ")
	}
	if len(validationRejections) > 0 {
		summary += "; 风控/open gate拒绝 " + strings.Join(chanlunV2OpenRejectionReasons(validationRejections), "; ")
	}

	return &decision.FullDecision{
		CoTTrace:        summary,
		Decisions:       allDecisions,
		Timestamp:       now,
		DecisionMode:    "chanlun_v2",
		StrategyName:    "chanlun_v2",
		StrategyVersion: "v0.1",
		ConfigHash:      e.configHash,
		OpenRejections:  openRejections,
		StrategyDiagnostics: map[string]any{
			"messages":                  diagnostics,
			"timeframes":                timeframes,
			"symbols":                   symbols,
			"raw_signal_count":          rawSignalCount,
			"signal_count":              activeSignalCount,
			"parent_structure_count":    parentStructureCount,
			"entry_trigger_count":       entryTriggerCount,
			"trigger_rejection_reasons": triggerRejectionReasons,
			"downgraded_stale_signals":  append([]string(nil), downgradedStaleSignals...),
			"open_rejections":           chanlunV2OpenRejectionReasons(openRejections),
			"freshness_rejections":      chanlunV2OpenRejectionReasons(freshnessRejections),
			"freshness_suppressed":      append([]string(nil), freshnessSuppressed...),
			"validation_rejections":     chanlunV2OpenRejectionReasons(validationRejections),
		},
	}, nil
}

type multiLevelResult struct {
	Symbol            string
	Results           map[string]*AnalysisResult // level → result
	LastClosedByLevel map[string]int64
}

func (e *Engine) analyzeMultiLevel(symbol string, data *market.Data, timeframes map[string]string) *multiLevelResult {
	result := &multiLevelResult{Symbol: symbol, Results: map[string]*AnalysisResult{}, LastClosedByLevel: map[string]int64{}}

	for level, tf := range timeframes {
		klines, ok := data.Klines[tf]
		if !ok || len(klines) < 30 {
			continue
		}
		result.LastClosedByLevel[level] = latestKlineCloseMillis(klines)
		input := e.buildInput(klines, tf)
		output, err := AnalyzeKlines(input)
		if err != nil {
			log.Printf("[chanlun_v2] %s %s 分析失败: %v", symbol, tf, err)
			continue
		}
		if output.Result != nil {
			result.Results[level] = output.Result
		}
	}

	if len(result.Results) == 0 {
		return nil
	}
	return result
}

func (e *Engine) analyzeSymbolFromContext(ctx *decision.Context, symbol string, timeframes map[string]string) *multiLevelResult {
	if ctx != nil && ctx.MarketDataMap != nil {
		normalized := market.Normalize(symbol)
		if data := ctx.MarketDataMap[normalized]; data != nil {
			if result := e.analyzeMultiLevel(normalized, data, timeframes); result != nil {
				return result
			}
		}
	}
	return e.analyzeSymbol(symbol, timeframes)
}

func (e *Engine) analyzeSymbol(symbol string, timeframes map[string]string) *multiLevelResult {
	result := &multiLevelResult{Symbol: symbol, Results: map[string]*AnalysisResult{}, LastClosedByLevel: map[string]int64{}}

	for level, tf := range timeframes {
		depth := 240
		if d, ok := e.Config.HistoryDepth[tf]; ok && d > 0 {
			depth = d
		}
		klines, err := market.GetKlines(symbol, tf, depth, true)
		if err != nil || len(klines) < 30 {
			continue
		}
		result.LastClosedByLevel[level] = latestKlineCloseMillis(klines)
		input := e.buildInput(klines, tf)
		output, err := AnalyzeKlines(input)
		if err != nil {
			log.Printf("[chanlun_v2] %s %s 分析失败: %v", symbol, tf, err)
			continue
		}
		if output.Result != nil {
			result.Results[level] = output.Result
		}
	}

	if len(result.Results) == 0 {
		return nil
	}
	return result
}

func (e *Engine) buildInput(klines []market.Kline, _ string) *AnalysisInput {
	input := &AnalysisInput{
		Klines:   make([]Kline, len(klines)),
		MACDHist: make([]float64, len(klines)),
		Config: AnalysisConfig{
			MinStrokeBars:         5,
			DivergenceThreshold:   0.8,
			EnableExtendedSignals: true,
			EnableRecursive:       true,
			RecursiveDepth:        2,
		},
	}
	for i, k := range klines {
		input.Klines[i] = Kline{
			OpenTime: k.OpenTime, CloseTime: k.CloseTime,
			Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume,
		}
		// 简化 MACD hist：用价格变化近似
		if i > 0 {
			input.MACDHist[i] = k.Close - klines[i-1].Close
		}
	}
	return input
}

func (e *Engine) multiLevelJudgment(mr *multiLevelResult, timeframes map[string]string) []Signal {
	tradeResult, ok := mr.Results["trade"]
	if !ok || tradeResult == nil {
		return nil
	}

	signals := tradeResult.Signals
	if len(signals) == 0 {
		return nil
	}

	// 高级别趋势过滤
	if higherResult, ok := mr.Results["higher"]; ok {
		for i := range signals {
			aligned := (signals[i].Direction == "long" && higherResult.Trend == "up_trend") ||
				(signals[i].Direction == "short" && higherResult.Trend == "down_trend")
			if aligned {
				signals[i].Confidence = min(signals[i].Confidence+15, 100)
			} else if higherResult.Trend != "consolidation" && higherResult.Trend != "unknown" {
				signals[i].Confidence = max(signals[i].Confidence-30, 0)
			}
		}
	}

	// 次级别确认加分
	if subResult, ok := mr.Results["sub"]; ok && len(subResult.Signals) > 0 {
		for i := range signals {
			for _, subSig := range subResult.Signals {
				if subSig.Direction == signals[i].Direction {
					signals[i].Confidence = min(signals[i].Confidence+10, 100)
					break
				}
			}
		}
	}

	// 过滤低置信度
	var filtered []Signal
	for _, s := range signals {
		if s.Confidence >= 50 {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func (e *Engine) signalToDecision(ctx *decision.Context, symbol string, sig Signal, timeframe string, decisionCloseTimes ...int64) decision.Decision {
	action := ""
	switch {
	case strings.HasPrefix(sig.SignalType, "buy") || strings.HasPrefix(sig.SignalType, "quasi_buy"):
		action = "open_long"
	case strings.HasPrefix(sig.SignalType, "sell") || strings.HasPrefix(sig.SignalType, "quasi_sell"):
		action = "open_short"
	}
	if action == "" {
		return decision.Decision{}
	}

	leverage := ctx.AltcoinLeverage
	if symbol == "BTCUSDT" || symbol == "ETHUSDT" {
		leverage = ctx.BTCETHLeverage
	}
	signalID := v2SignalID(symbol, timeframe, sig)
	closeTime := normalizeV2EpochMillis(sig.Timestamp)
	decisionCloseTime := closeTime
	if len(decisionCloseTimes) > 0 && decisionCloseTimes[0] > 0 {
		decisionCloseTime = decisionCloseTimes[0]
	}
	if decisionCloseTime > 0 && closeTime > 0 && decisionCloseTime < closeTime {
		decisionCloseTime = closeTime
	}

	return decision.Decision{
		Symbol:          symbol,
		Action:          action,
		Leverage:        leverage,
		StopLoss:        sig.StopLoss,
		TakeProfit:      sig.TakeProfit,
		Confidence:      sig.Confidence,
		Reasoning:       fmt.Sprintf("缠论V2 %s 置信度%d 背驰强度%.2f", sig.SignalType, sig.Confidence, sig.DivergenceStrength),
		StrategyMode:    "chanlun_v2",
		StrategyName:    "chanlun_v2",
		StrategyVersion: "v0.1",
		ConfigHash:      e.configHash,
		SignalID:        signalID,
		SignalType:      sig.SignalType,
		SignalTimeframe: timeframe,
		StructureTarget: sig.TakeProfit,
		StrategyMetadata: map[string]any{
			"layer":                 "trade_action",
			"timeframe":             timeframe,
			"trade_intent":          action,
			"signal_close_time":     closeTime,
			"decision_close_time":   decisionCloseTime,
			"evaluation_close_time": decisionCloseTime,
			"divergence_strength":   sig.DivergenceStrength,
			"center_id":             signalCenterID(sig),
			"reason_code":           "chanlun_v2_signal",
		},
	}
}

func (e *Engine) downgradeExpiredChanlunV2Signal(ctx *decision.Context, d decision.Decision, tradeTF string) (bool, string) {
	if !decision.IsOpenLikeAction(d.Action) {
		return false, ""
	}
	policy := config.NormalizeChanlunV2SignalFreshness(e.Config.SignalFreshness)
	if policy.Enabled != nil && !*policy.Enabled {
		return false, ""
	}
	d, eval := e.enrichChanlunV2FreshnessMetadata(ctx, d, policy, tradeTF)
	if eval.FreshnessState != "expired" {
		return false, ""
	}

	reasonCode := "freshness_gate.signal_expired"
	reason := fmt.Sprintf("%s %s 被前置降级为过期诊断: 信号已过期，年龄%d根%s超过硬上限%d根，结构时间=%d 评估K线=%d",
		d.Symbol, d.Action, eval.AgeCandles, eval.TradeTimeframe, eval.MaxLifetimeCandles, eval.SignalCloseTime, eval.DecisionCloseTime)
	d.StrategyMetadata["guard_reason_code"] = reasonCode
	d.StrategyMetadata["reason_code"] = reasonCode
	d.StrategyMetadata["stale_reason"] = reason
	d.StrategyMetadata["action_timestamp"] = time.Now().UnixMilli()

	rejection := decision.NewOpenRejectionFromDecision(d, reason)
	rejection.GateState = "blocked"
	rejection.GateReasons = []string{reasonCode}
	rejection.GateDiagnostics = map[string]any{
		"source":                "freshness_gate",
		"reason_code":           reasonCode,
		"freshness_state":       eval.FreshnessState,
		"age_candles":           eval.AgeCandles,
		"soft_age_candles":      eval.SoftAgeCandles,
		"max_lifetime_candles":  eval.MaxLifetimeCandles,
		"signal_close_time":     eval.SignalCloseTime,
		"decision_close_time":   eval.DecisionCloseTime,
		"evaluation_close_time": eval.EvaluationCloseTime,
		"current_price":         eval.CurrentPrice,
		"min_remaining_net_rr":  eval.MinRemainingNetRR,
		"downgraded":            true,
	}
	e.markDiagnosticRejectedOpenMarker(ctx, d, rejection, reason)

	suppressed, diagnostic := e.rememberOrSuppressTerminalFreshnessRejection(ctx, d, reasonCode, eval.FreshnessState, eval.SignalCloseTime, eval.DecisionCloseTime, reason)
	if suppressed && diagnostic != "" {
		return true, diagnostic
	}
	label := firstNonEmptyString(d.SignalType, d.Action, "signal")
	return true, fmt.Sprintf("%s %s 已降级为过期诊断: %s 年龄%d根%s超过硬上限%d根",
		market.Normalize(d.Symbol), label, reasonCode, eval.AgeCandles, eval.TradeTimeframe, eval.MaxLifetimeCandles)
}

func (e *Engine) enrichChanlunV2FreshnessMetadata(ctx *decision.Context, d decision.Decision, policy config.ChanlunV2SignalFreshnessConfig, tradeTF string) (decision.Decision, chanlunV2FreshnessEvaluation) {
	tradeTF = firstNonEmptyString(tradeTF, "1h")
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	evaluationTF := tradeTF
	if metadataString(d.StrategyMetadata, "layer") == v2LayerEntryTrigger {
		evaluationTF = firstNonEmptyString(metadataString(d.StrategyMetadata, "entry_trigger_timeframe"), metadataString(d.StrategyMetadata, "timeframe"), d.SignalTimeframe, tradeTF)
	}
	signalClose := firstPositiveInt64(metadataInt64(d.StrategyMetadata, "entry_trigger_close_time"), metadataInt64(d.StrategyMetadata, "trigger_close_time"), metadataInt64(d.StrategyMetadata, "signal_close_time"), metadataInt64(d.StrategyMetadata, "segment_end_time"))
	decisionClose := firstPositiveInt64(metadataInt64(d.StrategyMetadata, "decision_close_time"), metadataInt64(d.StrategyMetadata, "evaluation_close_time"), signalClose)
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	ageCandles := signalAgeCandles(signalClose, decisionClose, evaluationTF)
	softAge, maxLifetime := e.chanlunV2FreshnessLimits(policy, d.SignalType)
	freshnessState := "fresh"
	if ageCandles > maxLifetime {
		freshnessState = "expired"
	} else if ageCandles > softAge {
		freshnessState = "aged"
	}
	currentPrice := currentPriceForV2Guard(ctx, d.Symbol, evaluationTF)
	minRR := e.minRemainingNetRR(ctx, policy)
	d.StrategyMetadata["signal_close_time"] = signalClose
	d.StrategyMetadata["decision_close_time"] = decisionClose
	d.StrategyMetadata["evaluation_close_time"] = decisionClose
	d.StrategyMetadata["freshness_state"] = freshnessState
	d.StrategyMetadata["age_candles"] = ageCandles
	d.StrategyMetadata["soft_age_candles"] = softAge
	d.StrategyMetadata["max_lifetime_candles"] = maxLifetime
	d.StrategyMetadata["current_price"] = currentPrice
	d.StrategyMetadata["min_remaining_net_rr"] = minRR
	return d, chanlunV2FreshnessEvaluation{
		TradeTimeframe:      evaluationTF,
		SignalCloseTime:     signalClose,
		DecisionCloseTime:   decisionClose,
		EvaluationCloseTime: decisionClose,
		AgeCandles:          ageCandles,
		SoftAgeCandles:      softAge,
		MaxLifetimeCandles:  maxLifetime,
		FreshnessState:      freshnessState,
		CurrentPrice:        currentPrice,
		MinRemainingNetRR:   minRR,
	}
}

func (e *Engine) managePositions(ctx *decision.Context, timeframes map[string]string) []decision.Decision {
	if len(ctx.Positions) == 0 {
		return nil
	}
	var decisions []decision.Decision
	for _, pos := range ctx.Positions {
		if d := e.evaluateV2PositionManagement(ctx, pos, timeframes); d.Action != "" {
			decisions = append(decisions, d)
		}
	}
	return decisions
}

func (e *Engine) applyChanlunV2FreshnessGuard(ctx *decision.Context, decisions []decision.Decision, timeframes map[string]string) ([]decision.Decision, []decision.OpenRejection, []string) {
	if len(decisions) == 0 {
		return decisions, nil, nil
	}
	policy := config.NormalizeChanlunV2SignalFreshness(e.Config.SignalFreshness)
	if policy.Enabled != nil && !*policy.Enabled {
		return decisions, nil, nil
	}
	tradeTF := firstNonEmptyString(timeframes["trade"], "1h")
	out := make([]decision.Decision, 0, len(decisions))
	var rejections []decision.OpenRejection
	var suppressed []string
	for _, d := range decisions {
		if !decision.IsOpenLikeAction(d.Action) {
			out = append(out, d)
			continue
		}
		d, eval := e.enrichChanlunV2FreshnessMetadata(ctx, d, policy, tradeTF)
		signalClose := eval.SignalCloseTime
		decisionClose := eval.DecisionCloseTime
		ageCandles := eval.AgeCandles
		softAge := eval.SoftAgeCandles
		maxLifetime := eval.MaxLifetimeCandles
		freshnessState := eval.FreshnessState
		evaluationTF := firstNonEmptyString(eval.TradeTimeframe, tradeTF)
		currentPrice := eval.CurrentPrice
		minRR := eval.MinRemainingNetRR

		if isSuppressed, diagnostic := e.suppressKnownTerminalFreshnessSignal(ctx, d, signalClose, decisionClose); isSuppressed {
			if diagnostic != "" {
				suppressed = append(suppressed, diagnostic)
			}
			continue
		}

		reject := func(state, reasonCode, reason string, extra map[string]any) {
			d.StrategyMetadata["freshness_state"] = state
			d.StrategyMetadata["guard_reason_code"] = reasonCode
			d.StrategyMetadata["reason_code"] = reasonCode
			d.StrategyMetadata["stale_reason"] = reason
			d.StrategyMetadata["action_timestamp"] = time.Now().UnixMilli()
			for key, value := range extra {
				d.StrategyMetadata[key] = value
			}
			rejection := decision.NewOpenRejectionFromDecision(d, reason)
			rejection.GateState = "blocked"
			rejection.GateReasons = []string{reasonCode}
			rejection.GateDiagnostics = map[string]any{
				"source":                "freshness_gate",
				"reason_code":           reasonCode,
				"freshness_state":       state,
				"age_candles":           ageCandles,
				"soft_age_candles":      softAge,
				"max_lifetime_candles":  maxLifetime,
				"signal_close_time":     signalClose,
				"decision_close_time":   decisionClose,
				"evaluation_close_time": decisionClose,
				"current_price":         currentPrice,
				"min_remaining_net_rr":  minRR,
			}
			for key, value := range extra {
				rejection.GateDiagnostics[key] = value
			}
			if terminalChanlunV2FreshnessReason(reasonCode) {
				if isSuppressed, diagnostic := e.rememberOrSuppressTerminalFreshnessRejection(ctx, d, reasonCode, state, signalClose, decisionClose, reason); isSuppressed {
					if diagnostic != "" {
						suppressed = append(suppressed, diagnostic)
					}
					return
				}
			}
			rejections = append(rejections, rejection)
		}

		if currentPrice > 0 && policy.MissedTargetGuard != nil && *policy.MissedTargetGuard {
			if targetCrossed(d.Action, currentPrice, d.TakeProfit) {
				reason := fmt.Sprintf("%s %s 被信号新鲜度门控拒绝: 目标已穿越，当前价%.6f 目标%.6f，信号年龄%d根%s", d.Symbol, d.Action, currentPrice, d.TakeProfit, ageCandles, evaluationTF)
				reject("target_crossed", "freshness_gate.target_crossed", reason, map[string]any{"target_crossed": true})
				continue
			}
		}
		if freshnessState == "expired" {
			reason := fmt.Sprintf("%s %s 被信号新鲜度门控拒绝: 信号已过期，年龄%d根%s超过硬上限%d根，结构时间=%d 评估K线=%d", d.Symbol, d.Action, ageCandles, evaluationTF, maxLifetime, signalClose, decisionClose)
			reject("expired", "freshness_gate.signal_expired", reason, nil)
			continue
		}
		if currentPrice > 0 {
			if remainingRR, ok := remainingNetRRForV2Decision(d.Action, currentPrice, d.StopLoss, d.TakeProfit, v2TradingCostPct(ctx)); ok {
				d.StrategyMetadata["remaining_net_rr"] = remainingRR
				if remainingRR < minRR {
					reason := fmt.Sprintf("%s %s 被信号新鲜度门控拒绝: 剩余净RR %.2f低于阈值%.2f，当前价%.6f 止损%.6f 止盈%.6f", d.Symbol, d.Action, remainingRR, minRR, currentPrice, d.StopLoss, d.TakeProfit)
					reject("rr_invalid", "freshness_gate.rr_invalid", reason, map[string]any{"remaining_net_rr": remainingRR})
					continue
				}
			}
		} else {
			d.StrategyMetadata["freshness_diagnostic"] = "缺少当前价，已跳过目标穿越和剩余RR检查"
		}
		if freshnessState == "aged" {
			agedReason := fmt.Sprintf("信号老化%d根%s，超过soft阈值%d根", ageCandles, evaluationTF, softAge)
			d.StrategyMetadata["stale_reason"] = agedReason
			decay := (ageCandles - softAge) * policy.ConfidenceDecayPerAgedCandle
			if decay > 0 {
				before := d.Confidence
				d.Confidence = max(0, d.Confidence-decay)
				d.StrategyMetadata["confidence_before_freshness_decay"] = before
				d.StrategyMetadata["confidence_decay"] = decay
				d.Reasoning += fmt.Sprintf("；%s，置信度衰减%d", agedReason, decay)
			}
		}
		out = append(out, d)
	}
	return out, rejections, suppressed
}

func terminalChanlunV2FreshnessReason(reasonCode string) bool {
	switch strings.ToLower(strings.TrimSpace(reasonCode)) {
	case "freshness_gate.signal_expired", "freshness_gate.target_crossed", "freshness_gate.rr_invalid":
		return true
	default:
		return false
	}
}

func chanlunV2StaleLifecycleKey(traderID, symbol, signalID string) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		market.Normalize(symbol),
		strings.TrimSpace(signalID),
	}, "|")
}

func chanlunV2StaleDedupeKey(traderID, symbol, signalID, reasonCode string, evaluationCloseTime int64) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		market.Normalize(symbol),
		strings.TrimSpace(signalID),
		strings.TrimSpace(reasonCode),
		fmt.Sprintf("%d", evaluationCloseTime),
	}, "|")
}

func (e *Engine) rememberOrSuppressTerminalFreshnessRejection(ctx *decision.Context, d decision.Decision, reasonCode, freshnessState string, signalClose, decisionClose int64, reason string) (bool, string) {
	if e == nil || !terminalChanlunV2FreshnessReason(reasonCode) || strings.TrimSpace(d.SignalID) == "" {
		return false, ""
	}
	traderID := ""
	if ctx != nil {
		traderID = ctx.TraderID
	}
	symbol := market.Normalize(d.Symbol)
	evaluationClose := firstPositiveInt64(metadataInt64(d.StrategyMetadata, "evaluation_close_time"), decisionClose)
	lifecycleKey := chanlunV2StaleLifecycleKey(traderID, symbol, d.SignalID)
	dedupeKey := chanlunV2StaleDedupeKey(traderID, symbol, d.SignalID, reasonCode, evaluationClose)
	now := time.Now().UnixMilli()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.staleSuppressions == nil {
		e.staleSuppressions = map[string]chanlunV2StaleSuppression{}
	}
	record, exists := e.staleSuppressions[lifecycleKey]
	if exists {
		record.LastSeenAt = now
		record.SuppressedCount++
		record.LastReason = firstNonEmptyString(reason, record.LastReason)
		if record.DedupeKeys == nil {
			record.DedupeKeys = map[string]bool{}
		}
		record.DedupeKeys[dedupeKey] = true
		e.staleSuppressions[lifecycleKey] = record
		label := firstNonEmptyString(d.SignalType, d.Action, "signal")
		return true, fmt.Sprintf("%s %s 重复过期信号已静默: %s 已静默%d次", symbol, label, firstNonEmptyString(reasonCode, record.ReasonCode), record.SuppressedCount)
	}
	e.staleSuppressions[lifecycleKey] = chanlunV2StaleSuppression{
		TraderID:            traderID,
		Symbol:              symbol,
		SignalID:            d.SignalID,
		ReasonCode:          reasonCode,
		FreshnessState:      freshnessState,
		SignalCloseTime:     signalClose,
		DecisionCloseTime:   decisionClose,
		EvaluationCloseTime: evaluationClose,
		FirstSeenAt:         now,
		LastSeenAt:          now,
		LastReason:          reason,
		DedupeKeys:          map[string]bool{dedupeKey: true},
	}
	return false, ""
}

func (e *Engine) suppressKnownTerminalFreshnessSignal(ctx *decision.Context, d decision.Decision, signalClose, decisionClose int64) (bool, string) {
	if e == nil || strings.TrimSpace(d.SignalID) == "" {
		return false, ""
	}
	traderID := ""
	if ctx != nil {
		traderID = ctx.TraderID
	}
	symbol := market.Normalize(d.Symbol)
	lifecycleKey := chanlunV2StaleLifecycleKey(traderID, symbol, d.SignalID)
	evaluationClose := firstPositiveInt64(metadataInt64(d.StrategyMetadata, "evaluation_close_time"), decisionClose)
	now := time.Now().UnixMilli()

	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.staleSuppressions) == 0 {
		return false, ""
	}
	record, exists := e.staleSuppressions[lifecycleKey]
	if !exists {
		return false, ""
	}
	record.LastSeenAt = now
	record.SuppressedCount++
	if record.DedupeKeys == nil {
		record.DedupeKeys = map[string]bool{}
	}
	record.DedupeKeys[chanlunV2StaleDedupeKey(traderID, symbol, d.SignalID, record.ReasonCode, evaluationClose)] = true
	if signalClose > 0 {
		record.SignalCloseTime = signalClose
	}
	if decisionClose > 0 {
		record.DecisionCloseTime = decisionClose
	}
	if evaluationClose > 0 {
		record.EvaluationCloseTime = evaluationClose
	}
	e.staleSuppressions[lifecycleKey] = record

	label := firstNonEmptyString(d.SignalType, d.Action, "signal")
	return true, fmt.Sprintf("%s %s 重复过期信号已静默: %s 已静默%d次", symbol, label, firstNonEmptyString(record.ReasonCode, "freshness_gate.terminal"), record.SuppressedCount)
}

func (e *Engine) validateChanlunV2Decisions(ctx *decision.Context, decisions []decision.Decision, prep *decision.CyclePreparation) ([]decision.Decision, []decision.OpenRejection) {
	if len(decisions) == 0 {
		return decisions, nil
	}

	riskReducing := make([]decision.Decision, 0, len(decisions))
	openLike := make([]decision.Decision, 0, len(decisions))
	for _, d := range decisions {
		if decision.IsOpenLikeAction(d.Action) {
			openLike = append(openLike, d)
			continue
		}
		riskReducing = append(riskReducing, d)
	}
	if len(openLike) == 0 {
		validRisk, riskRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: "chanlun_v2"})
		return validRisk, riskRejections
	}

	if prep != nil && prep.RiskIncreaseBlocked {
		validRisk, riskRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: "chanlun_v2"})
		rejections := make([]decision.OpenRejection, 0, len(openLike))
		reason := strings.TrimSpace(prep.StopReason)
		if reason == "" {
			reason = "风险增加已阻断"
		}
		for _, d := range openLike {
			rejections = append(rejections, decision.NewOpenRejectionFromDecision(d, fmt.Sprintf("%s %s 被拒绝: %s", d.Symbol, d.Action, reason)))
		}
		rejections = append(riskRejections, rejections...)
		return validRisk, rejections
	}

	validOpenLike, rejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
		Source: "chanlun_v2",
	})
	rejections = annotateChanlunV2SizingRejections(rejections)
	validRisk, riskRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: "chanlun_v2"})
	rejections = append(riskRejections, rejections...)
	valid := append(validRisk, validOpenLike...)
	return valid, rejections
}

func annotateChanlunV2SizingRejections(rejections []decision.OpenRejection) []decision.OpenRejection {
	for i := range rejections {
		reason := strings.ToLower(rejections[i].Reason)
		reasonCode := ""
		switch {
		case strings.Contains(reason, "仓位大小必须>0") || strings.Contains(reason, "position size"):
			reasonCode = "position_sizing.zero_quantity"
		case strings.Contains(reason, "最小下单额") || strings.Contains(reason, "低于最小"):
			reasonCode = "position_sizing.min_notional"
		case strings.Contains(reason, "保证金") || strings.Contains(reason, "margin"):
			reasonCode = "position_sizing.margin_insufficient"
		case strings.Contains(reason, "sizing不可执行"):
			reasonCode = "position_sizing.not_executable"
		}
		if reasonCode == "" {
			continue
		}
		if len(rejections[i].GateReasons) == 0 {
			rejections[i].GateReasons = []string{reasonCode}
		} else if !stringSliceContains(rejections[i].GateReasons, reasonCode) {
			rejections[i].GateReasons = append([]string{reasonCode}, rejections[i].GateReasons...)
		}
		if rejections[i].GateDiagnostics == nil {
			rejections[i].GateDiagnostics = map[string]any{}
		}
		rejections[i].GateDiagnostics["reason_code"] = reasonCode
		rejections[i].GateDiagnostics["source"] = "position_sizing"
		if rejections[i].StrategyMetadata == nil {
			rejections[i].StrategyMetadata = map[string]any{}
		}
		rejections[i].StrategyMetadata["reason_code"] = reasonCode
	}
	return rejections
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (e *Engine) marketHistoryDepth(timeframes map[string]string) map[string]int {
	depth := map[string]int{}
	for _, tf := range timeframes {
		if tf != "" {
			depth[tf] = 240
		}
	}
	for tf, d := range e.Config.HistoryDepth {
		if tf != "" && d > 0 {
			depth[tf] = d
		}
	}
	return depth
}

func chanlunV2OpenRejectionReasons(rejections []decision.OpenRejection) []string {
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

func (e *Engine) resolveTimeframes() map[string]string {
	tf := map[string]string{"higher": "4h", "trade": "1h", "sub": "15m", "micro": "3m"}
	if e.Config.Timeframes != nil {
		for k, v := range e.Config.Timeframes {
			tf[k] = v
		}
	}
	return tf
}

func (e *Engine) resolveSymbols(ctx *decision.Context) []string {
	return strategySymbolNames(e.resolveSymbolUniverse(ctx))
}

func latestKlineCloseMillis(klines []market.Kline) int64 {
	if len(klines) == 0 {
		return 0
	}
	return normalizeV2EpochMillis(klines[len(klines)-1].CloseTime)
}

func evaluationCloseTime(mr *multiLevelResult, level string) int64 {
	if mr == nil {
		return 0
	}
	if value := mr.LastClosedByLevel[level]; value > 0 {
		return normalizeV2EpochMillis(value)
	}
	return 0
}

func signalAgeCandles(signalClose, decisionClose int64, timeframe string) int {
	if signalClose <= 0 || decisionClose <= signalClose {
		return 0
	}
	step := timeframeDurationMillis(timeframe)
	if step <= 0 {
		return 0
	}
	return int((decisionClose - signalClose) / step)
}

func timeframeDurationMillis(timeframe string) int64 {
	duration, err := time.ParseDuration(strings.TrimSpace(timeframe))
	if err != nil || duration <= 0 {
		return int64(time.Hour / time.Millisecond)
	}
	return int64(duration / time.Millisecond)
}

func (e *Engine) chanlunV2FreshnessLimits(policy config.ChanlunV2SignalFreshnessConfig, signalType string) (int, int) {
	soft := policy.SoftAgeCandles
	if value := policy.SoftAgeBySignalType[strings.ToLower(strings.TrimSpace(signalType))]; value > 0 {
		soft = value
	}
	maxLifetime := policy.MaxLifetimeCandles
	if value := policy.MaxLifetimeBySignalType[strings.ToLower(strings.TrimSpace(signalType))]; value > 0 {
		maxLifetime = value
	}
	if soft <= 0 {
		soft = 1
	}
	if maxLifetime < soft {
		maxLifetime = soft
	}
	return soft, maxLifetime
}

func (e *Engine) minRemainingNetRR(ctx *decision.Context, policy config.ChanlunV2SignalFreshnessConfig) float64 {
	if policy.MinRemainingNetRR > 0 {
		return policy.MinRemainingNetRR
	}
	if ctx != nil && ctx.StrategyRiskPolicy != nil && ctx.StrategyRiskPolicy.DefaultMinNetRR > 0 {
		return ctx.StrategyRiskPolicy.DefaultMinNetRR
	}
	return 1.2
}

func currentPriceForV2Guard(ctx *decision.Context, symbol, timeframe string) float64 {
	if ctx == nil || ctx.MarketDataMap == nil {
		return 0
	}
	data := ctx.MarketDataMap[market.Normalize(symbol)]
	if data == nil {
		data = ctx.MarketDataMap[symbol]
	}
	if data == nil {
		return 0
	}
	if data.CurrentPrice > 0 {
		return data.CurrentPrice
	}
	if data.Klines != nil {
		if klines := data.Klines[timeframe]; len(klines) > 0 {
			return klines[len(klines)-1].Close
		}
	}
	return 0
}

func targetCrossed(action string, currentPrice, takeProfit float64) bool {
	if currentPrice <= 0 || takeProfit <= 0 {
		return false
	}
	switch decision.DecisionDirection(action) {
	case "long":
		return currentPrice >= takeProfit
	case "short":
		return currentPrice <= takeProfit
	default:
		return false
	}
}

func remainingNetRRForV2Decision(action string, currentPrice, stopLoss, takeProfit, costPct float64) (float64, bool) {
	if currentPrice <= 0 || stopLoss <= 0 || takeProfit <= 0 {
		return 0, false
	}
	var risk, reward float64
	switch decision.DecisionDirection(action) {
	case "long":
		risk = currentPrice - stopLoss
		reward = takeProfit - currentPrice
	case "short":
		risk = stopLoss - currentPrice
		reward = currentPrice - takeProfit
	default:
		return 0, false
	}
	if risk <= 0 || reward <= 0 {
		return 0, true
	}
	net := reward/risk - costPct
	if net < 0 {
		return 0, true
	}
	return net, true
}

func v2TradingCostPct(ctx *decision.Context) float64 {
	if ctx != nil && ctx.StrategyRiskPolicy != nil && ctx.StrategyRiskPolicy.FeeSlippagePct > 0 {
		return ctx.StrategyRiskPolicy.FeeSlippagePct
	}
	return 0.002
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
