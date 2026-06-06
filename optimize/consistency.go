package optimize

func CheckReplayBacktestConsistency(replayMetrics, backtestMetrics *RunMetrics, tolerance float64) (bool, []string) {
	result := CompareReplayBacktest("", "", replayMetrics, backtestMetrics, tolerance)
	return result.Comparable && result.Verdict == "ok", result.Reasons
}
