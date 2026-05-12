package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReplayReport 是离线复盘输出。
type ReplayReport struct {
	GeneratedAt                   time.Time                   `json:"generated_at"`
	RecordCount                   int                         `json:"record_count"`
	OpenAttempts                  int                         `json:"open_attempts"`
	RejectedOpenCount             int                         `json:"rejected_open_count"`
	RejectionReasons              map[string]int              `json:"rejection_reasons"`
	TotalRiskUSD                  float64                     `json:"total_risk_usd"`
	MaxCycleRiskUSD               float64                     `json:"max_cycle_risk_usd"`
	TheoreticalPnL                float64                     `json:"theoretical_pn_l"`
	ExecutionFailureRate          float64                     `json:"execution_failure_rate"`
	UnmatchedCount                int                         `json:"unmatched_count"`
	TradeCount                    int                         `json:"trade_count"`
	Rolling                       *RollingPerformanceSnapshot `json:"rolling,omitempty"`
	Execution                     ExecutionQualityStats       `json:"execution_quality"`
	ReportOnly                    bool                        `json:"report_only"`
	DryRun                        bool                        `json:"dry_run"`
	ReportOnlySimulationCount     int                         `json:"report_only_simulation_count,omitempty"`
	ReportOnlySimulationScenarios map[string]int              `json:"report_only_simulation_scenarios,omitempty"`
	ReportOnlySimulationSources   map[string]int              `json:"report_only_simulation_sources,omitempty"`
	ReportOnlySimulationSymbols   map[string]int              `json:"report_only_simulation_symbols,omitempty"`
	Notes                         []string                    `json:"notes,omitempty"`
	RecentOpenRejectionText       []string                    `json:"recent_open_rejection_text,omitempty"`
}

// ReplayFilter 定义离线 replay 的样本过滤条件。
type ReplayFilter struct {
	IncludeBackups bool
	TraderID       string
	From           time.Time
	To             time.Time
}

// LoadDecisionRecordsRecursive 递归读取指定目录下的 decision_*.json 日志。
func LoadDecisionRecordsRecursive(logDir string) ([]*DecisionRecord, error) {
	var records []*DecisionRecord
	err := filepath.WalkDir(logDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "decision_") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil
		}
		record.SourcePath = path
		records = append(records, &record)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取replay日志失败: %w", err)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	return records, nil
}

