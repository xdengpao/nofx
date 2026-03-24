package decision

// Feature: circuit-breaker-auto-recovery, Property 2: Preservation
// 正常交易流程 + 熔断触发逻辑 + 统计更新 + 持久化兼容性
//
// IMPORTANT: 遵循观察优先方法论
// 观察阶段（在未修复代码上运行）:
//   - 无熔断条件时 CheckCircuitBreaker 返回 IsTriggered == false
//   - BTC 1h 跌 >5% 首次触发时 CooldownMinutes == 120
//   - 连续亏损 >=5 首次触发时 CooldownMinutes == 30
//   - UpdateStatistics 盈利时 ConsecutiveLosses = 0, ConsecutiveWins++
//   - UpdateStatistics 亏损时 ConsecutiveLosses++, ConsecutiveWins = 0
//   - saveToFile/loadFromFile 正确保存恢复 Plans、Statistics、Returns、ClosedTrades

import (
	"encoding/json"
	"math"
	"nofx/market"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 属性 2a — 正常交易流程不受影响 (Property 4)
// ============================================================================
//
// 生成随机市场条件（BTC 价格变化 > -5%、账户回撤 > -10%、连续亏损 0-4、
// 保证金使用率 0-89%），验证 CheckCircuitBreaker 返回 IsTriggered == false

func TestProperty2a_NormalTradingUnaffected(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("正常条件: CheckCircuitBreaker 不触发熔断", prop.ForAll(
		func(btcChange float64, pnlPct float64, consecLosses int, marginPct float64) bool {
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   pnlPct,
					MarginUsedPct: marginPct,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: btcChange},
				},
			}

			stats := &TradeStatistics{
				ConsecutiveLosses: consecLosses,
			}

			result := CheckCircuitBreaker(ctx, stats)

			if result.IsTriggered {
				t.Logf("反例: btcChange=%.2f%%, pnlPct=%.2f%%, consecLosses=%d, marginPct=%.2f%% → IsTriggered=true",
					btcChange, pnlPct, consecLosses, marginPct)
				return false
			}
			return true
		},
		gen.Float64Range(-4.99, 5.0),  // BTC 价格变化 > -5%
		gen.Float64Range(-9.99, 10.0), // 账户回撤 > -10%
		gen.IntRange(0, 4),            // 连续亏损 0-4 (< MaxConsecutiveLosses=5)
		gen.Float64Range(0.0, 89.99),  // 保证金使用率 0-89% (< 90%)
	))

	properties.TestingRun(t)
}


// ============================================================================
// 属性 2b — 熔断触发逻辑不变 (Property 5)
// ============================================================================
//
// 生成随机首次触发场景（BTC 暴跌/账户回撤/连续亏损/保证金过高），
// 验证触发后 IsTriggered == true 且冷却时间正确
// BTC暴跌/账户回撤: 120 分钟，连续亏损/保证金过高: 30 分钟

