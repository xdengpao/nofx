package chanlunv2

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"nofx/strategy/chanlun"
	"sort"
	"strings"
	"time"
)

const maxChanlunV2ReportMarkers = 200

type chanlunSignalReport = chanlun.SignalReport
type chanlunStrategySymbol = chanlun.StrategySymbol

func hashChanlunV2Config(cfg config.ChanlunV2StrategyConfig) string {
	payload, err := json.Marshal(cfg)
	if err != nil {
		return "v2-default"
	}
	sum := sha1.Sum(payload)
	return "v2-" + hex.EncodeToString(sum[:])[:10]
}

func (e *Engine) setUniverse(traderID string, universe []chanlunStrategySymbol) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.symbolUniverse[traderID] = append([]chanlunStrategySymbol(nil), universe...)
}

func (e *Engine) SymbolUniverse(traderID string) []chanlunStrategySymbol {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]chanlunStrategySymbol(nil), e.symbolUniverse[traderID]...)
}

func (e *Engine) LatestSignalsWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) (*chanlun.SignalReport, bool) {
	symbol = market.Normalize(symbol)
	e.mu.RLock()
	report, ok := e.latestSignals[traderID+"|"+symbol]
	copied := cloneChanlunV2Report(report)
	e.mu.RUnlock()
	if !ok {
		return nil, false
	}
	applyChanlunV2ReportOptions(copied, opts)
	return copied, true
}

func (e *Engine) EmptySignalReportWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) *chanlun.SignalReport {
	report := e.baseSignalReport(traderID, market.Normalize(symbol))
	report.LatestDiagnostics = map[string]any{
		"messages": []string{"暂无该标的的缠论V2策略信号"},
	}
	applyChanlunV2ReportOptions(report, opts)
	return report
}

func (e *Engine) setLatestReport(traderID, symbol string, mr *multiLevelResult, signals []Signal, diagnostics []string) {
	symbol = market.Normalize(symbol)
	report := e.baseSignalReport(traderID, symbol)
	report.Signals = make([]chanlun.ChanlunSignal, 0, len(signals))
	report.SignalMarkers = make([]chanlun.SignalMarker, 0, len(signals))
	for _, sig := range signals {
		clSignal := e.signalToReportSignal(symbol, report.TradeTimeframe, sig)
		report.Signals = append(report.Signals, clSignal)
		report.SignalMarkers = append(report.SignalMarkers, signalToV2Marker(clSignal, "ready", clSignal.ActionHint, "缠论V2买卖点信号"))
	}
	if len(diagnostics) == 0 && len(signals) == 0 {
		diagnostics = []string{fmt.Sprintf("%s %s 无买卖点信号", symbol, report.TradeTimeframe)}
	}
	report.LatestDiagnostics = map[string]any{
		"messages":  append([]string(nil), diagnostics...),
		"structure": describeMultiLevelResult(mr),
	}
	report.MarkerSummary = buildChanlunV2MarkerSummary(report.SignalMarkers, report.SignalMarkers)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.latestSignals[traderID+"|"+symbol] = report
}

func (e *Engine) appendDecisionMarker(traderID string, d decision.Decision) {
	symbol := market.Normalize(d.Symbol)
	if symbol == "" || symbol == "ALL" {
		return
	}
	marker := decisionToV2Marker(d)
	e.mu.Lock()
	defer e.mu.Unlock()
	key := traderID + "|" + symbol
	report := e.latestSignals[key]
	if report == nil {
		report = e.baseSignalReport(traderID, symbol)
	}
	report.SignalMarkers = append(report.SignalMarkers, marker)
	if len(report.SignalMarkers) > maxChanlunV2ReportMarkers {
		report.SignalMarkers = report.SignalMarkers[len(report.SignalMarkers)-maxChanlunV2ReportMarkers:]
	}
	if report.LatestDiagnostics == nil {
		report.LatestDiagnostics = map[string]any{}
	}
	report.LatestDiagnostics["position_management"] = d.Reasoning
	report.MarkerSummary = buildChanlunV2MarkerSummary(report.SignalMarkers, report.SignalMarkers)
	e.latestSignals[key] = report
}

