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
	RejectionBuckets              map[string]int              `json:"rejection_buckets"`
	RRRejectionRate               float64                     `json:"rr_rejection_rate"`
	TotalRiskUSD                  float64                     `json:"total_risk_usd"`
	MaxCycleRiskUSD               float64                     `json:"max_cycle_risk_usd"`
	TheoreticalPnL                float64                     `json:"theoretical_pn_l"`
	FirstBalance                  float64                     `json:"first_balance,omitempty"`
	LastBalance                   float64                     `json:"last_balance,omitempty"`
	BalanceDelta                  float64                     `json:"balance_delta,omitempty"`
	ExecutionFailureRate          float64                     `json:"execution_failure_rate"`
	UnmatchedCount                int                         `json:"unmatched_count"`
	TradeCount                    int                         `json:"trade_count"`
	RawCloseActions               int                         `json:"raw_close_actions"`
	DeduplicatedCloseActions      int                         `json:"deduplicated_close_actions"`
	DuplicateCloseCount           int                         `json:"duplicate_close_count"`
	DuplicateCloseGroups          map[string]int              `json:"duplicate_close_groups,omitempty"`
	DuplicateCloseCandidates      []DuplicateCloseCandidate   `json:"duplicate_close_candidates,omitempty"`
	ExchangeReconciliation        *ExchangeReconciliation     `json:"exchange_reconciliation,omitempty"`
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

// DuplicateCloseCandidate 表示同一 symbol/side 生命周期内多次 close 的候选污染。
type DuplicateCloseCandidate struct {
	TraderID           string    `json:"trader_id,omitempty"`
	Symbol             string    `json:"symbol"`
	Side               string    `json:"side"`
	Lifecycle          int       `json:"lifecycle"`
	FirstAction        string    `json:"first_action"`
	FirstCloseTime     time.Time `json:"first_close_time"`
	DuplicateAction    string    `json:"duplicate_action"`
	DuplicateCloseTime time.Time `json:"duplicate_close_time"`
	DuplicateSource    string    `json:"duplicate_source,omitempty"`
	Reason             string    `json:"reason,omitempty"`
}

// ExchangeCloseSnapshot 是从交易所订单/成交历史导出的只读平仓快照。
type ExchangeCloseSnapshot struct {
	TraderID    string    `json:"trader_id,omitempty"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`
	OrderID     int64     `json:"order_id,omitempty"`
	CloseTime   time.Time `json:"close_time"`
	RealizedPnL float64   `json:"realized_pnl,omitempty"`
	Source      string    `json:"source,omitempty"`
}

// ExchangeReconciliation 汇总本地日志 close 与交易所只读快照的对账结果。
type ExchangeReconciliation struct {
	Status                 string                  `json:"status"`
	LogCloseCount          int                     `json:"log_close_count"`
	ExchangeCloseCount     int                     `json:"exchange_close_count"`
	MatchedCloseCount      int                     `json:"matched_close_count"`
	MissingInLogsCount     int                     `json:"missing_in_logs_count"`
	MissingInExchangeCount int                     `json:"missing_in_exchange_count"`
	MissingInLogs          []ExchangeCloseSnapshot `json:"missing_in_logs,omitempty"`
	MissingInExchange      []ExchangeCloseSnapshot `json:"missing_in_exchange,omitempty"`
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
		RejectionBuckets:              make(map[string]int),
		ReportOnlySimulationScenarios: make(map[string]int),
		ReportOnlySimulationSources:   make(map[string]int),
		ReportOnlySimulationSymbols:   make(map[string]int),
		UnmatchedCount:                len(unmatched),
		TradeCount:                    len(outcomes),
		DuplicateCloseGroups:          make(map[string]int),
		Rolling:                       BuildRollingPerformance(outcomes, time.Now()),
		Execution:                     BuildExecutionQuality(records, len(unmatched)),
		ReportOnly:                    reportOnly,
		DryRun:                        dryRun,
	}

	var failedActions int
	for _, outcome := range outcomes {
		report.TheoreticalPnL += outcome.PnL
	}
	report.FirstBalance, report.LastBalance, report.BalanceDelta = extractBalanceDelta(records)
	closeSummary := buildCloseDedupeSummary(records)
	report.RawCloseActions = closeSummary.rawCloseActions
	report.DeduplicatedCloseActions = closeSummary.deduplicatedCloseActions
	report.DuplicateCloseCount = len(closeSummary.duplicates)
	report.DuplicateCloseCandidates = closeSummary.duplicates
	report.DuplicateCloseGroups = closeSummary.groups

	for _, record := range records {
		var cycleRisk float64
		for _, action := range record.Decisions {
			wasOpenRejection := false
			if isOpenAction(action.Action) {
				report.OpenAttempts++
				cycleRisk += action.RiskUSD
				report.TotalRiskUSD += action.RiskUSD
			}
			if action.Action == "open_rejected" || (isOpenAction(action.Action) && isOpenRejection(action)) {
				wasOpenRejection = true
				report.RejectedOpenCount++
				reason := actionFailureReason(action)
				if reason == "" {
					reason = "unknown"
				}
				report.RejectionReasons[reason]++
				report.RejectionBuckets[classifyRejectionBucket(reason, action)]++
				report.RecentOpenRejectionText = appendLimitedString(report.RecentOpenRejectionText, reason, 10)
				if reportOnly {
					addReplaySimulations(&report, action)
				}
			}
			if action.Action != "" && !action.Success {
				failedActions++
				if !wasOpenRejection {
					report.RejectionBuckets[classifyRejectionBucket(actionFailureReason(action), action)]++
				}
			}
		}
		if cycleRisk > report.MaxCycleRiskUSD {
			report.MaxCycleRiskUSD = cycleRisk
		}
	}
	if report.Execution.TotalActions > 0 {
		report.ExecutionFailureRate = float64(failedActions) / float64(report.Execution.TotalActions) * 100
	}
	if report.RejectedOpenCount > 0 {
		report.RRRejectionRate = float64(report.RejectionBuckets["rr"]) / float64(report.RejectedOpenCount) * 100
	}
	if len(records) == 0 {
		report.Notes = append(report.Notes, "未发现决策日志，报告仅包含空基线")
	}
	if reportOnly && report.ReportOnlySimulationCount > 0 {
		report.Notes = append(report.Notes, "report-only包含结构化模拟和/或旧日志文本推断；source=text_inferred不等同于实盘模拟")
	}
	return report
}

