package logger

// Feature: quant-trading-system
// 任务 18.1: 决策日志测试覆盖
// 覆盖需求: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8

import (
	"os"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// newTestLogger 创建使用临时目录的测试日志记录器
func newTestLogger(t *testing.T) (*DecisionLogger, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "logger_test_*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	l := NewDecisionLogger(dir)
	cleanup := func() { os.RemoveAll(dir) }
	return l, cleanup
}

// newRecord 构造一条基础决策记录
func newRecord(success bool) *DecisionRecord {
	return &DecisionRecord{
		InputPrompt:  "test prompt",
		CoTTrace:     "test cot",
		DecisionJSON: "{}",
		AccountState: AccountSnapshot{
			TotalBalance:     10000.0,
			AvailableBalance: 8000.0,
			PositionCount:    0,
		},
		Decisions:    []DecisionAction{},
		ExecutionLog: []string{},
		Success:      success,
	}
}

func TestBuildTradeOutcomes_AutoCloseAndReasoningBackfill(t *testing.T) {
	baseTime := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp:    baseTime,
			DecisionJSON: `[{"symbol":"BTCUSDT","action":"open_long","reasoning":"4H breakout"}]`,
			Decisions: []DecisionAction{{
				Action:    "open_long",
				Symbol:    "BTCUSDT",
				Quantity:  0.1,
				Leverage:  5,
				Price:     100,
				Timestamp: baseTime,
				Success:   true,
			}},
		},
		{
			Timestamp:    baseTime.Add(time.Hour),
			DecisionJSON: `[{"symbol":"BTCUSDT","action":"auto_close_long","reasoning":"take profit filled"}]`,
			Decisions: []DecisionAction{{
				Action:    "auto_close_long",
				Symbol:    "BTCUSDT",
				Price:     110,
				Timestamp: baseTime.Add(time.Hour),
				Success:   true,
			}},
		},
	}

	outcomes, unmatched := BuildTradeOutcomes(records)
	if len(unmatched) != 0 {
		t.Fatalf("不应有 unmatched，实际=%v", unmatched)
	}
	if len(outcomes) != 1 {
		t.Fatalf("期望1笔闭合交易，实际=%d", len(outcomes))
	}
	trade := outcomes[0]
	if trade.Symbol != "BTCUSDT" || trade.Side != "long" {
		t.Fatalf("交易标识错误: %+v", trade)
	}
	if trade.PnL != 1 {
		t.Fatalf("PnL 期望 1，实际 %.4f", trade.PnL)
	}
	if trade.OpenReason != "4H breakout" || trade.CloseReason != "take profit filled" {
		t.Fatalf("reasoning 回填失败: open=%q close=%q", trade.OpenReason, trade.CloseReason)
	}
}

func TestBuildTradeOutcomes_UnmatchedCloseIsReported(t *testing.T) {
	baseTime := time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{{
		Timestamp: baseTime,
		Decisions: []DecisionAction{{
			Action:    "close_short",
			Symbol:    "ETHUSDT",
			Price:     2000,
			Timestamp: baseTime,
			Success:   true,
		}},
	}}

	outcomes, unmatched := BuildTradeOutcomes(records)
	if len(outcomes) != 0 {
		t.Fatalf("不应生成闭合交易，实际=%d", len(outcomes))
	}
	if len(unmatched) != 1 {
		t.Fatalf("期望1条 unmatched，实际=%d", len(unmatched))
	}
	if unmatched[0].Reason != "missing_open" || unmatched[0].Action != "close_short" {
		t.Fatalf("unmatched 内容错误: %+v", unmatched[0])
	}
}

