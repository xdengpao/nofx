package backtest

import "fmt"

type StressTester struct {
	Delta             float64
	ShockIndex        int
	StrategyEvaluator PathEvaluator
}

type StressTestResult struct {
	Delta              float64 `json:"delta"`
	ShockIndex         int     `json:"shock_index"`
	BaseTerminalValue  float64 `json:"base_terminal_value"`
	ShockTerminalValue float64 `json:"shock_terminal_value"`
	MaxDrawdownPct     float64 `json:"max_drawdown_pct"`
	SurvivalRate       float64 `json:"survival_rate"`
	SharpeChange       float64 `json:"sharpe_change"`
}

func (s StressTester) Test(prices []float64, initialValue float64) (StressTestResult, error) {
	clean := positiveSeries(prices)
	if len(clean) < 2 {
		return StressTestResult{}, fmt.Errorf("压力测试至少需要2个正价格样本")
	}
	if initialValue <= 0 {
		return StressTestResult{}, fmt.Errorf("initial_value必须大于0")
	}
	delta := s.Delta
	if delta <= 0 {
		delta = 0.3
	}
	if delta > 1 {
		return StressTestResult{}, fmt.Errorf("delta必须在(0,1]范围内")
	}
	shockIndex := s.ShockIndex
	if shockIndex <= 0 || shockIndex >= len(clean) {
		shockIndex = len(clean) / 2
	}
	evaluator := s.StrategyEvaluator
	if evaluator == nil {
		evaluator = terminalValueEvaluator
	}
	shocked := append([]float64(nil), clean...)
	for i := shockIndex; i < len(shocked); i++ {
		shocked[i] *= 1 - delta
	}
	baseTerminal := evaluator(clean, initialValue)
	shockTerminal := evaluator(shocked, initialValue)
	baseReturns := logReturns(clean)
	shockReturns := logReturns(shocked)
	return StressTestResult{
		Delta:              delta,
		ShockIndex:         shockIndex,
		BaseTerminalValue:  baseTerminal,
		ShockTerminalValue: shockTerminal,
		MaxDrawdownPct:     priceMaxDrawdown(shocked),
		SurvivalRate:       boolRate(shockTerminal > 0),
		SharpeChange:       sharpeRatio(shockReturns) - sharpeRatio(baseReturns),
	}, nil
}

func priceMaxDrawdown(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	peak := prices[0]
	maxDD := 0.0
	for _, price := range prices {
		if price > peak {
			peak = price
		}
		if peak > 0 {
			dd := (peak - price) / peak * 100
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func boolRate(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}
