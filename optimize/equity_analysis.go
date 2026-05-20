package optimize

func BuildEquityCurveAnalysis(artifacts *RunArtifacts) EquityCurveAnalysis {
	if artifacts == nil {
		return EquityCurveAnalysis{}
	}
	return AnalyzeEquityCurve(artifacts.RunID, artifacts.Equity, artifacts.Trades)
}
