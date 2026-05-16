package manager

import (
	"nofx/config"
	"nofx/decision"
	"nofx/trader"
	"sort"
	"sync"
	"testing"
	"time"
)

// 辅助函数：创建测试用的 TraderConfig
func makeTestTraderConfig(id, name string) config.TraderConfig {
	return config.TraderConfig{
		ID:               id,
		Name:             name,
		Enabled:          true,
		AIModel:          "deepseek",
		Exchange:         "binance",
		BinanceAPIKey:    "fake-api-key-" + id,
		BinanceSecretKey: "fake-secret-key-" + id,
		DeepSeekKey:      "fake-deepseek-key-" + id,
		InitialBalance:   1000.0,
	}
}

// 默认杠杆配置
var defaultLeverage = config.LeverageConfig{
	BTCETHLeverage:  5,
	AltcoinLeverage: 5,
}

// --- 14.1: TraderManager 支持并发管理多个 AutoTrader 实例 ---

func TestNewTraderManager(t *testing.T) {
	tm := NewTraderManager()
	if tm == nil {
		t.Fatal("NewTraderManager 返回 nil")
	}
	if len(tm.GetAllTraders()) != 0 {
		t.Errorf("新建 TraderManager 应无 trader，实际有 %d 个", len(tm.GetAllTraders()))
	}
	if len(tm.GetTraderIDs()) != 0 {
		t.Errorf("新建 TraderManager 应无 trader ID，实际有 %d 个", len(tm.GetTraderIDs()))
	}
	// 清理
	tm.StopOrderTracking()
}

func TestAddMultipleTraders(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	ids := []string{"trader-1", "trader-2", "trader-3"}
	for _, id := range ids {
		cfg := makeTestTraderConfig(id, "Trader "+id)
		err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage)
		if err != nil {
			t.Fatalf("添加 trader %s 失败: %v", id, err)
		}
	}

	allTraders := tm.GetAllTraders()
	if len(allTraders) != 3 {
		t.Errorf("期望 3 个 trader，实际 %d 个", len(allTraders))
	}

	traderIDs := tm.GetTraderIDs()
	if len(traderIDs) != 3 {
		t.Errorf("期望 3 个 trader ID，实际 %d 个", len(traderIDs))
	}

	// 验证每个 ID 都存在
	sort.Strings(traderIDs)
	sort.Strings(ids)
	for i, id := range ids {
		if traderIDs[i] != id {
			t.Errorf("期望 ID %s，实际 %s", id, traderIDs[i])
		}
	}
}

func TestAddTrader_DuplicateID(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("dup-trader", "Dup Trader")
	err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage)
	if err != nil {
		t.Fatalf("首次添加失败: %v", err)
	}

	// 重复添加同一 ID 应返回错误
	err = tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage)
	if err == nil {
		t.Error("重复添加同一 ID 应返回错误")
	}
}

func TestAddTraderWithFrequency_PassesPolicy(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("freq-trader", "Frequency Trader")
	profile := config.TradingFrequencyProfile{
		Mode:                    config.TradingFrequencyModeBalanced,
		EffectiveMode:           config.TradingFrequencyModeBalanced,
		AnalysisIntervalMinutes: 12,
		PromptCandidateLimit:    10,
		HighADXReportOnly:       true,
		RRReportOnly:            true,
		RollingGateReportOnly:   true,
	}
	if err := tm.AddTraderWithFrequency(cfg, "", 10.0, 20.0, 30, defaultLeverage, profile); err != nil {
		t.Fatalf("添加带frequency policy的trader失败: %v", err)
	}
	at, err := tm.GetTrader("freq-trader")
	if err != nil {
		t.Fatalf("获取trader失败: %v", err)
	}
	status := at.GetStatus()
	policy, ok := status["frequency_policy"].(decision.FrequencyPolicy)
	if !ok {
		t.Fatalf("status应输出frequency_policy: %+v", status)
	}
	if policy.Mode != config.TradingFrequencyModeBalanced || policy.AnalysisIntervalMin != 12 || policy.PromptCandidateLimit != 10 {
		t.Fatalf("frequency_policy派生错误: %+v", policy)
	}
}

