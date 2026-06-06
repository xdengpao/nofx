package chanlun

import (
	"encoding/json"
	"log"
	"nofx/decision"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StateStore struct {
	path  string
	Clock func() time.Time
	mu    sync.Mutex
	data  ProgrammaticStateFile
}

const maxRecentSignalMarkers = 200

type ProgrammaticStateFile struct {
	Version   int                                `json:"version"`
	UpdatedAt time.Time                          `json:"updated_at"`
	Traders   map[string]ProgrammaticTraderState `json:"traders"`
}

type ProgrammaticTraderState struct {
	Symbols map[string]ProgrammaticSymbolState `json:"symbols"`
	Loosen  LoosenState                        `json:"loosen,omitempty"`
}

type ProgrammaticSymbolState struct {
	LastStructureHash       string                               `json:"last_structure_hash,omitempty"`
	LastAnalyzedClosedKline map[string]int64                     `json:"last_analyzed_closed_kline,omitempty"`
	ConfirmedSignals        map[string]StoredSignal              `json:"confirmed_signals,omitempty"`
	ExecutedSignals         map[string]SignalExec                `json:"executed_signals,omitempty"`
	SuppressedSignals       map[string]SignalSuppression         `json:"suppressed_signals,omitempty"`
	LifecycleTerminations   map[string]LifecycleTermination      `json:"lifecycle_terminations,omitempty"`
	ConfidenceSamples       []ConfidenceSample                   `json:"confidence_samples,omitempty"`
	SignalLifecycles        map[string]SignalLifecycle           `json:"signal_lifecycles,omitempty"`
	AddCountBySide          map[string]int                       `json:"add_count_by_side,omitempty"`
	ShortTradeState         *ShortTradeState                     `json:"short_trade_state,omitempty"`
	PositionStates          map[string]ProgrammaticPositionState `json:"position_states,omitempty"`
	RecentSignalMarkers     []SignalMarker                       `json:"recent_signal_markers,omitempty"`
}

type ProgrammaticPositionState struct {
	Side                   string                 `json:"side"`
	PeakPrice              float64                `json:"peak_price,omitempty"`
	PeakR                  float64                `json:"peak_r,omitempty"`
	LastBreakevenSignalID  string                 `json:"last_breakeven_signal_id,omitempty"`
	LastDrawdownSignalID   string                 `json:"last_drawdown_signal_id,omitempty"`
	LastStructureSignalID  string                 `json:"last_structure_signal_id,omitempty"`
	LastShortTradeSignalID string                 `json:"last_short_trade_signal_id,omitempty"`
	LastManagedAt          time.Time              `json:"last_managed_at,omitempty"`
	PartialCloseGuard      PartialCloseGuardState `json:"partial_close_guard,omitempty"`
}

type PartialCloseGuardState struct {
	LastPartialCloseAt        time.Time `json:"last_partial_close_at,omitempty"`
	LastPartialCloseRule      string    `json:"last_partial_close_rule,omitempty"`
	LastPartialCloseSignalID  string    `json:"last_partial_close_signal_id,omitempty"`
	LastPartialClosePct       float64   `json:"last_partial_close_pct,omitempty"`
	PartialCloseCount         int       `json:"partial_close_count,omitempty"`
	TotalPartialClosePct      float64   `json:"total_partial_close_pct,omitempty"`
	InitialTrackedQuantity    float64   `json:"initial_tracked_quantity,omitempty"`
	InitialTrackedValueUSD    float64   `json:"initial_tracked_value_usd,omitempty"`
	LastKnownQuantity         float64   `json:"last_known_quantity,omitempty"`
	TotalPartialCloseQuantity float64   `json:"total_partial_close_quantity,omitempty"`
	QuantityEstimated         bool      `json:"quantity_estimated,omitempty"`

	LastDrawdownPeakPrice     float64 `json:"last_drawdown_peak_price,omitempty"`
	LastDrawdownPeakPnLPct    float64 `json:"last_drawdown_peak_pnl_pct,omitempty"`
	LastDrawdownPeakR         float64 `json:"last_drawdown_peak_r,omitempty"`
	RequireNewPeakForDrawdown bool    `json:"require_new_peak_for_drawdown,omitempty"`
}

type StoredSignal struct {
	Signal    ChanlunSignal `json:"signal"`
	StoredAt  time.Time     `json:"stored_at"`
	Bootstrap bool          `json:"bootstrap,omitempty"`
}

type SignalExec struct {
	SignalID   string    `json:"signal_id"`
	Action     string    `json:"action"`
	ExecutedAt time.Time `json:"executed_at"`
}

type SignalSuppression struct {
	SignalID          string    `json:"signal_id"`
	StructureKey      string    `json:"structure_key,omitempty"`
	Action            string    `json:"action"`
	ReasonCode        string    `json:"reason_code"`
	SuppressedAt      time.Time `json:"suppressed_at"`
	LastSeenAt        time.Time `json:"last_seen_at,omitempty"`
	SeenCount         int       `json:"seen_count,omitempty"`
	ParentSignalID    string    `json:"parent_signal_id,omitempty"`
	EntryTriggerID    string    `json:"entry_trigger_id,omitempty"`
	EntryWindowState  string    `json:"entry_window_state,omitempty"`
	SignalCloseTime   int64     `json:"signal_close_time,omitempty"`
	DecisionCloseTime int64     `json:"decision_close_time,omitempty"`
	FreshnessState    string    `json:"freshness_state,omitempty"`
	CurrentPrice      float64   `json:"current_price,omitempty"`
	StopLoss          float64   `json:"stop_loss,omitempty"`
	TakeProfit        float64   `json:"take_profit,omitempty"`
	Severity          int       `json:"severity,omitempty"`
	PermanentSkip     bool      `json:"permanent_skip,omitempty"`
}

type LifecycleTermination struct {
	StructureKey string    `json:"structure_key"`
	ReasonCode   string    `json:"reason_code"`
	SignalID     string    `json:"signal_id,omitempty"`
	TerminatedAt time.Time `json:"terminated_at"`
	ExpiryAt     time.Time `json:"expiry_at,omitempty"`
}

type ConfidenceSample struct {
	SignalType string    `json:"signal_type"`
	Confidence int       `json:"confidence"`
	At         time.Time `json:"at"`
}

type LoosenState struct {
	Active    bool      `json:"active"`
	EnteredAt time.Time `json:"entered_at,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type SuppressionStats struct {
	TotalActive      int            `json:"total_active"`
	ByReason         map[string]int `json:"by_reason"`
	OldestAgeCandles int            `json:"oldest_age_candles"`
	PermanentSkip    int            `json:"permanent_skip"`
}

type SignalLifecycle struct {
	LifecycleKey       string       `json:"lifecycle_key"`
	StructureKey       string       `json:"structure_key,omitempty"`
	ParentStructureKey string       `json:"parent_structure_key,omitempty"`
	Marker             SignalMarker `json:"marker"`
	FirstSeenAt        time.Time    `json:"first_seen_at,omitempty"`
	LastSeenAt         time.Time    `json:"last_seen_at,omitempty"`
	SeenCount          int          `json:"seen_count,omitempty"`
	SuppressedCount    int          `json:"suppressed_count,omitempty"`
	CollapsedCount     int          `json:"collapsed_count,omitempty"`
	ConfigHashes       []string     `json:"config_hashes,omitempty"`
}

type ShortTradeState struct {
	ReducedSignalID string    `json:"reduced_signal_id,omitempty"`
	ReducedPct      float64   `json:"reduced_pct,omitempty"`
	CanReAdd        bool      `json:"can_re_add,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

func NewStateStore(path string) *StateStore {
	if path == "" {
		path = "data/programmatic_strategy_state.json"
	}
	store := &StateStore{path: path, Clock: time.Now}
	store.data = emptyStateFile()
	if err := store.Load(); err != nil {
		log.Printf("⚠️ 读取程序化策略状态失败，将使用空状态: %v", err)
	}
	return store
}

func (s *StateStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = emptyStateFile()
			return nil
		}
		return err
	}
	var state ProgrammaticStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		s.data = emptyStateFile()
		return err
	}
	if state.Traders == nil {
		state.Traders = map[string]ProgrammaticTraderState{}
	}
	s.data = state
	s.compactAllSignalMarkersLocked()
	return nil
}

