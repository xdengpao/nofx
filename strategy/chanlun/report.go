package chanlun

import (
	"sort"
	"strings"
)

func applySignalReportOptions(report *SignalReport, opts SignalReportOptions) {
	if report == nil {
		return
	}
	opts = normalizeSignalReportOptions(opts)
	raw := canonicalizeReportMarkers(report.SignalMarkers)
	filtered := filterReportMarkers(raw, opts)
	var returned []SignalMarker
	if opts.View == "audit" {
		returned = limitReportMarkers(filtered, opts.Limit)
	} else {
		returned = defaultReportMarkers(filtered, opts)
	}
	report.SignalMarkers = returned
	report.View = opts.View
	report.Filters = SignalReportFilters{
		Layers:   append([]string(nil), opts.Layers...),
		Statuses: append([]string(nil), opts.Statuses...),
		From:     opts.From,
		To:       opts.To,
		Limit:    opts.Limit,
	}
	report.MarkerSummary = buildSignalMarkerSummary(markHiddenReportMarkers(filtered, returned), returned)
	if report.LatestDiagnostics == nil {
		report.LatestDiagnostics = map[string]any{}
	}
	report.LatestDiagnostics["marker_summary"] = report.MarkerSummary
}

func normalizeSignalReportOptions(opts SignalReportOptions) SignalReportOptions {
	view := strings.ToLower(strings.TrimSpace(opts.View))
	if view == "" {
		view = "default"
	}
	if view != "audit" {
		view = "default"
	}
	opts.View = view
	opts.Layers = normalizeStringSet(opts.Layers)
	opts.Statuses = normalizeStringSet(opts.Statuses)
	if opts.Limit < 0 {
		opts.Limit = 0
	}
	if opts.Limit == 0 && opts.View == "audit" {
		opts.Limit = maxRecentSignalMarkers
	}
	return opts
}

func normalizeStringSet(values []string) []string {
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

func canonicalizeReportMarkers(markers []SignalMarker) []SignalMarker {
	out := make([]SignalMarker, 0, len(markers))
	for _, marker := range markers {
		if marker.SignalID == "" {
			continue
		}
		out = append(out, canonicalizeSignalMarker(marker))
	}
	return out
}

func filterReportMarkers(markers []SignalMarker, opts SignalReportOptions) []SignalMarker {
	layerSet := sliceSet(opts.Layers)
	statusSet := sliceSet(opts.Statuses)
	out := make([]SignalMarker, 0, len(markers))
	for _, marker := range markers {
		if len(layerSet) > 0 && !layerSet[strings.ToLower(strings.TrimSpace(marker.SourceLayer))] && !layerSet[strings.ToLower(strings.TrimSpace(marker.DisplayCategory))] {
			continue
		}
		if len(statusSet) > 0 && !statusSet[strings.ToLower(strings.TrimSpace(marker.Status))] {
			continue
		}
		t := markerReportTime(marker)
		if opts.From > 0 && t > 0 && t < opts.From {
			continue
		}
		if opts.To > 0 && t > 0 && t > opts.To {
			continue
		}
		out = append(out, marker)
	}
	sortReportMarkers(out)
	return out
}

func defaultReportMarkers(markers []SignalMarker, opts SignalReportOptions) []SignalMarker {
	var compacted []SignalMarker
	for _, marker := range markers {
		compacted = upsertSignalMarker(compacted, marker)
	}
	sortReportMarkers(compacted)
	latestPreviewKey := ""
	latestPreviewTime := int64(0)
	for _, marker := range compacted {
		if marker.SourceLayer != "preview_signal" {
			continue
		}
		t := markerReportTime(marker)
		if t >= latestPreviewTime {
			latestPreviewTime = t
			latestPreviewKey = marker.LifecycleKey
		}
	}
	out := make([]SignalMarker, 0, len(compacted))
	for _, marker := range compacted {
		if marker.SourceLayer == "preview_signal" && marker.LifecycleKey != latestPreviewKey {
			continue
		}
		if marker.SourceLayer == "preview_signal" && hasHigherPriorityContext(compacted, markerReportTime(marker)) {
			continue
		}
		out = append(out, marker)
	}
	return limitReportMarkers(out, opts.Limit)
}

func hasHigherPriorityContext(markers []SignalMarker, previewTime int64) bool {
	for _, marker := range markers {
		if marker.SourceLayer == "preview_signal" {
			continue
		}
		if marker.DisplayPriority >= 55 && markerReportTime(marker) >= previewTime {
			return true
		}
	}
	return false
}

func limitReportMarkers(markers []SignalMarker, limit int) []SignalMarker {
	sortReportMarkers(markers)
	if limit > 0 && len(markers) > limit {
		markers = markers[len(markers)-limit:]
	}
	return append([]SignalMarker(nil), markers...)
}

func markHiddenReportMarkers(filtered, returned []SignalMarker) []SignalMarker {
	visible := map[string]bool{}
	for _, marker := range returned {
		visible[signalMarkerKey(marker)] = true
	}
	out := make([]SignalMarker, 0, len(filtered))
	for _, marker := range filtered {
		if !visible[signalMarkerKey(marker)] {
			marker.HiddenByDefault = true
		}
		out = append(out, marker)
	}
	return out
}

func sortReportMarkers(markers []SignalMarker) {
	sort.SliceStable(markers, func(i, j int) bool {
		ti := markerReportTime(markers[i])
		tj := markerReportTime(markers[j])
		if ti != tj {
			return ti < tj
		}
		if markers[i].DisplayPriority != markers[j].DisplayPriority {
			return markers[i].DisplayPriority < markers[j].DisplayPriority
		}
		return signalMarkerKey(markers[i]) < signalMarkerKey(markers[j])
	})
}

func markerReportTime(marker SignalMarker) int64 {
	return firstPositiveInt64(marker.DisplayCloseTime, marker.DecisionCloseTime, marker.CloseTime, marker.SignalCloseTime)
}

func sliceSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}
