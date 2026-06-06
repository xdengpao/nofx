package diagnostics

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"nofx/logger"
	"nofx/pool"
)

// CandidateEntryEvaluationOptions 控制候选池与 entry trigger 只读评估。
type CandidateEntryEvaluationOptions struct {
	TraderID           string
	DryRun             bool
	ConfigSource       string
	StaticSymbols      []string
	DynamicSnapshot    *pool.DynamicCandidatePool
	DynamicMergedPool  *pool.MergedCoinPool
	DynamicPreviewErr  error
	GeneratedAt        time.Time
	EnabledTraderCount int
	Notes              []string
}

// CandidateEntryEvaluationReport 是动态候选池和缠论 V2 entry trigger 的综合只读报告。
type CandidateEntryEvaluationReport struct {
	GeneratedAt        time.Time                      `json:"generated_at"`
	Window             EvaluationWindow               `json:"window"`
	TraderID           string                         `json:"trader_id,omitempty"`
	DryRun             bool                           `json:"dry_run"`
	ConfigSource       string                         `json:"config_source,omitempty"`
	RuntimeFacts       RuntimeNoOrderFacts            `json:"runtime_facts"`
	StaticPool         CandidatePoolSnapshotSummary   `json:"static_pool"`
	DynamicPoolPreview CandidatePoolSnapshotSummary   `json:"dynamic_pool_preview,omitempty"`
	ShortSideCoverage  ShortSideCoverageReport        `json:"short_side_coverage,omitempty"`
	EntryTriggerFunnel EntryTriggerFunnelReport       `json:"entry_trigger_funnel"`
	OpenProbability    LayeredOpenProbabilityEstimate `json:"open_probability"`
	Notes              []string                       `json:"notes,omitempty"`
}

// EvaluationWindow 记录评估窗口。
type EvaluationWindow struct {
	From        time.Time `json:"from,omitempty"`
	To          time.Time `json:"to,omitempty"`
	RecordCount int       `json:"record_count"`
	Timezone    string    `json:"timezone,omitempty"`
}

// RuntimeNoOrderFacts 汇总最近日志中的执行事实。
type RuntimeNoOrderFacts struct {
	ActionDistribution      map[string]int `json:"action_distribution,omitempty"`
	FinalActionDistribution map[string]int `json:"final_action_distribution,omitempty"`
	TradeIntentDistribution map[string]int `json:"trade_intent_distribution,omitempty"`
	RealExchangeOrderCount  int            `json:"real_exchange_order_count"`
	OpenLikeCandidateCount  int            `json:"open_like_candidate_count"`
	OpenRejectedCount       int            `json:"open_rejected_count"`
	WaitCount               int            `json:"wait_count"`
	EnabledTraderCount      int            `json:"enabled_trader_count,omitempty"`
}

// CandidatePoolSnapshotSummary 汇总候选池覆盖。
type CandidatePoolSnapshotSummary struct {
	Source                  string                     `json:"source"`
	MarketRegime            string                     `json:"market_regime,omitempty"`
	SymbolCount             int                        `json:"symbol_count"`
	PromptCount             int                        `json:"prompt_count,omitempty"`
	ShortSideCandidateCount int                        `json:"short_side_candidate_count,omitempty"`
	ShortSidePromptCount    int                        `json:"short_side_prompt_count,omitempty"`
	Symbols                 []string                   `json:"symbols,omitempty"`
	SourceStatus            map[string]string          `json:"source_status,omitempty"`
	Candidates              []CandidateCoverageSummary `json:"candidates,omitempty"`
}

type CandidateCoverageSummary struct {
	Symbol           string   `json:"symbol"`
	Tier             string   `json:"tier,omitempty"`
	Score            float64  `json:"score,omitempty"`
	Sources          []string `json:"sources,omitempty"`
	Reasons          []string `json:"reasons,omitempty"`
	OIValueUSD       float64  `json:"oi_value_usd,omitempty"`
	QuoteVolumeUSD   float64  `json:"quote_volume_24h_usd,omitempty"`
	ADX              float64  `json:"adx,omitempty"`
	PriceChange1h    float64  `json:"price_change_1h,omitempty"`
	PriceChange4h    float64  `json:"price_change_4h,omitempty"`
	FundingRate      float64  `json:"funding_rate,omitempty"`
	SideBias         string   `json:"side_bias,omitempty"`
	ShortSideScore   float64  `json:"short_side_score,omitempty"`
	LongSideScore    float64  `json:"long_side_score,omitempty"`
	SideReasons      []string `json:"side_reasons,omitempty"`
	IncludedInPrompt bool     `json:"included_in_prompt,omitempty"`
}

