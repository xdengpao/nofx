package optimize

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"nofx/logger"
)

func ResolveCanonicalLogFields(action logger.DecisionAction) (CanonicalLogFields, []FieldResolutionError) {
	metadata := action.StrategyMetadata
	explanation := explanationDetails(action.Explanation)
	fields := CanonicalLogFields{
		StructureKey:      pickString("", metadata, explanation, "structure_key"),
		SignalID:          pickString(action.SignalID, metadata, explanation, "signal_id"),
		ParentSignalID:    pickString("", metadata, explanation, "parent_signal_id"),
		EntryTriggerID:    pickString("", metadata, explanation, "entry_trigger_id"),
		SourceLayer:       pickString("", metadata, explanation, "source_layer"),
		SignalType:        pickString(action.SignalType, metadata, explanation, "signal_type"),
		AnalysisTimeframe: pickString(action.SignalTimeframe, metadata, explanation, "analysis_timeframe", "signal_timeframe"),
		TriggerTimeframe:  pickString("", metadata, explanation, "trigger_timeframe", "entry_trigger_timeframe"),
		StructureTarget:   pickFloat(action.StructureTarget, metadata, explanation, "structure_target"),
		SignalCloseTime:   pickInt64(action.SignalCloseTime, metadata, explanation, "signal_close_time"),
		DecisionCloseTime: pickInt64(action.DecisionCloseTime, metadata, explanation, "decision_close_time"),
		AgeCandles:        int(pickInt64(0, metadata, explanation, "age_candles")),
		FreshnessState:    pickString("", metadata, explanation, "freshness_state"),
		ReasonCode:        pickString("", metadata, explanation, "reason_code"),
		ConfigHash:        pickString(action.ConfigHash, metadata, explanation, "config_hash"),
		StrategyVersion:   pickString(action.StrategyVersion, metadata, explanation, "strategy_version"),
	}
	var errs []FieldResolutionError
	required := map[string]bool{
		"structure_key":       fields.StructureKey != "",
		"signal_id":           fields.SignalID != "",
		"parent_signal_id":    fields.ParentSignalID != "",
		"entry_trigger_id":    fields.EntryTriggerID != "",
		"source_layer":        fields.SourceLayer != "",
		"signal_type":         fields.SignalType != "",
		"analysis_timeframe":  fields.AnalysisTimeframe != "",
		"trigger_timeframe":   fields.TriggerTimeframe != "",
		"structure_target":    fields.StructureTarget != 0,
		"signal_close_time":   fields.SignalCloseTime != 0,
		"decision_close_time": fields.DecisionCloseTime != 0,
		"age_candles":         fields.AgeCandles != 0,
		"freshness_state":     fields.FreshnessState != "",
		"reason_code":         fields.ReasonCode != "",
		"config_hash":         fields.ConfigHash != "",
		"strategy_version":    fields.StrategyVersion != "",
	}
	for field, ok := range required {
		if !ok {
			errs = append(errs, FieldResolutionError{Field: field, Reason: "missing"})
		}
	}
	return fields, errs
}

func explanationDetails(value any) map[string]any {
	if value == nil {
		return nil
	}
	var raw map[string]any
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if details, ok := raw["details"].(map[string]any); ok {
		return details
	}
	return raw
}

func pickString(top string, metadata map[string]any, explanation map[string]any, keys ...string) string {
	if strings.TrimSpace(top) != "" {
		return top
	}
	for _, values := range []map[string]any{metadata, explanation} {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if s := anyToString(value); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func pickFloat(top float64, metadata map[string]any, explanation map[string]any, keys ...string) float64 {
	if top != 0 {
		return top
	}
	for _, values := range []map[string]any{metadata, explanation} {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if f, ok := anyToFloat(value); ok {
					return f
				}
			}
		}
	}
	return 0
}

func pickInt64(top int64, metadata map[string]any, explanation map[string]any, keys ...string) int64 {
	if top != 0 {
		return top
	}
	for _, values := range []map[string]any{metadata, explanation} {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if i, ok := anyToInt64(value); ok {
					return i
				}
			}
		}
	}
	return 0
}

func anyToString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func anyToFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func anyToInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