func (e *Engine) baseSignalReport(traderID, symbol string) *chanlun.SignalReport {
	timeframes := e.resolveTimeframes()
	return &chanlun.SignalReport{
		TraderID:           traderID,
		Symbol:             symbol,
		DecisionMode:       "chanlun_v2",
		StrategyName:       "chanlun_v2",
		StrategyVersion:    "v0.1",
		ConfigHash:         e.configHash,
		TradeTimeframe:     timeframes["trade"],
		ComponentTimeframe: timeframes["sub"],
		MicroTimeframe:     timeframes["micro"],
		Signals:            []chanlun.ChanlunSignal{},
		SignalMarkers:      []chanlun.SignalMarker{},
	}
}

func (e *Engine) signalToReportSignal(symbol, timeframe string, sig Signal) chanlun.ChanlunSignal {
	closeTime := normalizeV2EpochMillis(sig.Timestamp)
	signalID := v2SignalID(symbol, timeframe, sig)
	centerID := signalCenterID(sig)
	return chanlun.ChanlunSignal{
		SignalID:          signalID,
		StructureKey:      v2StructureKey(symbol, timeframe, sig),
		LifecycleKey:      signalID,
		Symbol:            symbol,
		Direction:         sig.Direction,
		SignalType:        sig.SignalType,
		ActionHint:        signalActionHint(sig),
		AnalysisTF:        timeframe,
		TriggerTF:         timeframe,
		Level:             "trade",
		Price:             sig.Price,
		StopLoss:          sig.StopLoss,
		TakeProfit:        sig.TakeProfit,
		StructureTarget:   sig.TakeProfit,
		CenterID:          centerID,
		Confidence:        sig.Confidence,
		ConfirmedAt:       timeFromEpochMillis(closeTime),
		TriggerCloseTime:  closeTime,
		SignalCloseTime:   closeTime,
		DecisionCloseTime: closeTime,
		SegmentEndTime:    closeTime,
		Status:            "ready",
		SourceLayer:       "trade_action",
		ReasonCode:        "chanlun_v2_signal",
		Diagnostics: chanlun.SignalDiagnostics{
			Reasons: []string{fmt.Sprintf("缠论V2 %s 置信度%d", sig.SignalType, sig.Confidence)},
			Metrics: map[string]any{
				"divergence_strength": sig.DivergenceStrength,
				"center_id":           centerID,
			},
		},
	}
}

func signalToV2Marker(signal chanlun.ChanlunSignal, status, action, reason string) chanlun.SignalMarker {
	closeTime := firstPositiveInt64(signal.SignalCloseTime, signal.TriggerCloseTime, signal.SegmentEndTime)
	if closeTime == 0 {
		closeTime = normalizeV2EpochMillis(time.Now().UnixMilli())
	}
	if status == "" {
		status = signal.Status
	}
	if status == "" {
		status = "ready"
	}
	if action == "" {
		action = signal.ActionHint
	}
	return chanlun.SignalMarker{
		Symbol:            signal.Symbol,
		Timeframe:         signal.AnalysisTF,
		CloseTime:         closeTime,
		SignalCloseTime:   closeTime,
		DecisionCloseTime: firstPositiveInt64(signal.DecisionCloseTime, closeTime),
		DisplayCloseTime:  closeTime,
		SignalType:        signal.SignalType,
		Direction:         signal.Direction,
		Level:             signal.Level,
		SourceLayer:       "trade_action",
		Status:            status,
		SignalID:          signal.SignalID,
		StructureKey:      signal.StructureKey,
		LifecycleKey:      signal.LifecycleKey,
		ReasonCode:        signal.ReasonCode,
		DisplayCategory:   "trade_action",
		DisplayPriority:   90,
		Action:            action,
		FinalAction:       action,
		TradeIntent:       action,
		Price:             signal.Price,
		Reason:            reason,
	}
}

