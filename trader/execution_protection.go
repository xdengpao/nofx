package trader

import (
	"fmt"
	"nofx/decision"
	"nofx/logger"
	"strings"
)

const protectiveOrderMaxAttempts = 3

type protectiveOrderResult struct {
	stopLossSet    bool
	takeProfitSet  bool
	stopLossErr    error
	takeProfitErr  error
	protectionSide string
}

func boolPtr(value bool) *bool {
	return &value
}

func (at *AutoTrader) setProtectiveOrdersWithRecord(d *decision.Decision, side string, quantity float64, actionRecord *logger.DecisionAction) protectiveOrderResult {
	positionSide := "LONG"
	if side == "short" {
		positionSide = "SHORT"
	}

	result := protectiveOrderResult{protectionSide: positionSide}
	result.stopLossErr = at.setStopLossWithRetry(d.Symbol, positionSide, quantity, d.StopLoss)
	result.stopLossSet = result.stopLossErr == nil
	actionRecord.StopLossSet = boolPtr(result.stopLossSet)
	if result.stopLossErr != nil {
		logMsg := fmt.Sprintf("止损设置失败: %v", result.stopLossErr)
		actionRecord.ProtectionError = logMsg
		actionRecord.ExecutionRisk = "high"
		actionRecord.HighRisk = true
		actionRecord.HighRiskReason = "止损保护无法建立，仓位可能裸露"
	}

	result.takeProfitErr = at.trader.SetTakeProfit(d.Symbol, positionSide, quantity, d.TakeProfit)
	result.takeProfitSet = result.takeProfitErr == nil
	actionRecord.TakeProfitSet = boolPtr(result.takeProfitSet)
	if result.takeProfitErr != nil {
		errText := fmt.Sprintf("止盈设置失败: %v", result.takeProfitErr)
		if actionRecord.ProtectionError == "" {
			actionRecord.ProtectionError = errText
		} else {
			actionRecord.ProtectionError += "; " + errText
		}
	}

	return result
}

func (at *AutoTrader) setStopLossWithRetry(symbol, positionSide string, quantity, price float64) error {
	var lastErr error
	for attempt := 1; attempt <= protectiveOrderMaxAttempts; attempt++ {
		if err := at.trader.SetStopLoss(symbol, positionSide, quantity, price); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("重试%d次后仍失败: %w", protectiveOrderMaxAttempts, lastErr)
}

func (at *AutoTrader) handleUnprotectedOpen(d *decision.Decision, side string, actionRecord *logger.DecisionAction) error {
	reason := "止损保护无法建立，仓位处于高危裸仓"
	if actionRecord.ProtectionError != "" {
		reason = reason + ": " + actionRecord.ProtectionError
	}

	if at.config.EnableEmergencyClose {
		var err error
		if side == "long" {
			_, err = at.trader.CloseLong(d.Symbol, 0)
		} else {
			_, err = at.trader.CloseShort(d.Symbol, 0)
		}
		at.orderTracker.StopTracking(d.Symbol, side)
		delete(at.positionFirstSeenTime, d.Symbol+"_"+side)
		actionRecord.HighRiskReason = "止损保护无法建立，已触发紧急平仓"
		if err != nil {
			actionRecord.HighRiskReason = "止损保护无法建立，紧急平仓失败"
			return fmt.Errorf("%s，且紧急平仓失败: %w", reason, err)
		}
		return fmt.Errorf("%s，已紧急平仓", reason)
	}

	return fmt.Errorf("%s", reason)
}

func applyPreflightToActionRecord(result ExecutionPreflightResult, actionRecord *logger.DecisionAction) {
	if result.Allowed {
		return
	}
	actionRecord.GateState = "block"
	actionRecord.GateReasons = append(actionRecord.GateReasons, result.Reasons...)
	actionRecord.ExecutionRisk = "preflight_block"
	actionRecord.Error = strings.Join(result.Reasons, "; ")
}
