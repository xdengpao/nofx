package backtest

import (
	"testing"
	"time"
)

func TestStressTesterInjectsPriceShock(t *testing.T) {
	prices := []float64{100, 105, 110, 115, 120}
	result, err := (StressTester{Delta: 0.2, ShockIndex: 2}).Test(prices, 1000)
	if err != nil {
		t.Fatalf("压力测试失败: %v", err)
	}
	if result.ShockTerminalValue >= result.BaseTerminalValue {
		t.Fatalf("负向冲击后终值应下降: %+v", result)
	}
	if result.MaxDrawdownPct <= 0 {
		t.Fatalf("压力测试应产生回撤: %+v", result)
	}
}

func TestCompareStablecoinHedgeReducesDrawdown(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	equity := []EquityPoint{
		{Timestamp: start, Equity: 1000},
		{Timestamp: start.Add(time.Hour), Equity: 800},
		{Timestamp: start.Add(2 * time.Hour), Equity: 900},
	}
	result, err := CompareStablecoinHedge(equity, 0.3)
	if err != nil {
		t.Fatalf("稳定币对冲比较失败: %v", err)
	}
	if result.HedgedMaxDrawdownPct >= result.PureMaxDrawdownPct {
		t.Fatalf("稳定币组合应降低回撤: %+v", result)
	}
	if result.DrawdownReductionPct <= 0 {
		t.Fatalf("应给出正向回撤改善: %+v", result)
	}
}