func TestProperty2b_TriggerLogicPreserved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 子属性 2b-1: BTC 暴跌触发 → CooldownMinutes == 120
	properties.Property("BTC暴跌触发: IsTriggered=true, CooldownMinutes=120", prop.ForAll(
		func(btcDrop float64) bool {
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   -3.0,
					MarginUsedPct: 50.0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: btcDrop},
				},
			}
			stats := &TradeStatistics{ConsecutiveLosses: 0}

			result := CheckCircuitBreaker(ctx, stats)

			if !result.IsTriggered {
				t.Logf("反例: btcDrop=%.2f%% → IsTriggered=false (期望 true)", btcDrop)
				return false
			}
			if result.CooldownMinutes != 120 {
				t.Logf("反例: btcDrop=%.2f%% → CooldownMinutes=%d (期望 120)", btcDrop, result.CooldownMinutes)
				return false
			}
			return true
		},
		gen.Float64Range(-20.0, -5.01), // BTC 1h 跌幅 > 5%
	))

	// 子属性 2b-2: 账户回撤触发 → CooldownMinutes == 120
	properties.Property("账户回撤触发: IsTriggered=true, CooldownMinutes=120", prop.ForAll(
		func(drawdown float64) bool {
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   drawdown,
					MarginUsedPct: 50.0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: -1.0}, // BTC 正常
				},
			}
			stats := &TradeStatistics{ConsecutiveLosses: 0}

			result := CheckCircuitBreaker(ctx, stats)

			if !result.IsTriggered {
				t.Logf("反例: drawdown=%.2f%% → IsTriggered=false (期望 true)", drawdown)
				return false
			}
			if result.CooldownMinutes != 120 {
				t.Logf("反例: drawdown=%.2f%% → CooldownMinutes=%d (期望 120)", drawdown, result.CooldownMinutes)
				return false
			}
			return true
		},
		gen.Float64Range(-50.0, -10.01), // 账户回撤 > 10%
	))

	// 子属性 2b-3: 连续亏损触发 → CooldownMinutes == 30
	properties.Property("连续亏损触发: IsTriggered=true, CooldownMinutes=30", prop.ForAll(
		func(losses int) bool {
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   -3.0,
					MarginUsedPct: 50.0,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: -1.0},
				},
			}
			stats := &TradeStatistics{ConsecutiveLosses: losses}

			result := CheckCircuitBreaker(ctx, stats)

			if !result.IsTriggered {
				t.Logf("反例: losses=%d → IsTriggered=false (期望 true)", losses)
				return false
			}
			if result.CooldownMinutes != 30 {
				t.Logf("反例: losses=%d → CooldownMinutes=%d (期望 30)", losses, result.CooldownMinutes)
				return false
			}
			return true
		},
		gen.IntRange(5, 15), // 连续亏损 >= 5
	))

	// 子属性 2b-4: 保证金过高触发 → CooldownMinutes == 30
	properties.Property("保证金过高触发: IsTriggered=true, CooldownMinutes=30", prop.ForAll(
		func(marginPct float64) bool {
			ctx := &Context{
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   -3.0,
					MarginUsedPct: marginPct,
				},
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: -1.0},
				},
			}
			stats := &TradeStatistics{ConsecutiveLosses: 0}

			result := CheckCircuitBreaker(ctx, stats)

			if !result.IsTriggered {
				t.Logf("反例: marginPct=%.2f%% → IsTriggered=false (期望 true)", marginPct)
				return false
			}
			if result.CooldownMinutes != 30 {
				t.Logf("反例: marginPct=%.2f%% → CooldownMinutes=%d (期望 30)", marginPct, result.CooldownMinutes)
				return false
			}
			return true
		},
		gen.Float64Range(90.01, 100.0), // 保证金使用率 > 90%
	))

	properties.TestingRun(t)
}


// ============================================================================
// 属性 2c — 统计更新逻辑不变 (Property 6)
// ============================================================================
//
// 生成随机交易结果（盈亏百分比 -50% 到 +100%），验证 UpdateStatistics
// 正确更新 ConsecutiveLosses、ConsecutiveWins、WinRate、ProfitFactor

