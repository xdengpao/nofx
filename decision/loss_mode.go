package decision

import (
	"nofx/logger"
	"time"
)

const (
	defaultLossModeRiskPerTrade = 0.005
	defaultLossModeMaxPositions = 1
	defaultLossModeDailyOpens   = 1
	defaultLossModeConfidence   = 90
	defaultLossModeCooldown     = 24 * time.Hour
)

// BuildLossModeState 从去重后的 rolling performance 派生亏损模式。
func BuildLossModeState(rolling *logger.RollingPerformanceSnapshot, now time.Time) *LossModeState {
	state := &LossModeState{
		MaxRiskPerTrade: defaultLossModeRiskPerTrade,
		MaxPositions:    defaultLossModeMaxPositions,
		DailyOpenLimit:  defaultLossModeDailyOpens,
		MinConfidence:   defaultLossModeConfidence,
	}
	if rolling == nil {
		return state
	}
	if now.IsZero() {
		now = time.Now()
	}

	switch {
	case rolling.RecentLossStreak >= 2:
		state.Active = true
		state.Reason = "最近去重后连续亏损达到2笔"
	case rolling.Recent3.TradeCount >= 3 && rolling.Recent3Losses >= 2 && rolling.Recent3.TotalPnL < 0:
		state.Active = true
		state.Reason = "最近3笔去重交易中至少2笔亏损且总PnL为负"
	}
	if state.Active {
		state.CooldownUntil = now.Add(defaultLossModeCooldown)
	}
	return state
}