func TestAddTraderWithPolicies_PassesStrategyRiskPolicy(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("risk-trader", "Risk Trader")
	frequency := config.TradingFrequencyProfile{
		Mode:                    config.TradingFrequencyModeSafe,
		EffectiveMode:           config.TradingFrequencyModeSafe,
		AnalysisIntervalMinutes: 15,
		PromptCandidateLimit:    8,
	}
	strategyRisk := config.StrategyRiskProfile{
		Enabled:         true,
		FeeSlippagePct:  0.002,
		DefaultMinNetRR: 2.5,
		ADXTimeframe:    "1h",
		Profiles: []config.InstrumentProfileProfile{
			{
				Name:                "btc_eth",
				Symbols:             []string{"BTCUSDT", "ETHUSDT"},
				MinStopPct:          0.01,
				FallbackStopPct:     0.015,
				ATRMultiplier:       1.5,
				ATRTimeframe:        "1h",
				MinNetRR:            2.5,
				MaxRiskPct:          0.005,
				AllowLong:           true,
				AllowShort:          true,
				ExchangeFullTPMode:  config.StrategyRiskTPModeAlgorithmicFull,
				ExchangeFullTPMinRR: 2.5,
			},
		},
	}
	if err := tm.AddTraderWithPolicies(cfg, "", 10.0, 20.0, 30, defaultLeverage, frequency, strategyRisk); err != nil {
		t.Fatalf("添加带策略风控policy的trader失败: %v", err)
	}
	at, err := tm.GetTrader("risk-trader")
	if err != nil {
		t.Fatalf("获取trader失败: %v", err)
	}
	status := at.GetStatus()
	summary, ok := status["strategy_risk_policy"].(map[string]interface{})
	if !ok {
		t.Fatalf("status应输出strategy_risk_policy: %+v", status)
	}
	if summary["enabled"] != true || summary["adx_timeframe"] != "1h" || summary["profile_count"] != 1 {
		t.Fatalf("strategy_risk_policy派生错误: %+v", summary)
	}
	defaults, ok := summary["profile_defaults"].([]map[string]interface{})
	if !ok || len(defaults) != 1 {
		t.Fatalf("status应输出profile默认参数: %+v", summary)
	}
	if defaults[0]["exchange_full_tp_mode"] != config.StrategyRiskTPModeAlgorithmicFull || defaults[0]["min_stop_pct"] != 0.01 {
		t.Fatalf("profile默认参数摘要错误: %+v", defaults[0])
	}
}

// --- 14.1: 并发安全性测试 ---

func TestConcurrentAddTraders(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "concurrent-" + string(rune('A'+idx))
			cfg := makeTestTraderConfig(id, "Concurrent "+id)
			if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("并发添加 trader 出错: %v", err)
	}

	if len(tm.GetAllTraders()) != 10 {
		t.Errorf("期望 10 个 trader，实际 %d 个", len(tm.GetAllTraders()))
	}
}

// --- 14.6: GetTrader / GetAllTraders / GetTraderIDs ---

func TestGetTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("test-trader-1", "Test Trader 1")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	at, err := tm.GetTrader("test-trader-1")
	if err != nil {
		t.Fatalf("获取 trader 失败: %v", err)
	}
	if at.GetID() != "test-trader-1" {
		t.Errorf("期望 ID test-trader-1，实际 %s", at.GetID())
	}
	if at.GetName() != "Test Trader 1" {
		t.Errorf("期望名称 Test Trader 1，实际 %s", at.GetName())
	}
}

func TestGetTrader_NotFound(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	_, err := tm.GetTrader("nonexistent")
	if err == nil {
		t.Error("获取不存在的 trader 应返回错误")
	}
}

// --- 14.6: 动态添加和移除 trader ---

func TestRemoveTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("removable", "Removable Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	if len(tm.GetAllTraders()) != 1 {
		t.Fatal("添加后应有 1 个 trader")
	}

	err := tm.RemoveTrader("removable")
	if err != nil {
		t.Fatalf("移除 trader 失败: %v", err)
	}

	if len(tm.GetAllTraders()) != 0 {
		t.Error("移除后应无 trader")
	}

	// 移除后获取应返回错误
	_, err = tm.GetTrader("removable")
	if err == nil {
		t.Error("移除后获取 trader 应返回错误")
	}
}

func TestRemoveTrader_NotFound(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	err := tm.RemoveTrader("nonexistent")
	if err == nil {
		t.Error("移除不存在的 trader 应返回错误")
	}
}

