package chanlunv2

import (
	"nofx/config"
	"path/filepath"
	"testing"
)

func TestLifecycleStatePersistsAndReloadsWithTraderPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chanlun_v2_lifecycle_{trader_id}.json")
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{LifecycleStatePath: path})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	state := SignalLifecycleState{
		TraderID:              "t1",
		Symbol:                "BNBUSDT",
		ParentSignalID:        "parent-1",
		ParentSignalType:      "buy2",
		ParentSignalCloseTime: 1710000000000,
		Direction:             "long",
		Status:                "watching_entry",
		EntryTriggerID:        "trigger-1",
		EntryTriggerType:      "pullback_retest_resume",
		EntryTriggerCloseTime: 1710000900000,
	}
	engine.upsertLifecycle(state)

	reloaded, err := NewEngine(config.ChanlunV2StrategyConfig{LifecycleStatePath: path})
	if err != nil {
		t.Fatalf("重新创建缠论V2引擎失败: %v", err)
	}
	got, ok := reloaded.lifecycleState("t1", "BNBUSDT", "parent-1")
	if !ok {
		t.Fatal("重启后应能恢复父结构lifecycle")
	}
	if got.EntryTriggerID != "trigger-1" || got.Status != "watching_entry" {
		t.Fatalf("恢复后的lifecycle内容不符合预期: %+v", got)
	}
	if _, ok := reloaded.lifecycleStates[lifecycleTriggerKey("t1", "BNBUSDT", "trigger-1")]; !ok {
		t.Fatalf("entry trigger索引也应随状态恢复: %+v", reloaded.lifecycleStates)
	}
}

func TestLifecycleStateDefaultPathDoesNotWriteRuntimeData(t *testing.T) {
	engine, err := NewEngine(config.ChanlunV2StrategyConfig{})
	if err != nil {
		t.Fatalf("创建缠论V2引擎失败: %v", err)
	}
	if got := engine.lifecyclePersistencePath("t1"); got != "" {
		t.Fatalf("默认不应启用运行时lifecycle写盘路径: %q", got)
	}
	engine.upsertLifecycle(SignalLifecycleState{
		TraderID:       "t1",
		Symbol:         "BNBUSDT",
		ParentSignalID: "parent-1",
		Status:         "watching_entry",
	})
	if _, ok := engine.lifecycleState("t1", "BNBUSDT", "parent-1"); !ok {
		t.Fatal("默认无持久化时仍应保留内存lifecycle")
	}
}
