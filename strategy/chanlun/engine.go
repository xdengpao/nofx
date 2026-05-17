package chanlun

import (
	"fmt"
	"log"
	"nofx/decision"
	"nofx/market"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	Policy     decision.ProgrammaticStrategyPolicy
	StateStore *StateStore
	Clock      func() time.Time

	mu             sync.RWMutex
	latestSignals  map[string]*SignalReport
	symbolUniverse map[string][]StrategySymbol
}

func NewEngine(policy decision.ProgrammaticStrategyPolicy) (*Engine, error) {
	if policy.DecisionMode == "" {
		policy.DecisionMode = "programmatic"
	}
	if policy.StrategyName == "" {
		policy.StrategyName = "chanlun_programmatic"
	}
	if policy.StrategyVersion == "" {
		policy.StrategyVersion = "v1"
	}
	if policy.ConfigHash == "" {
		policy.ConfigHash = "default"
	}
	if policy.State.Path == "" {
		policy.State.Path = "data/programmatic_strategy_state.json"
	}
	if policy.Timeframes.Trade == "" {
		policy.Timeframes = decision.ProgrammaticTimeframesPolicy{Higher: "4h", Trade: "1h", Sub: "15m", Micro: "3m"}
	}
	if policy.PositionManagement.Timeframes.Structure == "" {
		policy.PositionManagement = defaultPositionManagementPolicy(policy.Position.PartialClosePct)
	}
	engine := &Engine{
		Policy:         policy,
		StateStore:     NewStateStore(policy.State.Path),
		Clock:          time.Now,
		latestSignals:  map[string]*SignalReport{},
		symbolUniverse: map[string][]StrategySymbol{},
	}
	return engine, nil
}

func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("缺少交易上下文")
	}
	now := e.now()
	universe := ResolveProgrammaticSymbols(ctx.CandidateCoins, ctx.Positions, e.Policy)
	marketSymbols := make([]string, 0, len(universe))
	for _, item := range universe {
		marketSymbols = append(marketSymbols, item.Symbol)
	}
	e.setUniverse(ctx.TraderID, universe)

	prep, err := decision.PrepareCycleContext(ctx, decision.CyclePreparationOptions{
		MarketSymbols: marketSymbols,
		MarketHistoryDepth: map[string]int{
			"3m":  e.Policy.HistoryDepth.M3,
			"15m": e.Policy.HistoryDepth.M15,
			"1h":  e.Policy.HistoryDepth.H1,
			"4h":  e.Policy.HistoryDepth.H4,
		},
		ClosedKlinesOnly:        true,
		IncludeMicroADX:         e.Policy.ADX.MicroADXFilter,
		AllowRiskReducingOnHalt: true,
	})
	if err != nil {
		return nil, err
	}
	if prep.FullStop && prep.HaltDecision != nil {
		prep.HaltDecision.UserPrompt = ""
		prep.HaltDecision.AICallAttempted = false
		e.applyDecisionMetadata(prep.HaltDecision, nil)
		return prep.HaltDecision, nil
	}

	var strategyDecisions []decision.Decision
	var diagnostics []string
	positionDecisions, positionDiagnostics := e.evaluatePositionManagement(ctx, now)
	strategyDecisions = append(strategyDecisions, positionDecisions...)
	diagnostics = append(diagnostics, positionDiagnostics...)
	if prep.RiskIncreaseBlocked {
		reason := prep.StopReason
		if strings.TrimSpace(reason) == "" {
			reason = "风险增加已阻断"
		}
		diagnostics = append(diagnostics, "主信号层跳过open/add: "+reason)
	} else {
		mainDecisions, mainDiagnostics := e.evaluateMainSignals(ctx, universe, now)
		strategyDecisions = append(strategyDecisions, mainDecisions...)
		diagnostics = append(diagnostics, mainDiagnostics...)
	}
	validDecisions, rejections := e.validateProgrammaticDecisions(ctx, strategyDecisions, prep)
	_ = e.StateStore.Save()
	allDecisions := decision.MergePublicAndStrategyDecisionsWithContext(ctx, prep.PositionDecisions, validDecisions)
	if len(allDecisions) == 0 {
		reason := "程序化策略未发现可执行信号"
		if prep.WaitDecision != nil && prep.WaitDecision.Reasoning != "" {
			reason = prep.WaitDecision.Reasoning
		}
		allDecisions = []decision.Decision{{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: reason,
		}}
	}
	summary := "程序化策略周期完成"
	if len(diagnostics) > 0 {
		summary += ": " + strings.Join(limitStrings(diagnostics, 8), "; ")
	}
	if len(rejections) > 0 {
		summary += "; 风控拒绝 " + strings.Join(openRejectionText(rejections), "; ")
	}
	fullDecision := &decision.FullDecision{
		UserPrompt:      "",
		CoTTrace:        summary,
		Decisions:       allDecisions,
		Timestamp:       now,
		AICallAttempted: false,
		AICallSucceeded: false,
		OpenRejections:  rejections,
	}
	e.applyDecisionMetadata(fullDecision, diagnostics)
	return fullDecision, nil
}

