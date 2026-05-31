package logger

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	AccountSemantics              *AccountSemanticsSummary    `json:"account_semantics,omitempty"`
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
	TradeEventStats               *TradeEventStats            `json:"trade_event_stats,omitempty"`
	Execution                     ExecutionQualityStats       `json:"execution_quality"`
	StrategyDisease               StrategyDiseaseReport       `json:"strategy_disease"`
	ReportOnly                    bool                        `json:"report_only"`
	DryRun                        bool                        `json:"dry_run"`
	ReportOnlySimulationCount     int                         `json:"report_only_simulation_count,omitempty"`
	ReportOnlySimulationScenarios map[string]int              `json:"report_only_simulation_scenarios,omitempty"`
	ReportOnlySimulationSources   map[string]int              `json:"report_only_simulation_sources,omitempty"`
	ReportOnlySimulationSymbols   map[string]int              `json:"report_only_simulation_symbols,omitempty"`
	Notes                         []string                    `json:"notes,omitempty"`
	RecentOpenRejectionText       []string                    `json:"recent_open_rejection_text,omitempty"`
}

// AccountSemanticsSummary 汇总回放日志中的资金基准和策略分配语义。
type AccountSemanticsSummary struct {
	FirstStrategyBaseline     float64 `json:"first_strategy_baseline,omitempty"`
	LastStrategyBaseline      float64 `json:"last_strategy_baseline,omitempty"`
	BaselineSource            string  `json:"baseline_source,omitempty"`
	EquitySource              string  `json:"equity_source,omitempty"`
	AllocationEnabled         bool    `json:"allocation_enabled,omitempty"`
	AllocatedBalance          float64 `json:"allocated_balance,omitempty"`
	AllocatedAvailableBalance float64 `json:"allocated_available_balance,omitempty"`
	AllocatedUsedMargin       float64 `json:"allocated_used_margin,omitempty"`
	SizingEquity              float64 `json:"sizing_equity,omitempty"`
	SizingAvailableBalance    float64 `json:"sizing_available_balance,omitempty"`
	SizingEquitySource        string  `json:"sizing_equity_source,omitempty"`
	RiskDenominator           float64 `json:"risk_denominator,omitempty"`
	RiskDenominatorSource     string  `json:"risk_denominator_source,omitempty"`
}

// OpenRejectionDailyReport 汇总开仓被拒和接近放行的诊断。
type OpenRejectionDailyReport struct {
	GeneratedAt              time.Time                    `json:"generated_at"`
	PeriodStart              time.Time                    `json:"period_start,omitempty"`
	PeriodEnd                time.Time                    `json:"period_end,omitempty"`
	RecordCount              int                          `json:"record_count"`
	RejectedOpenCount        int                          `json:"rejected_open_count"`
	DiagnosticCount          int                          `json:"diagnostic_count"`
	NoSuccessfulOpenHours    float64                      `json:"no_successful_open_hours,omitempty"`
	VersionDiagnosticMissing bool                         `json:"version_diagnostic_missing,omitempty"`
	TopNoOpenBuckets         []OpenRejectionBucketSummary `json:"top_no_open_buckets,omitempty"`
	FreshnessCompatibility   *FreshnessCompatibilityAudit `json:"freshness_compatibility,omitempty"`
	ByReason                 map[string]int               `json:"by_reason"`
	BySymbol                 map[string]int               `json:"by_symbol"`
	ByBucket                 map[string]int               `json:"by_bucket"`
	NearMisses               []OpenRejectionNearMiss      `json:"near_misses,omitempty"`
	RecentExamples           []OpenRejectionEvent         `json:"recent_examples,omitempty"`
	ChanlunV2NoOpen          *ChanlunV2NoOpenReport       `json:"chanlun_v2_no_open,omitempty"`
	Notes                    []string                     `json:"notes,omitempty"`
}

// OpenRejectionDailyOptions 控制只读开仓拒绝日报的附加审计。
type OpenRejectionDailyOptions struct {
	SignalTypeMinRR map[string]float64
	ConfigSource    string
}

// OpenRejectionBucketSummary 是 no-open bucket 的排序摘要。
type OpenRejectionBucketSummary struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

// FreshnessCompatibilityAudit 识别旧 freshness RR 阈值拒绝、但 signal-type 阈值可通过的样本。
type FreshnessCompatibilityAudit struct {
	ConfigSource               string                         `json:"config_source,omitempty"`
	ConfigProvided             bool                           `json:"config_provided"`
	CheckedCount               int                            `json:"checked_count"`
	WouldPassSignalTypeRR      int                            `json:"would_pass_signal_type_rr_count"`
	BySymbol                   map[string]int                 `json:"by_symbol,omitempty"`
	BySignalType               map[string]int                 `json:"by_signal_type,omitempty"`
	Samples                    []FreshnessCompatibilitySample `json:"samples,omitempty"`
	UsedDefaultSignalTypeMinRR bool                           `json:"used_default_signal_type_min_rr,omitempty"`
	Notes                      []string                       `json:"notes,omitempty"`
}

// FreshnessCompatibilitySample 是 freshness RR 兼容审计样本。
type FreshnessCompatibilitySample struct {
	Timestamp           time.Time `json:"timestamp"`
	SourceFile          string    `json:"source_file,omitempty"`
	CycleNumber         int       `json:"cycle_number,omitempty"`
	Symbol              string    `json:"symbol,omitempty"`
	Action              string    `json:"action,omitempty"`
	SignalType          string    `json:"signal_type,omitempty"`
	RemainingNetRR      float64   `json:"remaining_net_rr"`
	OldThreshold        float64   `json:"old_threshold,omitempty"`
	SignalTypeThreshold float64   `json:"signal_type_threshold"`
	Reason              string    `json:"reason,omitempty"`
}

// OpenRejectionEvent 是日报里的单条拒绝/阻塞样本。
type OpenRejectionEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	CycleNumber int       `json:"cycle_number,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	ReasonCode  string    `json:"reason_code"`
	Bucket      string    `json:"bucket"`
	Reason      string    `json:"reason"`
	Source      string    `json:"source"`
}

// OpenRejectionNearMiss 表示只差少量 RR 或 chase 阈值的候选。
type OpenRejectionNearMiss struct {
	Timestamp   time.Time `json:"timestamp"`
	CycleNumber int       `json:"cycle_number,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	ReasonCode  string    `json:"reason_code"`
	Metric      string    `json:"metric"`
	Value       float64   `json:"value"`
	Threshold   float64   `json:"threshold"`
	Gap         float64   `json:"gap"`
	Reason      string    `json:"reason"`
	Source      string    `json:"source"`
}

// ChanlunV2NoOpenReport 汇总 Chanlun V2 长时间未开仓的可观测路径。
type ChanlunV2NoOpenReport struct {
	ActionDistribution         map[string]int                      `json:"action_distribution,omitempty"`
	FinalActionDistribution    map[string]int                      `json:"final_action_distribution,omitempty"`
	TradeIntentDistribution    map[string]int                      `json:"trade_intent_distribution,omitempty"`
	DirectTerminalCount        int                                 `json:"direct_terminal_count,omitempty"`
	DirectTerminalByReason     map[string]int                      `json:"direct_terminal_by_reason,omitempty"`
	SuppressedTerminalCount    int                                 `json:"suppressed_terminal_count,omitempty"`
	SuppressedTerminalByReason map[string]int                      `json:"suppressed_terminal_by_reason,omitempty"`
	WaitingForTriggerCount     int                                 `json:"waiting_for_trigger_count,omitempty"`
	TriggerReadyCount          int                                 `json:"trigger_ready_count,omitempty"`
	TriggerReadyBySymbol       map[string]int                      `json:"trigger_ready_by_symbol,omitempty"`
	OpenGateRejectionCount     int                                 `json:"open_gate_rejection_count,omitempty"`
	OpenGateByReason           map[string]int                      `json:"open_gate_by_reason,omitempty"`
	RRDirectTerminalCount      int                                 `json:"rr_direct_terminal_count,omitempty"`
	RRDirectBySymbol           map[string]int                      `json:"rr_direct_by_symbol,omitempty"`
	RRDirectBySignalType       map[string]int                      `json:"rr_direct_by_signal_type,omitempty"`
	RRDirectByThreshold        map[string]int                      `json:"rr_direct_by_threshold,omitempty"`
	RRDirectSamples            []ChanlunV2RRDirectTerminalSample   `json:"rr_direct_samples,omitempty"`
	BTCGateRejectionCount      int                                 `json:"btc_gate_rejection_count,omitempty"`
	BTCGateDiagnostics         []ChanlunV2BTCGateDiagnosticSample  `json:"btc_gate_diagnostics,omitempty"`
	ConfidenceOverrideCount    int                                 `json:"confidence_override_count,omitempty"`
	ConfidenceOverrides        []ChanlunV2ConfidenceOverrideSample `json:"confidence_overrides,omitempty"`
	Samples                    []ChanlunV2NoOpenSample             `json:"samples,omitempty"`
}