func decisionToV2Marker(d decision.Decision) chanlun.SignalMarker {
	timeframe := firstNonEmptyString(metadataString(d.StrategyMetadata, "timeframe"), d.SignalTimeframe, "1h")
	closeTime := metadataInt64(d.StrategyMetadata, "signal_close_time")
	if closeTime == 0 {
		closeTime = normalizeV2EpochMillis(time.Now().UnixMilli())
	}
	decisionCloseTime := metadataInt64(d.StrategyMetadata, "decision_close_time")
	if decisionCloseTime == 0 {
		decisionCloseTime = closeTime
	}
	signalID := d.SignalID
	if signalID == "" {
		signalID = fmt.Sprintf("chanlun_v2:%s:%s:%d", market.Normalize(d.Symbol), d.Action, closeTime)
	}
	signalType := firstNonEmptyString(d.SignalType, metadataString(d.StrategyMetadata, "signal_type"), d.Action)
	direction := "long"
	if strings.Contains(strings.ToLower(d.Action), "short") {
		direction = "short"
	}
	return chanlun.SignalMarker{
		Symbol:            market.Normalize(d.Symbol),
		Timeframe:         timeframe,
		CloseTime:         closeTime,
		SignalCloseTime:   closeTime,
		DecisionCloseTime: decisionCloseTime,
		DisplayCloseTime:  decisionCloseTime,
		SignalType:        signalType,
		Direction:         direction,
		Level:             "position",
		SourceLayer:       "position_management",
		Status:            "ready",
		SignalID:          signalID,
		LifecycleKey:      signalID,
		ReasonCode:        firstNonEmptyString(metadataString(d.StrategyMetadata, "reason_code"), "chanlun_v2_position_management"),
		DisplayCategory:   "position_management",
		DisplayPriority:   70,
		Action:            d.Action,
		FinalAction:       d.Action,
		TradeIntent:       d.Action,
		PositionSide:      metadataString(d.StrategyMetadata, "position_side"),
		Reason:            d.Reasoning,
	}
}

func applyChanlunV2ReportOptions(report *chanlun.SignalReport, opts chanlun.SignalReportOptions) {
	if report == nil {
		return
	}
	opts = normalizeChanlunV2ReportOptions(opts)
	raw := append([]chanlun.SignalMarker(nil), report.SignalMarkers...)
	filtered := filterChanlunV2Markers(raw, opts)
	returned := filtered
	if opts.Limit > 0 && len(returned) > opts.Limit {
		returned = returned[len(returned)-opts.Limit:]
	}
	report.SignalMarkers = append([]chanlun.SignalMarker(nil), returned...)
	report.View = opts.View
	report.Filters = chanlun.SignalReportFilters{
		Layers:   append([]string(nil), opts.Layers...),
		Statuses: append([]string(nil), opts.Statuses...),
		From:     opts.From,
		To:       opts.To,
		Limit:    opts.Limit,
	}
	report.MarkerSummary = buildChanlunV2MarkerSummary(raw, returned)
	if report.LatestDiagnostics == nil {
		report.LatestDiagnostics = map[string]any{}
	}
	report.LatestDiagnostics["marker_summary"] = report.MarkerSummary
}

func normalizeChanlunV2ReportOptions(opts chanlun.SignalReportOptions) chanlun.SignalReportOptions {
	view := strings.ToLower(strings.TrimSpace(opts.View))
	if view != "audit" {
		view = "default"
	}
	opts.View = view
	opts.Layers = normalizeV2StringSet(opts.Layers)
	opts.Statuses = normalizeV2StringSet(opts.Statuses)
	if opts.Limit < 0 {
		opts.Limit = 0
	}
	if opts.View == "audit" && opts.Limit == 0 {
		opts.Limit = maxChanlunV2ReportMarkers
	}
	return opts
}

