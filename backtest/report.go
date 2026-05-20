package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nofx/decision"
	"nofx/strategy/chanlun"
)

type Report struct {
	RunID                    string                 `json:"run_id"`
	GeneratedAt              time.Time              `json:"generated_at"`
	GitCommit                string                 `json:"git_commit,omitempty"`
	ConfigHash               string                 `json:"config_hash"`
	DataHash                 string                 `json:"data_hash,omitempty"`
	DataHashes               map[string]string      `json:"data_hashes,omitempty"`
	TraderID                 string                 `json:"trader_id,omitempty"`
	Exchange                 string                 `json:"exchange,omitempty"`
	Timezone                 string                 `json:"timezone"`
	WarmupFrom               time.Time              `json:"warmup_from"`
	BacktestFrom             time.Time              `json:"backtest_from"`
	BacktestTo               time.Time              `json:"backtest_to"`
	MarketDataSource         string                 `json:"market_data_source"`
	ExecutionModel           string                 `json:"execution_model"`
	InstrumentMetadataSource string                 `json:"instrument_metadata_source"`
	FundingMode              string                 `json:"funding_mode"`
	OIMode                   string                 `json:"oi_mode"`
	LiquidationMode          string                 `json:"liquidation_mode"`
	CandidatePoolMode        string                 `json:"candidate_pool_mode"`
	Assumptions              []string               `json:"assumptions"`
	ConfigSnapshot           map[string]any         `json:"config_snapshot"`
	Summary                  SummaryStats           `json:"summary"`
	BySymbol                 map[string]SymbolStats `json:"by_symbol"`
	BySignalType             map[string]SignalStats `json:"by_signal_type"`
	BySide                   map[string]BucketStats `json:"by_side,omitempty"`
	ByMarketState            map[string]BucketStats `json:"by_market_state,omitempty"`
	ByATRProfile             map[string]BucketStats `json:"by_atr_profile,omitempty"`
	ByADXRange               map[string]BucketStats `json:"by_adx_range,omitempty"`
	BySymbolCategory         map[string]BucketStats `json:"by_symbol_category,omitempty"`
	RejectionBuckets         map[string]int         `json:"rejection_buckets"`
	MinNotionalRejects       int                    `json:"min_notional_rejects,omitempty"`
	CircuitBreakerEvents     []CircuitBreakerEvent  `json:"circuit_breaker_events,omitempty"`
	Files                    map[string]string      `json:"files"`
	Cancelled                bool                   `json:"cancelled,omitempty"`
}

