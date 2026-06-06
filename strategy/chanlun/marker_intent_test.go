package chanlun

import (
	"nofx/decision"
	"path/filepath"
	"testing"
)

func TestDeriveTradeIntentCoversProgrammaticActions(t *testing.T) {
	tests := []struct {
		name         string
		action       string
		finalAction  string
		positionSide string
		direction    string
		want         string
	}{
		{name: "open long", action: "open_long", want: "open_long"},
		{name: "open short", action: "open_short", want: "open_short"},
		{name: "add long", action: "add_long", want: "add_long"},
		{name: "add short", action: "add_short", want: "add_short"},
		{name: "close long", action: "close_long", want: "close_long"},
		{name: "close short", action: "close_short", want: "close_short"},
		{name: "reduce long", action: "partial_close", positionSide: "long", want: "reduce_long"},
		{name: "reduce short", action: "partial_close", positionSide: "short", want: "reduce_short"},
		{name: "reduce long from direction fallback", action: "partial_close", direction: "long", want: "reduce_long"},
		{name: "partial skipped", action: "partial_close", finalAction: "partial_close_skipped", positionSide: "long", want: "reduce_skipped"},
		{name: "partial upgraded close long", action: "partial_close", finalAction: "close_long", positionSide: "long", want: "close_long"},
		{name: "partial upgraded close short", action: "partial_close", finalAction: "close_short", positionSide: "short", want: "close_short"},
		{name: "detected only", action: "", direction: "long", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveTradeIntent(tt.action, tt.finalAction, tt.positionSide, tt.direction)
			if got != tt.want {
				t.Fatalf("deriveTradeIntent()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecutionResultMarkerKeepsOriginalAndFinalAction(t *testing.T) {
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	decision := decision.Decision{
		Symbol:          "ETHUSDT",
		Action:          "partial_close",
		StrategyMode:    "programmatic",
		Reasoning:       "结构破坏升级全平",
		SignalID:        "sig-close-upgrade",
		SignalType:      SignalSell2,
		SignalTimeframe: "15m",
		StrategyMetadata: map[string]any{
			"layer":              "position_management",
			"rule":               "structure_break",
			"side":               "long",
			"trigger_close_time": int64(123456),
		},
	}

	engine.OnExecutionResult(ProgrammaticExecutionResult{
		TraderID:    "t1",
		Decision:    decision,
		Success:     true,
		FinalAction: "close_long",
	})

	markers := engine.StateStore.RecentSignalMarkers("t1", "ETHUSDT", 10)
	if len(markers) != 1 {
		t.Fatalf("应写入1个marker: %+v", markers)
	}
	got := markers[0]
	if got.Action != "partial_close" || got.FinalAction != "close_long" || got.TradeIntent != "close_long" || got.PositionSide != "long" {
		t.Fatalf("marker应保留原始动作并按最终动作推导交易意图: %+v", got)
	}
}

func TestExecutionResultMarksExecutedOnlyAfterSuccessfulOpenLike(t *testing.T) {
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	openDecision := decision.Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_short",
		StrategyMode:    "programmatic",
		Reasoning:       "趋势同向开空",
		SignalID:        "sig-open-short",
		SignalType:      SignalSell2,
		SignalTimeframe: "1h",
		StrategyMetadata: map[string]any{
			"layer":             "main_signal",
			"rule":              SignalSell2,
			"signal_type":       SignalSell2,
			"trade_intent":      "open_short",
			"signal_close_time": int64(100),
		},
	}

	engine.OnExecutionResult(ProgrammaticExecutionResult{
		TraderID: "t1",
		Decision: openDecision,
		Success:  false,
		Error:    "交易所拒单",
	})
	if engine.StateStore.HasExecutedSignal("t1", "BTCUSDT", "sig-open-short") {
		t.Fatalf("失败执行不应写入executed_signals")
	}

	engine.OnExecutionResult(ProgrammaticExecutionResult{
		TraderID:    "t1",
		Decision:    openDecision,
		Success:     true,
		FinalAction: "open_short",
	})
	if !engine.StateStore.HasExecutedSignal("t1", "BTCUSDT", "sig-open-short") {
		t.Fatalf("成功open应写入executed_signals")
	}
}

func TestRejectedStrategyDecisionMarkerRetainsActionAndTradeIntent(t *testing.T) {
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	candidate := decision.Decision{
		Symbol:          "ETHUSDT",
		Action:          "open_short",
		Reasoning:       "一类卖点开空",
		SignalID:        "sig-open-short",
		SignalType:      SignalSell1,
		SignalTimeframe: "1h",
		StrategyMetadata: map[string]any{
			"layer":               "main_signal",
			"rule":                SignalSell1,
			"signal_type":         SignalSell1,
			"timeframe":           "1h",
			"signal_close_time":   int64(654321),
			"decision_close_time": int64(777777),
			"trigger_close_time":  int64(654321),
			"trade_intent":        "open_short",
		},
	}
	engine.markRejectedStrategyDecisions(&decision.Context{TraderID: "t1"}, []decision.Decision{candidate}, nil, []decision.OpenRejection{{
		Symbol:   "ETHUSDT",
		Action:   "open_short",
		Reason:   "通用原因",
		SignalID: "other-sig",
	}, {
		Symbol:   "ETHUSDT",
		Action:   "open_short",
		Reason:   "ADX不足",
		SignalID: "sig-open-short",
	}})

	markers := engine.StateStore.RecentSignalMarkers("t1", "ETHUSDT", 10)
	if len(markers) != 1 {
		t.Fatalf("应写入1个rejected marker: %+v", markers)
	}
	got := markers[0]
	if got.Status != "rejected" || got.Action != "open_short" || got.TradeIntent != "open_short" || got.Reason != "ADX不足" {
		t.Fatalf("rejected marker应保留动作和交易意图: %+v", got)
	}
	if got.SignalCloseTime != 654321 || got.DecisionCloseTime != 777777 || got.DisplayCloseTime != 777777 || got.CloseTime != 654321 {
		t.Fatalf("rejected marker应保留结构时间和决策时间: %+v", got)
	}
}

func TestMainSignalDecisionCarriesSignalAndDecisionCloseTime(t *testing.T) {
	engine, err := NewEngine(testProgrammaticPolicy(filepath.Join(t.TempDir(), "state.json")))
	if err != nil {
		t.Fatalf("创建engine失败: %v", err)
	}
	signal := ChanlunSignal{
		SignalID:          "sig-sell2",
		Symbol:            "BCHUSDT",
		Direction:         SideShort,
		SignalType:        SignalSell2,
		AnalysisTF:        "1h",
		TriggerTF:         "15m",
		Level:             "1h",
		Price:             100,
		StopLoss:          105,
		TakeProfit:        90,
		StructureTarget:   90,
		Confidence:        75,
		SignalCloseTime:   18_599_900,
		TriggerCloseTime:  18_599_900,
		SegmentStartTime:  17_599_900,
		SegmentEndTime:    18_599_900,
		DecisionCloseTime: 21_599_900,
	}
	d := engine.signalToMainDecision(&decision.Context{AltcoinLeverage: 5}, signal)
	if d.Action != "open_short" {
		t.Fatalf("应生成开空决策: %+v", d)
	}
	if got, _ := metadataInt64(d.StrategyMetadata, "signal_close_time"); got != signal.SignalCloseTime {
		t.Fatalf("决策应写入signal_close_time: %+v", d.StrategyMetadata)
	}
	if got, _ := metadataInt64(d.StrategyMetadata, "decision_close_time"); got != signal.DecisionCloseTime {
		t.Fatalf("决策应写入decision_close_time: %+v", d.StrategyMetadata)
	}
	marker, ok := engine.decisionToMarker(d, "rejected")
	if !ok {
		t.Fatalf("应生成marker")
	}
	if marker.CloseTime != signal.SignalCloseTime || marker.SignalCloseTime != signal.SignalCloseTime ||
		marker.DecisionCloseTime != signal.DecisionCloseTime || marker.DisplayCloseTime != signal.DecisionCloseTime {
		t.Fatalf("marker时间锚点错误: %+v", marker)
	}
}
