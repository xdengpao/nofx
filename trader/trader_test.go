package trader

// Feature: quant-trading-system
// 任务 17.1: 交易接口测试覆盖
// 覆盖需求: 2.1, 2.5, 2.7, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6

import (
	"fmt"
	"math"
	"nofx/decision"
	"nofx/logger"
	"nofx/market"
	"nofx/pool"
	"strconv"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 需求 2.5: calculatePrecision / trimTrailingZeros (FormatQuantity 核心逻辑)
// ============================================================================

func TestCalculatePrecision_WholeNumber(t *testing.T) {
	if p := calculatePrecision("1"); p != 0 {
		t.Errorf("stepSize=1: 期望精度0, 实际=%d", p)
	}
}

func TestCalculatePrecision_OneDecimal(t *testing.T) {
	if p := calculatePrecision("0.1"); p != 1 {
		t.Errorf("stepSize=0.1: 期望精度1, 实际=%d", p)
	}
}

func TestCalculatePrecision_ThreeDecimals(t *testing.T) {
	if p := calculatePrecision("0.001"); p != 3 {
		t.Errorf("stepSize=0.001: 期望精度3, 实际=%d", p)
	}
}

func TestCalculatePrecision_TrailingZeros(t *testing.T) {
	if p := calculatePrecision("0.00100"); p != 3 {
		t.Errorf("stepSize=0.00100: 期望精度3, 实际=%d", p)
	}
}

func TestCalculatePrecision_EightDecimals(t *testing.T) {
	if p := calculatePrecision("0.00000001"); p != 8 {
		t.Errorf("stepSize=0.00000001: 期望精度8, 实际=%d", p)
	}
}

func TestCalculatePrecision_DotAtEnd(t *testing.T) {
	if p := calculatePrecision("1."); p != 0 {
		t.Errorf("stepSize=1.: 期望精度0, 实际=%d", p)
	}
}

func TestTrimTrailingZeros_NoDecimal(t *testing.T) {
	if s := trimTrailingZeros("100"); s != "100" {
		t.Errorf("trimTrailingZeros(100): 期望100, 实际=%s", s)
	}
}

func TestTrimTrailingZeros_WithTrailingZeros(t *testing.T) {
	if s := trimTrailingZeros("0.00100"); s != "0.001" {
		t.Errorf("trimTrailingZeros(0.00100): 期望0.001, 实际=%s", s)
	}
}

func TestTrimTrailingZeros_AllZerosAfterDot(t *testing.T) {
	if s := trimTrailingZeros("1.000"); s != "1" {
		t.Errorf("trimTrailingZeros(1.000): 期望1, 实际=%s", s)
	}
}

func TestTrimTrailingZeros_NoTrailingZeros(t *testing.T) {
	if s := trimTrailingZeros("0.123"); s != "0.123" {
		t.Errorf("trimTrailingZeros(0.123): 期望0.123, 实际=%s", s)
	}
}

// TestFormatQuantity_RoundTrip 验证格式化后解析回来误差不超过 stepSize
func TestFormatQuantity_RoundTrip_Precision3(t *testing.T) {
	precision := 3
	stepSize := 0.001
	quantities := []float64{1.23456789, 0.001, 100.0, 0.0005}

	for _, q := range quantities {
		formatted := fmt.Sprintf("%."+strconv.Itoa(precision)+"f", q)
		parsed, err := strconv.ParseFloat(formatted, 64)
		if err != nil {
			t.Errorf("解析格式化结果失败: %v", err)
			continue
		}
		diff := q - parsed
		if diff < 0 {
			diff = -diff
		}
		if diff > stepSize {
			t.Errorf("精度3往返误差过大: 原始=%.8f, 格式化=%s, 解析=%.8f, 误差=%.8f > stepSize=%.8f",
				q, formatted, parsed, diff, stepSize)
		}
	}
}

func TestFormatQuantity_RoundTrip_Precision0(t *testing.T) {
	precision := 0
	stepSize := 1.0
	quantities := []float64{1.9, 10.5, 100.0}

	for _, q := range quantities {
		formatted := fmt.Sprintf("%."+strconv.Itoa(precision)+"f", q)
		parsed, err := strconv.ParseFloat(formatted, 64)
		if err != nil {
			t.Errorf("解析格式化结果失败: %v", err)
			continue
		}
		diff := q - parsed
		if diff < 0 {
			diff = -diff
		}
		if diff > stepSize {
			t.Errorf("精度0往返误差过大: 原始=%.4f, 格式化=%s, 解析=%.4f, 误差=%.4f > stepSize=%.4f",
				q, formatted, parsed, diff, stepSize)
		}
	}
}

// ============================================================================
// 需求 8.1: sortDecisionsByPriority — 平仓优先于开仓
// ============================================================================

func TestSortDecisionsByPriority_CloseBeforeOpen(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "close_long"},
		{Symbol: "SOLUSDT", Action: "open_short"},
		{Symbol: "BNBUSDT", Action: "close_short"},
	}
	sorted := sortDecisionsByPriority(decisions)

	for i := 0; i < 2; i++ {
		if sorted[i].Action != "close_long" && sorted[i].Action != "close_short" {
			t.Errorf("位置[%d]: 期望平仓决策, 实际=%s", i, sorted[i].Action)
		}
	}
	for i := 2; i < 4; i++ {
		if sorted[i].Action != "open_long" && sorted[i].Action != "open_short" {
			t.Errorf("位置[%d]: 期望开仓决策, 实际=%s", i, sorted[i].Action)
		}
	}
}

