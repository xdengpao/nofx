package chanlun

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type StateStore struct {
	path string
	mu   sync.Mutex
	data ProgrammaticStateFile
}

type ProgrammaticStateFile struct {
	Version   int                                `json:"version"`
	UpdatedAt time.Time                          `json:"updated_at"`
	Traders   map[string]ProgrammaticTraderState `json:"traders"`
}

type ProgrammaticTraderState struct {
	Symbols map[string]ProgrammaticSymbolState `json:"symbols"`
}

type ProgrammaticSymbolState struct {
	LastStructureHash       string                  `json:"last_structure_hash,omitempty"`
	LastAnalyzedClosedKline map[string]int64        `json:"last_analyzed_closed_kline,omitempty"`
	ConfirmedSignals        map[string]StoredSignal `json:"confirmed_signals,omitempty"`
	ExecutedSignals         map[string]SignalExec   `json:"executed_signals,omitempty"`
	AddCountBySide          map[string]int          `json:"add_count_by_side,omitempty"`
	ShortTradeState         *ShortTradeState        `json:"short_trade_state,omitempty"`
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
	if _, exists := state.ExecutedSignals[signalID]; exists {
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
	if state.AddCountBySide == nil {
		state.AddCountBySide = map[string]int{}
	}
	if state.LastAnalyzedClosedKline == nil {
		state.LastAnalyzedClosedKline = map[string]int64{}
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
