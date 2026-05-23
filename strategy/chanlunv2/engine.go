package chanlunv2

import (
	"fmt"
	"log"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"strings"
	"time"
)

// Engine 缠论V2策略引擎
type Engine struct {
	Config config.ChanlunV2StrategyConfig
}

// NewEngine 创建缠论V2引擎
func NewEngine(cfg config.ChanlunV2StrategyConfig) (*Engine, error) {
	return &Engine{Config: cfg}, nil
}

// GetFullDecision 实现 ChanlunV2EngineInterface
func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("缺少交易上下文")
	}
	now := time.Now()

	timeframes := e.resolveTimeframes()
	symbols := e.resolveSymbols(ctx)

	var allDecisions []decision.Decision
	var diagnostics []string

	// 对每个标的进行多级别分析
	for _, symbol := range symbols {
		data := ctx.MarketDataMap[symbol]
		if data == nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 数据不足", symbol))
			continue
		}

		// 多级别分析
		multiResult := e.analyzeMultiLevel(symbol, data, timeframes)
		if multiResult == nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 多级别分析无结果", symbol))
			continue
		}

		// 级别联立产出信号
		signals := e.multiLevelJudgment(multiResult, timeframes)
		if len(signals) == 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 无买卖点信号", symbol))
			continue
		}

		// 信号转 Decision
		for _, sig := range signals {
			d := e.signalToDecision(ctx, symbol, sig)
			if d.Action != "" {
				allDecisions = append(allDecisions, d)
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s 置信度%d", symbol, sig.SignalType, sig.Confidence))
			}
		}
	}

	// 持仓管理
	posDecisions := e.managePositions(ctx, timeframes)
	allDecisions = append(posDecisions, allDecisions...)

	if len(allDecisions) == 0 {
		allDecisions = []decision.Decision{{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: "缠论V2策略未发现可执行信号",
		}}
	}

	summary := "缠论V2策略周期完成"
	if len(diagnostics) > 0 {
		summary += ": " + strings.Join(diagnostics, "; ")
	}

	return &decision.FullDecision{
		CoTTrace:        summary,
		Decisions:       allDecisions,
		Timestamp:       now,
		DecisionMode:    "chanlun_v2",
		StrategyName:    "chanlun_v2",
		StrategyVersion: "v0.1",
		StrategyDiagnostics: map[string]any{
			"messages":   diagnostics,
			"timeframes": timeframes,
			"symbols":    symbols,
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

func (e *Engine) signalToDecision(ctx *decision.Context, symbol string, sig Signal) decision.Decision {
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

	return decision.Decision{
		Symbol:     symbol,
		Action:     action,
		Leverage:   leverage,
		StopLoss:   sig.StopLoss,
		TakeProfit: sig.TakeProfit,
		Confidence: sig.Confidence,
		Reasoning:  fmt.Sprintf("缠论V2 %s 置信度%d 背驰强度%.2f", sig.SignalType, sig.Confidence, sig.DivergenceStrength),
	}
}

func (e *Engine) managePositions(ctx *decision.Context, timeframes map[string]string) []decision.Decision {
	if len(ctx.Positions) == 0 {
		return nil
	}
	var decisions []decision.Decision
	for _, pos := range ctx.Positions {
		data := ctx.MarketDataMap[pos.Symbol]
		if data == nil {
			continue
		}
		mr := e.analyzeMultiLevel(pos.Symbol, data, timeframes)
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
					Symbol:    pos.Symbol,
					Action:    closeAction,
					Reasoning: fmt.Sprintf("缠论V2反向信号 %s 置信度%d", sig.SignalType, sig.Confidence),
				})
				break
			}
		}
	}
	return decisions
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
	var symbols []string
	for _, c := range ctx.CandidateCoins {
		symbols = append(symbols, c.Symbol)
	}
	if len(symbols) > 10 {
		symbols = symbols[:10]
	}
	return symbols
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
