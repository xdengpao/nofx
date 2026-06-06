package decision

import (
	"nofx/logger"
	"nofx/market"
	"strings"
	"testing"
	"time"
)

func TestEvaluateOpenGate_IgnoresRollingSymbolGate(t *testing.T) {
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
	if !result.Allowed {
		t.Fatalf("新策略 open gate 不应再用旧历史 symbol gate 阻止开仓: %+v", result)
	}
}

func TestEvaluateOpenGate_IgnoresRollingGlobalGate(t *testing.T) {
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
	if !result.Allowed {
		t.Fatalf("新策略 open gate 不应再用旧历史 global gate 阻止开仓: %+v", result)
	}
}

func TestBuildOpenRejection_UsesMarketGateDetails(t *testing.T) {
	ctx := newTestContext()
	ctx.MarketDataMap["ETHUSDT"] = &market.Data{
		Symbol:         "ETHUSDT",
		CurrentPrice:   100,
		CurrentADX:     30,
		CurrentDIPlus:  10,
		CurrentDIMinus: 25,
	}

	rejection := buildOpenRejection(
		Decision{Symbol: "ETHUSDT", Action: "open_long"},
		ctx,
		"ETHUSDT open_long 被风控过滤",
	)
	if rejection.GateState != "penalize" {
		t.Fatalf("应记录行情 gate state: %+v", rejection)
	}
	if len(rejection.GateReasons) == 0 || rejection.GateReasons[0] != "标的处于下行结构，多单属于逆势" {
		t.Fatalf("应记录行情 gate reason: %+v", rejection)
	}
}

func TestEvaluateOpenGate_ShortConfidenceRequirement(t *testing.T) {
	ctx := newTestContext()
	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "BTCUSDT", Action: "open_short", Confidence: 80},
		Context:    ctx,
		MarketData: newTestMarketData(100),
	})
	if !result.Allowed || result.MinConfidence != counterTrendMinConfidence {
		t.Fatalf("逆势空单应允许但要求更高置信度: %+v", result)
	}
}

func TestEvaluateOpenGate_RangeLongOverrideRelaxes(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.CurrentADX = 18
	data.BollingerWidth = 3

	withoutOverride := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "DOGEUSDT", Action: "open_long", Confidence: 65},
		Context:    ctx,
		MarketData: data,
	})
	if withoutOverride.MinConfidence <= 65 {
		t.Fatalf("无override时RANGING long应被置信度门槛拒绝: %+v", withoutOverride)
	}

	withOverride := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "DOGEUSDT", Action: "open_long", Confidence: 65},
		Context:    ctx,
		MarketData: data,
		MinConfidenceOverrides: MinConfidenceOverrides{
			LongBase:  60,
			RangeLong: 60,
		},
	})
	if withOverride.MinConfidence > 65 {
		t.Fatalf("long_base + range_long override后应放行65置信度: %+v", withOverride)
	}
	applied, ok := withOverride.Diagnostics["min_confidence_override_applied"].(map[string]any)
	if !ok || applied["rule"] != "range_long" {
		t.Fatalf("应记录range_long override诊断: %+v", withOverride.Diagnostics)
	}
}

func TestEvaluateOpenGate_RangeLongOnlyDoesNotBypassLongBase(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.CurrentADX = 18
	data.BollingerWidth = 3

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "DOGEUSDT", Action: "open_long", Confidence: 65},
		Context:    ctx,
		MarketData: data,
		MinConfidenceOverrides: MinConfidenceOverrides{
			RangeLong: 60,
		},
	})
	if result.MinConfidence != longBaseMinConfidence || result.MinConfidenceReasonCode != "gate.long_base_confidence" {
		t.Fatalf("只降range_long不应绕过long_base门槛: %+v", result)
	}
}

