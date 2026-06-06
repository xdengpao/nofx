package optimize

import (
	"context"

	"nofx/backtest"
	"nofx/historydb"
)

func RunBaseline(ctx context.Context, cfg *backtest.BacktestConfig, store *historydb.Store, traderID, exchange string) (*RunArtifacts, *RunMetrics, error) {
	return runBacktest(ctx, cfg, store, traderID, exchange)
}

func RunCandidate(ctx context.Context, cfg *backtest.BacktestConfig, store *historydb.Store, traderID, exchange string, baseline *RunMetrics) (*RunArtifacts, *RunMetrics, error) {
	artifacts, metrics, err := runBacktest(ctx, cfg, store, traderID, exchange)
	if err != nil {
		return nil, nil, err
	}
	if baseline != nil {
		if reasons := incomparableReasons(baseline, metrics); len(reasons) > 0 {
			return artifacts, metrics, ErrIncomparableRuns
		}
	}
	return artifacts, metrics, nil
}

func runBacktest(ctx context.Context, cfg *backtest.BacktestConfig, store *historydb.Store, traderID, exchange string) (*RunArtifacts, *RunMetrics, error) {
	if cfg != nil && exchange != "" {
		cfg.Exchange = exchange
	}
	runner, err := backtest.NewRunner(cfg, store)
	if err != nil {
		return nil, nil, err
	}
	result, err := runner.Run(ctx)
	if err != nil {
		return nil, nil, err
	}
	artifacts, loadErr := LoadRunArtifacts(result.OutputDir)
	if artifacts != nil && traderID != "" {
		artifacts.Report.TraderID = traderID
	}
	if loadErr != nil && loadErr != ErrMissingStructureSnapshots {
		return artifacts, nil, loadErr
	}
	metrics, err := ExtractRunMetrics(artifacts)
	if err != nil {
		return artifacts, nil, err
	}
	return artifacts, metrics, loadErr
}
