package decision

import (
	"log"
	"sync"
	"time"
)

// ============================================================================
// 数据结构定义
// ============================================================================

// ClosedTradeRecord 已平仓交易记录
type ClosedTradeRecord struct {
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	CloseReason     string    `json:"close_reason"`
	EntryPrice      float64   `json:"entry_price"`
	ExitPrice       float64   `json:"exit_price"`
	Quantity        float64   `json:"quantity"`
	Leverage        int       `json:"leverage"`
	RealizedPnL     float64   `json:"realized_pnl"`
	PnLPercent      float64   `json:"pnl_percent"`
	HoldTimeMinutes float64   `json:"hold_time_minutes"`
	EntryTime       time.Time `json:"entry_time"`
	ExitTime        time.Time `json:"exit_time"`
	Commission      float64   `json:"commission"`
}

// PersistentData 持久化数据结构
type PersistentData struct {
	Plans        map[string]*TradePlan `json:"plans"`
	Statistics   *TradeStatistics      `json:"statistics"`
	Returns      []float64             `json:"returns"`
	ClosedTrades []ClosedTradeRecord   `json:"closed_trades"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// ============================================================================
// 全局变量
// ============================================================================

var (
	closedTrades     []ClosedTradeRecord
	closedTradesLock sync.RWMutex
)

// ============================================================================
// 核心函数（已修复死锁）
// ============================================================================

// RecordClosedTrade 记录已平仓交易（修复死锁版本）
func RecordClosedTrade(record ClosedTradeRecord) {
	// Step 1: 在锁内完成数据操作
	closedTradesLock.Lock()

	closedTrades = append(closedTrades, record)

	// 保留最近500笔
	if len(closedTrades) > 500 {
		closedTrades = closedTrades[len(closedTrades)-500:]
	}

	// 复制日志需要的数据
	symbol := record.Symbol
	side := record.Side
	pnlPercent := record.PnLPercent
	closeReason := record.CloseReason

	closedTradesLock.Unlock() // ← 先释放锁！

	// Step 2: 在锁外执行日志输出
	log.Printf("📊 记录已平仓交易: %s %s, 盈亏: %.2f%% (%s)",
		symbol, side, pnlPercent, closeReason)

	// Step 3: 在锁外调用自动保存（避免死锁）
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// OnPositionClosed 平仓回调（修复死锁版本）
func OnPositionClosed(symbol string, reason string, pnlPercent float64, holdTimeMinutes float64) {
	// Step 1: 获取计划信息（planManager有自己的锁）
	plan := planManager.GetPlan(symbol)

	// Step 2: 创建平仓记录
	record := ClosedTradeRecord{
		Symbol:          symbol,
		CloseReason:     reason,
		PnLPercent:      pnlPercent,
		HoldTimeMinutes: holdTimeMinutes,
		ExitTime:        time.Now(),
	}

	if plan != nil {
		record.Side = plan.Direction
		record.EntryPrice = plan.EntryPrice
		record.Leverage = plan.Leverage
		record.Quantity = plan.PositionSizeUSD / plan.EntryPrice
		record.EntryTime = plan.CreatedAt
	}

	// Step 3: 记录到历史（内部会处理锁和保存）
	RecordClosedTrade(record)

	// Step 4: 移除计划（planManager有自己的锁，内部会触发保存）
	planManager.RemovePlan(symbol)

	// Step 5: 记录收益率用于夏普比率计算（returnsLock）
	AddReturn(pnlPercent)

	// Step 6: 更新统计（tradeStatsLock）
	UpdateStatistics(pnlPercent, holdTimeMinutes)

	// Step 7: 最终日志
	log.Printf("✅ 平仓完成: %s (原因: %s, 盈亏: %.2f%%, 持仓: %.0f分钟)",
		symbol, reason, pnlPercent, holdTimeMinutes)
}

// ============================================================================
// 查询函数
// ============================================================================

// GetClosedTrades 获取已平仓交易列表
func GetClosedTrades(limit int) []ClosedTradeRecord {
	closedTradesLock.RLock()
	defer closedTradesLock.RUnlock()

	if limit <= 0 || limit > len(closedTrades) {
		limit = len(closedTrades)
	}

	// 返回最近的limit笔
	start := len(closedTrades) - limit
	if start < 0 {
		start = 0
	}

	result := make([]ClosedTradeRecord, limit)
	copy(result, closedTrades[start:])
	return result
}

// GetClosedTradesCount 获取已平仓交易数量
func GetClosedTradesCount() int {
	closedTradesLock.RLock()
	defer closedTradesLock.RUnlock()
	return len(closedTrades)
}

// GetStatisticsByCloseReason 按平仓原因统计
func GetStatisticsByCloseReason() map[string]map[string]float64 {
	closedTradesLock.RLock()
	defer closedTradesLock.RUnlock()

	stats := make(map[string]map[string]float64)

	for _, trade := range closedTrades {
		reason := trade.CloseReason
		if _, ok := stats[reason]; !ok {
			stats[reason] = map[string]float64{
				"count":     0,
				"total_pnl": 0,
				"wins":      0,
				"losses":    0,
			}
		}

		stats[reason]["count"]++
		stats[reason]["total_pnl"] += trade.PnLPercent

		if trade.PnLPercent > 0 {
			stats[reason]["wins"]++
		} else {
			stats[reason]["losses"]++
		}
	}

	// 计算胜率和平均盈亏
	for reason := range stats {
		count := stats[reason]["count"]
		if count > 0 {
			stats[reason]["win_rate"] = stats[reason]["wins"] / count * 100
			stats[reason]["avg_pnl"] = stats[reason]["total_pnl"] / count
		}
	}

	return stats
}

// GetClosedTradesCopy 获取已平仓交易的副本（供 saveToFile 使用，避免长时间持锁）
func GetClosedTradesCopy() []ClosedTradeRecord {
	closedTradesLock.RLock()
	defer closedTradesLock.RUnlock()

	result := make([]ClosedTradeRecord, len(closedTrades))
	copy(result, closedTrades)
	return result
}

// ============================================================================
// 统计分析函数
// ============================================================================

// GetTradingSummary 获取交易摘要
func GetTradingSummary() map[string]interface{} {
	closedTradesLock.RLock()

	totalTrades := len(closedTrades)
	if totalTrades == 0 {
		closedTradesLock.RUnlock()
		return map[string]interface{}{
			"total_trades": 0,
			"message":      "暂无交易记录",
		}
	}

	var totalPnL float64
	var winCount, lossCount int
	var totalHoldTime float64
	var maxWin, maxLoss float64

	for _, trade := range closedTrades {
		totalPnL += trade.PnLPercent
		totalHoldTime += trade.HoldTimeMinutes

		if trade.PnLPercent > 0 {
			winCount++
			if trade.PnLPercent > maxWin {
				maxWin = trade.PnLPercent
			}
		} else {
			lossCount++
			if trade.PnLPercent < maxLoss {
				maxLoss = trade.PnLPercent
			}
		}
	}

	closedTradesLock.RUnlock() // ← 释放锁后再计算

	winRate := float64(winCount) / float64(totalTrades) * 100
	avgPnL := totalPnL / float64(totalTrades)
	avgHoldTime := totalHoldTime / float64(totalTrades)

	return map[string]interface{}{
		"total_trades":  totalTrades,
		"win_count":     winCount,
		"loss_count":    lossCount,
		"win_rate":      winRate,
		"total_pnl":     totalPnL,
		"avg_pnl":       avgPnL,
		"max_win":       maxWin,
		"max_loss":      maxLoss,
		"avg_hold_time": avgHoldTime,
	}
}

// ClearClosedTrades 清空已平仓交易记录（谨慎使用）
func ClearClosedTrades() {
	closedTradesLock.Lock()
	closedTrades = nil
	closedTradesLock.Unlock()

	log.Println("🗑️ 已清空平仓交易记录")

	// 在锁外触发保存
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}