type SummaryStats struct {
	InitialEquity       float64 `json:"initial_equity"`
	FinalEquity         float64 `json:"final_equity"`
	NetPnL              float64 `json:"net_pnl"`
	NetReturnPct        float64 `json:"net_return_pct"`
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`
	WinRate             float64 `json:"win_rate"`
	ProfitFactor        float64 `json:"profit_factor"`
	AverageR            float64 `json:"average_r"`
	TradeCount          int     `json:"trade_count"`
	ExecutionEventCount int     `json:"execution_event_count"`
	SignalCount         int     `json:"signal_count"`
	RejectionCount      int     `json:"rejection_count"`
	TotalFees           float64 `json:"total_fees"`
	TotalSlippage       float64 `json:"total_slippage"`
}

type SymbolStats struct {
	TradeCount int     `json:"trade_count"`
	NetPnL     float64 `json:"net_pnl"`
	WinRate    float64 `json:"win_rate"`
	Fees       float64 `json:"fees"`
}

type SignalStats struct {
	Count    int `json:"count"`
	Executed int `json:"executed"`
	Rejected int `json:"rejected"`
}

type BucketStats struct {
	TradeCount     int     `json:"trade_count"`
	RejectionCount int     `json:"rejection_count,omitempty"`
	NetPnL         float64 `json:"net_pnl"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor,omitempty"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct,omitempty"`
	AverageR       float64 `json:"average_r,omitempty"`
}

type CircuitBreakerEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	TriggerMetric string    `json:"trigger_metric"`
	PreFrequency  float64   `json:"pre_open_frequency"`
	PostFrequency float64   `json:"post_open_frequency"`
}

type TradeLifecycle struct {
	LifecycleID     string    `json:"lifecycle_id"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	EntryTime       time.Time `json:"entry_time"`
	ExitTime        time.Time `json:"exit_time,omitempty"`
	EntryPrice      float64   `json:"entry_price"`
	ExitPrice       float64   `json:"exit_price,omitempty"`
	EntryReason     string    `json:"entry_reason,omitempty"`
	ExitReason      string    `json:"exit_reason,omitempty"`
	RealizedPnL     float64   `json:"realized_pnl"`
	Fees            float64   `json:"fees"`
	RMultiple       float64   `json:"r_multiple,omitempty"`
	MFE             float64   `json:"mfe,omitempty"`
	MAE             float64   `json:"mae,omitempty"`
	MaxDrawdownPct  float64   `json:"max_drawdown_pct,omitempty"`
	FinalRMultiple  float64   `json:"final_r_multiple,omitempty"`
	RecoveryMinutes float64   `json:"recovery_minutes,omitempty"`
	DurationMinutes float64   `json:"duration_minutes,omitempty"`
	SignalID        string    `json:"signal_id,omitempty"`
	SignalType      string    `json:"signal_type,omitempty"`
	ExecutionCount  int       `json:"execution_count"`
	Closed          bool      `json:"closed"`
}

type ExecutionEvent struct {
	Timestamp         time.Time `json:"timestamp"`
	Symbol            string    `json:"symbol"`
	Action            string    `json:"action"`
	Side              string    `json:"side"`
	Status            string    `json:"status"`
	Price             float64   `json:"price"`
	Quantity          float64   `json:"quantity"`
	Fee               float64   `json:"fee"`
	Reason            string    `json:"reason,omitempty"`
	SignalID          string    `json:"signal_id,omitempty"`
	SignalType        string    `json:"signal_type,omitempty"`
	SignalCloseTime   int64     `json:"signal_close_time,omitempty"`
	DecisionCloseTime int64     `json:"decision_close_time,omitempty"`
}

type SignalOutcome struct {
	SignalID          string  `json:"signal_id"`
	Symbol            string  `json:"symbol"`
	Timeframe         string  `json:"timeframe"`
	SignalType        string  `json:"signal_type"`
	Direction         string  `json:"direction"`
	Status            string  `json:"status"`
	TradeIntent       string  `json:"trade_intent,omitempty"`
	PositionSide      string  `json:"position_side,omitempty"`
	SignalCloseTime   int64   `json:"signal_close_time,omitempty"`
	DecisionCloseTime int64   `json:"decision_close_time,omitempty"`
	DisplayCloseTime  int64   `json:"display_close_time,omitempty"`
	Price             float64 `json:"price,omitempty"`
	MFE               float64 `json:"mfe,omitempty"`
	MAE               float64 `json:"mae,omitempty"`
	Reached1R         bool    `json:"reached_1r,omitempty"`
	Reason            string  `json:"reason,omitempty"`
}

