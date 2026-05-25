package chanlunv2

import (
	"encoding/json"
	"nofx/decision"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SignalLifecycleState struct {
	TraderID              string  `json:"trader_id"`
	Symbol                string  `json:"symbol"`
	ParentSignalID        string  `json:"parent_signal_id"`
	ParentSignalType      string  `json:"parent_signal_type"`
	ParentSignalCloseTime int64   `json:"parent_signal_close_time"`
	Direction             string  `json:"direction"`
	Status                string  `json:"status"`
	EntryTriggerID        string  `json:"entry_trigger_id,omitempty"`
	EntryTriggerType      string  `json:"entry_trigger_type,omitempty"`
	EntryTriggerCloseTime int64   `json:"entry_trigger_close_time,omitempty"`
	LastEvaluationTime    int64   `json:"last_evaluation_time,omitempty"`
	TerminalReasonCode    string  `json:"terminal_reason_code,omitempty"`
	RemainingNetRR        float64 `json:"remaining_net_rr,omitempty"`
	UpdatedAt             int64   `json:"updated_at"`
}

type PositionManagementState struct {
	TraderID             string  `json:"trader_id"`
	Symbol               string  `json:"symbol"`
	Side                 string  `json:"side"`
	PeakFavorableR       float64 `json:"peak_favorable_r,omitempty"`
	LastPartialCloseAt   int64   `json:"last_partial_close_at,omitempty"`
	PartialCloseCount    int     `json:"partial_close_count,omitempty"`
	TotalPartialClosePct float64 `json:"total_partial_close_pct,omitempty"`
	UpdatedAt            int64   `json:"updated_at"`
}

type SignalExecutionState struct {
	TraderID        string `json:"trader_id"`
	Symbol          string `json:"symbol"`
	SignalID        string `json:"signal_id"`
	SignalType      string `json:"signal_type,omitempty"`
	Action          string `json:"action,omitempty"`
	Status          string `json:"status"`
	ReasonCode      string `json:"reason_code,omitempty"`
	FirstSeenAt     int64  `json:"first_seen_at,omitempty"`
	LastSeenAt      int64  `json:"last_seen_at,omitempty"`
	SuppressedCount int    `json:"suppressed_count,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
}

func lifecycleParentKey(traderID, symbol, parentSignalID string) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		normalizeSymbolForState(symbol),
		strings.TrimSpace(parentSignalID),
	}, "|")
}

func lifecycleTriggerKey(traderID, symbol, entryTriggerID string) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		normalizeSymbolForState(symbol),
		"entry_trigger",
		strings.TrimSpace(entryTriggerID),
	}, "|")
}

func positionManagementKey(traderID, symbol, side string) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		normalizeSymbolForState(symbol),
		strings.ToLower(strings.TrimSpace(side)),
	}, "|")
}

func signalExecutionKey(traderID, symbol, signalID string) string {
	return strings.Join([]string{
		strings.TrimSpace(traderID),
		normalizeSymbolForState(symbol),
		strings.TrimSpace(signalID),
	}, "|")
}

func normalizeSymbolForState(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func (e *Engine) lifecyclePersistencePath(traderID string) string {
	if e == nil {
		return ""
	}
	if path := strings.TrimSpace(e.Config.LifecycleStatePath); path != "" {
		if strings.Contains(path, "{trader_id}") {
			return strings.ReplaceAll(path, "{trader_id}", traderID)
		}
		return path
	}
	return ""
}

func (e *Engine) executionPersistencePath(traderID string) string {
	path := e.lifecyclePersistencePath(traderID)
	if path == "" {
		return ""
	}
	if strings.HasSuffix(path, ".json") {
		return strings.TrimSuffix(path, ".json") + ".executions.json"
	}
	return path + ".executions.json"
}

func (e *Engine) ensureLifecycleLoaded(traderID string) {
	if e == nil {
		return
	}
	path := e.lifecyclePersistencePath(traderID)
	if path == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lifecycleLoaded == nil {
		e.lifecycleLoaded = map[string]bool{}
	}
	if e.lifecycleLoaded[path] {
		return
	}
	e.lifecycleLoaded[path] = true
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var states map[string]SignalLifecycleState
	if err := json.Unmarshal(data, &states); err != nil {
		return
	}
	if e.lifecycleStates == nil {
		e.lifecycleStates = map[string]SignalLifecycleState{}
	}
	for key, state := range states {
		if strings.TrimSpace(state.TraderID) == "" || state.TraderID == traderID {
			e.lifecycleStates[key] = state
		}
	}
}

func (e *Engine) ensureExecutionLoaded(traderID string) {
	if e == nil {
		return
	}
	path := e.executionPersistencePath(traderID)
	if path == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.executionLoaded == nil {
		e.executionLoaded = map[string]bool{}
	}
	if e.executionLoaded[path] {
		return
	}
	e.executionLoaded[path] = true
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var states map[string]SignalExecutionState
	if err := json.Unmarshal(data, &states); err != nil {
		return
	}
	if e.signalExecutionStates == nil {
		e.signalExecutionStates = map[string]SignalExecutionState{}
	}
	for key, state := range states {
		if strings.TrimSpace(state.TraderID) == "" || state.TraderID == traderID {
			e.signalExecutionStates[key] = state
		}
	}
}

func (e *Engine) persistLifecycleLocked(traderID string) {
	path := e.lifecyclePersistencePath(traderID)
	if path == "" || e == nil {
		return
	}
	states := map[string]SignalLifecycleState{}
	for key, state := range e.lifecycleStates {
		if state.TraderID == traderID {
			states[key] = state
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (e *Engine) persistExecutionLocked(traderID string) {
	path := e.executionPersistencePath(traderID)
	if path == "" || e == nil {
		return
	}
	states := map[string]SignalExecutionState{}
	for key, state := range e.signalExecutionStates {
		if state.TraderID == traderID {
			states[key] = state
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (e *Engine) upsertLifecycle(state SignalLifecycleState) {
	if e == nil || strings.TrimSpace(state.ParentSignalID) == "" {
		return
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = time.Now().UnixMilli()
	}
	key := lifecycleParentKey(state.TraderID, state.Symbol, state.ParentSignalID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lifecycleStates == nil {
		e.lifecycleStates = map[string]SignalLifecycleState{}
	}
	e.lifecycleStates[key] = state
	if state.EntryTriggerID != "" {
		e.lifecycleStates[lifecycleTriggerKey(state.TraderID, state.Symbol, state.EntryTriggerID)] = state
	}
	e.persistLifecycleLocked(state.TraderID)
}

func (e *Engine) lifecycleState(traderID, symbol, parentSignalID string) (SignalLifecycleState, bool) {
	if e == nil {
		return SignalLifecycleState{}, false
	}
	e.ensureLifecycleLoaded(traderID)
	key := lifecycleParentKey(traderID, symbol, parentSignalID)
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.lifecycleStates[key]
	return state, ok
}

func (e *Engine) isTerminalParentLifecycle(traderID, symbol, parentSignalID string) bool {
	state, ok := e.lifecycleState(traderID, symbol, parentSignalID)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(state.Status)), "terminal_")
}

func (e *Engine) hasTerminalSignal(traderID, symbol, signalID string) bool {
	if e == nil || strings.TrimSpace(signalID) == "" {
		return false
	}
	e.ensureExecutionLoaded(traderID)
	key := signalExecutionKey(traderID, symbol, signalID)
	e.mu.RLock()
	defer e.mu.RUnlock()
	state, ok := e.signalExecutionStates[key]
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(state.Status)) {
	case "executed", "terminal_rejected":
		return true
	default:
		return false
	}
}

func (e *Engine) markSignalExecuted(traderID string, d decision.Decision, executedAt time.Time) {
	if e == nil || strings.TrimSpace(d.SignalID) == "" {
		return
	}
	if executedAt.IsZero() {
		executedAt = time.Now()
	}
	e.upsertSignalExecutionState(traderID, d, "executed", "", executedAt)
}

func (e *Engine) markSignalTerminalRejected(traderID string, d decision.Decision, reasonCode string, now time.Time) {
	if e == nil || strings.TrimSpace(d.SignalID) == "" || strings.TrimSpace(reasonCode) == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	e.upsertSignalExecutionState(traderID, d, "terminal_rejected", reasonCode, now)
}

func (e *Engine) upsertSignalExecutionState(traderID string, d decision.Decision, status, reasonCode string, now time.Time) {
	if e == nil || strings.TrimSpace(d.SignalID) == "" {
		return
	}
	if traderID == "" {
		traderID = metadataString(d.StrategyMetadata, "trader_id")
	}
	e.ensureExecutionLoaded(traderID)
	symbol := normalizeSymbolForState(d.Symbol)
	key := signalExecutionKey(traderID, symbol, d.SignalID)
	nowMs := now.UnixMilli()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.signalExecutionStates == nil {
		e.signalExecutionStates = map[string]SignalExecutionState{}
	}
	state := e.signalExecutionStates[key]
	if strings.EqualFold(state.Status, "executed") && status != "executed" {
		return
	}
	if state.FirstSeenAt == 0 {
		state.FirstSeenAt = nowMs
	}
	state.TraderID = traderID
	state.Symbol = symbol
	state.SignalID = d.SignalID
	state.SignalType = firstNonEmptyString(d.SignalType, state.SignalType)
	state.Action = firstNonEmptyString(d.Action, state.Action)
	state.Status = status
	state.ReasonCode = firstNonEmptyString(reasonCode, state.ReasonCode)
	state.LastSeenAt = nowMs
	state.UpdatedAt = nowMs
	e.signalExecutionStates[key] = state
	e.persistExecutionLocked(traderID)
}

func (e *Engine) suppressKnownTerminalSignal(traderID string, d decision.Decision) (bool, string) {
	if e == nil || strings.TrimSpace(d.SignalID) == "" {
		return false, ""
	}
	if traderID == "" {
		traderID = metadataString(d.StrategyMetadata, "trader_id")
	}
	e.ensureExecutionLoaded(traderID)
	symbol := normalizeSymbolForState(d.Symbol)
	key := signalExecutionKey(traderID, symbol, d.SignalID)
	now := time.Now().UnixMilli()
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.signalExecutionStates[key]
	if !ok {
		return false, ""
	}
	switch strings.ToLower(strings.TrimSpace(state.Status)) {
	case "executed", "terminal_rejected":
	default:
		return false, ""
	}
	state.LastSeenAt = now
	state.SuppressedCount++
	state.UpdatedAt = now
	if d.Action != "" {
		state.Action = d.Action
	}
	if d.SignalType != "" {
		state.SignalType = d.SignalType
	}
	e.signalExecutionStates[key] = state
	e.persistExecutionLocked(traderID)
	label := firstNonEmptyString(state.SignalType, d.SignalType, d.Action, "signal")
	reasonCode := firstNonEmptyString(state.ReasonCode, state.Status)
	return true, symbol + " " + label + " 终态信号已静默: " + reasonCode
}

func (e *Engine) terminalSuppressionSnapshot(traderID string, sampleLimit int) (int, map[string]int, []string) {
	if e == nil {
		return 0, nil, nil
	}
	if sampleLimit <= 0 {
		sampleLimit = 3
	}
	e.ensureExecutionLoaded(traderID)
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	reasons := map[string]int{}
	var samples []string
	for _, state := range e.signalExecutionStates {
		if state.TraderID != traderID || state.SuppressedCount <= 0 {
			continue
		}
		total += state.SuppressedCount
		reason := firstNonEmptyString(state.ReasonCode, state.Status, "unknown")
		reasons[reason] += state.SuppressedCount
		if len(samples) < sampleLimit {
			samples = append(samples, state.Symbol+" "+firstNonEmptyString(state.SignalType, state.Action, "signal")+" "+reason)
		}
	}
	for _, record := range e.staleSuppressions {
		if record.TraderID != traderID || record.SuppressedCount <= 0 {
			continue
		}
		total += record.SuppressedCount
		reason := firstNonEmptyString(record.ReasonCode, "freshness_gate.terminal")
		reasons[reason] += record.SuppressedCount
		if len(samples) < sampleLimit {
			samples = append(samples, record.Symbol+" "+reason)
		}
	}
	if len(reasons) == 0 {
		reasons = nil
	}
	return total, reasons, samples
}
