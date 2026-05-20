package backtest

import (
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"nofx/decision"
)

func TestPropertyNoDualSidePositionForSameSymbol(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 50
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 4: 同 symbol 无双向持仓", prop.ForAll(
		func(openLongFirst bool, price float64) bool {
			now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
			broker := NewPaperBrokerWithExchange(10_000, CostConfig{}, ExecutionConfig{}, "binance")
			firstAction := "open_long"
			secondAction := "open_short"
			if !openLongFirst {
				firstAction = "open_short"
				secondAction = "open_long"
			}
			first := propertyOpenDecision(firstAction, price)
			if err := broker.executeDecision(first, price, now, "property"); err != nil {
				t.Logf("初始开仓失败: %v", err)
				return false
			}
			second := propertyOpenDecision(secondAction, price)
			err := broker.executeDecision(second, price, now.Add(time.Minute), "property")
			if err == nil || !strings.Contains(err.Error(), "不允许双向持仓") {
				t.Logf("反向开仓应被拒绝，实际err=%v", err)
				return false
			}
			pos := broker.Positions["BTCUSDT"]
			if pos == nil || pos.Quantity <= 0 {
				return false
			}
			if openLongFirst {
				return pos.Side == "long"
			}
			return pos.Side == "short"
		},
		gen.Bool(),
		gen.Float64Range(100, 100000),
	))
	properties.TestingRun(t)
}

func propertyOpenDecision(action string, price float64) decision.Decision {
	d := decision.Decision{
		Action:          action,
		Symbol:          "BTCUSDT",
		PositionSizeUSD: 1_000,
		Leverage:        5,
		Confidence:      95,
		Reasoning:       "property test",
	}
	if action == "open_short" {
		d.StopLoss = price * 1.02
		d.TakeProfit = price * 0.94
		return d
	}
	d.StopLoss = price * 0.98
	d.TakeProfit = price * 1.06
	return d
}