func TestBuildRollingPerformance_SymbolAndSideGates(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	var outcomes []TradeOutcome
	for i := 0; i < 8; i++ {
		outcomes = append(outcomes, TradeOutcome{
			Symbol:    "BCHUSDT",
			Side:      "long",
			PnL:       -1,
			CloseTime: now.Add(time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < 20; i++ {
		outcomes = append(outcomes, TradeOutcome{
			Symbol:    "TESTUSDT",
			Side:      "short",
			PnL:       -0.5,
			CloseTime: now.Add(time.Duration(i+8) * time.Minute),
		})
	}

	rolling := BuildRollingPerformance(outcomes, now)
	if rolling.SymbolGates["BCHUSDT"].State != "block" {
		t.Fatalf("BCHUSDT 应被 block，实际=%+v", rolling.SymbolGates["BCHUSDT"])
	}
	if rolling.SideGates["short"].State != "penalize" || rolling.SideGates["short"].MinConfidence != 90 {
		t.Fatalf("short side 应被降权且置信度门槛为90，实际=%+v", rolling.SideGates["short"])
	}
	if rolling.EffectiveMaxRiskPerTrade != 0.005 {
		t.Fatalf("最近20笔亏损后风险应降至0.5%%，实际=%.4f", rolling.EffectiveMaxRiskPerTrade)
	}
}

func TestBuildExecutionQuality_DetectsRiskAndRejections(t *testing.T) {
	falseValue := false
	now := time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)
	records := []*DecisionRecord{
		{
			Timestamp:    now,
			Success:      false,
			ErrorMessage: "获取AI决策失败: AI API调用失败",
			Decisions: []DecisionAction{
				{
					Action:          "open_long",
					Symbol:          "BTCUSDT",
					Success:         true,
					Timestamp:       now,
					StopLossSet:     &falseValue,
					ProtectionError: "止损设置失败，仓位未保护",
					HighRisk:        true,
					HighRiskReason:  "高危裸仓",
				},
				{
					Action:      "open_short",
					Symbol:      "ETHUSDT",
					Success:     false,
					Error:       "rolling gate阻止开仓: 最近亏损",
					GateState:   "block",
					GateReasons: []string{"最近亏损"},
				},
				{
					Action:    "partial_close",
					Symbol:    "SOLUSDT",
					Success:   false,
					Error:     "交易所拒绝",
					Timestamp: now.Add(time.Minute),
				},
			},
		},
	}

	stats := BuildExecutionQuality(records, 2)
	if stats.AIFailureCount != 1 {
		t.Fatalf("AI失败计数错误: got=%d", stats.AIFailureCount)
	}
	if stats.OpenAttempts != 2 || stats.OpenFailures != 1 || stats.OpenRejectedCount != 1 {
		t.Fatalf("开仓统计错误: attempts=%d failures=%d rejected=%d", stats.OpenAttempts, stats.OpenFailures, stats.OpenRejectedCount)
	}
	if stats.PartialCloseAttempts != 1 || stats.PartialCloseFailures != 1 || stats.PartialCloseFailureRate != 100 {
		t.Fatalf("partial close统计错误: %+v", stats)
	}
	if stats.ProtectionOrderFailures != 1 || stats.HighRiskExecutionFailures != 1 {
		t.Fatalf("高危/保护单统计错误: protection=%d highRisk=%d", stats.ProtectionOrderFailures, stats.HighRiskExecutionFailures)
	}
	if stats.UnmatchedActionCount != 2 {
		t.Fatalf("unmatched计数错误: got=%d", stats.UnmatchedActionCount)
	}
	if len(stats.RecentHighRiskErrors) == 0 || stats.RecentHighRiskErrors[0].Symbol != "BTCUSDT" {
		t.Fatalf("应记录最近高危错误: %+v", stats.RecentHighRiskErrors)
	}
	if len(stats.RecentOpenRejectionReasons) == 0 {
		t.Fatalf("应记录最近开仓拒绝原因")
	}
}

// ============================================================================
// 需求 11.1: LogDecision 和 GetLatestRecords 往返
// ============================================================================

func TestLogDecision_RoundTrip(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	record := newRecord(true)
	record.InputPrompt = "unique-prompt-12345"

	if err := l.LogDecision(record); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("期望1条记录, 实际=%d", len(records))
	}
	if records[0].InputPrompt != "unique-prompt-12345" {
		t.Errorf("InputPrompt 往返失败: 期望 unique-prompt-12345, 实际=%s", records[0].InputPrompt)
	}
}

func TestLogDecision_SetsTimestamp(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	before := time.Now()
	if err := l.LogDecision(newRecord(true)); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}
	after := time.Now()

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	ts := records[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp 应在 [before, after] 范围内, 实际=%v", ts)
	}
}

func TestLogDecision_SetsCycleNumber(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		if err := l.LogDecision(newRecord(true)); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	records, err := l.GetLatestRecords(3)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("期望3条记录, 实际=%d", len(records))
	}
	for i, r := range records {
		if r.CycleNumber != i+1 {
			t.Errorf("记录[%d] CycleNumber 应为 %d, 实际=%d", i, i+1, r.CycleNumber)
		}
	}
}