func (e *Engine) evaluateMainSignals(ctx *decision.Context, universe []StrategySymbol, now time.Time) ([]decision.Decision, []string) {
	var strategyDecisions []decision.Decision
	var diagnostics []string
	noNewClosedCount := 0
	for _, symbol := range universe {
		data := ctx.MarketDataMap[symbol.Symbol]
		if data == nil || len(data.Klines) == 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 数据不足", symbol.Symbol))
			continue
		}
		signals, diag := e.analyzeMainSignal(ctx.TraderID, symbol.Symbol, data, now)
		for _, msg := range diag {
			if strings.Contains(msg, "无新闭合K线") && !symbol.HasPosition {
				noNewClosedCount++
				continue
			}
			diagnostics = append(diagnostics, msg)
		}
		e.setLatestSignals(ctx.TraderID, symbol.Symbol, signals, diag)
		for _, signal := range signals {
			e.StateStore.StoreConfirmedSignal(ctx.TraderID, signal.Symbol, signal, false)
			d := e.signalToMainDecision(ctx, signal)
			if d.Action == "" {
				continue
			}
			if !e.StateStore.MarkExecuted(ctx.TraderID, signal.Symbol, signal.SignalID, d.Action) {
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s 已处理过signal_id=%s", signal.Symbol, signal.SignalType, signal.SignalID))
				continue
			}
			strategyDecisions = append(strategyDecisions, d)
		}
	}
	if noNewClosedCount > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("主信号层%d个无持仓候选等待%s新闭合K线", noNewClosedCount, e.Policy.Timeframes.Trade))
	}
	return strategyDecisions, diagnostics
}

func (e *Engine) validateProgrammaticDecisions(ctx *decision.Context, strategyDecisions []decision.Decision, prep *decision.CyclePreparation) ([]decision.Decision, []decision.OpenRejection) {
	var riskReducing []decision.Decision
	var openLike []decision.Decision
	for _, d := range strategyDecisions {
		if decision.IsOpenLikeAction(d.Action) {
			openLike = append(openLike, d)
			continue
		}
		riskReducing = append(riskReducing, d)
	}
	validRiskReducing, rrRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: "programmatic"})
	var rejections []decision.OpenRejection
	rejections = append(rejections, rrRejections...)
	if prep != nil && prep.RiskIncreaseBlocked {
		for _, d := range openLike {
			reason := fmt.Sprintf("%s %s 被拒绝: %s", d.Symbol, d.Action, prep.StopReason)
			rejections = append(rejections, decision.NewOpenRejectionFromDecision(d, reason))
		}
		e.markRejectedStrategyDecisions(ctx, strategyDecisions, validRiskReducing, rejections)
		return validRiskReducing, rejections
	}
	validOpenLike, openRejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
		Source:   "programmatic",
		AllowAdd: true,
	})
	rejections = append(rejections, openRejections...)
	valid := append(validRiskReducing, validOpenLike...)
	e.markRejectedStrategyDecisions(ctx, strategyDecisions, valid, rejections)
	return valid, rejections
}

func (e *Engine) markRejectedStrategyDecisions(ctx *decision.Context, candidates, valid []decision.Decision, rejections []decision.OpenRejection) {
	if ctx == nil {
		return
	}
	validIDs := map[string]bool{}
	for _, d := range valid {
		if d.SignalID != "" {
			validIDs[d.SignalID] = true
		}
	}
	reasonBySymbolAction := map[string]string{}
	reasonBySignalID := map[string]string{}
	for _, rejection := range rejections {
		if rejection.SignalID != "" {
			reasonBySignalID[rejection.SignalID] = rejection.Reason
		}
		reasonBySymbolAction[market.Normalize(rejection.Symbol)+"|"+rejection.Action] = rejection.Reason
	}
	for _, d := range candidates {
		if d.SignalID == "" || validIDs[d.SignalID] {
			continue
		}
		reason := reasonBySignalID[d.SignalID]
		if reason == "" {
			reason = reasonBySymbolAction[market.Normalize(d.Symbol)+"|"+d.Action]
		}
		if reason == "" {
			reason = "程序化动作被验证层拒绝"
		}
		if marker, ok := e.decisionToMarker(d, "rejected"); ok {
			marker.Reason = reason
			e.StateStore.StoreSignalMarker(ctx.TraderID, market.Normalize(d.Symbol), marker)
		} else {
			e.StateStore.UpdateSignalMarkerStatus(ctx.TraderID, market.Normalize(d.Symbol), d.SignalID, "rejected", reason)
		}
	}
}

