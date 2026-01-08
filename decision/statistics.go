package decision

import (
	"log"
	"sync"
	"time"
)

// decision/statistics.go - 新增或修改

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

// PersistentData 持久化数据结构 - 扩展
type PersistentData struct {
	Plans        map[string]*TradePlan `json:"plans"`
	Statistics   *TradeStatistics      `json:"statistics"`
	Returns      []float64             `json:"returns"`
	ClosedTrades []ClosedTradeRecord   `json:"closed_trades"` // 🆕 新增
	UpdatedAt    time.Time             `json:"updated_at"`
}

var (
	closedTrades     []ClosedTradeRecord
	closedTradesLock sync.RWMutex
)

// RecordClosedTrade 记录已平仓交易
func RecordClosedTrade(record ClosedTradeRecord) {
	closedTradesLock.Lock()
	defer closedTradesLock.Unlock()

	closedTrades = append(closedTrades, record)

	// 保留最近500笔
	if len(closedTrades) > 500 {
		closedTrades = closedTrades[len(closedTrades)-500:]
	}

	log.Printf("📊 记录已平仓交易: %s %s, 盈亏: %.2f%% (%s)",
		record.Symbol, record.Side, record.PnLPercent, record.CloseReason)

	// 自动保存
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// OnPositionClosed 平仓回调 - 增强版
func OnPositionClosed(symbol string, reason string, pnlPercent float64, holdTimeMinutes float64) {
	// 获取并移除计划
	plan := planManager.GetPlan(symbol)

	// 创建平仓记录
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

	// 记录到历史
	RecordClosedTrade(record)

	// 移除计划
	planManager.RemovePlan(symbol)

	// 记录收益率用于夏普比率计算
	AddReturn(pnlPercent)

	// 更新统计
	UpdateStatistics(pnlPercent, holdTimeMinutes)

	log.Printf("✅ 平仓记录: %s (原因: %s, 盈亏: %.2f%%, 持仓: %.0f分钟)",
		symbol, reason, pnlPercent, holdTimeMinutes)
}

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

	// 计算胜率
	for reason := range stats {
		count := stats[reason]["count"]
		if count > 0 {
			stats[reason]["win_rate"] = stats[reason]["wins"] / count * 100
			stats[reason]["avg_pnl"] = stats[reason]["total_pnl"] / count
		}
	}

	return stats
}