func (s *StateStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.UpdatedAt = s.now()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *StateStore) SymbolState(traderID, symbol string) ProgrammaticSymbolState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureSymbolLocked(traderID, symbol)
}

func (s *StateStore) StoreConfirmedSignal(traderID, symbol string, signal ChanlunSignal, bootstrap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.ConfirmedSignals == nil {
		state.ConfirmedSignals = map[string]StoredSignal{}
	}
	state.ConfirmedSignals[signal.SignalID] = StoredSignal{Signal: signal, StoredAt: s.now(), Bootstrap: bootstrap}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) StoreConfidenceSample(traderID, symbol, signalType string, confidence int, at time.Time) {
	if traderID == "" || symbol == "" || signalType == "" || confidence <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if at.IsZero() {
		at = s.now()
	}
	state.ConfidenceSamples = append(state.ConfidenceSamples, ConfidenceSample{
		SignalType: strings.ToLower(strings.TrimSpace(signalType)),
		Confidence: confidence,
		At:         at,
	})
	cutoff := at.Add(-8 * 24 * time.Hour)
	kept := state.ConfidenceSamples[:0]
	for _, sample := range state.ConfidenceSamples {
		if sample.At.IsZero() || !sample.At.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	state.ConfidenceSamples = kept
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) ConfidenceWindow(traderID, signalType string, duration time.Duration) []int {
	if traderID == "" || signalType == "" || duration <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trader := s.data.Traders[traderID]
	if len(trader.Symbols) == 0 {
		return nil
	}
	cutoff := s.now().Add(-duration)
	normalizedType := strings.ToLower(strings.TrimSpace(signalType))
	var out []int
	for _, state := range trader.Symbols {
		for _, sample := range state.ConfidenceSamples {
			if strings.ToLower(strings.TrimSpace(sample.SignalType)) != normalizedType {
				continue
			}
			if !sample.At.IsZero() && sample.At.Before(cutoff) {
				continue
			}
			out = append(out, sample.Confidence)
		}
	}
	return out
}

func (s *StateStore) MarkExecuted(traderID, symbol, signalID, action string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.ExecutedSignals == nil {
		state.ExecutedSignals = map[string]SignalExec{}
	}
	if _, exists := state.ExecutedSignals[signalID]; exists && !hasRetryableSignalMarker(state.RecentSignalMarkers, signalID) {
		return false
	}
	state.ExecutedSignals[signalID] = SignalExec{SignalID: signalID, Action: action, ExecutedAt: s.now()}
	switch action {
	case "add_long":
		state.AddCountBySide[SideLong]++
	case "add_short":
		state.AddCountBySide[SideShort]++
	case "partial_close":
		state.ShortTradeState = &ShortTradeState{ReducedSignalID: signalID, CanReAdd: true, UpdatedAt: s.now()}
	}
	s.setSymbolLocked(traderID, symbol, state)
	return true
}

func (s *StateStore) HasExecutedSignal(traderID, symbol, signalID string) bool {
	if signalID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	_, exists := state.ExecutedSignals[signalID]
	return exists && !hasRetryableSignalMarker(state.RecentSignalMarkers, signalID)
}

func (s *StateStore) HasSuppressedSignal(traderID, symbol, signalID, action, reasonCode string) bool {
	if signalID == "" || action == "" || reasonCode == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	_, exists := state.SuppressedSignals[signalSuppressionKey(signalID, action, reasonCode)]
	return exists
}

func (s *StateStore) HasSuppressedStructure(traderID, symbol, structureKey, action, reasonCode string) bool {
	if structureKey == "" || action == "" || reasonCode == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	_, exists := state.SuppressedSignals[structureSuppressionKey(structureKey, action, reasonCode)]
	return exists
}

func (s *StateStore) SuppressedSignalForAction(traderID, symbol, signalID, action string) (SignalSuppression, bool) {
	if signalID == "" || action == "" {
		return SignalSuppression{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	prefix := signalID + "|" + strings.ToLower(strings.TrimSpace(action)) + "|"
	var latest SignalSuppression
	found := false
	for key, suppression := range state.SuppressedSignals {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if !found || suppression.SuppressedAt.After(latest.SuppressedAt) {
			latest = suppression
			found = true
		}
	}
	return latest, found
}

func (s *StateStore) StoreSignalSuppression(traderID, symbol string, suppression SignalSuppression) {
	if suppression.SignalID == "" || suppression.Action == "" || suppression.ReasonCode == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.SuppressedSignals == nil {
		state.SuppressedSignals = map[string]SignalSuppression{}
	}
	if suppression.SuppressedAt.IsZero() {
		suppression.SuppressedAt = s.now()
	}
	if suppression.LastSeenAt.IsZero() {
		suppression.LastSeenAt = suppression.SuppressedAt
	}
	if suppression.SeenCount <= 0 {
		suppression.SeenCount = 1
	}
	state.SuppressedSignals[signalSuppressionKey(suppression.SignalID, suppression.Action, suppression.ReasonCode)] = suppression
	if suppression.StructureKey != "" {
		key := structureSuppressionKey(suppression.StructureKey, suppression.Action, suppression.ReasonCode)
		if existing, ok := state.SuppressedSignals[key]; ok {
			suppression.SuppressedAt = existing.SuppressedAt
			suppression.SeenCount += existing.SeenCount
		}
		state.SuppressedSignals[key] = suppression
	}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) StoreStructureSuppression(traderID, symbol string, suppression SignalSuppression) {
	if suppression.StructureKey == "" || suppression.Action == "" || suppression.ReasonCode == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.SuppressedSignals == nil {
		state.SuppressedSignals = map[string]SignalSuppression{}
	}
	if suppression.SuppressedAt.IsZero() {
		suppression.SuppressedAt = s.now()
	}
	if suppression.LastSeenAt.IsZero() {
		suppression.LastSeenAt = suppression.SuppressedAt
	}
	if suppression.SeenCount <= 0 {
		suppression.SeenCount = 1
	}
	key := structureSuppressionKey(suppression.StructureKey, suppression.Action, suppression.ReasonCode)
	if existing, ok := state.SuppressedSignals[key]; ok {
		suppression.SuppressedAt = existing.SuppressedAt
		suppression.SeenCount += existing.SeenCount
	}
	state.SuppressedSignals[key] = suppression
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) FastSkipReason(traderID, symbol, structureKey string) (string, bool) {
	if traderID == "" || symbol == "" || structureKey == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if term, ok := state.LifecycleTerminations[structureKey]; ok {
		if !term.ExpiryAt.IsZero() && !term.ExpiryAt.After(s.now()) {
			delete(state.LifecycleTerminations, structureKey)
			s.setSymbolLocked(traderID, symbol, state)
			return "", false
		}
		reason := term.ReasonCode
		if reason == "" {
			reason = "lifecycle_terminated"
		}
		return reason, true
	}
	var latest SignalSuppression
	found := false
	for _, suppression := range state.SuppressedSignals {
		if suppression.StructureKey != structureKey {
			continue
		}
		if !found || suppression.LastSeenAt.After(latest.LastSeenAt) {
			latest = suppression
			found = true
		}
	}
	if !found {
		return "", false
	}
	reason := latest.ReasonCode
	if latest.PermanentSkip {
		reason = "permanent_skip"
	}
	if reason == "" {
		reason = "suppressed"
	}
	return reason, true
}

func (s *StateStore) FastSkipSet(traderID string) map[string]string {
	if traderID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trader := s.data.Traders[traderID]
	if len(trader.Symbols) == 0 {
		return nil
	}
	now := s.now()
	result := map[string]string{}
	changed := false
	for symbol, state := range trader.Symbols {
		for structureKey, term := range state.LifecycleTerminations {
			if !term.ExpiryAt.IsZero() && !term.ExpiryAt.After(now) {
				delete(state.LifecycleTerminations, structureKey)
				changed = true
				continue
			}
			reason := term.ReasonCode
			if reason == "" {
				reason = "lifecycle_terminated"
			}
			result[structureKey] = reason
		}
		for _, suppression := range state.SuppressedSignals {
			if suppression.StructureKey == "" {
				continue
			}
			reason := suppression.ReasonCode
			if suppression.PermanentSkip {
				reason = "permanent_skip"
			}
			if reason == "" {
				reason = "suppressed"
			}
			if _, exists := result[suppression.StructureKey]; !exists {
				result[suppression.StructureKey] = reason
			}
		}
		if changed {
			trader.Symbols[symbol] = state
		}
	}
	if changed {
		s.data.Traders[traderID] = trader
	}
	return result
}

func (s *StateStore) TerminateLifecycle(traderID, symbol, structureKey, reasonCode, signalID string, expiry time.Time) {
	if traderID == "" || symbol == "" || structureKey == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.LifecycleTerminations == nil {
		state.LifecycleTerminations = map[string]LifecycleTermination{}
	}
	state.LifecycleTerminations[structureKey] = LifecycleTermination{
		StructureKey: structureKey,
		ReasonCode:   reasonCode,
		SignalID:     signalID,
		TerminatedAt: s.now(),
		ExpiryAt:     expiry,
	}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) IsLifecycleTerminated(traderID, symbol, structureKey string) bool {
	if traderID == "" || symbol == "" || structureKey == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	term, ok := state.LifecycleTerminations[structureKey]
	if !ok {
		return false
	}
	if !term.ExpiryAt.IsZero() && !term.ExpiryAt.After(s.now()) {
		delete(state.LifecycleTerminations, structureKey)
		s.setSymbolLocked(traderID, symbol, state)
		return false
	}
	return true
}

func (s *StateStore) UpgradeSuppression(traderID, symbol, structureKey, newReason string) {
	if traderID == "" || symbol == "" || structureKey == "" || newReason == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	for key, suppression := range state.SuppressedSignals {
		if suppression.StructureKey != structureKey {
			continue
		}
		suppression.ReasonCode = strings.ToLower(strings.TrimSpace(newReason))
		if suppression.Severity < 1 {
			suppression.Severity = 1
		}
		suppression.LastSeenAt = s.now()
		suppression.SeenCount++
		state.SuppressedSignals[key] = suppression
	}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) MarkPermanentSkip(traderID, symbol, structureKey string) {
	if traderID == "" || symbol == "" || structureKey == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	for key, suppression := range state.SuppressedSignals {
		if suppression.StructureKey != structureKey {
			continue
		}
		suppression.PermanentSkip = true
		suppression.Severity = 2
		suppression.LastSeenAt = s.now()
		state.SuppressedSignals[key] = suppression
	}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) GCExpiredSuppressions(now time.Time) {
	if now.IsZero() {
		now = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-8 * 24 * time.Hour)
	for traderID, trader := range s.data.Traders {
		for symbol, state := range trader.Symbols {
			for structureKey, term := range state.LifecycleTerminations {
				if !term.ExpiryAt.IsZero() && !term.ExpiryAt.After(now) {
					delete(state.LifecycleTerminations, structureKey)
				}
			}
			for key, suppression := range state.SuppressedSignals {
				if suppression.PermanentSkip {
					continue
				}
				lastSeen := suppression.LastSeenAt
				if lastSeen.IsZero() {
					lastSeen = suppression.SuppressedAt
				}
				if !lastSeen.IsZero() && lastSeen.Before(cutoff) {
					delete(state.SuppressedSignals, key)
				}
			}
			trader.Symbols[symbol] = state
		}
		s.data.Traders[traderID] = trader
	}
}

func (s *StateStore) SuppressionStats(traderID string) SuppressionStats {
	stats := SuppressionStats{ByReason: map[string]int{}}
	if traderID == "" {
		return stats
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trader := s.data.Traders[traderID]
	now := s.now()
	for _, state := range trader.Symbols {
		for _, suppression := range state.SuppressedSignals {
			stats.TotalActive++
			reason := suppression.ReasonCode
			if reason == "" {
				reason = "suppressed"
			}
			stats.ByReason[reason]++
			if suppression.PermanentSkip {
				stats.PermanentSkip++
			}
			started := suppression.SuppressedAt
			if started.IsZero() {
				started = suppression.LastSeenAt
			}
			if !started.IsZero() {
				ageCandles := int(now.Sub(started) / time.Hour)
				if ageCandles > stats.OldestAgeCandles {
					stats.OldestAgeCandles = ageCandles
				}
			}
		}
	}
	return stats
}

func (s *StateStore) LoosenState(traderID string) LoosenState {
	if traderID == "" {
		return LoosenState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Traders[traderID].Loosen
}

func (s *StateStore) SetLoosenState(traderID string, state LoosenState) {
	if traderID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trader := s.data.Traders[traderID]
	if trader.Symbols == nil {
		trader.Symbols = map[string]ProgrammaticSymbolState{}
	}
	trader.Loosen = state
	s.data.Traders[traderID] = trader
}

func (s *StateStore) StructureSuppressionSeenCount(traderID, symbol, structureKey string) int {
	if traderID == "" || symbol == "" || structureKey == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	maxSeen := 0
	for _, suppression := range state.SuppressedSignals {
		if suppression.StructureKey != structureKey {
			continue
		}
		if suppression.SeenCount > maxSeen {
			maxSeen = suppression.SeenCount
		}
	}
	return maxSeen
}

func signalSuppressionKey(signalID, action, reasonCode string) string {
	return signalID + "|" + strings.ToLower(strings.TrimSpace(action)) + "|" + strings.ToLower(strings.TrimSpace(reasonCode))
}

func structureSuppressionKey(structureKey, action, reasonCode string) string {
	return "structure:" + structureKey + "|" + strings.ToLower(strings.TrimSpace(action)) + "|" + strings.ToLower(strings.TrimSpace(reasonCode))
}

func hasRetryableSignalMarker(markers []SignalMarker, signalID string) bool {
	for i := len(markers) - 1; i >= 0; i-- {
		if markers[i].SignalID != signalID {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(markers[i].Status)) {
		case "rejected", "failed":
			return true
		default:
			return false
		}
	}
	return false
}

func (s *StateStore) SetLastAnalyzedClosedKline(traderID, symbol, timeframe string, closeTime int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	if state.LastAnalyzedClosedKline == nil {
		state.LastAnalyzedClosedKline = map[string]int64{}
	}
	state.LastAnalyzedClosedKline[timeframe] = closeTime
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) PositionState(traderID, symbol, side string) ProgrammaticPositionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	key := normalizeSideKey(side)
	posState := state.PositionStates[key]
	if posState.Side == "" {
		posState.Side = key
	}
	return posState
}

func (s *StateStore) UpdatePositionState(traderID, symbol, side string, updateFn func(*ProgrammaticPositionState)) ProgrammaticPositionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	key := normalizeSideKey(side)
	posState := state.PositionStates[key]
	if posState.Side == "" {
		posState.Side = key
	}
	if updateFn != nil {
		updateFn(&posState)
	}
	posState.LastManagedAt = s.now()
	state.PositionStates[key] = posState
	s.setSymbolLocked(traderID, symbol, state)
	return posState
}

func (s *StateStore) MarkPositionSignal(traderID, symbol, side, rule, signalID string) bool {
	if signalID == "" {
		return false
	}
	marked := false
	s.UpdatePositionState(traderID, symbol, side, func(state *ProgrammaticPositionState) {
		switch rule {
		case "breakeven":
			if state.LastBreakevenSignalID == signalID {
				return
			}
			state.LastBreakevenSignalID = signalID
		case "floating_drawdown":
			if state.LastDrawdownSignalID == signalID {
				return
			}
			state.LastDrawdownSignalID = signalID
		case "structure_break":
			if state.LastStructureSignalID == signalID {
				return
			}
			state.LastStructureSignalID = signalID
		case "short_trade":
			if state.LastShortTradeSignalID == signalID {
				return
			}
			state.LastShortTradeSignalID = signalID
		default:
			return
		}
		marked = true
	})
	return marked
}

func (s *StateStore) HasPositionSignal(traderID, symbol, side, rule, signalID string) bool {
	if signalID == "" {
		return false
	}
	state := s.PositionState(traderID, symbol, side)
	switch rule {
	case "breakeven":
		return state.LastBreakevenSignalID == signalID
	case "floating_drawdown":
		return state.LastDrawdownSignalID == signalID
	case "structure_break":
		return state.LastStructureSignalID == signalID
	case "short_trade":
		return state.LastShortTradeSignalID == signalID
	default:
		return false
	}
}

func (s *StateStore) PartialCloseGuardState(traderID, symbol, side string) PartialCloseGuardState {
	return s.PositionState(traderID, symbol, side).PartialCloseGuard
}

type ProgrammaticPartialCloseRecord struct {
	TraderID                 string
	Symbol                   string
	Side                     string
	Rule                     string
	SignalID                 string
	RequestedClosePercentage float64
	ExecutedClosePercentage  float64
	ExecutedQuantity         float64
	PositionQuantityBefore   float64
	Price                    float64
	Estimated                bool
	ExecutedAt               time.Time
	PeakPrice                float64
	PeakPnLPct               float64
	PeakR                    float64
}

func (s *StateStore) RecordProgrammaticPartialClose(record ProgrammaticPartialCloseRecord) ProgrammaticPositionState {
	executedAt := record.ExecutedAt
	if executedAt.IsZero() {
		executedAt = s.now()
	}
	return s.UpdatePositionState(record.TraderID, record.Symbol, record.Side, func(state *ProgrammaticPositionState) {
		guard := state.PartialCloseGuard
		executedQty := record.ExecutedQuantity
		estimated := record.Estimated
		if executedQty <= 0 && record.PositionQuantityBefore > 0 && record.ExecutedClosePercentage > 0 {
			executedQty = record.PositionQuantityBefore * record.ExecutedClosePercentage / 100
			estimated = true
		}
		if executedQty <= 0 && guard.LastKnownQuantity > 0 && record.RequestedClosePercentage > 0 {
			executedQty = guard.LastKnownQuantity * record.RequestedClosePercentage / 100
			estimated = true
		}
		if guard.InitialTrackedQuantity <= 0 {
			switch {
			case record.PositionQuantityBefore > 0:
				guard.InitialTrackedQuantity = record.PositionQuantityBefore
			case executedQty > 0 && record.ExecutedClosePercentage > 0:
				guard.InitialTrackedQuantity = executedQty / (record.ExecutedClosePercentage / 100)
			case guard.LastKnownQuantity > 0:
				guard.InitialTrackedQuantity = guard.LastKnownQuantity + executedQty
			default:
				guard.InitialTrackedQuantity = executedQty
			}
			if record.Price > 0 {
				guard.InitialTrackedValueUSD = guard.InitialTrackedQuantity * record.Price
			}
		}
		if record.PositionQuantityBefore > 0 {
			guard.LastKnownQuantity = record.PositionQuantityBefore - executedQty
			if guard.LastKnownQuantity < 0 {
				guard.LastKnownQuantity = 0
			}
		} else if guard.LastKnownQuantity > 0 && executedQty > 0 {
			guard.LastKnownQuantity -= executedQty
			if guard.LastKnownQuantity < 0 {
				guard.LastKnownQuantity = 0
			}
		}
		guard.TotalPartialCloseQuantity += executedQty
		if guard.InitialTrackedQuantity > 0 {
			guard.TotalPartialClosePct = guard.TotalPartialCloseQuantity / guard.InitialTrackedQuantity * 100
			if guard.TotalPartialClosePct > 100 {
				guard.TotalPartialClosePct = 100
			}
		}
		lastPct := record.ExecutedClosePercentage
		if lastPct <= 0 {
			lastPct = record.RequestedClosePercentage
		}
		guard.LastPartialCloseAt = executedAt
		guard.LastPartialCloseRule = record.Rule
		guard.LastPartialCloseSignalID = record.SignalID
		guard.LastPartialClosePct = lastPct
		guard.PartialCloseCount++
		guard.QuantityEstimated = estimated
		if record.Rule == "floating_drawdown" {
			guard.RequireNewPeakForDrawdown = true
			guard.LastDrawdownPeakPrice = record.PeakPrice
			guard.LastDrawdownPeakPnLPct = record.PeakPnLPct
			guard.LastDrawdownPeakR = record.PeakR
		}
		state.PartialCloseGuard = guard
	})
}

func (s *StateStore) RecordProgrammaticFullClose(traderID, symbol, side string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	delete(state.PositionStates, normalizeSideKey(side))
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) ResetPositionGuardIfMissing(traderID string, activePositions []decision.PositionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := map[string]bool{}
	for _, pos := range activePositions {
		symbol := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		if symbol == "" {
			continue
		}
		active[symbol+"|"+normalizeSideKey(pos.Side)] = true
	}
	trader := s.data.Traders[traderID]
	for symbol, state := range trader.Symbols {
		for side := range state.PositionStates {
			if !active[strings.ToUpper(symbol)+"|"+normalizeSideKey(side)] {
				delete(state.PositionStates, side)
			}
		}
		trader.Symbols[symbol] = state
	}
	s.data.Traders[traderID] = trader
}

func (s *StateStore) StoreSignalMarker(traderID, symbol string, marker SignalMarker) {
	if marker.SignalID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	marker = canonicalizeSignalMarker(marker)
	state.RecentSignalMarkers = upsertSignalMarker(state.RecentSignalMarkers, marker)
	state.SignalLifecycles = upsertSignalLifecycleMap(state.SignalLifecycles, marker, s.now())
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) UpdateSignalMarkerStatus(traderID, symbol, signalID, status, reason string) {
	if signalID == "" || status == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	for i := range state.RecentSignalMarkers {
		if state.RecentSignalMarkers[i].SignalID != signalID {
			continue
		}
		state.RecentSignalMarkers[i].Status = status
		if reason != "" {
			state.RecentSignalMarkers[i].Reason = reason
		}
		state.RecentSignalMarkers[i] = canonicalizeSignalMarker(state.RecentSignalMarkers[i])
		state.SignalLifecycles = upsertSignalLifecycleMap(state.SignalLifecycles, state.RecentSignalMarkers[i], s.now())
	}
	s.setSymbolLocked(traderID, symbol, state)
}

func (s *StateStore) RecentSignalMarkers(traderID, symbol string, limit int) []SignalMarker {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	markers := append([]SignalMarker(nil), state.RecentSignalMarkers...)
	if limit <= 0 || len(markers) <= limit {
		return markers
	}
	return markers[len(markers)-limit:]
}

func (s *StateStore) CompactSignalMarkers(traderID, symbol string) SignalMarkerSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ensureSymbolLocked(traderID, symbol)
	raw := append([]SignalMarker(nil), state.RecentSignalMarkers...)
	state.RecentSignalMarkers, state.SignalLifecycles = compactSignalMarkers(raw, s.now())
	s.setSymbolLocked(traderID, symbol, state)
	return buildSignalMarkerSummary(raw, state.RecentSignalMarkers)
}

func upsertSignalMarker(markers []SignalMarker, marker SignalMarker) []SignalMarker {
	marker = canonicalizeSignalMarker(marker)
	key := signalMarkerKey(marker)
	for i := range markers {
		if signalMarkerKey(markers[i]) == key || legacySignalMarkerKey(markers[i]) == legacySignalMarkerKey(marker) {
			markers[i] = mergeSignalMarkerLifecycle(markers[i], marker)
			return markers
		}
	}
	markers = append(markers, marker)
	if len(markers) > maxRecentSignalMarkers {
		markers = markers[len(markers)-maxRecentSignalMarkers:]
	}
	return markers
}

func signalMarkerKey(marker SignalMarker) string {
	if marker.LifecycleKey != "" {
		return marker.LifecycleKey
	}
	return MarkerLifecycleKey(marker)
}

func legacySignalMarkerKey(marker SignalMarker) string {
	closeTime := marker.CloseTime
	if closeTime == 0 {
		closeTime = marker.SignalCloseTime
	}
	return marker.SignalID + "|" + marker.Timeframe + "|" + strconv.FormatInt(closeTime, 10)
}

func mergeSignalMarkerLifecycle(existing, incoming SignalMarker) SignalMarker {
	existing = canonicalizeSignalMarker(existing)
	incoming = canonicalizeSignalMarker(incoming)
	if isProcessedSignalMarker(existing) && isDetectedOnlyMarker(incoming) {
		if existing.SignalCloseTime == 0 {
			existing.SignalCloseTime = incoming.SignalCloseTime
		}
		if existing.DecisionCloseTime == 0 {
			existing.DecisionCloseTime = incoming.DecisionCloseTime
		}
		if existing.DisplayCloseTime == 0 {
			existing.DisplayCloseTime = incoming.DisplayCloseTime
		}
		if existing.Price == 0 {
			existing.Price = incoming.Price
		}
		if existing.Direction == "" {
			existing.Direction = incoming.Direction
		}
		if existing.SignalType == "" {
			existing.SignalType = incoming.SignalType
		}
		if existing.Level == "" {
			existing.Level = incoming.Level
		}
		if existing.SourceLayer == "" {
			existing.SourceLayer = incoming.SourceLayer
		}
		existing = mergeSignalMarkerCounters(existing, incoming)
		return existing
	}
	if isProcessedSignalMarker(existing) && !isProcessedSignalMarker(incoming) {
		return mergeSignalMarkerCounters(existing, incoming)
	}
	merged := incoming
	if markerTerminalRank(existing.Status) > markerTerminalRank(incoming.Status) {
		merged = existing
		merged.DecisionCloseTime = firstPositiveInt64(incoming.DecisionCloseTime, existing.DecisionCloseTime)
		merged.DisplayCloseTime = firstPositiveInt64(incoming.DisplayCloseTime, existing.DisplayCloseTime)
		merged.LastSeenCloseTime = firstPositiveInt64(incoming.LastSeenCloseTime, incoming.DecisionCloseTime, incoming.DisplayCloseTime, existing.LastSeenCloseTime)
		merged.LastUpdatedAt = firstPositiveInt64(incoming.LastUpdatedAt, merged.LastSeenCloseTime, existing.LastUpdatedAt)
		if incoming.Reason != "" {
			merged.Reason = incoming.Reason
		}
		if incoming.ReasonCode != "" {
			merged.ReasonCode = incoming.ReasonCode
		}
	}
	if merged.SignalID == incoming.SignalID && merged.Status == incoming.Status {
		merged = mergeSignalMarkerCounters(merged, existing)
	} else {
		merged = mergeSignalMarkerCounters(merged, incoming)
	}
	return merged
}

func mergeSignalMarkerCounters(base, other SignalMarker) SignalMarker {
	firstSeen := firstPositiveInt64(base.FirstSeenCloseTime, base.SignalCloseTime, base.CloseTime, other.FirstSeenCloseTime)
	if other.FirstSeenCloseTime > 0 && (firstSeen == 0 || other.FirstSeenCloseTime < firstSeen) {
		firstSeen = other.FirstSeenCloseTime
	}
	base.FirstSeenCloseTime = firstSeen
	base.LastSeenCloseTime = maxInt64(base.LastSeenCloseTime, other.LastSeenCloseTime, other.DecisionCloseTime, other.DisplayCloseTime, other.CloseTime)
	base.LastUpdatedAt = maxInt64(base.LastUpdatedAt, other.LastUpdatedAt, base.LastSeenCloseTime)
	base.Collapsed = true
	base.CollapsedCount += other.CollapsedCount + 1
	base.HiddenByDefault = base.HiddenByDefault || other.HiddenByDefault
	return base
}

func markerTerminalRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "executed":
		return 60
	case "failed":
		return 50
	case "rejected":
		return 40
	case "invalidated", "expired", "suppressed":
		return 30
	case "ready":
		return 20
	case "background", "confirmed", "watchlist", "detected":
		return 10
	default:
		return 0
	}
}

