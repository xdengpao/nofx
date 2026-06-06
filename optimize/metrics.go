package optimize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"

	"nofx/backtest"
)

func ExtractRunMetrics(artifacts *RunArtifacts) (*RunMetrics, error) {
	return ExtractRunMetricsWithConfig(artifacts, DefaultOptimizationConfig())
}

func ExtractRunMetricsWithConfig(artifacts *RunArtifacts, cfg *OptimizationConfig) (*RunMetrics, error) {
	if artifacts == nil {
		return nil, fmt.Errorf("run artifacts为空")
	}
	if cfg == nil {
		cfg = DefaultOptimizationConfig()
	}
	ApplyOptimizationDefaults(cfg)
	report := artifacts.Report
	totalRejections := len(artifacts.Rejections)
	if totalRejections == 0 {
		totalRejections = report.Summary.RejectionCount
	}
	tradeCount := len(artifacts.Trades)
	if tradeCount == 0 {
		tradeCount = report.Summary.TradeCount
	}
	totalSignals := report.Summary.SignalCount
	if totalSignals == 0 {
		totalSignals = len(artifacts.Signals)
	}
	rejectionRate := 0.0
	if totalSignals+totalRejections > 0 {
		rejectionRate = float64(totalRejections) / float64(totalSignals+totalRejections)
	}
	minNotionalRate := 0.0
	if totalRejections > 0 {
		minNotionalRate = float64(report.MinNotionalRejects) / float64(totalRejections)
	}
	avgHold := 0.0
	for _, trade := range artifacts.Trades {
		avgHold += trade.DurationMinutes
	}
	if len(artifacts.Trades) > 0 {
		avgHold /= float64(len(artifacts.Trades))
	}
	feeRatio := 0.0
	if report.Summary.InitialEquity > 0 {
		feeRatio = report.Summary.TotalFees / report.Summary.InitialEquity
	}
	slippageRatio := 0.0
	if report.Summary.InitialEquity > 0 {
		slippageRatio = report.Summary.TotalSlippage / report.Summary.InitialEquity
	}
	signalDelay, fillDelay, delayCount := delayStats(artifacts.Executions)
	metrics := &RunMetrics{
		RunID:                         firstNonEmpty(artifacts.RunID, report.RunID),
		TraderID:                      report.TraderID,
		Exchange:                      report.Exchange,
		ConfigHash:                    report.ConfigHash,
		DataHash:                      report.DataHash,
		Timezone:                      report.Timezone,
		MetricsSource:                 "derived",
		SymbolSetHash:                 symbolSetHash(report, artifacts.Trades),
		InitialEquity:                 report.Summary.InitialEquity,
		FeeModelHash:                  stableHash(report.ConfigSnapshot["costs"]),
		SlippageModelHash:             stableHash(report.ConfigSnapshot["costs"]),
		FundingMode:                   report.FundingMode,
		LiquidationMode:               report.LiquidationMode,
		ExecutionModelHash:            stableHash(report.ExecutionModel),
		WinRate:                       report.Summary.WinRate,
		ProfitFactor:                  report.Summary.ProfitFactor,
		NetPnL:                        report.Summary.NetPnL,
		NetPnLPct:                     report.Summary.NetReturnPct,
		MaxDrawdownPct:                report.Summary.MaxDrawdownPct,
		AverageR:                      report.Summary.AverageR,
		AvgHoldMinutes:                avgHold,
		FeeRatio:                      feeRatio,
		SlippageRatio:                 slippageRatio,
		RejectionRate:                 rejectionRate,
		MinNotionalRejectionRate:      minNotionalRate,
		CircuitBreakerFrequencyPerDay: circuitBreakerFrequency(report),
		TradeCount:                    tradeCount,
		RejectionCount:                totalRejections,
		SignalToExecDelay: DelayStats{
			SignalToDecisionMS: signalDelay,
			DecisionToFillMS:   fillDelay,
			SampleCount:        delayCount,
		},
		BySymbol:         symbolBuckets(report.BySymbol),
		BySide:           bucketStats(report.BySide),
		BySignalType:     signalBuckets(report.BySignalType),
		ByMarketState:    bucketStats(report.ByMarketState),
		ByATRProfile:     bucketStats(report.ByATRProfile),
		ByADXRange:       bucketStats(report.ByADXRange),
		BySymbolCategory: bucketStats(report.BySymbolCategory),
		ByTraderExchange: map[string]*BucketMetrics{},
	}
	if artifacts.Metrics != nil && artifacts.Metrics.MetricsSource != "" {
		metrics.MetricsSource = artifacts.Metrics.MetricsSource
	}
	metrics.BootstrapCIs = BootstrapIntervals(artifacts.Trades, cfg)
	metrics.CIStatus = aggregateCIStatus(metrics.BootstrapCIs)
	traderExchange := strings.Trim(metrics.TraderID+"|"+metrics.Exchange, "|")
	if traderExchange != "" {
		metrics.ByTraderExchange[traderExchange] = &BucketMetrics{
			TradeCount:               metrics.TradeCount,
			RejectionCount:           metrics.RejectionCount,
			WinRate:                  metrics.WinRate,
			ProfitFactor:             metrics.ProfitFactor,
			NetPnL:                   metrics.NetPnL,
			NetPnLPct:                metrics.NetPnLPct,
			MaxDrawdownPct:           metrics.MaxDrawdownPct,
			AverageR:                 metrics.AverageR,
			RejectionRate:            metrics.RejectionRate,
			MinNotionalRejectionRate: metrics.MinNotionalRejectionRate,
			SampleCount:              metrics.TradeCount + metrics.RejectionCount,
		}
	}
	return metrics, nil
}

