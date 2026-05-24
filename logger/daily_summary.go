package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type DailySummary struct {
	Date             string         `json:"date"`
	TraderID         string         `json:"trader_id"`
	CycleCount       int            `json:"cycle_count"`
	OpenCount        int            `json:"open_count"`
	OpenRejected     int            `json:"open_rejected"`
	SignalCount      int            `json:"signal_count"`
	WaitReasonHist   map[string]int `json:"wait_reason_histogram"`
	SuppressionFinal map[string]any `json:"suppressions_final,omitempty"`
	AccountStartEnd  [2]float64     `json:"account_start_end_balance"`
	LoosenModeEnters int            `json:"loosen_mode_enters"`
}

func WriteDailySummary(traderID string, date time.Time, logDir string) error {
	if logDir == "" {
		logDir = "decision_logs"
	}
	dateKey := date.Format("20060102")
	files, err := filepath.Glob(filepath.Join(logDir, fmt.Sprintf("decision_%s_*.json", dateKey)))
	if err != nil {
		return err
	}
	summary := DailySummary{
		Date:           date.Format("2006-01-02"),
		TraderID:       traderID,
		WaitReasonHist: map[string]int{},
	}
	var sawBalance bool
	lastLoosen := false
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		if traderID != "" && !recordMatchesTrader(&record, traderID) {
			continue
		}
		summary.CycleCount++
		if record.WaitReasonSummary != "" {
			summary.WaitReasonHist[record.WaitReasonSummary]++
		}
		if !sawBalance {
			summary.AccountStartEnd[0] = record.AccountState.TotalBalance
			sawBalance = true
		}
		summary.AccountStartEnd[1] = record.AccountState.TotalBalance
		signalCount, ok := strategySignalCount(record.StrategyDiagnostics)
		if !ok {
			signalCount, ok = strategyPerCandidateCount(record.StrategyDiagnostics)
		}
		if !ok {
			signalCount = len(record.CandidateDetails)
		}
		summary.SignalCount += signalCount
		for _, action := range record.Decisions {
			if isOpenAction(action.Action) && action.Success {
				summary.OpenCount++
			}
			if action.Action == "open_rejected" {
				summary.OpenRejected++
			}
		}
		if record.RiskState != nil {
			activeLoosen := record.RiskState.ActiveMode == "loosen"
			if activeLoosen && !lastLoosen {
				summary.LoosenModeEnters++
			}
			lastLoosen = activeLoosen
			if record.RiskState.Suppressions != nil {
				summary.SuppressionFinal = record.RiskState.Suppressions
			}
		}
	}
	if summary.CycleCount == 0 {
		return nil
	}
	outPath := filepath.Join(logDir, fmt.Sprintf("daily_summary_%s.json", dateKey))
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0644)
}

func strategySignalCount(diagnostics map[string]any) (int, bool) {
	if len(diagnostics) == 0 {
		return 0, false
	}
	value, ok := diagnostics["signal_count"]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case int32:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	default:
		return 0, false
	}
}

func strategyPerCandidateCount(diagnostics map[string]any) (int, bool) {
	if len(diagnostics) == 0 {
		return 0, false
	}
	switch values := diagnostics["per_candidate"].(type) {
	case []any:
		return len(values), true
	case []map[string]any:
		return len(values), true
	default:
		return 0, false
	}
}