// ChanlunV2RRDirectTerminalSample 是 RR 直接终态的结构化样本。
type ChanlunV2RRDirectTerminalSample struct {
	Timestamp   time.Time `json:"timestamp"`
	SourceFile  string    `json:"source_file,omitempty"`
	CycleNumber int       `json:"cycle_number,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	SignalType  string    `json:"signal_type,omitempty"`
	RemainingRR float64   `json:"remaining_net_rr,omitempty"`
	Threshold   float64   `json:"threshold,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// ChanlunV2BTCGateDiagnosticSample 是 BTC hard veto 的展开样本。
type ChanlunV2BTCGateDiagnosticSample struct {
	Timestamp       time.Time      `json:"timestamp"`
	SourceFile      string         `json:"source_file,omitempty"`
	CycleNumber     int            `json:"cycle_number,omitempty"`
	Symbol          string         `json:"symbol,omitempty"`
	Action          string         `json:"action,omitempty"`
	ReasonCode      string         `json:"reason_code,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	BTC             map[string]any `json:"btc,omitempty"`
	GateDiagnostics map[string]any `json:"gate_diagnostics,omitempty"`
}

// ChanlunV2ConfidenceOverrideSample 是 open gate 置信度 override 生效样本。
type ChanlunV2ConfidenceOverrideSample struct {
	Timestamp   time.Time      `json:"timestamp"`
	SourceFile  string         `json:"source_file,omitempty"`
	CycleNumber int            `json:"cycle_number,omitempty"`
	Symbol      string         `json:"symbol,omitempty"`
	Action      string         `json:"action,omitempty"`
	Rule        string         `json:"rule,omitempty"`
	From        float64        `json:"from,omitempty"`
	To          float64        `json:"to,omitempty"`
	Actual      float64        `json:"actual_confidence,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

// ChanlunV2NoOpenSample 是 no-open 报告里的最近样本索引。
type ChanlunV2NoOpenSample struct {
	Timestamp   time.Time `json:"timestamp"`
	SourceFile  string    `json:"source_file,omitempty"`
	CycleNumber int       `json:"cycle_number,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	Category    string    `json:"category"`
	ReasonCode  string    `json:"reason_code,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// StrategyDiseaseReport 汇总策略病因诊断。
type StrategyDiseaseReport struct {
	MicroStopCount               int                              `json:"micro_stop_count"`
	MicroTPCount                 int                              `json:"micro_tp_count"`
	LowADXEntryCount             int                              `json:"low_adx_entry_count"`
	CounterDIEntryCount          int                              `json:"counter_di_entry_count"`
	SameSideCorrelationCount     int                              `json:"same_side_correlation_count"`
	ProfileMismatchCount         int                              `json:"profile_mismatch_count"`
	PrematureFullTPCount         int                              `json:"premature_full_tp_count"`
	RewrittenStopCount           int                              `json:"rewritten_stop_count"`
	RewrittenTPCount             int                              `json:"rewritten_tp_count"`
	RewrittenExchangeFullTPCount int                              `json:"rewritten_exchange_full_tp_count"`
	RejectedByADXCount           int                              `json:"rejected_by_adx_count"`
	RejectedByProfileCount       int                              `json:"rejected_by_profile_count"`
	RejectedByCorrelationCount   int                              `json:"rejected_by_correlation_count"`
	RMultipleStats               RMultipleStats                   `json:"r_multiple_stats"`
	ByProfile                    map[string]*StrategyDiseaseGroup `json:"by_profile,omitempty"`
	BySymbol                     map[string]*StrategyDiseaseGroup `json:"by_symbol,omitempty"`
	BySide                       map[string]*StrategyDiseaseGroup `json:"by_side,omitempty"`
	ByCloseReason                map[string]*StrategyDiseaseGroup `json:"by_close_reason,omitempty"`
}

// StrategyDiseaseGroup 是 profile/symbol/side/close reason 维度的病因聚合。
type StrategyDiseaseGroup struct {
	OpenCount                    int            `json:"open_count,omitempty"`
	TradeCount                   int            `json:"trade_count,omitempty"`
	MicroStopCount               int            `json:"micro_stop_count,omitempty"`
	MicroTPCount                 int            `json:"micro_tp_count,omitempty"`
	LowADXEntryCount             int            `json:"low_adx_entry_count,omitempty"`
	CounterDIEntryCount          int            `json:"counter_di_entry_count,omitempty"`
	SameSideCorrelationCount     int            `json:"same_side_correlation_count,omitempty"`
	ProfileMismatchCount         int            `json:"profile_mismatch_count,omitempty"`
	PrematureFullTPCount         int            `json:"premature_full_tp_count,omitempty"`
	RewrittenStopCount           int            `json:"rewritten_stop_count,omitempty"`
	RewrittenTPCount             int            `json:"rewritten_tp_count,omitempty"`
	RewrittenExchangeFullTPCount int            `json:"rewritten_exchange_full_tp_count,omitempty"`
	RMultipleStats               RMultipleStats `json:"r_multiple_stats,omitempty"`
}

// RMultipleStats 汇总以初始风险距离为单位的收益倍数。
type RMultipleStats struct {
	Count        int     `json:"count"`
	Average      float64 `json:"average,omitempty"`
	Median       float64 `json:"median,omitempty"`
	Min          float64 `json:"min,omitempty"`
	Max          float64 `json:"max,omitempty"`
	WinningCount int     `json:"winning_count,omitempty"`
	LosingCount  int     `json:"losing_count,omitempty"`
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
	replay := BuildTradeReplay(records)
	outcomes := replay.FullOutcomes
	unmatched := replay.Unmatched
	eventStats := BuildTradeEventStats(replay.Events)
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
		TradeEventStats:               &eventStats,
		Execution:                     BuildExecutionQuality(records, len(unmatched)),
		ReportOnly:                    reportOnly,
		DryRun:                        dryRun,
	}

	var failedActions int
	for _, outcome := range outcomes {
		report.TheoreticalPnL += outcome.PnL
	}
	report.StrategyDisease = BuildStrategyDiseaseReport(records)
	report.FirstBalance, report.LastBalance, report.BalanceDelta = extractBalanceDelta(records)
	report.AccountSemantics = extractAccountSemantics(records)
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

var (
	rrNearMissPattern               = regexp.MustCompile(`([A-Z0-9]+USDT).*剩余净RR\s*([0-9]+(?:\.[0-9]+)?)低于阈值([0-9]+(?:\.[0-9]+)?)`)
	chaseNearMissPattern            = regexp.MustCompile(`([A-Z0-9]+USDT).*入场追价比例([0-9]+(?:\.[0-9]+)?)超过阈值([0-9]+(?:\.[0-9]+)?)`)
	suppressedPattern               = regexp.MustCompile(`已因([a-zA-Z0-9_]+)抑制`)
	v2RRDirectTerminalPattern       = regexp.MustCompile(`([A-Z0-9]+USDT)\s+([a-zA-Z0-9_]+).*父结构终止:.*剩余净RR\s*([0-9]+(?:\.[0-9]+)?)低于阈值([0-9]+(?:\.[0-9]+)?)`)
	v2TerminalSuppressedCodePattern = regexp.MustCompile(`(?:终态信号已静默|重复过期信号已静默):\s*([a-zA-Z0-9_.]+)`)
)

// BuildOpenRejectionDailyReport 基于已过滤记录生成只读开仓拒绝日报。
func BuildOpenRejectionDailyReport(records []*DecisionRecord, maxNearMisses int) OpenRejectionDailyReport {
	return BuildOpenRejectionDailyReportWithOptions(records, maxNearMisses, OpenRejectionDailyOptions{})
}

func BuildOpenRejectionDailyReportWithOptions(records []*DecisionRecord, maxNearMisses int, opts OpenRejectionDailyOptions) OpenRejectionDailyReport {
	if maxNearMisses <= 0 {
		maxNearMisses = 20
	}
	report := OpenRejectionDailyReport{
		GeneratedAt: time.Now(),
		RecordCount: len(records),
		ByReason:    make(map[string]int),
		BySymbol:    make(map[string]int),
		ByBucket:    make(map[string]int),
	}
	if len(records) == 0 {
		report.Notes = append(report.Notes, "未发现决策日志，日报为空")
		return report
	}
	report.ChanlunV2NoOpen = newChanlunV2NoOpenReport()
	report.PeriodStart = records[0].Timestamp
	report.PeriodEnd = records[len(records)-1].Timestamp
	for _, record := range records {
		if record == nil {
			continue
		}
		addChanlunV2ActionDistributions(report.ChanlunV2NoOpen, record)
		for _, action := range record.Decisions {
			if action.Action != "open_rejected" && !(isOpenAction(action.Action) && isOpenRejection(action)) {
				continue
			}
			report.RejectedOpenCount++
			reason := actionFailureReason(action)
			if reason == "" {
				reason = "unknown"
			}
			reasonCode := openRejectionReasonCode(reason, action)
			event := OpenRejectionEvent{
				Timestamp:   record.Timestamp,
				CycleNumber: record.CycleNumber,
				Symbol:      marketSymbolOrAction(action.Symbol, reason),
				ReasonCode:  reasonCode,
				Bucket:      classifyRejectionBucket(reason, action),
				Reason:      reason,
				Source:      "decision_action",
			}
			addOpenRejectionEvent(&report, event)
			addChanlunV2OpenGateRejection(report.ChanlunV2NoOpen, record, action, reasonCode, reason)
			if !appendNearMissFromText(&report, record, event.Symbol, reasonCode, reason, event.Source) {
				appendNearMissFromAction(&report, record, action, reasonCode, reason)
			}
		}
		for _, message := range strategyDiagnosticMessages(record) {
			addChanlunV2DiagnosticMessage(report.ChanlunV2NoOpen, record, message)
			reasonCode := diagnosticRejectionReasonCode(message)
			if reasonCode == "" {
				continue
			}
			report.DiagnosticCount++
			event := OpenRejectionEvent{
				Timestamp:   record.Timestamp,
				CycleNumber: record.CycleNumber,
				Symbol:      marketSymbolOrAction("", message),
				ReasonCode:  reasonCode,
				Bucket:      classifyRejectionBucket(reasonCode+" "+message, DecisionAction{}),
				Reason:      message,
				Source:      "strategy_diagnostic",
			}
			addOpenRejectionEvent(&report, event)
			appendNearMissFromText(&report, record, event.Symbol, reasonCode, message, event.Source)
		}
	}
	finalizeOpenRejectionDailyReport(&report, records, opts)
	sort.SliceStable(report.NearMisses, func(i, j int) bool {
		if report.NearMisses[i].Gap == report.NearMisses[j].Gap {
			return report.NearMisses[i].Timestamp.After(report.NearMisses[j].Timestamp)
		}
		return report.NearMisses[i].Gap < report.NearMisses[j].Gap
	})
	if len(report.NearMisses) > maxNearMisses {
		report.NearMisses = report.NearMisses[:maxNearMisses]
	}
	if report.RejectedOpenCount == 0 && report.DiagnosticCount == 0 {
		report.Notes = append(report.Notes, "未发现开仓拒绝或策略阻塞诊断")
	}
	return report
}

func finalizeOpenRejectionDailyReport(report *OpenRejectionDailyReport, records []*DecisionRecord, opts OpenRejectionDailyOptions) {
	if report == nil || len(records) == 0 {
		return
	}
	report.NoSuccessfulOpenHours = noSuccessfulOpenHours(records, report.PeriodStart, report.PeriodEnd)
	report.VersionDiagnosticMissing = chanlunV2VersionDiagnosticMissing(records)
	report.TopNoOpenBuckets = topOpenRejectionBuckets(report.ByBucket, 5)
	audit := buildFreshnessCompatibilityAudit(records, opts)
	if audit != nil {
		report.FreshnessCompatibility = audit
		if len(audit.Notes) > 0 {
			report.Notes = append(report.Notes, audit.Notes...)
		}
	}
	if report.VersionDiagnosticMissing {
		report.Notes = append(report.Notes, "version_diagnostic_missing=true: 部分缠论V2日志缺少active_mode/effective_entry_timing，建议确认运行进程已重启到当前HEAD")
	}
}

func noSuccessfulOpenHours(records []*DecisionRecord, periodStart, periodEnd time.Time) float64 {
	if periodEnd.IsZero() || periodStart.IsZero() || periodEnd.Before(periodStart) {
		return 0
	}
	var lastOpen time.Time
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, action := range record.Decisions {
			finalAction := strings.TrimSpace(action.FinalAction)
			if finalAction == "" {
				finalAction = action.Action
			}
			if action.Success && isOpenAction(finalAction) {
				t := replayActionTime(record, action)
				if t.After(lastOpen) {
					lastOpen = t
				}
			}
		}
	}
	if lastOpen.IsZero() {
		return periodEnd.Sub(periodStart).Hours()
	}
	if periodEnd.Before(lastOpen) {
		return 0
	}
	return periodEnd.Sub(lastOpen).Hours()
}

func chanlunV2VersionDiagnosticMissing(records []*DecisionRecord) bool {
	for _, record := range records {
		if !isChanlunV2Record(record) {
			continue
		}
		if len(record.StrategyDiagnostics) == 0 {
			return true
		}
		if _, ok := record.StrategyDiagnostics["active_mode"]; !ok {
			return true
		}
		if _, ok := record.StrategyDiagnostics["effective_entry_timing"]; !ok {
			return true
		}
	}
	return false
}

func isChanlunV2Record(record *DecisionRecord) bool {
	if record == nil {
		return false
	}
	if strings.EqualFold(record.DecisionMode, "chanlun_v2") || strings.EqualFold(record.StrategyName, "chanlun_v2") {
		return true
	}
	for _, action := range record.Decisions {
		if strings.EqualFold(action.StrategyName, "chanlun_v2") || strings.EqualFold(action.StrategyMode, "chanlun_v2") {
			return true
		}
		if strings.HasPrefix(strings.TrimSpace(action.SignalID), "chanlun_v2") {
			return true
		}
	}
	return false
}

func topOpenRejectionBuckets(values map[string]int, limit int) []OpenRejectionBucketSummary {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	out := make([]OpenRejectionBucketSummary, 0, len(values))
	for bucket, count := range values {
		if count <= 0 {
			continue
		}
		out = append(out, OpenRejectionBucketSummary{Bucket: bucket, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Bucket < out[j].Bucket
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func buildFreshnessCompatibilityAudit(records []*DecisionRecord, opts OpenRejectionDailyOptions) *FreshnessCompatibilityAudit {
	thresholds, usedDefault := normalizeFreshnessSignalTypeMinRR(opts.SignalTypeMinRR)
	audit := &FreshnessCompatibilityAudit{
		ConfigSource:               strings.TrimSpace(opts.ConfigSource),
		ConfigProvided:             strings.TrimSpace(opts.ConfigSource) != "",
		BySymbol:                   map[string]int{},
		BySignalType:               map[string]int{},
		UsedDefaultSignalTypeMinRR: usedDefault,
	}
	if usedDefault {
		audit.Notes = append(audit.Notes, "未提供replay配置或配置缺少signal_type_min_rr，兼容审计使用内置默认信号类型RR阈值")
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, action := range record.Decisions {
			reason := actionFailureReason(action)
			if reason == "" {
				reason = action.Error
			}
			if openRejectionReasonCode(reason, action) != "freshness_gate.rr_invalid" {
				continue
			}
			signalType := replayActionSignalType(action)
			if signalType == "" {
				continue
			}
			threshold, ok := thresholds[signalType]
			if !ok || threshold <= 0 {
				continue
			}
			remaining, ok := replayActionFloat(action, "remaining_net_rr")
			if !ok {
				continue
			}
			oldThreshold, _ := replayActionFloat(action, "min_remaining_net_rr")
			audit.CheckedCount++
			if remaining+1e-9 < threshold {
				continue
			}
			audit.WouldPassSignalTypeRR++
			symbol := marketSymbolOrAction(action.Symbol, reason)
			audit.BySymbol[firstNonEmpty(symbol, "UNKNOWN")]++
			audit.BySignalType[signalType]++
			audit.Samples = appendLimitedFreshnessCompatibilitySamples(audit.Samples, FreshnessCompatibilitySample{
				Timestamp:           replayActionTime(record, action),
				SourceFile:          record.SourcePath,
				CycleNumber:         record.CycleNumber,
				Symbol:              symbol,
				Action:              action.TradeIntent,
				SignalType:          signalType,
				RemainingNetRR:      remaining,
				OldThreshold:        oldThreshold,
				SignalTypeThreshold: threshold,
				Reason:              reason,
			}, 20)
		}
	}
	if audit.CheckedCount == 0 && audit.ConfigSource == "" {
		return nil
	}
	return audit
}

func normalizeFreshnessSignalTypeMinRR(values map[string]float64) (map[string]float64, bool) {
	if len(values) == 0 {
		return defaultFreshnessSignalTypeMinRR(), true
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || value <= 0 {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return defaultFreshnessSignalTypeMinRR(), true
	}
	return out, false
}

func defaultFreshnessSignalTypeMinRR() map[string]float64 {
	return map[string]float64{
		"buy1":  2.0,
		"sell1": 2.0,
		"buy2":  1.5,
		"sell2": 1.5,
		"buy3":  1.2,
		"sell3": 1.2,
	}
}

func replayActionSignalType(action DecisionAction) string {
	if value := strings.ToLower(strings.TrimSpace(action.SignalType)); value != "" {
		return value
	}
	for _, key := range []string{"parent_signal_type", "signal_type"} {
		if value, ok := metadataString(action.StrategyMetadata, key); ok {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}

func replayActionFloat(action DecisionAction, key string) (float64, bool) {
	if value, ok := metadataFloat(action.GateDiagnostics, key); ok {
		return value, true
	}
	if value, ok := metadataFloat(action.StrategyMetadata, key); ok {
		return value, true
	}
	return 0, false
}

func appendLimitedFreshnessCompatibilitySamples(values []FreshnessCompatibilitySample, sample FreshnessCompatibilitySample, max int) []FreshnessCompatibilitySample {
	if max <= 0 {
		return values
	}
	values = append(values, sample)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func addOpenRejectionEvent(report *OpenRejectionDailyReport, event OpenRejectionEvent) {
	if report == nil {
		return
	}
	if event.ReasonCode == "" {
		event.ReasonCode = "unknown"
	}
	if event.Bucket == "" {
		event.Bucket = "other"
	}
	if event.Symbol == "" {
		event.Symbol = "UNKNOWN"
	}
	report.ByReason[event.ReasonCode]++
	report.BySymbol[event.Symbol]++
	report.ByBucket[event.Bucket]++
	report.RecentExamples = appendLimitedEvent(report.RecentExamples, event, 20)
}

func newChanlunV2NoOpenReport() *ChanlunV2NoOpenReport {
	return &ChanlunV2NoOpenReport{
		ActionDistribution:         make(map[string]int),
		FinalActionDistribution:    make(map[string]int),
		TradeIntentDistribution:    make(map[string]int),
		DirectTerminalByReason:     make(map[string]int),
		SuppressedTerminalByReason: make(map[string]int),
		TriggerReadyBySymbol:       make(map[string]int),
		OpenGateByReason:           make(map[string]int),
		RRDirectBySymbol:           make(map[string]int),
		RRDirectBySignalType:       make(map[string]int),
		RRDirectByThreshold:        make(map[string]int),
	}
}

func addChanlunV2ActionDistributions(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord) {
	if noOpen == nil || record == nil {
		return
	}
	for _, action := range record.Decisions {
		if value := strings.TrimSpace(action.Action); value != "" {
			noOpen.ActionDistribution[value]++
		}
		if value := strings.TrimSpace(action.FinalAction); value != "" {
			noOpen.FinalActionDistribution[value]++
		}
		tradeIntent := strings.TrimSpace(action.TradeIntent)
		if tradeIntent == "" {
			tradeIntent, _ = metadataString(action.StrategyMetadata, "trade_intent")
		}
		if tradeIntent != "" {
			noOpen.TradeIntentDistribution[tradeIntent]++
		}
	}
}

func addChanlunV2OpenGateRejection(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord, action DecisionAction, reasonCode, reason string) {
	if noOpen == nil || record == nil {
		return
	}
	noOpen.OpenGateRejectionCount++
	if reasonCode == "" {
		reasonCode = "unknown"
	}
	noOpen.OpenGateByReason[reasonCode]++
	symbol := marketSymbolOrAction(action.Symbol, reason)
	appendChanlunV2NoOpenSample(noOpen, record, symbol, "open_gate_rejection", reasonCode, reason)
	addChanlunV2ConfidenceOverride(noOpen, record, action, symbol)
	if !chanlunV2ActionHasBTCGate(action, reasonCode, reason) {
		return
	}
	noOpen.BTCGateRejectionCount++
	sample := ChanlunV2BTCGateDiagnosticSample{
		Timestamp:       record.Timestamp,
		SourceFile:      record.SourcePath,
		CycleNumber:     record.CycleNumber,
		Symbol:          symbol,
		Action:          action.Action,
		ReasonCode:      reasonCode,
		Reason:          reason,
		GateDiagnostics: action.GateDiagnostics,
	}
	if btc, ok := action.GateDiagnostics["btc"].(map[string]any); ok {
		sample.BTC = btc
	}
	noOpen.BTCGateDiagnostics = appendLimitedBTCGateSamples(noOpen.BTCGateDiagnostics, sample, 20)
	appendChanlunV2NoOpenSample(noOpen, record, symbol, "btc_gate_rejection", reasonCode, reason)
}

func addChanlunV2ConfidenceOverride(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord, action DecisionAction, symbol string) {
	if noOpen == nil || record == nil || len(action.GateDiagnostics) == 0 {
		return
	}
	raw, ok := action.GateDiagnostics["min_confidence_override_applied"]
	if !ok || raw == nil {
		return
	}
	diagnostics, ok := raw.(map[string]any)
	if !ok {
		return
	}
	noOpen.ConfidenceOverrideCount++
	rule, _ := metadataString(diagnostics, "rule")
	from, _ := metadataFloat(diagnostics, "from")
	to, _ := metadataFloat(diagnostics, "to")
	actual, _ := metadataFloat(diagnostics, "actual_confidence")
	noOpen.ConfidenceOverrides = appendLimitedConfidenceOverrideSamples(noOpen.ConfidenceOverrides, ChanlunV2ConfidenceOverrideSample{
		Timestamp:   record.Timestamp,
		SourceFile:  record.SourcePath,
		CycleNumber: record.CycleNumber,
		Symbol:      symbol,
		Action:      action.Action,
		Rule:        rule,
		From:        from,
		To:          to,
		Actual:      actual,
		Diagnostics: diagnostics,
	}, 20)
}

func addChanlunV2DiagnosticMessage(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord, message string) {
	if noOpen == nil || record == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	reasonCode := chanlunV2NoOpenReasonCode(message)
	symbol := marketSymbolOrAction("", message)
	switch {
	case strings.Contains(message, "fresh entry trigger ready"):
		noOpen.TriggerReadyCount++
		noOpen.TriggerReadyBySymbol[firstNonEmpty(symbol, "UNKNOWN")]++
		appendChanlunV2NoOpenSample(noOpen, record, symbol, "trigger_ready", reasonCode, message)
	case strings.Contains(message, "等待") && strings.Contains(message, "fresh entry trigger"):
		noOpen.WaitingForTriggerCount++
		appendChanlunV2NoOpenSample(noOpen, record, symbol, "waiting_for_fresh_entry_trigger", firstNonEmpty(reasonCode, "waiting_for_fresh_entry_trigger"), message)
	case isChanlunV2SuppressedTerminalMessage(message):
		noOpen.SuppressedTerminalCount++
		noOpen.SuppressedTerminalByReason[firstNonEmpty(reasonCode, "terminal_suppressed")]++
		appendChanlunV2NoOpenSample(noOpen, record, symbol, "suppressed_terminal", reasonCode, message)
	case isChanlunV2DirectTerminalMessage(message):
		noOpen.DirectTerminalCount++
		noOpen.DirectTerminalByReason[firstNonEmpty(reasonCode, "terminal")]++
		appendChanlunV2NoOpenSample(noOpen, record, symbol, "direct_terminal", reasonCode, message)
		addChanlunV2RRDirectTerminal(noOpen, record, message)
	}
}

func addChanlunV2RRDirectTerminal(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord, message string) {
	if noOpen == nil || record == nil {
		return
	}
	matches := v2RRDirectTerminalPattern.FindStringSubmatch(message)
	if len(matches) != 5 {
		return
	}
	remainingRR, rrOK := parseReplayFloat(matches[3])
	threshold, thresholdOK := parseReplayFloat(matches[4])
	if !rrOK || !thresholdOK {
		return
	}
	symbol := marketSymbolOrAction(matches[1], message)
	signalType := strings.ToLower(strings.TrimSpace(matches[2]))
	noOpen.RRDirectTerminalCount++
	noOpen.RRDirectBySymbol[firstNonEmpty(symbol, "UNKNOWN")]++
	noOpen.RRDirectBySignalType[firstNonEmpty(signalType, "unknown")]++
	noOpen.RRDirectByThreshold[fmt.Sprintf("%s@%.2f", firstNonEmpty(signalType, "unknown"), threshold)]++
	noOpen.RRDirectSamples = appendLimitedRRDirectSamples(noOpen.RRDirectSamples, ChanlunV2RRDirectTerminalSample{
		Timestamp:   record.Timestamp,
		SourceFile:  record.SourcePath,
		CycleNumber: record.CycleNumber,
		Symbol:      symbol,
		SignalType:  signalType,
		RemainingRR: remainingRR,
		Threshold:   threshold,
		Reason:      message,
	}, 20)
}

func chanlunV2NoOpenReasonCode(message string) string {
	if matches := v2TerminalSuppressedCodePattern.FindStringSubmatch(message); len(matches) == 2 {
		return matches[1]
	}
	if matches := suppressedPattern.FindStringSubmatch(message); len(matches) == 2 {
		return matches[1]
	}
	switch {
	case strings.Contains(message, "剩余净RR"):
		return "entry_rr_invalid"
	case strings.Contains(message, "观察窗口过期"):
		return "entry_parent.watch_window_expired"
	case strings.Contains(message, "当前价") && strings.Contains(message, "已穿越目标"):
		return "entry_parent.target_crossed"
	case strings.Contains(message, "止损/止盈结构无效"):
		return "entry_parent.invalid_structure"
	case strings.Contains(message, "trigger_skipped: gate_blocked"):
		return "gate_blocked"
	case strings.Contains(message, "fresh entry trigger ready"):
		return "entry_trigger_ready"
	}
	return diagnosticRejectionReasonCode(message)
}

func isChanlunV2DirectTerminalMessage(message string) bool {
	if strings.Contains(message, "父结构终止:") || strings.Contains(message, "父结构观察窗口过期") || strings.Contains(message, "trigger_skipped: gate_blocked") {
		return true
	}
	return false
}

func isChanlunV2SuppressedTerminalMessage(message string) bool {
	return strings.Contains(message, "终态信号已静默") || strings.Contains(message, "重复过期信号已静默") || suppressedPattern.MatchString(message)
}

func chanlunV2ActionHasBTCGate(action DecisionAction, reasonCode, reason string) bool {
	if strings.EqualFold(reasonCode, "btc") || strings.Contains(reason, "BTC 1h/4h") || strings.Contains(reason, "高 beta 山寨多单") {
		return true
	}
	for _, gateReason := range action.GateReasons {
		if strings.EqualFold(strings.TrimSpace(gateReason), "btc") {
			return true
		}
	}
	if len(action.GateDiagnostics) == 0 {
		return false
	}
	if _, ok := action.GateDiagnostics["btc"]; ok {
		return true
	}
	if code, ok := metadataString(action.GateDiagnostics, "reason_code"); ok && strings.EqualFold(code, "btc") {
		return true
	}
	return false
}

func appendChanlunV2NoOpenSample(noOpen *ChanlunV2NoOpenReport, record *DecisionRecord, symbol, category, reasonCode, reason string) {
	if noOpen == nil || record == nil {
		return
	}
	noOpen.Samples = appendLimitedNoOpenSamples(noOpen.Samples, ChanlunV2NoOpenSample{
		Timestamp:   record.Timestamp,
		SourceFile:  record.SourcePath,
		CycleNumber: record.CycleNumber,
		Symbol:      marketSymbolOrAction(symbol, reason),
		Category:    category,
		ReasonCode:  reasonCode,
		Reason:      reason,
	}, 30)
}

func appendLimitedRRDirectSamples(values []ChanlunV2RRDirectTerminalSample, sample ChanlunV2RRDirectTerminalSample, max int) []ChanlunV2RRDirectTerminalSample {
	if max <= 0 {
		return values
	}
	values = append(values, sample)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func appendLimitedBTCGateSamples(values []ChanlunV2BTCGateDiagnosticSample, sample ChanlunV2BTCGateDiagnosticSample, max int) []ChanlunV2BTCGateDiagnosticSample {
	if max <= 0 {
		return values
	}
	values = append(values, sample)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func appendLimitedConfidenceOverrideSamples(values []ChanlunV2ConfidenceOverrideSample, sample ChanlunV2ConfidenceOverrideSample, max int) []ChanlunV2ConfidenceOverrideSample {
	if max <= 0 {
		return values
	}
	values = append(values, sample)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func appendLimitedNoOpenSamples(values []ChanlunV2NoOpenSample, sample ChanlunV2NoOpenSample, max int) []ChanlunV2NoOpenSample {
	if max <= 0 {
		return values
	}
	values = append(values, sample)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func appendLimitedEvent(values []OpenRejectionEvent, event OpenRejectionEvent, max int) []OpenRejectionEvent {
	if max <= 0 {
		return values
	}
	values = append(values, event)
	if len(values) > max {
		return values[len(values)-max:]
	}
	return values
}

func strategyDiagnosticMessages(record *DecisionRecord) []string {
	if record == nil || len(record.StrategyDiagnostics) == 0 {
		return nil
	}
	raw, ok := record.StrategyDiagnostics["messages"]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func diagnosticRejectionReasonCode(message string) string {
	switch {
	case strings.Contains(message, "入场追价比例"):
		return "entry_chase_ratio_too_high"
	case strings.Contains(message, "剩余净RR"):
		return "remaining_net_rr_too_low"
	case strings.Contains(message, "direct_structure_open关闭") || strings.Contains(message, "作为结构背景保留"):
		return "structure_background_only"
	case strings.Contains(message, "止损/止盈结构不合法") || strings.Contains(message, "结构不合法"):
		return "invalid_stop_take_profit_structure"
	case strings.Contains(message, "越过止盈目标") || strings.Contains(message, "target_already_crossed"):
		return "target_already_crossed"
	case strings.Contains(message, "signal_expired") || strings.Contains(message, "信号已过期"):
		return "signal_expired"
	}
	if matches := suppressedPattern.FindStringSubmatch(message); len(matches) == 2 {
		return matches[1]
	}
	return ""
}

func openRejectionReasonCode(reason string, action DecisionAction) string {
	if len(action.GateReasons) > 0 && strings.TrimSpace(action.GateReasons[0]) != "" {
		return strings.TrimSpace(action.GateReasons[0])
	}
	if action.GateDiagnostics != nil {
		if code, ok := metadataString(action.GateDiagnostics, "reason_code"); ok && code != "" {
			return code
		}
	}
	if code := diagnosticRejectionReasonCode(reason); code != "" {
		return code
	}
	return classifyRejectionBucket(reason, action)
}

func marketSymbolOrAction(symbol, text string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol != "" {
		return symbol
	}
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, "：:，,;；()[]{}")
		if strings.HasSuffix(field, "USDT") {
			return field
		}
	}
	return ""
}

func appendNearMissFromText(report *OpenRejectionDailyReport, record *DecisionRecord, symbol, reasonCode, reason, source string) bool {
	if report == nil || record == nil || reason == "" {
		return false
	}
	if matches := rrNearMissPattern.FindStringSubmatch(reason); len(matches) == 4 {
		value, valueOK := parseReplayFloat(matches[2])
		threshold, thresholdOK := parseReplayFloat(matches[3])
		if valueOK && thresholdOK {
			appendNearMiss(report, record, firstNonEmpty(symbol, matches[1]), reasonCode, "remaining_net_rr", value, threshold, threshold-value, reason, source)
			return true
		}
		return false
	}
	if matches := chaseNearMissPattern.FindStringSubmatch(reason); len(matches) == 4 {
		value, valueOK := parseReplayFloat(matches[2])
		threshold, thresholdOK := parseReplayFloat(matches[3])
		if valueOK && thresholdOK {
			appendNearMiss(report, record, firstNonEmpty(symbol, matches[1]), reasonCode, "entry_chase_ratio", value, threshold, value-threshold, reason, source)
			return true
		}
	}
	return false
}

func appendNearMissFromAction(report *OpenRejectionDailyReport, record *DecisionRecord, action DecisionAction, reasonCode, reason string) {
	if report == nil || record == nil {
		return
	}
	remaining, hasRemaining := metadataFloat(action.GateDiagnostics, "remaining_net_rr")
	if !hasRemaining {
		remaining, hasRemaining = metadataFloat(action.StrategyMetadata, "remaining_net_rr")
	}
	threshold, hasThreshold := metadataFloat(action.StrategyMetadata, "min_remaining_net_rr")
	if hasRemaining && hasThreshold {
		appendNearMiss(report, record, action.Symbol, reasonCode, "remaining_net_rr", remaining, threshold, threshold-remaining, reason, "decision_action")
	}
}

func appendNearMiss(report *OpenRejectionDailyReport, record *DecisionRecord, symbol, reasonCode, metric string, value, threshold, gap float64, reason, source string) {
	if report == nil || record == nil {
		return
	}
	if gap < 0 {
		gap = -gap
	}
	report.NearMisses = append(report.NearMisses, OpenRejectionNearMiss{
		Timestamp:   record.Timestamp,
		CycleNumber: record.CycleNumber,
		Symbol:      marketSymbolOrAction(symbol, reason),
		ReasonCode:  reasonCode,
		Metric:      metric,
		Value:       value,
		Threshold:   threshold,
		Gap:         gap,
		Reason:      reason,
		Source:      source,
	})
}

func parseReplayFloat(value string) (float64, bool) {
	out, err := strconv.ParseFloat(value, 64)
	return out, err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

const (
	replayMicroStopRatio = 0.01
	replayMicroTPRatio   = 0.02
	replayMinTPRR        = 2.0
)

type diseaseOpenTrace struct {
	traderID            string
	symbol              string
	side                string
	profile             string
	openPrice           float64
	openTime            time.Time
	requestedStopLoss   float64
	effectiveStopLoss   float64
	requestedTakeProfit float64
	effectiveTakeProfit float64
	exchangeFullTP      float64
	exchangeFullTPMode  string
	requestedStopRatio  float64
	stopRatio           float64
	requestedTPRatio    float64
	effectiveTPRatio    float64
	exchangeFullTPRatio float64
	takeProfitRatio     float64
	initialRiskDistance float64
	gateDiagnostics     map[string]any
	reasonText          string
	riskNormalization   interface{}
}

type diseaseScanState struct {
	rValues        []float64
	byProfileR     map[string][]float64
	bySymbolR      map[string][]float64
	bySideR        map[string][]float64
	byCloseReasonR map[string][]float64
}

// BuildStrategyDiseaseReport 从决策动作中复盘微止损、微止盈、方向过滤、相关性叠加和R倍数。
func BuildStrategyDiseaseReport(records []*DecisionRecord) StrategyDiseaseReport {
	report := newStrategyDiseaseReport()
	state := diseaseScanState{
		byProfileR:     make(map[string][]float64),
		bySymbolR:      make(map[string][]float64),
		bySideR:        make(map[string][]float64),
		byCloseReasonR: make(map[string][]float64),
	}
	active := make(map[string][]diseaseOpenTrace)

	for _, record := range sortedReplayRecords(records) {
		if record == nil {
			continue
		}
		traderID := replayRecordTraderID(record)
		for _, action := range record.Decisions {
			side, ok := actionSide(action.Action)
			if action.Action == "open_rejected" || (isOpenAction(action.Action) && isOpenRejection(action)) {
				addRejectedDisease(&report, action)
				continue
			}
			if !ok || action.Symbol == "" || !action.Success {
				continue
			}

			key := replayLifecycleKey(traderID, action.Symbol, side)
			if isOpenAction(action.Action) {
				trace := buildDiseaseOpenTrace(record, traderID, side, action)
				active[key] = append(active[key], trace)
				addOpenDisease(&report, trace)
				continue
			}
			if !isCloseAction(action.Action) {
				continue
			}

			opens := active[key]
			if len(opens) == 0 {
				continue
			}
			closeReason := closeReasonBucket(action)
			for _, trace := range opens {
				addClosedDisease(&report, &state, trace, action, closeReason)
			}
			delete(active, key)
		}
	}

	report.RMultipleStats = buildRMultipleStats(state.rValues)
	attachGroupRStats(report.ByProfile, state.byProfileR)
	attachGroupRStats(report.BySymbol, state.bySymbolR)
	attachGroupRStats(report.BySide, state.bySideR)
	attachGroupRStats(report.ByCloseReason, state.byCloseReasonR)
	return report
}

func newStrategyDiseaseReport() StrategyDiseaseReport {
	return StrategyDiseaseReport{
		ByProfile:     make(map[string]*StrategyDiseaseGroup),
		BySymbol:      make(map[string]*StrategyDiseaseGroup),
		BySide:        make(map[string]*StrategyDiseaseGroup),
		ByCloseReason: make(map[string]*StrategyDiseaseGroup),
	}
}

func buildDiseaseOpenTrace(record *DecisionRecord, traderID, side string, action DecisionAction) diseaseOpenTrace {
	entry := action.Price
	requestedStopRatio := ratioFromPriceDistance(entry, action.RequestedStopLoss)
	effectiveStopRatio := ratioFromPriceDistance(entry, action.EffectiveStopLoss)
	loggedStopRatio := normalizeLoggedDistanceRatio(action.StopDistanceRatio, action.StopDistancePercent)
	if loggedStopRatio == 0 {
		loggedStopRatio = normalizeLoggedDistanceRatio(action.StopDistancePct, 0)
	}
	stopRatio := firstPositive(effectiveStopRatio, loggedStopRatio, requestedStopRatio)

	requestedTPRatio := ratioFromPriceDistance(entry, action.RequestedTakeProfit)
	effectiveTPRatio := ratioFromPriceDistance(entry, action.EffectiveTakeProfit)
	exchangeFullTPRatio := ratioFromPriceDistance(entry, action.ExchangeFullTakeProfit)
	loggedTPRatio := normalizeLoggedDistanceRatio(action.TakeProfitRatio, action.TakeProfitPercent)
	takeProfitRatio := firstPositive(exchangeFullTPRatio, loggedTPRatio, effectiveTPRatio, requestedTPRatio)

	riskDistance := 0.0
	switch {
	case entry > 0 && action.EffectiveStopLoss > 0:
		riskDistance = math.Abs(entry - action.EffectiveStopLoss)
	case entry > 0 && action.RequestedStopLoss > 0:
		riskDistance = math.Abs(entry - action.RequestedStopLoss)
	case entry > 0 && stopRatio > 0:
		riskDistance = entry * stopRatio
	}

	actionTime := replayActionTime(record, action)
	return diseaseOpenTrace{
		traderID:            traderID,
		symbol:              action.Symbol,
		side:                side,
		profile:             strings.TrimSpace(action.ProfileName),
		openPrice:           entry,
		openTime:            actionTime,
		requestedStopLoss:   action.RequestedStopLoss,
		effectiveStopLoss:   action.EffectiveStopLoss,
		requestedTakeProfit: action.RequestedTakeProfit,
		effectiveTakeProfit: action.EffectiveTakeProfit,
		exchangeFullTP:      action.ExchangeFullTakeProfit,
		exchangeFullTPMode:  action.ExchangeFullTPMode,
		requestedStopRatio:  requestedStopRatio,
		stopRatio:           stopRatio,
		requestedTPRatio:    requestedTPRatio,
		effectiveTPRatio:    effectiveTPRatio,
		exchangeFullTPRatio: exchangeFullTPRatio,
		takeProfitRatio:     takeProfitRatio,
		initialRiskDistance: riskDistance,
		gateDiagnostics:     action.GateDiagnostics,
		reasonText:          actionFailureReason(action),
		riskNormalization:   action.RiskNormalization,
	}
}

func addOpenDisease(report *StrategyDiseaseReport, trace diseaseOpenTrace) {
	if report == nil {
		return
	}
	incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
		group.OpenCount++
	})

	if traceHasMicroStop(trace) {
		report.MicroStopCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.MicroStopCount++
		})
	}
	if traceHasMicroTP(trace) {
		report.MicroTPCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.MicroTPCount++
		})
	}
	if traceHasLowADX(trace) {
		report.LowADXEntryCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.LowADXEntryCount++
		})
	}
	if traceHasCounterDI(trace) {
		report.CounterDIEntryCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.CounterDIEntryCount++
		})
	}
	if traceHasSameSideCorrelation(trace) {
		report.SameSideCorrelationCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.SameSideCorrelationCount++
		})
	}
	if missingProfile(trace.profile) {
		report.ProfileMismatchCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.ProfileMismatchCount++
		})
	}
	if traceStopRewritten(trace) {
		report.RewrittenStopCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.RewrittenStopCount++
		})
	}
	if traceTPRewritten(trace) {
		report.RewrittenTPCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.RewrittenTPCount++
		})
	}
	if traceExchangeFullTPRewritten(trace) {
		report.RewrittenExchangeFullTPCount++
		incrementOpenGroups(report, trace, func(group *StrategyDiseaseGroup) {
			group.RewrittenExchangeFullTPCount++
		})
	}
}

