package chanlun

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DisplayCategoryStructureBackground = "structure_background"
	DisplayCategoryPreviewWatch        = "preview_watch"
	DisplayCategoryEntryTrigger        = "entry_trigger"
	DisplayCategoryTradeAction         = "trade_action"
	DisplayCategoryInvalidRejected     = "invalid_rejected"
	DisplayCategoryPositionManagement  = "position_management"
)

func canonicalizeSignalMarker(marker SignalMarker) SignalMarker {
	if marker.StructureKey == "" {
		marker.StructureKey = fallbackStructureKey(marker)
	}
	if marker.ParentStructureKey == "" && marker.EntryTriggerID != "" {
		marker.ParentStructureKey = marker.StructureKey
	}
	if marker.LifecycleKey == "" {
		marker.LifecycleKey = MarkerLifecycleKey(marker)
	}
	if marker.DisplayCategory == "" {
		marker.DisplayCategory = markerDisplayCategory(marker)
	}
	if marker.DisplayPriority == 0 {
		marker.DisplayPriority = markerDisplayPriority(marker)
	}
	if marker.FirstSeenCloseTime == 0 {
		marker.FirstSeenCloseTime = firstPositiveInt64(marker.SignalCloseTime, marker.CloseTime, marker.DisplayCloseTime, marker.DecisionCloseTime)
	}
	if marker.LastSeenCloseTime == 0 {
		marker.LastSeenCloseTime = firstPositiveInt64(marker.DecisionCloseTime, marker.DisplayCloseTime, marker.CloseTime, marker.SignalCloseTime)
	}
	if marker.LastUpdatedAt == 0 {
		marker.LastUpdatedAt = firstPositiveInt64(marker.DecisionCloseTime, marker.DisplayCloseTime, marker.CloseTime, marker.SignalCloseTime)
	}
	return marker
}

func MarkerLifecycleKey(marker SignalMarker) string {
	if marker.LifecycleKey != "" {
		return marker.LifecycleKey
	}
	if marker.EntryTriggerID != "" && !markerHasTradeAction(marker) && marker.SourceLayer == "entry_trigger" {
		return "entry_trigger:" + marker.EntryTriggerID
	}
	if markerHasTradeAction(marker) {
		intent := firstNonEmptyString(marker.TradeIntent, marker.FinalAction, marker.Action)
		if intent == "" {
			intent = strings.ToLower(strings.TrimSpace(marker.Status))
		}
		return "action:" + marker.SignalID + ":" + intent
	}
	if marker.SourceLayer == "preview_signal" {
		structureKey := firstNonEmptyString(marker.StructureKey, fallbackStructureKey(marker))
		phase := firstNonEmptyString(marker.PreviewPhase, "preview")
		anchor := firstPositiveInt64(marker.DisplayCloseTime, marker.DecisionCloseTime, marker.CloseTime, marker.SignalCloseTime)
		tradeClose := parentTradeCandleClose(anchor, marker.Timeframe)
		return "preview:" + structureKey + ":" + phase + ":" + strconv.FormatInt(tradeClose, 10)
	}
	if marker.SourceLayer == "position_management" {
		return "position:" + marker.SignalID + ":" + firstNonEmptyString(marker.FinalAction, marker.Action, marker.Status)
	}
	if marker.StructureKey != "" {
		return "structure:" + marker.StructureKey
	}
	return marker.SignalID + "|" + marker.Timeframe + "|" + strconv.FormatInt(marker.CloseTime, 10)
}

func fallbackStructureKey(marker SignalMarker) string {
	parts := []string{
		"legacy",
		strings.ToUpper(strings.TrimSpace(marker.Symbol)),
		strings.ToLower(strings.TrimSpace(marker.Direction)),
		strings.ToLower(strings.TrimSpace(marker.SignalType)),
		strings.ToLower(strings.TrimSpace(marker.Timeframe)),
		strconv.FormatInt(firstPositiveInt64(marker.SignalCloseTime, marker.CloseTime), 10),
	}
	return strings.Join(parts, "|")
}

