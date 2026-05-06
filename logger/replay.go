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
	GeneratedAt             time.Time                   `json:"generated_at"`
	RecordCount             int                         `json:"record_count"`
	OpenAttempts            int                         `json:"open_attempts"`
	RejectedOpenCount       int                         `json:"rejected_open_count"`
	RejectionReasons        map[string]int              `json:"rejection_reasons"`
	TotalRiskUSD            float64                     `json:"total_risk_usd"`
	MaxCycleRiskUSD         float64                     `json:"max_cycle_risk_usd"`
	TheoreticalPnL          float64                     `json:"theoretical_pn_l"`
	ExecutionFailureRate    float64                     `json:"execution_failure_rate"`
	UnmatchedCount          int                         `json:"unmatched_count"`
	TradeCount              int                         `json:"trade_count"`
	Rolling                 *RollingPerformanceSnapshot `json:"rolling,omitempty"`
	Execution               ExecutionQualityStats       `json:"execution_quality"`
	ReportOnly              bool                        `json:"report_only"`
	DryRun                  bool                        `json:"dry_run"`
	Notes                   []string                    `json:"notes,omitempty"`
	RecentOpenRejectionText []string                    `json:"recent_open_rejection_text,omitempty"`
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

// BuildReplayReport 根据决策日志生成离线复盘报告。
func BuildReplayReport(records []*DecisionRecord, reportOnly bool, dryRun bool) ReplayReport {
	outcomes, unmatched := BuildTradeOutcomes(records)
	report := ReplayReport{
		GeneratedAt:      time.Now(),
		RecordCount:      len(records),
		RejectionReasons: make(map[string]int),
		UnmatchedCount:   len(unmatched),
		TradeCount:       len(outcomes),
		Rolling:          BuildRollingPerformance(outcomes, time.Now()),
		Execution:        BuildExecutionQuality(records, len(unmatched)),
		ReportOnly:       reportOnly,
		DryRun:           dryRun,
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
	return report
}
