package chanlun

import (
	"testing"
	"time"
)

func TestSuppressionLifecycleFastSkipPermanentAndGC(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	store := &StateStore{Clock: func() time.Time { return now }, data: emptyStateFile()}

	store.StoreSignalSuppression("t1", "BTCUSDT", SignalSuppression{
		SignalID:     "sig1",
		StructureKey: "struct1",
		Action:       "open_long",
		ReasonCode:   "remaining_net_rr_too_low",
		SuppressedAt: now,
		LastSeenAt:   now,
		SeenCount:    1,
	})
	if reason, ok := store.FastSkipReason("t1", "BTCUSDT", "struct1"); !ok || reason != "remaining_net_rr_too_low" {
		t.Fatalf("fast skip reason错误: ok=%v reason=%s", ok, reason)
	}

	store.UpgradeSuppression("t1", "BTCUSDT", "struct1", "target_already_crossed")
	stats := store.SuppressionStats("t1")
	if stats.TotalActive != 2 || stats.ByReason["target_already_crossed"] != 2 {
		t.Fatalf("suppression升级统计错误: %+v", stats)
	}

	store.MarkPermanentSkip("t1", "BTCUSDT", "struct1")
	if reason, ok := store.FastSkipReason("t1", "BTCUSDT", "struct1"); !ok || reason != "permanent_skip" {
		t.Fatalf("permanent fast skip错误: ok=%v reason=%s", ok, reason)
	}

	store.TerminateLifecycle("t1", "BTCUSDT", "struct2", "signal_expired", "sig2", now.Add(time.Hour))
	if !store.IsLifecycleTerminated("t1", "BTCUSDT", "struct2") {
		t.Fatal("未过期生命周期应处于终结状态")
	}
	now = now.Add(2 * time.Hour)
	store.GCExpiredSuppressions(now)
	if store.IsLifecycleTerminated("t1", "BTCUSDT", "struct2") {
		t.Fatal("过期生命周期应被GC")
	}
}

func TestChanlunSignalIsBornInvalid(t *testing.T) {
	if !(&ChanlunSignal{Direction: SideLong, Price: 100, StopLoss: 101, TakeProfit: 110}).IsBornInvalid() {
		t.Fatal("做多止损高于价格应出生无效")
	}
	if !(&ChanlunSignal{Direction: SideShort, Price: 100, StopLoss: 90, TakeProfit: 80}).IsBornInvalid() {
		t.Fatal("做空止损低于价格应出生无效")
	}
	if (&ChanlunSignal{Direction: SideLong, Price: 100, StopLoss: 95, TakeProfit: 110}).IsBornInvalid() {
		t.Fatal("合法做多结构不应出生无效")
	}
}