func TestEvaluateOpenGate_OverrideDoesNotAffectCounterTrend(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.CurrentADX = 22
	data.CurrentDIPlus = 10
	data.CurrentDIMinus = 25

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "DOGEUSDT", Action: "open_long", Confidence: 70},
		Context:    ctx,
		MarketData: data,
		MinConfidenceOverrides: MinConfidenceOverrides{
			LongBase:  60,
			RangeLong: 60,
		},
	})
	if result.MinConfidence != counterTrendMinConfidence || result.MinConfidenceReasonCode != "gate.counter_trend_confidence" {
		t.Fatalf("override不应影响逆势置信度门槛: %+v", result)
	}
}

func TestEvaluateOpenGate_OverrideAboveDefaultIsIgnored(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.CurrentADX = 18
	data.BollingerWidth = 3

	result := EvaluateOpenGate(OpenGateInput{
		Decision:   &Decision{Symbol: "DOGEUSDT", Action: "open_long", Confidence: 83},
		Context:    ctx,
		MarketData: data,
		MinConfidenceOverrides: MinConfidenceOverrides{
			RangeLong: 90,
		},
	})
	if result.MinConfidence != rangeLongMinConfidence {
		t.Fatalf("高于默认值的override应被忽略: %+v", result)
	}
}

func TestValidateStrategyDecisionsPersistsOpenGateDiagnostics(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.Symbol = "DOGEUSDT"
	data.CurrentADX = 18
	data.BollingerWidth = 3
	ctx.MarketDataMap["DOGEUSDT"] = data

	valid, rejections := ValidateStrategyDecisions(ctx, []Decision{{
		Symbol:          "DOGEUSDT",
		Action:          "open_long",
		Confidence:      65,
		StopLoss:        95,
		TakeProfit:      120,
		PositionSizeUSD: 50,
		StrategyMode:    "chanlun_v2",
		StrategyMetadata: map[string]any{
			"parent_signal_id": "parent-1",
			"entry_trigger_id": "trigger-1",
		},
	}}, StrategyValidationOptions{
		Source: "chanlun_v2",
		MinConfidenceOverrides: MinConfidenceOverrides{
			LongBase:  60,
			RangeLong: 60,
		},
	})
	if len(rejections) != 0 || len(valid) != 1 {
		t.Fatalf("override后65置信度开仓应通过: valid=%+v rejections=%+v", valid, rejections)
	}
	diagnostics, ok := valid[0].StrategyMetadata["gate_diagnostics"].(map[string]any)
	if !ok || diagnostics["min_confidence_reason_code"] != "gate.range_long_confidence" {
		t.Fatalf("accepted path应保留open gate诊断: %+v", valid[0].StrategyMetadata)
	}
	if valid[0].Confidence != 65 {
		t.Fatalf("override不应污染原始confidence: %+v", valid[0])
	}
}

func TestBuildOpenRejectionIncludesMinConfidenceReasonCode(t *testing.T) {
	ctx := newTestContext()
	data := newTestMarketData(100)
	data.Symbol = "DOGEUSDT"
	data.CurrentADX = 18
	data.BollingerWidth = 3
	ctx.MarketDataMap["DOGEUSDT"] = data

	rejection := buildOpenRejection(Decision{
		Symbol:     "DOGEUSDT",
		Action:     "open_long",
		Confidence: 55,
	}, ctx, "DOGEUSDT open_long 被风控过滤", openValidationOptions{
		MinConfidenceOverrides: MinConfidenceOverrides{
			LongBase:  60,
			RangeLong: 60,
		},
	})
	if rejection.GateDiagnostics["min_confidence_reason_code"] != "gate.range_long_confidence" ||
		rejection.GateDiagnostics["min_confidence_rule"] != "range_long" {
		t.Fatalf("rejection path应输出结构化置信度拒因: %+v", rejection.GateDiagnostics)
	}
}