// ============================================================================
// 需求 11.2: GetLatestRecords 返回正确数量
// ============================================================================

func TestGetLatestRecords_ReturnsCorrectCount(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		if err := l.LogDecision(newRecord(true)); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	records, err := l.GetLatestRecords(3)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("期望3条记录, 实际=%d", len(records))
	}
}

func TestGetLatestRecords_EmptyDir_ReturnsEmpty(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	records, err := l.GetLatestRecords(10)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("空目录应返回0条记录, 实际=%d", len(records))
	}
}

func TestGetLatestRecords_RequestMoreThanAvailable(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	if err := l.LogDecision(newRecord(true)); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(100)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("只有1条记录时应返回1条, 实际=%d", len(records))
	}
}

// ============================================================================
// 需求 11.3: GetLatestRecords 按时间戳升序排列
// ============================================================================

func TestGetLatestRecords_AscendingTimestampOrder(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		if err := l.LogDecision(newRecord(true)); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	records, err := l.GetLatestRecords(3)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records) < 2 {
		t.Skip("记录数不足，跳过排序测试")
	}

	for i := 1; i < len(records); i++ {
		if records[i].Timestamp.Before(records[i-1].Timestamp) {
			t.Errorf("记录[%d].Timestamp(%v) 应 >= 记录[%d].Timestamp(%v)",
				i, records[i].Timestamp, i-1, records[i-1].Timestamp)
		}
	}
}

// ============================================================================
// 需求 11.4: Success 字段正确记录
// ============================================================================

func TestLogDecision_SuccessField_True(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	if err := l.LogDecision(newRecord(true)); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if !records[0].Success {
		t.Error("Success=true 应被正确保存")
	}
}

func TestLogDecision_SuccessField_False(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	r := newRecord(false)
	r.ErrorMessage = "测试错误"
	if err := l.LogDecision(r); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if records[0].Success {
		t.Error("Success=false 应被正确保存")
	}
	if records[0].ErrorMessage != "测试错误" {
		t.Errorf("ErrorMessage 应为 '测试错误', 实际=%s", records[0].ErrorMessage)
	}
}

// ============================================================================
// 需求 11.5: Decisions 字段正确记录
// ============================================================================

func TestLogDecision_DecisionsField_RoundTrip(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	r := newRecord(true)
	r.Decisions = []DecisionAction{
		{
			Action:    "open_long",
			Symbol:    "BTCUSDT",
			Quantity:  0.1,
			Leverage:  5,
			Price:     50000.0,
			Timestamp: time.Now(),
			Success:   true,
		},
	}

	if err := l.LogDecision(r); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}
	if len(records[0].Decisions) != 1 {
		t.Fatalf("期望1个决策动作, 实际=%d", len(records[0].Decisions))
	}
	d := records[0].Decisions[0]
	if d.Action != "open_long" {
		t.Errorf("Action 应为 open_long, 实际=%s", d.Action)
	}
	if d.Symbol != "BTCUSDT" {
		t.Errorf("Symbol 应为 BTCUSDT, 实际=%s", d.Symbol)
	}
}

// ============================================================================
// 需求 11.6: AnalyzePerformance 胜率计算正确
// ============================================================================

func TestAnalyzePerformance_EmptyRecords_ReturnsZero(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	analysis, err := l.AnalyzePerformance(10)
	if err != nil {
		t.Fatalf("AnalyzePerformance 失败: %v", err)
	}
	if analysis.TotalTrades != 0 {
		t.Errorf("无记录时 TotalTrades 应为0, 实际=%d", analysis.TotalTrades)
	}
	if analysis.WinRate != 0 {
		t.Errorf("无记录时 WinRate 应为0, 实际=%.2f", analysis.WinRate)
	}
}

