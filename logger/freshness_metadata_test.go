package logger

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecisionActionFreshnessMetadataJSONCompatibility(t *testing.T) {
	var legacy DecisionRecord
	if err := json.Unmarshal([]byte(`{"timestamp":"2026-05-23T00:00:00Z","decisions":[{"action":"wait","symbol":"ALL","timestamp":"2026-05-23T00:00:00Z","success":true}]}`), &legacy); err != nil {
		t.Fatalf("旧日志缺少freshness字段时仍应可读: %v", err)
	}
	if len(legacy.Decisions) != 1 || legacy.Decisions[0].FreshnessState != "" || legacy.Decisions[0].AgeCandles != 0 {
		t.Fatalf("旧日志缺省字段应保持零值: %+v", legacy.Decisions)
	}

	record := DecisionRecord{
		Timestamp: time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC),
		Decisions: []DecisionAction{{
			Action:              "open_rejected",
			Symbol:              "BNBUSDT",
			Timestamp:           time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC),
			SignalID:            "chanlun_v2:BNBUSDT:1h:buy2:1710000000000",
			SignalCloseTime:     1710000000000,
			DecisionCloseTime:   1710010800000,
			EvaluationCloseTime: 1710010800000,
			ActionTimestamp:     1710010860000,
			FreshnessState:      "expired",
			AgeCandles:          3,
			StaleReason:         "信号已过期",
		}},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("新日志序列化失败: %v", err)
	}
	var decoded DecisionRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("新日志反序列化失败: %v", err)
	}
	action := decoded.Decisions[0]
	if action.EvaluationCloseTime != 1710010800000 || action.ActionTimestamp != 1710010860000 ||
		action.FreshnessState != "expired" || action.AgeCandles != 3 || action.StaleReason != "信号已过期" {
		t.Fatalf("freshness字段JSON往返不一致: %+v", action)
	}
}