func FilterReplayRecords(records []*DecisionRecord, filter ReplayFilter) []*DecisionRecord {
	var filtered []*DecisionRecord
	for _, record := range records {
		if record == nil {
			continue
		}
		if !filter.IncludeBackups && isBackupReplayPath(record.SourcePath) {
			continue
		}
		if filter.TraderID != "" && !recordMatchesTrader(record, filter.TraderID) {
			continue
		}
		if !filter.From.IsZero() && record.Timestamp.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && record.Timestamp.After(filter.To) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func isBackupReplayPath(path string) bool {
	if path == "" {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		lower := strings.ToLower(part)
		if strings.Contains(lower, ".bak") || strings.Contains(lower, "backup") {
			return true
		}
	}
	return false
}

func recordMatchesTrader(record *DecisionRecord, traderID string) bool {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		return true
	}
	if record.RiskState != nil && record.RiskState.TraderID == traderID {
		return true
	}
	source := filepath.ToSlash(record.SourcePath)
	for _, part := range strings.Split(source, "/") {
		if part == traderID {
			return true
		}
	}
	return false
}

// BuildReplayReport 根据决策日志生成离线复盘报告。
func BuildReplayReport(records []*DecisionRecord, reportOnly bool, dryRun bool) ReplayReport {
	outcomes, unmatched := BuildTradeOutcomes(records)
	report := ReplayReport{
		GeneratedAt:                   time.Now(),
		RecordCount:                   len(records),
		RejectionReasons:              make(map[string]int),
		ReportOnlySimulationScenarios: make(map[string]int),
		ReportOnlySimulationSources:   make(map[string]int),
		ReportOnlySimulationSymbols:   make(map[string]int),
		UnmatchedCount:                len(unmatched),
		TradeCount:                    len(outcomes),
		Rolling:                       BuildRollingPerformance(outcomes, time.Now()),
		Execution:                     BuildExecutionQuality(records, len(unmatched)),
		ReportOnly:                    reportOnly,
		DryRun:                        dryRun,
	}

	var failedActions int
	for _, outcome := range outcomes {
		report.TheoreticalPnL += outcome.PnL
	}
	for _, record := range records {
		var cycleRisk float64
		for _, action := range record.Decisions {
			if isOpenAction(action.Action) {
				report.OpenAttempts++
				cycleRisk += action.RiskUSD
				report.TotalRiskUSD += action.RiskUSD
			}
			if action.Action == "open_rejected" || (isOpenAction(action.Action) && isOpenRejection(action)) {
				report.RejectedOpenCount++
				reason := actionFailureReason(action)
				if reason == "" {
					reason = "unknown"
				}
				report.RejectionReasons[reason]++
				report.RecentOpenRejectionText = appendLimitedString(report.RecentOpenRejectionText, reason, 10)
				if reportOnly {
					addReplaySimulations(&report, action)
				}
			}
			if action.Action != "" && !action.Success {
				failedActions++
			}
		}
		if cycleRisk > report.MaxCycleRiskUSD {
			report.MaxCycleRiskUSD = cycleRisk
		}
	}
	if report.Execution.TotalActions > 0 {
		report.ExecutionFailureRate = float64(failedActions) / float64(report.Execution.TotalActions) * 100
	}
	if len(records) == 0 {
		report.Notes = append(report.Notes, "未发现决策日志，报告仅包含空基线")
	}
	if reportOnly && report.ReportOnlySimulationCount > 0 {
		report.Notes = append(report.Notes, "report-only包含结构化模拟和/或旧日志文本推断；source=text_inferred不等同于实盘模拟")
	}
	return report
}

func addReplaySimulations(report *ReplayReport, action DecisionAction) {
	if report == nil {
		return
	}
	if len(action.Simulations) > 0 {
		for _, sim := range action.Simulations {
			source := sim.Source
			if source == "" {
				source = "structured"
			}
			report.ReportOnlySimulationCount++
			report.ReportOnlySimulationScenarios[sim.Scenario]++
			report.ReportOnlySimulationSources[source]++
			if action.Symbol != "" {
				report.ReportOnlySimulationSymbols[action.Symbol]++
			}
		}
		return
	}
	if sim, ok := inferOpenFrequencySimulationFromText(actionFailureReason(action)); ok {
		report.ReportOnlySimulationCount++
		report.ReportOnlySimulationScenarios[sim.Scenario]++
		report.ReportOnlySimulationSources[sim.Source]++
		if action.Symbol != "" {
			report.ReportOnlySimulationSymbols[action.Symbol]++
		}
	}
}

func inferOpenFrequencySimulationFromText(reason string) (OpenFrequencySimulationSnapshot, bool) {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(reason, "ADX"):
		return OpenFrequencySimulationSnapshot{Scenario: "high_adx_active_candidate", Source: "text_inferred", Reason: reason}, true
	case strings.Contains(reason, "风险回报比") || strings.Contains(lower, "rr"):
		return OpenFrequencySimulationSnapshot{Scenario: "rr_threshold_candidate", Source: "text_inferred", Reason: reason}, true
	case strings.Contains(lower, "rolling") || strings.Contains(reason, "历史滚动") || strings.Contains(reason, "最近"):
		return OpenFrequencySimulationSnapshot{Scenario: "rolling_risk_only_candidate", Source: "text_inferred", Reason: reason}, true
	default:
		return OpenFrequencySimulationSnapshot{}, false
	}
}