func TestAnalyzePerformance_WinRate_Calculation(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	openTime := time.Now()

	// 交易1: 开多 BTCUSDT @ 50000，平多 @ 55000（盈利）
	r1 := newRecord(true)
	r1.Decisions = []DecisionAction{
		{Action: "open_long", Symbol: "BTCUSDT", Quantity: 0.1, Leverage: 5,
			Price: 50000.0, Timestamp: openTime, Success: true},
	}
	if err := l.LogDecision(r1); err != nil {
		t.Fatalf("LogDecision r1 失败: %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	r2 := newRecord(true)
	r2.Decisions = []DecisionAction{
		{Action: "close_long", Symbol: "BTCUSDT", Quantity: 0.1, Leverage: 5,
			Price: 55000.0, Timestamp: openTime.Add(time.Hour), Success: true},
	}
	if err := l.LogDecision(r2); err != nil {
		t.Fatalf("LogDecision r2 失败: %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	// 交易2: 开空 ETHUSDT @ 3000，平空 @ 3100（亏损）
	r3 := newRecord(true)
	r3.Decisions = []DecisionAction{
		{Action: "open_short", Symbol: "ETHUSDT", Quantity: 1.0, Leverage: 5,
			Price: 3000.0, Timestamp: openTime, Success: true},
	}
	if err := l.LogDecision(r3); err != nil {
		t.Fatalf("LogDecision r3 失败: %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	r4 := newRecord(true)
	r4.Decisions = []DecisionAction{
		{Action: "close_short", Symbol: "ETHUSDT", Quantity: 1.0, Leverage: 5,
			Price: 3100.0, Timestamp: openTime.Add(time.Hour), Success: true},
	}
	if err := l.LogDecision(r4); err != nil {
		t.Fatalf("LogDecision r4 失败: %v", err)
	}

	analysis, err := l.AnalyzePerformance(10)
	if err != nil {
		t.Fatalf("AnalyzePerformance 失败: %v", err)
	}

	if analysis.TotalTrades != 2 {
		t.Errorf("TotalTrades 应为2, 实际=%d", analysis.TotalTrades)
	}
	if analysis.WinningTrades != 1 {
		t.Errorf("WinningTrades 应为1, 实际=%d", analysis.WinningTrades)
	}
	if analysis.LosingTrades != 1 {
		t.Errorf("LosingTrades 应为1, 实际=%d", analysis.LosingTrades)
	}
	if analysis.WinRate != 50.0 {
		t.Errorf("WinRate 应为 50.0%%, 实际=%.1f%%", analysis.WinRate)
	}
}

func TestAnalyzePerformance_AllWins_WinRate100(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	openTime := time.Now()
	for i, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
		r1 := newRecord(true)
		r1.Decisions = []DecisionAction{
			{Action: "open_long", Symbol: symbol, Quantity: 0.1, Leverage: 5,
				Price: 50000.0, Timestamp: openTime.Add(time.Duration(i) * time.Minute), Success: true},
		}
		if err := l.LogDecision(r1); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(15 * time.Millisecond)

		r2 := newRecord(true)
		r2.Decisions = []DecisionAction{
			{Action: "close_long", Symbol: symbol, Quantity: 0.1, Leverage: 5,
				Price: 55000.0, Timestamp: openTime.Add(time.Duration(i)*time.Minute + time.Hour), Success: true},
		}
		if err := l.LogDecision(r2); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(15 * time.Millisecond)
	}

	analysis, err := l.AnalyzePerformance(10)
	if err != nil {
		t.Fatalf("AnalyzePerformance 失败: %v", err)
	}
	if analysis.WinRate != 100.0 {
		t.Errorf("全部盈利时 WinRate 应为100%%, 实际=%.1f%%", analysis.WinRate)
	}
}

// ============================================================================
// 需求 11.7: GetStatistics 统计信息正确
// ============================================================================

func TestGetStatistics_CountsCycles(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		if err := l.LogDecision(newRecord(true)); err != nil {
			t.Fatalf("LogDecision 失败: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := l.LogDecision(newRecord(false)); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	stats, err := l.GetStatistics()
	if err != nil {
		t.Fatalf("GetStatistics 失败: %v", err)
	}
	if stats.TotalCycles != 4 {
		t.Errorf("TotalCycles 应为4, 实际=%d", stats.TotalCycles)
	}
	if stats.SuccessfulCycles != 3 {
		t.Errorf("SuccessfulCycles 应为3, 实际=%d", stats.SuccessfulCycles)
	}
	if stats.FailedCycles != 1 {
		t.Errorf("FailedCycles 应为1, 实际=%d", stats.FailedCycles)
	}
}

func TestGetStatistics_CountsOpenClose(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	r := newRecord(true)
	r.Decisions = []DecisionAction{
		{Action: "open_long", Symbol: "BTCUSDT", Success: true},
		{Action: "open_short", Symbol: "ETHUSDT", Success: true},
		{Action: "close_long", Symbol: "SOLUSDT", Success: true},
	}
	if err := l.LogDecision(r); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	stats, err := l.GetStatistics()
	if err != nil {
		t.Fatalf("GetStatistics 失败: %v", err)
	}
	if stats.TotalOpenPositions != 2 {
		t.Errorf("TotalOpenPositions 应为2, 实际=%d", stats.TotalOpenPositions)
	}
	if stats.TotalClosePositions != 1 {
		t.Errorf("TotalClosePositions 应为1, 实际=%d", stats.TotalClosePositions)
	}
}

// ============================================================================
// 需求 11.8: AccountState 快照正确保存
// ============================================================================

func TestLogDecision_AccountState_RoundTrip(t *testing.T) {
	l, cleanup := newTestLogger(t)
	defer cleanup()

	r := newRecord(true)
	r.AccountState = AccountSnapshot{
		TotalBalance:          12345.67,
		AvailableBalance:      9876.54,
		TotalUnrealizedProfit: 100.0,
		PositionCount:         2,
		MarginUsedPct:         15.5,
	}

	if err := l.LogDecision(r); err != nil {
		t.Fatalf("LogDecision 失败: %v", err)
	}

	records, err := l.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords 失败: %v", err)
	}

	snap := records[0].AccountState
	if snap.TotalBalance != 12345.67 {
		t.Errorf("TotalBalance 应为 12345.67, 实际=%.2f", snap.TotalBalance)
	}
	if snap.PositionCount != 2 {
		t.Errorf("PositionCount 应为2, 实际=%d", snap.PositionCount)
	}
}

// ============================================================================
// Property 41: 决策日志往返 (属性基测试)
// Feature: quant-trading-system, Property 41: 决策日志往返
// 验证需求: 11.1
// 对任意 DecisionRecord，LogDecision 后 GetLatestRecords(1) 应返回包含该记录关键字段的记录
// ============================================================================

func TestProperty41_DecisionLogRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("Property 41: 任意 DecisionRecord 经 LogDecision 后 GetLatestRecords(1) 应返回等价记录", prop.ForAll(
		func(
			inputPrompt string,
			cotTrace string,
			decisionJSON string,
			totalBalance float64,
			availableBalance float64,
			positionCount int,
			success bool,
		) bool {
			l, cleanup := newTestLogger(t)
			defer cleanup()

			record := &DecisionRecord{
				InputPrompt:  inputPrompt,
				CoTTrace:     cotTrace,
				DecisionJSON: decisionJSON,
				AccountState: AccountSnapshot{
					TotalBalance:     totalBalance,
					AvailableBalance: availableBalance,
					PositionCount:    positionCount,
				},
				Decisions:    []DecisionAction{},
				ExecutionLog: []string{},
				Success:      success,
			}

			if err := l.LogDecision(record); err != nil {
				return false
			}

			records, err := l.GetLatestRecords(1)
			if err != nil || len(records) != 1 {
				return false
			}

			got := records[0]
			return got.InputPrompt == inputPrompt &&
				got.CoTTrace == cotTrace &&
				got.DecisionJSON == decisionJSON &&
				got.AccountState.TotalBalance == totalBalance &&
				got.AccountState.AvailableBalance == availableBalance &&
				got.AccountState.PositionCount == positionCount &&
				got.Success == success &&
				got.CycleNumber > 0 &&
				!got.Timestamp.IsZero()
		},
		gen.AnyString(),
		gen.AnyString(),
		gen.AnyString(),
		gen.Float64Range(0, 1000000),
		gen.Float64Range(0, 1000000),
		gen.IntRange(0, 3),
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 42: 日志时间正序 (属性基测试)
// Feature: quant-trading-system, Property 42: 日志时间正序
// 验证需求: 11.3
// 对任意 N 条按时间顺序记录的日志，GetLatestRecords(N) 应按时间戳升序排列
// ============================================================================

func TestProperty42LogTimeAscending(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	properties.Property("Property 42: GetLatestRecords(N) 应按时间戳升序排列", prop.ForAll(
		func(n int) bool {
			l, cleanup := newTestLogger(t)
			defer cleanup()

			// 按顺序记录 n 条日志，每条之间间隔确保时间戳严格递增
			for i := 0; i < n; i++ {
				if err := l.LogDecision(newRecord(true)); err != nil {
					return false
				}
				// 确保相邻记录的时间戳不同（LogDecision 使用 time.Now()）
				time.Sleep(2 * time.Millisecond)
			}

			records, err := l.GetLatestRecords(n)
			if err != nil {
				return false
			}
			if len(records) != n {
				return false
			}

			// 验证时间戳升序（每条记录的时间戳 <= 下一条）
			for i := 1; i < len(records); i++ {
				if records[i].Timestamp.Before(records[i-1].Timestamp) {
					return false
				}
			}
			return true
		},
		gen.IntRange(1, 10),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 43: 表现分析胜率正确性 (属性基测试)
// Feature: quant-trading-system, Property 43: 表现分析胜率正确性
// 验证需求: 11.6
// 对任意已知开仓和平仓配对，AnalyzePerformance 的 WinRate = 盈利交易数 / 总交易数 × 100
// ============================================================================

// tradeSpec 描述一笔交易的参数（用于属性测试生成）
type tradeSpec struct {
	symbol     string
	side       string // "long" or "short"
	openPrice  float64
	closePrice float64
	quantity   float64
	leverage   int
}

// pnlForTrade 计算单笔交易的盈亏
func pnlForTrade(ts tradeSpec) float64 {
	if ts.side == "long" {
		return ts.quantity * (ts.closePrice - ts.openPrice)
	}
	return ts.quantity * (ts.openPrice - ts.closePrice)
}

func TestProperty43_WinRateCorrectness(t *testing.T) {
	// **Validates: Requirements 11.6**
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)

	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"}

	// 属性 43a: 对 N 笔已知盈亏的交易，WinRate = 盈利数 / 总数 × 100
	properties.Property("Property 43: WinRate = 盈利交易数 / 总交易数 × 100", prop.ForAll(
		func(n int) bool {
			if n == 0 {
				return true
			}
			l, cleanup := newTestLogger(t)
			defer cleanup()

			baseTime := time.Now()
			profitableCount := 0
			totalCount := 0

			for i := 0; i < n; i++ {
				symbol := symbols[i%len(symbols)]
				side := "long"
				if i%2 == 1 {
					side = "short"
				}
				openPrice := 10000.0 + float64(i)*100
				var closePrice float64
				if i%3 != 0 {
					// 盈利
					if side == "long" {
						closePrice = openPrice * 1.05
					} else {
						closePrice = openPrice * 0.95
					}
					profitableCount++
				} else {
					// 亏损
					if side == "long" {
						closePrice = openPrice * 0.95
					} else {
						closePrice = openPrice * 1.05
					}
				}
				totalCount++

				openAction := "open_long"
				closeAction := "close_long"
				if side == "short" {
					openAction = "open_short"
					closeAction = "close_short"
				}

				rOpen := newRecord(true)
				rOpen.Decisions = []DecisionAction{
					{
						Action:    openAction,
						Symbol:    symbol,
						Quantity:  0.1,
						Leverage:  5,
						Price:     openPrice,
						Timestamp: baseTime.Add(time.Duration(i*2) * time.Millisecond),
						Success:   true,
					},
				}
				if err := l.LogDecision(rOpen); err != nil {
					return false
				}
				time.Sleep(2 * time.Millisecond)

				rClose := newRecord(true)
				rClose.Decisions = []DecisionAction{
					{
						Action:    closeAction,
						Symbol:    symbol,
						Quantity:  0.1,
						Leverage:  5,
						Price:     closePrice,
						Timestamp: baseTime.Add(time.Duration(i*2+1) * time.Millisecond),
						Success:   true,
					},
				}
				if err := l.LogDecision(rClose); err != nil {
					return false
				}
				time.Sleep(2 * time.Millisecond)
			}

			// lookbackCycles 覆盖所有记录（每笔交易写2条记录）
			lookback := n * 2
			analysis, err := l.AnalyzePerformance(lookback)
			if err != nil {
				return false
			}

			if analysis.TotalTrades != totalCount {
				return false
			}

			expectedWinRate := float64(profitableCount) / float64(totalCount) * 100.0
			diff := analysis.WinRate - expectedWinRate
			if diff < 0 {
				diff = -diff
			}
			return diff < 0.001
		},
		gen.IntRange(1, 5),
	))

	// 属性 43b: 单笔随机开平仓对，胜率为 0% 或 100%
	properties.Property("Property 43b: 单笔交易胜率为0或100", prop.ForAll(
		func(sideIdx int, openPrice, closePrice float64, leverage int) bool {
			side := []string{"long", "short"}[sideIdx]
			ts := tradeSpec{
				symbol:     "BTCUSDT",
				side:       side,
				openPrice:  openPrice,
				closePrice: closePrice,
				quantity:   0.1,
				leverage:   leverage,
			}

			l, cleanup := newTestLogger(t)
			defer cleanup()

			openAction := "open_long"
			closeAction := "close_long"
			if ts.side == "short" {
				openAction = "open_short"
				closeAction = "close_short"
			}

			baseTime := time.Now()

			rOpen := newRecord(true)
			rOpen.Decisions = []DecisionAction{
				{
					Action:    openAction,
					Symbol:    ts.symbol,
					Quantity:  ts.quantity,
					Leverage:  ts.leverage,
					Price:     ts.openPrice,
					Timestamp: baseTime,
					Success:   true,
				},
			}
			if err := l.LogDecision(rOpen); err != nil {
				return false
			}
			time.Sleep(2 * time.Millisecond)

			rClose := newRecord(true)
			rClose.Decisions = []DecisionAction{
				{
					Action:    closeAction,
					Symbol:    ts.symbol,
					Quantity:  ts.quantity,
					Leverage:  ts.leverage,
					Price:     ts.closePrice,
					Timestamp: baseTime.Add(time.Hour),
					Success:   true,
				},
			}
			if err := l.LogDecision(rClose); err != nil {
				return false
			}

			analysis, err := l.AnalyzePerformance(10)
			if err != nil {
				return false
			}

			if analysis.TotalTrades != 1 {
				return false
			}

			pnl := pnlForTrade(ts)
			var expectedWinRate float64
			if pnl > 0 {
				expectedWinRate = 100.0
			} else {
				// pnl <= 0: 不计入盈利，WinRate = 0
				expectedWinRate = 0.0
			}

			diff := analysis.WinRate - expectedWinRate
			if diff < 0 {
				diff = -diff
			}
			return diff < 0.001
		},
		gen.IntRange(0, 1),
		gen.Float64Range(100, 50000),
		gen.Float64Range(100, 50000),
		gen.IntRange(1, 10),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Feature: quant-trading-system, Property 47: 成交记录字段完整性
// 验证: 需求 13.11, 13.14
// ============================================================================

func TestProperty47_TradeOutcomeFieldCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"}

	// 属性 47a: N 笔开平仓配对，每个 TradeOutcome 所有核心字段非零，时间有序
	properties.Property("Property 47: 成交记录字段完整性", prop.ForAll(
		func(n int) bool {
			if n == 0 {
				return true
			}
			l, cleanup := newTestLogger(t)
			defer cleanup()
			baseTime := time.Now().Add(-time.Hour)
			for i := 0; i < n; i++ {
				symbol := symbols[i%len(symbols)]
				side := "long"
				if i%2 == 1 {
					side = "short"
				}
				openPrice := 10000.0 + float64(i)*500
				closePrice := openPrice * 1.03
				if (i%3 == 0 && side == "long") || (i%3 != 0 && side == "short") {
					closePrice = openPrice * 0.97
				}
				openAction, closeAction := "open_long", "close_long"
				if side == "short" {
					openAction, closeAction = "open_short", "close_short"
				}
				openTime := baseTime.Add(time.Duration(i*10) * time.Minute)
				closeTime := openTime.Add(5 * time.Minute)
				qty := 0.5 + float64(i)*0.1
				lev := 3 + (i % 8)
				rOpen := newRecord(true)
				rOpen.Decisions = []DecisionAction{{Action: openAction, Symbol: symbol, Quantity: qty, Leverage: lev, Price: openPrice, Timestamp: openTime, Success: true}}
				if err := l.LogDecision(rOpen); err != nil {
					return false
				}
				time.Sleep(2 * time.Millisecond)
				rClose := newRecord(true)
				rClose.Decisions = []DecisionAction{{Action: closeAction, Symbol: symbol, Quantity: qty, Leverage: lev, Price: closePrice, Timestamp: closeTime, Success: true}}
				if err := l.LogDecision(rClose); err != nil {
					return false
				}
				time.Sleep(2 * time.Millisecond)
			}
			analysis, err := l.AnalyzePerformance(n * 2)
			if err != nil {
				return false
			}
			if analysis.TotalTrades != n {
				return false
			}
			for _, trade := range analysis.RecentTrades {
				if trade.OpenTime.IsZero() || trade.CloseTime.IsZero() {
					return false
				}
				if !trade.CloseTime.After(trade.OpenTime) {
					return false
				}
				if trade.Symbol == "" || trade.Side == "" {
					return false
				}
				if trade.Quantity <= 0 || trade.Leverage <= 0 {
					return false
				}
				if trade.OpenPrice <= 0 || trade.ClosePrice <= 0 {
					return false
				}
				if trade.PositionValue <= 0 || trade.MarginUsed <= 0 {
					return false
				}
			}
			return true
		},
		gen.IntRange(1, 5),
	))

	// 属性 47b: 单笔随机参数交易，字段完整性和计算正确性
	properties.Property("Property 47b: 单笔随机交易字段完整性", prop.ForAll(
		func(sideIdx int, openPrice, closePrice, quantity float64, leverage int) bool {
			l, cleanup := newTestLogger(t)
			defer cleanup()
			side := []string{"long", "short"}[sideIdx]
			symbol := "BTCUSDT"
			openAction, closeAction := "open_long", "close_long"
			if side == "short" {
				openAction, closeAction = "open_short", "close_short"
			}
			baseTime := time.Now().Add(-time.Hour)
			closeTime := baseTime.Add(30 * time.Minute)
			rOpen := newRecord(true)
			rOpen.Decisions = []DecisionAction{{Action: openAction, Symbol: symbol, Quantity: quantity, Leverage: leverage, Price: openPrice, Timestamp: baseTime, Success: true}}
			if err := l.LogDecision(rOpen); err != nil {
				return false
			}
			time.Sleep(2 * time.Millisecond)
			rClose := newRecord(true)
			rClose.Decisions = []DecisionAction{{Action: closeAction, Symbol: symbol, Quantity: quantity, Leverage: leverage, Price: closePrice, Timestamp: closeTime, Success: true}}
			if err := l.LogDecision(rClose); err != nil {
				return false
			}
			analysis, err := l.AnalyzePerformance(10)
			if err != nil || analysis.TotalTrades != 1 {
				return false
			}
			trade := analysis.RecentTrades[0]
			if trade.OpenTime.IsZero() || trade.CloseTime.IsZero() || !trade.CloseTime.After(trade.OpenTime) {
				return false
			}
			if trade.Symbol != symbol || trade.Side != side {
				return false
			}
			if trade.Quantity != quantity || trade.Leverage != leverage {
				return false
			}
			if trade.OpenPrice != openPrice || trade.ClosePrice != closePrice {
				return false
			}
			expectedPV := quantity * openPrice
			expectedMargin := expectedPV / float64(leverage)
			if trade.PositionValue <= 0 || trade.MarginUsed <= 0 {
				return false
			}
			pvDiff := trade.PositionValue - expectedPV
			if pvDiff < 0 {
				pvDiff = -pvDiff
			}
			marginDiff := trade.MarginUsed - expectedMargin
			if marginDiff < 0 {
				marginDiff = -marginDiff
			}
			return pvDiff < 0.01 && marginDiff < 0.01
		},
		gen.IntRange(0, 1),
		gen.Float64Range(100, 50000),
		gen.Float64Range(100, 50000),
		gen.Float64Range(0.01, 10.0),
		gen.IntRange(1, 20),
	))

	properties.TestingRun(t)
}
