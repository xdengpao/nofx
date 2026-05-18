package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"nofx/decision"
	"nofx/strategy/chanlun"
)

type Report struct {
	RunID                    string                 `json:"run_id"`
	GeneratedAt              time.Time              `json:"generated_at"`
	GitCommit                string                 `json:"git_commit,omitempty"`
	ConfigHash               string                 `json:"config_hash"`
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
	RejectionBuckets         map[string]int         `json:"rejection_buckets"`
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

type ReportArtifacts struct {
	Report     Report
	Trades     []TradeLifecycle
	Executions []ExecutionEvent
	Signals    []SignalOutcome
	Rejections []decision.OpenRejection
	Equity     []EquityPoint
	Markers    map[string][]chanlun.SignalMarker
}

func WriteArtifacts(dir string, artifacts ReportArtifacts) error {
	if err := os.MkdirAll(filepath.Join(dir, "markers"), 0755); err != nil {
		return err
	}
	report := artifacts.Report
	report.Files = map[string]string{
		"report":     "report.json",
		"trades":     "trades.csv",
		"equity":     "equity.csv",
		"signals":    "signals.csv",
		"rejections": "rejections.csv",
	}
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

func BuildSummary(initial float64, final PaperAccount, trades []TradeLifecycle, executions []ExecutionEvent, signals []SignalOutcome, rejections []decision.OpenRejection, equity []EquityPoint) SummaryStats {
	wins := 0
	grossProfit := 0.0
	grossLoss := 0.0
	for _, trade := range trades {
		if trade.RealizedPnL >= 0 {
			wins++
			grossProfit += trade.RealizedPnL
		} else {
			grossLoss += -trade.RealizedPnL
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
	return SummaryStats{
		InitialEquity:       initial,
		FinalEquity:         final.Equity,
		NetPnL:              final.Equity - initial,
		NetReturnPct:        (final.Equity - initial) / initial * 100,
		MaxDrawdownPct:      maxDrawdown(equity),
		WinRate:             winRate,
		ProfitFactor:        profitFactor,
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
	_ = w.Write([]string{"lifecycle_id", "symbol", "side", "entry_time", "exit_time", "entry_price", "exit_price", "realized_pnl", "fees", "entry_reason", "exit_reason", "signal_id", "signal_type", "closed"})
	for _, row := range rows {
		_ = w.Write([]string{row.LifecycleID, row.Symbol, row.Side, row.EntryTime.Format(time.RFC3339), row.ExitTime.Format(time.RFC3339), f(row.EntryPrice), f(row.ExitPrice), f(row.RealizedPnL), f(row.Fees), row.EntryReason, row.ExitReason, row.SignalID, row.SignalType, strconv.FormatBool(row.Closed)})
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

func f(value float64) string { return fmt.Sprintf("%.8f", value) }
