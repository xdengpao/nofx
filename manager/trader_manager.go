package manager

import (
	"context"
	"fmt"
	"log"
	"nofx/config"
	"nofx/trader"
	"sync"
	"time"
)

// TraderManager 管理多个trader实例
type TraderManager struct {
	traders       map[string]*trader.AutoTrader   // key: trader ID
	orderTrackers map[string]*trader.OrderTracker // key: trader ID -> OrderTracker
	mu            sync.RWMutex

	// 订单追踪相关
	trackerCtx        context.Context
	trackerCancel     context.CancelFunc
	trackerInterval   time.Duration
	autoCloseCallback func(traderID string, order trader.AutoClosedOrder) // 自动平仓回调
}

// NewTraderManager 创建trader管理器
func NewTraderManager() *TraderManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TraderManager{
		traders:         make(map[string]*trader.AutoTrader),
		orderTrackers:   make(map[string]*trader.OrderTracker),
		trackerCtx:      ctx,
		trackerCancel:   cancel,
		trackerInterval: 30 * time.Second, // 默认30秒检查一次
	}
}

// SetAutoCloseCallback 设置自动平仓回调函数
func (tm *TraderManager) SetAutoCloseCallback(callback func(traderID string, order trader.AutoClosedOrder)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.autoCloseCallback = callback
}

// SetTrackerInterval 设置订单追踪检查间隔
func (tm *TraderManager) SetTrackerInterval(interval time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.trackerInterval = interval
}

// AddTrader 添加一个trader
func (tm *TraderManager) AddTrader(cfg config.TraderConfig, coinPoolURL string, maxDailyLoss, maxDrawdown float64, stopTradingMinutes int, leverage config.LeverageConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.traders[cfg.ID]; exists {
		return fmt.Errorf("trader ID '%s' 已存在", cfg.ID)
	}

	// 构建AutoTraderConfig
	traderConfig := trader.AutoTraderConfig{
		ID:                    cfg.ID,
		Name:                  cfg.Name,
		AIModel:               cfg.AIModel,
		Exchange:              cfg.Exchange,
		BinanceAPIKey:         cfg.BinanceAPIKey,
		BinanceSecretKey:      cfg.BinanceSecretKey,
		HyperliquidPrivateKey: cfg.HyperliquidPrivateKey,
		HyperliquidWalletAddr: cfg.HyperliquidWalletAddr,
		HyperliquidTestnet:    cfg.HyperliquidTestnet,
		AsterUser:             cfg.AsterUser,
		AsterSigner:           cfg.AsterSigner,
		AsterPrivateKey:       cfg.AsterPrivateKey,
		CoinPoolAPIURL:        coinPoolURL,
		UseQwen:               cfg.AIModel == "qwen",
		DeepSeekKey:           cfg.DeepSeekKey,
		QwenKey:               cfg.QwenKey,
		CustomAPIURL:          cfg.CustomAPIURL,
		CustomAPIKey:          cfg.CustomAPIKey,
		CustomModelName:       cfg.CustomModelName,
		ScanInterval:          cfg.GetScanInterval(),
		InitialBalance:        cfg.InitialBalance,
		BTCETHLeverage:        leverage.BTCETHLeverage,  // 使用配置的杠杆倍数
		AltcoinLeverage:       leverage.AltcoinLeverage, // 使用配置的杠杆倍数
		MaxDailyLoss:          maxDailyLoss,
		MaxDrawdown:           maxDrawdown,
		StopTradingTime:       time.Duration(stopTradingMinutes) * time.Minute,
	}

	// 创建trader实例
	at, err := trader.NewAutoTrader(traderConfig)
	if err != nil {
		return fmt.Errorf("创建trader失败: %w", err)
	}

	tm.traders[cfg.ID] = at

	// 为每个trader创建对应的OrderTracker
	//注意：这里需要AutoTrader实现Trader接口，或者提供获取底层Trader的方法
	if traderImpl := at.GetTrader(); traderImpl != nil {
		tm.orderTrackers[cfg.ID] = trader.NewOrderTracker(traderImpl)
		log.Printf("📋 [TraderManager] 已为 '%s' 创建订单追踪器", cfg.Name)
	}
	log.Printf("✓ Trader '%s' (%s) 已添加", cfg.Name, cfg.AIModel)
	return nil
}

// GetOrderTracker 获取指定trader的订单追踪器
func (tm *TraderManager) GetOrderTracker(traderID string) (*trader.OrderTracker, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	ot, exists := tm.orderTrackers[traderID]
	if !exists {
		return nil, fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}
	return ot, nil
}

