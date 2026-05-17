package chanlun

import (
	"encoding/json"
	"nofx/decision"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStorePartialCloseGuardPersistenceAndLegacyCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStateStore(path)

	store.RecordProgrammaticPartialClose(ProgrammaticPartialCloseRecord{
		TraderID:                 "t1",
		Symbol:                   "ETHUSDT",
		Side:                     "short",
		Rule:                     "floating_drawdown",
		SignalID:                 "sig-1",
		RequestedClosePercentage: 30,
		ExecutedClosePercentage:  30,
		ExecutedQuantity:         0.3,
		PositionQuantityBefore:   1,
		Price:                    2000,
		PeakPrice:                1900,
		PeakPnLPct:               2.1,
		PeakR:                    1.7,
		ExecutedAt:               time.Unix(100, 0),
	})
	if err := store.Save(); err != nil {
		t.Fatalf("保存状态失败: %v", err)
	}

	reloaded := NewStateStore(path)
	guard := reloaded.PartialCloseGuardState("t1", "ETHUSDT", "short")
	if guard.PartialCloseCount != 1 || guard.TotalPartialClosePct != 30 {
		t.Fatalf("partial close guard持久化错误: %+v", guard)
	}
	if !guard.RequireNewPeakForDrawdown || guard.LastDrawdownPeakPnLPct != 2.1 {
		t.Fatalf("floating drawdown peak锁定状态错误: %+v", guard)
	}

	legacy := ProgrammaticStateFile{
		Version: 1,
		Traders: map[string]ProgrammaticTraderState{
			"t1": {
				Symbols: map[string]ProgrammaticSymbolState{
					"BTCUSDT": {
						PositionStates: map[string]ProgrammaticPositionState{
							"long": {Side: "long", PeakPrice: 100},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("序列化旧状态失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("写入旧状态失败: %v", err)
	}
	reloaded = NewStateStore(path)
	if got := reloaded.PartialCloseGuardState("t1", "BTCUSDT", "long"); got.PartialCloseCount != 0 {
		t.Fatalf("旧状态缺字段应按空guard读取: %+v", got)
	}
}

func TestStateStoreSignalMarkersBoundedAndRecovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStateStore(path)
	for i := 0; i < 205; i++ {
		store.StoreSignalMarker("t1", "ETHUSDT", SignalMarker{
			Symbol:      "ETHUSDT",
			Timeframe:   "1h",
			CloseTime:   int64(i),
			SignalType:  SignalBuy1,
			Direction:   SideLong,
			Level:       "1h",
			SourceLayer: "main_signal",
			Status:      "detected",
			SignalID:    "sig",
		})
	}
	markers := store.RecentSignalMarkers("t1", "ETHUSDT", 300)
	if len(markers) != 200 {
		t.Fatalf("marker应限制为200条，实际=%d", len(markers))
	}
	store.UpdateSignalMarkerStatus("t1", "ETHUSDT", "sig", "executed", "成交")
	if err := store.Save(); err != nil {
		t.Fatalf("保存marker失败: %v", err)
	}
	reloaded := NewStateStore(path)
	recovered := reloaded.RecentSignalMarkers("t1", "ETHUSDT", 10)
	if len(recovered) != 10 {
		t.Fatalf("应能恢复最近marker: %d", len(recovered))
	}
	if recovered[len(recovered)-1].Status != "executed" {
		t.Fatalf("marker状态应恢复为executed: %+v", recovered[len(recovered)-1])
	}
}

func TestStateStoreResetPositionGuardIfMissing(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	store.RecordProgrammaticPartialClose(ProgrammaticPartialCloseRecord{
		TraderID:                 "t1",
		Symbol:                   "ETHUSDT",
		Side:                     "long",
		Rule:                     "short_trade",
		SignalID:                 "sig-1",
		RequestedClosePercentage: 20,
		ExecutedClosePercentage:  20,
		ExecutedQuantity:         0.2,
		PositionQuantityBefore:   1,
	})
	store.ResetPositionGuardIfMissing("t1", []decision.PositionInfo{{Symbol: "BTCUSDT", Side: "long"}})
	if got := store.PartialCloseGuardState("t1", "ETHUSDT", "long"); got.PartialCloseCount != 0 {
		t.Fatalf("持仓消失后应清理旧guard状态: %+v", got)
	}
}
