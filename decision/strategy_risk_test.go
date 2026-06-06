package decision

import (
	"math"
	"testing"

	"nofx/market"
)

func testStrategyPolicy() *StrategyRiskPolicy {
	return &StrategyRiskPolicy{
		Enabled:         true,
		FeeSlippagePct:  0.002,
		DefaultMinNetRR: 2.5,
		ADXTimeframe:    "1h",
		Profiles: []InstrumentProfile{
			{
				Name:                "btc_eth",
				Symbols:             []string{"ETHUSDT"},
				MinStopPct:          0.01,
				FallbackStopPct:     0.015,
				ATRMultiplier:       1.5,
				ATRTimeframe:        "1h",
				MinNetRR:            2.5,
				MaxRiskPct:          0.005,
				MinADX:              20,
				AllowLong:           true,
				AllowShort:          true,
				MinOrderValueUSDT:   10,
				ExchangeFullTPMode:  ExchangeFullTPModeAlgorithmicFull,
				ExchangeFullTPMinRR: 2.5,
			},
		},
	}
}

func TestNormalizeOpenDecisionRisk_RewritesMicroStopAndFullTP(t *testing.T) {
	ctx := &Context{StrategyRiskPolicy: testStrategyPolicy()}
	md := &market.Data{
		Symbol:       "ETHUSDT",
		CurrentPrice: 100,
		MidTermSeries1h: &market.MidTermData1h{
			ATRValues: []float64{1.0},
		},
	}
	d := &Decision{
		Symbol:          "ETHUSDT",
		Action:          "open_long",
		StopLoss:        99.97,
		TakeProfit:      100.07,
		PositionSizeUSD: 100,
		Leverage:        3,
		Confidence:      90,
	}

	norm, err := NormalizeOpenDecisionRisk(d, ctx, md)
	if err != nil {
		t.Fatalf("风险规范化失败: %v", err)
	}
	if norm == nil {
		t.Fatal("启用策略风险时应返回规范化结果")
	}
	if !norm.RewrittenStop || math.Abs(d.StopLoss-98.5) > 1e-9 {
		t.Fatalf("止损应按1.5*ATR重写到98.5: d=%+v norm=%+v", d, norm)
	}
	expectedTP := 100 * (1 + 0.015*2.5 + 0.002)
	if !norm.RewrittenTakeProfit || math.Abs(d.ExchangeFullTakeProfit-expectedTP) > 1e-9 {
		t.Fatalf("整仓TP应按净RR算法重写: got %.8f want %.8f norm=%+v", d.ExchangeFullTakeProfit, expectedTP, norm)
	}
	if d.TakeProfit != d.ExchangeFullTakeProfit {
		t.Fatalf("执行层使用的TakeProfit应为algorithmic full TP: %+v", d)
	}
	if d.StopDistanceRatio != 0.015 || d.StopDistancePercent != 1.5 {
		t.Fatalf("止损单位字段错误: %+v", d)
	}
}

func TestNormalizeOpenDecisionRisk_LegacyPolicyNoop(t *testing.T) {
	ctx := &Context{StrategyRiskPolicy: &StrategyRiskPolicy{Legacy: true, RollbackLegacyValidation: true}}
	d := &Decision{Symbol: "ETHUSDT", Action: "open_long", StopLoss: 99, TakeProfit: 105}
	norm, err := NormalizeOpenDecisionRisk(d, ctx, &market.Data{CurrentPrice: 100})
	if err != nil {
		t.Fatalf("legacy策略不应报错: %v", err)
	}
	if norm != nil {
		t.Fatalf("legacy策略不应产生规范化结果: %+v", norm)
	}
	if d.StopLoss != 99 || d.TakeProfit != 105 {
		t.Fatalf("legacy策略不应改写决策: %+v", d)
	}
}