func filterChanlunV2Markers(markers []chanlun.SignalMarker, opts chanlun.SignalReportOptions) []chanlun.SignalMarker {
	layerSet := stringSet(opts.Layers)
	statusSet := stringSet(opts.Statuses)
	out := make([]chanlun.SignalMarker, 0, len(markers))
	for _, marker := range markers {
		if len(layerSet) > 0 &&
			!layerSet[strings.ToLower(strings.TrimSpace(marker.SourceLayer))] &&
			!layerSet[strings.ToLower(strings.TrimSpace(marker.DisplayCategory))] {
			continue
		}
		if len(statusSet) > 0 && !statusSet[strings.ToLower(strings.TrimSpace(marker.Status))] {
			continue
		}
		t := firstPositiveInt64(marker.DisplayCloseTime, marker.DecisionCloseTime, marker.SignalCloseTime, marker.CloseTime)
		if opts.From > 0 && t > 0 && t < opts.From {
			continue
		}
		if opts.To > 0 && t > 0 && t > opts.To {
			continue
		}
		out = append(out, marker)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti := firstPositiveInt64(out[i].DisplayCloseTime, out[i].DecisionCloseTime, out[i].SignalCloseTime, out[i].CloseTime)
		tj := firstPositiveInt64(out[j].DisplayCloseTime, out[j].DecisionCloseTime, out[j].SignalCloseTime, out[j].CloseTime)
		if ti != tj {
			return ti < tj
		}
		return out[i].SignalID < out[j].SignalID
	})
	return out
}

func buildChanlunV2MarkerSummary(raw, returned []chanlun.SignalMarker) chanlun.SignalMarkerSummary {
	summary := chanlun.SignalMarkerSummary{
		TotalRaw:        len(raw),
		TotalReturned:   len(returned),
		HiddenByDefault: maxInt(len(raw)-len(returned), 0),
		ByCategory:      map[string]int{},
		ByStatus:        map[string]int{},
	}
	latencies := make([]float64, 0, len(returned))
	for _, marker := range returned {
		category := marker.DisplayCategory
		if category == "" {
			category = marker.SourceLayer
		}
		status := marker.Status
		if status == "" {
			status = "unknown"
		}
		summary.ByCategory[category]++
		summary.ByStatus[status]++
		signalTime := firstPositiveInt64(marker.SignalCloseTime, marker.CloseTime)
		decisionTime := firstPositiveInt64(marker.DecisionCloseTime, marker.DisplayCloseTime)
		if signalTime > 0 && decisionTime > signalTime {
			latencies = append(latencies, float64(decisionTime-signalTime)/float64(time.Hour/time.Millisecond))
		}
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		summary.MaxLatencyHours = latencies[len(latencies)-1]
		mid := len(latencies) / 2
		if len(latencies)%2 == 0 {
			summary.MedianLatencyHours = (latencies[mid-1] + latencies[mid]) / 2
		} else {
			summary.MedianLatencyHours = latencies[mid]
		}
	}
	return summary
}

func describeMultiLevelResult(mr *multiLevelResult) map[string]any {
	if mr == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for level, result := range mr.Results {
		if result == nil {
			continue
		}
		out[level] = map[string]any{
			"trend":       result.Trend,
			"fractals":    len(result.Fractals),
			"strokes":     len(result.Strokes),
			"segments":    len(result.Segments),
			"centers":     len(result.Centers),
			"divergences": len(result.Divergences),
			"signals":     len(result.Signals),
		}
	}
	return out
}