// ShortSideCoverageReport 汇总 BTC 弱势下的方向覆盖。
type ShortSideCoverageReport struct {
	Enabled               bool     `json:"enabled"`
	ReportOnly            bool     `json:"report_only"`
	BTCWeak               bool     `json:"btc_weak"`
	MarketRegime          string   `json:"market_regime,omitempty"`
	CandidateCount        int      `json:"candidate_count,omitempty"`
	PromptCount           int      `json:"prompt_count,omitempty"`
	StaticCandidateCount  int      `json:"static_candidate_count,omitempty"`
	DynamicCandidateCount int      `json:"dynamic_candidate_count,omitempty"`
	Symbols               []string `json:"symbols,omitempty"`
	Reasons               []string `json:"reasons,omitempty"`
}

// EntryTriggerFunnelReport 汇总缠论 V2 父结构到 entry trigger 的漏斗。
type EntryTriggerFunnelReport struct {
	RawSignalCount          int                                 `json:"raw_signal_count,omitempty"`
	ParentStructureCount    int                                 `json:"parent_structure_count,omitempty"`
	ParentTerminalByReason  map[string]int                      `json:"parent_terminal_by_reason,omitempty"`
	WaitingForTriggerCount  int                                 `json:"waiting_for_trigger_count,omitempty"`
	TriggerReadyCount       int                                 `json:"trigger_ready_count,omitempty"`
	TriggerReadyByType      map[string]int                      `json:"trigger_ready_by_type,omitempty"`
	TriggerRejectedByReason map[string]int                      `json:"trigger_rejected_by_reason,omitempty"`
	TerminalSuppressedCount int                                 `json:"terminal_suppressed_count,omitempty"`
	OpenRejectionByReason   map[string]int                      `json:"open_rejection_by_reason,omitempty"`
	PerSymbol               map[string]EntryTriggerSymbolFunnel `json:"per_symbol,omitempty"`
	FieldMissingCount       int                                 `json:"field_missing_count,omitempty"`
}

type EntryTriggerSymbolFunnel struct {
	RawSignalCount         int            `json:"raw_signal_count,omitempty"`
	ParentStructureCount   int            `json:"parent_structure_count,omitempty"`
	ParentTerminalByReason map[string]int `json:"parent_terminal_by_reason,omitempty"`
	WaitingForTriggerCount int            `json:"waiting_for_trigger_count,omitempty"`
	TriggerReadyCount      int            `json:"trigger_ready_count,omitempty"`
}

// LayeredOpenProbabilityEstimate 是分层开仓概率评估，不是收益或下单承诺。
type LayeredOpenProbabilityEstimate struct {
	CandidateCoverageRate float64  `json:"candidate_coverage_rate,omitempty"`
	ParentSignalRate      float64  `json:"parent_signal_rate,omitempty"`
	EntryTriggerRate      float64  `json:"entry_trigger_rate,omitempty"`
	OpenGatePassRate      float64  `json:"open_gate_pass_rate,omitempty"`
	FinalRRPassRate       float64  `json:"final_rr_pass_rate,omitempty"`
	Qualitative           string   `json:"qualitative_probability"`
	BlockingLayers        []string `json:"blocking_layers,omitempty"`
	Notes                 []string `json:"notes,omitempty"`
}

var (
	v2RRDirectTerminal = regexp.MustCompile(`([A-Z0-9]+USDT)\s+([a-zA-Z0-9_]+).*父结构终止:.*剩余净RR`)
	v2WaitTrigger      = regexp.MustCompile(`([A-Z0-9]+USDT)\s+([a-zA-Z0-9_]+).*等待.*entry trigger`)
	v2ReadyTrigger     = regexp.MustCompile(`([A-Z0-9]+USDT)\s+([a-zA-Z0-9_]+).*entry trigger ready`)
)