func TestRemoveTrader_AlsoRemovesOrderTracker(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("tracked-trader", "Tracked Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	// 添加后应有 OrderTracker
	_, err := tm.GetOrderTracker("tracked-trader")
	if err != nil {
		t.Fatalf("添加后应有 OrderTracker: %v", err)
	}

	// 移除后 OrderTracker 也应消失
	if err := tm.RemoveTrader("tracked-trader"); err != nil {
		t.Fatalf("移除 trader 失败: %v", err)
	}
	_, err = tm.GetOrderTracker("tracked-trader")
	if err == nil {
		t.Error("移除 trader 后 OrderTracker 也应被移除")
	}
}

// --- 14.2: 每个 trader 获得独立的 OrderTracker ---

func TestEachTraderHasIndependentOrderTracker(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	ids := []string{"ot-1", "ot-2", "ot-3"}
	for _, id := range ids {
		cfg := makeTestTraderConfig(id, "OT "+id)
		if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
			t.Fatalf("添加 trader %s 失败: %v", id, err)
		}
	}

	for _, id := range ids {
		ot, err := tm.GetOrderTracker(id)
		if err != nil {
			t.Errorf("trader %s 应有 OrderTracker: %v", id, err)
		}
		if ot == nil {
			t.Errorf("trader %s 的 OrderTracker 不应为 nil", id)
		}
	}
}

func TestGetOrderTracker_NotFound(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	_, err := tm.GetOrderTracker("nonexistent")
	if err == nil {
		t.Error("获取不存在的 OrderTracker 应返回错误")
	}
}

// --- 14.4: SetAutoCloseCallback ---

func TestSetAutoCloseCallback(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	tm.SetAutoCloseCallback(func(traderID string, order trader.AutoClosedOrder) {
		// 回调在实际自动平仓时才会触发
	})

	// 验证回调已设置
	tm.mu.RLock()
	hasCallback := tm.autoCloseCallback != nil
	tm.mu.RUnlock()

	if !hasCallback {
		t.Error("SetAutoCloseCallback 后回调不应为 nil")
	}
}

// --- 14.5: StartAll / StopAll ---

func TestStartAllStopAll_NoPanic(t *testing.T) {
	tm := NewTraderManager()

	cfg1 := makeTestTraderConfig("start-1", "Start Trader 1")
	cfg2 := makeTestTraderConfig("start-2", "Start Trader 2")
	if err := tm.AddTrader(cfg1, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}
	if err := tm.AddTrader(cfg2, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	// StopAll 不应 panic（即使未 StartAll）
	tm.StopAll()
}

func TestStartAllStopAll_EmptyManager(t *testing.T) {
	tm := NewTraderManager()

	// 空 manager 的 StopAll 不应 panic
	tm.StopAll()
}

// --- SetTrackerInterval ---

func TestSetTrackerInterval(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	tm.SetTrackerInterval(5 * time.Second)

	tm.mu.RLock()
	interval := tm.trackerInterval
	tm.mu.RUnlock()

	if interval != 5*time.Second {
		t.Errorf("期望间隔 5s，实际 %v", interval)
	}
}

// --- 14.3: GetComparisonData 返回所有 trader 的状态 ---

func TestGetComparisonData_ReturnsAllTraders(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	ids := []string{"comp-1", "comp-2"}
	for _, id := range ids {
		cfg := makeTestTraderConfig(id, "Comp "+id)
		if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
			t.Fatalf("添加 trader %s 失败: %v", id, err)
		}
	}

	data, err := tm.GetComparisonData()
	if err != nil {
		t.Fatalf("GetComparisonData 失败: %v", err)
	}

	count, ok := data["count"].(int)
	if !ok {
		t.Fatal("count 字段缺失或类型错误")
	}
	// GetComparisonData 内部调用 GetAccountInfo，fake API key 会导致 API 调用失败
	// 所以 count 可能为 0（因为 continue on error），但数据结构应正确
	traders, ok := data["traders"].([]map[string]interface{})
	if !ok {
		t.Fatal("traders 字段缺失或类型错误")
	}
	if count != len(traders) {
		t.Errorf("count (%d) 与 traders 长度 (%d) 不一致", count, len(traders))
	}
}

func TestGetComparisonData_EmptyManager(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	data, err := tm.GetComparisonData()
	if err != nil {
		t.Fatalf("空 manager 的 GetComparisonData 不应失败: %v", err)
	}

	count, ok := data["count"].(int)
	if !ok || count != 0 {
		t.Errorf("空 manager 的 count 应为 0，实际 %v", data["count"])
	}
}

// --- 14.7: GetTrackingSummary ---

func TestGetTrackingSummary_EmptyManager(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	summary := tm.GetTrackingSummary()
	if summary == nil {
		t.Fatal("GetTrackingSummary 不应返回 nil")
	}

	totalTracked, ok := summary["total_tracked"].(int)
	if !ok || totalTracked != 0 {
		t.Errorf("空 manager 的 total_tracked 应为 0，实际 %v", summary["total_tracked"])
	}
}

