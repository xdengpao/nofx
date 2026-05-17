// trader/order_tracker.go - 新建文件

package trader

import (
	"log"
	"sync"
	"time"
)

// OrderTracker 订单追踪器（追踪交易所自动成交的订单）
type OrderTracker struct {
	trader        Trader
	trackedOrders map[string]*TrackedOrder // symbol_side -> order info
	lastCheckTime time.Time
	checkInterval time.Duration
	mu            sync.RWMutex
}

// TrackedOrder 追踪的订单信息
type TrackedOrder struct {
	Symbol            string
	Side              string // long/short
	EntryOrderID      int64  // 开仓订单ID
	StopLossOrderID   int64  // 止损订单ID
	TakeProfitOrderID int64  // 止盈订单ID
	EntryPrice        float64
	EntryTime         time.Time
	Quantity          float64
	Leverage          int
}

// NewOrderTracker 创建订单追踪器
func NewOrderTracker(trader Trader) *OrderTracker {
	return &OrderTracker{
		trader:        trader,
		trackedOrders: make(map[string]*TrackedOrder),
		checkInterval: 30 * time.Second, // 每30秒检查一次
	}
}

// TrackNewPosition 追踪新开仓位
func (ot *OrderTracker) TrackNewPosition(symbol, side string, entryOrderID int64, entryPrice float64, quantity float64, leverage int) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	key := symbol + "_" + side
	if tracked, ok := ot.trackedOrders[key]; ok {
		totalQuantity := tracked.Quantity + quantity
		if totalQuantity > 0 && tracked.EntryPrice > 0 && entryPrice > 0 {
			tracked.EntryPrice = (tracked.EntryPrice*tracked.Quantity + entryPrice*quantity) / totalQuantity
		}
		tracked.Quantity = totalQuantity
		tracked.EntryOrderID = entryOrderID
		tracked.Leverage = leverage
		log.Printf("📋 [OrderTracker] 更新追踪: %s %s @ %.4f 数量 %.6f", symbol, side, tracked.EntryPrice, tracked.Quantity)
		return
	}

	ot.trackedOrders[key] = &TrackedOrder{
		Symbol:       symbol,
		Side:         side,
		EntryOrderID: entryOrderID,
		EntryPrice:   entryPrice,
		EntryTime:    time.Now(),
		Quantity:     quantity,
		Leverage:     leverage,
	}

	log.Printf("📋 [OrderTracker] 开始追踪: %s %s @ %.4f", symbol, side, entryPrice)
}

// UpdateStopLossOrderID 更新止损订单ID
func (ot *OrderTracker) UpdateStopLossOrderID(symbol, side string, orderID int64) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	key := symbol + "_" + side
	if tracked, ok := ot.trackedOrders[key]; ok {
		tracked.StopLossOrderID = orderID
	}
}

// UpdateTakeProfitOrderID 更新止盈订单ID
func (ot *OrderTracker) UpdateTakeProfitOrderID(symbol, side string, orderID int64) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	key := symbol + "_" + side
	if tracked, ok := ot.trackedOrders[key]; ok {
		tracked.TakeProfitOrderID = orderID
	}
}

// StopTracking 停止追踪（手动平仓时调用）
func (ot *OrderTracker) StopTracking(symbol, side string) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	key := symbol + "_" + side
	delete(ot.trackedOrders, key)
	log.Printf("📋 [OrderTracker] 停止追踪: %s %s", symbol, side)
}

// CheckAutoClosedOrders 检查自动成交的订单
// 返回自动平仓的订单列表及其盈亏信息
func (ot *OrderTracker) CheckAutoClosedOrders() []AutoClosedOrder {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	var autoClosedOrders []AutoClosedOrder

	// 获取当前持仓
	positions, err := ot.trader.GetPositions()
	if err != nil {
		log.Printf("⚠️ [OrderTracker] 获取持仓失败: %v", err)
		return nil
	}

	// 创建当前持仓的map
	currentPositions := make(map[string]bool)
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		key := symbol + "_" + side
		currentPositions[key] = true
	}

	// 检查哪些追踪的仓位已经消失
	keysToRemove := []string{}

	for key, tracked := range ot.trackedOrders {
		if !currentPositions[key] {
			// 仓位消失了，查询成交详情
			log.Printf("🔍 [OrderTracker] 检测到仓位消失: %s, 查询成交记录...", key)

			autoClosedOrder := ot.queryAutoCloseDetails(tracked)
			if autoClosedOrder != nil {
				autoClosedOrders = append(autoClosedOrders, *autoClosedOrder)
			}

			keysToRemove = append(keysToRemove, key)
		}
	}

	// 移除已平仓的追踪记录
	for _, key := range keysToRemove {
		delete(ot.trackedOrders, key)
	}

	return autoClosedOrders
}

// AutoClosedOrder 自动平仓的订单信息
type AutoClosedOrder struct {
	Symbol          string
	Side            string
	CloseReason     string // STOP_LOSS / TAKE_PROFIT / LIQUIDATION
	EntryPrice      float64
	ExitPrice       float64
	Quantity        float64
	Leverage        int
	RealizedPnL     float64 // 真实盈亏（从交易所获取）
	PnLPercent      float64 // 盈亏百分比
	HoldTimeMinutes float64
	CloseTime       time.Time
	OrderID         int64
	Commission      float64
}

