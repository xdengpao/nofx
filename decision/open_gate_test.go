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

func TestEvaluateOpenGate_GlobalRollingBlock(t *testing.T) {
	ctx := newTestContext()
	ctx.PerformanceGates = &logger.RollingPerformanceSnapshot{
		GlobalGate: logger.PerformanceGate{
			Key:           "ALL",
			Scope:         "global",
			State:         "block",
			CooldownUntil: time.Now().Add(time.Hour),
			Reason:        "全局负期望暂停",
		},
		SymbolGates: map[string]logger.PerformanceGate{},
		SideGates:   map[string]logger.PerformanceGate{},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "ETHUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("global rolling block 应拒绝开仓: %+v", result)
	}
}

func TestBuildOpenRejection_IncludesGateDetails(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["ETHUSDT"] = newTestMarketData(100)
	ctx.PerformanceGates = &logger.RollingPerformanceSnapshot{
		GlobalGate: logger.PerformanceGate{
			Key:           "ALL",
			Scope:         "global",
			State:         "block",
			CooldownUntil: time.Now().Add(time.Hour),
			Reason:        "全局负期望暂停",
		},
		SymbolGates: map[string]logger.PerformanceGate{},
		SideGates:   map[string]logger.PerformanceGate{},
	}

	rejection := buildOpenRejection(
		Decision{Symbol: "ETHUSDT", Action: "open_long"},
		ctx,
		"ETHUSDT open_long 被风控过滤",
	)
	if rejection.GateState != "block" {
		t.Fatalf("应记录 gate state: %+v", rejection)
	}
	if len(rejection.GateReasons) == 0 || rejection.GateReasons[0] != "全局负期望暂停" {
		t.Fatalf("应记录 gate reason: %+v", rejection)
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

func TestEvaluateOpenGate_BTCNormalBollingerWidthDoesNotPenalize(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		PriceChange1h:  0.1,
		PriceChange4h:  0.2,
		BollingerWidth: 3.5,
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BTCUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed || result.State != "allow" || result.MinConfidence != 0 {
		t.Fatalf("3.5%% 布林带宽度不应触发 BTC 高波动降权: %+v", result)
	}
}

func TestEvaluateOpenGate_BTCHighBollingerWidthPenalizes(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		PriceChange1h:  0.1,
		PriceChange4h:  0.2,
		BollingerWidth: 13.0,
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BTCUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed || result.State != "penalize" || result.MinConfidence < 85 {
		t.Fatalf("13%% 布林带宽度应触发 BTC 高波动降权: %+v", result)
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

func TestEvaluateOpenGate_BTCMultiTimeframeBearishBlocksAltLong(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		CurrentPrice:   95000,
		CurrentDIPlus:  12,
		CurrentDIMinus: 28,
		LongerTermContext: &market.LongerTermData{
			EMA20: 97000,
			EMA50: 98000,
		},
		MidTermSeries1h: &market.MidTermData1h{
			EMA20Values: []float64{96000},
			EMA50Values: []float64{98000},
			MACDHist:    []float64{-10},
		},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "SOLUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("BTC 1h/4h 空头结构应阻断高 beta 多单: %+v", result)
	}
	if result.Diagnostics["btc"] == nil {
		t.Fatalf("BTC阻断应记录诊断信息: %+v", result)
	}
}

func TestEvaluateOpenGate_BTCMildBearishPenalizesAltLong(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		CurrentPrice:   100000,
		PriceChange1h:  -0.2,
		CurrentDIPlus:  28,
		CurrentDIMinus: 16,
		LongerTermContext: &market.LongerTermData{
			EMA20: 99000,
			EMA50: 97000,
		},
		MidTermSeries1h: &market.MidTermData1h{
			EMA20Values: []float64{100200},
			EMA50Values: []float64{99000},
			MACDHist:    []float64{-1},
		},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "SOLUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed {
		t.Fatalf("BTC轻微转弱应降权而不是阻断: %+v", result)
	}
	if result.State != "penalize" || result.MinConfidence < btcConflictMinConfidence {
		t.Fatalf("BTC轻微转弱应提高门槛: %+v", result)
	}
	if result.Diagnostics["btc"] == nil {
		t.Fatalf("BTC降权应记录诊断信息: %+v", result)
	}
}

func TestEvaluateOpenGate_BTCConflictPenalizesAltLong(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		CurrentPrice:   100000,
		CurrentDIPlus:  30,
		CurrentDIMinus: 15,
		LongerTermContext: &market.LongerTermData{
			EMA20: 99000,
			EMA50: 97000,
		},
		MidTermSeries15m: &market.MidTermData15m{
			EMA20Values: []float64{99500},
			EMA50Values: []float64{100100},
			MACDHist:    []float64{-1},
		},
		MidTermSeries1h: &market.MidTermData1h{
			EMA20Values: []float64{100200},
			EMA50Values: []float64{99000},
			MACDHist:    []float64{1},
		},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "SOLUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed || result.MinConfidence < btcConflictMinConfidence || result.EffectiveRisk >= ctx.MaxRiskPerTrade {
		t.Fatalf("BTC 多周期冲突应降权并提高置信度: %+v", result)
	}
}

func TestBuildOpenRejection_IncludesBTCDiagnostics(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["SOLUSDT"] = newTestMarketData(100)
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		Symbol:         "BTCUSDT",
		CurrentPrice:   95000,
		CurrentDIPlus:  12,
		CurrentDIMinus: 28,
		LongerTermContext: &market.LongerTermData{
			EMA20: 97000,
			EMA50: 98000,
		},
		MidTermSeries1h: &market.MidTermData1h{
			EMA20Values: []float64{96000},
			EMA50Values: []float64{98000},
			MACDHist:    []float64{-10},
		},
	}

	rejection := buildOpenRejection(
		Decision{Symbol: "SOLUSDT", Action: "open_long"},
		ctx,
		"SOLUSDT open_long 被风控过滤",
	)
	if rejection.GateState != "block" {
		t.Fatalf("应记录 block gate state: %+v", rejection)
	}
	if rejection.GateDiagnostics["btc"] == nil {
		t.Fatalf("应记录 BTC gate diagnostics: %+v", rejection)
	}
}