type closeDedupeSummary struct {
	rawCloseActions          int
	deduplicatedCloseActions int
	duplicates               []DuplicateCloseCandidate
	groups                   map[string]int
}

type replayCloseState struct {
	lifecycle int
	closed    bool
	action    string
	closedAt  time.Time
}

func buildCloseDedupeSummary(records []*DecisionRecord) closeDedupeSummary {
	sortedRecords := sortedReplayRecords(records)
	stateByKey := make(map[string]replayCloseState)
	summary := closeDedupeSummary{groups: make(map[string]int)}

	for _, record := range sortedRecords {
		traderID := replayRecordTraderID(record)
		for _, action := range record.Decisions {
			if !action.Success {
				continue
			}
			side, ok := actionSide(action.Action)
			if !ok || action.Symbol == "" {
				continue
			}
			key := replayLifecycleKey(traderID, action.Symbol, side)
			actionTime := replayActionTime(record, action)
			state := stateByKey[key]

			if isOpenAction(action.Action) {
				state.lifecycle++
				state.closed = false
				state.action = ""
				state.closedAt = time.Time{}
				stateByKey[key] = state
				continue
			}
			if !isCloseAction(action.Action) {
				continue
			}

			summary.rawCloseActions++
			if state.closed {
				candidate := DuplicateCloseCandidate{
					TraderID:           traderID,
					Symbol:             action.Symbol,
					Side:               side,
					Lifecycle:          state.lifecycle,
					FirstAction:        state.action,
					FirstCloseTime:     state.closedAt,
					DuplicateAction:    action.Action,
					DuplicateCloseTime: actionTime,
					DuplicateSource:    action.CloseSource,
					Reason:             actionFailureReason(action),
				}
				summary.duplicates = append(summary.duplicates, candidate)
				summary.groups[action.Symbol+"_"+side]++
				continue
			}

			summary.deduplicatedCloseActions++
			state.closed = true
			state.action = action.Action
			state.closedAt = actionTime
			stateByKey[key] = state
		}
	}
	return summary
}

func extractBalanceDelta(records []*DecisionRecord) (float64, float64, float64) {
	sortedRecords := sortedReplayRecords(records)
	var first, last float64
	for _, record := range sortedRecords {
		if record == nil || record.AccountState.TotalBalance <= 0 {
			continue
		}
		if first == 0 {
			first = record.AccountState.TotalBalance
		}
		last = record.AccountState.TotalBalance
	}
	if first == 0 || last == 0 {
		return first, last, 0
	}
	return first, last, last - first
}

func classifyRejectionBucket(reason string, action DecisionAction) string {
	text := strings.ToLower(reason + " " + action.Error + " " + action.HighRiskReason)
	switch {
	case strings.Contains(text, "rr") || strings.Contains(text, "风险回报") || strings.Contains(text, "reward"):
		return "rr"
	case strings.Contains(text, "btc") || strings.Contains(text, "高beta") || strings.Contains(text, "high beta"):
		return "btc_gate"
	case strings.Contains(text, "置信") || strings.Contains(text, "confidence"):
		return "confidence"
	case strings.Contains(text, "失效") || strings.Contains(text, "invalidation"):
		return "invalidation"
	case strings.Contains(text, "执行") || strings.Contains(text, "下单") || strings.Contains(text, "order") || strings.Contains(text, "protect"):
		return "execution_error"
	case strings.Contains(text, "风险预算") || strings.Contains(text, "risk budget"):
		return "risk_budget"
	case strings.Contains(text, "频率") || strings.Contains(text, "daily_open") || strings.Contains(text, "rolling"):
		return "frequency"
	default:
		return "other"
	}
}

