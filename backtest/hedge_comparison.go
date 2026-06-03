package backtest

import "fmt"

type HedgeComparisonResult struct {
	StablecoinRatio          float64 `json:"stablecoin_ratio"`
	PureMaxDrawdownPct       float64 `json:"pure_max_drawdown_pct"`
	HedgedMaxDrawdownPct     float64 `json:"hedged_max_drawdown_pct"`
	DrawdownReductionPct     float64 `json:"drawdown_reduction_pct"`
	PureTerminalValue        float64 `json:"pure_terminal_value"`
	HedgedTerminalValue      float64 `json:"hedged_terminal_value"`
	TerminalReturnDeltaValue float64 `json:"terminal_return_delta_value"`
}

func CompareStablecoinHedge(equity []EquityPoint, stablecoinRatio float64) (HedgeComparisonResult, error) {
	if len(equity) < 2 {
		return HedgeComparisonResult{}, fmt.Errorf("稳定币对冲比较至少需要2个权益点")
	}
	if stablecoinRatio <= 0 {
		stablecoinRatio = 0.3
	}
	if stablecoinRatio > 1 {
		return HedgeComparisonResult{}, fmt.Errorf("stablecoin_ratio必须在(0,1]范围内")
	}
	initial := equity[0].Equity
	if initial <= 0 {
		return HedgeComparisonResult{}, fmt.Errorf("初始权益必须大于0")
	}
	hedged := make([]EquityPoint, len(equity))
	stableValue := initial * stablecoinRatio
	riskValue := initial * (1 - stablecoinRatio)
	for i, point := range equity {
		ratio := 1.0
		if equity[0].Equity > 0 {
			ratio = point.Equity / equity[0].Equity
		}
		hedged[i] = point
		hedged[i].Equity = stableValue + riskValue*ratio
	}
	pureDD := maxDrawdown(equity)
	hedgedDD := maxDrawdown(hedged)
	reduction := 0.0
	if pureDD > 0 {
		reduction = (pureDD - hedgedDD) / pureDD * 100
	}
	pureTerminal := equity[len(equity)-1].Equity
	hedgedTerminal := hedged[len(hedged)-1].Equity
	return HedgeComparisonResult{
		StablecoinRatio:          stablecoinRatio,
		PureMaxDrawdownPct:       pureDD,
		HedgedMaxDrawdownPct:     hedgedDD,
		DrawdownReductionPct:     reduction,
		PureTerminalValue:        pureTerminal,
		HedgedTerminalValue:      hedgedTerminal,
		TerminalReturnDeltaValue: hedgedTerminal - pureTerminal,
	}, nil
}
