package drl

import (
	"fmt"
	"math"
	"nofx/decision"
	"nofx/market"
	"strings"
)

type ActionMapper struct {
	Threshold       float64
	MaxPositionPct  float64
	DefaultLeverage int
}

func NewActionMapper(cfg DRLEngineConfig) *ActionMapper {
	return &ActionMapper{
		Threshold:       cfg.ActionThreshold,
		MaxPositionPct:  cfg.MaxPositionPct,
		DefaultLeverage: cfg.DefaultLeverage,
	}
}

func (m *ActionMapper) Map(rawAction float32, symbol string, currentPos *decision.PositionInfo, account decision.AccountInfo, atr float64, referencePrice float64, cfg *DRLEngineConfig) []decision.Decision {
	if m == nil {
		m = &ActionMapper{}
	}
	threshold := m.Threshold
	if threshold <= 0 {
		threshold = 0.1
	}
	actionValue := float64(rawAction)
	absAction := math.Abs(actionValue)
	symbol = market.Normalize(symbol)
	if absAction <= threshold {
		return []decision.Decision{m.withMetadata(decision.Decision{
			Symbol:     symbol,
			Action:     "wait",
			Confidence: confidenceFromAction(actionValue),
			Reasoning:  fmt.Sprintf("DRL输出 %.4f 未超过动作阈值 %.4f，等待", actionValue, threshold),
		}, cfg, "inference", "threshold_wait", actionValue)}
	}

	if actionValue > threshold {
		return m.mapDirectional("long", actionValue, symbol, currentPos, account, atr, referencePrice, cfg)
	}
	return m.mapDirectional("short", actionValue, symbol, currentPos, account, atr, referencePrice, cfg)
}

func (m *ActionMapper) mapDirectional(targetSide string, rawAction float64, symbol string, currentPos *decision.PositionInfo, account decision.AccountInfo, atr float64, referencePrice float64, cfg *DRLEngineConfig) []decision.Decision {
	currentSide := ""
	if currentPos != nil {
		currentSide = strings.ToLower(strings.TrimSpace(currentPos.Side))
		if referencePrice <= 0 {
			referencePrice = currentPos.MarkPrice
		}
		if referencePrice <= 0 {
			referencePrice = currentPos.EntryPrice
		}
	}
	if currentSide == targetSide {
		return []decision.Decision{m.withMetadata(decision.Decision{
			Symbol:     symbol,
			Action:     "hold",
			Confidence: confidenceFromAction(rawAction),
			Reasoning:  fmt.Sprintf("DRL输出 %.4f 与当前%s持仓同向，继续持有", rawAction, targetSide),
		}, cfg, "inference", "same_side_hold", rawAction)}
	}

	openAction := "open_long"
	closeAction := "close_short"
	if targetSide == "short" {
		openAction = "open_short"
		closeAction = "close_long"
	}
	open := m.openDecision(openAction, symbol, rawAction, account, atr, referencePrice, cfg)
	if currentSide == "" {
		return []decision.Decision{open}
	}
	return []decision.Decision{
		m.withMetadata(decision.Decision{
			Symbol:     symbol,
			Action:     closeAction,
			Confidence: confidenceFromAction(rawAction),
			Reasoning:  fmt.Sprintf("DRL输出 %.4f 与当前%s持仓反向，先平仓", rawAction, currentSide),
		}, cfg, "risk_reducing", "reverse_close", rawAction),
		open,
	}
}

func (m *ActionMapper) openDecision(action, symbol string, rawAction float64, account decision.AccountInfo, atr float64, referencePrice float64, cfg *DRLEngineConfig) decision.Decision {
	maxPct := m.MaxPositionPct
	if maxPct <= 0 {
		maxPct = 0.3
	}
	equity := decision.AccountSizingEquity(account)
	if equity <= 0 {
		equity = account.TotalEquity
	}
	size := math.Abs(rawAction) * maxPct * equity
	stopLoss, takeProfit := atrStopLossTakeProfit(action, referencePrice, atr, cfg)
	leverage := m.DefaultLeverage
	if leverage <= 0 {
		leverage = 5
	}
	return m.withMetadata(decision.Decision{
		Symbol:                   symbol,
		Action:                   action,
		Leverage:                 leverage,
		RequestedPositionSizeUSD: size,
		PositionSizeUSD:          size,
		StopLoss:                 stopLoss,
		TakeProfit:               takeProfit,
		Confidence:               confidenceFromAction(rawAction),
		Reasoning:                fmt.Sprintf("DRL输出 %.4f 超过阈值，生成%s，目标仓位 %.2f USDT", rawAction, action, size),
	}, cfg, "inference", action, rawAction)
}

func (m *ActionMapper) withMetadata(d decision.Decision, cfg *DRLEngineConfig, layer, rule string, rawAction float64) decision.Decision {
	d.StrategyMode = StrategyMode
	d.StrategyName = StrategyName
	if cfg != nil {
		d.StrategyVersion = cfg.ModelVersion
	}
	if d.StrategyVersion == "" {
		d.StrategyVersion = "default"
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	d.StrategyMetadata["layer"] = layer
	d.StrategyMetadata["rule"] = rule
	d.StrategyMetadata["raw_action"] = rawAction
	return d
}

func confidenceFromAction(rawAction float64) int {
	return int(clipFloat64(math.Abs(rawAction)*100, 0, 100))
}
