package optimize

import (
	"sort"
	"time"

	"nofx/backtest"
)

type SignalQualityInput struct {
	Signals    []backtest.SignalOutcome
	Structures []StructureSnapshot
	Trades     []backtest.TradeLifecycle
	Executions []backtest.ExecutionEvent
}

type SignalQualityMetrics struct {
	SignalType       string  `json:"signal_type"`
	SourceLayer      string  `json:"source_layer,omitempty"`
	GenerationRate   float64 `json:"generation_rate_per_day"`
	ConfirmDelayBars float64 `json:"confirm_delay_bars_avg"`
	RevocationRate   float64 `json:"revocation_rate"`
	OneRHitRate      float64 `json:"one_r_hit_rate"`
	FinalRMultiple   float64 `json:"final_r_multiple_avg"`
	SampleCount      int     `json:"sample_count"`
}

type SignalQualityReport struct {
	RunID             string                            `json:"run_id"`
	Metrics           []SignalQualityMetrics            `json:"metrics"`
	ByATRProfile      map[string][]SignalQualityMetrics `json:"by_atr_profile,omitempty"`
	ByADXRange        map[string][]SignalQualityMetrics `json:"by_adx_range,omitempty"`
	MissingStructures bool                              `json:"missing_structures,omitempty"`
	Assumptions       []string                          `json:"assumptions,omitempty"`
}

type EquityCurveAnalysis struct {
	RunID                string                         `json:"run_id"`
	DailyEquity          []DailyEquityPoint             `json:"daily_equity"`
	RollingMetrics       []RollingMetricPoint           `json:"rolling_metrics"`
	DrawdownIntervals    []DrawdownAttribution          `json:"drawdown_intervals"`
	CircuitBreakerEvents []backtest.CircuitBreakerEvent `json:"circuit_breaker_events,omitempty"`
	LossModeSwitches     []LossModeSwitchEvent          `json:"loss_mode_switches,omitempty"`
}

type DailyEquityPoint struct {
	Date        string  `json:"date"`
	Equity      float64 `json:"equity"`
	NetPnL      float64 `json:"net_pnl"`
	DrawdownPct float64 `json:"drawdown_pct"`
}

type RollingMetricPoint struct {
	Timestamp    int64   `json:"timestamp"`
	WindowTrades int     `json:"window_trades"`
	ProfitFactor float64 `json:"profit_factor"`
	WinRate      float64 `json:"win_rate"`
}

