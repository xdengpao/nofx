package chanlun

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestStableStructureKeyIgnoresConfigHash(t *testing.T) {
	segment := Segment{StartTime: 1000, EndTime: 2000}
	idA := StableSignalID("t1", "btcusdt", SideShort, SignalSell2, "1h", "15m", "c1", segment, "hash-a")
	idB := StableSignalID("t1", "btcusdt", SideShort, SignalSell2, "1h", "15m", "c1", segment, "hash-b")
	if idA == idB {
		t.Fatal("StableSignalID仍应保留config hash差异用于动作 lineage")
	}
	keyA := StableStructureKey("t1", "btcusdt", SideShort, SignalSell2, "1h", "15m", "c1", segment)
	keyB := StableStructureKey("t1", "BTCUSDT", SideShort, SignalSell2, "1h", "15m", "c1", segment)
	if keyA != keyB {
		t.Fatalf("同一结构语义应得到相同structure key: %s != %s", keyA, keyB)
	}
}

func TestStateStoreFoldsRepeatedInvalidatedStructureByStructureKey(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	for _, signalID := range []string{"sig-a", "sig-b"} {
		store.StoreSignalMarker("t1", "BTCUSDT", SignalMarker{
			Symbol:            "BTCUSDT",
			Timeframe:         "1h",
			CloseTime:         1000,
			SignalCloseTime:   1000,
			DecisionCloseTime: 2000,
			DisplayCloseTime:  2000,
			SignalType:        SignalSell2,
			Direction:         SideShort,
			Level:             "1h",
			SourceLayer:       "structure",
			Status:            "invalidated",
			SignalID:          signalID,
			StructureKey:      "structure-key-1",
			ReasonCode:        "target_already_crossed",
			Reason:            "已越过目标",
		})
	}
	markers := store.RecentSignalMarkers("t1", "BTCUSDT", 10)
	if len(markers) != 1 {
		t.Fatalf("同structure key的失效结构应折叠为一条marker: %+v", markers)
	}
	if markers[0].CollapsedCount == 0 || markers[0].LifecycleKey != "structure:structure-key-1" {
		t.Fatalf("折叠marker应保留生命周期统计: %+v", markers[0])
	}
}

func TestPreviewMarkersFoldWithinSameTradeCandle(t *testing.T) {
	store := NewStateStore(filepath.Join(t.TempDir(), "state.json"))
	base := int64(3_599_999)
	for i, closeTime := range []int64{base - 30*60*1000, base - 15*60*1000} {
		store.StoreSignalMarker("t1", "ETHUSDT", SignalMarker{
			Symbol:            "ETHUSDT",
			Timeframe:         "1h",
			CloseTime:         closeTime,
			SignalCloseTime:   closeTime,
			DecisionCloseTime: closeTime,
			DisplayCloseTime:  closeTime,
			SignalType:        SignalBuy3,
			Direction:         SideLong,
			Level:             "1h",
			SourceLayer:       "preview_signal",
			Status:            "confirmed",
			SignalID:          fmt.Sprintf("preview-%d", i),
			StructureKey:      "preview-structure",
			PreviewPhase:      "preview_2x15m",
		})
	}
	markers := store.RecentSignalMarkers("t1", "ETHUSDT", 10)
	if len(markers) != 1 {
		t.Fatalf("同一1h K线内preview应折叠: %+v", markers)
	}
	if markers[0].CollapsedCount == 0 {
		t.Fatalf("preview折叠应计数: %+v", markers[0])
	}
}

func TestSignalReportDefaultAndAuditViews(t *testing.T) {
	report := &SignalReport{
		TraderID:     "t1",
		Symbol:       "SOLUSDT",
		DecisionMode: "programmatic",
		Signals:      []ChanlunSignal{},
		SignalMarkers: []SignalMarker{
			{
				Symbol:            "SOLUSDT",
				Timeframe:         "1h",
				CloseTime:         1000,
				SignalCloseTime:   1000,
				DecisionCloseTime: 2000,
				DisplayCloseTime:  2000,
				SignalType:        SignalSell2,
				Direction:         SideShort,
				Level:             "1h",
				SourceLayer:       "preview_signal",
				Status:            "confirmed",
				SignalID:          "preview",
				StructureKey:      "s1",
				PreviewPhase:      "preview_2x15m",
			},
			{
				Symbol:            "SOLUSDT",
				Timeframe:         "1h",
				CloseTime:         1000,
				SignalCloseTime:   1000,
				DecisionCloseTime: 3000,
				DisplayCloseTime:  3000,
				SignalType:        SignalSell2,
				Direction:         SideShort,
				Level:             "1h",
				SourceLayer:       "structure",
				Status:            "invalidated",
				SignalID:          "structure",
				StructureKey:      "s1",
				ReasonCode:        "target_already_crossed",
			},
		},
	}
	defaultReport := *report
	defaultReport.SignalMarkers = append([]SignalMarker(nil), report.SignalMarkers...)
	applySignalReportOptions(&defaultReport, SignalReportOptions{})
	if defaultReport.View != "default" || len(defaultReport.SignalMarkers) != 1 || defaultReport.SignalMarkers[0].SourceLayer != "structure" {
		t.Fatalf("默认视图应隐藏普通preview历史: view=%s markers=%+v", defaultReport.View, defaultReport.SignalMarkers)
	}
	auditReport := *report
	auditReport.SignalMarkers = append([]SignalMarker(nil), report.SignalMarkers...)
	applySignalReportOptions(&auditReport, SignalReportOptions{View: "audit"})
	if auditReport.View != "audit" || len(auditReport.SignalMarkers) != 2 {
		t.Fatalf("审计视图应返回完整历史: view=%s markers=%+v", auditReport.View, auditReport.SignalMarkers)
	}
}