// TrackNewPosition 追踪新开仓位
func (tm *TraderManager) TrackNewPosition(traderID, symbol, side string, entryOrderID int64, entryPrice float64, quantity float64, leverage int) error {
	tm.mu.RLock()
	ot, exists := tm.orderTrackers[traderID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}

	ot.TrackNewPosition(symbol, side, entryOrderID, entryPrice, quantity, leverage)
	return nil
}

// UpdateStopLossOrderID 更新止损订单ID
func (tm *TraderManager) UpdateStopLossOrderID(traderID, symbol, side string, orderID int64) error {
	tm.mu.RLock()
	ot, exists := tm.orderTrackers[traderID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}

	ot.UpdateStopLossOrderID(symbol, side, orderID)
	return nil
}

// UpdateTakeProfitOrderID 更新止盈订单ID
func (tm *TraderManager) UpdateTakeProfitOrderID(traderID, symbol, side string, orderID int64) error {
	tm.mu.RLock()
	ot, exists := tm.orderTrackers[traderID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}

	ot.UpdateTakeProfitOrderID(symbol, side, orderID)
	return nil
}

// StopTracking 停止追踪指定仓位
func (tm *TraderManager) StopTracking(traderID, symbol, side string) error {
	tm.mu.RLock()
	ot, exists := tm.orderTrackers[traderID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}

	ot.StopTracking(symbol, side)
	return nil
}

// StartOrderTracking 启动订单追踪后台任务
func (tm *TraderManager) StartOrderTracking() {
	log.Println("📋 [TraderManager] 启动订单追踪服务...")

	go func() {
		ticker := time.NewTicker(tm.trackerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-tm.trackerCtx.Done():
				log.Println("📋 [TraderManager] 订单追踪服务已停止")
				return
			case <-ticker.C:
				tm.checkAllAutoClosedOrders()
			}
		}
	}()
}

// StopOrderTracking 停止订单追踪
func (tm *TraderManager) StopOrderTracking() {
	tm.trackerCancel()
}

// checkAllAutoClosedOrders 检查所有trader的自动平仓订单
func (tm *TraderManager) checkAllAutoClosedOrders() {
	tm.mu.RLock()
	trackers := make(map[string]*trader.OrderTracker)
	for id, ot := range tm.orderTrackers {
		trackers[id] = ot
	}
	callback := tm.autoCloseCallback
	tm.mu.RUnlock()

	for traderID, ot := range trackers {
		autoClosedOrders := ot.CheckAutoClosedOrders()

		for _, order := range autoClosedOrders {
			// 记录日志
			tm.logAutoClosedOrder(traderID, order)

			// 执行回调
			if callback != nil {
				callback(traderID, order)
			}
		}
	}
}

// logAutoClosedOrder 记录自动平仓订单日志
func (tm *TraderManager) logAutoClosedOrder(traderID string, order trader.AutoClosedOrder) {
	pnlEmoji := "🟢"
	if order.RealizedPnL < 0 {
		pnlEmoji = "🔴"
	}

	reasonEmoji := map[string]string{
		"STOP_LOSS":   "🛑",
		"TAKE_PROFIT": "🎯",
		"LIQUIDATION": "💥",
		"AUTO_CLOSE":  "⚡",
	}

	emoji := reasonEmoji[order.CloseReason]
	if emoji == "" {
		emoji = "📋"
	}

	log.Printf("%s [%s] %s 自动平仓: %s %s", emoji, traderID, order.CloseReason, order.Symbol, order.Side)
	log.Printf("   %s 盈亏: %.4f USDT (%.2f%%)", pnlEmoji, order.RealizedPnL, order.PnLPercent)
	log.Printf("   入场价: %.4f -> 出场价: %.4f", order.EntryPrice, order.ExitPrice)
	log.Printf("   持仓时间: %.1f 分钟, 手续费: %.4f", order.HoldTimeMinutes, order.Commission)
}

// GetAllTrackedOrders 获取所有追踪中的订单
func (tm *TraderManager) GetAllTrackedOrders() map[string]map[string]*trader.TrackedOrder {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]map[string]*trader.TrackedOrder)
	for traderID, ot := range tm.orderTrackers {
		result[traderID] = ot.GetTrackedOrders()
	}
	return result
}

// GetTrackedOrdersForTrader 获取指定trader的追踪订单
func (tm *TraderManager) GetTrackedOrdersForTrader(traderID string) (map[string]*trader.TrackedOrder, error) {
	tm.mu.RLock()
	ot, exists := tm.orderTrackers[traderID]
	tm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("trader ID '%s' 的订单追踪器不存在", traderID)
	}

	return ot.GetTrackedOrders(), nil
}