func (e *Engine) applyDecisionMetadata(fullDecision *decision.FullDecision, diagnostics []string) {
	if fullDecision == nil {
		return
	}
	fullDecision.DecisionMode = "programmatic"
	fullDecision.StrategyName = e.Policy.StrategyName
	fullDecision.StrategyVersion = e.Policy.StrategyVersion
	fullDecision.ConfigHash = e.Policy.ConfigHash
	fullDecision.StrategyParams = map[string]any{
		"timeframes":          e.Policy.Timeframes,
		"history_depth":       e.Policy.HistoryDepth,
		"symbol_pool":         e.Policy.SymbolPool,
		"moving_average":      e.Policy.MovingAverage,
		"structure":           e.Policy.Structure,
		"divergence":          e.Policy.Divergence,
		"adx":                 e.Policy.ADX,
		"position":            e.Policy.Position,
		"position_management": e.Policy.PositionManagement,
		"take_profit":         e.Policy.TakeProfit,
	}
	if len(diagnostics) > 0 {
		mainMessages, positionMessages := splitLayerDiagnostics(diagnostics)
		fullDecision.StrategyDiagnostics = map[string]any{
			"messages": append([]string(nil), diagnostics...),
			"main_signal": map[string]any{
				"trade_timeframe": e.Policy.Timeframes.Trade,
				"next_close_time": nextCloseTime(e.now(), e.Policy.Timeframes.Trade).Format(time.RFC3339),
				"messages":        mainMessages,
			},
			"position_management": map[string]any{
				"enabled":  e.Policy.PositionManagement.Enabled,
				"messages": positionMessages,
			},
		}
	}
}

func (e *Engine) LatestSignals(traderID, symbol string) (*SignalReport, bool) {
	e.mu.RLock()
	report, ok := e.latestSignals[traderID+"|"+symbol]
	e.mu.RUnlock()
	if !ok {
		return nil, false
	}
	copied := *report
	copied.Signals = append([]ChanlunSignal{}, report.Signals...)
	copied.SignalMarkers = mergeSignalMarkers(e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers), report.SignalMarkers)
	return &copied, true
}

func (e *Engine) EmptySignalReport(traderID, symbol string) *SignalReport {
	symbol = market.Normalize(symbol)
	return &SignalReport{
		TraderID:           traderID,
		Symbol:             symbol,
		DecisionMode:       "programmatic",
		StrategyName:       e.Policy.StrategyName,
		StrategyVersion:    e.Policy.StrategyVersion,
		ConfigHash:         e.Policy.ConfigHash,
		TradeTimeframe:     e.Policy.Timeframes.Trade,
		ComponentTimeframe: e.Policy.Timeframes.Sub,
		MicroTimeframe:     e.Policy.Timeframes.Micro,
		Signals:            []ChanlunSignal{},
		SignalMarkers:      e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers),
		LatestDiagnostics: map[string]any{
			"messages": []string{"暂无该标的的程序化策略信号"},
		},
	}
}

type ProgrammaticExecutionResult struct {
	TraderID                 string
	Decision                 decision.Decision
	Success                  bool
	FinalAction              string
	RequestedClosePercentage float64
	ExecutedClosePercentage  float64
	ExecutedQuantity         float64
	PositionQuantityBefore   float64
	Price                    float64
	Error                    string
	ExecutedAt               time.Time
}

