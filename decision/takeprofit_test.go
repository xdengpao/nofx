package decision

// Feature: quant-trading-system
// 任务 14.1: 持仓评估器测试覆盖
// 覆盖需求: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10

import (
	"math"
	"nofx/market"
	"os"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestMain 初始化 planManager，避免 evaluateAdaptiveScaledExit 中 nil 指针
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "takeprofit_test_*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if err := InitPlanManager(dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// ============================================================================
// 测试辅助函数
// ============================================================================

func newLongPosition(entryPrice, markPrice, pnlPct float64, updateTime int64) *PositionInfo {
	return &PositionInfo{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		EntryPrice:       entryPrice,
		MarkPrice:        markPrice,
		Quantity:         0.1,
		UnrealizedPnLPct: pnlPct,
		UpdateTime:       updateTime,
	}
}

func newShortPosition(entryPrice, markPrice, pnlPct float64, updateTime int64) *PositionInfo {
	return &PositionInfo{
		Symbol:           "BTCUSDT",
		Side:             "SELL",
		EntryPrice:       entryPrice,
		MarkPrice:        markPrice,
		Quantity:         0.1,
		UnrealizedPnLPct: pnlPct,
		UpdateTime:       updateTime,
	}
}

func newLongPlan(entry, sl, tp float64) *TradePlan {
	return &TradePlan{
		Symbol:           "BTCUSDT",
		Direction:        "long",
		EntryPrice:       entry,
		StopLoss:         sl,
		TakeProfit:       tp,
		CurrentStopLoss:  sl,
		MinHoldMinutes:   30,
		CreatedAt:        time.Now().Add(-2 * time.Hour),
		Status:           "active",
		ExecutedTranches: make(map[int]bool),
	}
}

func newShortPlan(entry, sl, tp float64) *TradePlan {
	return &TradePlan{
		Symbol:           "BTCUSDT",
		Direction:        "short",
		EntryPrice:       entry,
		StopLoss:         sl,
		TakeProfit:       tp,
		CurrentStopLoss:  sl,
		MinHoldMinutes:   30,
		CreatedAt:        time.Now().Add(-2 * time.Hour),
		Status:           "active",
		ExecutedTranches: make(map[int]bool),
	}
}

func newMarketData(currentPrice float64) *market.Data {
	return &market.Data{
		CurrentPrice:   currentPrice,
		CurrentADX:     30.0,
		CurrentRSI14:   50.0,
		CurrentEMA20:   currentPrice * 0.99,
		CurrentEMA50:   currentPrice * 0.98,
		CurrentDIPlus:  25.0,
		CurrentDIMinus: 15.0,
		LongerTermContext: &market.LongerTermData{
			ATR14: currentPrice * 0.02,
			EMA20: currentPrice * 0.99,
			EMA50: currentPrice * 0.98,
		},
	}
}

func pastTime(minutesAgo int) int64 {
	return time.Now().Add(-time.Duration(minutesAgo) * time.Minute).UnixMilli()
}

// ============================================================================
// 需求 4.2: 止损触发立即平仓 (单元测试)
// ============================================================================

func TestStopLoss_Long_TriggerClose(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 48999, -2.0, pastTime(60))
	md := newMarketData(48999)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("多头止损: 期望 action=close, 实际=%s", result.Action)
	}
	if !result.IsHardStop {
		t.Error("多头止损: 期望 IsHardStop=true")
	}
}

func TestStopLoss_Long_AtExactPrice(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 49000, -2.0, pastTime(60))
	md := newMarketData(49000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("多头止损(等于): 期望 action=close, 实际=%s", result.Action)
	}
}

func TestStopLoss_Long_NoTrigger(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 50500, 1.0, pastTime(60))
	md := newMarketData(50500)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" && result.IsHardStop {
		t.Error("多头: 价格高于止损价不应触发止损平仓")
	}
}

func TestStopLoss_Short_TriggerClose(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 51001, -2.0, pastTime(60))
	md := newMarketData(51001)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头止损: 期望 action=close, 实际=%s", result.Action)
	}
	if !result.IsHardStop {
		t.Error("空头止损: 期望 IsHardStop=true")
	}
}

func TestStopLoss_Short_AtExactPrice(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 51000, -2.0, pastTime(60))
	md := newMarketData(51000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头止损(等于): 期望 action=close, 实际=%s", result.Action)
	}
}

func TestStopLoss_Short_NoTrigger(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 49500, 1.0, pastTime(60))
	md := newMarketData(49500)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" && result.IsHardStop {
		t.Error("空头: 价格低于止损价不应触发止损平仓")
	}
}

func TestStopLoss_UsesCurrentStopLoss(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.CurrentStopLoss = 50500
	pos := newLongPosition(50000, 50400, 0.8, pastTime(60))
	md := newMarketData(50400)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("移动止损: 当前价50400 <= CurrentStopLoss50500, 期望 close, 实际=%s", result.Action)
	}
}

// ============================================================================
// 需求 4.3: 止盈触发立即平仓
// ============================================================================

func TestTakeProfit_Long_TriggerClose(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 55001, 10.0, pastTime(60))
	md := newMarketData(55001)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("多头止盈: 期望 action=close, 实际=%s", result.Action)
	}
}

func TestTakeProfit_Long_AtExactPrice(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 55000, 10.0, pastTime(60))
	md := newMarketData(55000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("多头止盈(等于): 期望 action=close, 实际=%s", result.Action)
	}
}