func TestGetTrackingSummary_WithTraders(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("summary-1", "Summary Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	summary := tm.GetTrackingSummary()
	traders, ok := summary["traders"].([]map[string]interface{})
	if !ok {
		t.Fatal("traders 字段缺失或类型错误")
	}
	if len(traders) != 1 {
		t.Errorf("期望 1 个 trader summary，实际 %d 个", len(traders))
	}
}

// --- 订单追踪操作: TrackNewPosition, UpdateStopLossOrderID, UpdateTakeProfitOrderID, StopTracking ---

func TestTrackNewPosition(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("track-1", "Track Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	err := tm.TrackNewPosition("track-1", "BTCUSDT", "long", 12345, 50000.0, 0.01, 10)
	if err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	// 验证追踪摘要
	summary := tm.GetTrackingSummary()
	totalTracked := summary["total_tracked"].(int)
	if totalTracked != 1 {
		t.Errorf("期望 1 个追踪订单，实际 %d 个", totalTracked)
	}
}

func TestTrackNewPosition_NonexistentTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	err := tm.TrackNewPosition("nonexistent", "BTCUSDT", "long", 12345, 50000.0, 0.01, 10)
	if err == nil {
		t.Error("对不存在的 trader 追踪应返回错误")
	}
}

func TestUpdateStopLossOrderID(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("sl-1", "SL Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	// 先追踪一个仓位
	if err := tm.TrackNewPosition("sl-1", "ETHUSDT", "long", 100, 3000.0, 1.0, 5); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	// 更新止损订单 ID
	err := tm.UpdateStopLossOrderID("sl-1", "ETHUSDT", "long", 200)
	if err != nil {
		t.Fatalf("UpdateStopLossOrderID 失败: %v", err)
	}
}

func TestUpdateStopLossOrderID_NonexistentTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	err := tm.UpdateStopLossOrderID("nonexistent", "ETHUSDT", "long", 200)
	if err == nil {
		t.Error("对不存在的 trader 更新止损应返回错误")
	}
}

func TestUpdateTakeProfitOrderID(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("tp-1", "TP Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	if err := tm.TrackNewPosition("tp-1", "SOLUSDT", "short", 300, 150.0, 10.0, 3); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	err := tm.UpdateTakeProfitOrderID("tp-1", "SOLUSDT", "short", 400)
	if err != nil {
		t.Fatalf("UpdateTakeProfitOrderID 失败: %v", err)
	}
}

func TestUpdateTakeProfitOrderID_NonexistentTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	err := tm.UpdateTakeProfitOrderID("nonexistent", "SOLUSDT", "short", 400)
	if err == nil {
		t.Error("对不存在的 trader 更新止盈应返回错误")
	}
}

func TestStopTracking(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("stop-track-1", "Stop Track Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	if err := tm.TrackNewPosition("stop-track-1", "BTCUSDT", "long", 500, 50000.0, 0.01, 10); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	// 验证追踪中
	summary := tm.GetTrackingSummary()
	if summary["total_tracked"].(int) != 1 {
		t.Fatal("追踪前应有 1 个订单")
	}

	// 停止追踪
	err := tm.StopTracking("stop-track-1", "BTCUSDT", "long")
	if err != nil {
		t.Fatalf("StopTracking 失败: %v", err)
	}

	// 验证已停止
	summary = tm.GetTrackingSummary()
	if summary["total_tracked"].(int) != 0 {
		t.Error("停止追踪后应无追踪订单")
	}
}

func TestStopTracking_NonexistentTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	err := tm.StopTracking("nonexistent", "BTCUSDT", "long")
	if err == nil {
		t.Error("对不存在的 trader 停止追踪应返回错误")
	}
}

// --- StartOrderTracking / StopOrderTracking ---

func TestStartStopOrderTracking_NoPanic(t *testing.T) {
	tm := NewTraderManager()

	cfg := makeTestTraderConfig("ot-track-1", "OT Track Trader")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	// 设置短间隔以便快速测试
	tm.SetTrackerInterval(50 * time.Millisecond)
	tm.StartOrderTracking()

	// 等待几个 tick
	time.Sleep(200 * time.Millisecond)

	// 停止不应 panic
	tm.StopOrderTracking()
}

// --- GetAllTrackedOrders / GetTrackedOrdersForTrader ---

func TestGetAllTrackedOrders(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg1 := makeTestTraderConfig("all-track-1", "All Track 1")
	cfg2 := makeTestTraderConfig("all-track-2", "All Track 2")
	if err := tm.AddTrader(cfg1, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}
	if err := tm.AddTrader(cfg2, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	if err := tm.TrackNewPosition("all-track-1", "BTCUSDT", "long", 1, 50000.0, 0.01, 10); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}
	if err := tm.TrackNewPosition("all-track-2", "ETHUSDT", "short", 2, 3000.0, 1.0, 5); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	allTracked := tm.GetAllTrackedOrders()
	if len(allTracked) != 2 {
		t.Errorf("期望 2 个 trader 的追踪数据，实际 %d 个", len(allTracked))
	}

	if len(allTracked["all-track-1"]) != 1 {
		t.Errorf("all-track-1 应有 1 个追踪订单")
	}
	if len(allTracked["all-track-2"]) != 1 {
		t.Errorf("all-track-2 应有 1 个追踪订单")
	}
}

func TestGetTrackedOrdersForTrader(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	cfg := makeTestTraderConfig("for-trader-1", "For Trader 1")
	if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
		t.Fatalf("添加 trader 失败: %v", err)
	}

	if err := tm.TrackNewPosition("for-trader-1", "BTCUSDT", "long", 1, 50000.0, 0.01, 10); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}
	if err := tm.TrackNewPosition("for-trader-1", "ETHUSDT", "short", 2, 3000.0, 1.0, 5); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	orders, err := tm.GetTrackedOrdersForTrader("for-trader-1")
	if err != nil {
		t.Fatalf("GetTrackedOrdersForTrader 失败: %v", err)
	}
	if len(orders) != 2 {
		t.Errorf("期望 2 个追踪订单，实际 %d 个", len(orders))
	}
}