func TestSortDecisionsByPriority_HoldLast(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "BTCUSDT", Action: "hold"},
		{Symbol: "ETHUSDT", Action: "open_long"},
		{Symbol: "SOLUSDT", Action: "close_short"},
	}
	sorted := sortDecisionsByPriority(decisions)

	last := sorted[len(sorted)-1]
	if last.Action != "hold" {
		t.Errorf("hold 应排在最后, 实际最后=%s", last.Action)
	}
}

func TestSortDecisionsByPriority_UpdateStopLoss_BeforeOpen(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "update_stop_loss"},
	}
	sorted := sortDecisionsByPriority(decisions)

	if sorted[0].Action != "update_stop_loss" {
		t.Errorf("update_stop_loss 应在 open_long 之前, 实际第一=%s", sorted[0].Action)
	}
}

func TestSortDecisionsByPriority_PartialClose_HighestPriority(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "update_stop_loss"},
		{Symbol: "SOLUSDT", Action: "partial_close"},
	}
	sorted := sortDecisionsByPriority(decisions)

	if sorted[0].Action != "partial_close" {
		t.Errorf("partial_close 应有最高优先级, 实际第一=%s", sorted[0].Action)
	}
}

func TestSortDecisionsByPriority_SingleDecision_Unchanged(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
	}
	sorted := sortDecisionsByPriority(decisions)
	if len(sorted) != 1 || sorted[0].Action != "open_long" {
		t.Error("单个决策排序后应不变")
	}
}

func TestSortDecisionsByPriority_EmptyList(t *testing.T) {
	sorted := sortDecisionsByPriority([]decision.Decision{})
	if len(sorted) != 0 {
		t.Error("空列表排序后应仍为空")
	}
}

func TestDeterminePartialClosePlan_Normal(t *testing.T) {
	plan := determinePartialClosePlan(10, 20, 10, 5, 10)
	if plan.Mode != partialCloseModeNormal {
		t.Fatalf("期望 normal，实际=%s", plan.Mode)
	}
	if math.Abs(plan.CloseValue-20) > 0.0001 {
		t.Fatalf("平仓名义额期望20，实际=%.4f", plan.CloseValue)
	}
	if math.Abs(plan.RemainingValue-80) > 0.0001 {
		t.Fatalf("剩余名义额期望80，实际=%.4f", plan.RemainingValue)
	}
}

func TestDeterminePartialClosePlan_RemainingTooSmall_FullClose(t *testing.T) {
	plan := determinePartialClosePlan(10, 95, 10, 5, 10)
	if plan.Mode != partialCloseModeFull {
		t.Fatalf("剩余仓位过小时应自动全平，实际=%s", plan.Mode)
	}
	if plan.RemainingQuantity != 0 || plan.CloseQuantity != 10 {
		t.Fatalf("全平数量错误: close=%.4f remaining=%.4f", plan.CloseQuantity, plan.RemainingQuantity)
	}
}

func TestDeterminePartialClosePlan_CloseValueTooSmall_Skip(t *testing.T) {
	plan := determinePartialClosePlan(10, 4, 10, 5, 10)
	if plan.Mode != partialCloseModeSkip {
		t.Fatalf("平仓名义额过小时应跳过下单，实际=%s", plan.Mode)
	}
	if math.Abs(plan.CloseValue-4) > 0.0001 {
		t.Fatalf("平仓名义额期望4，实际=%.4f", plan.CloseValue)
	}
}

func TestDeterminePartialClosePlan_SmallPositionTwentyPct_FullClose(t *testing.T) {
	plan := determinePartialClosePlan(2.4, 20, 10, 5, 10)
	if plan.Mode != partialCloseModeFull {
		t.Fatalf("小仓位20%%分批应自动全平，实际=%s", plan.Mode)
	}
	if math.Abs(plan.CloseValue-24) > 0.0001 || plan.RemainingValue != 0 {
		t.Fatalf("全平名义额错误: close=%.4f remaining=%.4f", plan.CloseValue, plan.RemainingValue)
	}
}

