package decision

import "testing"

func TestCalculatePositionSizing_ATRStopAndFeeSlippage(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            10000,
		AvailableBalance:         5000,
		CurrentPrice:             100,
		StopLoss:                 98,
		Leverage:                 5,
		EffectiveRiskPct:         0.02,
		RemainingRiskBudgetPct:   0.02,
		FeeSlippagePct:           0.002,
		MinOrderValueUSDT:        10,
		RequestedPositionSizeUSD: 5000,
	})

	if !result.Executable {
		t.Fatalf("sizing 应可执行: %+v", result)
	}
	if result.RiskUSD <= 0 || result.RiskPct <= 0 {
		t.Fatalf("应计算风险金额和比例: %+v", result)
	}
	if result.PositionSizeUSD > result.MaxPositionSizeUSD {
		t.Fatalf("仓位不应超过风险上限: %+v", result)
	}
}

func TestCalculatePositionSizing_AvailableMarginLimitsSize(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            10000,
		AvailableBalance:         20,
		CurrentPrice:             100,
		StopLoss:                 99,
		Leverage:                 2,
		EffectiveRiskPct:         0.20,
		RequestedPositionSizeUSD: 1000,
		MinOrderValueUSDT:        10,
	})

	if result.PositionSizeUSD > 36.0001 {
		t.Fatalf("仓位应受可用保证金限制: %+v", result)
	}
}

func TestCalculatePositionSizing_MinNotionalNotExecutable(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            100,
		AvailableBalance:         100,
		CurrentPrice:             100,
		StopLoss:                 50,
		Leverage:                 1,
		EffectiveRiskPct:         0.01,
		RequestedPositionSizeUSD: 5,
		MinOrderValueUSDT:        10,
	})

	if result.Executable {
		t.Fatalf("低于最小名义额不应可执行: %+v", result)
	}
}

func TestCalculatePositionSizing_CannotPartialExit(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            1000,
		AvailableBalance:         1000,
		CurrentPrice:             100,
		StopLoss:                 99,
		Leverage:                 2,
		EffectiveRiskPct:         0.02,
		RequestedPositionSizeUSD: 30,
		MinOrderValueUSDT:        10,
		PartialClosePct:          20,
	})

	if result.Executable {
		t.Fatalf("无法可靠分批退出时应标记不可执行: %+v", result)
	}
	if result.CanPartialExit {
		t.Fatalf("小仓位不应支持分批退出: %+v", result)
	}
}

func TestCalculatePositionSizing_AllocationDisabledPreservesExchangeSizing(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            10000,
		AvailableBalance:         10000,
		CurrentPrice:             100,
		StopLoss:                 99,
		Leverage:                 5,
		EffectiveRiskPct:         0.02,
		RequestedPositionSizeUSD: 10000,
		MinOrderValueUSDT:        10,
	})

	if !result.Executable {
		t.Fatalf("未启用allocation时应保持交易所资金sizing行为: %+v", result)
	}
	if result.PositionSizeUSD < 9999 {
		t.Fatalf("未启用allocation时不应被小资金上限压缩: %+v", result)
	}
}

func TestCalculatePositionSizing_AllocationInsufficientReasonCode(t *testing.T) {
	result := CalculatePositionSizing(PositionSizingInput{
		AccountEquity:            100,
		AvailableBalance:         2,
		ExchangeAvailableBalance: 10000,
		AllocationEnabled:        true,
		AllocatedBalance:         100,
		AllocatedAvailable:       2,
		CurrentPrice:             100,
		StopLoss:                 99,
		Leverage:                 5,
		EffectiveRiskPct:         0.02,
		RequestedPositionSizeUSD: 10000,
		MinOrderValueUSDT:        10,
	})

	if result.Executable {
		t.Fatalf("分配资金不足时不应可执行: %+v", result)
	}
	if result.ReasonCode != "position_sizing.allocation_insufficient" {
		t.Fatalf("应输出allocation结构化拒绝码: %+v", result)
	}
}