func (e *Engine) OnExecutionResult(result ProgrammaticExecutionResult) {
	d := result.Decision
	if d.StrategyMode != "programmatic" || d.SignalID == "" || d.Symbol == "" {
		return
	}
	symbol := market.Normalize(d.Symbol)
	status := "executed"
	if !result.Success {
		status = "failed"
	}
	reason := result.Error
	if reason == "" {
		reason = d.Reasoning
	}
	if marker, ok := e.decisionToMarker(d, status); ok {
		marker.FinalAction = result.FinalAction
		marker.TradeIntent = deriveTradeIntent(marker.Action, marker.FinalAction, marker.PositionSide, marker.Direction)
		marker.Reason = reason
		e.StateStore.StoreSignalMarker(result.TraderID, symbol, marker)
	}
	if !result.Success {
		_ = e.StateStore.Save()
		return
	}
	finalAction := firstNonEmptyString(result.FinalAction, d.Action)
	if finalAction == "hold" || finalAction == "partial_close_skipped" {
		e.StateStore.UpdateSignalMarkerStatus(result.TraderID, symbol, d.SignalID, "rejected", reason)
		_ = e.StateStore.Save()
		return
	}
	rule := metadataString(d.StrategyMetadata, "rule")
	side := metadataString(d.StrategyMetadata, "side")
	if side == "" {
		side = directionForAction(finalAction)
	}
	if side != "" && rule != "" {
		e.StateStore.MarkPositionSignal(result.TraderID, symbol, side, rule, d.SignalID)
	}
	switch finalAction {
	case "partial_close":
		if result.ExecutedQuantity > 0 || result.ExecutedClosePercentage > 0 {
			e.StateStore.RecordProgrammaticPartialClose(ProgrammaticPartialCloseRecord{
				TraderID:                 result.TraderID,
				Symbol:                   symbol,
				Side:                     side,
				Rule:                     rule,
				SignalID:                 d.SignalID,
				RequestedClosePercentage: result.RequestedClosePercentage,
				ExecutedClosePercentage:  result.ExecutedClosePercentage,
				ExecutedQuantity:         result.ExecutedQuantity,
				PositionQuantityBefore:   result.PositionQuantityBefore,
				Price:                    result.Price,
				Estimated:                result.ExecutedQuantity <= 0,
				ExecutedAt:               result.ExecutedAt,
				PeakPrice:                metadataFloat64(d.StrategyMetadata, "peak_price"),
				PeakPnLPct:               metadataFloat64(d.StrategyMetadata, "peak_pnl_pct"),
				PeakR:                    metadataFloat64(d.StrategyMetadata, "peak_r"),
			})
		}
	case "close_long", "close_short":
		e.StateStore.RecordProgrammaticFullClose(result.TraderID, symbol, side)
	}
	_ = e.StateStore.Save()
}

func metadataFloat64(values map[string]any, key string) float64 {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (e *Engine) SymbolUniverse(traderID string) []StrategySymbol {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]StrategySymbol(nil), e.symbolUniverse[traderID]...)
}

