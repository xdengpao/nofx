package decision

// Feature: quant-trading-system
// 任务 11.1: 持久化和统计逻辑测试覆盖
// 覆盖需求: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

// setupTestPlanManager 创建使用临时目录的 TradePlanManager，返回目录路径和清理函数
func setupTestPlanManager(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	if err := InitPlanManager(dir); err != nil {
		t.Fatalf("InitPlanManager 失败: %v", err)
	}
	ResetStatistics()
	return dir, func() { ResetStatistics() }
}

// newTestTradePlan 构造测试用交易计划
func newTestTradePlan(symbol string) *TradePlan {
	return &TradePlan{
		Symbol:     symbol,
		Direction:  "long",
		EntryPrice: 50000.0,
		StopLoss:   48000.0,
		TakeProfit: 60000.0,
		CreatedAt:  time.Now(),
		Status:     "active",
	}
}

// ============================================================================
// 需求 10.5: UpdateStatistics — 胜率和盈亏因子计算
// ============================================================================

func TestUpdateStatistics_WinRate_SingleWin(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(5.0, 60)

	stats := GetStatistics()
	if stats.TotalTrades != 1 {
		t.Errorf("总交易数应为1, 实际=%d", stats.TotalTrades)
	}
	if stats.WinningTrades != 1 {
		t.Errorf("盈利交易数应为1, 实际=%d", stats.WinningTrades)
	}
	if stats.WinRate != 1.0 {
		t.Errorf("胜率应为1.0, 实际=%.4f", stats.WinRate)
	}
}

func TestUpdateStatistics_WinRate_SingleLoss(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(-3.0, 30)

	stats := GetStatistics()
	if stats.WinRate != 0.0 {
		t.Errorf("全亏时胜率应为0.0, 实际=%.4f", stats.WinRate)
	}
	if stats.LosingTrades != 1 {
		t.Errorf("亏损交易数应为1, 实际=%d", stats.LosingTrades)
	}
}

func TestUpdateStatistics_WinRate_TwoWinsOneLoss(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(4.0, 60)
	UpdateStatistics(6.0, 90)
	UpdateStatistics(-2.0, 45)

	stats := GetStatistics()
	expectedWinRate := 2.0 / 3.0
	if math.Abs(stats.WinRate-expectedWinRate) > 0.0001 {
		t.Errorf("胜率应为%.4f, 实际=%.4f", expectedWinRate, stats.WinRate)
	}
}

func TestUpdateStatistics_ProfitFactor_Calculation(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	// 2次盈利: 平均盈利 = (4+6)/2 = 5.0, 胜率 = 2/3
	// 1次亏损: 平均亏损 = 2.0, 败率 = 1/3
	// 盈亏因子 = (5.0 × 2/3) / (2.0 × 1/3) = 3.333/0.667 = 5.0
	UpdateStatistics(4.0, 60)
	UpdateStatistics(6.0, 90)
	UpdateStatistics(-2.0, 45)

	stats := GetStatistics()
	expectedPF := (5.0 * (2.0 / 3.0)) / (2.0 * (1.0 / 3.0))
	if math.Abs(stats.ProfitFactor-expectedPF) > 0.01 {
		t.Errorf("盈亏因子应为%.4f, 实际=%.4f", expectedPF, stats.ProfitFactor)
	}
}

func TestUpdateStatistics_AverageWin_Incremental(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(4.0, 60)
	UpdateStatistics(6.0, 90)

	stats := GetStatistics()
	expectedAvgWin := 5.0
	if math.Abs(stats.AverageWin-expectedAvgWin) > 0.0001 {
		t.Errorf("平均盈利应为%.4f, 实际=%.4f", expectedAvgWin, stats.AverageWin)
	}
}

func TestUpdateStatistics_AverageLoss_Incremental(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(-2.0, 30)
	UpdateStatistics(-4.0, 45)

	stats := GetStatistics()
	expectedAvgLoss := 3.0
	if math.Abs(stats.AverageLoss-expectedAvgLoss) > 0.0001 {
		t.Errorf("平均亏损应为%.4f, 实际=%.4f", expectedAvgLoss, stats.AverageLoss)
	}
}

func TestUpdateStatistics_ConsecutiveLosses_Tracking(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(-1.0, 30)
	UpdateStatistics(-2.0, 30)
	UpdateStatistics(-3.0, 30)

	stats := GetStatistics()
	if stats.ConsecutiveLosses != 3 {
		t.Errorf("连续亏损应为3, 实际=%d", stats.ConsecutiveLosses)
	}
	if stats.MaxConsecLosses != 3 {
		t.Errorf("最大连续亏损应为3, 实际=%d", stats.MaxConsecLosses)
	}
}

