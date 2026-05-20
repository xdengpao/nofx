package optimize

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nofx/backtest"
	"nofx/decision"
	"nofx/strategy/chanlun"
)

func LoadRunArtifacts(outputDir string) (*RunArtifacts, error) {
	if strings.TrimSpace(outputDir) == "" {
		return nil, fmt.Errorf("run输出目录为空")
	}
	var report backtest.Report
	if err := readJSON(filepath.Join(outputDir, "report.json"), &report); err != nil {
		return nil, err
	}
	var configSnapshot map[string]any
	if err := readJSON(filepath.Join(outputDir, "config_snapshot.json"), &configSnapshot); err == nil && len(configSnapshot) > 0 {
		report.ConfigSnapshot = configSnapshot
	}
	artifacts := &RunArtifacts{
		RunID:     report.RunID,
		OutputDir: outputDir,
		Report:    report,
		Markers:   map[string][]chanlun.SignalMarker{},
	}
	artifacts.Trades, _ = readTradesCSV(filepath.Join(outputDir, "trades.csv"))
	artifacts.Signals, _ = readSignalsCSV(filepath.Join(outputDir, "signals.csv"))
	artifacts.Rejections, _ = readRejectionsCSV(filepath.Join(outputDir, "rejections.csv"))
	artifacts.Equity, _ = readEquityCSV(filepath.Join(outputDir, "equity.csv"))
	metricsSource := "derived"
	var metricsSnapshot backtest.MetricsSnapshot
	if err := readJSON(filepath.Join(outputDir, "metrics.json"), &metricsSnapshot); err == nil {
		metricsSource = "artifact"
		if report.DataHash == "" {
			report.DataHash = metricsSnapshot.DataHash
		}
		if len(report.DataHashes) == 0 {
			report.DataHashes = metricsSnapshot.DataHashes
		}
		if report.TraderID == "" {
			report.TraderID = metricsSnapshot.TraderID
		}
		if report.Exchange == "" {
			report.Exchange = metricsSnapshot.Exchange
		}
		if report.ConfigHash == "" {
			report.ConfigHash = metricsSnapshot.ConfigHash
		}
		if report.Timezone == "" {
			report.Timezone = metricsSnapshot.Timezone
		}
	}
	structuresPath := filepath.Join(outputDir, "structures.json")
	if err := readJSON(structuresPath, &artifacts.Structures); err != nil {
		if os.IsNotExist(err) {
			return artifacts, ErrMissingStructureSnapshots
		}
		return artifacts, err
	}
	_ = readMarkers(filepath.Join(outputDir, "markers"), artifacts.Markers)
	metrics, err := ExtractRunMetrics(artifacts)
	if err != nil {
		return nil, err
	}
	metrics.MetricsSource = metricsSource
	artifacts.Metrics = metrics
	return artifacts, nil
}

func readJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("解析%s失败: %w", path, err)
	}
	return nil
}

func readMarkers(dir string, out map[string][]chanlun.SignalMarker) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var markers []chanlun.SignalMarker
		if err := readJSON(filepath.Join(dir, entry.Name()), &markers); err != nil {
			return err
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		out[key] = markers
	}
	return nil
}

func readTradesCSV(path string) ([]backtest.TradeLifecycle, error) {
	records, err := readCSV(path)
	if err != nil || len(records) <= 1 {
		return nil, err
	}
	header := headerIndex(records[0])
	var rows []backtest.TradeLifecycle
	for _, record := range records[1:] {
		rows = append(rows, backtest.TradeLifecycle{
			LifecycleID:     csvString(record, header, "lifecycle_id"),
			Symbol:          csvString(record, header, "symbol"),
			Side:            csvString(record, header, "side"),
			EntryTime:       csvTime(record, header, "entry_time"),
			ExitTime:        csvTime(record, header, "exit_time"),
			EntryPrice:      csvFloat(record, header, "entry_price"),
			ExitPrice:       csvFloat(record, header, "exit_price"),
			RealizedPnL:     csvFloat(record, header, "realized_pnl"),
			Fees:            csvFloat(record, header, "fees"),
			RMultiple:       csvFloat(record, header, "r_multiple"),
			MFE:             csvFloat(record, header, "mfe"),
			MAE:             csvFloat(record, header, "mae"),
			MaxDrawdownPct:  csvFloat(record, header, "max_drawdown_pct"),
			FinalRMultiple:  csvFloat(record, header, "final_r_multiple"),
			RecoveryMinutes: csvFloat(record, header, "recovery_minutes"),
			DurationMinutes: csvFloat(record, header, "duration_minutes"),
			EntryReason:     csvString(record, header, "entry_reason"),
			ExitReason:      csvString(record, header, "exit_reason"),
			SignalID:        csvString(record, header, "signal_id"),
			SignalType:      csvString(record, header, "signal_type"),
			Closed:          csvBool(record, header, "closed"),
		})
	}
	return rows, nil
}