func TestProperty2c_StatisticsUpdatePreserved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 子属性 2c-1: 盈利交易 → ConsecutiveLosses=0, ConsecutiveWins++
	properties.Property("盈利交易: ConsecutiveLosses=0, ConsecutiveWins++", prop.ForAll(
		func(pnlPct float64, prevConsecWins int, prevConsecLosses int) bool {
			// 重置全局统计状态
			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{
				ConsecutiveWins:   prevConsecWins,
				ConsecutiveLosses: prevConsecLosses,
			}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = []float64{}
			returnsLock.Unlock()

			UpdateStatistics(pnlPct, 30.0)

			tradeStatsLock.RLock()
			consecLosses := tradeStats.ConsecutiveLosses
			consecWins := tradeStats.ConsecutiveWins
			tradeStatsLock.RUnlock()

			if consecLosses != 0 {
				t.Logf("反例: pnl=+%.2f%%, prevWins=%d, prevLosses=%d → ConsecutiveLosses=%d (期望 0)",
					pnlPct, prevConsecWins, prevConsecLosses, consecLosses)
				return false
			}
			if consecWins != prevConsecWins+1 {
				t.Logf("反例: pnl=+%.2f%%, prevWins=%d → ConsecutiveWins=%d (期望 %d)",
					pnlPct, prevConsecWins, consecWins, prevConsecWins+1)
				return false
			}
			return true
		},
		gen.Float64Range(0.01, 100.0), // 盈利百分比
		gen.IntRange(0, 10),           // 之前连续盈利次数
		gen.IntRange(0, 10),           // 之前连续亏损次数
	))

	// 子属性 2c-2: 亏损交易 → ConsecutiveLosses++, ConsecutiveWins=0
	properties.Property("亏损交易: ConsecutiveLosses++, ConsecutiveWins=0", prop.ForAll(
		func(pnlPct float64, prevConsecWins int, prevConsecLosses int) bool {
			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{
				ConsecutiveWins:   prevConsecWins,
				ConsecutiveLosses: prevConsecLosses,
			}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = []float64{}
			returnsLock.Unlock()

			UpdateStatistics(pnlPct, 30.0)

			tradeStatsLock.RLock()
			consecLosses := tradeStats.ConsecutiveLosses
			consecWins := tradeStats.ConsecutiveWins
			tradeStatsLock.RUnlock()

			if consecWins != 0 {
				t.Logf("反例: pnl=%.2f%%, prevWins=%d, prevLosses=%d → ConsecutiveWins=%d (期望 0)",
					pnlPct, prevConsecWins, prevConsecLosses, consecWins)
				return false
			}
			if consecLosses != prevConsecLosses+1 {
				t.Logf("反例: pnl=%.2f%%, prevLosses=%d → ConsecutiveLosses=%d (期望 %d)",
					pnlPct, prevConsecLosses, consecLosses, prevConsecLosses+1)
				return false
			}
			return true
		},
		gen.Float64Range(-50.0, -0.01), // 亏损百分比
		gen.IntRange(0, 10),            // 之前连续盈利次数
		gen.IntRange(0, 10),            // 之前连续亏损次数
	))

	// 子属性 2c-3: WinRate 和 ProfitFactor 正确计算
	properties.Property("WinRate 和 ProfitFactor 正确更新", prop.ForAll(
		func(wins int, losses int) bool {
			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = []float64{}
			returnsLock.Unlock()

			// 模拟一系列交易
			for i := 0; i < wins; i++ {
				UpdateStatistics(2.0, 30.0) // 每次盈利 2%
			}
			for i := 0; i < losses; i++ {
				UpdateStatistics(-1.0, 30.0) // 每次亏损 1%
			}

			tradeStatsLock.RLock()
			totalTrades := tradeStats.TotalTrades
			winRate := tradeStats.WinRate
			profitFactor := tradeStats.ProfitFactor
			tradeStatsLock.RUnlock()

			total := wins + losses
			if totalTrades != total {
				t.Logf("反例: wins=%d, losses=%d → TotalTrades=%d (期望 %d)", wins, losses, totalTrades, total)
				return false
			}

			if total > 0 {
				expectedWinRate := float64(wins) / float64(total)
				if math.Abs(winRate-expectedWinRate) > 0.001 {
					t.Logf("反例: wins=%d, losses=%d → WinRate=%.4f (期望 %.4f)", wins, losses, winRate, expectedWinRate)
					return false
				}
			}

			// ProfitFactor 应 > 0 当有盈利和亏损时
			if wins > 0 && losses > 0 && profitFactor <= 0 {
				t.Logf("反例: wins=%d, losses=%d → ProfitFactor=%.4f (期望 > 0)", wins, losses, profitFactor)
				return false
			}

			return true
		},
		gen.IntRange(1, 20), // 盈利次数
		gen.IntRange(1, 20), // 亏损次数
	))

	properties.TestingRun(t)
}


// ============================================================================
// 属性 2d — 现有持久化行为不变 (Property 7)
// ============================================================================
//
// 生成随机 PersistentData（含 Plans、Statistics、Returns、ClosedTrades），
// 验证 saveToFile 后 loadFromFile 正确恢复所有字段