func TestTakeProfit_Long_NoTrigger(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 52000, 4.0, pastTime(60))
	md := newMarketData(52000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" && result.IsHardStop {
		t.Error("多头: 价格未达止盈价不应触发止盈平仓")
	}
}

func TestTakeProfit_Short_TriggerClose(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 44999, 10.0, pastTime(60))
	md := newMarketData(44999)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头止盈: 期望 action=close, 实际=%s", result.Action)
	}
}

func TestTakeProfit_Short_AtExactPrice(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 45000, 10.0, pastTime(60))
	md := newMarketData(45000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头止盈(等于): 期望 action=close, 实际=%s", result.Action)
	}
}

func TestTakeProfit_Short_NoTrigger(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 47000, 6.0, pastTime(60))
	md := newMarketData(47000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" && result.IsHardStop {
		t.Error("空头: 价格未达止盈价不应触发止盈平仓")
	}
}

// ============================================================================
// 需求 4.4: 最小持仓时间保护
// ============================================================================

func TestMinHoldTime_WithinProtection_Hold(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 49500, -1.0, pastTime(10))
	md := newMarketData(49500)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" {
		t.Errorf("保护期内(-1%%): 期望 hold, 实际=%s (原因: %s)", result.Action, result.Reason)
	}
}

func TestMinHoldTime_WithinProtection_ExtremeLoss_Close(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 48250, -3.5, pastTime(10))
	md := newMarketData(48250)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("保护期内极端亏损(-3.5%%): 期望 close, 实际=%s", result.Action)
	}
}

func TestMinHoldTime_WithinProtection_ExactlyMinus3_Hold(t *testing.T) {
	plan := newLongPlan(50000, 47000, 55000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 48500, -3.0, pastTime(10))
	md := newMarketData(48500)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" {
		t.Errorf("保护期内(-3.0%%边界): 期望 hold, 实际=%s (原因: %s)", result.Action, result.Reason)
	}
}

func TestMinHoldTime_CustomMinutes(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 60
	pos := newLongPosition(50000, 49800, -0.4, pastTime(30))
	md := newMarketData(49800)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" {
		t.Errorf("自定义60分钟保护期内(30分钟): 期望 hold, 实际=%s", result.Action)
	}
}

func TestMinHoldTime_AfterProtection_AllowsClose(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	plan.PeakPnLPercent = 10.0
	pos := newLongPosition(50000, 52000, 4.0, pastTime(60))
	pos.UnrealizedPnLPct = 4.0
	md := newMarketData(52000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("超过保护期后利润保护应可触发, 实际=%s", result.Action)
	}
}

// ============================================================================
// 需求 4.5: 利润保护触发
// ============================================================================

func TestProfitProtection_Triggered(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.PeakPnLPercent = 10.0
	pos := newLongPosition(50000, 52000, 4.0, pastTime(60))
	pos.UnrealizedPnLPct = 4.0
	md := newMarketData(52000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("利润保护: 峰值10%%, 当前4%% < 5%%, 期望 close, 实际=%s", result.Action)
	}
}

func TestProfitProtection_NotTriggered_PeakBelowThreshold(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.PeakPnLPercent = 6.0
	pos := newLongPosition(50000, 51000, 2.0, pastTime(60))
	pos.UnrealizedPnLPct = 2.0
	md := newMarketData(51000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" && !result.ShouldUpdatePeak {
		t.Errorf("利润保护: 峰值6%% < 8%%, 不应触发, 实际=%s", result.Action)
	}
}

func TestProfitProtection_Short_Triggered(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	plan.PeakPnLPercent = 10.0
	pos := newShortPosition(50000, 48000, 4.0, pastTime(60))
	pos.UnrealizedPnLPct = 4.0
	md := newMarketData(48000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头利润保护: 峰值10%%, 当前4%% < 5%%, 期望 close, 实际=%s", result.Action)
	}
}