func addRejectedDisease(report *StrategyDiseaseReport, action DecisionAction) {
	if report == nil {
		return
	}
	bucket := classifyRejectionBucket(actionFailureReason(action), action)
	switch bucket {
	case "adx", "counter_di":
		report.RejectedByADXCount++
	case "correlation":
		report.RejectedByCorrelationCount++
	case "profile":
		report.RejectedByProfileCount++
	}
}

func addClosedDisease(report *StrategyDiseaseReport, state *diseaseScanState, trace diseaseOpenTrace, closeAction DecisionAction, closeReason string) {
	if report == nil || state == nil {
		return
	}
	incrementTradeGroups(report, trace, closeReason, func(group *StrategyDiseaseGroup) {
		group.TradeCount++
	})
	addTraceDiseaseToCloseReasonGroup(report, trace, closeReason)
	if r, ok := rMultipleForClose(trace, closeAction); ok {
		state.rValues = append(state.rValues, r)
		profile := profileGroupKey(trace.profile)
		symbol := symbolGroupKey(trace.symbol)
		side := sideGroupKey(trace.side)
		state.byProfileR[profile] = append(state.byProfileR[profile], r)
		state.bySymbolR[symbol] = append(state.bySymbolR[symbol], r)
		state.bySideR[side] = append(state.bySideR[side], r)
		state.byCloseReasonR[closeReason] = append(state.byCloseReasonR[closeReason], r)
	}
	if isPrematureFullTP(trace, closeAction) {
		report.PrematureFullTPCount++
		incrementTradeGroups(report, trace, closeReason, func(group *StrategyDiseaseGroup) {
			group.PrematureFullTPCount++
		})
	}
}