type EquityPoint struct {
	Timestamp     time.Time `json:"timestamp"`
	Equity        float64   `json:"equity"`
	Cash          float64   `json:"cash"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	RealizedPnL   float64   `json:"realized_pnl"`
	DrawdownPct   float64   `json:"drawdown_pct"`
}

type StructureSnapshot struct {
	SnapshotID       string  `json:"snapshot_id"`
	TraderID         string  `json:"trader_id"`
	Exchange         string  `json:"exchange,omitempty"`
	Symbol           string  `json:"symbol"`
	Timeframe        string  `json:"timeframe"`
	StructureKey     string  `json:"structure_key"`
	SignalID         string  `json:"signal_id,omitempty"`
	EntryTriggerID   string  `json:"entry_trigger_id,omitempty"`
	SignalType       string  `json:"signal_type,omitempty"`
	SourceLayer      string  `json:"source_layer,omitempty"`
	CenterID         string  `json:"center_id,omitempty"`
	SegmentEndMS     int64   `json:"segment_end_ms,omitempty"`
	ConfirmCloseMS   int64   `json:"confirm_close_ms,omitempty"`
	Revoked          bool    `json:"revoked,omitempty"`
	RevokedAtMS      int64   `json:"revoked_at_ms,omitempty"`
	RevocationReason string  `json:"revocation_reason,omitempty"`
	ATRProfile       string  `json:"atr_profile,omitempty"`
	ADXRange         string  `json:"adx_range,omitempty"`
	BTCMarketState   string  `json:"btc_market_state,omitempty"`
	StructureTarget  float64 `json:"structure_target,omitempty"`
	ParentSignalID   string  `json:"parent_signal_id,omitempty"`
}

type MetricsSnapshot struct {
	RunID                      string                 `json:"run_id"`
	DataHash                   string                 `json:"data_hash,omitempty"`
	DataHashes                 map[string]string      `json:"data_hashes,omitempty"`
	TraderID                   string                 `json:"trader_id,omitempty"`
	Exchange                   string                 `json:"exchange,omitempty"`
	ConfigHash                 string                 `json:"config_hash"`
	Timezone                   string                 `json:"timezone"`
	Summary                    SummaryStats           `json:"summary"`
	BySymbol                   map[string]SymbolStats `json:"by_symbol,omitempty"`
	BySignalType               map[string]SignalStats `json:"by_signal_type,omitempty"`
	BySide                     map[string]BucketStats `json:"by_side,omitempty"`
	ByMarketState              map[string]BucketStats `json:"by_market_state,omitempty"`
	ByATRProfile               map[string]BucketStats `json:"by_atr_profile,omitempty"`
	ByADXRange                 map[string]BucketStats `json:"by_adx_range,omitempty"`
	BySymbolCategory           map[string]BucketStats `json:"by_symbol_category,omitempty"`
	RejectionBuckets           map[string]int         `json:"rejection_buckets,omitempty"`
	MinNotionalRejects         int                    `json:"min_notional_rejects,omitempty"`
	CircuitBreakerEvents       []CircuitBreakerEvent  `json:"circuit_breaker_events,omitempty"`
	SignalToDecisionDelayMS    float64                `json:"signal_to_decision_delay_ms_avg,omitempty"`
	DecisionToExecutionDelayMS float64                `json:"decision_to_execution_delay_ms_avg,omitempty"`
}

type ReportArtifacts struct {
	Report     Report
	Trades     []TradeLifecycle
	Executions []ExecutionEvent
	Signals    []SignalOutcome
	Rejections []decision.OpenRejection
	Equity     []EquityPoint
	Markers    map[string][]chanlun.SignalMarker
	Structures []StructureSnapshot
}

func WriteArtifacts(dir string, artifacts ReportArtifacts) error {
	if err := os.MkdirAll(filepath.Join(dir, "markers"), 0755); err != nil {
		return err
	}
	report := artifacts.Report
	report.Files = ArtifactFiles()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0644); err != nil {
		return err
	}
	if err := writeTradesCSV(filepath.Join(dir, "trades.csv"), artifacts.Trades); err != nil {
		return err
	}
	if err := writeEquityCSV(filepath.Join(dir, "equity.csv"), artifacts.Equity); err != nil {
		return err
	}
	if err := writeSignalsCSV(filepath.Join(dir, "signals.csv"), artifacts.Signals); err != nil {
		return err
	}
	if err := writeRejectionsCSV(filepath.Join(dir, "rejections.csv"), artifacts.Rejections); err != nil {
		return err
	}
	structuresData, err := json.MarshalIndent(artifacts.Structures, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "structures.json"), structuresData, 0644); err != nil {
		return err
	}
	metrics := buildMetricsSnapshot(report, artifacts.Executions)
	metricsData, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "metrics.json"), metricsData, 0644); err != nil {
		return err
	}
	for key, markers := range artifacts.Markers {
		markerData, err := json.MarshalIndent(markers, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "markers", key+".json"), markerData, 0644); err != nil {
			return err
		}
	}
	return nil
}

func ArtifactFiles() map[string]string {
	return map[string]string{
		"report":          "report.json",
		"config_snapshot": "config_snapshot.json",
		"trades":          "trades.csv",
		"equity":          "equity.csv",
		"signals":         "signals.csv",
		"rejections":      "rejections.csv",
		"markers":         "markers/",
		"structures":      "structures.json",
		"metrics":         "metrics.json",
	}
}

func BuildSummary(initial float64, final PaperAccount, trades []TradeLifecycle, executions []ExecutionEvent, signals []SignalOutcome, rejections []decision.OpenRejection, equity []EquityPoint) SummaryStats {
	wins := 0
	grossProfit := 0.0
	grossLoss := 0.0
	totalR := 0.0
	rCount := 0
	for _, trade := range trades {
		if trade.RealizedPnL >= 0 {
			wins++
			grossProfit += trade.RealizedPnL
		} else {
			grossLoss += -trade.RealizedPnL
		}
		if trade.RMultiple != 0 {
			totalR += trade.RMultiple
			rCount++
		}
	}
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(wins) / float64(len(trades)) * 100
	}
	profitFactor := 0.0
	if grossLoss > 0 {
		profitFactor = grossProfit / grossLoss
	}
	averageR := 0.0
	if rCount > 0 {
		averageR = totalR / float64(rCount)
	}
	return SummaryStats{
		InitialEquity:       initial,
		FinalEquity:         final.Equity,
		NetPnL:              final.Equity - initial,
		NetReturnPct:        (final.Equity - initial) / initial * 100,
		MaxDrawdownPct:      maxDrawdown(equity),
		WinRate:             winRate,
		ProfitFactor:        profitFactor,
		AverageR:            averageR,
		TradeCount:          len(trades),
		ExecutionEventCount: len(executions),
		SignalCount:         len(signals),
		RejectionCount:      len(rejections),
		TotalFees:           final.FeePaid,
		TotalSlippage:       final.SlippagePaid,
	}
}

func writeTradesCSV(path string, rows []TradeLifecycle) error {
	w, file, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer w.Flush()
	_ = w.Write([]string{"lifecycle_id", "symbol", "side", "entry_time", "exit_time", "entry_price", "exit_price", "realized_pnl", "fees", "r_multiple", "mfe", "mae", "max_drawdown_pct", "final_r_multiple", "recovery_minutes", "duration_minutes", "entry_reason", "exit_reason", "signal_id", "signal_type", "closed"})
	for _, row := range rows {
		_ = w.Write([]string{row.LifecycleID, row.Symbol, row.Side, row.EntryTime.Format(time.RFC3339), row.ExitTime.Format(time.RFC3339), f(row.EntryPrice), f(row.ExitPrice), f(row.RealizedPnL), f(row.Fees), f(row.RMultiple), f(row.MFE), f(row.MAE), f(row.MaxDrawdownPct), f(row.FinalRMultiple), f(row.RecoveryMinutes), f(row.DurationMinutes), row.EntryReason, row.ExitReason, row.SignalID, row.SignalType, strconv.FormatBool(row.Closed)})
	}
	return w.Error()
}

func writeEquityCSV(path string, rows []EquityPoint) error {
	w, file, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer w.Flush()
	_ = w.Write([]string{"timestamp", "equity", "cash", "unrealized_pnl", "realized_pnl", "drawdown"})
	for _, row := range rows {
		_ = w.Write([]string{row.Timestamp.Format(time.RFC3339), f(row.Equity), f(row.Cash), f(row.UnrealizedPnL), f(row.RealizedPnL), f(row.DrawdownPct)})
	}
	return w.Error()
}

func writeSignalsCSV(path string, rows []SignalOutcome) error {
	w, file, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer w.Flush()
	_ = w.Write([]string{"signal_id", "symbol", "timeframe", "signal_type", "direction", "status", "trade_intent", "position_side", "signal_close_time", "decision_close_time", "display_close_time", "price", "mfe", "mae", "reached_1r", "reason"})
	for _, row := range rows {
		_ = w.Write([]string{row.SignalID, row.Symbol, row.Timeframe, row.SignalType, row.Direction, row.Status, row.TradeIntent, row.PositionSide, strconv.FormatInt(row.SignalCloseTime, 10), strconv.FormatInt(row.DecisionCloseTime, 10), strconv.FormatInt(row.DisplayCloseTime, 10), f(row.Price), f(row.MFE), f(row.MAE), strconv.FormatBool(row.Reached1R), row.Reason})
	}
	return w.Error()
}

func writeRejectionsCSV(path string, rows []decision.OpenRejection) error {
	w, file, err := csvWriter(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer w.Flush()
	_ = w.Write([]string{"symbol", "action", "reason", "signal_id", "signal_type", "signal_close_time", "decision_close_time"})
	for _, row := range rows {
		_ = w.Write([]string{row.Symbol, row.Action, row.Reason, row.SignalID, row.SignalType, strconv.FormatInt(row.SignalCloseTime, 10), strconv.FormatInt(row.DecisionCloseTime, 10)})
	}
	return w.Error()
}

func csvWriter(path string) (*csv.Writer, *os.File, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return csv.NewWriter(file), file, nil
}

func maxDrawdown(points []EquityPoint) float64 {
	peak := 0.0
	maxDD := 0.0
	for _, point := range points {
		if point.Equity > peak {
			peak = point.Equity
		}
		if peak > 0 {
			dd := (peak - point.Equity) / peak * 100
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func BuildStructureSnapshots(traderID, exchange string, markers map[string][]chanlun.SignalMarker) []StructureSnapshot {
	seen := map[string]bool{}
	var out []StructureSnapshot
	for _, list := range markers {
		for _, marker := range list {
			if marker.StructureKey == "" && marker.SignalID == "" {
				continue
			}
			id := marker.StructureKey
			if id == "" {
				id = marker.SignalID
			}
			key := id + "|" + marker.SignalID + "|" + marker.Status + "|" + marker.Action
			if seen[key] {
				continue
			}
			seen[key] = true
			confirmMS := marker.SignalCloseTime
			if confirmMS == 0 {
				confirmMS = marker.CloseTime
			}
			out = append(out, StructureSnapshot{
				SnapshotID:       key,
				TraderID:         traderID,
				Exchange:         exchange,
				Symbol:           marker.Symbol,
				Timeframe:        marker.Timeframe,
				StructureKey:     marker.StructureKey,
				SignalID:         marker.SignalID,
				EntryTriggerID:   marker.EntryTriggerID,
				SignalType:       marker.SignalType,
				SourceLayer:      marker.SourceLayer,
				SegmentEndMS:     marker.SignalCloseTime,
				ConfirmCloseMS:   confirmMS,
				Revoked:          marker.EntryInvalidated || marker.Status == "rejected" || marker.Status == "invalidated",
				RevokedAtMS:      marker.LastUpdatedAt,
				RevocationReason: firstNonEmpty(marker.EntryInvalidReason, marker.Reason),
				StructureTarget:  marker.Price,
				ParentSignalID:   marker.ParentSignalID,
			})
		}
	}
	return out
}

func BuildBucketStats(trades []TradeLifecycle, keyFn func(TradeLifecycle) string) map[string]BucketStats {
	stats := map[string]BucketStats{}
	wins := map[string]int{}
	grossProfit := map[string]float64{}
	grossLoss := map[string]float64{}
	totalR := map[string]float64{}
	rCount := map[string]int{}
	for _, trade := range trades {
		key := keyFn(trade)
		if key == "" {
			key = "unknown"
		}
		item := stats[key]
		item.TradeCount++
		item.NetPnL += trade.RealizedPnL
		if trade.RealizedPnL >= 0 {
			wins[key]++
			grossProfit[key] += trade.RealizedPnL
		} else {
			grossLoss[key] += -trade.RealizedPnL
		}
		if trade.RMultiple != 0 {
			totalR[key] += trade.RMultiple
			rCount[key]++
		}
		stats[key] = item
	}
	for key, item := range stats {
		if item.TradeCount > 0 {
			item.WinRate = float64(wins[key]) / float64(item.TradeCount) * 100
		}
		if grossLoss[key] > 0 {
			item.ProfitFactor = grossProfit[key] / grossLoss[key]
		}
		if rCount[key] > 0 {
			item.AverageR = totalR[key] / float64(rCount[key])
		}
		stats[key] = item
	}
	return stats
}

func CountMinNotionalRejects(rejections []decision.OpenRejection) int {
	count := 0
	for _, rejection := range rejections {
		reason := strings.ToLower(rejection.Reason + " " + strings.Join(rejection.GateReasons, " "))
		if strings.Contains(reason, "名义额") || strings.Contains(reason, "min_notional") || strings.Contains(reason, "minimum notional") {
			count++
		}
	}
	return count
}

func buildMetricsSnapshot(report Report, executions []ExecutionEvent) MetricsSnapshot {
	signalDelay, fillDelay := averageDelays(executions)
	return MetricsSnapshot{
		RunID:                      report.RunID,
		DataHash:                   report.DataHash,
		DataHashes:                 report.DataHashes,
		TraderID:                   report.TraderID,
		Exchange:                   report.Exchange,
		ConfigHash:                 report.ConfigHash,
		Timezone:                   report.Timezone,
		Summary:                    report.Summary,
		BySymbol:                   report.BySymbol,
		BySignalType:               report.BySignalType,
		BySide:                     report.BySide,
		ByMarketState:              report.ByMarketState,
		ByATRProfile:               report.ByATRProfile,
		ByADXRange:                 report.ByADXRange,
		BySymbolCategory:           report.BySymbolCategory,
		RejectionBuckets:           report.RejectionBuckets,
		MinNotionalRejects:         report.MinNotionalRejects,
		CircuitBreakerEvents:       report.CircuitBreakerEvents,
		SignalToDecisionDelayMS:    signalDelay,
		DecisionToExecutionDelayMS: fillDelay,
	}
}

func averageDelays(executions []ExecutionEvent) (float64, float64) {
	var signalToDecision float64
	var signalCount int
	var decisionToExecution float64
	var fillCount int
	for _, event := range executions {
		if event.SignalCloseTime > 0 && event.DecisionCloseTime > 0 && event.DecisionCloseTime >= event.SignalCloseTime {
			signalToDecision += float64(event.DecisionCloseTime - event.SignalCloseTime)
			signalCount++
		}
		if event.DecisionCloseTime > 0 && event.Timestamp.UnixMilli() >= event.DecisionCloseTime {
			decisionToExecution += float64(event.Timestamp.UnixMilli() - event.DecisionCloseTime)
			fillCount++
		}
	}
	if signalCount > 0 {
		signalToDecision /= float64(signalCount)
	}
	if fillCount > 0 {
		decisionToExecution /= float64(fillCount)
	}
	return signalToDecision, decisionToExecution
}

func SymbolCategory(symbol string) string {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "BTCUSDT", "BTC":
		return "BTC"
	case "ETHUSDT", "ETH":
		return "ETH"
	default:
		return "altcoin"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func f(value float64) string { return fmt.Sprintf("%.8f", value) }
