package logger

import (
	"encoding/json"
	"fmt"
	"math"
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
	report.StrategyDisease = BuildStrategyDiseaseReport(records)
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