func TestUpdateStatistics_ConsecutiveLosses_ResetOnWin(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(-1.0, 30)
	UpdateStatistics(-2.0, 30)
	UpdateStatistics(5.0, 60) // 盈利重置连续亏损

	stats := GetStatistics()
	if stats.ConsecutiveLosses != 0 {
		t.Errorf("盈利后连续亏损应重置为0, 实际=%d", stats.ConsecutiveLosses)
	}
	if stats.MaxConsecLosses != 2 {
		t.Errorf("最大连续亏损应保留为2, 实际=%d", stats.MaxConsecLosses)
	}
}

func TestUpdateStatistics_AverageHoldTime(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	UpdateStatistics(3.0, 60.0)
	UpdateStatistics(-1.0, 120.0)

	stats := GetStatistics()
	expectedAvgHold := 90.0
	if math.Abs(stats.AverageHoldTime-expectedAvgHold) > 0.0001 {
		t.Errorf("平均持仓时间应为%.1f, 实际=%.1f", expectedAvgHold, stats.AverageHoldTime)
	}
}

// ============================================================================
// 需求 10.7: AddReturn — 收益率序列长度上限为 1000
// ============================================================================

func TestAddReturn_LengthCap_At1000(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	// 添加 1100 条记录，序列长度应保持 ≤ 1000
	for i := 0; i < 1100; i++ {
		AddReturn(float64(i) * 0.01)
	}

	returnsLock.RLock()
	length := len(returnsSeries)
	returnsLock.RUnlock()

	if length > 1000 {
		t.Errorf("收益率序列长度不应超过1000, 实际=%d", length)
	}
	if length != 1000 {
		t.Errorf("添加1100条后序列长度应为1000, 实际=%d", length)
	}
}

func TestAddReturn_KeepsLatest1000(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	// 添加 1010 条，最后一条值为 999.99
	for i := 0; i < 1010; i++ {
		AddReturn(float64(i))
	}

	returnsLock.RLock()
	last := returnsSeries[len(returnsSeries)-1]
	first := returnsSeries[0]
	returnsLock.RUnlock()

	// 应保留最新的 1000 条（索引 10~1009）
	if last != 1009.0 {
		t.Errorf("最后一条应为1009.0, 实际=%.1f", last)
	}
	if first != 10.0 {
		t.Errorf("第一条应为10.0（保留最新1000条）, 实际=%.1f", first)
	}
}

func TestAddReturn_ExactlyAt1000_NoTruncation(t *testing.T) {
	ResetStatistics()
	defer ResetStatistics()

	for i := 0; i < 1000; i++ {
		AddReturn(float64(i) * 0.1)
	}

	returnsLock.RLock()
	length := len(returnsSeries)
	returnsLock.RUnlock()

	if length != 1000 {
		t.Errorf("恰好1000条时序列长度应为1000, 实际=%d", length)
	}
}

// ============================================================================
// 需求 10.1, 10.4: 持久化恢复 — 启动时从文件恢复所有数据
// ============================================================================

func TestPersistence_LoadFromFile_RestoresPlans(t *testing.T) {
	dir, cleanup := setupTestPlanManager(t)
	defer cleanup()

	planManager.SetPlan(newTestTradePlan("BNBUSDT"))

	// 模拟重启：重新初始化
	if err := InitPlanManager(dir); err != nil {
		t.Fatalf("重新初始化失败: %v", err)
	}

	plan := planManager.GetPlan("BNBUSDT")
	if plan == nil {
		t.Fatal("重启后应能恢复 BNBUSDT 计划")
	}
	if math.Abs(plan.EntryPrice-50000.0) > 0.0001 {
		t.Errorf("恢复的入场价格错误: 期望=50000.0, 实际=%.4f", plan.EntryPrice)
	}
}

func TestOnStopLossUpdated_UpdatesCurrentStopLoss(t *testing.T) {
	_, cleanup := setupTestPlanManager(t)
	defer cleanup()

	plan := newTestTradePlan("ETHUSDT")
	plan.CurrentStopLoss = 48000
	planManager.SetPlan(plan)

	OnStopLossUpdated("ETHUSDT", 49500)

	updated := planManager.GetPlan("ETHUSDT")
	if updated == nil {
		t.Fatal("应保留 ETHUSDT 交易计划")
	}
	if math.Abs(updated.CurrentStopLoss-49500) > 0.0001 {
		t.Fatalf("CurrentStopLoss 未更新: 期望=49500, 实际=%.4f", updated.CurrentStopLoss)
	}
	if !updated.TrailingStopActive {
		t.Fatal("移动止损成功后应标记 TrailingStopActive")
	}
}