func addTraceDiseaseToCloseReasonGroup(report *StrategyDiseaseReport, trace diseaseOpenTrace, closeReason string) {
	group := diseaseGroup(report.ByCloseReason, closeReason)
	if traceHasMicroStop(trace) {
		group.MicroStopCount++
	}
	if traceHasMicroTP(trace) {
		group.MicroTPCount++
	}
	if traceHasLowADX(trace) {
		group.LowADXEntryCount++
	}
	if traceHasCounterDI(trace) {
		group.CounterDIEntryCount++
	}
	if traceHasSameSideCorrelation(trace) {
		group.SameSideCorrelationCount++
	}
	if missingProfile(trace.profile) {
		group.ProfileMismatchCount++
	}
	if traceStopRewritten(trace) {
		group.RewrittenStopCount++
	}
	if traceTPRewritten(trace) {
		group.RewrittenTPCount++
	}
	if traceExchangeFullTPRewritten(trace) {
		group.RewrittenExchangeFullTPCount++
	}
}

func traceHasMicroStop(trace diseaseOpenTrace) bool {
	return (trace.requestedStopRatio > 0 && trace.requestedStopRatio < replayMicroStopRatio) ||
		(trace.stopRatio > 0 && trace.stopRatio < replayMicroStopRatio)
}

