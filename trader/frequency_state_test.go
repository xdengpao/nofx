package trader

import (
	"nofx/decision"
	"nofx/logger"
	"testing"
	"time"
)

func TestCountSignals24hHonorsExplicitSignalCount(t *testing.T) {
	now := time.Now()
	records := []*logger.DecisionRecord{
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"signal_count": 0},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}, {Symbol: "SOLUSDT"}},
		},
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"signal_count": float64(2)},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BNBUSDT"}, {Symbol: "XRPUSDT"}, {Symbol: "DOGEUSDT"}},
		},
		{
			Timestamp:        now,
			CandidateDetails: []logger.CandidateSnapshot{{Symbol: "ADAUSDT"}},
		},
	}
	if got := countSignals24h(records, now.Add(-time.Hour)); got != 3 {
		t.Fatalf("显式signal_count应优先于候选币fallback: got=%d want=3", got)
	}
}

func TestCountSignals24hUsesPerCandidateBeforeFallback(t *testing.T) {
	now := time.Now()
	records := []*logger.DecisionRecord{
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"per_candidate": []any{}},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}},
		},
		{
			Timestamp:           now,
			StrategyDiagnostics: map[string]any{"per_candidate": []any{map[string]any{"symbol": "SOLUSDT"}}},
			CandidateDetails:    []logger.CandidateSnapshot{{Symbol: "SOLUSDT"}, {Symbol: "BNBUSDT"}},
		},
	}
	if got := countSignals24h(records, now.Add(-time.Hour)); got != 1 {
		t.Fatalf("per_candidate空数组也应阻止候选币fallback: got=%d want=1", got)
	}
}

func TestDeriveNoOpenStateUsesLastSuccessfulOpenAndFinalActionFallback(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	openAt := now.Add(-90 * time.Minute)
	records := []*logger.DecisionRecord{
		{
			Timestamp: now.Add(-14 * time.Hour),
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
		},
		{
			Timestamp: now.Add(-2 * time.Hour),
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
			Decisions: []logger.DecisionAction{{
				Action:      "wait",
				FinalAction: "open_long",
				Success:     true,
				Timestamp:   openAt,
			}},
		},
	}

	state := deriveNoOpenState(records, "trader-alpha", now, 30)

	if state.Source != "last_successful_open" {
		t.Fatalf("应从最近成功开仓计算no-open: %+v", state)
	}
	if state.Minutes != 90 || !state.Since.Equal(openAt) || !state.LastSuccessfulOpenAt.Equal(openAt) {
		t.Fatalf("no-open时长或起点错误: %+v", state)
	}
	if !state.LogWindowStart.Equal(now.Add(-14*time.Hour)) || !state.LogWindowEnd.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("日志窗口边界错误: %+v", state)
	}
}

func TestDeriveNoOpenStateUsesLogWindowStartAcrossTwelveHours(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-13 * time.Hour)
	records := []*logger.DecisionRecord{
		{
			Timestamp: windowStart,
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
			Decisions: []logger.DecisionAction{{
				Action:    "open_rejected",
				Success:   false,
				Timestamp: windowStart,
			}},
		},
		{
			Timestamp: now.Add(-30 * time.Minute),
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
			Decisions: []logger.DecisionAction{{
				Action:    "wait",
				Success:   true,
				Timestamp: now.Add(-30 * time.Minute),
			}},
		},
	}

	state := deriveNoOpenState(records, "trader-alpha", now, 5)

	if state.Source != "log_window_start" {
		t.Fatalf("无成功开仓但有日志时应从窗口起点计算: %+v", state)
	}
	if state.Minutes != 13*60 || !state.Since.Equal(windowStart) {
		t.Fatalf("应继承超过12小时no-open窗口: %+v", state)
	}
	if !state.LastSuccessfulOpenAt.IsZero() || state.Warning != "" {
		t.Fatalf("不应记录成功开仓或warning: %+v", state)
	}
}

func TestDeriveNoOpenStateRuntimeFallbackWarns(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)

	state := deriveNoOpenState(nil, "trader-alpha", now, 47)

	if state.Source != "runtime_fallback" || state.Minutes != 47 {
		t.Fatalf("空日志应回退runtime: %+v", state)
	}
	if state.Warning != "frequency_history_unavailable" {
		t.Fatalf("空日志应输出可观测warning: %+v", state)
	}
	if !state.Since.Equal(now.Add(-47 * time.Minute)) {
		t.Fatalf("runtime fallback起点错误: %+v", state)
	}
}