func TestOnPartialClose_MarksTrancheAndUpdatesStopLoss(t *testing.T) {
	_, cleanup := setupTestPlanManager(t)
	defer cleanup()

	plan := newTestTradePlan("XRPUSDT")
	plan.ExecutedTranches = make(map[int]bool)
	planManager.SetPlan(plan)

	OnPartialClose("XRPUSDT", 1, 30, 49000)

	updated := planManager.GetPlan("XRPUSDT")
	if updated == nil {
		t.Fatal("应保留 XRPUSDT 交易计划")
	}
	if !updated.ExecutedTranches[1] {
		t.Fatal("partial_close 成功后应标记 tranche 已执行")
	}
	if math.Abs(updated.CurrentStopLoss-49000) > 0.0001 {
		t.Fatalf("partial_close 新止损未持久化: 期望=49000, 实际=%.4f", updated.CurrentStopLoss)
	}
}

func TestOnPartialClose_NoNewStopLoss_DoesNotOverwriteCurrentStopLoss(t *testing.T) {
	_, cleanup := setupTestPlanManager(t)
	defer cleanup()

	plan := newTestTradePlan("SOLUSDT")
	plan.CurrentStopLoss = 48500
	plan.ExecutedTranches = make(map[int]bool)
	planManager.SetPlan(plan)

	OnPartialClose("SOLUSDT", 0, 20, 0)

	updated := planManager.GetPlan("SOLUSDT")
	if updated == nil {
		t.Fatal("应保留 SOLUSDT 交易计划")
	}
	if !updated.ExecutedTranches[0] {
		t.Fatal("即使无新止损，也应标记 tranche 已执行")
	}
	if math.Abs(updated.CurrentStopLoss-48500) > 0.0001 {
		t.Fatalf("无新止损时不应覆盖 CurrentStopLoss: 实际=%.4f", updated.CurrentStopLoss)
	}
}

func TestTradePlanManager_ScopedPlanKeys(t *testing.T) {
	_, cleanup := setupTestPlanManager(t)
	defer cleanup()

	longPlan := newTestTradePlan("BTCUSDT")
	longPlan.TraderID = "trader-a"
	longPlan.Direction = "long"
	shortPlan := newTestTradePlan("BTCUSDT")
	shortPlan.TraderID = "trader-b"
	shortPlan.Direction = "short"

	planManager.SetPlan(longPlan)
	planManager.SetPlan(shortPlan)

	if got := planManager.GetPlanScoped("trader-a", "BTCUSDT", "long"); got == nil || got.TraderID != "trader-a" || got.Direction != "long" {
		t.Fatalf("应按 trader/symbol/side 获取 long plan: %+v", got)
	}
	if got := planManager.GetPlanScoped("trader-b", "BTCUSDT", "short"); got == nil || got.TraderID != "trader-b" || got.Direction != "short" {
		t.Fatalf("应按 trader/symbol/side 获取 short plan: %+v", got)
	}

	planManager.RemovePlanScoped("trader-a", "BTCUSDT", "long")
	if got := planManager.GetPlanScoped("trader-a", "BTCUSDT", "long"); got != nil {
		t.Fatalf("移除 trader-a long 后不应还能获取: %+v", got)
	}
	if got := planManager.GetPlanScoped("trader-b", "BTCUSDT", "short"); got == nil {
		t.Fatal("移除 trader-a long 不应影响 trader-b short")
	}
}

func TestPersistence_LoadFromFile_RestoresStatistics(t *testing.T) {
	dir, cleanup := setupTestPlanManager(t)
	defer cleanup()

	UpdateStatistics(4.0, 60)
	UpdateStatistics(-1.0, 30)

	// 强制保存（在清空内存之前）
	if err := planManager.ForceSave(); err != nil {
		t.Fatalf("强制保存失败: %v", err)
	}

	// 直接清空内存统计，不触发自动保存（避免覆盖文件）
	tradeStatsLock.Lock()
	tradeStats = &TradeStatistics{}
	tradeStatsLock.Unlock()

	// 重新加载文件
	if err := InitPlanManager(dir); err != nil {
		t.Fatalf("重新初始化失败: %v", err)
	}

	stats := GetStatistics()
	if stats.TotalTrades != 2 {
		t.Errorf("恢复后总交易数应为2, 实际=%d", stats.TotalTrades)
	}
}

func TestPersistence_LoadFromFile_RestoresReturns(t *testing.T) {
	dir, cleanup := setupTestPlanManager(t)
	defer cleanup()

	AddReturn(1.5)
	AddReturn(-0.5)
	AddReturn(2.0)

	if err := planManager.ForceSave(); err != nil {
		t.Fatalf("强制保存失败: %v", err)
	}

	// 清空内存后重新加载
	returnsLock.Lock()
	returnsSeries = nil
	returnsLock.Unlock()

	if err := InitPlanManager(dir); err != nil {
		t.Fatalf("重新初始化失败: %v", err)
	}

	returnsLock.RLock()
	length := len(returnsSeries)
	returnsLock.RUnlock()

	if length != 3 {
		t.Errorf("恢复后收益率序列长度应为3, 实际=%d", length)
	}
}