type DrawdownAttribution struct {
	StartMS        int64   `json:"start_ms"`
	EndMS          int64   `json:"end_ms"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	LifecycleID    string  `json:"lifecycle_id,omitempty"`
	SignalType     string  `json:"signal_type,omitempty"`
	BTCMarketState string  `json:"btc_market_state,omitempty"`
	ATRProfile     string  `json:"atr_profile,omitempty"`
	ADXRange       string  `json:"adx_range,omitempty"`
	Exchange       string  `json:"exchange,omitempty"`
}

type LossModeSwitchEvent struct {
	Timestamp     int64   `json:"timestamp"`
	TriggerMetric string  `json:"trigger_metric"`
	BeforeMode    string  `json:"before_mode"`
	AfterMode     string  `json:"after_mode"`
	OpenFrequency float64 `json:"open_frequency,omitempty"`
}

type aggregate struct {
	count          int
	revoked        int
	oneR           int
	finalRTotal    float64
	finalRCount    int
	delayBarsTotal float64
	delayCount     int
	firstMS        int64
	lastMS         int64
	sourceLayer    string
}

type ConsistencyResult struct {
	ReplayRunID   string             `json:"replay_run_id,omitempty"`
	BacktestRunID string             `json:"backtest_run_id,omitempty"`
	Comparable    bool               `json:"comparable"`
	Verdict       string             `json:"verdict"`
	Comparisons   []MetricComparison `json:"comparisons"`
	Reasons       []string           `json:"reasons,omitempty"`
}

func BuildSignalQualityReport(runID string, input SignalQualityInput) (SignalQualityReport, error) {
	report := SignalQualityReport{
		RunID:        runID,
		ByATRProfile: map[string][]SignalQualityMetrics{},
		ByADXRange:   map[string][]SignalQualityMetrics{},
	}
	if len(input.Structures) == 0 {
		report.MissingStructures = true
		report.Assumptions = append(report.Assumptions, "缺少structures.json，无法批准结构级优化")
		return report, ErrMissingStructureSnapshots
	}
	aggs := map[string]*aggregate{}
	atrAggs := map[string]map[string]*aggregate{}
	adxAggs := map[string]map[string]*aggregate{}
	signalsByID := map[string]backtest.SignalOutcome{}
	tradesBySignal := map[string][]backtest.TradeLifecycle{}
	for _, signal := range input.Signals {
		signalsByID[signal.SignalID] = signal
	}
	for _, trade := range input.Trades {
		if trade.SignalID != "" {
			tradesBySignal[trade.SignalID] = append(tradesBySignal[trade.SignalID], trade)
		}
	}
	for _, snapshot := range input.Structures {
		key := snapshot.SignalType
		if key == "" {
			key = "unknown"
		}
		addSignalQualitySample(aggs, key, snapshot, signalsByID, tradesBySignal)
		if snapshot.ATRProfile != "" {
			if atrAggs[snapshot.ATRProfile] == nil {
				atrAggs[snapshot.ATRProfile] = map[string]*aggregate{}
			}
			addSignalQualitySample(atrAggs[snapshot.ATRProfile], key, snapshot, signalsByID, tradesBySignal)
		}
		if snapshot.ADXRange != "" {
			if adxAggs[snapshot.ADXRange] == nil {
				adxAggs[snapshot.ADXRange] = map[string]*aggregate{}
			}
			addSignalQualitySample(adxAggs[snapshot.ADXRange], key, snapshot, signalsByID, tradesBySignal)
		}
	}
	report.Metrics = qualityMetricsFromAggregates(aggs)
	for bucket, bucketAgg := range atrAggs {
		report.ByATRProfile[bucket] = qualityMetricsFromAggregates(bucketAgg)
	}
	for bucket, bucketAgg := range adxAggs {
		report.ByADXRange[bucket] = qualityMetricsFromAggregates(bucketAgg)
	}
	return report, nil
}

func AnalyzeEquityCurve(runID string, points []backtest.EquityPoint, trades []backtest.TradeLifecycle) EquityCurveAnalysis {
	analysis := EquityCurveAnalysis{RunID: runID}
	byDay := map[string]backtest.EquityPoint{}
	sortedPoints := append([]backtest.EquityPoint(nil), points...)
	sort.Slice(sortedPoints, func(i, j int) bool { return sortedPoints[i].Timestamp.Before(sortedPoints[j].Timestamp) })
	sortedTrades := append([]backtest.TradeLifecycle(nil), trades...)
	sort.Slice(sortedTrades, func(i, j int) bool { return sortedTrades[i].ExitTime.Before(sortedTrades[j].ExitTime) })
	for _, point := range sortedPoints {
		day := point.Timestamp.Format("2006-01-02")
		byDay[day] = point
		if point.DrawdownPct > 0 {
			trade := nearestTradeForDrawdown(point.Timestamp, sortedTrades)
			analysis.DrawdownIntervals = append(analysis.DrawdownIntervals, DrawdownAttribution{
				StartMS:        point.Timestamp.UnixMilli(),
				EndMS:          point.Timestamp.UnixMilli(),
				MaxDrawdownPct: point.DrawdownPct,
				LifecycleID:    trade.LifecycleID,
				SignalType:     trade.SignalType,
			})
		}
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		point := byDay[day]
		analysis.DailyEquity = append(analysis.DailyEquity, DailyEquityPoint{
			Date:        day,
			Equity:      point.Equity,
			NetPnL:      point.RealizedPnL,
			DrawdownPct: point.DrawdownPct,
		})
	}
	window := 30
	for i := range sortedTrades {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		slice := sortedTrades[start : i+1]
		analysis.RollingMetrics = append(analysis.RollingMetrics, RollingMetricPoint{
			Timestamp:    sortedTrades[i].ExitTime.UnixMilli(),
			WindowTrades: len(slice),
			ProfitFactor: profitFactor(slice),
			WinRate:      winRate(slice),
		})
	}
	return analysis
}

func addSignalQualitySample(aggs map[string]*aggregate, key string, snapshot StructureSnapshot, signals map[string]backtest.SignalOutcome, trades map[string][]backtest.TradeLifecycle) {
	item := aggs[key]
	if item == nil {
		item = &aggregate{}
		aggs[key] = item
	}
	item.count++
	item.sourceLayer = firstNonEmpty(item.sourceLayer, snapshot.SourceLayer)
	if snapshot.Revoked {
		item.revoked++
	}
	if snapshot.ConfirmCloseMS > 0 {
		if item.firstMS == 0 || snapshot.ConfirmCloseMS < item.firstMS {
			item.firstMS = snapshot.ConfirmCloseMS
		}
		if snapshot.ConfirmCloseMS > item.lastMS {
			item.lastMS = snapshot.ConfirmCloseMS
		}
	}
	if snapshot.SegmentEndMS > 0 && snapshot.ConfirmCloseMS >= snapshot.SegmentEndMS {
		item.delayBarsTotal += float64(snapshot.ConfirmCloseMS-snapshot.SegmentEndMS) / float64(timeframeMillis(snapshot.Timeframe))
		item.delayCount++
	}
	if signal, ok := signals[snapshot.SignalID]; ok && signal.Reached1R {
		item.oneR++
	}
	for _, trade := range trades[snapshot.SignalID] {
		item.finalRTotal += firstNonZero(trade.FinalRMultiple, trade.RMultiple)
		item.finalRCount++
	}
}

func qualityMetricsFromAggregates(aggs map[string]*aggregate) []SignalQualityMetrics {
	keys := make([]string, 0, len(aggs))
	for key := range aggs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]SignalQualityMetrics, 0, len(keys))
	for _, key := range keys {
		item := aggs[key]
		metric := SignalQualityMetrics{
			SignalType:  key,
			SourceLayer: item.sourceLayer,
			SampleCount: item.count,
		}
		if item.count > 0 {
			metric.RevocationRate = float64(item.revoked) / float64(item.count)
			metric.OneRHitRate = float64(item.oneR) / float64(item.count)
		}
		if item.delayCount > 0 {
			metric.ConfirmDelayBars = item.delayBarsTotal / float64(item.delayCount)
		}
		if item.finalRCount > 0 {
			metric.FinalRMultiple = item.finalRTotal / float64(item.finalRCount)
		}
		if item.firstMS > 0 && item.lastMS > item.firstMS {
			days := float64(item.lastMS-item.firstMS) / float64(24*time.Hour/time.Millisecond)
			if days > 0 {
				metric.GenerationRate = float64(item.count) / days
			}
		}
		out = append(out, metric)
	}
	return out
}

func timeframeMillis(timeframe string) int64 {
	switch timeframe {
	case "3m":
		return int64(3 * time.Minute / time.Millisecond)
	case "15m":
		return int64(15 * time.Minute / time.Millisecond)
	case "1h":
		return int64(time.Hour / time.Millisecond)
	case "4h":
		return int64(4 * time.Hour / time.Millisecond)
	default:
		return int64(time.Hour / time.Millisecond)
	}
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func nearestTradeForDrawdown(ts time.Time, trades []backtest.TradeLifecycle) backtest.TradeLifecycle {
	for _, trade := range trades {
		if !trade.ExitTime.IsZero() && !trade.ExitTime.Before(ts) {
			return trade
		}
	}
	if len(trades) > 0 {
		return trades[len(trades)-1]
	}
	return backtest.TradeLifecycle{}
}

func CompareReplayBacktest(replayID, backtestID string, replay, backtest *RunMetrics, tolerance float64) ConsistencyResult {
	result := ConsistencyResult{ReplayRunID: replayID, BacktestRunID: backtestID, Comparable: true, Verdict: "ok"}
	if replay == nil || backtest == nil {
		result.Comparable = false
		result.Verdict = "invalid_input"
		result.Reasons = append(result.Reasons, "replay或backtest指标为空")
		return result
	}
	result.Comparisons = append(result.Comparisons,
		absoluteDeltaComparison("win_rate", replay.WinRate, backtest.WinRate, tolerance),
		absoluteDeltaComparison("profit_factor", replay.ProfitFactor, backtest.ProfitFactor, tolerance),
		absoluteDeltaComparison("rejection_rate", replay.RejectionRate, backtest.RejectionRate, tolerance),
		absoluteDeltaComparison("min_notional_rejection_rate", replay.MinNotionalRejectionRate, backtest.MinNotionalRejectionRate, tolerance),
	)
	for _, cmp := range result.Comparisons {
		if !cmp.Passed {
			result.Verdict = "warning"
			result.Reasons = append(result.Reasons, cmp.Metric+"差异超过阈值")
		}
	}
	return result
}

func profitFactor(trades []backtest.TradeLifecycle) float64 {
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
}

func winRate(trades []backtest.TradeLifecycle) float64 {
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
}

func unixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
