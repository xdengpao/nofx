package chanlunv2

import (
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"nofx/strategy/chanlun"
	"strings"
	"testing"
)

func TestLatestSignalsWithOptionsReturnsV2ReportMarkers(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{
		Timeframes: map[string]string{"trade": "1h", "sub": "15m", "micro": "3m"},
	})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	centerID := 7
	signal := Signal{
		SignalType:         "buy1",
		Direction:          "long",
		Price:              100,
		StopLoss:           95,
		TakeProfit:         115,
		Confidence:         88,
		CenterID:           &centerID,
		DivergenceStrength: 0.82,
		Timestamp:          1710000000000,
	}
	mr := &multiLevelResult{
		Symbol: "ETHUSDT",
		Results: map[string]*AnalysisResult{
			"trade": {
				Trend:   "down_trend",
				Signals: []Signal{signal},
			},
		},
	}

	engine.setLatestReport("t1", "ETHUSDT", mr, []Signal{signal}, []string{"ETHUSDT buy1 置信度88"})
	report, ok := engine.LatestSignalsWithOptions("t1", "ETHUSDT", chanlun.SignalReportOptions{})
	if !ok {
		t.Fatal("应返回最新信号报告")
	}
	if report.DecisionMode != "chanlun_v2" || report.TradeTimeframe != "1h" || report.ComponentTimeframe != "15m" {
		t.Fatalf("报告元数据不符合预期: %+v", report)
	}
	if len(report.Signals) != 1 || report.Signals[0].ActionHint != "open_long" {
		t.Fatalf("信号转换不符合预期: %+v", report.Signals)
	}
	if len(report.SignalMarkers) != 1 {
		t.Fatalf("应生成一个marker: %+v", report.SignalMarkers)
	}
	marker := report.SignalMarkers[0]
	if marker.DisplayCategory != "trade_action" || marker.TradeIntent != "open_long" || marker.Status != "ready" {
		t.Fatalf("marker无法被前端识别为交易动作: %+v", marker)
	}
}

func TestLatestSignalsWithOptionsFiltersV2Markers(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	buy := Signal{SignalType: "buy2", Direction: "long", Confidence: 80, Timestamp: 1710000000000}
	sell := Signal{SignalType: "sell1", Direction: "short", Confidence: 80, Timestamp: 1710003600000}
	engine.setLatestReport("t1", "BTCUSDT", nil, []Signal{buy, sell}, nil)

	report, ok := engine.LatestSignalsWithOptions("t1", "BTCUSDT", chanlun.SignalReportOptions{
		View:     "audit",
		Statuses: []string{"ready"},
		Limit:    1,
	})
	if !ok {
		t.Fatal("应返回最新信号报告")
	}
	if report.View != "audit" || report.Filters.Limit != 1 {
		t.Fatalf("应保留audit过滤参数: %+v", report)
	}
	if len(report.SignalMarkers) != 1 || report.MarkerSummary.TotalRaw != 2 || report.MarkerSummary.TotalReturned != 1 {
		t.Fatalf("limit过滤摘要不符合预期: markers=%+v summary=%+v", report.SignalMarkers, report.MarkerSummary)
	}
}

func TestValidateChanlunV2DecisionsEnrichesOpenSizing(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	sig := Signal{
		SignalType:         "buy2",
		Direction:          "long",
		Price:              100,
		StopLoss:           95,
		TakeProfit:         120,
		Confidence:         90,
		DivergenceStrength: 0.7,
		Timestamp:          1710000000000,
	}
	d := engine.signalToDecision(ctx, "SOLUSDT", sig, "1h")
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, nil)
	if len(rejections) != 0 {
		t.Fatalf("开仓信号不应被拒绝: %+v", rejections)
	}
	if len(valid) != 1 {
		t.Fatalf("应保留一个有效决策: %+v", valid)
	}
	got := valid[0]
	if got.PositionSizeUSD <= 0 || got.RequestedPositionSizeUSD <= 0 || got.AdjustedPositionSizeUSD <= 0 || got.RiskUSD <= 0 {
		t.Fatalf("V2开仓决策应补全仓位和风险字段: %+v", got)
	}
}

func TestValidateChanlunV2DecisionsRiskBlockedRejectsOpen(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	ctx := chanlunV2ValidationContext(1000)
	d := engine.signalToDecision(ctx, "SOLUSDT", Signal{
		SignalType: "buy2",
		Direction:  "long",
		StopLoss:   95,
		TakeProfit: 120,
		Confidence: 90,
		Timestamp:  1710000000000,
	}, "1h")
	valid, rejections := engine.validateChanlunV2Decisions(ctx, []decision.Decision{d}, &decision.CyclePreparation{
		RiskIncreaseBlocked: true,
		StopReason:          "测试阻断",
	})
	if len(valid) != 0 {
		t.Fatalf("风险阻断时不应保留开仓决策: %+v", valid)
	}
	if len(rejections) != 1 || !strings.Contains(rejections[0].Reason, "测试阻断") {
		t.Fatalf("应记录风险阻断拒绝原因: %+v", rejections)
	}
}

func chanlunV2ValidationContext(equity float64) *decision.Context {
	return &decision.Context{
		TraderID:        "t1",
		BTCETHLeverage:  5,
		AltcoinLeverage: 5,
		MaxRiskPerTrade: 0.02,
		TotalRiskBudget: 0.08,
		Account:         decision.AccountInfo{TotalEquity: equity, AvailableBalance: equity},
		FrequencyPolicy: &decision.FrequencyPolicy{DailyOpenLimit: 10},
		FrequencyState:  &decision.FrequencyState{},
		CorrelationMap:  map[string]*decision.CorrelationData{},
		MarketDataMap: map[string]*market.Data{
			"SOLUSDT": chanlunV2ValidationMarketData("SOLUSDT", 100),
			"BTCUSDT": chanlunV2ValidationMarketData("BTCUSDT", 100000),
		},
	}
}

func chanlunV2ValidationMarketData(symbol string, price float64) *market.Data {
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   price,
		CurrentEMA20:   price * 0.98,
		CurrentEMA50:   price * 0.95,
		CurrentADX:     35,
		CurrentDIPlus:  30,
		CurrentDIMinus: 10,
		LongerTermContext: &market.LongerTermData{
			EMA20:     price * 0.98,
			EMA50:     price * 0.95,
			ATR14:     price * 0.02,
			MACDHist:  []float64{1},
			ADXValues: []float64{35},
			DIPlus:    []float64{30},
			DIMinus:   []float64{10},
		},
	}
}
