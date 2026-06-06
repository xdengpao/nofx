package optimize

func BuildSignalQualityReportFromArtifacts(artifacts *RunArtifacts) (SignalQualityReport, error) {
	if artifacts == nil {
		return BuildSignalQualityReport("", SignalQualityInput{})
	}
	return BuildSignalQualityReport(artifacts.RunID, SignalQualityInput{
		Signals:    artifacts.Signals,
		Structures: artifacts.Structures,
		Trades:     artifacts.Trades,
		Executions: artifacts.Executions,
	})
}