func TestEvaluateProfitProtection_HighPeakUsesSixtyFivePercentLine(t *testing.T) {
	plan := newLongPlan(50000, 45000, 60000)
	plan.PeakPnLPercent = 12.0
	pos := newLongPosition(50000, 51000, 7.7, pastTime(90))
	md := newMarketData(51000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "close" {
		t.Fatalf("峰值12%%时保护线应至少65%%，当前7.7%%低于7.8%%应平仓: action=%s reason=%s", result.Action, result.Reason)
	}
}

// ============================================================================
// 需求 4.8: 移动止损单调性
// ============================================================================

func TestEvaluateTrailingStop_BreakevenAtSixPercent(t *testing.T) {
	plan := newLongPlan(50000, 40000, 65000)
	plan.CurrentStopLoss = 40000
	pos := newLongPosition(50000, 53000, 6.0, pastTime(90))
	md := newMarketData(53000)
	md.CurrentADX = 25
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "update_stop_loss" {
		t.Fatalf("浮盈6%%应允许移动到保本/小幅盈利止损: action=%s reason=%s", result.Action, result.Reason)
	}
	if result.NewStopLoss <= plan.EntryPrice {
		t.Fatalf("6%%保本止损应高于入场价: newSL=%.4f entry=%.4f", result.NewStopLoss, plan.EntryPrice)
	}
}

func TestTrailingStop_Long_Monotonic_OnlyRises(t *testing.T) {
	plan := newLongPlan(50000, 49000, 60000)
	plan.CurrentStopLoss = 49000
	pos := newLongPosition(50000, 60000, 20.0, pastTime(60))
	pos.UnrealizedPnLPct = 20.0
	md := newMarketData(60000)
	md.CurrentADX = 35.0
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "update_stop_loss" {
		if result.NewStopLoss <= plan.CurrentStopLoss {
			t.Errorf("多头移动止损单调性: 新止损%.4f 应 > 当前止损%.4f", result.NewStopLoss, plan.CurrentStopLoss)
		}
		if result.NewStopLoss >= md.CurrentPrice {
			t.Errorf("多头移动止损: 新止损%.4f 不应 >= 当前价%.4f", result.NewStopLoss, md.CurrentPrice)
		}
	}
}

func TestTrailingStop_Short_Monotonic_OnlyFalls(t *testing.T) {
	plan := newShortPlan(50000, 51000, 40000)
	plan.CurrentStopLoss = 51000
	pos := newShortPosition(50000, 40000, 20.0, pastTime(60))
	pos.UnrealizedPnLPct = 20.0
	md := newMarketData(40000)
	md.CurrentADX = 35.0
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "update_stop_loss" {
		if result.NewStopLoss >= plan.CurrentStopLoss {
			t.Errorf("空头移动止损单调性: 新止损%.4f 应 < 当前止损%.4f", result.NewStopLoss, plan.CurrentStopLoss)
		}
		if result.NewStopLoss <= md.CurrentPrice {
			t.Errorf("空头移动止损: 新止损%.4f 不应 <= 当前价%.4f", result.NewStopLoss, md.CurrentPrice)
		}
	}
}

func TestTrailingStop_Long_NoDowngrade(t *testing.T) {
	plan := newLongPlan(50000, 49000, 60000)
	plan.CurrentStopLoss = 50500
	pos := newLongPosition(50000, 50600, 1.2, pastTime(60))
	pos.UnrealizedPnLPct = 1.2
	md := newMarketData(50600)
	md.CurrentADX = 25.0
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "update_stop_loss" && result.NewStopLoss < plan.CurrentStopLoss {
		t.Errorf("多头移动止损不应下降: 新%.4f < 当前%.4f", result.NewStopLoss, plan.CurrentStopLoss)
	}
}

func TestTrailingStop_Short_NoUpgrade(t *testing.T) {
	plan := newShortPlan(50000, 51000, 40000)
	plan.CurrentStopLoss = 49500
	pos := newShortPosition(50000, 49400, 1.2, pastTime(60))
	pos.UnrealizedPnLPct = 1.2
	md := newMarketData(49400)
	md.CurrentADX = 25.0
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "update_stop_loss" && result.NewStopLoss > plan.CurrentStopLoss {
		t.Errorf("空头移动止损不应上升: 新%.4f > 当前%.4f", result.NewStopLoss, plan.CurrentStopLoss)
	}
}

func TestTrailingStop_Long_LargeATRAllowsBreakeven(t *testing.T) {
	plan := newLongPlan(603.89, 545.46, 910.655)
	plan.CurrentStopLoss = 577.6480637059689
	pos := newLongPosition(603.89, 612.7, 7.1, pastTime(120))
	md := newMarketData(612.7)
	md.CurrentADX = 25
	md.LongerTermContext.ATR14 = 23.37129086268741
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "ZECUSDT"}

	result := e.evaluateTrailingStop()
	if result == nil || result.Action != "update_stop_loss" {
		t.Fatalf("大ATR不应阻止保本止损上移: result=%+v", result)
	}

	expected := plan.EntryPrice * 1.002
	if math.Abs(result.NewStopLoss-expected) > 0.0001 {
		t.Fatalf("应移动到保本附近 %.4f，实际 %.4f", expected, result.NewStopLoss)
	}
	if result.NewStopLoss <= plan.CurrentStopLoss {
		t.Fatalf("多头止损应上移: new=%.4f current=%.4f", result.NewStopLoss, plan.CurrentStopLoss)
	}
}

func TestTrailingStop_Long_DoesNotLowerWhenGuardBelowCurrentStop(t *testing.T) {
	plan := newLongPlan(603.89, 545.46, 910.655)
	plan.CurrentStopLoss = 606.5
	pos := newLongPosition(603.89, 612.7, 7.1, pastTime(120))
	md := newMarketData(612.7)
	md.CurrentADX = 25
	md.LongerTermContext.ATR14 = 23.37129086268741
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "ZECUSDT"}

	if result := e.evaluateTrailingStop(); result != nil {
		t.Fatalf("多头已有更高止损时不应下调: %+v", result)
	}
}

func TestTrailingStop_Long_TriggerGuardAdjustsOnlyTooCloseTarget(t *testing.T) {
	plan := newLongPlan(100, 90, 150)
	plan.CurrentStopLoss = 105
	pos := newLongPosition(100, 113, 20, pastTime(120))
	md := newMarketData(113)
	md.CurrentADX = 35
	md.LongerTermContext.ATR14 = 20
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "TESTUSDT"}

	result := e.evaluateTrailingStop()
	if result == nil || result.Action != "update_stop_loss" {
		t.Fatalf("过近目标应按触发保护线调整后更新: result=%+v", result)
	}

	triggerGuard := math.Max(113*0.005, math.Min(20*0.25, 113*0.012))
	expected := 113 - triggerGuard
	if math.Abs(result.NewStopLoss-expected) > 0.0001 {
		t.Fatalf("应按triggerGuard调整到 %.4f，实际 %.4f", expected, result.NewStopLoss)
	}
}