func upsertSignalLifecycleMap(lifecycles map[string]SignalLifecycle, marker SignalMarker, now time.Time) map[string]SignalLifecycle {
	if lifecycles == nil {
		lifecycles = map[string]SignalLifecycle{}
	}
	marker = canonicalizeSignalMarker(marker)
	lifecycle := lifecycles[marker.LifecycleKey]
	if lifecycle.LifecycleKey == "" {
		lifecycle = SignalLifecycle{
			LifecycleKey:       marker.LifecycleKey,
			StructureKey:       marker.StructureKey,
			ParentStructureKey: marker.ParentStructureKey,
			Marker:             marker,
			FirstSeenAt:        now,
			LastSeenAt:         now,
			SeenCount:          1,
			CollapsedCount:     marker.CollapsedCount,
		}
	} else {
		lifecycle.Marker = mergeSignalMarkerLifecycle(lifecycle.Marker, marker)
		lifecycle.LastSeenAt = now
		lifecycle.SeenCount++
		lifecycle.CollapsedCount = lifecycle.Marker.CollapsedCount
		if isSuppressedRepeatMarker(marker) {
			lifecycle.SuppressedCount++
		}
	}
	lifecycles[marker.LifecycleKey] = lifecycle
	return lifecycles
}

func compactSignalMarkers(raw []SignalMarker, now time.Time) ([]SignalMarker, map[string]SignalLifecycle) {
	var markers []SignalMarker
	lifecycles := map[string]SignalLifecycle{}
	for _, marker := range raw {
		if marker.SignalID == "" {
			continue
		}
		marker = canonicalizeSignalMarker(marker)
		markers = upsertSignalMarker(markers, marker)
		lifecycles = upsertSignalLifecycleMap(lifecycles, marker, now)
	}
	return markers, lifecycles
}