func traceHasMicroTP(trace diseaseOpenTrace) bool {
	tpRatio := firstPositive(trace.requestedTPRatio, trace.effectiveTPRatio, trace.takeProfitRatio)
	if tpRatio > 0 && tpRatio < replayMicroTPRatio {
		return true
	}
	stopRatio := firstPositive(trace.stopRatio, trace.requestedStopRatio)
	if tpRatio > 0 && stopRatio > 0 && tpRatio/stopRatio < replayMinTPRR {
		return true
	}
	return false
}

func traceHasLowADX(trace diseaseOpenTrace) bool {
	diag := nestedDiagnostics(trace.gateDiagnostics, "adx_regime")
	gate := strings.ToLower(stringFromAny(diag["gate"]))
	if gate == "low_adx" || gate == "missing" {
		return true
	}
	text := strings.ToLower(trace.reasonText)
	return strings.Contains(text, "low_adx") || strings.Contains(trace.reasonText, "ADX") && strings.Contains(trace.reasonText, "低于")
}

func traceHasCounterDI(trace diseaseOpenTrace) bool {
	diag := nestedDiagnostics(trace.gateDiagnostics, "adx_regime")
	gate := strings.ToLower(stringFromAny(diag["gate"]))
	if gate == "counter_di" {
		return true
	}
	text := strings.ToLower(trace.reasonText)
	return strings.Contains(text, "counter_di") || strings.Contains(trace.reasonText, "DI方向")
}