// BuildCandidateEntryEvaluationReport 基于已过滤日志和可选动态池预览构建只读评估报告。
func BuildCandidateEntryEvaluationReport(records []*logger.DecisionRecord, opts CandidateEntryEvaluationOptions) CandidateEntryEvaluationReport {
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	report := CandidateEntryEvaluationReport{
		GeneratedAt:        generatedAt,
		TraderID:           strings.TrimSpace(opts.TraderID),
		DryRun:             opts.DryRun,
		ConfigSource:       strings.TrimSpace(opts.ConfigSource),
		Window:             buildEvaluationWindow(records),
		RuntimeFacts:       buildRuntimeFacts(records),
		StaticPool:         buildStaticPoolSummary(records, opts.StaticSymbols),
		EntryTriggerFunnel: BuildEntryTriggerFunnelReport(records),
		Notes:              append([]string(nil), opts.Notes...),
	}
	if opts.DynamicPreviewErr != nil {
		report.Notes = append(report.Notes, "dynamic_pool_preview_error: "+opts.DynamicPreviewErr.Error())
	}
	if opts.EnabledTraderCount > 0 {
		report.RuntimeFacts.EnabledTraderCount = opts.EnabledTraderCount
	}
	if opts.DynamicSnapshot != nil {
		report.DynamicPoolPreview = buildDynamicPoolSummary(opts.DynamicSnapshot, opts.DynamicMergedPool)
		report.ShortSideCoverage = buildShortSideCoverageReport(report.StaticPool, report.DynamicPoolPreview, opts.DynamicSnapshot)
	}
	report.OpenProbability = buildLayeredOpenProbability(report)
	if report.DryRun {
		report.Notes = append(report.Notes, "dry_run=true: 本报告只读评估候选覆盖和entry trigger漏斗，不触发真实下单")
	}
	return report
}

func buildEvaluationWindow(records []*logger.DecisionRecord) EvaluationWindow {
	window := EvaluationWindow{RecordCount: len(records)}
	if len(records) == 0 {
		return window
	}
	window.From = records[0].Timestamp
	window.To = records[len(records)-1].Timestamp
	if loc := window.To.Location(); loc != nil {
		window.Timezone = loc.String()
	}
	return window
}

func buildRuntimeFacts(records []*logger.DecisionRecord) RuntimeNoOrderFacts {
	facts := RuntimeNoOrderFacts{
		ActionDistribution:      map[string]int{},
		FinalActionDistribution: map[string]int{},
		TradeIntentDistribution: map[string]int{},
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, action := range record.Decisions {
			name := strings.TrimSpace(action.Action)
			if name == "" {
				name = "unknown"
			}
			facts.ActionDistribution[name]++
			finalAction := strings.TrimSpace(action.FinalAction)
			if finalAction == "" {
				finalAction = name
			}
			facts.FinalActionDistribution[finalAction]++
			if intent := strings.TrimSpace(action.TradeIntent); intent != "" {
				facts.TradeIntentDistribution[intent]++
			}
			if isOpenLikeActionName(name) || isOpenLikeActionName(finalAction) {
				facts.OpenLikeCandidateCount++
			}
			if name == "open_rejected" || finalAction == "open_rejected" {
				facts.OpenRejectedCount++
			}
			if finalAction == "wait" || name == "wait" {
				facts.WaitCount++
			}
			if action.Success && isOpenLikeActionName(finalAction) && finalAction != "open_rejected" {
				facts.RealExchangeOrderCount++
			}
		}
	}
	return facts
}

func buildStaticPoolSummary(records []*logger.DecisionRecord, configured []string) CandidatePoolSnapshotSummary {
	summary := CandidatePoolSnapshotSummary{Source: "static_or_logged"}
	seen := map[string]bool{}
	add := func(symbol string) {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" || seen[symbol] {
			return
		}
		seen[symbol] = true
		summary.Symbols = append(summary.Symbols, symbol)
	}
	for _, symbol := range configured {
		add(symbol)
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, symbol := range record.CandidateCoins {
			add(symbol)
		}
		for _, detail := range record.CandidateDetails {
			add(detail.Symbol)
		}
	}
	sort.Strings(summary.Symbols)
	summary.SymbolCount = len(summary.Symbols)
	return summary
}

