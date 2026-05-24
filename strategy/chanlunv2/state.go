package chanlunv2

import (
	"encoding/json"
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