func TestEvaluateOpenGate_SameSideExposureBlocksThirdHighBetaLong(t *testing.T) {
	ctx := newTestContext()
	ctx.Positions = []PositionInfo{
		{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: 1},
		{Symbol: "SOLUSDT", Side: "BUY", UnrealizedPnLPct: 1},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BNBUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("已有2个同向多单时应拒绝新增高 beta 多单: %+v", result)
	}
}

func TestEvaluateOpenGate_LosingSameSidePositionBlocksAdd(t *testing.T) {
	ctx := newTestContext()
	ctx.Positions = []PositionInfo{
		{Symbol: "SOLUSDT", Side: "long", UnrealizedPnLPct: -4.2},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "ETHUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if result.Allowed {
		t.Fatalf("已有同向浮亏持仓时应拒绝继续加同向仓: %+v", result)
	}
}

func TestEvaluateOpenGate_ExtremeADXChaseBlocks(t *testing.T) {
	ctx := newTestContext()
	md := newTestMarketData(100)
	md.CurrentADX = 65
	md.PriceChange1h = 2.0
	md.CurrentEMA20 = 94
	md.MidTermSeries15m = &market.MidTermData15m{
		RSI14Values: []float64{68},
		MACDHist:    []float64{2, 1},
	}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "SOLUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: md,
	})
	if result.Allowed {
		t.Fatalf("极高ADX且无回踩确认时应拒绝追高: %+v", result)
	}
}

func TestEvaluateOpenGate_ElevatedADXPenalizes(t *testing.T) {
	ctx := newTestContext()
	md := newTestMarketData(100)
	md.CurrentADX = 55

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "SOLUSDT", Action: "open_long"},
		Context:    ctx,
		MarketData: md,
	})
	if !result.Allowed || result.MinConfidence < highADXMinConfidence || result.EffectiveRisk >= ctx.MaxRiskPerTrade {
		t.Fatalf("高ADX但未追高阻断时应降权并提高置信度: %+v", result)
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