func traceHasSameSideCorrelation(trace diseaseOpenTrace) bool {
	if len(nestedDiagnostics(trace.gateDiagnostics, "same_side_exposure")) > 0 ||
		len(nestedDiagnostics(trace.gateDiagnostics, "correlation_concentration")) > 0 {
		return true
	}
	text := strings.ToLower(trace.reasonText)
	return strings.Contains(text, "same_side") || strings.Contains(text, "correlation") ||
		strings.Contains(trace.reasonText, "同向") || strings.Contains(trace.reasonText, "高相关")
}

func traceStopRewritten(trace diseaseOpenTrace) bool {
	if boolFromRiskNormalization(trace.riskNormalization, "rewritten_stop_loss") ||
		boolFromRiskNormalization(trace.riskNormalization, "rewritten_stop") {
		return true
	}
	return trace.requestedStopLoss > 0 && trace.effectiveStopLoss > 0 &&
		!pricesNear(trace.openPrice, trace.requestedStopLoss, trace.effectiveStopLoss)
}

func traceTPRewritten(trace diseaseOpenTrace) bool {
	if boolFromRiskNormalization(trace.riskNormalization, "rewritten_take_profit") {
		return true
	}
	return trace.requestedTakeProfit > 0 && trace.effectiveTakeProfit > 0 &&
		!pricesNear(trace.openPrice, trace.requestedTakeProfit, trace.effectiveTakeProfit)
}