func (e *Engine) analyzeMainSignal(traderID, symbol string, data *market.Data, now time.Time) ([]ChanlunSignal, []string) {
	tradeTF := e.Policy.Timeframes.Trade
	subTF := ComponentTimeframe(tradeTF)
	if subTF == "" {
		subTF = e.Policy.Timeframes.Sub
	}
	tradeKlines := data.Klines[tradeTF]
	if len(tradeKlines) < 30 {
		return nil, []string{fmt.Sprintf("%s %s K线不足", symbol, tradeTF)}
	}
	lastClosed := tradeKlines[len(tradeKlines)-1].CloseTime
	symbolState := e.StateStore.SymbolState(traderID, symbol)
	if !e.Policy.State.Bootstrap && symbolState.LastAnalyzedClosedKline[tradeTF] == lastClosed {
		return nil, []string{fmt.Sprintf("%s %s 无新闭合K线", symbol, tradeTF)}
	}
	candles := marketKlinesToCandles(tradeTF, tradeKlines)
	normalized := NormalizeInclusion(candles)
	fractals := FindFractals(normalized, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
	strokes := BuildStrokes(fractals, normalized, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
	segments := BuildSegments(strokes, e.Policy.Structure.Strictness)
	centerSegments := segments
	if subTF != "" && len(data.Klines[subTF]) > 0 {
		subCandles := NormalizeInclusion(marketKlinesToCandles(subTF, data.Klines[subTF]))
		subFractals := FindFractals(subCandles, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
		subStrokes := BuildStrokes(subFractals, subCandles, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
		centerSegments = BuildSegments(subStrokes, e.Policy.Structure.Strictness)
	}
	centers := BuildCenters(centerSegments, tradeTF)
	if len(segments) < 3 {
		e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
		return nil, []string{fmt.Sprintf("%s 无足够走势段", symbol)}
	}
	hist := macdHistForTF(data, tradeTF)
	shortEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.ShortPeriod)
	longEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.LongPeriod)
	maKiss := DetectMAKiss(shortEMA, longEMA, e.Policy.MovingAverage.KissDistancePct, e.Policy.MovingAverage.WetKissBars)
	signals := DetectSignals(SignalInput{
		TraderID:          traderID,
		Symbol:            symbol,
		AnalysisTF:        tradeTF,
		TriggerTF:         e.Policy.Timeframes.Sub,
		Centers:           centers,
		Segments:          segments,
		MACDHist:          hist,
		ConfigHash:        e.Policy.ConfigHash,
		Now:               now,
		EnabledSignal:     enabledSignalMap(e.Policy.EnabledSignals),
		DivergenceRatio:   e.Policy.Divergence.Ratio,
		PriceTolerancePct: e.Policy.Divergence.PriceTolerancePct,
		RequireBZeroAxis:  e.Policy.Divergence.RequireBZeroAxis,
		MAKiss:            maKiss,
	})
	var timeDiagnostics []string
	for i := range signals {
		signalClose := signals[i].SignalCloseTime
		if signalClose == 0 {
			signalClose = signals[i].TriggerCloseTime
		}
		if signalClose == 0 {
			signalClose = signals[i].SegmentEndTime
		}
		signals[i].SignalCloseTime = signalClose
		signals[i].TriggerCloseTime = signalClose
		signals[i].DecisionCloseTime = lastClosed
		if signalClose > 0 && lastClosed > 0 && lastClosed < signalClose {
			timeDiagnostics = append(timeDiagnostics, fmt.Sprintf("%s %s 时间锚点异常: decision_close_time早于signal_close_time", symbol, signals[i].SignalType))
			signals[i].DecisionCloseTime = signalClose
		}
	}
	if len(signals) == 0 {
		e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
		return nil, []string{fmt.Sprintf("%s 无买卖点信号", symbol)}
	}
	e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
	diagnostics := []string{fmt.Sprintf("%s 识别到%d个信号", symbol, len(signals))}
	diagnostics = append(diagnostics, timeDiagnostics...)
	return signals, diagnostics
}

func (e *Engine) signalToMainDecision(ctx *decision.Context, signal ChanlunSignal) decision.Decision {
	action := ""
	positionSide := positionSideForSymbol(ctx.Positions, signal.Symbol)
	switch {
	case positionSide == "" && signal.Direction == SideLong && e.Policy.AllowLong:
		action = "open_long"
	case positionSide == "" && signal.Direction == SideShort && e.Policy.AllowShort:
		action = "open_short"
	case positionSide == SideLong && signal.Direction == SideLong:
		action = "add_long"
	case positionSide == SideShort && signal.Direction == SideShort:
		action = "add_short"
	}
	if action == "" {
		return decision.Decision{}
	}
	signalClose := signal.SignalCloseTime
	if signalClose == 0 {
		signalClose = signal.TriggerCloseTime
	}
	if signalClose == 0 {
		signalClose = signal.SegmentEndTime
	}
	decisionClose := signal.DecisionCloseTime
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	d := decision.Decision{
		Symbol:          signal.Symbol,
		Action:          action,
		Leverage:        leverageForSymbol(ctx, signal.Symbol),
		StopLoss:        signal.StopLoss,
		TakeProfit:      signal.TakeProfit,
		Confidence:      signal.Confidence,
		Reasoning:       fmt.Sprintf("程序化缠论%s信号: %s %s", signal.SignalType, signal.AnalysisTF, signal.CenterID),
		PositionSizeUSD: 0,
		StrategyMode:    "programmatic",
		StrategyName:    e.Policy.StrategyName,
		StrategyVersion: e.Policy.StrategyVersion,
		ConfigHash:      e.Policy.ConfigHash,
		SignalID:        signal.SignalID,
		SignalType:      signal.SignalType,
		SignalTimeframe: signal.AnalysisTF,
		StructureTarget: signal.StructureTarget,
		StrategyMetadata: map[string]any{
			"layer":               "main_signal",
			"rule":                signal.SignalType,
			"signal_type":         signal.SignalType,
			"center_id":           signal.CenterID,
			"trigger_timeframe":   signal.TriggerTF,
			"level":               signal.Level,
			"signal_close_time":   signalClose,
			"decision_close_time": decisionClose,
			"trigger_close_time":  signalClose,
			"segment_start_time":  signal.SegmentStartTime,
			"segment_end_time":    signal.SegmentEndTime,
			"trade_intent":        action,
		},
		StrategyDiagnosis: map[string]any{
			"diagnostics": signal.Diagnostics,
		},
	}
	d.Explanation = &decision.DecisionExplanation{
		Summary:        d.Reasoning,
		Layer:          "main_signal",
		Rule:           signal.SignalType,
		ReasonCode:     "chanlun_signal_detected",
		Timeframe:      signal.AnalysisTF,
		SignalType:     signal.SignalType,
		SignalID:       signal.SignalID,
		TriggerPrice:   signal.Price,
		ReferencePrice: signal.StructureTarget,
		Details: map[string]any{
			"center_id":           signal.CenterID,
			"trigger_timeframe":   signal.TriggerTF,
			"signal_close_time":   signalClose,
			"decision_close_time": decisionClose,
			"trigger_close_time":  signalClose,
			"segment_start_time":  signal.SegmentStartTime,
			"segment_end_time":    signal.SegmentEndTime,
			"trade_intent":        action,
			"level":               signal.Level,
		},
	}
	if action == "partial_close" {
		d.ClosePercentage = e.Policy.Position.PartialClosePct
	}
	if decision.IsAddAction(action) {
		if value := positionValueForSymbolSide(ctx.Positions, signal.Symbol, signal.Direction); value > 0 {
			multiplier := e.Policy.Position.AddSizeMultiplier
			if multiplier <= 0 {
				multiplier = 0.5
			}
			d.PositionSizeUSD = value * multiplier
		}
	}
	return d
}

func isReduceSignal(signalType string) bool {
	switch signalType {
	case SignalBuy2, SignalBuy3, SignalSell2, SignalSell3:
		return true
	default:
		return false
	}
}

func ResolveProgrammaticSymbols(candidates []decision.CandidateCoin, positions []decision.PositionInfo, policy decision.ProgrammaticStrategyPolicy) []StrategySymbol {
	base := map[string]StrategySymbol{}
	for _, coin := range candidates {
		symbol := market.Normalize(coin.Symbol)
		base[symbol] = StrategySymbol{Symbol: symbol, Sources: append([]string(nil), coin.Sources...), Selected: true}
	}
	custom := map[string]bool{}
	for _, symbol := range policy.SymbolPool.Symbols {
		custom[market.Normalize(symbol)] = true
	}
	core := map[string]bool{}
	for _, symbol := range policy.SymbolPool.CoreSymbols {
		core[market.Normalize(symbol)] = true
	}
	mode := policy.SymbolPool.Mode
	if mode == "" {
		mode = "append"
	}
	result := map[string]StrategySymbol{}
	switch mode {
	case "override":
		for symbol := range custom {
			result[symbol] = StrategySymbol{Symbol: symbol, Sources: []string{"custom"}, Selected: true}
		}
		for symbol := range core {
			result[symbol] = StrategySymbol{Symbol: symbol, Sources: []string{"core"}, Selected: true}
		}
	case "filter":
		for symbol, item := range base {
			if custom[symbol] {
				item.Sources = appendSource(item.Sources, "custom")
				result[symbol] = item
			}
		}
	default:
		for symbol, item := range base {
			result[symbol] = item
		}
		for symbol := range custom {
			item := result[symbol]
			item.Symbol = symbol
			item.Selected = true
			item.Sources = appendSource(item.Sources, "custom")
			result[symbol] = item
		}
	}
	for _, pos := range positions {
		symbol := market.Normalize(pos.Symbol)
		item := result[symbol]
		item.Symbol = symbol
		item.Selected = true
		item.HasPosition = true
		item.Sources = appendSource(item.Sources, "position")
		result[symbol] = item
	}
	symbols := make([]string, 0, len(result))
	for symbol := range result {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	out := make([]StrategySymbol, 0, len(symbols))
	for _, symbol := range symbols {
		out = append(out, result[symbol])
	}
	return out
}

func (e *Engine) setLatestSignals(traderID, symbol string, signals []ChanlunSignal, diagnostics []string) {
	markers := make([]SignalMarker, 0, len(signals))
	for _, signal := range signals {
		marker := signalToMarker(signal, "main_signal", "detected", "", "")
		markers = append(markers, marker)
		e.StateStore.StoreSignalMarker(traderID, symbol, marker)
	}
	markers = mergeSignalMarkers(e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers), markers)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.latestSignals[traderID+"|"+symbol] = &SignalReport{
		TraderID:           traderID,
		Symbol:             symbol,
		DecisionMode:       "programmatic",
		StrategyName:       e.Policy.StrategyName,
		StrategyVersion:    e.Policy.StrategyVersion,
		ConfigHash:         e.Policy.ConfigHash,
		TradeTimeframe:     e.Policy.Timeframes.Trade,
		ComponentTimeframe: e.Policy.Timeframes.Sub,
		MicroTimeframe:     e.Policy.Timeframes.Micro,
		Signals:            append([]ChanlunSignal{}, signals...),
		SignalMarkers:      markers,
		LatestDiagnostics: map[string]any{
			"messages": diagnostics,
		},
	}
}

func signalToMarker(signal ChanlunSignal, sourceLayer, status, action, reason string) SignalMarker {
	if sourceLayer == "" {
		sourceLayer = signal.SourceLayer
	}
	if sourceLayer == "" {
		sourceLayer = "main_signal"
	}
	if status == "" {
		status = signal.Status
	}
	if status == "" {
		status = "detected"
	}
	timeframe := signal.AnalysisTF
	if sourceLayer == "position_management" && signal.TriggerTF != "" {
		timeframe = signal.TriggerTF
	}
	closeTime := signal.SignalCloseTime
	if closeTime == 0 {
		closeTime = signal.TriggerCloseTime
	}
	if closeTime == 0 {
		closeTime = signal.SegmentEndTime
	}
	return SignalMarker{
		Symbol:           signal.Symbol,
		Timeframe:        timeframe,
		CloseTime:        closeTime,
		SignalCloseTime:  closeTime,
		DisplayCloseTime: closeTime,
		SignalType:       signal.SignalType,
		Direction:        signal.Direction,
		Level:            signal.Level,
		SourceLayer:      sourceLayer,
		Status:           status,
		SignalID:         signal.SignalID,
		Action:           action,
		TradeIntent:      deriveTradeIntent(action, "", "", signal.Direction),
		Price:            signal.Price,
		Reason:           reason,
	}
}

func (e *Engine) decisionToMarker(d decision.Decision, status string) (SignalMarker, bool) {
	if d.SignalID == "" || d.Symbol == "" {
		return SignalMarker{}, false
	}
	layer := metadataString(d.StrategyMetadata, "layer")
	if layer == "" {
		layer = "position_management"
	}
	rule := metadataString(d.StrategyMetadata, "rule")
	signalType := d.SignalType
	if signalType == "" {
		signalType = metadataString(d.StrategyMetadata, "signal_type")
	}
	if signalType == "" {
		signalType = rule
	}
	timeframe := d.SignalTimeframe
	if timeframe == "" {
		timeframe = metadataString(d.StrategyMetadata, "timeframe")
	}
	if timeframe == "" {
		timeframe = metadataString(d.StrategyMetadata, "structure_timeframe")
	}
	if timeframe == "" {
		timeframe = e.Policy.Timeframes.Trade
	}
	signalClose, _ := metadataInt64Any(d.StrategyMetadata, "signal_close_time", "trigger_close_time", "segment_end_time")
	decisionClose, _ := metadataInt64(d.StrategyMetadata, "decision_close_time")
	metadataDisplayClose, _ := metadataInt64(d.StrategyMetadata, "display_close_time")
	closeTime := signalClose
	displayClose := signalClose
	if metadataDisplayClose > 0 {
		displayClose = metadataDisplayClose
	}
	if isTradeActionMarker(d, status) && decisionClose > 0 {
		if signalClose == 0 || decisionClose >= signalClose {
			displayClose = decisionClose
		}
	}
	direction := metadataString(d.StrategyMetadata, "side")
	if direction == "" {
		direction = directionForAction(d.Action)
	}
	positionSide := derivePositionSide(d, "")
	tradeIntent := metadataString(d.StrategyMetadata, "trade_intent")
	if tradeIntent == "" {
		tradeIntent = deriveTradeIntent(d.Action, "", positionSide, direction)
	}
	price := d.StopLoss
	if d.Action == "update_stop_loss" {
		price = d.NewStopLoss
	}
	return SignalMarker{
		Symbol:            market.Normalize(d.Symbol),
		Timeframe:         timeframe,
		CloseTime:         closeTime,
		SignalCloseTime:   signalClose,
		DecisionCloseTime: decisionClose,
		DisplayCloseTime:  displayClose,
		SignalType:        signalType,
		Direction:         direction,
		Level:             timeframe,
		SourceLayer:       layer,
		Status:            status,
		SignalID:          d.SignalID,
		Action:            d.Action,
		TradeIntent:       tradeIntent,
		PositionSide:      positionSide,
		Price:             price,
		Reason:            d.Reasoning,
	}, true
}

func derivePositionSide(d decision.Decision, finalAction string) string {
	if side := normalizeSide(metadataString(d.StrategyMetadata, "side")); side != "" {
		return side
	}
	if side := normalizeSide(metadataString(d.StrategyMetadata, "position_side")); side != "" {
		return side
	}
	if side := directionForAction(finalAction); side != "" {
		return side
	}
	if side := directionForAction(d.Action); side != "" {
		return side
	}
	if side := normalizeSide(d.SignalType); side != "" {
		return side
	}
	return ""
}

func isTradeActionMarker(d decision.Decision, status string) bool {
	if strings.TrimSpace(d.Action) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "rejected", "executed", "failed":
		return true
	default:
		return false
	}
}

func deriveTradeIntent(action, finalAction, positionSide, direction string) string {
	effective := effectiveMarkerAction(action, finalAction)
	switch effective {
	case "open_long", "open_short", "add_long", "add_short", "close_long", "close_short":
		return effective
	case "partial_close_skipped":
		return "reduce_skipped"
	case "partial_close":
		side := normalizeSide(positionSide)
		if side == "" {
			side = normalizeSide(direction)
		}
		switch side {
		case SideLong:
			return "reduce_long"
		case SideShort:
			return "reduce_short"
		}
	}
	return ""
}

func effectiveMarkerAction(action, finalAction string) string {
	if finalAction != "" {
		return finalAction
	}
	return action
}

func normalizeSide(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SideLong, "buy", "bull", "bullish":
		return SideLong
	case SideShort, "sell", "bear", "bearish":
		return SideShort
	default:
		return ""
	}
}

