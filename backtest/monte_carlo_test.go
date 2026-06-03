package backtest

import "testing"

func TestMonteCarloSimulatorSimulate(t *testing.T) {
	prices := []float64{100, 101, 102, 101, 103, 104}
	result, err := (MonteCarloSimulator{Paths: 128, HorizonSteps: 12, Seed: 7}).Simulate(prices, 1000)
	if err != nil {
		t.Fatalf("蒙特卡洛模拟失败: %v", err)
	}
	if result.Paths != 128 || len(result.TerminalValues) != 128 {
		t.Fatalf("路径数量异常: %+v", result)
	}
	if result.VaR95 < 0 || result.CVaR95 < 0 || result.LossProbability < 0 || result.LossProbability > 1 {
		t.Fatalf("风险指标范围异常: %+v", result)
	}
}

func TestMonteCarloSimulatorUsesStrategyEvaluator(t *testing.T) {
	prices := []float64{100, 101, 102}
	result, err := (MonteCarloSimulator{
		Paths:        4,
		HorizonSteps: 2,
		Seed:         1,
		StrategyEvaluator: func(path []float64, initialValue float64) float64 {
			return initialValue + 10
		},
	}).Simulate(prices, 1000)
	if err != nil {
		t.Fatalf("蒙特卡洛模拟失败: %v", err)
	}
	if result.MeanTerminal != 1010 || result.LossProbability != 0 {
		t.Fatalf("应使用注入的策略evaluator: %+v", result)
	}
}