func buildDynamicPoolSummary(snapshot *pool.DynamicCandidatePool, merged *pool.MergedCoinPool) CandidatePoolSnapshotSummary {
	summary := CandidatePoolSnapshotSummary{
		Source:       "dynamic_preview",
		MarketRegime: snapshot.MarketRegime,
		SourceStatus: cloneStringMap(snapshot.SourceStatus),
	}
	promptSet := map[string]bool{}
	if merged != nil {
		for _, symbol := range merged.AllSymbols {
			promptSet[symbol] = true
		}
	}
	for _, candidate := range snapshot.Symbols {
		inPrompt := promptSet[candidate.Symbol]
		if inPrompt {
			summary.PromptCount++
		}
		if candidate.SideProfile.Bias == "short" {
			summary.ShortSideCandidateCount++
			if inPrompt {
				summary.ShortSidePromptCount++
			}
		}
		summary.Symbols = append(summary.Symbols, candidate.Symbol)
		summary.Candidates = append(summary.Candidates, CandidateCoverageSummary{
			Symbol:           candidate.Symbol,
			Tier:             candidate.Tier,
			Score:            candidate.Score,
			Sources:          append([]string(nil), candidate.Sources...),
			Reasons:          append([]string(nil), candidate.Reasons...),
			OIValueUSD:       candidate.Metrics.OIValueUSD,
			QuoteVolumeUSD:   candidate.Metrics.QuoteVolume24hUSD,
			ADX:              candidate.Metrics.ADX,
			PriceChange1h:    candidate.Metrics.PriceChange1h,
			PriceChange4h:    candidate.Metrics.PriceChange4h,
			FundingRate:      candidate.Metrics.FundingRate,
			SideBias:         candidate.SideProfile.Bias,
			ShortSideScore:   candidate.SideProfile.ShortScore,
			LongSideScore:    candidate.SideProfile.LongScore,
			SideReasons:      append([]string(nil), candidate.SideProfile.Reasons...),
			IncludedInPrompt: inPrompt,
		})
	}
	summary.SymbolCount = len(summary.Symbols)
	return summary
}

func buildShortSideCoverageReport(static, dynamic CandidatePoolSnapshotSummary, snapshot *pool.DynamicCandidatePool) ShortSideCoverageReport {
	report := ShortSideCoverageReport{
		Enabled:               snapshot.ShortSideSummary.Enabled,
		ReportOnly:            snapshot.ShortSideSummary.ReportOnly,
		BTCWeak:               snapshot.ShortSideSummary.BTCWeak,
		MarketRegime:          snapshot.MarketRegime,
		CandidateCount:        snapshot.ShortSideSummary.CandidateCount,
		PromptCount:           snapshot.ShortSideSummary.PromptCount,
		StaticCandidateCount:  static.ShortSideCandidateCount,
		DynamicCandidateCount: dynamic.ShortSideCandidateCount,
		Symbols:               append([]string(nil), snapshot.ShortSideSummary.Symbols...),
		Reasons:               append([]string(nil), snapshot.ShortSideSummary.Reasons...),
	}
	if report.CandidateCount == 0 {
		report.CandidateCount = dynamic.ShortSideCandidateCount
	}
	if report.PromptCount == 0 {
		report.PromptCount = dynamic.ShortSidePromptCount
	}
	return report
}