func metadataInt64(values map[string]any, key string) (int64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	switch value := values[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		return int64(value), true
	default:
		return 0, false
	}
}

func metadataInt64Any(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := metadataInt64(values, key); ok && value != 0 {
			return value, true
		}
	}
	return 0, false
}

func directionForAction(action string) string {
	switch action {
	case "open_long", "add_long", "close_long":
		return SideLong
	case "open_short", "add_short", "close_short":
		return SideShort
	default:
		return ""
	}
}

func mergeSignalMarkers(first, second []SignalMarker) []SignalMarker {
	seen := map[string]bool{}
	out := make([]SignalMarker, 0, len(first)+len(second))
	for _, marker := range append(append([]SignalMarker(nil), first...), second...) {
		if marker.SignalID == "" {
			continue
		}
		key := signalMarkerKey(marker)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, marker)
	}
	if len(out) > maxRecentSignalMarkers {
		out = out[len(out)-maxRecentSignalMarkers:]
	}
	return out
}

func (e *Engine) setUniverse(traderID string, universe []StrategySymbol) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.symbolUniverse[traderID] = append([]StrategySymbol(nil), universe...)
}

func (e *Engine) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

func marketKlinesToCandles(timeframe string, klines []market.Kline) []Candle {
	candles := make([]Candle, 0, len(klines))
	for _, k := range klines {
		candles = append(candles, Candle{
			Timeframe: timeframe,
			OpenTime:  k.OpenTime,
			CloseTime: k.CloseTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
		})
	}
	return candles
}

