package drl

import (
	"math"
	"nofx/decision"
	"testing"
)

func testEngineConfig() DRLEngineConfig {
	return DRLEngineConfig{
		ModelPath:         "models/drl/test.onnx",
		ModelVersion:      "test-v1",
		ObservationWindow: 10,
		Timeframe:         "4h",
		ActionThreshold:   0.1,
		MaxPositionPct:    0.3,
		DefaultLeverage:   5,
		StopLossATRMult:   2,
		TakeProfitATRMult: 3,
	}
}

func TestActionMapperBranches(t *testing.T) {
	cfg := testEngineConfig()
	mapper := NewActionMapper(cfg)
	account := decision.AccountInfo{TotalEquity: 1000, AvailableBalance: 800, SizingEquity: 1000}

	wait := mapper.Map(0.05, "ethusdt", nil, account, 10, 1000, &cfg)
	if len(wait) != 1 || wait[0].Action != "wait" {
		t.Fatalf("阈值内应wait: %+v", wait)
	}

	openLong := mapper.Map(0.8, "ETHUSDT", nil, account, 10, 1000, &cfg)
	if len(openLong) != 1 || openLong[0].Action != "open_long" || math.Abs(openLong[0].PositionSizeUSD-240) > 1e-3 {
		t.Fatalf("正向信号应open_long并按比例 sizing: %+v", openLong)
	}
	if openLong[0].StopLoss != 980 || openLong[0].TakeProfit != 1030 || openLong[0].Confidence != 80 {
		t.Fatalf("open_long SL/TP/confidence异常: %+v", openLong[0])
	}

	openShort := mapper.Map(-0.5, "ETHUSDT", nil, account, 10, 1000, &cfg)
	if len(openShort) != 1 || openShort[0].Action != "open_short" || openShort[0].StopLoss != 1020 || openShort[0].TakeProfit != 970 {
		t.Fatalf("负向信号应open_short并计算反向SL/TP: %+v", openShort)
	}

	longPos := &decision.PositionInfo{Symbol: "ETHUSDT", Side: "long", EntryPrice: 1000, MarkPrice: 1010}
	hold := mapper.Map(0.7, "ETHUSDT", longPos, account, 10, 1010, &cfg)
	if len(hold) != 1 || hold[0].Action != "hold" {
		t.Fatalf("同向持仓应hold: %+v", hold)
	}

	reverse := mapper.Map(-0.7, "ETHUSDT", longPos, account, 10, 1010, &cfg)
	if len(reverse) != 2 || reverse[0].Action != "close_long" || reverse[1].Action != "open_short" {
		t.Fatalf("反向信号应先平仓再开反向: %+v", reverse)
	}
	if reverse[0].StrategyMode != StrategyMode || reverse[1].StrategyName != StrategyName {
		t.Fatalf("动作应带DRL策略元数据: %+v", reverse)
	}
}