func TestGetTrackedOrdersForTrader_NotFound(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	_, err := tm.GetTrackedOrdersForTrader("nonexistent")
	if err == nil {
		t.Error("获取不存在 trader 的追踪订单应返回错误")
	}
}

// --- 综合场景：添加、追踪、移除的完整生命周期 ---

func TestFullLifecycle(t *testing.T) {
	tm := NewTraderManager()
	defer tm.StopOrderTracking()

	// 1. 添加多个 trader
	for i := 0; i < 3; i++ {
		id := "lifecycle-" + string(rune('A'+i))
		cfg := makeTestTraderConfig(id, "Lifecycle "+id)
		if err := tm.AddTrader(cfg, "", 10.0, 20.0, 30, defaultLeverage); err != nil {
			t.Fatalf("添加 trader %s 失败: %v", id, err)
		}
	}
	if len(tm.GetAllTraders()) != 3 {
		t.Fatalf("期望 3 个 trader")
	}

	// 2. 追踪仓位
	if err := tm.TrackNewPosition("lifecycle-A", "BTCUSDT", "long", 1, 50000.0, 0.01, 10); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}
	if err := tm.TrackNewPosition("lifecycle-B", "ETHUSDT", "short", 2, 3000.0, 1.0, 5); err != nil {
		t.Fatalf("TrackNewPosition 失败: %v", err)
	}

	summary := tm.GetTrackingSummary()
	if summary["total_tracked"].(int) != 2 {
		t.Errorf("期望 2 个追踪订单，实际 %d", summary["total_tracked"])
	}

	// 3. 更新止损/止盈
	if err := tm.UpdateStopLossOrderID("lifecycle-A", "BTCUSDT", "long", 100); err != nil {
		t.Fatalf("UpdateStopLossOrderID 失败: %v", err)
	}
	if err := tm.UpdateTakeProfitOrderID("lifecycle-B", "ETHUSDT", "short", 200); err != nil {
		t.Fatalf("UpdateTakeProfitOrderID 失败: %v", err)
	}

	// 4. 停止追踪一个
	if err := tm.StopTracking("lifecycle-A", "BTCUSDT", "long"); err != nil {
		t.Fatalf("StopTracking 失败: %v", err)
	}
	summary = tm.GetTrackingSummary()
	if summary["total_tracked"].(int) != 1 {
		t.Errorf("停止一个追踪后应剩 1 个，实际 %d", summary["total_tracked"])
	}

	// 5. 移除一个 trader
	if err := tm.RemoveTrader("lifecycle-C"); err != nil {
		t.Fatalf("移除 trader 失败: %v", err)
	}
	if len(tm.GetAllTraders()) != 2 {
		t.Errorf("移除后应剩 2 个 trader")
	}

	// 6. 验证 GetComparisonData 不 panic
	_, err := tm.GetComparisonData()
	if err != nil {
		t.Errorf("GetComparisonData 不应失败: %v", err)
	}
}