func TestDeriveNoOpenStateScopesRecordsByTrader(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	records := []*logger.DecisionRecord{
		{
			Timestamp: now.Add(-10 * time.Minute),
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "other-trader",
			},
			Decisions: []logger.DecisionAction{{
				Action:    "open_short",
				Success:   true,
				Timestamp: now.Add(-10 * time.Minute),
			}},
		},
		{
			Timestamp: now.Add(-13 * time.Hour),
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
			Decisions: []logger.DecisionAction{{
				Action:    "wait",
				Success:   true,
				Timestamp: now.Add(-13 * time.Hour),
			}},
		},
	}

	state := deriveNoOpenState(records, "trader-alpha", now, 30)

	if state.Source != "log_window_start" || state.Minutes != 13*60 {
		t.Fatalf("应忽略其他trader的成功开仓: %+v", state)
	}
}

func TestBuildFrequencyStateAtPopulatesNoOpenFields(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-3 * time.Hour)
	openAt := now.Add(-45 * time.Minute)
	windowEnd := now.Add(-30 * time.Minute)
	at := &AutoTrader{
		id:        "trader-alpha",
		startTime: now.Add(-2 * time.Hour),
		config: AutoTraderConfig{
			FrequencyPolicy: decision.FrequencyPolicy{
				Mode:                "balanced",
				RollbackWindowHours: 24,
			},
		},
	}
	records := []*logger.DecisionRecord{
		{
			Timestamp: windowStart,
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
		},
		{
			Timestamp: windowEnd,
			RiskState: &logger.RiskStateSnapshot{
				TraderID: "trader-alpha",
			},
			Decisions: []logger.DecisionAction{{
				Action:    "open_long",
				Success:   true,
				Timestamp: openAt,
			}},
		},
	}

	state := at.buildFrequencyStateAt(records, 1000, now)

	if state.OpenCount24h != 1 || state.InactivitySource != "last_successful_open" || state.InactivityMinutes != 45 {
		t.Fatalf("频率状态no-open字段错误: %+v", state)
	}
	if !state.LastOpenAt.Equal(openAt) || !state.NoOpenSince.Equal(openAt) {
		t.Fatalf("最近开仓与no_open_since应一致: %+v", state)
	}
	if !state.LogWindowStart.Equal(windowStart) || !state.LogWindowEnd.Equal(windowEnd) {
		t.Fatalf("日志窗口字段错误: %+v", state)
	}
}

func TestBuildRiskStateSnapshotCopiesInactivityFields(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	ctx := &decision.Context{
		TraderID:        "trader-alpha",
		Exchange:        "aster",
		TotalRiskBudget: 0.08,
		FrequencyState: &decision.FrequencyState{
			OpenCount24h:      0,
			OpenRejected24h:   12,
			SignalCount24h:    4,
			InactivityMinutes: 13 * 60,
			InactivitySource:  "log_window_start",
			NoOpenSince:       now.Add(-13 * time.Hour),
			LogWindowStart:    now.Add(-13 * time.Hour),
			LogWindowEnd:      now.Add(-3 * time.Minute),
			InactivityWarning: "frequency_history_unavailable",
		},
	}

	snapshot := (&AutoTrader{}).buildRiskStateSnapshot(ctx, nil)

	if snapshot.InactivityMinutes != 13*60 || snapshot.InactivitySource != "log_window_start" {
		t.Fatalf("顶层inactivity字段未同步: %+v", snapshot)
	}
	if snapshot.FrequencyState == nil || snapshot.FrequencyState.InactivityMinutes != 13*60 ||
		snapshot.FrequencyState.InactivitySource != "log_window_start" {
		t.Fatalf("嵌套frequency_state字段未同步: %+v", snapshot.FrequencyState)
	}
	if snapshot.NoOpenSince == "" || snapshot.LogWindowStart == "" || snapshot.LogWindowEnd == "" {
		t.Fatalf("no-open时间字段未格式化输出: %+v", snapshot)
	}
	if !snapshot.Warnings["frequency_history_unavailable"] || !snapshot.Warnings["runaway_rejection_loop"] {
		t.Fatalf("warning未写入risk_state.warnings: %+v", snapshot.Warnings)
	}
}