func sortedReplayRecords(records []*DecisionRecord) []*DecisionRecord {
	sortedRecords := append([]*DecisionRecord(nil), records...)
	sort.SliceStable(sortedRecords, func(i, j int) bool {
		if sortedRecords[i] == nil {
			return false
		}
		if sortedRecords[j] == nil {
			return true
		}
		return sortedRecords[i].Timestamp.Before(sortedRecords[j].Timestamp)
	})
	return sortedRecords
}

func replayRecordTraderID(record *DecisionRecord) string {
	if record == nil {
		return ""
	}
	if record.RiskState != nil && record.RiskState.TraderID != "" {
		return record.RiskState.TraderID
	}
	source := filepath.ToSlash(record.SourcePath)
	parts := strings.Split(source, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "decision_") && i > 0 {
			return parts[i-1]
		}
	}
	return ""
}

func replayLifecycleKey(traderID, symbol, side string) string {
	return traderID + ":" + symbol + ":" + side
}

func replayActionTime(record *DecisionRecord, action DecisionAction) time.Time {
	if !action.Timestamp.IsZero() {
		return action.Timestamp
	}
	if record != nil {
		return record.Timestamp
	}
	return time.Time{}
}

func isCloseAction(action string) bool {
	return action == "close_long" || action == "close_short" ||
		action == "auto_close_long" || action == "auto_close_short"
}

// AttachExchangeCloseSnapshots 将交易所只读 close 快照附加到 replay 报告。
func AttachExchangeCloseSnapshots(report *ReplayReport, records []*DecisionRecord, exchangeCloses []ExchangeCloseSnapshot) {
	if report == nil {
		return
	}
	logCloses := buildLogCloseSnapshots(records)
	reconciliation := ExchangeReconciliation{
		Status:             "ok",
		LogCloseCount:      len(logCloses),
		ExchangeCloseCount: len(exchangeCloses),
	}

	matchedExchange := make(map[int]bool)
	for _, logClose := range logCloses {
		matchIndex := -1
		for i, exchangeClose := range exchangeCloses {
			if matchedExchange[i] || !closeSnapshotsMatch(logClose, exchangeClose) {
				continue
			}
			matchIndex = i
			break
		}
		if matchIndex >= 0 {
			matchedExchange[matchIndex] = true
			reconciliation.MatchedCloseCount++
			continue
		}
		reconciliation.MissingInExchangeCount++
		reconciliation.MissingInExchange = appendLimitedExchangeSnapshot(reconciliation.MissingInExchange, logClose, 20)
	}
	for i, exchangeClose := range exchangeCloses {
		if matchedExchange[i] {
			continue
		}
		reconciliation.MissingInLogsCount++
		reconciliation.MissingInLogs = appendLimitedExchangeSnapshot(reconciliation.MissingInLogs, exchangeClose, 20)
	}
	report.ExchangeReconciliation = &reconciliation
}

func buildLogCloseSnapshots(records []*DecisionRecord) []ExchangeCloseSnapshot {
	var snapshots []ExchangeCloseSnapshot
	for _, record := range sortedReplayRecords(records) {
		traderID := replayRecordTraderID(record)
		for _, action := range record.Decisions {
			if !action.Success || !isCloseAction(action.Action) {
				continue
			}
			side, ok := actionSide(action.Action)
			if !ok {
				continue
			}
			snapshots = append(snapshots, ExchangeCloseSnapshot{
				TraderID:  traderID,
				Symbol:    action.Symbol,
				Side:      side,
				OrderID:   action.OrderID,
				CloseTime: replayActionTime(record, action),
				Source:    action.CloseSource,
			})
		}
	}
	return snapshots
}

func closeSnapshotsMatch(a, b ExchangeCloseSnapshot) bool {
	if a.Symbol != b.Symbol || a.Side != b.Side {
		return false
	}
	if a.TraderID != "" && b.TraderID != "" && a.TraderID != b.TraderID {
		return false
	}
	if a.OrderID > 0 && b.OrderID > 0 {
		return a.OrderID == b.OrderID
	}
	if a.CloseTime.IsZero() || b.CloseTime.IsZero() {
		return true
	}
	diff := a.CloseTime.Sub(b.CloseTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 10*time.Minute
}

func appendLimitedExchangeSnapshot(items []ExchangeCloseSnapshot, item ExchangeCloseSnapshot, limit int) []ExchangeCloseSnapshot {
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
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
