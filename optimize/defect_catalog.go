package optimize

import (
	"fmt"
	"strings"
	"time"
)

func BuildDefectCatalog(replayInputs []ReplayReportInput, backtestInputs []BacktestRunInput, config *OptimizationConfig) (*DefectCatalog, error) {
	if config == nil {
		config = DefaultOptimizationConfig()
	}
	catalog := &DefectCatalog{
		GeneratedAt: time.Now().UTC(),
		DiagnosisInterval: DiagnosisInterval{
			ReplayFrom:   config.ReplayFrom,
			ReplayTo:     config.ReplayTo,
			BacktestFrom: config.BacktestFrom,
			BacktestTo:   config.BacktestTo,
			WarmupFrom:   config.WarmupFrom,
			Timezone:     config.Timezone,
		},
		Summary: DefectSummary{
			ByDefectCode:     map[string]int{},
			ByTraderExchange: map[string]int{},
			BySignalType:     map[string]int{},
		},
	}
	for _, input := range replayInputs {
		if strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.TraderID) == "" || strings.TrimSpace(input.Exchange) == "" {
			catalog.Summary.RejectedNoEvidenceCount++
			continue
		}
		addReplayDisease(catalog, input, "MICRO_STOP", "微止损频率过高", input.Report.StrategyDisease.MicroStopCount, "micro_stop_rate")
		addReplayDisease(catalog, input, "LOW_ADX_ENTRY", "低ADX环境开仓过多", input.Report.StrategyDisease.LowADXEntryCount, "low_adx_entry_rate")
		addReplayDisease(catalog, input, "COUNTER_DI_ENTRY", "DI方向逆势开仓", input.Report.StrategyDisease.CounterDIEntryCount, "counter_di_entry_rate")
		addReplayDisease(catalog, input, "PROFILE_MISMATCH", "ATR/ADX profile不匹配", input.Report.StrategyDisease.ProfileMismatchCount, "profile_mismatch_rate")
		addReplayDisease(catalog, input, "SAME_SIDE_CORRELATION", "同方向相关性集中", input.Report.StrategyDisease.SameSideCorrelationCount, "same_side_correlation_rate")
	}
	for _, input := range backtestInputs {
		if input.Artifacts == nil || strings.TrimSpace(input.TraderID) == "" || strings.TrimSpace(input.Exchange) == "" {
			catalog.Summary.RejectedNoEvidenceCount++
			continue
		}
		for _, rejection := range input.Artifacts.Rejections {
			code := normalizeDefectCode(rejection.Reason)
			if code == "" {
				code = "OPEN_REJECTION"
			}
			ref := EvidenceRef{
				Source:     "backtest_report",
				Path:       input.Artifacts.OutputDir,
				RunID:      firstNonEmpty(input.RunID, input.Artifacts.RunID),
				DataHash:   firstNonEmpty(input.DataHash, input.Artifacts.Report.DataHash),
				TraderID:   input.TraderID,
				Exchange:   input.Exchange,
				Symbol:     rejection.Symbol,
				SignalID:   rejection.SignalID,
				ReasonCode: code,
				TimeFrom:   config.BacktestFrom,
				TimeTo:     config.BacktestTo,
			}
			entry := DefectEntry{
				DefectCode:                    code,
				DescriptionZH:                 rejection.Reason,
				EvidenceRefs:                  []EvidenceRef{ref},
				AffectedTraders:               []string{input.TraderID},
				AffectedExchanges:             []string{input.Exchange},
				AffectedSymbols:               nonEmptyList(rejection.Symbol),
				AffectedSignalTypes:           nonEmptyList(rejection.SignalType),
				PrimaryMetric:                 "rejection_rate",
				SampleCountByTraderExchange:   map[string]int{input.TraderID + "|" + input.Exchange: 1},
				DirectionDistByTraderExchange: map[string]string{input.TraderID + "|" + input.Exchange: rejection.Action},
			}
			catalog.Defects = append(catalog.Defects, entry)
			updateSummary(&catalog.Summary, entry)
		}
	}
	if len(catalog.Defects) == 0 {
		return catalog, ErrInsufficientEvidence
	}
	catalog.Summary.TotalDefects = len(catalog.Defects)
	return catalog, nil
}

func addReplayDisease(catalog *DefectCatalog, input ReplayReportInput, code, description string, count int, metric string) {
	if count <= 0 {
		return
	}
	ref := EvidenceRef{
		Source:     "replay_report",
		Path:       input.Path,
		RunID:      input.RunID,
		DataHash:   input.DataHash,
		TraderID:   input.TraderID,
		Exchange:   input.Exchange,
		ReasonCode: strings.ToLower(code),
		TimeFrom:   input.From,
		TimeTo:     input.To,
	}
	key := input.TraderID + "|" + input.Exchange
	entry := DefectEntry{
		DefectCode:                    code,
		DescriptionZH:                 description,
		EvidenceRefs:                  []EvidenceRef{ref},
		AffectedTraders:               []string{input.TraderID},
		AffectedExchanges:             []string{input.Exchange},
		PrimaryMetric:                 metric,
		SampleCountByTraderExchange:   map[string]int{key: count},
		DirectionDistByTraderExchange: map[string]string{key: "mixed"},
	}
	catalog.Defects = append(catalog.Defects, entry)
	updateSummary(&catalog.Summary, entry)
}

func updateSummary(summary *DefectSummary, entry DefectEntry) {
	summary.ByDefectCode[entry.DefectCode]++
	for key, count := range entry.SampleCountByTraderExchange {
		summary.ByTraderExchange[key] += count
	}
	for _, signalType := range entry.AffectedSignalTypes {
		if signalType != "" {
			summary.BySignalType[signalType]++
		}
	}
}

func normalizeDefectCode(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "名义额") || strings.Contains(reason, "notional"):
		return "MIN_NOTIONAL_REJECTION"
	case strings.Contains(reason, "adx"):
		return "LOW_ADX_ENTRY"
	case strings.Contains(reason, "相关"):
		return "SAME_SIDE_CORRELATION"
	case reason == "":
		return ""
	default:
		return "OPEN_REJECTION_" + strings.ToUpper(strings.ReplaceAll(fmt.Sprintf("%.24s", reason), " ", "_"))
	}
}

func nonEmptyList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}
