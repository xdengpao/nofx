package chanlunv2

import "nofx/market"

type entryTriggerFunnelDiagnostics struct {
	RawSignalCount          int                                 `json:"raw_signal_count"`
	ParentStructureCount    int                                 `json:"parent_structure_count"`
	ParentTerminalByReason  map[string]int                      `json:"parent_terminal_by_reason,omitempty"`
	WaitingForTriggerCount  int                                 `json:"waiting_for_trigger_count,omitempty"`
	TriggerReadyCount       int                                 `json:"trigger_ready_count,omitempty"`
	TriggerReadyByType      map[string]int                      `json:"trigger_ready_by_type,omitempty"`
	TriggerRejectedByReason map[string]int                      `json:"trigger_rejected_by_reason,omitempty"`
	TerminalSuppressedCount int                                 `json:"terminal_suppressed_count,omitempty"`
	PerSymbol               map[string]entryTriggerSymbolFunnel `json:"per_symbol,omitempty"`
}

type entryTriggerSymbolFunnel struct {
	RawSignalCount         int            `json:"raw_signal_count,omitempty"`
	ParentStructureCount   int            `json:"parent_structure_count,omitempty"`
	ParentTerminalByReason map[string]int `json:"parent_terminal_by_reason,omitempty"`
	WaitingForTriggerCount int            `json:"waiting_for_trigger_count,omitempty"`
	TriggerReadyCount      int            `json:"trigger_ready_count,omitempty"`
}

func newEntryTriggerFunnelDiagnostics() entryTriggerFunnelDiagnostics {
	return entryTriggerFunnelDiagnostics{
		ParentTerminalByReason:  map[string]int{},
		TriggerReadyByType:      map[string]int{},
		TriggerRejectedByReason: map[string]int{},
		PerSymbol:               map[string]entryTriggerSymbolFunnel{},
	}
}

func (f *entryTriggerFunnelDiagnostics) addRawSignals(symbol string, count int) {
	if f == nil || count <= 0 {
		return
	}
	f.RawSignalCount += count
	f.mutateSymbol(symbol, func(symbolFunnel *entryTriggerSymbolFunnel) {
		symbolFunnel.RawSignalCount += count
	})
}

func (f *entryTriggerFunnelDiagnostics) addParentSeen(symbol string) {
	if f == nil {
		return
	}
	f.ParentStructureCount++
	f.mutateSymbol(symbol, func(symbolFunnel *entryTriggerSymbolFunnel) {
		symbolFunnel.ParentStructureCount++
	})
}

func (f *entryTriggerFunnelDiagnostics) addParentTerminal(symbol, reason string) {
	if f == nil {
		return
	}
	reason = firstNonEmptyString(reason, "parent_terminal")
	f.ParentTerminalByReason[reason]++
	f.mutateSymbol(symbol, func(symbolFunnel *entryTriggerSymbolFunnel) {
		if symbolFunnel.ParentTerminalByReason == nil {
			symbolFunnel.ParentTerminalByReason = map[string]int{}
		}
		symbolFunnel.ParentTerminalByReason[reason]++
	})
}

func (f *entryTriggerFunnelDiagnostics) addWaitingForTrigger(symbol string) {
	if f == nil {
		return
	}
	f.WaitingForTriggerCount++
	f.mutateSymbol(symbol, func(symbolFunnel *entryTriggerSymbolFunnel) {
		symbolFunnel.WaitingForTriggerCount++
	})
}

func (f *entryTriggerFunnelDiagnostics) addTriggerRejected(symbol, reason string) {
	if f == nil {
		return
	}
	reason = firstNonEmptyString(reason, "entry_trigger_rejected")
	f.TriggerRejectedByReason[reason]++
}

func (f *entryTriggerFunnelDiagnostics) addTriggerReady(symbol, triggerType string) {
	if f == nil {
		return
	}
	triggerType = firstNonEmptyString(triggerType, "entry_trigger")
	f.TriggerReadyCount++
	f.TriggerReadyByType[triggerType]++
	f.mutateSymbol(symbol, func(symbolFunnel *entryTriggerSymbolFunnel) {
		symbolFunnel.TriggerReadyCount++
	})
}

func (f *entryTriggerFunnelDiagnostics) withTerminalSuppressions(count int) {
	if f == nil || count <= 0 {
		return
	}
	f.TerminalSuppressedCount = count
}

func (f entryTriggerFunnelDiagnostics) compact() entryTriggerFunnelDiagnostics {
	if len(f.ParentTerminalByReason) == 0 {
		f.ParentTerminalByReason = nil
	}
	if len(f.TriggerReadyByType) == 0 {
		f.TriggerReadyByType = nil
	}
	if len(f.TriggerRejectedByReason) == 0 {
		f.TriggerRejectedByReason = nil
	}
	if len(f.PerSymbol) == 0 {
		f.PerSymbol = nil
	}
	return f
}

func (f *entryTriggerFunnelDiagnostics) mutateSymbol(symbol string, mutate func(*entryTriggerSymbolFunnel)) {
	if f == nil || mutate == nil {
		return
	}
	symbol = market.Normalize(symbol)
	if symbol == "" {
		return
	}
	value := f.PerSymbol[symbol]
	mutate(&value)
	f.PerSymbol[symbol] = value
}
