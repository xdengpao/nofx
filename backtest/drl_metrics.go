package backtest

import (
	"math"
	"nofx/config"
	"time"
)

// DRLBacktestMetrics 汇总DRL策略专用回测指标。
type DRLBacktestMetrics struct {
	AnnualizedReturnPct float64 `json:"annualized_return_pct"`
	SharpeRatio         float64 `json:"sharpe_ratio"`
	SortinoRatio        float64 `json:"sortino_ratio"`
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`
	WinRate             float64 `json:"win_rate"`
	ProfitLossRatio     float64 `json:"profit_loss_ratio"`
	DirectionAccuracy   float64 `json:"direction_accuracy"`
}

func buildDRLExtendedRiskForReport(mode string, cfg config.DRLStrategyConfig, equity []EquityPoint, initialValue float64) (*MonteCarloResult, *StressTestResult, *HedgeComparisonResult) {
	if mode != config.DecisionModeDRL {
		return nil, nil, nil
	}
	values := equityValues(equity)
	var mc *MonteCarloResult
	if cfg.MonteCarloEnabled {
		result, err := (MonteCarloSimulator{
			Paths:        cfg.MonteCarloPaths,
			HorizonSteps: len(values),
			Seed:         42,
		}).Simulate(values, initialValue)
		if err == nil {
			mc = &result
		}
	}
	var stress *StressTestResult
	if cfg.StressTestEnabled {
		result, err := (StressTester{Delta: cfg.StressTestDelta}).Test(values, initialValue)
		if err == nil {
			stress = &result
		}
	}
	var hedge *HedgeComparisonResult
	if cfg.StablecoinHedge {
		result, err := CompareStablecoinHedge(equity, cfg.StablecoinRatio)
		if err == nil {
			hedge = &result
		}
	}
	return mc, stress, hedge
}

func equityValues(equity []EquityPoint) []float64 {
	out := make([]float64, 0, len(equity))
	for _, point := range equity {
		if point.Equity > 0 {
			out = append(out, point.Equity)
		}
	}
	return out
}

func buildDRLMetricsForMode(mode string, equity []EquityPoint, trades []TradeLifecycle, from, to time.Time) *DRLBacktestMetrics {
	if mode != config.DecisionModeDRL {
		return nil
	}
	metrics := CalculateDRLBacktestMetrics(equity, trades, from, to)
	return &metrics
}

func CalculateDRLBacktestMetrics(equity []EquityPoint, trades []TradeLifecycle, from, to time.Time) DRLBacktestMetrics {
	returns := equityReturns(equity)
	totalReturn := 0.0
	if len(equity) >= 2 && equity[0].Equity > 0 {
		totalReturn = equity[len(equity)-1].Equity/equity[0].Equity - 1
	}
	years := to.Sub(from).Hours() / (24 * 365)
	annualized := 0.0
	if years > 0 && totalReturn > -1 {
		annualized = (math.Pow(1+totalReturn, 1/years) - 1) * 100
	}
	winRate, profitLossRatio := tradeWinLossStats(trades)
	return DRLBacktestMetrics{
		AnnualizedReturnPct: annualized,
		SharpeRatio:         sharpeRatio(returns),
		SortinoRatio:        sortinoRatio(returns),
		MaxDrawdownPct:      maxDrawdown(equity),
		WinRate:             winRate,
		ProfitLossRatio:     profitLossRatio,
		DirectionAccuracy:   winRate,
	}
}

func equityReturns(equity []EquityPoint) []float64 {
	if len(equity) < 2 {
		return nil
	}
	out := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		prev := equity[i-1].Equity
		if prev <= 0 {
			continue
		}
		out = append(out, equity[i].Equity/prev-1)
	}
	return out
}

func sharpeRatio(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	mean, std := meanStd(returns)
	if std <= 1e-12 {
		return 0
	}
	return mean / std * math.Sqrt(float64(len(returns)))
}

func sortinoRatio(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	mean, _ := meanStd(returns)
	var downside []float64
	for _, value := range returns {
		if value < 0 {
			downside = append(downside, value)
		}
	}
	_, downsideStd := meanStd(downside)
	if downsideStd <= 1e-12 {
		return 0
	}
	return mean / downsideStd * math.Sqrt(float64(len(returns)))
}

func meanStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	variance := 0.0
	for _, value := range values {
		diff := value - mean
		variance += diff * diff
	}
	return mean, math.Sqrt(variance / float64(len(values)))
}

func tradeWinLossStats(trades []TradeLifecycle) (float64, float64) {
	if len(trades) == 0 {
		return 0, 0
	}
	wins := 0
	grossProfit := 0.0
	grossLoss := 0.0
	closed := 0
	for _, trade := range trades {
		if !trade.Closed {
			continue
		}
		closed++
		if trade.RealizedPnL > 0 {
			wins++
			grossProfit += trade.RealizedPnL
		} else if trade.RealizedPnL < 0 {
			grossLoss += math.Abs(trade.RealizedPnL)
		}
	}
	if closed == 0 {
		return 0, 0
	}
	plRatio := 0.0
	if grossLoss > 0 {
		plRatio = grossProfit / grossLoss
	} else if grossProfit > 0 {
		plRatio = grossProfit
	}
	return float64(wins) / float64(closed), plRatio
}