func macdHistForTF(data *market.Data, timeframe string) []float64 {
	switch timeframe {
	case "15m":
		if data.MidTermSeries15m != nil {
			return data.MidTermSeries15m.MACDHist
		}
	case "1h":
		if data.MidTermSeries1h != nil {
			return data.MidTermSeries1h.MACDHist
		}
	case "4h":
		if data.LongerTermContext != nil {
			return data.LongerTermContext.MACDHist
		}
	}
	return nil
}

func enabledSignalMap(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func emaSeriesFromCandles(candles []Candle, period int) []float64 {
	if period <= 0 || len(candles) == 0 {
		return nil
	}
	alpha := 2.0 / float64(period+1)
	out := make([]float64, len(candles))
	out[0] = candles[0].Close
	for i := 1; i < len(candles); i++ {
		out[i] = alpha*candles[i].Close + (1-alpha)*out[i-1]
	}
	return out
}

func positionSideForSymbol(positions []decision.PositionInfo, symbol string) string {
	for _, pos := range positions {
		if market.Normalize(pos.Symbol) == market.Normalize(symbol) {
			return strings.ToLower(pos.Side)
		}
	}
	return ""
}

func positionValueForSymbolSide(positions []decision.PositionInfo, symbol, side string) float64 {
	for _, pos := range positions {
		if market.Normalize(pos.Symbol) != market.Normalize(symbol) || strings.ToLower(pos.Side) != side {
			continue
		}
		price := pos.MarkPrice
		if price <= 0 {
			price = pos.EntryPrice
		}
		return pos.Quantity * price
	}
	return 0
}

func leverageForSymbol(ctx *decision.Context, symbol string) int {
	if symbol == "BTCUSDT" || symbol == "ETHUSDT" {
		return ctx.BTCETHLeverage
	}
	return ctx.AltcoinLeverage
}

func appendSource(values []string, source string) []string {
	for _, value := range values {
		if value == source {
			return values
		}
	}
	return append(values, source)
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func splitLayerDiagnostics(values []string) ([]string, []string) {
	var mainMessages []string
	var positionMessages []string
	for _, value := range values {
		if strings.Contains(value, "持仓") || strings.Contains(value, "保本") || strings.Contains(value, "回撤") || strings.Contains(value, "结构") || strings.Contains(value, "短差") {
			positionMessages = append(positionMessages, value)
			continue
		}
		mainMessages = append(mainMessages, value)
	}
	return mainMessages, positionMessages
}

func nextCloseTime(now time.Time, timeframe string) time.Time {
	duration := time.Hour
	switch timeframe {
	case "15m":
		duration = 15 * time.Minute
	case "4h":
		duration = 4 * time.Hour
	}
	truncated := now.Truncate(duration)
	if truncated.Equal(now) {
		return now
	}
	return truncated.Add(duration)
}

func openRejectionText(rejections []decision.OpenRejection) []string {
	result := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		text := rejection.Reason
		if text == "" {
			text = strings.Join(rejection.GateReasons, ",")
		}
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func init() {
	log.SetFlags(log.Flags())
}
