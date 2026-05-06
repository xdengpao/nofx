package decision

import (
	"nofx/logger"
	"nofx/market"
	"testing"
	"time"
)

func TestEvaluateOpenGate_RollingBlock(t *testing.T) {
	ctx := newTestContext()
	ctx.PerformanceGates = &logger.RollingPerformanceSnapshot{
		SymbolGates: map[string]logger.PerformanceGate{
			"BCHUSDT": {
				Key:           "BCHUSDT",
				Scope:         "symbol",
				State:         "block",
				CooldownUntil: time.Now().Add(time.Hour),
				Reason:        "测试禁交易",
			},
		},
		SideGates: map[string]logger.PerformanceGate{},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BCHUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("rolling block 应拒绝开仓: %+v", result)
	}
}

func TestEvaluateOpenGate_ShortConfidenceRequirement(t *testing.T) {
	ctx := newTestContext()
	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BTCUSDT", Action: "open_short", Confidence: 80},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed || result.MinConfidence < 90 {
		t.Fatalf("short侧应允许但要求更高置信度: %+v", result)
	}
}

func TestEvaluateOpenGate_CorrelationConcentrationBlocks(t *testing.T) {
	ctx := newTestContext()
	ctx.Positions = []PositionInfo{
		{Symbol: "ETHUSDT", Side: "long"},
		{Symbol: "SOLUSDT", Side: "long"},
	}
	ctx.CorrelationMap = map[string]*CorrelationData{
		"ETHUSDT": {IsHighCorr: true},
		"SOLUSDT": {IsHighCorr: true},
		"BNBUSDT": {IsHighCorr: true},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BNBUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("同向高相关集中应拒绝: %+v", result)
	}
}

func TestEvaluateOpenGate_BTCMarketCrashBlocks(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{Symbol: "BTCUSDT", PriceChange1h: -6}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "ETHUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("BTC闪崩应拒绝新开仓: %+v", result)
	}
}

func TestEvaluateOpenGate_ExecutionQualityBlocks(t *testing.T) {
	ctx := newTestContext()
	quality := &logger.ExecutionQualityStats{ProtectionOrderFailures: 1}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:         &Decision{Symbol: "ETHUSDT", Action: "open_long"},
		Context:          ctx,
		MarketData:       newTestMarketData(100),
		ExecutionQuality: quality,
	})
	if result.Allowed {
		t.Fatalf("保护单失败后应暂停新开仓: %+v", result)
	}
}

func TestValidateOpenDecision_ShortConfidenceTooLow(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = newTestMarketData(100)
	d := &Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_short",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        106,
		TakeProfit:      80,
		Confidence:      80,
	}

	if err := validateOpenDecision(d, ctx); err == nil {
		t.Fatal("低置信度 short 应被 open gate 拒绝")
	}
}