func traceExchangeFullTPRewritten(trace diseaseOpenTrace) bool {
	if boolFromRiskNormalization(trace.riskNormalization, "rewritten_exchange_full_tp") {
		return true
	}
	return trace.requestedTakeProfit > 0 && trace.exchangeFullTP > 0 &&
		!pricesNear(trace.openPrice, trace.requestedTakeProfit, trace.exchangeFullTP)
}

func rMultipleForClose(trace diseaseOpenTrace, closeAction DecisionAction) (float64, bool) {
	if trace.openPrice <= 0 || closeAction.Price <= 0 || trace.initialRiskDistance <= 0 {
		return 0, false
	}
	if trace.side == "short" {
		return (trace.openPrice - closeAction.Price) / trace.initialRiskDistance, true
	}
	return (closeAction.Price - trace.openPrice) / trace.initialRiskDistance, true
}

func isPrematureFullTP(trace diseaseOpenTrace, closeAction DecisionAction) bool {
	if trace.exchangeFullTP <= 0 || closeAction.Price <= 0 || trace.openPrice <= 0 {
		return false
	}
	text := strings.ToLower(closeAction.CloseSource + " " + closeAction.Reasoning + " " + closeAction.Error)
	if !(strings.Contains(text, "tp") || strings.Contains(text, "take profit") || strings.Contains(text, "止盈")) {
		return false
	}
	if trace.side == "short" {
		return closeAction.Price > trace.exchangeFullTP && trace.exchangeFullTPRatio > trace.effectiveTPRatio
	}
	return closeAction.Price < trace.exchangeFullTP && trace.exchangeFullTPRatio > trace.effectiveTPRatio
}