func markerDisplayCategory(marker SignalMarker) string {
	if marker.SourceLayer == "position_management" {
		return DisplayCategoryPositionManagement
	}
	if marker.SourceLayer == "preview_signal" {
		return DisplayCategoryPreviewWatch
	}
	if markerHasTradeAction(marker) {
		return DisplayCategoryTradeAction
	}
	if marker.SourceLayer == "entry_trigger" || marker.EntryTriggerID != "" {
		return DisplayCategoryEntryTrigger
	}
	switch strings.ToLower(strings.TrimSpace(marker.Status)) {
	case "invalidated", "rejected", "failed", "expired", "deduped", "suppressed":
		return DisplayCategoryInvalidRejected
	default:
		return DisplayCategoryStructureBackground
	}
}

func markerDisplayPriority(marker SignalMarker) int {
	if marker.SourceLayer == "position_management" {
		return 70
	}
	if markerHasTradeAction(marker) {
		switch strings.ToLower(strings.TrimSpace(marker.Status)) {
		case "executed", "failed":
			return 100
		case "rejected":
			return 80
		default:
			return 85
		}
	}
	if marker.SourceLayer == "entry_trigger" || marker.EntryTriggerID != "" {
		if strings.EqualFold(marker.Status, "ready") {
			return 90
		}
		return 75
	}
	if marker.SourceLayer == "preview_signal" {
		return 40
	}
	switch strings.ToLower(strings.TrimSpace(marker.Status)) {
	case "background":
		return 60
	case "invalidated", "rejected", "expired", "suppressed":
		return 55
	default:
		return 65
	}
}

func markerHasTradeAction(marker SignalMarker) bool {
	if strings.TrimSpace(marker.Action) != "" || strings.TrimSpace(marker.FinalAction) != "" || strings.TrimSpace(marker.TradeIntent) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(marker.Status)) {
	case "executed", "failed":
		return true
	default:
		return false
	}
}

func parentTradeCandleClose(closeTime int64, timeframe string) int64 {
	if closeTime <= 0 {
		return 0
	}
	duration := timeframeDuration(timeframe)
	if duration <= 0 {
		duration = time.Hour
	}
	step := int64(duration / time.Millisecond)
	if step <= 0 {
		return closeTime
	}
	return ((closeTime + 1 + step - 1) / step * step) - 1
}

func buildSignalMarkerSummary(raw, returned []SignalMarker) SignalMarkerSummary {
	summary := SignalMarkerSummary{
		TotalRaw:      len(raw),
		TotalReturned: len(returned),
		ByCategory:    map[string]int{},
		ByStatus:      map[string]int{},
	}
	for _, marker := range raw {
		marker = canonicalizeSignalMarker(marker)
		summary.ByCategory[marker.DisplayCategory]++
		summary.ByStatus[strings.ToLower(strings.TrimSpace(marker.Status))]++
		if marker.HiddenByDefault {
			summary.HiddenByDefault++
			if marker.SourceLayer == "preview_signal" {
				summary.PreviewHidden++
			}
		}
		if marker.CollapsedCount > 0 {
			summary.CollapsedLifecycle += marker.CollapsedCount
			if isSuppressedRepeatMarker(marker) {
				summary.SuppressedRepeats += marker.CollapsedCount
			}
		}
	}
	summary.HiddenByDefault += maxInt(0, len(raw)-len(returned)-summary.HiddenByDefault)
	latencies := markerLatenciesHours(raw)
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
	if len(summary.ByCategory) == 0 {
		summary.ByCategory = nil
	}
	if len(summary.ByStatus) == 0 {
		summary.ByStatus = nil
	}
	return summary
}

func markerLatenciesHours(markers []SignalMarker) []float64 {
	var out []float64
	for _, marker := range markers {
		signalClose := firstPositiveInt64(marker.SignalCloseTime, marker.CloseTime)
		decisionClose := firstPositiveInt64(marker.DecisionCloseTime, marker.DisplayCloseTime)
		if signalClose <= 0 || decisionClose <= signalClose {
			continue
		}
		out = append(out, float64(decisionClose-signalClose)/float64(time.Hour/time.Millisecond))
	}
	return out
}

func isSuppressedRepeatMarker(marker SignalMarker) bool {
	status := strings.ToLower(strings.TrimSpace(marker.Status))
	reason := strings.ToLower(strings.TrimSpace(firstNonEmptyString(marker.ReasonCode, marker.EntryInvalidReason, marker.EntryWindowState)))
	return status == "invalidated" || status == "background" || status == "rejected" ||
		strings.Contains(reason, "target_already_crossed") ||
		strings.Contains(reason, "entry_window") ||
		strings.Contains(reason, "signal_expired") ||
		strings.Contains(reason, "fresh_entry_trigger")
}
