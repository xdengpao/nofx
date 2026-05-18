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
	path string
	mu   sync.Mutex
	data ProgrammaticStateFile
}

const maxRecentSignalMarkers = 200

type ProgrammaticStateFile struct {
	Version   int                                `json:"version"`
	UpdatedAt time.Time                          `json:"updated_at"`
	Traders   map[string]ProgrammaticTraderState `json:"traders"`
}

type ProgrammaticTraderState struct {
	Symbols map[string]ProgrammaticSymbolState `json:"symbols"`
}

type ProgrammaticSymbolState struct {
	LastStructureHash       string                               `json:"last_structure_hash,omitempty"`
	LastAnalyzedClosedKline map[string]int64                     `json:"last_analyzed_closed_kline,omitempty"`
	ConfirmedSignals        map[string]StoredSignal              `json:"confirmed_signals,omitempty"`
	ExecutedSignals         map[string]SignalExec                `json:"executed_signals,omitempty"`
	SuppressedSignals       map[string]SignalSuppression         `json:"suppressed_signals,omitempty"`
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
	Action            string    `json:"action"`
	ReasonCode        string    `json:"reason_code"`
	SuppressedAt      time.Time `json:"suppressed_at"`
	SignalCloseTime   int64     `json:"signal_close_time,omitempty"`
	DecisionCloseTime int64     `json:"decision_close_time,omitempty"`
	FreshnessState    string    `json:"freshness_state,omitempty"`
	CurrentPrice      float64   `json:"current_price,omitempty"`
	StopLoss          float64   `json:"stop_loss,omitempty"`
	TakeProfit        float64   `json:"take_profit,omitempty"`
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
	store := &StateStore{path: path}
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
	return nil
}

func (s *StateStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.UpdatedAt = time.Now()
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
	state.ConfirmedSignals[signal.SignalID] = StoredSignal{Signal: signal, StoredAt: time.Now(), Bootstrap: bootstrap}
	s.setSymbolLocked(traderID, symbol, state)
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
	state.ExecutedSignals[signalID] = SignalExec{SignalID: signalID, Action: action, ExecutedAt: time.Now()}
	switch action {
	case "add_long":
		state.AddCountBySide[SideLong]++
	case "add_short":
		state.AddCountBySide[SideShort]++
	case "partial_close":
		state.ShortTradeState = &ShortTradeState{ReducedSignalID: signalID, CanReAdd: true, UpdatedAt: time.Now()}
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
		suppression.SuppressedAt = time.Now()
	}
	state.SuppressedSignals[signalSuppressionKey(suppression.SignalID, suppression.Action, suppression.ReasonCode)] = suppression
	s.setSymbolLocked(traderID, symbol, state)
}

func signalSuppressionKey(signalID, action, reasonCode string) string {
	return signalID + "|" + strings.ToLower(strings.TrimSpace(action)) + "|" + strings.ToLower(strings.TrimSpace(reasonCode))
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
	posState.LastManagedAt = time.Now()
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
		executedAt = time.Now()
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
	state.RecentSignalMarkers = upsertSignalMarker(state.RecentSignalMarkers, marker)
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

func upsertSignalMarker(markers []SignalMarker, marker SignalMarker) []SignalMarker {
	key := signalMarkerKey(marker)
	for i := range markers {
		if signalMarkerKey(markers[i]) == key {
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
	return marker.SignalID + "|" + marker.Timeframe + "|" + strconv.FormatInt(marker.CloseTime, 10)
}

func mergeSignalMarkerLifecycle(existing, incoming SignalMarker) SignalMarker {
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
		return existing
	}
	return incoming
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

func normalizeSideKey(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case SideShort:
		return SideShort
	default:
		return SideLong
	}
}