func incrementOpenGroups(report *StrategyDiseaseReport, trace diseaseOpenTrace, fn func(*StrategyDiseaseGroup)) {
	if fn == nil {
		return
	}
	fn(diseaseGroup(report.ByProfile, profileGroupKey(trace.profile)))
	fn(diseaseGroup(report.BySymbol, symbolGroupKey(trace.symbol)))
	fn(diseaseGroup(report.BySide, sideGroupKey(trace.side)))
}

func incrementTradeGroups(report *StrategyDiseaseReport, trace diseaseOpenTrace, closeReason string, fn func(*StrategyDiseaseGroup)) {
	incrementOpenGroups(report, trace, fn)
	if fn != nil {
		fn(diseaseGroup(report.ByCloseReason, closeReason))
	}
}

func diseaseGroup(groups map[string]*StrategyDiseaseGroup, key string) *StrategyDiseaseGroup {
	if key == "" {
		key = "unknown"
	}
	group := groups[key]
	if group == nil {
		group = &StrategyDiseaseGroup{}
		groups[key] = group
	}
	return group
}

func profileGroupKey(profile string) string {
	profile = strings.TrimSpace(profile)
	if missingProfile(profile) {
		return "unknown_profile"
	}
	return profile
}

func symbolGroupKey(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "unknown_symbol"
	}
	return symbol
}

func sideGroupKey(side string) string {
	side = strings.TrimSpace(strings.ToLower(side))
	if side == "" {
		return "unknown_side"
	}
	return side
}

func missingProfile(profile string) bool {
	profile = strings.TrimSpace(strings.ToLower(profile))
	return profile == "" || profile == "unknown" || profile == "unknown_profile"
}

func closeReasonBucket(action DecisionAction) string {
	text := strings.TrimSpace(actionFailureReason(action))
	if action.CloseSource != "" {
		if text == "" {
			text = strings.TrimSpace(action.CloseSource)
		} else {
			text += " " + strings.TrimSpace(action.CloseSource)
		}
	}
	lower := strings.ToLower(text)
	switch {
	case text == "":
		return "unknown"
	case strings.Contains(lower, "exchange_full_tp") || strings.Contains(lower, "algorithmic_full"):
		return "exchange_full_tp"
	case strings.Contains(lower, "stop") || strings.Contains(text, "止损"):
		return "stop_loss"
	case strings.Contains(lower, "tp") || strings.Contains(lower, "take profit") || strings.Contains(text, "止盈"):
		return "take_profit"
	case strings.Contains(lower, "trailing") || strings.Contains(text, "跟踪"):
		return "trailing"
	case strings.Contains(lower, "scaled") || strings.Contains(text, "分批"):
		return "scaled_exit"
	case strings.Contains(lower, "snapshot") || strings.Contains(text, "快照"):
		return "snapshot"
	default:
		return compactReplayKey(lower)
	}
}

func compactReplayKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(" ", "_", ";", "_", ":", "_", "/", "_", "\\", "_")
	value = replacer.Replace(value)
	if len(value) > 48 {
		return value[:48]
	}
	return value
}

func ratioFromPriceDistance(entry, target float64) float64 {
	if entry <= 0 || target <= 0 {
		return 0
	}
	return math.Abs(entry-target) / entry
}

func normalizeLoggedDistanceRatio(ratioValue, percentValue float64) float64 {
	if ratioValue > 0 {
		if ratioValue > 1 {
			return ratioValue / 100
		}
		return ratioValue
	}
	if percentValue > 0 {
		return percentValue / 100
	}
	return 0
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func pricesNear(reference, a, b float64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	tolerance := math.Max(math.Abs(reference)*1e-6, 1e-8)
	return math.Abs(a-b) <= tolerance
}

func nestedDiagnostics(source map[string]any, key string) map[string]any {
	if len(source) == 0 {
		return nil
	}
	return mapFromAny(source[key])
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func boolFromRiskNormalization(value any, key string) bool {
	m := mapFromAny(value)
	if len(m) == 0 {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func buildRMultipleStats(values []float64) RMultipleStats {
	stats := RMultipleStats{Count: len(values)}
	if len(values) == 0 {
		return stats
	}
	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)
	stats.Min = sortedValues[0]
	stats.Max = sortedValues[len(sortedValues)-1]
	sum := 0.0
	for _, value := range values {
		sum += value
		if value > 0 {
			stats.WinningCount++
		} else if value < 0 {
			stats.LosingCount++
		}
	}
	stats.Average = sum / float64(len(values))
	mid := len(sortedValues) / 2
	if len(sortedValues)%2 == 0 {
		stats.Median = (sortedValues[mid-1] + sortedValues[mid]) / 2
	} else {
		stats.Median = sortedValues[mid]
	}
	return stats
}

func attachGroupRStats(groups map[string]*StrategyDiseaseGroup, valuesByKey map[string][]float64) {
	for key, values := range valuesByKey {
		diseaseGroup(groups, key).RMultipleStats = buildRMultipleStats(values)
	}
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

func extractAccountSemantics(records []*DecisionRecord) *AccountSemanticsSummary {
	sortedRecords := sortedReplayRecords(records)
	var summary AccountSemanticsSummary
	for _, record := range sortedRecords {
		if record == nil {
			continue
		}
		snapshot := record.AccountState
		baseline := snapshot.StrategyBaseline
		if baseline <= 0 {
			baseline = snapshot.CostBasis
		}
		if baseline > 0 {
			if summary.FirstStrategyBaseline == 0 {
				summary.FirstStrategyBaseline = baseline
			}
			summary.LastStrategyBaseline = baseline
		}
		if snapshot.BaselineSource != "" {
			summary.BaselineSource = snapshot.BaselineSource
		} else if snapshot.PnLSource != "" && summary.BaselineSource == "" {
			summary.BaselineSource = snapshot.PnLSource
		}
		if snapshot.EquitySource != "" {
			summary.EquitySource = snapshot.EquitySource
		}
		if snapshot.AllocationEnabled {
			summary.AllocationEnabled = true
			summary.AllocatedBalance = snapshot.AllocatedBalance
			summary.AllocatedAvailableBalance = snapshot.AllocatedAvailable
			summary.AllocatedUsedMargin = snapshot.AllocatedUsedMargin
			summary.SizingEquity = snapshot.SizingEquity
			summary.SizingAvailableBalance = snapshot.SizingAvailableBalance
			summary.SizingEquitySource = snapshot.SizingEquitySource
			summary.RiskDenominator = snapshot.RiskDenominator
			summary.RiskDenominatorSource = snapshot.RiskDenominatorSource
		}
	}
	if summary.FirstStrategyBaseline == 0 && summary.BaselineSource == "" && summary.EquitySource == "" && !summary.AllocationEnabled {
		return nil
	}
	return &summary
}

func classifyRejectionBucket(reason string, action DecisionAction) string {
	text := strings.ToLower(reason + " " + action.Error + " " + action.HighRiskReason)
	switch {
	case strings.Contains(text, "counter_di") || strings.Contains(text, "di方向") || strings.Contains(text, "di direction"):
		return "counter_di"
	case strings.Contains(text, "adx") || strings.Contains(text, "趋势开仓"):
		return "adx"
	case strings.Contains(text, "correlation") || strings.Contains(text, "高相关") || strings.Contains(text, "同向高相关") || strings.Contains(text, "同向持仓"):
		return "correlation"
	case strings.Contains(text, "profile") || strings.Contains(text, "品种") || strings.Contains(text, "instrument profile"):
		return "profile"
	case strings.Contains(text, "micro_stop") || strings.Contains(text, "止损距离") || strings.Contains(text, "微止损"):
		return "micro_stop"
	case strings.Contains(text, "micro_tp") || strings.Contains(text, "微止盈"):
		return "micro_tp"
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