// BuildEntryTriggerFunnelReport 从结构化 diagnostics 和旧日志文案中聚合 entry trigger 漏斗。
func BuildEntryTriggerFunnelReport(records []*logger.DecisionRecord) EntryTriggerFunnelReport {
	report := EntryTriggerFunnelReport{
		ParentTerminalByReason:  map[string]int{},
		TriggerReadyByType:      map[string]int{},
		TriggerRejectedByReason: map[string]int{},
		OpenRejectionByReason:   map[string]int{},
		PerSymbol:               map[string]EntryTriggerSymbolFunnel{},
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		if !mergeStructuredFunnel(&report, record.StrategyDiagnostics) {
			report.FieldMissingCount++
			mergeLegacyDiagnostics(&report, record)
		}
		for _, action := range record.Decisions {
			if action.Action == "open_rejected" {
				reason := actionGateReasonCode(action)
				report.OpenRejectionByReason[reason]++
			}
		}
	}
	pruneEmptyFunnelMaps(&report)
	return report
}

func mergeStructuredFunnel(report *EntryTriggerFunnelReport, diagnostics map[string]any) bool {
	if len(diagnostics) == 0 {
		return false
	}
	if funnelRaw, ok := diagnostics["entry_trigger_funnel"]; ok {
		if funnel, ok := funnelRaw.(map[string]any); ok {
			report.RawSignalCount += anyInt(funnel["raw_signal_count"])
			report.ParentStructureCount += anyInt(funnel["parent_structure_count"])
			report.WaitingForTriggerCount += anyInt(funnel["waiting_for_trigger_count"])
			report.TriggerReadyCount += anyInt(funnel["trigger_ready_count"])
			report.TerminalSuppressedCount += anyInt(funnel["terminal_suppressed_count"])
			mergeAnyMapCounts(report.ParentTerminalByReason, funnel["parent_terminal_by_reason"])
			mergeAnyMapCounts(report.TriggerReadyByType, funnel["trigger_ready_by_type"])
			mergeAnyMapCounts(report.TriggerRejectedByReason, funnel["trigger_rejected_by_reason"])
			mergeAnySymbolFunnels(report, funnel["per_symbol"])
			return true
		}
	}
	seen := false
	for key, dst := range map[string]*int{
		"raw_signal_count":          &report.RawSignalCount,
		"parent_structure_count":    &report.ParentStructureCount,
		"entry_trigger_count":       &report.TriggerReadyCount,
		"terminal_suppressed_count": &report.TerminalSuppressedCount,
	} {
		if value, ok := diagnostics[key]; ok {
			*dst += anyInt(value)
			seen = true
		}
	}
	if value, ok := diagnostics["trigger_rejection_reasons"]; ok {
		mergeAnyMapCounts(report.TriggerRejectedByReason, value)
		seen = true
	}
	return seen
}

func mergeLegacyDiagnostics(report *EntryTriggerFunnelReport, record *logger.DecisionRecord) {
	for _, message := range strategyDiagnosticMessages(record) {
		symbol := symbolFromMessage(message)
		if strings.Contains(message, "终态信号已静默") || strings.Contains(message, "重复过期信号已静默") {
			report.TerminalSuppressedCount++
			report.ParentTerminalByReason[firstNonEmpty(reasonCodeFromMessage(message), "terminal_suppressed")]++
			continue
		}
		if v2RRDirectTerminal.MatchString(message) || strings.Contains(message, "父结构终止") {
			reason := firstNonEmpty(reasonCodeFromMessage(message), inferParentTerminalReason(message))
			report.ParentTerminalByReason[reason]++
			incrementSymbolFunnel(report, symbol, func(f *EntryTriggerSymbolFunnel) {
				if f.ParentTerminalByReason == nil {
					f.ParentTerminalByReason = map[string]int{}
				}
				f.ParentTerminalByReason[reason]++
			})
			continue
		}
		if strings.Contains(message, "waiting_for_fresh_entry_trigger") || v2WaitTrigger.MatchString(message) {
			report.WaitingForTriggerCount++
			incrementSymbolFunnel(report, symbol, func(f *EntryTriggerSymbolFunnel) { f.WaitingForTriggerCount++ })
			continue
		}
		if strings.Contains(message, "fresh entry trigger ready") || v2ReadyTrigger.MatchString(message) {
			report.TriggerReadyCount++
			incrementSymbolFunnel(report, symbol, func(f *EntryTriggerSymbolFunnel) { f.TriggerReadyCount++ })
		}
	}
}