func readSignalsCSV(path string) ([]backtest.SignalOutcome, error) {
	records, err := readCSV(path)
	if err != nil || len(records) <= 1 {
		return nil, err
	}
	header := headerIndex(records[0])
	var rows []backtest.SignalOutcome
	for _, record := range records[1:] {
		rows = append(rows, backtest.SignalOutcome{
			SignalID:          csvString(record, header, "signal_id"),
			Symbol:            csvString(record, header, "symbol"),
			Timeframe:         csvString(record, header, "timeframe"),
			SignalType:        csvString(record, header, "signal_type"),
			Direction:         csvString(record, header, "direction"),
			Status:            csvString(record, header, "status"),
			TradeIntent:       csvString(record, header, "trade_intent"),
			PositionSide:      csvString(record, header, "position_side"),
			SignalCloseTime:   csvInt64(record, header, "signal_close_time"),
			DecisionCloseTime: csvInt64(record, header, "decision_close_time"),
			DisplayCloseTime:  csvInt64(record, header, "display_close_time"),
			Price:             csvFloat(record, header, "price"),
			MFE:               csvFloat(record, header, "mfe"),
			MAE:               csvFloat(record, header, "mae"),
			Reached1R:         csvBool(record, header, "reached_1r"),
			Reason:            csvString(record, header, "reason"),
		})
	}
	return rows, nil
}

func readRejectionsCSV(path string) ([]decision.OpenRejection, error) {
	records, err := readCSV(path)
	if err != nil || len(records) <= 1 {
		return nil, err
	}
	header := headerIndex(records[0])
	var rows []decision.OpenRejection
	for _, record := range records[1:] {
		rows = append(rows, decision.OpenRejection{
			Symbol:            csvString(record, header, "symbol"),
			Action:            csvString(record, header, "action"),
			Reason:            csvString(record, header, "reason"),
			SignalID:          csvString(record, header, "signal_id"),
			SignalType:        csvString(record, header, "signal_type"),
			SignalCloseTime:   csvInt64(record, header, "signal_close_time"),
			DecisionCloseTime: csvInt64(record, header, "decision_close_time"),
		})
	}
	return rows, nil
}

func readEquityCSV(path string) ([]backtest.EquityPoint, error) {
	records, err := readCSV(path)
	if err != nil || len(records) <= 1 {
		return nil, err
	}
	header := headerIndex(records[0])
	var rows []backtest.EquityPoint
	for _, record := range records[1:] {
		rows = append(rows, backtest.EquityPoint{
			Timestamp:     csvTime(record, header, "timestamp"),
			Equity:        csvFloat(record, header, "equity"),
			Cash:          csvFloat(record, header, "cash"),
			UnrealizedPnL: csvFloat(record, header, "unrealized_pnl"),
			RealizedPnL:   csvFloat(record, header, "realized_pnl"),
			DrawdownPct:   csvFloat(record, header, "drawdown"),
		})
	}
	return rows, nil
}

func readCSV(path string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return csv.NewReader(file).ReadAll()
}

func headerIndex(header []string) map[string]int {
	out := map[string]int{}
	for i, value := range header {
		out[value] = i
	}
	return out
}

func csvString(record []string, header map[string]int, key string) string {
	i, ok := header[key]
	if !ok || i >= len(record) {
		return ""
	}
	return record[i]
}

func csvFloat(record []string, header map[string]int, key string) float64 {
	value, _ := strconv.ParseFloat(csvString(record, header, key), 64)
	return value
}

func csvInt64(record []string, header map[string]int, key string) int64 {
	value, _ := strconv.ParseInt(csvString(record, header, key), 10, 64)
	return value
}

func csvBool(record []string, header map[string]int, key string) bool {
	value, _ := strconv.ParseBool(csvString(record, header, key))
	return value
}

func csvTime(record []string, header map[string]int, key string) time.Time {
	value := csvString(record, header, key)
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, value)
	return t
}
