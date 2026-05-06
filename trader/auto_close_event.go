package trader

import (
	"fmt"
	"nofx/decision"
	"nofx/logger"
	"time"
)

const autoCloseDedupeTTL = 10 * time.Minute

type autoCloseEvent struct {
	Symbol          string
	Side            string
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

func (at *AutoTrader) handleAutoCloseEvent(event autoCloseEvent, writeStandaloneLog bool) logger.DecisionAction {
	action := "auto_close_long"
	if event.Side == "short" {
		action = "auto_close_short"
	}
	if event.CloseTime.IsZero() {
		event.CloseTime = time.Now()
	}

	actionRecord := logger.DecisionAction{
		Action:    action,
		Symbol:    event.Symbol,
		Quantity:  event.Quantity,
		Leverage:  event.Leverage,
		Price:     event.ExitPrice,
		OrderID:   event.OrderID,
		Timestamp: event.CloseTime,
		Success:   true,
		Reasoning: event.CloseReason,
	}

	if event.RealizedPnL != 0 || event.PnLPercent != 0 {
		decision.OnPositionClosedScoped(
			at.id,
			event.Symbol,
			event.Side,
			event.ExitPrice,
			event.PnLPercent,
			event.RealizedPnL,
			event.CloseReason,
		)
	} else {
		decision.OnPositionClosedSimpleScoped(at.id, event.Symbol, event.Side, event.CloseReason)
	}

	if at.orderTracker != nil {
		at.orderTracker.StopTracking(event.Symbol, event.Side)
	}

	if writeStandaloneLog {
		at.logAutoClosedAction(actionRecord, event)
	}
	return actionRecord
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