// GetTrader 获取指定ID的trader
func (tm *TraderManager) GetTrader(id string) (*trader.AutoTrader, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, exists := tm.traders[id]
	if !exists {
		return nil, fmt.Errorf("trader ID '%s' 不存在", id)
	}
	return t, nil
}

// GetAllTraders 获取所有trader
func (tm *TraderManager) GetAllTraders() map[string]*trader.AutoTrader {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]*trader.AutoTrader)
	for id, t := range tm.traders {
		result[id] = t
	}
	return result
}

// GetTraderIDs 获取所有trader ID列表
func (tm *TraderManager) GetTraderIDs() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	ids := make([]string, 0, len(tm.traders))
	for id := range tm.traders {
		ids = append(ids, id)
	}
	return ids
}

// StartAll 启动所有trader
func (tm *TraderManager) StartAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	log.Println("🚀 启动所有Trader...")
	for id, t := range tm.traders {
		go func(traderID string, at *trader.AutoTrader) {
			log.Printf("▶️  启动 %s...", at.GetName())
			if err := at.Run(); err != nil {
				log.Printf("❌ %s 运行错误: %v", at.GetName(), err)
			}
		}(id, t)
	}

	// 同时启动订单追踪服务
	tm.StartOrderTracking()
}

// StopAll 停止所有trader
func (tm *TraderManager) StopAll() {
	// 先停止订单追踪
	tm.StopOrderTracking()

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	log.Println("⏹  停止所有Trader...")
	for _, t := range tm.traders {
		t.Stop()
	}
}

// RemoveTrader 移除一个trader
func (tm *TraderManager) RemoveTrader(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, exists := tm.traders[id]
	if !exists {
		return fmt.Errorf("trader ID '%s' 不存在", id)
	}

	// 停止trader
	t.Stop()

	// 删除trader和对应的订单追踪器
	delete(tm.traders, id)
	delete(tm.orderTrackers, id)

	log.Printf("✓ Trader '%s' 已移除", id)
	return nil
}

// GetComparisonData 获取对比数据
func (tm *TraderManager) GetComparisonData() (map[string]interface{}, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	comparison := make(map[string]interface{})
	traders := make([]map[string]interface{}, 0, len(tm.traders))

	for id, t := range tm.traders {
		account, err := t.GetAccountInfo()
		if err != nil {
			continue
		}

		status := t.GetStatus()

		// 获取追踪订单数量
		trackedCount := 0
		if ot, exists := tm.orderTrackers[id]; exists {
			trackedCount = len(ot.GetTrackedOrders())
		}

		traders = append(traders, map[string]interface{}{
			"trader_id":       t.GetID(),
			"trader_name":     t.GetName(),
			"ai_model":        t.GetAIModel(),
			"total_equity":    account["total_equity"],
			"total_pnl":       account["total_pnl"],
			"total_pnl_pct":   account["total_pnl_pct"],
			"position_count":  account["position_count"],
			"margin_used_pct": account["margin_used_pct"],
			"call_count":      status["call_count"],
			"is_running":      status["is_running"],
			"tracked_orders":  trackedCount, // 新增：追踪中的订单数
		})
	}

	comparison["traders"] = traders
	comparison["count"] = len(traders)

	return comparison, nil
}

// GetTrackingSummary 获取订单追踪摘要
func (tm *TraderManager) GetTrackingSummary() map[string]interface{} {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	summary := make(map[string]interface{})
	traderSummaries := make([]map[string]interface{}, 0)

	totalTracked := 0

	for traderID, ot := range tm.orderTrackers {
		trackedOrders := ot.GetTrackedOrders()
		count := len(trackedOrders)
		totalTracked += count

		orders := make([]map[string]interface{}, 0)
		for _, tracked := range trackedOrders {
			orders = append(orders, map[string]interface{}{
				"symbol":      tracked.Symbol,
				"side":        tracked.Side,
				"entry_price": tracked.EntryPrice,
				"quantity":    tracked.Quantity,
				"leverage":    tracked.Leverage,
				"entry_time":  tracked.EntryTime,
				"has_sl":      tracked.StopLossOrderID > 0,
				"has_tp":      tracked.TakeProfitOrderID > 0,
			})
		}

		traderSummaries = append(traderSummaries, map[string]interface{}{
			"trader_id":      traderID,
			"tracked_count":  count,
			"tracked_orders": orders,
		})
	}

	summary["total_tracked"] = totalTracked
	summary["traders"] = traderSummaries

	return summary
}
