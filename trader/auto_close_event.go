package trader

import (
	"fmt"
	"nofx/decision"
	"nofx/logger"
	"time"
)

const autoCloseDedupeTTL = 10 * time.Minute
const closedPositionDedupeTTL = 30 * time.Minute

const (
	autoCloseSourceOrderTracker = "order_tracker"
	autoCloseSourceSnapshot     = "snapshot"
	autoCloseSourceStalePlan    = "stale_plan"
)

type autoCloseEvent struct {
	Symbol          string
	Side            string
	Source          string
	OrderID         int64
	EntryPrice      float64
	ExitPrice       float64
	Quantity        float64
	Leverage        int
	RealizedPnL     float64
	PnLPercent      float64
	HoldTimeMinutes float64
	Commission      float64
	CloseReason     string
	CloseTime       time.Time
}

func positionLifecycleKey(symbol, side string) string {
	return symbol + "_" + side
}

func (at *AutoTrader) claimAutoCloseEvent(symbol, side string, orderID int64, eventTime time.Time) bool {
	if at.autoCloseDedupe == nil {
		at.autoCloseDedupe = make(map[string]time.Time)
	}
	if eventTime.IsZero() {
		eventTime = time.Now()
	}
	for key, seenAt := range at.autoCloseDedupe {
		if eventTime.Sub(seenAt) > autoCloseDedupeTTL {
			delete(at.autoCloseDedupe, key)
		}
	}

	keys := []string{fmt.Sprintf("%s:%s:%s", at.id, symbol, side)}
	if orderID > 0 {
		keys = append(keys, fmt.Sprintf("%s:order:%d", at.id, orderID))
	}
	for _, key := range keys {
		if seenAt, exists := at.autoCloseDedupe[key]; exists && eventTime.Sub(seenAt) <= autoCloseDedupeTTL {
			return false
		}
	}
	for _, key := range keys {
		at.autoCloseDedupe[key] = eventTime
	}
	return true
}

func (at *AutoTrader) markPositionLifecycleClosed(symbol, side string, closedAt time.Time) {
	if at.closedPositionDedupe == nil {
		at.closedPositionDedupe = make(map[string]time.Time)
	}
	if closedAt.IsZero() {
		closedAt = time.Now()
	}
	at.closedPositionDedupe[positionLifecycleKey(symbol, side)] = closedAt
	at.clearLocalPositionLifecycle(symbol, side)
}

func (at *AutoTrader) clearLocalPositionLifecycle(symbol, side string) {
	key := positionLifecycleKey(symbol, side)
	delete(at.lastPositions, key)
	delete(at.positionFirstSeenTime, key)
}

func (at *AutoTrader) wasPositionLifecycleRecentlyClosed(symbol, side string, now time.Time) bool {
	if len(at.closedPositionDedupe) == 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	for key, closedAt := range at.closedPositionDedupe {
		if now.Sub(closedAt) > closedPositionDedupeTTL {
			delete(at.closedPositionDedupe, key)
		}
	}
	closedAt, ok := at.closedPositionDedupe[positionLifecycleKey(symbol, side)]
	return ok && now.Sub(closedAt) <= closedPositionDedupeTTL
}

func (at *AutoTrader) handleAutoCloseEvent(event autoCloseEvent, writeStandaloneLog bool) logger.DecisionAction {
	action := "auto_close_long"
	if event.Side == "short" {
		action = "auto_close_short"
	}
	if event.CloseTime.IsZero() {
		event.CloseTime = time.Now()
	}
	if event.Source == "" {
		event.Source = autoCloseSourceSnapshot
	}

	actionRecord := logger.DecisionAction{
		Action:           action,
		Symbol:           event.Symbol,
		Quantity:         event.Quantity,
		Leverage:         event.Leverage,
		Price:            event.ExitPrice,
		OrderID:          event.OrderID,
		Timestamp:        event.CloseTime,
		Success:          true,
		Reasoning:        event.CloseReason,
		CloseSource:      event.Source,
		ExchangeMetadata: event.Source == autoCloseSourceOrderTracker,
	}

	counted := false
	if event.ExitPrice > 0 && event.EntryPrice > 0 && event.Quantity > 0 {
		pnlUSD, pnlPercent := autoClosePnL(event.Side, event.EntryPrice, event.ExitPrice, event.Quantity, event.Leverage)
		if event.RealizedPnL != 0 {
			pnlUSD = event.RealizedPnL
		}
		if event.PnLPercent != 0 {
			pnlPercent = event.PnLPercent
		}
		counted = decision.OnPositionClosedWithInput(decision.ClosedPositionInput{
			TraderID:            at.id,
			Symbol:              event.Symbol,
			Side:                event.Side,
			Source:              event.Source,
			EntryPrice:          event.EntryPrice,
			ExitPrice:           event.ExitPrice,
			Quantity:            event.Quantity,
			Leverage:            event.Leverage,
			PnLPercent:          pnlPercent,
			PnLUSD:              pnlUSD,
			Commission:          event.Commission,
			Reason:              event.CloseReason,
			CloseTime:           event.CloseTime,
			HoldingMinutes:      event.HoldTimeMinutes,
			HasExchangeMetadata: event.Source == autoCloseSourceOrderTracker,
		})
	} else {
		decision.OnPositionClosedSimpleScoped(at.id, event.Symbol, event.Side, event.CloseReason)
	}
	actionRecord.CountedInStats = &counted

	if at.orderTracker != nil {
		at.orderTracker.StopTracking(event.Symbol, event.Side)
	}
	at.markPositionLifecycleClosed(event.Symbol, event.Side, event.CloseTime)

	if writeStandaloneLog {
		at.logAutoClosedAction(actionRecord, event)
	}
	return actionRecord
}

func autoClosePnL(side string, entryPrice, exitPrice, quantity float64, leverage int) (float64, float64) {
	if leverage <= 0 {
		leverage = 1
	}

	pnlUSD := quantity * (exitPrice - entryPrice)
	if side == "short" {
		pnlUSD = quantity * (entryPrice - exitPrice)
	}

	margin := quantity * entryPrice / float64(leverage)
	pnlPercent := 0.0
	if margin > 0 {
		pnlPercent = pnlUSD / margin * 100
	}
	return pnlUSD, pnlPercent
}

func (at *AutoTrader) logAutoClosedAction(actionRecord logger.DecisionAction, event autoCloseEvent) {
	record := &logger.DecisionRecord{
		ExecutionLog: []string{
			fmt.Sprintf("[AUTO-CLOSE] %s %s 触发: %s", event.Symbol, event.Side, event.CloseReason),
			fmt.Sprintf("盈亏: %.4f USDT (%.2f%%)", event.RealizedPnL, event.PnLPercent),
		},
		Decisions: []logger.DecisionAction{actionRecord},
		Success:   true,
	}

	if err := at.decisionLogger.LogDecision(record); err != nil {
		fmt.Printf("⚠️ 记录自动平仓日志失败: %v\n", err)
	}
}
