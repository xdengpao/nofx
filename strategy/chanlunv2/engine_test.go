package chanlunv2

import (
	"nofx/config"
	"nofx/strategy/chanlun"
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