func TestProperty2d_PersistencePreserved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("持久化: saveToFile/loadFromFile 正确保存恢复所有字段", prop.ForAll(
		func(
			totalTrades int,
			winningTrades int,
			totalPnL float64,
			consecWins int,
			consecLosses int,
			numReturns int,
			numClosedTrades int,
		) bool {
			// 创建临时目录
			tmpDir, err := os.MkdirTemp("", "cb_preservation_test_*")
			if err != nil {
				t.Logf("创建临时目录失败: %v", err)
				return false
			}
			defer os.RemoveAll(tmpDir)

			filePath := filepath.Join(tmpDir, "test_plans.json")

			// 构造测试数据
			testPlans := make(map[string]*TradePlan)
			testPlans["BTCUSDT"] = &TradePlan{
				ID:               "test-plan-1",
				Symbol:           "BTCUSDT",
				Direction:        "long",
				EntryPrice:       50000.0,
				StopLoss:         49000.0,
				TakeProfit:       55000.0,
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}
			testPlans["ETHUSDT"] = &TradePlan{
				ID:               "test-plan-2",
				Symbol:           "ETHUSDT",
				Direction:        "short",
				EntryPrice:       3000.0,
				StopLoss:         3100.0,
				TakeProfit:       2700.0,
				Status:           "active",
				ExecutedTranches: make(map[int]bool),
			}

			losingTrades := totalTrades - winningTrades
			if losingTrades < 0 {
				losingTrades = 0
				winningTrades = totalTrades
			}

			testStats := &TradeStatistics{
				TotalTrades:       totalTrades,
				WinningTrades:     winningTrades,
				LosingTrades:      losingTrades,
				TotalPnL:          totalPnL,
				ConsecutiveWins:   consecWins,
				ConsecutiveLosses: consecLosses,
				WinRate:           0.6,
				ProfitFactor:      1.5,
				LastUpdated:       time.Now().Truncate(time.Second),
			}

			testReturns := make([]float64, numReturns)
			for i := range testReturns {
				testReturns[i] = float64(i)*0.5 - 2.0
			}

			testClosedTrades := make([]ClosedTradeRecord, numClosedTrades)
			for i := range testClosedTrades {
				testClosedTrades[i] = ClosedTradeRecord{
					Symbol:     "BTCUSDT",
					Side:       "long",
					PnLPercent: float64(i) * 0.3,
					ClosedAt:   time.Now().Truncate(time.Second),
				}
			}

			// 保存原始全局状态
			tradeStatsLock.Lock()
			origStats := tradeStats
			tradeStats = testStats
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			origReturns := returnsSeries
			returnsSeries = testReturns
			returnsLock.Unlock()

			closedTradesLock.Lock()
			origClosed := closedTrades
			closedTrades = testClosedTrades
			closedTradesLock.Unlock()

			// 保存
			saveMgr := &TradePlanManager{
				plans:    testPlans,
				filePath: filePath,
				autoSave: false,
			}

			if err := saveMgr.saveToFile(); err != nil {
				t.Logf("saveToFile 失败: %v", err)
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}

			// 重置全局状态
			tradeStatsLock.Lock()
			tradeStats = &TradeStatistics{}
			tradeStatsLock.Unlock()

			returnsLock.Lock()
			returnsSeries = nil
			returnsLock.Unlock()

			closedTradesLock.Lock()
			closedTrades = nil
			closedTradesLock.Unlock()

			// 加载
			loadMgr := &TradePlanManager{
				plans:    make(map[string]*TradePlan),
				filePath: filePath,
				autoSave: false,
			}

			if err := loadMgr.loadFromFile(); err != nil {
				t.Logf("loadFromFile 失败: %v", err)
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}

			// 验证 Plans
			if len(loadMgr.plans) != len(testPlans) {
				t.Logf("反例: Plans 数量 loaded=%d, expected=%d", len(loadMgr.plans), len(testPlans))
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}
			for symbol, origPlan := range testPlans {
				loadedPlan, ok := loadMgr.plans[symbol]
				if !ok {
					t.Logf("反例: Plan %s 未加载", symbol)
					restoreGlobals(origStats, origReturns, origClosed)
					return false
				}
				if loadedPlan.EntryPrice != origPlan.EntryPrice || loadedPlan.Direction != origPlan.Direction {
					t.Logf("反例: Plan %s 数据不匹配", symbol)
					restoreGlobals(origStats, origReturns, origClosed)
					return false
				}
			}

			// 验证 Statistics
			tradeStatsLock.RLock()
			loadedStats := *tradeStats
			tradeStatsLock.RUnlock()

			if loadedStats.TotalTrades != testStats.TotalTrades ||
				loadedStats.WinningTrades != testStats.WinningTrades ||
				loadedStats.ConsecutiveWins != testStats.ConsecutiveWins ||
				loadedStats.ConsecutiveLosses != testStats.ConsecutiveLosses {
				t.Logf("反例: Statistics 不匹配 — loaded.TotalTrades=%d expected=%d",
					loadedStats.TotalTrades, testStats.TotalTrades)
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}

			// 验证 Returns
			returnsLock.RLock()
			loadedReturns := make([]float64, len(returnsSeries))
			copy(loadedReturns, returnsSeries)
			returnsLock.RUnlock()

			if len(loadedReturns) != len(testReturns) {
				t.Logf("反例: Returns 长度 loaded=%d, expected=%d", len(loadedReturns), len(testReturns))
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}

			// 验证 ClosedTrades
			closedTradesLock.RLock()
			loadedClosed := make([]ClosedTradeRecord, len(closedTrades))
			copy(loadedClosed, closedTrades)
			closedTradesLock.RUnlock()

			if len(loadedClosed) != len(testClosedTrades) {
				t.Logf("反例: ClosedTrades 长度 loaded=%d, expected=%d", len(loadedClosed), len(testClosedTrades))
				restoreGlobals(origStats, origReturns, origClosed)
				return false
			}

			// 恢复原始全局状态
			restoreGlobals(origStats, origReturns, origClosed)
			return true
		},
		gen.IntRange(1, 50),             // totalTrades
		gen.IntRange(0, 50),             // winningTrades
		gen.Float64Range(-100.0, 100.0), // totalPnL
		gen.IntRange(0, 10),             // consecWins
		gen.IntRange(0, 10),             // consecLosses
		gen.IntRange(0, 20),             // numReturns
		gen.IntRange(0, 10),             // numClosedTrades
	))

	properties.TestingRun(t)
}

