package trader

import (
	"nofx/decision"
	"nofx/logger"
	"testing"
	"time"
)

func TestAppendOpenRejectionsToRecordPreservesFreshnessTiming(t *testing.T) {
	actionTimestamp := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC).UnixMilli()
	rejection := decision.OpenRejection{
		Symbol:              "BNBUSDT",
		Action:              "open_long",
		Reason:              "旧信号过期",
		GateState:           "blocked",
		GateReasons:         []string{"freshness_gate.signal_expired"},
		StrategyMode:        "chanlun_v2",
		StrategyName:        "chanlun_v2",
		StrategyVersion:     "v0.1",
		ConfigHash:          "v2-test",
		SignalID:            "chanlun_v2:BNBUSDT:1h:buy2:1710000000000",
		SignalType:          "buy2",
		SignalTimeframe:     "1h",
		SignalCloseTime:     1710000000000,
		DecisionCloseTime:   1710010800000,
		EvaluationCloseTime: 1710010800000,
		ActionTimestamp:     actionTimestamp,
		TradeIntent:         "open_long",
		FreshnessState:      "expired",
		AgeCandles:          3,
		StaleReason:         "信号已过期",
		StrategyMetadata: map[string]any{
			"signal_close_time":     int64(1710000000000),
			"decision_close_time":   int64(1710010800000),
			"evaluation_close_time": int64(1710010800000),
		},
	}
	record := &logger.DecisionRecord{}

	(&AutoTrader{}).appendOpenRejectionsToRecord(record, []decision.OpenRejection{rejection})

	if len(record.Decisions) != 1 {
		t.Fatalf("应写入一条open_rejected动作: %+v", record.Decisions)
	}
	action := record.Decisions[0]
	if action.Action != "open_rejected" || action.SignalID != rejection.SignalID || action.TradeIntent != "open_long" {
		t.Fatalf("拒绝动作基础字段不符合预期: %+v", action)
	}
	if action.SignalCloseTime != rejection.SignalCloseTime || action.DecisionCloseTime != rejection.DecisionCloseTime ||
		action.EvaluationCloseTime != rejection.EvaluationCloseTime || action.ActionTimestamp != actionTimestamp {
		t.Fatalf("拒绝动作应保留结构/评估/动作三类时间: %+v", action)
	}
	if action.FreshnessState != "expired" || action.AgeCandles != 3 || action.StaleReason != "信号已过期" {
		t.Fatalf("拒绝动作应保留freshness元数据: %+v", action)
	}
}