// queryAutoCloseDetails 查询自动平仓详情
func (ot *OrderTracker) queryAutoCloseDetails(tracked *TrackedOrder) *AutoClosedOrder {
	// 方法1: 查询止损订单状态
	if tracked.StopLossOrderID > 0 {
		order, err := ot.trader.GetOrderStatus(tracked.Symbol, tracked.StopLossOrderID)
		if err == nil && order.Status == "FILLED" {
			return ot.buildAutoClosedOrder(tracked, order, "STOP_LOSS")
		}
	}

	// 方法2: 查询止盈订单状态
	if tracked.TakeProfitOrderID > 0 {
		order, err := ot.trader.GetOrderStatus(tracked.Symbol, tracked.TakeProfitOrderID)
		if err == nil && order.Status == "FILLED" {
			return ot.buildAutoClosedOrder(tracked, order, "TAKE_PROFIT")
		}
	}

	// 方法3: 查询最近的成交记录
	trades, err := ot.trader.GetTradeHistory(
		tracked.Symbol,
		tracked.EntryTime.UnixMilli(),
		time.Now().UnixMilli(),
		50,
	)
	if err != nil {
		log.Printf("⚠️ [OrderTracker] 查询成交记录失败: %v", err)
		return nil
	}

	// 查找平仓成交
	for i := len(trades) - 1; i >= 0; i-- {
		trade := trades[i]

		// 判断是否为平仓成交（方向相反）
		isCloseTrade := false
		if tracked.Side == "long" && trade.Side == "SELL" {
			isCloseTrade = true
		} else if tracked.Side == "short" && trade.Side == "BUY" {
			isCloseTrade = true
		}

		if isCloseTrade && trade.Time.After(tracked.EntryTime) {
			return &AutoClosedOrder{
				Symbol:          tracked.Symbol,
				Side:            tracked.Side,
				CloseReason:     "AUTO_CLOSE", // 无法确定具体原因
				EntryPrice:      tracked.EntryPrice,
				ExitPrice:       trade.Price,
				Quantity:        trade.Qty,
				Leverage:        tracked.Leverage,
				RealizedPnL:     trade.RealizedPnL,
				PnLPercent:      ot.calculatePnLPercent(tracked, trade.Price),
				HoldTimeMinutes: trade.Time.Sub(tracked.EntryTime).Minutes(),
				CloseTime:       trade.Time,
				OrderID:         trade.OrderID,
				Commission:      trade.Commission,
			}
		}
	}

	// 方法4: 查询订单历史
	orders, err := ot.trader.GetOrderHistory(
		tracked.Symbol,
		tracked.EntryTime.UnixMilli(),
		time.Now().UnixMilli(),
		100,
	)
	if err != nil {
		log.Printf("⚠️ [OrderTracker] 查询订单历史失败: %v", err)
		return nil
	}

	// 查找已成交的止盈/止损订单
	for i := len(orders) - 1; i >= 0; i-- {
		order := orders[i]
		if order.Status == "FILLED" && order.IsAutoClose {
			return ot.buildAutoClosedOrder(tracked, &order, order.CloseReason)
		}
	}

	log.Printf("⚠️ [OrderTracker] 未能查询到 %s %s 的平仓详情", tracked.Symbol, tracked.Side)
	return nil
}

// buildAutoClosedOrder 构建自动平仓订单记录
func (ot *OrderTracker) buildAutoClosedOrder(tracked *TrackedOrder, order *OrderRecord, reason string) *AutoClosedOrder {
	exitPrice := order.AvgPrice
	if exitPrice == 0 {
		exitPrice = order.Price
	}

	return &AutoClosedOrder{
		Symbol:          tracked.Symbol,
		Side:            tracked.Side,
		CloseReason:     reason,
		EntryPrice:      tracked.EntryPrice,
		ExitPrice:       exitPrice,
		Quantity:        order.ExecutedQty,
		Leverage:        tracked.Leverage,
		RealizedPnL:     order.RealizedPnL,
		PnLPercent:      ot.calculatePnLPercent(tracked, exitPrice),
		HoldTimeMinutes: order.UpdateTime.Sub(tracked.EntryTime).Minutes(),
		CloseTime:       order.UpdateTime,
		OrderID:         order.OrderID,
		Commission:      order.Commission,
	}
}

// calculatePnLPercent 计算盈亏百分比
func (ot *OrderTracker) calculatePnLPercent(tracked *TrackedOrder, exitPrice float64) float64 {
	if tracked.EntryPrice == 0 {
		return 0
	}

	var pnlPercent float64
	if tracked.Side == "long" {
		pnlPercent = ((exitPrice - tracked.EntryPrice) / tracked.EntryPrice) * float64(tracked.Leverage) * 100
	} else {
		pnlPercent = ((tracked.EntryPrice - exitPrice) / tracked.EntryPrice) * float64(tracked.Leverage) * 100
	}

	return pnlPercent
}

// GetTrackedOrders 获取当前追踪的订单列表
func (ot *OrderTracker) GetTrackedOrders() map[string]*TrackedOrder {
	ot.mu.RLock()
	defer ot.mu.RUnlock()

	result := make(map[string]*TrackedOrder)
	for k, v := range ot.trackedOrders {
		result[k] = v
	}
	return result
}