func TestEvaluateOpenGate_ADXRegimeBlocksLowADX(t *testing.T) {
	ctx := newTestContext()
	policy := &StrategyRiskPolicy{
		Enabled:      true,
		ADXTimeframe: "1h",
		Profiles: []InstrumentProfile{{
			Name:       "btc_eth",
			Symbols:    []string{"ETHUSDT"},
			MinADX:     20,
			AllowLong:  true,
			AllowShort: true,
		}},
	}
	data := newTestMarketData(100)
	data.MidTermSeries1h = &market.MidTermData1h{
		ADXValues: []float64{18},
		DIPlus:    []float64{25},
		DIMinus:   []float64{10},
		ATRValues: []float64{2},
	}
	result := EvaluateOpenGate(OpenGateInput{
		Decision:        &Decision{Symbol: "ETHUSDT", Action: "open_long", Confidence: 95},
		Context:         ctx,
		MarketData:      data,
		StrategyPolicy:  policy,
		StrategyProfile: ResolveInstrumentProfile("ETHUSDT", policy),
	})
	if result.Allowed {
		t.Fatalf("1h ADX低于阈值应阻止开仓: %+v", result)
	}
	if result.Diagnostics["adx_regime"] == nil {
		t.Fatalf("应输出ADX诊断: %+v", result)
	}
}

func TestEvaluateOpenGate_ADXRegimeBlocksWrongDI(t *testing.T) {
	ctx := newTestContext()
	policy := &StrategyRiskPolicy{
		Enabled:      true,
		ADXTimeframe: "1h",
		Profiles: []InstrumentProfile{{
			Name:       "btc_eth",
			Symbols:    []string{"ETHUSDT"},
			MinADX:     20,
			AllowLong:  true,
			AllowShort: true,
		}},
	}
	data := newTestMarketData(100)
	data.MidTermSeries1h = &market.MidTermData1h{
		ADXValues: []float64{30},
		DIPlus:    []float64{12},
		DIMinus:   []float64{28},
		ATRValues: []float64{2},
	}
	result := EvaluateOpenGate(OpenGateInput{
		Decision:        &Decision{Symbol: "ETHUSDT", Action: "open_long", Confidence: 95},
		Context:         ctx,
		MarketData:      data,
		StrategyPolicy:  policy,
		StrategyProfile: ResolveInstrumentProfile("ETHUSDT", policy),
	})
	if result.Allowed {
		t.Fatalf("ADX趋势中DI方向错误应阻止开仓: %+v", result)
	}
}

func TestEvaluateOpenGate_ADXReportOnlyPenalizes(t *testing.T) {
	ctx := newTestContext()
	policy := &StrategyRiskPolicy{
		Enabled:                  true,
		RollbackLegacyValidation: true,
		ADXTimeframe:             "1h",
		Profiles: []InstrumentProfile{{
			Name:       "btc_eth",
			Symbols:    []string{"ETHUSDT"},
			MinADX:     20,
			AllowLong:  true,
			AllowShort: true,
		}},
	}
	data := newTestMarketData(100)
	data.MidTermSeries1h = &market.MidTermData1h{
		ADXValues: []float64{18},
		DIPlus:    []float64{25},
		DIMinus:   []float64{10},
		ATRValues: []float64{2},
	}
	result := EvaluateOpenGate(OpenGateInput{
		Decision:        &Decision{Symbol: "ETHUSDT", Action: "open_long", Confidence: 95},
		Context:         ctx,
		MarketData:      data,
		StrategyPolicy:  policy,
		StrategyProfile: ResolveInstrumentProfile("ETHUSDT", policy),
	})
	if !result.Allowed || result.State != "penalize" {
		t.Fatalf("rollback/report-only ADX gate应只降权不阻止: %+v", result)
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
	if !result.Allowed || result.State != "allow" || result.MinConfidence != longBaseMinConfidence {
		t.Fatalf("3.5%% 布林带宽度只应保留多单基础门槛: %+v", result)
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

func TestEvaluateOpenGate_AIFailurePenaltyIsModeAware(t *testing.T) {
	cases := []struct {
		name          string
		mode          string
		wantPenalty   bool
		wantRiskBelow bool
	}{
		{name: "legacy", wantPenalty: true, wantRiskBelow: true},
		{name: "ai", mode: "ai", wantPenalty: true, wantRiskBelow: true},
		{name: "programmatic", mode: "programmatic"},
		{name: "chanlun v2", mode: "chanlun_v2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext()
			ctx.DecisionMode = tc.mode
			result := EvaluateOpenGate(OpenGateInput{
				Decision:         &Decision{Symbol: "ETHUSDT", Action: "open_long"},
				Context:          ctx,
				ExecutionQuality: &logger.ExecutionQualityStats{AIFailureCount: 3},
			})

			hasPenalty := openGateHasReason(result, "AI调用失败次数偏高")
			if hasPenalty != tc.wantPenalty {
				t.Fatalf("AI failure penalty mismatch: got=%v want=%v result=%+v", hasPenalty, tc.wantPenalty, result)
			}
			if tc.wantRiskBelow && result.EffectiveRisk >= ctx.MaxRiskPerTrade {
				t.Fatalf("AI failure penalty 应降低 effective risk: %+v", result)
			}
			if !tc.wantRiskBelow && result.EffectiveRisk != ctx.MaxRiskPerTrade {
				t.Fatalf("非AI模式不应因 AI failure 降低 risk: %+v", result)
			}
		})
	}
}

func TestEvaluateOpenGate_AIBackoffIsModeAware(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		wantBlock bool
	}{
		{name: "legacy", wantBlock: true},
		{name: "ai", mode: "ai", wantBlock: true},
		{name: "programmatic", mode: "programmatic"},
		{name: "chanlun v2", mode: "chanlun_v2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext()
			ctx.DecisionMode = tc.mode
			ctx.AIBackoffUntil = time.Now().Add(time.Hour)
			result := EvaluateOpenGate(OpenGateInput{
				Decision: &Decision{Symbol: "ETHUSDT", Action: "open_long"},
				Context:  ctx,
			})

			if result.Allowed == tc.wantBlock {
				t.Fatalf("AIBackoff gate mismatch: wantBlock=%v result=%+v", tc.wantBlock, result)
			}
			hasReason := openGateHasReason(result, "AI调用退避中")
			if hasReason != tc.wantBlock {
				t.Fatalf("AIBackoff reason mismatch: got=%v want=%v result=%+v", hasReason, tc.wantBlock, result)
			}
		})
	}
}

