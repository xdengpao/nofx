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
	Config         config.ChanlunV2StrategyConfig
	mu             sync.RWMutex
	latestSignals  map[string]*chanlunSignalReport
	symbolUniverse map[string][]chanlunStrategySymbol
	configHash     string
}

// NewEngine 创建缠论V2引擎
func NewEngine(cfg config.ChanlunV2StrategyConfig) (*Engine, error) {
	return &Engine{
		Config:         cfg,
		latestSignals:  map[string]*chanlunSignalReport{},
		symbolUniverse: map[string][]chanlunStrategySymbol{},
		configHash:     hashChanlunV2Config(cfg),
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

	// 对每个标的进行多级别分析
	for _, symbol := range symbols {
		symbolDiagnostics := []string{}
		// 直接获取 K 线数据（不依赖 ctx.MarketDataMap）
		multiResult := e.analyzeSymbol(symbol, timeframes)
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
		e.setLatestReport(ctx.TraderID, symbol, multiResult, signals, symbolDiagnostics)

		// 信号转 Decision
		for _, sig := range signals {
			d := e.signalToDecision(ctx, symbol, sig, timeframes["trade"])
			if d.Action != "" {
				allDecisions = append(allDecisions, d)
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s 置信度%d", symbol, sig.SignalType, sig.Confidence))
			}
		}
	}

	// 持仓管理
	posDecisions := e.managePositions(ctx, timeframes)
	for _, d := range posDecisions {
		e.appendDecisionMarker(ctx.TraderID, d)
	}
	allDecisions = append(posDecisions, allDecisions...)

	rawDecisions := append([]decision.Decision(nil), allDecisions...)
	openRejections := []decision.OpenRejection{}
	allDecisions, openRejections = e.validateChanlunV2Decisions(ctx, allDecisions, prep)
	e.markRejectedOpenMarkers(ctx, rawDecisions, allDecisions, openRejections)

	if len(allDecisions) == 0 {
		reason := "缠论V2策略未发现可执行信号"
		if len(openRejections) > 0 {
			reason = "缠论V2开仓信号已全部被风控过滤: " + strings.Join(chanlunV2OpenRejectionReasons(openRejections), "; ")
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
	if len(openRejections) > 0 {
		summary += "; 风控拒绝 " + strings.Join(chanlunV2OpenRejectionReasons(openRejections), "; ")
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
			"messages":        diagnostics,
			"timeframes":      timeframes,
			"symbols":         symbols,
			"open_rejections": chanlunV2OpenRejectionReasons(openRejections),
		},
	}, nil
}

type multiLevelResult struct {
	Symbol  string
	Results map[string]*AnalysisResult // timeframe → result
}

func (e *Engine) analyzeMultiLevel(symbol string, data *market.Data, timeframes map[string]string) *multiLevelResult {
	result := &multiLevelResult{Symbol: symbol, Results: map[string]*AnalysisResult{}}

	for level, tf := range timeframes {
		klines, ok := data.Klines[tf]
		if !ok || len(klines) < 30 {
			continue
		}
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

func (e *Engine) analyzeSymbol(symbol string, timeframes map[string]string) *multiLevelResult {
	result := &multiLevelResult{Symbol: symbol, Results: map[string]*AnalysisResult{}}

	for level, tf := range timeframes {
		depth := 240
		if d, ok := e.Config.HistoryDepth[tf]; ok && d > 0 {
			depth = d
		}
		klines, err := market.GetKlines(symbol, tf, depth, true)
		if err != nil || len(klines) < 30 {
			continue
		}
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

func (e *Engine) signalToDecision(ctx *decision.Context, symbol string, sig Signal, timeframe string) decision.Decision {
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
			"layer":               "trade_action",
			"timeframe":           timeframe,
			"signal_close_time":   closeTime,
			"decision_close_time": closeTime,
			"divergence_strength": sig.DivergenceStrength,
			"center_id":           signalCenterID(sig),
			"reason_code":         "chanlun_v2_signal",
		},
	}
}

func (e *Engine) managePositions(ctx *decision.Context, timeframes map[string]string) []decision.Decision {
	if len(ctx.Positions) == 0 {
		return nil
	}
	var decisions []decision.Decision
	for _, pos := range ctx.Positions {
		mr := e.analyzeSymbol(pos.Symbol, timeframes)
		if mr == nil {
			continue
		}
		tradeResult, ok := mr.Results["trade"]
		if !ok {
			continue
		}
		// 反向信号出现 → 平仓
		for _, sig := range tradeResult.Signals {
			if (pos.Side == "LONG" && sig.Direction == "short" && sig.Confidence >= 60) ||
				(pos.Side == "SHORT" && sig.Direction == "long" && sig.Confidence >= 60) {
				closeAction := "close_long"
				if pos.Side == "SHORT" {
					closeAction = "close_short"
				}
				decisions = append(decisions, decision.Decision{
					Symbol:          pos.Symbol,
					Action:          closeAction,
					Reasoning:       fmt.Sprintf("缠论V2反向信号 %s 置信度%d", sig.SignalType, sig.Confidence),
					StrategyMode:    "chanlun_v2",
					StrategyName:    "chanlun_v2",
					StrategyVersion: "v0.1",
					ConfigHash:      e.configHash,
					SignalID:        v2SignalID(pos.Symbol, timeframes["trade"], sig),
					SignalType:      sig.SignalType,
					SignalTimeframe: timeframes["trade"],
					StrategyMetadata: map[string]any{
						"layer":               "position_management",
						"timeframe":           timeframes["trade"],
						"signal_close_time":   normalizeV2EpochMillis(sig.Timestamp),
						"decision_close_time": normalizeV2EpochMillis(sig.Timestamp),
						"position_side":       strings.ToLower(pos.Side),
						"reason_code":         "chanlun_v2_reverse_signal",
					},
				})
				break
			}
		}
	}
	return decisions
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
		return decisions, nil
	}

	if prep != nil && prep.RiskIncreaseBlocked {
		rejections := make([]decision.OpenRejection, 0, len(openLike))
		reason := strings.TrimSpace(prep.StopReason)
		if reason == "" {
			reason = "风险增加已阻断"
		}
		for _, d := range openLike {
			rejections = append(rejections, decision.NewOpenRejectionFromDecision(d, fmt.Sprintf("%s %s 被拒绝: %s", d.Symbol, d.Action, reason)))
		}
		return riskReducing, rejections
	}

	validOpenLike, rejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
		Source: "chanlun_v2",
	})
	valid := append(riskReducing, validOpenLike...)
	return valid, rejections
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