func buildLayeredOpenProbability(report CandidateEntryEvaluationReport) LayeredOpenProbabilityEstimate {
	recordCount := float64(report.Window.RecordCount)
	estimate := LayeredOpenProbabilityEstimate{Qualitative: "unknown"}
	if recordCount > 0 {
		if report.DynamicPoolPreview.SymbolCount > 0 {
			estimate.CandidateCoverageRate = ratio(float64(report.DynamicPoolPreview.SymbolCount), math.Max(1, float64(report.StaticPool.SymbolCount)))
		}
		estimate.ParentSignalRate = ratio(float64(report.EntryTriggerFunnel.ParentStructureCount), recordCount)
	}
	estimate.EntryTriggerRate = ratio(float64(report.EntryTriggerFunnel.TriggerReadyCount), float64(report.EntryTriggerFunnel.ParentStructureCount))
	openCandidates := report.RuntimeFacts.OpenLikeCandidateCount
	success := report.RuntimeFacts.RealExchangeOrderCount
	estimate.OpenGatePassRate = ratio(float64(success), float64(openCandidates))
	finalRRRejects := report.EntryTriggerFunnel.OpenRejectionByReason["final_rr"] + report.EntryTriggerFunnel.OpenRejectionByReason["final_rr_2.5"]
	estimate.FinalRRPassRate = ratio(float64(openCandidates-finalRRRejects), float64(openCandidates))
	if report.EntryTriggerFunnel.TriggerReadyCount == 0 {
		estimate.Qualitative = "very_low"
		estimate.BlockingLayers = append(estimate.BlockingLayers, "entry_trigger_generation")
	} else if openCandidates == 0 {
		estimate.Qualitative = "low"
		estimate.BlockingLayers = append(estimate.BlockingLayers, "open_gate_or_final_validation")
	} else if success == 0 {
		estimate.Qualitative = "low"
		estimate.BlockingLayers = append(estimate.BlockingLayers, "final_rr_2.5_or_open_gate")
	} else {
		estimate.Qualitative = "medium"
	}
	if report.EntryTriggerFunnel.TerminalSuppressedCount > report.EntryTriggerFunnel.TriggerReadyCount {
		estimate.BlockingLayers = append(estimate.BlockingLayers, "terminal_suppression")
	}
	estimate.Notes = append(estimate.Notes, "分层概率基于历史日志计数和当前候选池预览，不构成收益或下单承诺")
	return estimate
}

func strategyDiagnosticMessages(record *logger.DecisionRecord) []string {
	if record == nil || len(record.StrategyDiagnostics) == 0 {
		return nil
	}
	raw := record.StrategyDiagnostics["messages"]
	switch values := raw.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), values...)
	default:
		return nil
	}
}

func isOpenLikeActionName(action string) bool {
	switch strings.TrimSpace(action) {
	case "open_long", "open_short", "open_rejected":
		return true
	default:
		return false
	}
}

func actionGateReasonCode(action logger.DecisionAction) string {
	if len(action.GateReasons) > 0 {
		if reason := strings.TrimSpace(action.GateReasons[0]); reason != "" {
			return normalizeRejectionReason(reason)
		}
	}
	if action.GateDiagnostics != nil {
		for _, key := range []string{"reason_code", "guard_reason_code", "min_confidence_reason_code"} {
			if value, ok := action.GateDiagnostics[key]; ok {
				if reason := strings.TrimSpace(fmtAny(value)); reason != "" {
					return normalizeRejectionReason(reason)
				}
			}
		}
	}
	return normalizeRejectionReason(firstNonEmpty(action.Error, action.Reasoning, "unknown"))
}