// restoreGlobals 恢复全局状态（测试清理辅助函数）
func restoreGlobals(stats *TradeStatistics, returns []float64, closed []ClosedTradeRecord) {
	tradeStatsLock.Lock()
	tradeStats = stats
	tradeStatsLock.Unlock()

	returnsLock.Lock()
	returnsSeries = returns
	returnsLock.Unlock()

	closedTradesLock.Lock()
	closedTrades = closed
	closedTradesLock.Unlock()
}

// ============================================================================
// 辅助: 验证 JSON 序列化往返一致性
// ============================================================================

func TestProperty2d_JSONRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("JSON 序列化/反序列化: PersistentData 字段完整", prop.ForAll(
		func(totalTrades int, pnl float64, numReturns int) bool {
			original := PersistentData{
				Plans: map[string]*TradePlan{
					"BTCUSDT": {
						ID:               "plan-1",
						Symbol:           "BTCUSDT",
						EntryPrice:       50000,
						ExecutedTranches: make(map[int]bool),
					},
				},
				Statistics: &TradeStatistics{
					TotalTrades: totalTrades,
					TotalPnL:    pnl,
				},
				Returns:   make([]float64, numReturns),
				UpdatedAt: time.Now().Truncate(time.Second),
			}

			for i := range original.Returns {
				original.Returns[i] = float64(i) * 0.1
			}

			data, err := json.Marshal(original)
			if err != nil {
				t.Logf("序列化失败: %v", err)
				return false
			}

			var loaded PersistentData
			if err := json.Unmarshal(data, &loaded); err != nil {
				t.Logf("反序列化失败: %v", err)
				return false
			}

			// 验证关键字段
			if len(loaded.Plans) != len(original.Plans) {
				t.Logf("反例: Plans 数量不匹配 loaded=%d expected=%d", len(loaded.Plans), len(original.Plans))
				return false
			}
			if loaded.Statistics == nil || loaded.Statistics.TotalTrades != totalTrades {
				t.Logf("反例: Statistics.TotalTrades 不匹配")
				return false
			}
			if len(loaded.Returns) != numReturns {
				t.Logf("反例: Returns 长度不匹配 loaded=%d expected=%d", len(loaded.Returns), numReturns)
				return false
			}

			return true
		},
		gen.IntRange(0, 100),            // totalTrades
		gen.Float64Range(-100.0, 100.0), // pnl
		gen.IntRange(0, 50),             // numReturns
	))

	properties.TestingRun(t)
}