func TestCalibratedMinOrderValue_BinanceBTC(t *testing.T) {
	if got := calibratedOpenMinOrderValueUSDT("binance", "BTCUSDT"); got != 50 {
		t.Fatalf("Binance BTCUSDT开仓最小名义额应按外部校准提高到50，实际=%.2f", got)
	}
	if got := calibratedPartialCloseMinValueUSDT("binance", "ETHUSDT"); got != 20 {
		t.Fatalf("Binance ETHUSDT部分平仓最小名义额应按外部校准提高到20，实际=%.2f", got)
	}
	if got := calibratedOpenMinOrderValueUSDT("aster", "BTCUSDT"); got != 10 {
		t.Fatalf("Aster开仓预检应保留系统更保守10USDT，实际=%.2f", got)
	}
}

func TestResolveProtectiveStopLoss_UsesRequestedWhenPresent(t *testing.T) {
	got := resolveProtectiveStopLoss(101.5, 98.0)
	if math.Abs(got-101.5) > 0.0001 {
		t.Fatalf("应优先使用 partial_close 给出的新止损，实际=%.4f", got)
	}
}

func TestResolveProtectiveStopLoss_FallsBackToCurrentPlanStop(t *testing.T) {
	got := resolveProtectiveStopLoss(0, 98.0)
	if math.Abs(got-98.0) > 0.0001 {
		t.Fatalf("缺失 new_stop_loss 时应回退到当前有效止损，实际=%.4f", got)
	}
}

func TestSortDecisionsByPriority_FullPriorityOrder(t *testing.T) {
	decisions := []decision.Decision{
		{Symbol: "A", Action: "wait"},
		{Symbol: "B", Action: "open_short"},
		{Symbol: "C", Action: "update_take_profit"},
		{Symbol: "D", Action: "close_long"},
		{Symbol: "E", Action: "hold"},
		{Symbol: "F", Action: "open_long"},
		{Symbol: "G", Action: "close_short"},
	}
	sorted := sortDecisionsByPriority(decisions)

	// 统计各类数量
	closeCount := 0
	holdWaitCount := 0
	for _, d := range sorted {
		switch d.Action {
		case "close_long", "close_short", "partial_close":
			closeCount++
		case "hold", "wait":
			holdWaitCount++
		}
	}

	// 前 closeCount 个应全为平仓类
	for i := 0; i < closeCount; i++ {
		a := sorted[i].Action
		if a != "close_long" && a != "close_short" && a != "partial_close" {
			t.Errorf("位置[%d] 应为平仓类, 实际=%s", i, a)
		}
	}

	// 最后 holdWaitCount 个应全为 hold/wait
	n := len(sorted)
	for i := n - holdWaitCount; i < n; i++ {
		a := sorted[i].Action
		if a != "hold" && a != "wait" {
			t.Errorf("位置[%d] 应为 hold/wait, 实际=%s", i, a)
		}
	}
}

// ============================================================================
// Feature: quant-trading-system, Property 33: 决策执行排序
// 需求 8.1: 对任意包含开仓和平仓决策的列表，排序后所有平仓决策应在开仓决策之前
// ============================================================================