func normalizeRejectionReason(reason string) string {
	reason = strings.TrimSpace(reason)
	lower := strings.ToLower(reason)
	switch {
	case reason == "":
		return "unknown"
	case strings.Contains(lower, "final_rr") ||
		strings.Contains(lower, "remaining_net_rr") ||
		strings.Contains(lower, "net rr") ||
		strings.Contains(reason, "风险回报比"):
		return "final_rr"
	case strings.Contains(lower, "btc"):
		return "btc"
	case strings.Contains(lower, "freshness_gate") ||
		strings.Contains(lower, "signal_expired") ||
		strings.Contains(reason, "过期"):
		return "freshness"
	case strings.Contains(lower, "confidence") ||
		strings.Contains(reason, "置信度"):
		return "confidence"
	case strings.Contains(lower, "adx"):
		return "adx"
	case strings.Contains(lower, "loss_mode") ||
		strings.Contains(reason, "亏损模式"):
		return "loss_mode"
	case strings.Contains(lower, "correlation") ||
		strings.Contains(reason, "相关"):
		return "correlation"
	default:
		fields := strings.Fields(reason)
		if len(fields) > 0 && len(fields[0]) <= 80 {
			return strings.Trim(fields[0], "。；;，,")
		}
		return "other"
	}
}

func fmtAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func mergeAnyMapCounts(dst map[string]int, raw any) {
	switch values := raw.(type) {
	case map[string]int:
		for key, value := range values {
			dst[key] += value
		}
	case map[string]any:
		for key, value := range values {
			dst[key] += anyInt(value)
		}
	}
}

func mergeAnySymbolFunnels(report *EntryTriggerFunnelReport, raw any) {
	if report == nil {
		return
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for symbol, rawValue := range values {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		rawMap, ok := rawValue.(map[string]any)
		if !ok {
			continue
		}
		current := report.PerSymbol[symbol]
		current.RawSignalCount += anyInt(rawMap["raw_signal_count"])
		current.ParentStructureCount += anyInt(rawMap["parent_structure_count"])
		current.WaitingForTriggerCount += anyInt(rawMap["waiting_for_trigger_count"])
		current.TriggerReadyCount += anyInt(rawMap["trigger_ready_count"])
		if current.ParentTerminalByReason == nil {
			current.ParentTerminalByReason = map[string]int{}
		}
		mergeAnyMapCounts(current.ParentTerminalByReason, rawMap["parent_terminal_by_reason"])
		if len(current.ParentTerminalByReason) == 0 {
			current.ParentTerminalByReason = nil
		}
		report.PerSymbol[symbol] = current
	}
}

func pruneEmptyFunnelMaps(report *EntryTriggerFunnelReport) {
	if len(report.ParentTerminalByReason) == 0 {
		report.ParentTerminalByReason = nil
	}
	if len(report.TriggerReadyByType) == 0 {
		report.TriggerReadyByType = nil
	}
	if len(report.TriggerRejectedByReason) == 0 {
		report.TriggerRejectedByReason = nil
	}
	if len(report.OpenRejectionByReason) == 0 {
		report.OpenRejectionByReason = nil
	}
	if len(report.PerSymbol) == 0 {
		report.PerSymbol = nil
	}
}

func incrementSymbolFunnel(report *EntryTriggerFunnelReport, symbol string, mutate func(*EntryTriggerSymbolFunnel)) {
	if symbol == "" {
		return
	}
	value := report.PerSymbol[symbol]
	mutate(&value)
	report.PerSymbol[symbol] = value
}

func symbolFromMessage(message string) string {
	for _, field := range strings.Fields(message) {
		field = strings.Trim(field, " ,;:[]()")
		if strings.HasSuffix(field, "USDT") {
			return field
		}
	}
	return ""
}

func reasonCodeFromMessage(message string) string {
	idx := strings.LastIndex(message, ":")
	if idx < 0 || idx+1 >= len(message) {
		return ""
	}
	value := strings.TrimSpace(message[idx+1:])
	value = strings.Fields(value)[0]
	if strings.ContainsAny(value, "。；,，") {
		value = strings.Trim(value, "。；,，")
	}
	if strings.Contains(value, ".") || strings.Contains(value, "_") {
		return value
	}
	return ""
}

func inferParentTerminalReason(message string) string {
	switch {
	case strings.Contains(message, "剩余净RR"):
		return "entry_rr_invalid"
	case strings.Contains(message, "目标"):
		return "entry_parent.target_crossed"
	case strings.Contains(message, "观察窗口过期"):
		return "entry_parent.watch_window_expired"
	case strings.Contains(message, "结构无效"):
		return "entry_parent.invalid_structure"
	default:
		return "parent_terminal"
	}
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