func (s *StateStore) compactAllSignalMarkersLocked() {
	now := s.now()
	for traderID, traderState := range s.data.Traders {
		for symbol, symbolState := range traderState.Symbols {
			symbolState.RecentSignalMarkers, symbolState.SignalLifecycles = compactSignalMarkers(symbolState.RecentSignalMarkers, now)
			traderState.Symbols[symbol] = symbolState
		}
		s.data.Traders[traderID] = traderState
	}
}

func maxInt64(values ...int64) int64 {
	var max int64
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func isProcessedSignalMarker(marker SignalMarker) bool {
	if marker.Action != "" || marker.FinalAction != "" || marker.TradeIntent != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(marker.Status)) {
	case "rejected", "executed", "failed", "deduped":
		return true
	default:
		return false
	}
}

func isDetectedOnlyMarker(marker SignalMarker) bool {
	if marker.Action != "" || marker.FinalAction != "" || marker.TradeIntent != "" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(marker.Status))
	return status == "" || status == "detected"
}

func (s *StateStore) ensureSymbolLocked(traderID, symbol string) ProgrammaticSymbolState {
	if s.data.Traders == nil {
		s.data.Traders = map[string]ProgrammaticTraderState{}
	}
	trader := s.data.Traders[traderID]
	if trader.Symbols == nil {
		trader.Symbols = map[string]ProgrammaticSymbolState{}
	}
	state := trader.Symbols[symbol]
	if state.ConfirmedSignals == nil {
		state.ConfirmedSignals = map[string]StoredSignal{}
	}
	if state.ExecutedSignals == nil {
		state.ExecutedSignals = map[string]SignalExec{}
	}
	if state.SuppressedSignals == nil {
		state.SuppressedSignals = map[string]SignalSuppression{}
	}
	if state.LifecycleTerminations == nil {
		state.LifecycleTerminations = map[string]LifecycleTermination{}
	}
	if state.SignalLifecycles == nil {
		state.SignalLifecycles = map[string]SignalLifecycle{}
	}
	if state.AddCountBySide == nil {
		state.AddCountBySide = map[string]int{}
	}
	if state.LastAnalyzedClosedKline == nil {
		state.LastAnalyzedClosedKline = map[string]int64{}
	}
	if state.PositionStates == nil {
		state.PositionStates = map[string]ProgrammaticPositionState{}
	}
	trader.Symbols[symbol] = state
	s.data.Traders[traderID] = trader
	return state
}

func (s *StateStore) setSymbolLocked(traderID, symbol string, state ProgrammaticSymbolState) {
	trader := s.data.Traders[traderID]
	if trader.Symbols == nil {
		trader.Symbols = map[string]ProgrammaticSymbolState{}
	}
	trader.Symbols[symbol] = state
	s.data.Traders[traderID] = trader
}

func emptyStateFile() ProgrammaticStateFile {
	return ProgrammaticStateFile{
		Version:   1,
		UpdatedAt: time.Now(),
		Traders:   map[string]ProgrammaticTraderState{},
	}
}

func (s *StateStore) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func normalizeSideKey(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case SideShort:
		return SideShort
	default:
		return SideLong
	}
}
