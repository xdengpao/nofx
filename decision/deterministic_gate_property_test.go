package decision

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"nofx/market"
)

func TestPropertyRiskIncreaseActionPassesDeterministicGates(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 50
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 7: Risk_Increase_Action 通过所有 Deterministic_Gate", prop.ForAll(
		func(longSide bool, price float64, requestedSize float64) bool {
			ctx := propertyGateContext(price)
			d := propertyGateDecision(longSide, price, requestedSize)
			if err := ValidateAndEnrichDecision(&d, ctx); err != nil {
				t.Logf("ValidateAndEnrichDecision失败: %v", err)
				return false
			}
			if err := validateOpenDecision(&d, ctx); err != nil {
				t.Logf("validateOpenDecision失败: %v d=%+v", err, d)
				return false
			}
			gate := EvaluateOpenGate(OpenGateInput{
				Decision:   &d,
				Context:    ctx,
				MarketData: ctx.MarketDataMap[d.Symbol],
			})
			if !gate.Allowed {
				t.Logf("EvaluateOpenGate拒绝: %+v", gate)
				return false
			}
			sizing := CalculatePositionSizing(PositionSizingInput{
				AccountEquity:            ctx.Account.TotalEquity,
				AvailableBalance:         ctx.Account.AvailableBalance,
				CurrentPrice:             price,
				StopLoss:                 d.StopLoss,
				Leverage:                 d.Leverage,
				EffectiveRiskPct:         gate.EffectiveRisk,
				RemainingRiskBudgetPct:   ctx.TotalRiskBudget,
				RequestedPositionSizeUSD: d.PositionSizeUSD,
				MinOrderValueUSDT:        defaultMinOrderValueUSDT,
			})
			if !sizing.Executable {
				t.Logf("CalculatePositionSizing不可执行: %+v", sizing)
				return false
			}
			filtered, rejections := enforceFinalDecisionLimits([]Decision{d}, ctx)
			if len(rejections) != 0 || len(filtered) != 1 {
				t.Logf("enforceFinalDecisionLimits拒绝: filtered=%+v rejections=%+v", filtered, rejections)
				return false
			}
			return true
		},
		gen.Bool(),
		gen.Float64Range(100, 100000),
		gen.Float64Range(100, 1000),
	))
	properties.TestingRun(t)
}

func propertyGateContext(price float64) *Context {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	return &Context{
		CurrentTime: now.Format(time.RFC3339),
		TraderID:    "property",
		Exchange:    "binance",
		Account: AccountInfo{
			TotalEquity:      10_000,
			AvailableBalance: 10_000,
			PositionCount:    0,
		},
		CandidateCoins:  []CandidateCoin{{Symbol: "BTCUSDT", IncludedInPrompt: true}},
		MarketDataMap:   map[string]*market.Data{"BTCUSDT": propertyMarketData("BTCUSDT", price)},
		CorrelationMap:  map[string]*CorrelationData{},
		BTCETHLeverage:  5,
		AltcoinLeverage: 3,
		MaxRiskPerTrade: 0.02,
		TotalRiskBudget: 0.08,
	}
}

func propertyMarketData(symbol string, price float64) *market.Data {
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   price,
		CurrentADX:     35,
		CurrentDIPlus:  30,
		CurrentDIMinus: 15,
		BollingerWidth: 2,
		MidTermSeries1h: &market.MidTermData1h{
			ADXValues: []float64{35},
			DIPlus:    []float64{30},
			DIMinus:   []float64{15},
			ATRValues: []float64{price * 0.01},
		},
		LongerTermContext: &market.LongerTermData{
			ADXValues: []float64{35},
			DIPlus:    []float64{30},
			DIMinus:   []float64{15},
			ATR14:     price * 0.01,
		},
	}
}

func propertyGateDecision(longSide bool, price, requestedSize float64) Decision {
	action := "open_long"
	stopLoss := price * 0.98
	takeProfit := price * 1.08
	if !longSide {
		action = "open_short"
		stopLoss = price * 1.02
		takeProfit = price * 0.92
	}
	return Decision{
		Symbol:          "BTCUSDT",
		Action:          action,
		Leverage:        5,
		PositionSizeUSD: requestedSize,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		Confidence:      95,
		RiskUSD:         requestedSize * 0.02,
		Reasoning:       "property test",
	}
}