func TestProperty33_SortDecisionsByPriority_CloseBeforeOpen(t *testing.T) {
	// Feature: quant-trading-system, Property 33: 决策执行排序
	properties := gopter.NewProperties(gopter.DefaultTestParameters())

	openActions := []string{"open_long", "open_short"}
	closeActions := []string{"close_long", "close_short", "partial_close"}
	allActions := []string{"open_long", "open_short", "close_long", "close_short", "partial_close", "hold", "wait", "update_stop_loss", "update_take_profit"}

	properties.Property("排序后所有平仓决策在开仓决策之前", prop.ForAll(
		func(actionIndices []uint8) bool {
			if len(actionIndices) == 0 {
				return true
			}

			// 构造决策列表
			decisions := make([]decision.Decision, len(actionIndices))
			for i, idx := range actionIndices {
				decisions[i] = decision.Decision{
					Symbol: fmt.Sprintf("COIN%dUSDT", i),
					Action: allActions[int(idx)%len(allActions)],
				}
			}

			sorted := sortDecisionsByPriority(decisions)

			// 验证：找到第一个开仓决策的位置，其后不应有平仓决策
			firstOpenIdx := -1
			for i, d := range sorted {
				for _, oa := range openActions {
					if d.Action == oa {
						firstOpenIdx = i
						break
					}
				}
				if firstOpenIdx >= 0 {
					break
				}
			}

			if firstOpenIdx == -1 {
				// 没有开仓决策，无需验证
				return true
			}

			// 开仓决策之后不应有平仓决策
			for i := firstOpenIdx + 1; i < len(sorted); i++ {
				for _, ca := range closeActions {
					if sorted[i].Action == ca {
						return false
					}
				}
			}
			return true
		},
		gen.SliceOf(gen.UInt8()),
	))

	properties.Property("排序后列表长度不变", prop.ForAll(
		func(actionIndices []uint8) bool {
			decisions := make([]decision.Decision, len(actionIndices))
			for i, idx := range actionIndices {
				decisions[i] = decision.Decision{
					Symbol: fmt.Sprintf("COIN%dUSDT", i),
					Action: allActions[int(idx)%len(allActions)],
				}
			}
			sorted := sortDecisionsByPriority(decisions)
			return len(sorted) == len(decisions)
		},
		gen.SliceOf(gen.UInt8()),
	))

	properties.Property("排序后hold/wait在开仓决策之后", prop.ForAll(
		func(actionIndices []uint8) bool {
			if len(actionIndices) == 0 {
				return true
			}
			decisions := make([]decision.Decision, len(actionIndices))
			for i, idx := range actionIndices {
				decisions[i] = decision.Decision{
					Symbol: fmt.Sprintf("COIN%dUSDT", i),
					Action: allActions[int(idx)%len(allActions)],
				}
			}
			sorted := sortDecisionsByPriority(decisions)

			// 找到最后一个开仓决策的位置
			lastOpenIdx := -1
			for i, d := range sorted {
				if d.Action == "open_long" || d.Action == "open_short" {
					lastOpenIdx = i
				}
			}

			if lastOpenIdx == -1 {
				return true
			}

			// 开仓决策之前不应有 hold/wait
			for i := 0; i < lastOpenIdx; i++ {
				if sorted[i].Action == "hold" || sorted[i].Action == "wait" {
					return false
				}
			}
			return true
		},
		gen.SliceOf(gen.UInt8()),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Mock Trader 接口 — 验证 Trader 接口可被 mock 实现
// ============================================================================

// mockTrader 实现 Trader 接口，用于单元测试
type mockTrader struct {
	openLongCalled   bool
	openShortCalled  bool
	closeLongCalled  bool
	closeShortCalled bool
	lastSymbol       string
	lastQuantity     float64
	lastLeverage     int
	shouldError      bool
	stopLossError    bool
	takeProfitError  bool
	stopLossCalls    int
	takeProfitCalls  int
}

func (m *mockTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{
		"totalWalletBalance":    10000.0,
		"totalUnrealizedProfit": 0.0,
		"availableBalance":      8000.0,
	}, nil
}

func (m *mockTrader) GetPositions() ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

func (m *mockTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	m.openLongCalled = true
	m.lastSymbol = symbol
	m.lastQuantity = quantity
	m.lastLeverage = leverage
	if m.shouldError {
		return nil, fmt.Errorf("mock 开多仓失败")
	}
	return map[string]interface{}{"orderId": int64(12345)}, nil
}

func (m *mockTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	m.openShortCalled = true
	m.lastSymbol = symbol
	m.lastQuantity = quantity
	m.lastLeverage = leverage
	if m.shouldError {
		return nil, fmt.Errorf("mock 开空仓失败")
	}
	return map[string]interface{}{"orderId": int64(12346)}, nil
}

func (m *mockTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	m.closeLongCalled = true
	m.lastSymbol = symbol
	if m.shouldError {
		return nil, fmt.Errorf("mock 平多仓失败")
	}
	return map[string]interface{}{"orderId": int64(12347)}, nil
}

func (m *mockTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	m.closeShortCalled = true
	m.lastSymbol = symbol
	if m.shouldError {
		return nil, fmt.Errorf("mock 平空仓失败")
	}
	return map[string]interface{}{"orderId": int64(12348)}, nil
}

func (m *mockTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (m *mockTrader) GetMarketPrice(symbol string) (float64, error) { return 50000.0, nil }
func (m *mockTrader) SetStopLoss(symbol, positionSide string, qty, price float64) error {
	m.stopLossCalls++
	if m.stopLossError {
		return fmt.Errorf("mock 设置止损失败")
	}
	return nil
}
func (m *mockTrader) SetTakeProfit(symbol, positionSide string, qty, price float64) error {
	m.takeProfitCalls++
	if m.takeProfitError {
		return fmt.Errorf("mock 设置止盈失败")
	}
	return nil
}
func (m *mockTrader) CancelStopOrders(symbol string) error       { return nil }
func (m *mockTrader) CancelStopLossOrders(symbol string) error   { return nil }
func (m *mockTrader) CancelTakeProfitOrders(symbol string) error { return nil }
func (m *mockTrader) CancelAllOrders(symbol string) error        { return nil }
func (m *mockTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.3f", quantity), nil
}
func (m *mockTrader) GetOrderHistory(symbol string, startTime, endTime int64, limit int) ([]OrderRecord, error) {
	return []OrderRecord{}, nil
}
func (m *mockTrader) GetTradeHistory(symbol string, startTime, endTime int64, limit int) ([]TradeRecord, error) {
	return []TradeRecord{}, nil
}
func (m *mockTrader) GetOrderStatus(symbol string, orderID int64) (*OrderRecord, error) {
	return nil, nil
}

// 编译期验证 mockTrader 实现了 Trader 接口
var _ Trader = (*mockTrader)(nil)

func TestMockTrader_ImplementsInterface(t *testing.T) {
	var tr Trader = &mockTrader{}
	if tr == nil {
		t.Error("mockTrader 应实现 Trader 接口")
	}
}

func TestMockTrader_OpenLong_RecordsCall(t *testing.T) {
	m := &mockTrader{}
	result, err := m.OpenLong("BTCUSDT", 0.1, 5)
	if err != nil {
		t.Fatalf("OpenLong 不应返回错误: %v", err)
	}
	if !m.openLongCalled {
		t.Error("OpenLong 应被调用")
	}
	if m.lastSymbol != "BTCUSDT" {
		t.Errorf("Symbol 应为 BTCUSDT, 实际=%s", m.lastSymbol)
	}
	if result["orderId"] == nil {
		t.Error("返回结果应包含 orderId")
	}
}

func TestMockTrader_OpenShort_RecordsCall(t *testing.T) {
	m := &mockTrader{}
	_, err := m.OpenShort("ETHUSDT", 1.0, 10)
	if err != nil {
		t.Fatalf("OpenShort 不应返回错误: %v", err)
	}
	if !m.openShortCalled {
		t.Error("OpenShort 应被调用")
	}
	if m.lastLeverage != 10 {
		t.Errorf("Leverage 应为 10, 实际=%d", m.lastLeverage)
	}
}

func TestMockTrader_ShouldError_ReturnsError(t *testing.T) {
	m := &mockTrader{shouldError: true}
	_, err := m.OpenLong("BTCUSDT", 0.1, 5)
	if err == nil {
		t.Error("shouldError=true 时 OpenLong 应返回错误")
	}
}

func TestMockTrader_FormatQuantity_ThreeDecimals(t *testing.T) {
	m := &mockTrader{}
	result, err := m.FormatQuantity("BTCUSDT", 1.23456)
	if err != nil {
		t.Fatalf("FormatQuantity 不应返回错误: %v", err)
	}
	if result != "1.235" {
		t.Errorf("FormatQuantity(1.23456): 期望1.235, 实际=%s", result)
	}
}

func TestEvaluateExecutionPreflight_MinNotionalBlocked(t *testing.T) {
	result := EvaluateExecutionPreflight(ExecutionPreflightInput{
		Symbol:        "BTCUSDT",
		Side:          "long",
		Quantity:      0.01,
		Price:         100,
		Leverage:      5,
		MinOrderValue: 10,
	})
	if result.Allowed {
		t.Fatalf("小额订单应被preflight拦截: %+v", result)
	}
}

func TestExecuteOpenLong_StopLossFailure_MarksHighRisk(t *testing.T) {
	originalGetter := getMarketData
	getMarketData = func(symbol string) (*market.Data, error) {
		return &market.Data{Symbol: symbol, CurrentPrice: 100}, nil
	}
	t.Cleanup(func() { getMarketData = originalGetter })
	if err := decision.InitPlanManager(t.TempDir()); err != nil {
		t.Fatalf("初始化计划管理器失败: %v", err)
	}

	m := &mockTrader{stopLossError: true}
	at := &AutoTrader{
		id:                    "test-trader",
		config:                AutoTraderConfig{},
		trader:                m,
		orderTracker:          NewOrderTracker(m),
		positionFirstSeenTime: make(map[string]int64),
	}
	record := &logger.DecisionAction{}
	d := &decision.Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        95,
		TakeProfit:      120,
		RiskUSD:         50,
		Reasoning:       "test",
	}

	err := at.executeOpenLongWithRecord(d, record)
	if err == nil {
		t.Fatal("止损失败时应返回高危错误")
	}
	if record.StopLossSet == nil || *record.StopLossSet {
		t.Fatalf("应记录止损未设置: %+v", record)
	}
	if !record.HighRisk || record.ExecutionRisk != "high" {
		t.Fatalf("应标记高危执行: %+v", record)
	}
	if m.stopLossCalls != protectiveOrderMaxAttempts {
		t.Fatalf("止损应重试%d次，实际=%d", protectiveOrderMaxAttempts, m.stopLossCalls)
	}
	if !m.openLongCalled {
		t.Fatal("应先开仓后发现保护单失败")
	}
}

func TestExecuteOpenLong_StopLossFailure_EmergencyClose(t *testing.T) {
	originalGetter := getMarketData
	getMarketData = func(symbol string) (*market.Data, error) {
		return &market.Data{Symbol: symbol, CurrentPrice: 100}, nil
	}
	t.Cleanup(func() { getMarketData = originalGetter })
	if err := decision.InitPlanManager(t.TempDir()); err != nil {
		t.Fatalf("初始化计划管理器失败: %v", err)
	}

	m := &mockTrader{stopLossError: true}
	at := &AutoTrader{
		id:                    "test-trader",
		config:                AutoTraderConfig{EnableEmergencyClose: true},
		trader:                m,
		orderTracker:          NewOrderTracker(m),
		positionFirstSeenTime: make(map[string]int64),
	}
	record := &logger.DecisionAction{}
	d := &decision.Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        5,
		PositionSizeUSD: 1000,
		StopLoss:        95,
		TakeProfit:      120,
		RiskUSD:         50,
		Reasoning:       "test",
	}

	err := at.executeOpenLongWithRecord(d, record)
	if err == nil {
		t.Fatal("紧急平仓后仍应返回错误供日志记录")
	}
	if !m.closeLongCalled {
		t.Fatal("启用紧急平仓后应调用 CloseLong")
	}
	if !record.HighRisk {
		t.Fatalf("应保留高危标记: %+v", record)
	}
}

func TestAutoCloseDedupe_ClaimFallbackAndOrderID(t *testing.T) {
	at := &AutoTrader{id: "trader-a"}
	now := time.Now()

	if !at.claimAutoCloseEvent("BTCUSDT", "long", 123, now) {
		t.Fatal("首次自动平仓事件应被接收")
	}
	if at.claimAutoCloseEvent("BTCUSDT", "long", 0, now.Add(time.Minute)) {
		t.Fatal("同一symbol/side的降级事件应被去重")
	}
	if at.claimAutoCloseEvent("ETHUSDT", "long", 123, now.Add(time.Minute)) {
		t.Fatal("同一order id应被去重")
	}
}

func TestDetectAutoClosedPositions_DedupesPreviouslyClaimedEvent(t *testing.T) {
	originalGetter := getMarketData
	getMarketData = func(symbol string) (*market.Data, error) {
		return &market.Data{Symbol: symbol, CurrentPrice: 110}, nil
	}
	t.Cleanup(func() { getMarketData = originalGetter })

	at := &AutoTrader{
		id: "trader-a",
		lastPositions: map[string]*PositionSnapshot{
			"BTCUSDT_long": {
				Symbol:     "BTCUSDT",
				Side:       "long",
				Quantity:   1,
				EntryPrice: 100,
				Leverage:   5,
			},
		},
	}
	at.claimAutoCloseEvent("BTCUSDT", "long", 456, time.Now())

	actions := at.detectAutoClosedPositions(nil)
	if len(actions) != 0 {
		t.Fatalf("已由订单追踪路径处理的事件不应再由快照路径生成: %+v", actions)
	}
}

func TestAutoTraderApplyAICallState_SuccessUpdatesLastAnalysis(t *testing.T) {
	at := &AutoTrader{
		config:             AutoTraderConfig{ScanInterval: 2 * time.Minute},
		aiBackoffUntil:     time.Now().Add(time.Hour),
		lastAIError:        "old error",
		consecutiveAIFails: 2,
	}
	attemptTime := time.Now().Add(-time.Minute)

	at.applyAICallState(&decision.FullDecision{
		Timestamp:       attemptTime,
		AICallAttempted: true,
		AICallSucceeded: true,
	})

	if !at.lastAIAttemptTime.Equal(attemptTime) {
		t.Fatalf("lastAIAttemptTime 未更新: got=%v want=%v", at.lastAIAttemptTime, attemptTime)
	}
	if !at.lastAISuccessTime.Equal(attemptTime) {
		t.Fatalf("lastAISuccessTime 未更新: got=%v want=%v", at.lastAISuccessTime, attemptTime)
	}
	if !at.lastAnalysisTime.Equal(attemptTime) {
		t.Fatalf("lastAnalysisTime 未更新: got=%v want=%v", at.lastAnalysisTime, attemptTime)
	}
	if !at.aiBackoffUntil.IsZero() {
		t.Fatalf("AI成功后应清除退避时间: got=%v", at.aiBackoffUntil)
	}
	if at.consecutiveAIFails != 0 {
		t.Fatalf("AI成功后连续失败次数应清零: got=%d", at.consecutiveAIFails)
	}
	if at.lastAIError != "" {
		t.Fatalf("AI成功后应清除错误原因: got=%q", at.lastAIError)
	}
}

func TestAutoTraderApplyAICallState_FailureSetsBackoff(t *testing.T) {
	at := &AutoTrader{
		config: AutoTraderConfig{ScanInterval: 2 * time.Minute},
	}
	attemptTime := time.Now().Add(-time.Minute)

	at.applyAICallState(&decision.FullDecision{
		Timestamp:       attemptTime,
		AICallAttempted: true,
		AICallSucceeded: false,
		AIFailureReason: "timeout",
	})

	if !at.lastAIAttemptTime.Equal(attemptTime) {
		t.Fatalf("lastAIAttemptTime 未更新: got=%v want=%v", at.lastAIAttemptTime, attemptTime)
	}
	if !at.lastAnalysisTime.IsZero() {
		t.Fatalf("AI失败不应刷新lastAnalysisTime: got=%v", at.lastAnalysisTime)
	}
	if !at.aiBackoffUntil.Equal(attemptTime.Add(2 * time.Minute)) {
		t.Fatalf("退避时间错误: got=%v want=%v", at.aiBackoffUntil, attemptTime.Add(2*time.Minute))
	}
	if at.consecutiveAIFails != 1 {
		t.Fatalf("连续失败次数应加1: got=%d", at.consecutiveAIFails)
	}
	if at.lastAIError != "timeout" {
		t.Fatalf("最近AI错误未记录: got=%q", at.lastAIError)
	}
}

func TestBuildTradingContext_InjectsAIStateRiskAndExecutionQuality(t *testing.T) {
	pool.SetCoinPoolAPI("")
	pool.SetOITopAPI("")
	pool.SetDefaultCoins([]string{"BTCUSDT", "ETHUSDT"})
	pool.SetUseDefaultCoins(true)
	t.Cleanup(func() {
		pool.SetUseDefaultCoins(false)
		pool.SetDefaultCoins([]string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT", "DOGEUSDT", "ADAUSDT", "HYPEUSDT"})
	})

	lastAnalysis := time.Now().Add(-30 * time.Minute)
	lastAttempt := time.Now().Add(-20 * time.Minute)
	lastSuccess := time.Now().Add(-45 * time.Minute)
	backoffUntil := time.Now().Add(5 * time.Minute)

	at := &AutoTrader{
		id:       "trader-alpha",
		name:     "Alpha",
		exchange: "binance",
		config: AutoTraderConfig{
			MaxRiskPerTrade:     0.015,
			TotalRiskBudget:     0.07,
			MaxDailyLoss:        0.06,
			MaxDrawdown:         0.25,
			AnalysisIntervalMin: 9,
			BTCETHLeverage:      5,
			AltcoinLeverage:     4,
		},
		trader:                &mockTrader{},
		decisionLogger:        logger.NewDecisionLogger(t.TempDir()),
		initialBalance:        10000,
		startTime:             time.Now().Add(-2 * time.Hour),
		positionFirstSeenTime: make(map[string]int64),
		lastAnalysisTime:      lastAnalysis,
		lastAIAttemptTime:     lastAttempt,
		lastAISuccessTime:     lastSuccess,
		aiBackoffUntil:        backoffUntil,
		lastAIError:           "timeout",
		consecutiveAIFails:    3,
	}

	ctx, err := at.buildTradingContext()
	if err != nil {
		t.Fatalf("buildTradingContext 不应失败: %v", err)
	}

	if ctx.TraderID != "trader-alpha" || ctx.Exchange != "binance" {
		t.Fatalf("trader作用域字段错误: trader_id=%q exchange=%q", ctx.TraderID, ctx.Exchange)
	}
	if math.Abs(ctx.MaxRiskPerTrade-0.015) > 0.000001 {
		t.Fatalf("MaxRiskPerTrade 未注入: got=%.4f", ctx.MaxRiskPerTrade)
	}
	if math.Abs(ctx.TotalRiskBudget-0.07) > 0.000001 {
		t.Fatalf("TotalRiskBudget 未注入: got=%.4f", ctx.TotalRiskBudget)
	}
	if math.Abs(ctx.MaxDailyLossPct-0.06) > 0.000001 {
		t.Fatalf("MaxDailyLossPct 未注入: got=%.4f", ctx.MaxDailyLossPct)
	}
	if math.Abs(ctx.MaxAccountDrawdownPct-25.0) > 0.000001 {
		t.Fatalf("MaxAccountDrawdownPct 未归一化注入: got=%.4f", ctx.MaxAccountDrawdownPct)
	}
	if ctx.AnalysisIntervalMin != 9 {
		t.Fatalf("AnalysisIntervalMin 未注入: got=%d", ctx.AnalysisIntervalMin)
	}
	if !ctx.LastAnalysisTime.Equal(lastAnalysis) || !ctx.LastAIAttemptTime.Equal(lastAttempt) || !ctx.LastAISuccessTime.Equal(lastSuccess) {
		t.Fatalf("AI时间状态未正确注入")
	}
	if !ctx.AIBackoffUntil.Equal(backoffUntil) || ctx.LastAIError != "timeout" || ctx.ConsecutiveAIFails != 3 {
		t.Fatalf("AI失败状态未正确注入")
	}
	if ctx.ExecutionQuality == nil {
		t.Fatal("ExecutionQuality 应从历史表现分析注入")
	}
	if len(ctx.CandidateCoins) == 0 {
		t.Fatal("默认币种池应生成候选币")
	}
}

// ============================================================================
// Feature: quant-trading-system, Property 7: 数量精度格式化往返
// 需求 2.5: 对任意正浮点数，FormatQuantity 产生的字符串解析回浮点数后差值不超过 stepSize
// ============================================================================

// formatQuantityWithStepSize 使用 stepSize 字符串格式化数量（复用 calculatePrecision 逻辑）
func formatQuantityWithStepSize(quantity float64, stepSize string) (string, error) {
	precision := calculatePrecision(stepSize)
	format := fmt.Sprintf("%%.%df", precision)
	return fmt.Sprintf(format, quantity), nil
}

// stepSizeToFloat 将 stepSize 字符串转换为浮点数
func stepSizeToFloat(stepSize string) float64 {
	v, err := strconv.ParseFloat(stepSize, 64)
	if err != nil {
		return 1.0
	}
	return v
}

// TestProperty7_FormatQuantity_RoundTrip 属性基测试: 数量精度格式化往返
// 对任意正浮点数和有效 stepSize，格式化后解析回来的误差不超过 stepSize
func TestProperty7_FormatQuantity_RoundTrip(t *testing.T) {
	// Feature: quant-trading-system, Property 7: 数量精度格式化往返
	properties := gopter.NewProperties(gopter.DefaultTestParameters())

	// 常见的 stepSize 值（对应精度 0~8）
	stepSizes := []string{"1", "0.1", "0.01", "0.001", "0.0001", "0.00001", "0.000001", "0.0000001", "0.00000001"}

	properties.Property("格式化往返误差不超过stepSize", prop.ForAll(
		func(quantity float64, stepIdx uint8) bool {
			stepSize := stepSizes[int(stepIdx)%len(stepSizes)]
			stepVal := stepSizeToFloat(stepSize)

			formatted, err := formatQuantityWithStepSize(quantity, stepSize)
			if err != nil {
				return false
			}

			parsed, err := strconv.ParseFloat(formatted, 64)
			if err != nil {
				return false
			}

			diff := math.Abs(quantity - parsed)
			return diff <= stepVal
		},
		// 生成正浮点数 (1e-8, 1e6]
		gen.Float64Range(1e-8, 1e6),
		// 生成 stepSize 索引
		gen.UInt8(),
	))

	properties.TestingRun(t)
}

// TestProperty7_FormatQuantity_RoundTrip_CommonPrecisions 针对常见精度的详细验证
func TestProperty7_FormatQuantity_RoundTrip_CommonPrecisions(t *testing.T) {
	// Feature: quant-trading-system, Property 7: 数量精度格式化往返
	properties := gopter.NewProperties(gopter.DefaultTestParameters())

	properties.Property("精度3格式化往返误差不超过0.001", prop.ForAll(
		func(quantity float64) bool {
			stepSize := "0.001"
			stepVal := 0.001
			formatted, err := formatQuantityWithStepSize(quantity, stepSize)
			if err != nil {
				return false
			}
			parsed, err := strconv.ParseFloat(formatted, 64)
			if err != nil {
				return false
			}
			return math.Abs(quantity-parsed) <= stepVal
		},
		gen.Float64Range(0.001, 10000.0),
	))

	properties.Property("精度0格式化往返误差不超过1", prop.ForAll(
		func(quantity float64) bool {
			stepSize := "1"
			stepVal := 1.0
			formatted, err := formatQuantityWithStepSize(quantity, stepSize)
			if err != nil {
				return false
			}
			parsed, err := strconv.ParseFloat(formatted, 64)
			if err != nil {
				return false
			}
			return math.Abs(quantity-parsed) <= stepVal
		},
		gen.Float64Range(1.0, 100000.0),
	))

	properties.Property("精度8格式化往返误差不超过0.00000001", prop.ForAll(
		func(quantity float64) bool {
			stepSize := "0.00000001"
			stepVal := 0.00000001
			formatted, err := formatQuantityWithStepSize(quantity, stepSize)
			if err != nil {
				return false
			}
			parsed, err := strconv.ParseFloat(formatted, 64)
			if err != nil {
				return false
			}
			return math.Abs(quantity-parsed) <= stepVal
		},
		gen.Float64Range(0.00000001, 100.0),
	))

	properties.TestingRun(t)
}