func BootstrapIntervals(trades []backtest.TradeLifecycle, cfg *OptimizationConfig) map[string]BootstrapCI {
	if cfg == nil {
		cfg = DefaultOptimizationConfig()
	}
	ApplyOptimizationDefaults(cfg)
	seed := cfg.BootstrapSeed
	if seed == 0 {
		seed = 42
	}
	metrics := []string{"net_pnl", "win_rate", "profit_factor", "average_r"}
	out := make(map[string]BootstrapCI, len(metrics))
	if len(trades) < 2 {
		for _, metric := range metrics {
			out[metric] = BootstrapCI{Metric: metric, Status: "insufficient_samples", SampleCount: len(trades), Seed: seed}
		}
		return out
	}
	rng := rand.New(rand.NewSource(seed))
	iterations := cfg.BootstrapIterations
	if iterations <= 0 {
		iterations = DefaultBootstrapIterations
	}
	for _, metric := range metrics {
		samples := make([]float64, 0, iterations)
		for i := 0; i < iterations; i++ {
			resample := make([]backtest.TradeLifecycle, len(trades))
			for j := range resample {
				resample[j] = trades[rng.Intn(len(trades))]
			}
			samples = append(samples, bootstrapMetric(metric, resample))
		}
		sort.Float64s(samples)
		out[metric] = BootstrapCI{
			Metric:      metric,
			Interval:    percentileInterval(samples, 0.025, 0.975),
			Status:      "ok",
			Iterations:  iterations,
			SampleCount: len(trades),
			Seed:        seed,
		}
	}
	return out
}

func bootstrapMetric(metric string, trades []backtest.TradeLifecycle) float64 {
	switch metric {
	case "net_pnl":
		total := 0.0
		for _, trade := range trades {
			total += trade.RealizedPnL
		}
		return total
	case "win_rate":
		if len(trades) == 0 {
			return 0
		}
		wins := 0
		for _, trade := range trades {
			if trade.RealizedPnL >= 0 {
				wins++
			}
		}
		return float64(wins) / float64(len(trades)) * 100
	case "profit_factor":
		grossProfit := 0.0
		grossLoss := 0.0
		for _, trade := range trades {
			if trade.RealizedPnL >= 0 {
				grossProfit += trade.RealizedPnL
			} else {
				grossLoss += -trade.RealizedPnL
			}
		}
		if grossLoss == 0 {
			return 0
		}
		return grossProfit / grossLoss
	case "average_r":
		total := 0.0
		count := 0
		for _, trade := range trades {
			if trade.RMultiple != 0 {
				total += trade.RMultiple
				count++
			}
		}
		if count == 0 {
			return 0
		}
		return total / float64(count)
	default:
		return 0
	}
}