func TestEvaluateOpenGate_ExecutionQualityBlocksAllModes(t *testing.T) {
	for _, mode := range []string{"ai", "programmatic", "chanlun_v2"} {
		t.Run(mode, func(t *testing.T) {
			ctx := newTestContext()
			ctx.DecisionMode = mode
			result := EvaluateOpenGate(OpenGateInput{
				Decision:         &Decision{Symbol: "ETHUSDT", Action: "open_long"},
				Context:          ctx,
				ExecutionQuality: &logger.ExecutionQualityStats{HighRiskExecutionFailures: 1},
			})
			if result.Allowed {
				t.Fatalf("%s 模式下高危执行失败仍应阻断: %+v", mode, result)
			}
		})
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

func TestEvaluateOpenGate_LossModeBlocksSecondSameSideHighCorrShort(t *testing.T) {
	ctx := newTestContext()
	ctx.LossMode = &LossModeState{Active: true, MaxRiskPerTrade: 0.005}
	ctx.Positions = []PositionInfo{
		{Symbol: "ETHUSDT", Side: "short", UnrealizedPnLPct: 0.5},
	}
	ctx.CorrelationMap = map[string]*CorrelationData{
		"ETHUSDT": {IsHighCorr: true},
		"BCHUSDT": {IsHighCorr: true},
	}
	profile := InstrumentProfile{Name: "major_alt", MaxSameSideHighCorr: 2, MaxSameSideLossPct: 0.02}

	result := EvaluateOpenGate(OpenGateInput{
		Decision:        &Decision{Symbol: "BCHUSDT", Action: "open_short", Confidence: 95},
		Context:         ctx,
		MarketData:      newTestMarketData(100),
		StrategyProfile: profile,
	})
	if result.Allowed {
		t.Fatalf("亏损模式下第二个同向高相关空单应被阻止: %+v", result)
	}
	if result.Diagnostics["correlation_concentration"] == nil {
		t.Fatalf("应输出相关性诊断: %+v", result)
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

func openGateHasReason(result OpenGateResult, needle string) bool {
	for _, reason := range result.Reasons {
		if strings.Contains(reason, needle) {
			return true
		}
	}
	return false
}