func (e *Engine) resolveSymbolUniverse(ctx *decision.Context) []chanlunStrategySymbol {
	if ctx == nil {
		return nil
	}
	items := map[string]chanlunStrategySymbol{}
	order := make([]string, 0, len(ctx.CandidateCoins)+len(ctx.Positions))
	for _, coin := range ctx.CandidateCoins {
		if len(order) >= 10 {
			break
		}
		symbol := market.Normalize(coin.Symbol)
		if symbol == "" {
			continue
		}
		if !items[symbol].Selected {
			order = append(order, symbol)
		}
		sources := append([]string(nil), coin.Sources...)
		if len(sources) == 0 {
			sources = []string{"candidate"}
		}
		items[symbol] = chanlunStrategySymbol{
			Symbol:   symbol,
			Sources:  mergeSources(items[symbol].Sources, sources...),
			Selected: true,
		}
	}
	for _, pos := range ctx.Positions {
		symbol := market.Normalize(pos.Symbol)
		if symbol == "" {
			continue
		}
		item := items[symbol]
		if item.Symbol == "" {
			order = append(order, symbol)
			item.Symbol = symbol
			item.Selected = true
		}
		item.HasPosition = true
		item.Sources = mergeSources(item.Sources, "position")
		items[symbol] = item
	}
	out := make([]chanlunStrategySymbol, 0, len(order))
	for _, symbol := range order {
		if item := items[symbol]; item.Symbol != "" {
			out = append(out, item)
		}
	}
	return out
}

func strategySymbolNames(universe []chanlunStrategySymbol) []string {
	out := make([]string, 0, len(universe))
	for _, item := range universe {
		symbol := market.Normalize(item.Symbol)
		if symbol != "" {
			out = append(out, symbol)
		}
	}
	return out
}

func cloneChanlunV2Report(report *chanlun.SignalReport) *chanlun.SignalReport {
	if report == nil {
		return nil
	}
	copied := *report
	copied.Signals = append([]chanlun.ChanlunSignal(nil), report.Signals...)
	copied.SignalMarkers = append([]chanlun.SignalMarker(nil), report.SignalMarkers...)
	copied.LatestDiagnostics = cloneMap(report.LatestDiagnostics)
	return &copied
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func v2SignalID(symbol, timeframe string, sig Signal) string {
	return fmt.Sprintf("chanlun_v2:%s:%s:%s:%d", market.Normalize(symbol), timeframe, sig.SignalType, normalizeV2EpochMillis(sig.Timestamp))
}

func v2StructureKey(symbol, timeframe string, sig Signal) string {
	centerID := signalCenterID(sig)
	if centerID == "" {
		centerID = "no_center"
	}
	return fmt.Sprintf("chanlun_v2:%s:%s:%s:%s", market.Normalize(symbol), timeframe, centerID, sig.SignalType)
}

func signalCenterID(sig Signal) string {
	if sig.CenterID == nil {
		return ""
	}
	return fmt.Sprintf("%d", *sig.CenterID)
}

func signalActionHint(sig Signal) string {
	if strings.HasPrefix(sig.SignalType, "buy") || strings.HasPrefix(sig.SignalType, "quasi_buy") || sig.Direction == "long" {
		return "open_long"
	}
	if strings.HasPrefix(sig.SignalType, "sell") || strings.HasPrefix(sig.SignalType, "quasi_sell") || sig.Direction == "short" {
		return "open_short"
	}
	return ""
}

func normalizeV2EpochMillis(value int64) int64 {
	if value <= 0 {
		return 0
	}
	abs := math.Abs(float64(value))
	switch {
	case abs < 1e11:
		return value * 1000
	case abs < 1e14:
		return value
	case abs < 1e17:
		return value / 1000
	default:
		return value / 1_000_000
	}
}

func timeFromEpochMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value)
}

func normalizeV2StringSet(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.ToLower(strings.TrimSpace(part))
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	return out
}

func stringSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func metadataInt64(metadata map[string]any, key string) int64 {
	if metadata == nil {
		return 0
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mergeSources(existing []string, values ...string) []string {
	out := append([]string(nil), existing...)
	seen := map[string]bool{}
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