func TestTrailingStop_Short_BreakevenAllowsStopLower(t *testing.T) {
	plan := newShortPlan(100, 110, 80)
	plan.CurrentStopLoss = 110
	pos := newShortPosition(100, 96, 7, pastTime(120))
	md := newMarketData(96)
	md.CurrentADX = 25
	md.LongerTermContext.ATR14 = 8
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "TESTUSDT"}

	result := e.evaluateTrailingStop()
	if result == nil || result.Action != "update_stop_loss" {
		t.Fatalf("空头达到保本条件应允许止损下移: result=%+v", result)
	}

	expected := plan.EntryPrice * 0.998
	if math.Abs(result.NewStopLoss-expected) > 0.0001 {
		t.Fatalf("空头保本止损应为 %.4f，实际 %.4f", expected, result.NewStopLoss)
	}
	if result.NewStopLoss >= plan.CurrentStopLoss || result.NewStopLoss <= md.CurrentPrice {
		t.Fatalf("空头止损必须下移且高于当前价: new=%.4f currentSL=%.4f price=%.4f", result.NewStopLoss, plan.CurrentStopLoss, md.CurrentPrice)
	}
}

func TestTrailingStop_Short_DoesNotRaiseStop(t *testing.T) {
	plan := newShortPlan(100, 110, 80)
	plan.CurrentStopLoss = 98
	pos := newShortPosition(100, 96, 7, pastTime(120))
	md := newMarketData(96)
	md.CurrentADX = 25
	md.LongerTermContext.ATR14 = 8
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "TESTUSDT"}

	if result := e.evaluateTrailingStop(); result != nil {
		t.Fatalf("空头已有更低止损时不应上调: %+v", result)
	}
}

func TestTrailingStop_DistancesSplitTriggerGuardAndTrendTolerance(t *testing.T) {
	e := &PositionEvaluator{}
	cfg := defaultTrailingConfig
	atr := 23.37129086268741
	currentPrice := 612.7

	triggerGuard := e.calculateTriggerGuardDistance(cfg, atr, currentPrice)
	trendTolerance := e.calculateTrendToleranceDistance(cfg, atr, currentPrice)

	expectedGuard := math.Max(currentPrice*0.005, math.Min(atr*0.25, currentPrice*0.012))
	expectedTolerance := math.Max(atr*1.2, currentPrice*0.012)
	if math.Abs(triggerGuard-expectedGuard) > 0.0001 {
		t.Fatalf("triggerGuard=%.4f want %.4f", triggerGuard, expectedGuard)
	}
	if math.Abs(trendTolerance-expectedTolerance) > 0.0001 {
		t.Fatalf("trendTolerance=%.4f want %.4f", trendTolerance, expectedTolerance)
	}
	if triggerGuard >= trendTolerance {
		t.Fatalf("触发保护距离应小于趋势容忍距离: guard=%.4f tolerance=%.4f", triggerGuard, trendTolerance)
	}
}

func TestEvaluateSoftStop_AfterProtectionAtMinusFive(t *testing.T) {
	plan := newLongPlan(50000, 44000, 62000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 47500, -5.0, pastTime(60))
	md := newMarketData(47500)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "close" || result.IsHardStop {
		t.Fatalf("保护期后-5%%应触发软止损平仓: action=%s hard=%v reason=%s", result.Action, result.IsHardStop, result.Reason)
	}
}

func TestEvaluateSoftStop_NoMomentumAfterSixtyMinutes(t *testing.T) {
	plan := newLongPlan(50000, 45000, 62000)
	plan.MinHoldMinutes = 30
	plan.PeakPnLPercent = 2.0
	pos := newLongPosition(50000, 48400, -3.2, pastTime(70))
	md := newMarketData(48400)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "close" || result.IsHardStop {
		t.Fatalf("持仓超过60分钟且MFE不足、当前亏损超过3%%应软止损: action=%s hard=%v reason=%s", result.Action, result.IsHardStop, result.Reason)
	}
}

func TestEvaluateSoftStop_LostBreakevenWithWeakMomentum(t *testing.T) {
	plan := newLongPlan(50000, 45000, 62000)
	plan.MinHoldMinutes = 30
	plan.PeakPnLPercent = 4.0
	pos := newLongPosition(50000, 49900, -0.2, pastTime(90))
	md := newMarketData(49900)
	md.PriceChange1h = -0.8
	md.MidTermSeries15m = &market.MidTermData15m{
		EMA20Values: []float64{49800},
		EMA50Values: []float64{50100},
		MACDHist:    []float64{-1},
	}
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "close" {
		t.Fatalf("曾盈利后跌回开仓价以下且动量转弱应平仓: action=%s reason=%s", result.Action, result.Reason)
	}
}

func TestEvaluateAdaptiveScaledExit_SkipsSmallPosition(t *testing.T) {
	plan := newLongPlan(100, 90, 200)
	plan.PositionSizeUSD = 30
	pos := newLongPosition(100, 131, 31.0, pastTime(90))
	md := newMarketData(131)
	md.CurrentADX = 30
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action == "partial_close" {
		t.Fatalf("小仓位不应触发注定失败的分批止盈: %+v", result)
	}
}

func TestEvaluateAdaptiveScaledExit_AllowsExecutablePosition(t *testing.T) {
	plan := newLongPlan(100, 90, 200)
	plan.PositionSizeUSD = 120
	pos := newLongPosition(100, 131, 31.0, pastTime(90))
	md := newMarketData(131)
	md.CurrentADX = 30
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}

	result := e.Evaluate()
	if result.Action != "partial_close" {
		t.Fatalf("可执行仓位达到RR档位时应允许分批止盈: action=%s reason=%s", result.Action, result.Reason)
	}
}