func percentileInterval(samples []float64, lower, upper float64) [2]float64 {
	if len(samples) == 0 {
		return [2]float64{}
	}
	idx := func(p float64) int {
		if p <= 0 {
			return 0
		}
		if p >= 1 {
			return len(samples) - 1
		}
		return int(math.Round(p * float64(len(samples)-1)))
	}
	return [2]float64{samples[idx(lower)], samples[idx(upper)]}
}

func aggregateCIStatus(cis map[string]BootstrapCI) string {
	if len(cis) == 0 {
		return ""
	}
	for _, ci := range cis {
		if ci.Status != "ok" {
			return ci.Status
		}
	}
	return "ok"
}

func bucketStats(in map[string]backtest.BucketStats) map[string]*BucketMetrics {
	out := map[string]*BucketMetrics{}
	for key, item := range in {
		tradesAndRejects := item.TradeCount + item.RejectionCount
		rejectionRate := 0.0
		if tradesAndRejects > 0 {
			rejectionRate = float64(item.RejectionCount) / float64(tradesAndRejects)
		}
		out[key] = &BucketMetrics{
			TradeCount:     item.TradeCount,
			RejectionCount: item.RejectionCount,
			WinRate:        item.WinRate,
			ProfitFactor:   item.ProfitFactor,
			NetPnL:         item.NetPnL,
			MaxDrawdownPct: item.MaxDrawdownPct,
			AverageR:       item.AverageR,
			RejectionRate:  rejectionRate,
			SampleCount:    tradesAndRejects,
		}
	}
	return out
}

func symbolBuckets(in map[string]backtest.SymbolStats) map[string]*BucketMetrics {
	out := map[string]*BucketMetrics{}
	for key, item := range in {
		out[key] = &BucketMetrics{
			TradeCount:  item.TradeCount,
			WinRate:     item.WinRate,
			NetPnL:      item.NetPnL,
			SampleCount: item.TradeCount,
		}
	}
	return out
}

func signalBuckets(in map[string]backtest.SignalStats) map[string]*BucketMetrics {
	out := map[string]*BucketMetrics{}
	for key, item := range in {
		rejectionRate := 0.0
		if item.Count > 0 {
			rejectionRate = float64(item.Rejected) / float64(item.Count)
		}
		out[key] = &BucketMetrics{
			TradeCount:     item.Executed,
			RejectionCount: item.Rejected,
			RejectionRate:  rejectionRate,
			SampleCount:    item.Count,
		}
	}
	return out
}

func circuitBreakerFrequency(report backtest.Report) float64 {
	days := report.BacktestTo.Sub(report.BacktestFrom).Hours() / 24
	if days <= 0 {
		return 0
	}
	return float64(len(report.CircuitBreakerEvents)) / days
}

func delayStats(events []backtest.ExecutionEvent) (float64, float64, int) {
	var signalToDecision float64
	var signalCount int
	var decisionToFill float64
	var fillCount int
	for _, event := range events {
		if event.SignalCloseTime > 0 && event.DecisionCloseTime >= event.SignalCloseTime {
			signalToDecision += float64(event.DecisionCloseTime - event.SignalCloseTime)
			signalCount++
		}
		if event.DecisionCloseTime > 0 && event.Timestamp.UnixMilli() >= event.DecisionCloseTime {
			decisionToFill += float64(event.Timestamp.UnixMilli() - event.DecisionCloseTime)
			fillCount++
		}
	}
	if signalCount > 0 {
		signalToDecision /= float64(signalCount)
	}
	if fillCount > 0 {
		decisionToFill /= float64(fillCount)
	}
	return signalToDecision, decisionToFill, maxInt(signalCount, fillCount)
}

func symbolSetHash(report backtest.Report, trades []backtest.TradeLifecycle) string {
	seen := map[string]bool{}
	for key := range report.BySymbol {
		seen[key] = true
	}
	for _, trade := range trades {
		seen[trade.Symbol] = true
	}
	var symbols []string
	for symbol := range seen {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return stableHash(symbols)
}

func stableHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