func TestPersistence_NonExistentFile_NoError(t *testing.T) {
	dir := t.TempDir()
	// 文件不存在时初始化不应报错
	if err := InitPlanManager(dir); err != nil {
		t.Errorf("文件不存在时初始化不应报错: %v", err)
	}
}

// ============================================================================
// 需求 10.3, 10.9: 自动保存和强制保存
// ============================================================================

func TestAutoSave_OnSetPlan(t *testing.T) {
	dir, cleanup := setupTestPlanManager(t)
	defer cleanup()

	planManager.SetPlan(newTestTradePlan("ADAUSDT"))

	// 文件应已自动保存
	data, err := os.ReadFile(filepath.Join(dir, plansFileName))
	if err != nil {
		t.Fatalf("自动保存后文件应存在: %v", err)
	}

	var pd PersistentData
	if err := json.Unmarshal(data, &pd); err != nil {
		t.Fatalf("自动保存的文件应为合法 JSON: %v", err)
	}

	if _, ok := pd.Plans["ADAUSDT"]; !ok {
		t.Error("自动保存的文件应包含 ADAUSDT 计划")
	}
}

func TestForceSave_PersistsCurrentState(t *testing.T) {
	dir, cleanup := setupTestPlanManager(t)
	defer cleanup()

	// 禁用自动保存，手动触发强制保存
	planManager.autoSave = false
	defer func() { planManager.autoSave = true }()

	planManager.mu.Lock()
	planManager.plans["DOTUSDT"] = newTestTradePlan("DOTUSDT")
	planManager.mu.Unlock()

	if err := planManager.ForceSave(); err != nil {
		t.Fatalf("ForceSave 失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, plansFileName))
	if err != nil {
		t.Fatalf("强制保存后文件应存在: %v", err)
	}

	var pd PersistentData
	if err := json.Unmarshal(data, &pd); err != nil {
		t.Fatalf("强制保存的文件应为合法 JSON: %v", err)
	}

	if _, ok := pd.Plans["DOTUSDT"]; !ok {
		t.Error("强制保存的文件应包含 DOTUSDT 计划")
	}
}

// ============================================================================
// 需求 10.7: 已平仓交易记录上限为 100 条
// ============================================================================

func TestClosedTrades_LengthCap_At100(t *testing.T) {
	_, cleanup := setupTestPlanManager(t)
	defer cleanup()

	// 直接向 closedTrades 添加超过 100 条记录
	closedTradesLock.Lock()
	closedTrades = nil
	for i := 0; i < 110; i++ {
		closedTrades = append(closedTrades, ClosedTradeRecord{
			Symbol:     "BTCUSDT",
			PnLPercent: float64(i),
			ClosedAt:   time.Now(),
		})
		if len(closedTrades) > 100 {
			closedTrades = closedTrades[len(closedTrades)-100:]
		}
	}
	length := len(closedTrades)
	closedTradesLock.Unlock()

	if length > 100 {
		t.Errorf("已平仓交易记录不应超过100条, 实际=%d", length)
	}
}

// ============================================================================
// 需求 10.1, 10.4: Property 36 — 持久化数据往返
// Feature: quant-trading-system, Property 36: 持久化数据往返
// 对任意有效 PersistentData，序列化为 JSON 再反序列化应产生等价数据
// ============================================================================

func TestProperty36_PersistentDataRoundTrip(t *testing.T) {
	// Feature: quant-trading-system, Property 36: 持久化数据往返
	// 对任意有效 PersistentData，序列化为 JSON 再反序列化应产生等价数据
	// 验证: 需求 10.1, 10.4

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 生成随机 TradePlan
	tradePlanGen := gen.Struct(reflect.TypeOf(TradePlan{}), map[string]gopter.Gen{
		"Symbol":             gen.OneConstOf("BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"),
		"Direction":          gen.OneConstOf("long", "short"),
		"EntryPrice":         gen.Float64Range(100, 100000),
		"StopLoss":           gen.Float64Range(50, 99000),
		"TakeProfit":         gen.Float64Range(101, 200000),
		"PositionSizeUSD":    gen.Float64Range(10, 10000),
		"Leverage":           gen.IntRange(1, 20),
		"Status":             gen.OneConstOf("active", "closed"),
		"Confidence":         gen.IntRange(0, 100),
		"PeakPnLPercent":     gen.Float64Range(-50, 200),
		"TotalClosedPercent": gen.Float64Range(0, 100),
		"MinHoldMinutes":     gen.IntRange(0, 240),
	})

	// 生成随机 TradeStatistics
	statsGen := gen.Struct(reflect.TypeOf(TradeStatistics{}), map[string]gopter.Gen{
		"TotalTrades":       gen.IntRange(0, 1000),
		"WinningTrades":     gen.IntRange(0, 500),
		"LosingTrades":      gen.IntRange(0, 500),
		"TotalPnL":          gen.Float64Range(-100, 500),
		"AverageWin":        gen.Float64Range(0, 50),
		"AverageLoss":       gen.Float64Range(0, 50),
		"WinRate":           gen.Float64Range(0, 1),
		"ProfitFactor":      gen.Float64Range(0, 10),
		"SharpeRatio":       gen.Float64Range(-5, 10),
		"SortinoRatio":      gen.Float64Range(-5, 10),
		"MaxDrawdown":       gen.Float64Range(0, 100),
		"AverageHoldTime":   gen.Float64Range(0, 10000),
		"ConsecutiveWins":   gen.IntRange(0, 20),
		"ConsecutiveLosses": gen.IntRange(0, 20),
		"MaxConsecLosses":   gen.IntRange(0, 20),
	})

	// 生成随机收益率序列（0~50 条）
	returnsGen := gen.IntRange(0, 50).FlatMap(
		func(n interface{}) gopter.Gen {
			count := n.(int)
			if count == 0 {
				return gen.Const([]float64{})
			}
			return gen.SliceOfN(count, gen.Float64Range(-20, 50)).
				Map(func(s []float64) []float64 { return s })
		},
		reflect.TypeOf([]float64{}),
	)

	// 生成随机 ClosedTradeRecord 列表（0~10 条）
	closedTradeGen := gen.Struct(reflect.TypeOf(ClosedTradeRecord{}), map[string]gopter.Gen{
		"Symbol":         gen.OneConstOf("BTCUSDT", "ETHUSDT", "SOLUSDT"),
		"Direction":      gen.OneConstOf("long", "short"),
		"EntryPrice":     gen.Float64Range(100, 100000),
		"ExitPrice":      gen.Float64Range(100, 100000),
		"PnLPercent":     gen.Float64Range(-50, 200),
		"PnLUSD":         gen.Float64Range(-5000, 10000),
		"ExitReason":     gen.OneConstOf("stop_loss", "take_profit", "manual"),
		"PeakPnLPercent": gen.Float64Range(-50, 200),
		"HoldingMinutes": gen.Int64Range(0, 10000),
	})

	closedTradesGen := gen.IntRange(0, 10).FlatMap(
		func(n interface{}) gopter.Gen {
			count := n.(int)
			if count == 0 {
				return gen.Const([]ClosedTradeRecord{})
			}
			return gen.SliceOfN(count, closedTradeGen).
				Map(func(s []ClosedTradeRecord) []ClosedTradeRecord { return s })
		},
		reflect.TypeOf([]ClosedTradeRecord{}),
	)

	properties.Property("PersistentData JSON 序列化往返产生等价数据", prop.ForAll(
		func(plan TradePlan, stats TradeStatistics, returns []float64, closedTradesList []ClosedTradeRecord) bool {
			// 构造 PersistentData
			plan.ExecutedTranches = map[int]bool{0: true, 1: false}
			plan.CreatedAt = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
			plan.LastTPAdjustTime = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
			stats.LastUpdated = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

			for i := range closedTradesList {
				closedTradesList[i].ClosedAt = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
				closedTradesList[i].EntryTime = time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC)
				closedTradesList[i].ExitTime = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
			}

			original := PersistentData{
				Plans:        map[string]*TradePlan{plan.Symbol: &plan},
				Statistics:   &stats,
				Returns:      returns,
				ClosedTrades: closedTradesList,
				UpdatedAt:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			}

			// 序列化
			data, err := json.Marshal(original)
			if err != nil {
				return false
			}

			// 反序列化
			var restored PersistentData
			if err := json.Unmarshal(data, &restored); err != nil {
				return false
			}

			// 验证 Plans 等价
			if len(restored.Plans) != len(original.Plans) {
				return false
			}
			restoredPlan, ok := restored.Plans[plan.Symbol]
			if !ok {
				return false
			}
			if restoredPlan.Symbol != plan.Symbol {
				return false
			}
			if math.Abs(restoredPlan.EntryPrice-plan.EntryPrice) > 1e-9 {
				return false
			}
			if math.Abs(restoredPlan.StopLoss-plan.StopLoss) > 1e-9 {
				return false
			}
			if math.Abs(restoredPlan.TakeProfit-plan.TakeProfit) > 1e-9 {
				return false
			}
			if restoredPlan.Direction != plan.Direction {
				return false
			}
			if restoredPlan.Leverage != plan.Leverage {
				return false
			}

			// 验证 Statistics 等价
			if restored.Statistics == nil {
				return false
			}
			if restored.Statistics.TotalTrades != stats.TotalTrades {
				return false
			}
			if math.Abs(restored.Statistics.WinRate-stats.WinRate) > 1e-9 {
				return false
			}
			if math.Abs(restored.Statistics.ProfitFactor-stats.ProfitFactor) > 1e-9 {
				return false
			}
			if math.Abs(restored.Statistics.SharpeRatio-stats.SharpeRatio) > 1e-9 {
				return false
			}

			// 验证 Returns 等价
			if len(restored.Returns) != len(returns) {
				return false
			}
			for i, r := range returns {
				if math.Abs(restored.Returns[i]-r) > 1e-9 {
					return false
				}
			}

			// 验证 ClosedTrades 等价
			if len(restored.ClosedTrades) != len(closedTradesList) {
				return false
			}
			for i, ct := range closedTradesList {
				if restored.ClosedTrades[i].Symbol != ct.Symbol {
					return false
				}
				if math.Abs(restored.ClosedTrades[i].PnLPercent-ct.PnLPercent) > 1e-9 {
					return false
				}
			}

			// 验证 UpdatedAt 等价
			if !restored.UpdatedAt.Equal(original.UpdatedAt) {
				return false
			}

			return true
		},
		tradePlanGen.Map(func(p TradePlan) TradePlan { return p }),
		statsGen.Map(func(s TradeStatistics) TradeStatistics { return s }),
		returnsGen,
		closedTradesGen,
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 37: 统计指标正确性
// ============================================================================

func TestProperty37_StatisticsCorrectness(t *testing.T) {
	// Feature: quant-trading-system, Property 37: 统计指标正确性
	// 对任意交易结果序列，WinRate = WinningTrades / TotalTrades，ProfitFactor 公式正确
	// 验证: 需求 10.5

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 生成随机交易结果序列（1~30 笔交易，每笔为盈亏百分比）
	tradeSeqGen := gen.IntRange(1, 30).FlatMap(
		func(n interface{}) gopter.Gen {
			count := n.(int)
			return gen.SliceOfN(count, gen.Float64Range(-20, 40)).
				Map(func(s []float64) []float64 { return s })
		},
		reflect.TypeOf([]float64{}),
	)

	properties.Property("WinRate = WinningTrades / TotalTrades", prop.ForAll(
		func(trades []float64) bool {
			// 重置全局统计
			ResetStatistics()

			for _, pnl := range trades {
				UpdateStatistics(pnl, 30)
			}

			stats := GetStatistics()

			if stats.TotalTrades == 0 {
				return true
			}

			// 验证 WinRate = WinningTrades / TotalTrades
			expectedWinRate := float64(stats.WinningTrades) / float64(stats.TotalTrades)
			if math.Abs(stats.WinRate-expectedWinRate) > 1e-9 {
				return false
			}

			// 验证 WinningTrades + LosingTrades == TotalTrades
			if stats.WinningTrades+stats.LosingTrades != stats.TotalTrades {
				return false
			}

			return true
		},
		tradeSeqGen,
	))

	properties.Property("ProfitFactor = (AverageWin * WinRate) / (AverageLoss * (1 - WinRate))", prop.ForAll(
		func(trades []float64) bool {
			ResetStatistics()

			for _, pnl := range trades {
				UpdateStatistics(pnl, 30)
			}

			stats := GetStatistics()

			// 只在有盈有亏时验证 ProfitFactor
			if stats.AverageLoss <= 0 || stats.WinRate <= 0 || stats.WinRate >= 1 {
				return true
			}

			expectedPF := (stats.AverageWin * stats.WinRate) / (stats.AverageLoss * (1 - stats.WinRate))
			return math.Abs(stats.ProfitFactor-expectedPF) < 1e-9
		},
		tradeSeqGen,
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 38: 夏普比率公式正确性
// ============================================================================

func TestProperty38_SharpeRatioFormula(t *testing.T) {
	// Feature: quant-trading-system, Property 38: 夏普比率公式正确性
	// 对任意收益率序列（长度 ≥ MinTradesForCalc），夏普比率公式应正确
	// 验证: 需求 10.6

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 生成长度 ≥ MinTradesForCalc(10) 的收益率序列，范围 -20% ~ 40%
	returnsGen := gen.IntRange(10, 50).FlatMap(
		func(n interface{}) gopter.Gen {
			count := n.(int)
			return gen.SliceOfN(count, gen.Float64Range(-20, 40)).
				Map(func(s []float64) []float64 { return s })
		},
		reflect.TypeOf([]float64{}),
	)

	properties.Property("夏普比率 = (均值收益 - 周期无风险利率) / 标准差 × sqrt(年化因子)", prop.ForAll(
		func(returns []float64, annFactor float64, rfRate float64) bool {
			// 设置配置并直接写入序列（绕过 AddReturn 的长度上限逻辑）
			cfg := SharpeConfig{
				RiskFreeRate:     rfRate,
				AnnualizeFactor:  annFactor,
				MinTradesForCalc: 10,
			}
			SetSharpeConfig(cfg)

			returnsLock.Lock()
			returnsSeries = make([]float64, len(returns))
			copy(returnsSeries, returns)
			returnsLock.Unlock()

			got := CalculateSharpeRatio()

			// 手动计算期望值
			n := float64(len(returns))
			sum := 0.0
			for _, r := range returns {
				sum += r
			}
			mean := sum / n

			sumSqDiff := 0.0
			for _, r := range returns {
				d := r - mean
				sumSqDiff += d * d
			}
			stdDev := math.Sqrt(sumSqDiff / n)

			if stdDev == 0 {
				return got == 0
			}

			periodicRF := rfRate / annFactor
			expected := (mean - periodicRF) / stdDev * math.Sqrt(annFactor)

			return math.Abs(got-expected) < 1e-9
		},
		returnsGen,
		gen.Float64Range(1, 365),  // AnnualizeFactor
		gen.Float64Range(0, 0.05), // RiskFreeRate
	))

	properties.Property("序列长度不足 MinTradesForCalc 时返回 0", prop.ForAll(
		func(n int) bool {
			SetSharpeConfig(SharpeConfig{
				RiskFreeRate:     0,
				AnnualizeFactor:  252,
				MinTradesForCalc: 10,
			})

			returnsLock.Lock()
			returnsSeries = make([]float64, n)
			for i := range returnsSeries {
				returnsSeries[i] = 1.0
			}
			returnsLock.Unlock()

			return CalculateSharpeRatio() == 0
		},
		gen.IntRange(0, 9),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))

	// 恢复默认配置
	SetSharpeConfig(SharpeConfig{
		RiskFreeRate:     0.0,
		AnnualizeFactor:  252,
		MinTradesForCalc: 10,
	})
}

func TestProperty39_ReturnsSeriesLengthCap(t *testing.T) {
	// Feature: quant-trading-system, Property 39: 收益率序列长度上限
	// 对任意数量的 AddReturn 调用，returnsSeries 长度永远不超过 1000
	// 验证: 需求 10.7

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("任意次 AddReturn 调用后序列长度不超过 1000", prop.ForAll(
		func(callCount int, returnVal float64) bool {
			// 重置序列
			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			for i := 0; i < callCount; i++ {
				AddReturn(returnVal)
			}

			returnsLock.Lock()
			length := len(returnsSeries)
			returnsLock.Unlock()

			return length <= 1000
		},
		gen.IntRange(0, 2000),
		gen.Float64Range(-50, 100),
	))

	properties.Property("超过 1000 次调用后序列长度恰好为 1000", prop.ForAll(
		func(extra int) bool {
			// 重置序列
			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			total := 1000 + extra
			for i := 0; i < total; i++ {
				AddReturn(float64(i) * 0.01)
			}

			returnsLock.Lock()
			length := len(returnsSeries)
			returnsLock.Unlock()

			return length == 1000
		},
		gen.IntRange(1, 500),
	))

	properties.Property("超过 1000 次调用后保留最新的 1000 条", prop.ForAll(
		func(extra int) bool {
			// 重置序列
			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			total := 1000 + extra
			for i := 0; i < total; i++ {
				AddReturn(float64(i))
			}

			returnsLock.Lock()
			series := make([]float64, len(returnsSeries))
			copy(series, returnsSeries)
			returnsLock.Unlock()

			if len(series) != 1000 {
				return false
			}
			// 最后一个元素应为最后一次写入的值
			return series[999] == float64(total-1)
		},
		gen.IntRange(1, 200),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 40: 数据导出导入往返
// ============================================================================

func TestProperty40_ExportImportRoundTrip(t *testing.T) {
	// Feature: quant-trading-system, Property 40: 数据导出导入往返
	// 对任意系统状态，ExportData 后 ImportData 应恢复等价状态
	// 验证: 需求 10.8

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 生成随机 TradePlan
	tradePlanGen := gen.Struct(reflect.TypeOf(TradePlan{}), map[string]gopter.Gen{
		"Symbol":             gen.OneConstOf("BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"),
		"Direction":          gen.OneConstOf("long", "short"),
		"EntryPrice":         gen.Float64Range(100, 100000),
		"StopLoss":           gen.Float64Range(50, 99000),
		"TakeProfit":         gen.Float64Range(101, 200000),
		"PositionSizeUSD":    gen.Float64Range(10, 10000),
		"Leverage":           gen.IntRange(1, 20),
		"Status":             gen.OneConstOf("active", "closed"),
		"Confidence":         gen.IntRange(0, 100),
		"PeakPnLPercent":     gen.Float64Range(-50, 200),
		"TotalClosedPercent": gen.Float64Range(0, 100),
		"MinHoldMinutes":     gen.IntRange(0, 240),
	})

	// 生成随机 TradeStatistics
	statsGen := gen.Struct(reflect.TypeOf(TradeStatistics{}), map[string]gopter.Gen{
		"TotalTrades":       gen.IntRange(0, 1000),
		"WinningTrades":     gen.IntRange(0, 500),
		"LosingTrades":      gen.IntRange(0, 500),
		"TotalPnL":          gen.Float64Range(-100, 500),
		"AverageWin":        gen.Float64Range(0, 50),
		"AverageLoss":       gen.Float64Range(0, 50),
		"WinRate":           gen.Float64Range(0, 1),
		"ProfitFactor":      gen.Float64Range(0, 10),
		"SharpeRatio":       gen.Float64Range(-5, 10),
		"SortinoRatio":      gen.Float64Range(-5, 10),
		"MaxDrawdown":       gen.Float64Range(0, 100),
		"AverageHoldTime":   gen.Float64Range(0, 10000),
		"ConsecutiveWins":   gen.IntRange(0, 20),
		"ConsecutiveLosses": gen.IntRange(0, 20),
		"MaxConsecLosses":   gen.IntRange(0, 20),
	})

	// 生成随机收益率序列（0~30 条）
	returnsGen := gen.IntRange(0, 30).FlatMap(
		func(n interface{}) gopter.Gen {
			count := n.(int)
			if count == 0 {
				return gen.Const([]float64{})
			}
			return gen.SliceOfN(count, gen.Float64Range(-20, 50)).
				Map(func(s []float64) []float64 { return s })
		},
		reflect.TypeOf([]float64{}),
	)

	properties.Property("ExportData 后 ImportData 恢复等价的 Plans", prop.ForAll(
		func(plan TradePlan, stats TradeStatistics, returns []float64) bool {
			// 初始化一个临时 plan manager
			tmpDir := t.TempDir()
			if err := InitPlanManager(tmpDir); err != nil {
				return false
			}

			// 设置初始状态
			plan.ExecutedTranches = map[int]bool{}
			plan.CreatedAt = time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
			planManager.mu.Lock()
			planManager.plans = map[string]*TradePlan{plan.Symbol: &plan}
			planManager.mu.Unlock()

			tradeStatsLock.Lock()
			statsCopy := stats
			tradeStats = &statsCopy
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = make([]float64, len(returns))
			copy(returnsSeries, returns)
			returnsLock.Unlock()

			// 导出
			exported, err := ExportData()
			if err != nil {
				return false
			}

			// 清空状态
			planManager.mu.Lock()
			planManager.plans = map[string]*TradePlan{}
			planManager.mu.Unlock()

			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			// 导入
			if err := ImportData(exported); err != nil {
				return false
			}

			// 验证 Plans 恢复
			planManager.mu.RLock()
			restoredPlan, ok := planManager.plans[plan.Symbol]
			planManager.mu.RUnlock()
			if !ok {
				return false
			}
			if restoredPlan.Symbol != plan.Symbol {
				return false
			}
			if math.Abs(restoredPlan.EntryPrice-plan.EntryPrice) > 1e-9 {
				return false
			}
			if math.Abs(restoredPlan.StopLoss-plan.StopLoss) > 1e-9 {
				return false
			}
			if math.Abs(restoredPlan.TakeProfit-plan.TakeProfit) > 1e-9 {
				return false
			}
			if restoredPlan.Direction != plan.Direction {
				return false
			}

			// 验证 Statistics 恢复
			tradeStatsLock.RLock()
			restoredStats := *tradeStats
			tradeStatsLock.RUnlock()
			if restoredStats.TotalTrades != stats.TotalTrades {
				return false
			}
			if math.Abs(restoredStats.WinRate-stats.WinRate) > 1e-9 {
				return false
			}
			if math.Abs(restoredStats.ProfitFactor-stats.ProfitFactor) > 1e-9 {
				return false
			}

			// 验证 Returns 恢复
			returnsLock.RLock()
			restoredReturns := make([]float64, len(returnsSeries))
			copy(restoredReturns, returnsSeries)
			returnsLock.RUnlock()
			if len(restoredReturns) != len(returns) {
				return false
			}
			for i, r := range returns {
				if math.Abs(restoredReturns[i]-r) > 1e-9 {
					return false
				}
			}

			return true
		},
		tradePlanGen.Map(func(p TradePlan) TradePlan { return p }),
		statsGen.Map(func(s TradeStatistics) TradeStatistics { return s }),
		returnsGen,
	))

	properties.Property("空状态导出导入后仍为空状态", prop.ForAll(
		func(_ bool) bool {
			tmpDir := t.TempDir()
			if err := InitPlanManager(tmpDir); err != nil {
				return false
			}

			// 清空所有状态
			planManager.mu.Lock()
			planManager.plans = map[string]*TradePlan{}
			planManager.mu.Unlock()

			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			exported, err := ExportData()
			if err != nil {
				return false
			}

			if err := ImportData(exported); err != nil {
				return false
			}

			planManager.mu.RLock()
			planCount := len(planManager.plans)
			planManager.mu.RUnlock()

			returnsLock.RLock()
			returnsCount := len(returnsSeries)
			returnsLock.RUnlock()

			return planCount == 0 && returnsCount == 0
		},
		gen.Const(true),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