func TestEvaluateAdaptiveScaledExit_UsesBinanceSymbolCalibration(t *testing.T) {
	plan := newLongPlan(100, 90, 200)
	plan.PositionSizeUSD = 120
	pos := newLongPosition(100, 131, 31.0, pastTime(90))
	md := newMarketData(131)
	md.CurrentADX = 30
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT", Exchange: "binance"}

	result := e.Evaluate()
	if result.Action == "partial_close" {
		t.Fatalf("Binance BTCUSDT 20%%分批名义额低于50USDT时不应输出partial_close: %+v", result)
	}
}

// ============================================================================
// 需求 4.10: 计划失效条件检查时机
// ============================================================================

func TestPlanInvalidation_Before60Min_NotChecked(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 50200, 0.4, pastTime(30))
	md := newMarketData(50200)
	md.LongerTermContext.EMA20 = 49000
	md.LongerTermContext.EMA50 = 50000
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.IsPlanInvalidated {
		t.Errorf("持仓30分钟: 不应检查失效条件, 实际 IsPlanInvalidated=true (原因: %s)", result.Reason)
	}
}

func TestPlanInvalidation_After60Min_EMADeathCross_Long(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 50200, 0.4, pastTime(90))
	md := newMarketData(50200)
	md.LongerTermContext.EMA20 = 49000
	md.LongerTermContext.EMA50 = 50000
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("持仓90分钟+EMA死叉: 期望 close, 实际=%s (原因: %s)", result.Action, result.Reason)
	}
	if !result.IsPlanInvalidated {
		t.Error("持仓90分钟+EMA死叉: 期望 IsPlanInvalidated=true")
	}
}

func TestPlanInvalidation_After60Min_EMAGoldenCross_Short(t *testing.T) {
	plan := newShortPlan(50000, 51000, 45000)
	pos := newShortPosition(50000, 49800, 0.4, pastTime(90))
	md := newMarketData(49800)
	md.LongerTermContext.EMA20 = 51000
	md.LongerTermContext.EMA50 = 50000
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("空头持仓90分钟+EMA金叉: 期望 close, 实际=%s", result.Action)
	}
	if !result.IsPlanInvalidated {
		t.Error("空头持仓90分钟+EMA金叉: 期望 IsPlanInvalidated=true")
	}
}

func TestPlanInvalidation_Exactly60Min_IsChecked(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 50200, 0.4, pastTime(60))
	md := newMarketData(50200)
	md.LongerTermContext.EMA20 = 49000
	md.LongerTermContext.EMA50 = 50000
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("持仓60分钟+EMA死叉: 期望 close, 实际=%s", result.Action)
	}
}

// ============================================================================
// 需求 4.1: 优先级链顺序验证
// ============================================================================

func TestPriority_StopLoss_BeforeMinHoldTime(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 48999, -2.0, pastTime(10))
	md := newMarketData(48999)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" || !result.IsHardStop {
		t.Errorf("优先级: 止损应先于最小持仓时间保护, action=%s, IsHardStop=%v", result.Action, result.IsHardStop)
	}
}

func TestPriority_TakeProfit_BeforeMinHoldTime(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	pos := newLongPosition(50000, 55001, 10.0, pastTime(10))
	md := newMarketData(55001)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action != "close" {
		t.Errorf("优先级: 止盈应先于最小持仓时间保护, action=%s", result.Action)
	}
}

func TestPriority_MinHoldTime_BeforeProfitProtection(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	plan.MinHoldMinutes = 30
	plan.PeakPnLPercent = 10.0
	pos := newLongPosition(50000, 52000, 4.0, pastTime(10))
	pos.UnrealizedPnLPct = 4.0
	md := newMarketData(52000)
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.Action == "close" {
		t.Errorf("优先级: 保护期内不应触发利润保护平仓, action=%s, reason=%s", result.Action, result.Reason)
	}
}

func TestPriority_PlanInvalidation_After60Min_NotBefore(t *testing.T) {
	plan := newLongPlan(50000, 49000, 55000)
	pos := newLongPosition(50000, 50200, 0.4, pastTime(30))
	md := newMarketData(50200)
	md.LongerTermContext.EMA20 = 49000
	md.LongerTermContext.EMA50 = 50000
	e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result.IsPlanInvalidated {
		t.Error("优先级: 30分钟内不应触发计划失效检查")
	}
}

