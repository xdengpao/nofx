package backtest

import (
	"testing"
	"time"
)

func TestCalculateDRLBacktestMetrics(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	equity := []EquityPoint{
		{Timestamp: start, Equity: 1000},
		{Timestamp: start.Add(time.Hour), Equity: 1010},
		{Timestamp: start.Add(2 * time.Hour), Equity: 1005},
		{Timestamp: start.Add(3 * time.Hour), Equity: 1020},
	}
	trades := []TradeLifecycle{
		{Closed: true, RealizedPnL: 12},
		{Closed: true, RealizedPnL: -4},
	}
	metrics := CalculateDRLBacktestMetrics(equity, trades, start, start.Add(24*time.Hour))
	if metrics.MaxDrawdownPct <= 0 {
		t.Fatalf("应计算最大回撤: %+v", metrics)
	}
	if metrics.WinRate != 0.5 || metrics.DirectionAccuracy != 0.5 {
		t.Fatalf("胜率/方向准确性异常: %+v", metrics)
	}
	if metrics.ProfitLossRatio != 3 {
		t.Fatalf("盈亏比异常: %+v", metrics)
	}
}
