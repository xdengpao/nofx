package optimize

import "fmt"

func DefaultGradualRolloutConfig() GradualRolloutConfig {
	return GradualRolloutConfig{
		DryRunEnabled:       true,
		MaxPositionRatio:    0.30,
		RollingWindowTrades: 30,
	}
}

func CheckRollbackCondition(liveMetrics, baselineMetrics *RunMetrics, config *GradualRolloutConfig) (bool, string) {
	if liveMetrics == nil || baselineMetrics == nil {
		return true, "live或baseline指标为空"
	}
	cfg := DefaultGradualRolloutConfig()
	if config != nil {
		cfg = *config
	}
	if cfg.NetPnLDegradationThreshold > 0 && liveMetrics.NetPnL < baselineMetrics.NetPnL-cfg.NetPnLDegradationThreshold {
		return true, fmt.Sprintf("滚动Net_PnL退化超过阈值: live=%.4f baseline=%.4f", liveMetrics.NetPnL, baselineMetrics.NetPnL)
	}
	if cfg.MaxDrawdownThreshold > 0 && liveMetrics.MaxDrawdownPct > cfg.MaxDrawdownThreshold {
		return true, fmt.Sprintf("滚动最大回撤超过阈值: %.4f", liveMetrics.MaxDrawdownPct)
	}
	if cfg.RejectionRateThreshold > 0 && liveMetrics.RejectionRate > cfg.RejectionRateThreshold {
		return true, fmt.Sprintf("preflight拒绝率超过阈值: %.4f", liveMetrics.RejectionRate)
	}
	return false, ""
}