func TestPriority_NilPosition_ReturnsHold(t *testing.T) {
	e := &PositionEvaluator{Position: nil, Plan: nil, MarketData: nil, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result == nil {
		t.Fatal("Evaluate() 不应返回 nil")
	}
	if result.Action != "hold" {
		t.Errorf("nil 持仓: 期望 hold, 实际=%s", result.Action)
	}
}

func TestPriority_NilMarketData_ReturnsHold(t *testing.T) {
	pos := newLongPosition(50000, 50000, 0, pastTime(60))
	e := &PositionEvaluator{Position: pos, Plan: nil, MarketData: nil, Symbol: "BTCUSDT"}
	result := e.Evaluate()
	if result == nil {
		t.Fatal("Evaluate() 不应返回 nil")
	}
	if result.Action != "hold" {
		t.Errorf("nil 市场数据: 期望 hold, 实际=%s", result.Action)
	}
}

func TestEvaluate_NeverReturnsNil(t *testing.T) {
	cases := []struct {
		name string
		e    *PositionEvaluator
	}{
		{"nil_all", &PositionEvaluator{}},
		{"nil_plan", &PositionEvaluator{
			Position:   newLongPosition(50000, 50000, 0, pastTime(60)),
			MarketData: newMarketData(50000),
		}},
		{"valid_long", &PositionEvaluator{
			Position:   newLongPosition(50000, 50000, 0, pastTime(60)),
			Plan:       newLongPlan(50000, 49000, 55000),
			MarketData: newMarketData(50000),
			Symbol:     "BTCUSDT",
		}},
		{"valid_short", &PositionEvaluator{
			Position:   newShortPosition(50000, 50000, 0, pastTime(60)),
			Plan:       newShortPlan(50000, 51000, 45000),
			MarketData: newMarketData(50000),
			Symbol:     "BTCUSDT",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.e.Evaluate()
			if result == nil {
				t.Errorf("%s: Evaluate() 不应返回 nil", tc.name)
			}
		})
	}
}

// ============================================================================
// 需求 4.2: 止损触发立即平仓 - 属性基测试 (Property 15)
// Feature: quant-trading-system, Property 15: 止损触发立即平仓
// Validates: Requirements 4.2
// ============================================================================

func TestProperty15_StopLossTriggerClose(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 多头: 当前价格 <= 止损价 → action="close"
	properties.Property("多头止损触发立即平仓", prop.ForAll(
		func(entryNorm, slOffsetNorm, priceOffsetNorm float64) bool {
			entry := 10000.0 + entryNorm*90000.0
			sl := entry * (1.0 - 0.001 - slOffsetNorm*0.099)
			currentPrice := sl * (1.0 - priceOffsetNorm*0.05)

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       entry * 1.1,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: (currentPrice - entry) / entry * 100,
				UpdateTime:       pastTime(60),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 空头: 当前价格 >= 止损价 → action="close"
	properties.Property("空头止损触发立即平仓", prop.ForAll(
		func(entryNorm, slOffsetNorm, priceOffsetNorm float64) bool {
			entry := 10000.0 + entryNorm*90000.0
			sl := entry * (1.0 + 0.001 + slOffsetNorm*0.099)
			currentPrice := sl * (1.0 + priceOffsetNorm*0.05)

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "short",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       entry * 0.9,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "SELL",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: (entry - currentPrice) / entry * 100,
				UpdateTime:       pastTime(60),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 4.4: 最小持仓时间保护 - 属性基测试 (Property 17)
// Feature: quant-trading-system, Property 17: 最小持仓时间保护
// Validates: Requirements 4.4
// ============================================================================

func TestProperty17_MinHoldTimeProtection(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 保护期内且盈亏 > -3% → hold（不平仓）
	properties.Property("保护期内盈亏大于-3%应持有", prop.ForAll(
		func(minHoldNorm float64, elapsedNorm float64, pnlNorm float64) bool {
			// minHoldMinutes: 10~120
			minHold := 10 + int(minHoldNorm*110)
			// elapsed: 0 ~ minHold-1 分钟（保护期内）
			maxElapsed := minHold - 1
			if maxElapsed < 1 {
				maxElapsed = 1
			}
			elapsedMin := 1 + int(elapsedNorm*float64(maxElapsed-1))
			// pnlPct: -3.0 ~ +20.0（不触发极端亏损）
			pnlPct := -3.0 + pnlNorm*23.0

			entry := 50000.0
			sl := entry * 0.90
			tp := entry * 1.15

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       tp,
				MinHoldMinutes:   minHold,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			// 当前价格不触发止损/止盈
			currentPrice := entry * (1.0 + pnlPct/100.0)
			if currentPrice <= sl {
				currentPrice = sl + 1
			}
			if currentPrice >= tp {
				currentPrice = tp - 1
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: pnlPct,
				UpdateTime:       pastTime(elapsedMin),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			// 保护期内且盈亏 > -3%，不应平仓
			return result.Action != "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1), // maps to pnlPct in [-3.0, +20.0]
	))

	// 保护期内且盈亏 < -3% → close（极端亏损紧急平仓）
	properties.Property("保护期内盈亏小于-3%应平仓", prop.ForAll(
		func(minHoldNorm float64, elapsedNorm float64, extraLossNorm float64) bool {
			// minHoldMinutes: 10~120
			minHold := 10 + int(minHoldNorm*110)
			// elapsed: 0 ~ minHold-1 分钟（保护期内）
			maxElapsed := minHold - 1
			if maxElapsed < 1 {
				maxElapsed = 1
			}
			elapsedMin := 1 + int(elapsedNorm*float64(maxElapsed-1))
			// pnlPct: -3.01 ~ -20.0（严格小于 -3%）
			pnlPct := -3.01 - extraLossNorm*16.99

			entry := 50000.0
			// 止损设得足够低，不会先触发止损
			sl := entry * (1.0 + pnlPct/100.0*1.5)
			if sl <= 0 {
				sl = entry * 0.5
			}
			tp := entry * 1.15

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       tp,
				MinHoldMinutes:   minHold,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			currentPrice := entry * (1.0 + pnlPct/100.0)
			if currentPrice <= 0 {
				currentPrice = 1.0
			}
			// 确保当前价格高于止损（避免被止损逻辑先捕获）
			if currentPrice <= sl {
				currentPrice = sl + 1
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: pnlPct,
				UpdateTime:       pastTime(elapsedMin),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 4.3: 止盈触发立即平仓 - 属性基测试 (Property 16)
// Feature: quant-trading-system, Property 16: 止盈触发立即平仓
// Validates: Requirements 4.3
// ============================================================================

func TestProperty16_TakeProfitTriggerClose(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 多头: 当前价格 >= 止盈价 → action="close"
	properties.Property("多头止盈触发立即平仓", prop.ForAll(
		func(entryNorm, tpOffsetNorm, priceOffsetNorm float64) bool {
			entry := 10000.0 + entryNorm*90000.0
			// 止盈价高于入场价 0.1%~10%
			tp := entry * (1.0 + 0.001 + tpOffsetNorm*0.099)
			// 当前价格 >= 止盈价
			currentPrice := tp * (1.0 + priceOffsetNorm*0.05)

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         entry * 0.95,
				CurrentStopLoss:  entry * 0.95,
				TakeProfit:       tp,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: (currentPrice - entry) / entry * 100,
				UpdateTime:       pastTime(60),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 空头: 当前价格 <= 止盈价 → action="close"
	properties.Property("空头止盈触发立即平仓", prop.ForAll(
		func(entryNorm, tpOffsetNorm, priceOffsetNorm float64) bool {
			entry := 10000.0 + entryNorm*90000.0
			// 止盈价低于入场价 0.1%~10%
			tp := entry * (1.0 - 0.001 - tpOffsetNorm*0.099)
			// 当前价格 <= 止盈价
			currentPrice := tp * (1.0 - priceOffsetNorm*0.05)

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "short",
				EntryPrice:       entry,
				StopLoss:         entry * 1.05,
				CurrentStopLoss:  entry * 1.05,
				TakeProfit:       tp,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "SELL",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: (entry - currentPrice) / entry * 100,
				UpdateTime:       pastTime(60),
			}
			md := newMarketData(currentPrice)
			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 4.5: 利润保护触发 - 属性基测试 (Property 18)
// Feature: quant-trading-system, Property 18: 利润保护触发
// Validates: Requirements 4.5
// ============================================================================

func TestProperty18_ProfitProtectionTrigger(t *testing.T) {
	// Feature: quant-trading-system, Property 18: 利润保护触发
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 峰值盈利 >= 8% 且当前盈利 < 峰值 × 50% → 平仓
	properties.Property("峰值盈利超过触发线且当前盈利回落至保护线以下应平仓", prop.ForAll(
		func(peakNorm float64, currentNorm float64, entryNorm float64) bool {
			// 入场价: 10000 ~ 100000
			entry := 10000.0 + entryNorm*90000.0

			// 峰值盈利: 8.0% ~ 30.0%（满足触发条件 >= 8%）
			peakPnLPct := 8.0 + peakNorm*22.0

			// 保护线 = peakPnLPct * 0.5
			protectLine := peakPnLPct * 0.5

			// 当前盈利严格小于保护线，范围 [-5%, protectLine - 0.01%]
			maxCurrent := protectLine - 0.01
			minCurrent := -5.0
			if minCurrent > maxCurrent {
				minCurrent = maxCurrent - 1.0
			}
			currentPnLPct := minCurrent + currentNorm*(maxCurrent-minCurrent)

			// 止损/止盈设置得足够远，不会先触发
			sl := entry * 0.80 // 止损 -20%
			tp := entry * 1.50 // 止盈 +50%

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       tp,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
				PeakPnLPercent:   peakPnLPct,
			}

			currentPrice := entry * (1.0 + currentPnLPct/100.0)
			if currentPrice <= 0 {
				currentPrice = 1.0
			}
			if currentPrice <= sl {
				currentPrice = sl + entry*0.001
			}
			if currentPrice >= tp {
				currentPrice = tp - entry*0.001
			}

			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: currentPnLPct,
				UpdateTime:       pastTime(60),
			}

			md := newMarketData(currentPrice)
			e := &PositionEvaluator{
				Position:   pos,
				Plan:       plan,
				MarketData: md,
				Symbol:     "BTCUSDT",
			}
			result := e.Evaluate()
			return result.Action == "close"
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 4.8: 移动止损单调性 - 属性基测试 (Property 19)
// Feature: quant-trading-system, Property 19: 移动止损单调性
// Validates: Requirements 4.8
// ============================================================================

func TestProperty19_TrailingStopMonotonicity(t *testing.T) {
	// Feature: quant-trading-system, Property 19: 移动止损单调性
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 多头: 当 evaluateTrailingStop 返回 update_stop_loss 时，新止损 >= 当前止损
	properties.Property("多头移动止损只升不降", prop.ForAll(
		func(entryNorm, pnlNorm, adxNorm, currentSLOffsetNorm float64) bool {
			// 入场价: 10000 ~ 100000
			entry := 10000.0 + entryNorm*90000.0

			// 盈利: 15% ~ 35%（足够触发移动止损档位）
			pnlPct := 15.0 + pnlNorm*20.0
			currentPrice := entry * (1.0 + pnlPct/100.0)

			// ADX: 20 ~ 60（足够强的趋势）
			adx := 20.0 + adxNorm*40.0

			// 当前止损: 入场价以上 0.1% ~ 10%（已经移动过的止损）
			currentSLOffset := 0.001 + currentSLOffsetNorm*0.099
			currentSL := entry * (1.0 + currentSLOffset)

			// 确保当前止损低于当前价格
			if currentSL >= currentPrice*0.99 {
				currentSL = currentPrice * 0.90
			}

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         entry * 0.95,
				CurrentStopLoss:  currentSL,
				TakeProfit:       entry * 1.50,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
				PeakPnLPercent:   pnlPct,
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: pnlPct,
				UpdateTime:       pastTime(120),
			}
			md := newMarketData(currentPrice)
			md.CurrentADX = adx

			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()

			// 如果触发了移动止损更新，新止损必须 >= 当前止损
			if result.Action == "update_stop_loss" {
				return result.NewStopLoss >= currentSL
			}
			// 未触发移动止损更新也是合法的（可能触发了其他动作）
			return true
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 空头: 当 evaluateTrailingStop 返回 update_stop_loss 时，新止损 <= 当前止损
	properties.Property("空头移动止损只降不升", prop.ForAll(
		func(entryNorm, pnlNorm, adxNorm, currentSLOffsetNorm float64) bool {
			// 入场价: 10000 ~ 100000
			entry := 10000.0 + entryNorm*90000.0

			// 盈利: 15% ~ 35%（足够触发移动止损档位）
			pnlPct := 15.0 + pnlNorm*20.0
			currentPrice := entry * (1.0 - pnlPct/100.0)
			if currentPrice <= 0 {
				currentPrice = entry * 0.01
			}

			// ADX: 20 ~ 60
			adx := 20.0 + adxNorm*40.0

			// 当前止损: 入场价以下 0.1% ~ 10%（已经移动过的止损）
			currentSLOffset := 0.001 + currentSLOffsetNorm*0.099
			currentSL := entry * (1.0 - currentSLOffset)

			// 确保当前止损高于当前价格
			if currentSL <= currentPrice*1.01 {
				currentSL = currentPrice * 1.10
			}

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "short",
				EntryPrice:       entry,
				StopLoss:         entry * 1.05,
				CurrentStopLoss:  currentSL,
				TakeProfit:       entry * 0.50,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
				PeakPnLPercent:   pnlPct,
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "SELL",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: pnlPct,
				UpdateTime:       pastTime(120),
			}
			md := newMarketData(currentPrice)
			md.CurrentADX = adx

			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()

			// 如果触发了移动止损更新，新止损必须 <= 当前止损
			if result.Action == "update_stop_loss" {
				return result.NewStopLoss <= currentSL
			}
			return true
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 需求 4.10: 计划失效条件检查时机 - 属性基测试 (Property 20)
// Feature: quant-trading-system, Property 20: 计划失效条件检查时机
// Validates: Requirements 4.10
// ============================================================================

func TestProperty20_PlanInvalidationTiming(t *testing.T) {
	// Feature: quant-trading-system, Property 20: 计划失效条件检查时机
	properties := gopter.NewProperties(gopter.DefaultTestParametersWithSeed(42))

	// 持仓 < 60 分钟时，即使失效条件已触发，也不应返回 IsPlanInvalidated=true
	properties.Property("持仓不足60分钟不检查失效条件", prop.ForAll(
		func(elapsedNorm float64, entryNorm float64) bool {
			// 持仓时间: 1 ~ 59 分钟（严格小于 60）
			elapsedMin := 1 + int(elapsedNorm*58.0)

			entry := 10000.0 + entryNorm*90000.0
			sl := entry * 0.90
			tp := entry * 1.15
			currentPrice := entry * 1.001 // 略高于入场价，不触发止损/止盈

			// 构造 EMA 死叉（多头失效条件已触发）
			md := newMarketData(currentPrice)
			md.LongerTermContext.EMA20 = entry * 0.97 // EMA20 < EMA50 → 死叉
			md.LongerTermContext.EMA50 = entry * 0.99

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       tp,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: 0.1,
				UpdateTime:       pastTime(elapsedMin),
			}

			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()

			// 持仓不足 60 分钟，失效条件不应被检查
			return !result.IsPlanInvalidated
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	// 持仓 >= 60 分钟且失效条件已触发 → 应返回 action="close" 且 IsPlanInvalidated=true
	properties.Property("持仓达到60分钟且失效条件触发应平仓", prop.ForAll(
		func(elapsedNorm float64, entryNorm float64) bool {
			// 持仓时间: 60 ~ 240 分钟（>= 60）
			elapsedMin := 60 + int(elapsedNorm*180.0)

			entry := 10000.0 + entryNorm*90000.0
			sl := entry * 0.90
			tp := entry * 1.15
			currentPrice := entry * 1.001 // 不触发止损/止盈

			// 构造 EMA 死叉（多头失效条件已触发）
			md := newMarketData(currentPrice)
			md.LongerTermContext.EMA20 = entry * 0.97 // EMA20 < EMA50 → 死叉
			md.LongerTermContext.EMA50 = entry * 0.99

			plan := &TradePlan{
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       entry,
				StopLoss:         sl,
				CurrentStopLoss:  sl,
				TakeProfit:       tp,
				MinHoldMinutes:   30,
				CreatedAt:        time.Now().Add(-2 * time.Hour),
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			pos := &PositionInfo{
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				EntryPrice:       entry,
				MarkPrice:        currentPrice,
				Quantity:         0.1,
				UnrealizedPnLPct: 0.1,
				UpdateTime:       pastTime(elapsedMin),
			}

			e := &PositionEvaluator{Position: pos, Plan: plan, MarketData: md, Symbol: "BTCUSDT"}
			result := e.Evaluate()

			// 持仓 >= 60 分钟且 EMA 死叉触发，应平仓
			return result.Action == "close" && result.IsPlanInvalidated
		},
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
