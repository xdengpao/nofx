package chanlun

import (
	"fmt"
	"log"
	"nofx/decision"
	"nofx/market"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	Policy             decision.ProgrammaticStrategyPolicy
	StateStore         *StateStore
	Clock              func() time.Time
	MarketDataProvider func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error)
	DisableOITopFetch  bool

	mu               sync.RWMutex
	latestSignals    map[string]*SignalReport
	symbolUniverse   map[string][]StrategySymbol
	activeMode       string
	loosenMinRRDelta float64
	loosenChaseBump  float64
}

func NewEngine(policy decision.ProgrammaticStrategyPolicy) (*Engine, error) {
	if policy.DecisionMode == "" {
		policy.DecisionMode = "programmatic"
	}
	if policy.StrategyName == "" {
		policy.StrategyName = "chanlun_programmatic"
	}
	if policy.StrategyVersion == "" {
		policy.StrategyVersion = "v1"
	}
	if policy.ConfigHash == "" {
		policy.ConfigHash = "default"
	}
	if policy.State.Path == "" {
		policy.State.Path = "data/programmatic_strategy_state.json"
	}
	if policy.Timeframes.Trade == "" {
		policy.Timeframes = decision.ProgrammaticTimeframesPolicy{Higher: "4h", Trade: "1h", Sub: "15m", Micro: "3m"}
	}
	if policy.PositionManagement.Timeframes.Structure == "" {
		policy.PositionManagement = defaultPositionManagementPolicy(policy.Position.PartialClosePct)
	}
	policy.SignalFreshness = normalizeRuntimeSignalFreshness(policy.SignalFreshness, policy.TakeProfit.MinNetRR)
	policy.PreviewSignals = normalizeRuntimePreviewSignals(policy.PreviewSignals, policy.Timeframes, policy.DefectFixPackEnabled)
	policy.EntryTiming = normalizeRuntimeEntryTiming(policy.EntryTiming, policy.Timeframes, policy.TakeProfit.MinNetRR, policy.DefectFixPackEnabled)
	engine := &Engine{
		Policy:         policy,
		StateStore:     NewStateStore(policy.State.Path),
		Clock:          time.Now,
		latestSignals:  map[string]*SignalReport{},
		symbolUniverse: map[string][]StrategySymbol{},
	}
	return engine, nil
}

func normalizeRuntimeSignalFreshness(policy decision.ProgrammaticSignalFreshnessPolicy, fallbackMinRR float64) decision.ProgrammaticSignalFreshnessPolicy {
	uninitialized := policy.SoftAgeCandles == 0 && policy.MaxLifetimeCandles == 0 &&
		len(policy.SoftAgeBySignalType) == 0 && len(policy.MaxLifetimeBySignalType) == 0 &&
		policy.ConfidenceDecayPerAgedCandle == 0 && policy.MinRemainingNetRR == 0
	if policy.SoftAgeCandles <= 0 {
		policy.SoftAgeCandles = 2
	}
	if policy.MaxLifetimeCandles <= 0 || policy.MaxLifetimeCandles < policy.SoftAgeCandles {
		policy.MaxLifetimeCandles = maxInt(policy.SoftAgeCandles, 4)
	}
	if policy.SoftAgeBySignalType == nil {
		policy.SoftAgeBySignalType = map[string]int{}
	}
	if policy.MaxLifetimeBySignalType == nil {
		policy.MaxLifetimeBySignalType = map[string]int{}
	}
	for _, signalType := range []string{SignalBuy1, SignalSell1, SignalBuy2, SignalSell2} {
		if policy.SoftAgeBySignalType[signalType] <= 0 {
			policy.SoftAgeBySignalType[signalType] = policy.SoftAgeCandles
		}
		if policy.MaxLifetimeBySignalType[signalType] <= 0 {
			policy.MaxLifetimeBySignalType[signalType] = policy.MaxLifetimeCandles
		}
	}
	for _, signalType := range []string{SignalBuy3, SignalSell3} {
		if policy.SoftAgeBySignalType[signalType] <= 0 {
			policy.SoftAgeBySignalType[signalType] = 1
		}
		if policy.MaxLifetimeBySignalType[signalType] <= 0 {
			policy.MaxLifetimeBySignalType[signalType] = 2
		}
	}
	if policy.ConfidenceDecayPerAgedCandle <= 0 {
		policy.ConfidenceDecayPerAgedCandle = 3
	}
	if policy.MinRemainingNetRR <= 0 {
		policy.MinRemainingNetRR = fallbackMinRR
	}
	if policy.MinRemainingNetRR <= 0 {
		policy.MinRemainingNetRR = 2.5
	}
	if uninitialized {
		policy.Enabled = true
		policy.MissedTargetGuard = true
	}
	return policy
}

func normalizeRuntimePreviewSignals(policy decision.ProgrammaticPreviewSignalsPolicy, timeframes decision.ProgrammaticTimeframesPolicy, defectFixPackEnabled bool) decision.ProgrammaticPreviewSignalsPolicy {
	uninitialized := policy.ComponentTimeframe == "" && policy.TradeTimeframe == "" &&
		policy.WatchAfterClosedComponents == 0 && policy.PilotAfterClosedComponents == 0 &&
		policy.PilotRiskFraction == 0 && policy.PilotMinConfidence == 0 &&
		!policy.AllowPilotOpen && !policy.RequireConfirmedUpgrade
	if policy.ComponentTimeframe == "" {
		policy.ComponentTimeframe = timeframes.Sub
	}
	if policy.ComponentTimeframe == "" {
		policy.ComponentTimeframe = ComponentTimeframe(timeframes.Trade)
	}
	if policy.TradeTimeframe == "" {
		policy.TradeTimeframe = timeframes.Trade
	}
	if policy.TradeTimeframe == "" {
		policy.TradeTimeframe = "1h"
	}
	if policy.WatchAfterClosedComponents <= 0 {
		policy.WatchAfterClosedComponents = 2
	}
	if policy.PilotAfterClosedComponents <= 0 {
		policy.PilotAfterClosedComponents = 3
	}
	if policy.PilotRiskFraction <= 0 {
		policy.PilotRiskFraction = 0.3
	}
	if policy.PilotMinConfidence <= 0 {
		if defectFixPackEnabled {
			policy.PilotMinConfidence = 70
		} else {
			policy.PilotMinConfidence = 90
		}
	}
	if policy.P75Floor <= 0 {
		policy.P75Floor = 65
	}
	if policy.P75Ceiling <= 0 {
		policy.P75Ceiling = 85
	}
	if uninitialized {
		policy.Enabled = true
		policy.RequireConfirmedUpgrade = true
		policy.PilotMinConfidenceUseP75 = defectFixPackEnabled
	}
	return policy
}

func normalizeRuntimeEntryTiming(policy decision.ProgrammaticEntryTimingPolicy, timeframes decision.ProgrammaticTimeframesPolicy, fallbackMinRR float64, defectFixPackEnabled bool) decision.ProgrammaticEntryTimingPolicy {
	uninitialized := !policy.Enabled && !policy.DirectStructureOpen && policy.DirectOpenMaxAgeCandles == 0 &&
		!policy.RequireFreshTrigger && policy.TriggerTimeframe == "" && len(policy.AllowedTriggerTypes) == 0 &&
		policy.EntryZone.Mode == "" && policy.EntryZone.MaxChaseRatio == 0 && policy.EntryZone.MinRemainingNetRR == 0 &&
		len(policy.EntryZone.SymbolOverrides) == 0 &&
		policy.MaxTriggerAgeCandles == 0 && policy.MinTriggerConfidence == 0 &&
		!policy.Pilot.Enabled && policy.Pilot.RiskFraction == 0 && policy.Pilot.MinConfidence == 0 &&
		policy.ContinuationAfterTargetCrossed == ""
	if uninitialized {
		policy.Enabled = true
		policy.RequireFreshTrigger = true
		policy.DirectStructureOpen = defectFixPackEnabled
	}
	if policy.DirectStructureMinConfidence <= 0 {
		policy.DirectStructureMinConfidence = 70
	}
	if policy.TriggerTimeframe == "" {
		policy.TriggerTimeframe = timeframes.Sub
	}
	if policy.TriggerTimeframe == "" {
		policy.TriggerTimeframe = ComponentTimeframe(timeframes.Trade)
	}
	if len(policy.AllowedTriggerTypes) == 0 {
		policy.AllowedTriggerTypes = []string{"preview_2x15m_watchlist", "preview_3x15m_pilot", "pullback_retest_resume"}
	}
	if policy.EntryZone.Mode == "" {
		policy.EntryZone.Mode = "structure_range"
	}
	if policy.EntryZone.MaxChaseRatio <= 0 {
		policy.EntryZone.MaxChaseRatio = 0.35
	}
	if policy.EntryZone.MinRemainingNetRR <= 0 {
		if defectFixPackEnabled {
			policy.EntryZone.MinRemainingNetRR = 2.0
		} else {
			policy.EntryZone.MinRemainingNetRR = fallbackMinRR
		}
	}
	if policy.EntryZone.MinRemainingNetRR <= 0 {
		policy.EntryZone.MinRemainingNetRR = 2.5
	}
	if policy.EntryZone.MaxChaseATRMultiplier <= 0 {
		policy.EntryZone.MaxChaseATRMultiplier = 0.6
	}
	if policy.EntryZone.FreshAgeChaseRelax <= 0 {
		policy.EntryZone.FreshAgeChaseRelax = 0.10
	}
	if policy.EntryZone.SignalTypeMinRR == nil {
		policy.EntryZone.SignalTypeMinRR = map[string]float64{}
	}
	if defectFixPackEnabled && len(policy.EntryZone.SignalTypeMinRR) == 0 {
		policy.EntryZone.SignalTypeMinRR = defaultRuntimeSignalTypeMinRR()
		policy.EntryZone.TheoreticalRRUnreachableSkip = true
	}
	if policy.MaxTriggerAgeCandles <= 0 {
		policy.MaxTriggerAgeCandles = 1
	}
	if policy.MaxNoTriggerSubCandles <= 0 {
		policy.MaxNoTriggerSubCandles = 3
	}
	if policy.Pilot.RiskFraction <= 0 {
		policy.Pilot.RiskFraction = 0.3
	}
	if policy.Pilot.MinConfidence <= 0 {
		if defectFixPackEnabled {
			policy.Pilot.MinConfidence = 70
		} else {
			policy.Pilot.MinConfidence = 90
		}
	}
	if policy.ContinuationAfterTargetCrossed == "" {
		policy.ContinuationAfterTargetCrossed = "disabled"
	}
	return policy
}

func defaultRuntimeSignalTypeMinRR() map[string]float64 {
	return map[string]float64{
		"buy1@1h":  2.0,
		"sell1@1h": 2.0,
		"buy2@1h":  1.6,
		"sell2@1h": 1.6,
		"buy3@1h":  1.4,
		"sell3@1h": 1.4,
		"buy1":     2.0,
		"sell1":    2.0,
		"buy2":     1.6,
		"sell2":    1.6,
		"buy3":     1.4,
		"sell3":    1.4,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampInt(value, low, high int) int {
	if low <= 0 {
		low = 1
	}
	if high < low {
		high = low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("缺少交易上下文")
	}
	now := e.now()
	if e.StateStore != nil {
		e.StateStore.Clock = e.now
	}
	governorDiagnostics := e.applyCandidateGovernor(ctx)
	universe := ResolveProgrammaticSymbols(ctx.CandidateCoins, ctx.Positions, e.Policy)
	marketSymbols := make([]string, 0, len(universe))
	for _, item := range universe {
		marketSymbols = append(marketSymbols, item.Symbol)
	}
	e.setUniverse(ctx.TraderID, universe)

	prep, err := decision.PrepareCycleContext(ctx, decision.CyclePreparationOptions{
		MarketSymbols: marketSymbols,
		MarketHistoryDepth: map[string]int{
			"3m":  e.Policy.HistoryDepth.M3,
			"15m": e.Policy.HistoryDepth.M15,
			"1h":  e.Policy.HistoryDepth.H1,
			"4h":  e.Policy.HistoryDepth.H4,
		},
		ClosedKlinesOnly:        true,
		IncludeMicroADX:         e.Policy.ADX.MicroADXFilter,
		AllowRiskReducingOnHalt: true,
		MarketDataProvider:      e.MarketDataProvider,
		DisableOITopFetch:       e.DisableOITopFetch,
		Clock:                   e.now,
	})
	if err != nil {
		return nil, err
	}
	if prep.FullStop && prep.HaltDecision != nil {
		prep.HaltDecision.UserPrompt = ""
		prep.HaltDecision.AICallAttempted = false
		e.applyDecisionMetadata(ctx, prep.HaltDecision, nil, nil, AccountSizeDecision{})
		return prep.HaltDecision, nil
	}

	var strategyDecisions []decision.Decision
	var diagnostics []string
	var preRejections []decision.OpenRejection
	diagnostics = append(diagnostics, governorDiagnostics...)
	positionDecisions, positionDiagnostics := e.evaluatePositionManagement(ctx, now)
	strategyDecisions = append(strategyDecisions, positionDecisions...)
	diagnostics = append(diagnostics, positionDiagnostics...)
	accountGate := e.accountSizeGate(ctx)
	activeMode := e.loosenModeController(ctx, accountGate.AccountTooSmall)
	if accountGate.HoldOnly {
		diagnostics = append(diagnostics, accountGate.Reason)
	}
	if activeMode != "" && activeMode != "normal" {
		diagnostics = append(diagnostics, fmt.Sprintf("active_mode=%s", activeMode))
	}
	if prep.RiskIncreaseBlocked {
		reason := prep.StopReason
		if strings.TrimSpace(reason) == "" {
			reason = "风险增加已阻断"
		}
		diagnostics = append(diagnostics, "主信号层跳过open/add: "+reason)
	} else if accountGate.HoldOnly {
		diagnostics = append(diagnostics, "账户尺寸gate进入hold_only，仅保留持仓管理")
	} else {
		mainDecisions, mainDiagnostics, mainRejections := e.evaluateMainSignals(ctx, universe, now)
		strategyDecisions = append(strategyDecisions, mainDecisions...)
		diagnostics = append(diagnostics, mainDiagnostics...)
		preRejections = append(preRejections, mainRejections...)
	}
	validDecisions, rejections := e.validateProgrammaticDecisions(ctx, strategyDecisions, prep)
	rejections = append(preRejections, rejections...)
	_ = e.StateStore.Save()
	allDecisions := decision.MergePublicAndStrategyDecisionsWithContext(ctx, prep.PositionDecisions, validDecisions)
	if len(allDecisions) == 0 {
		reason := "程序化策略未发现可执行信号"
		if prep.WaitDecision != nil && prep.WaitDecision.Reasoning != "" {
			reason = prep.WaitDecision.Reasoning
		}
		allDecisions = []decision.Decision{{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: reason,
		}}
	}
	summary := "程序化策略周期完成"
	if len(diagnostics) > 0 {
		summary += ": " + strings.Join(limitStrings(diagnostics, 8), "; ")
	}
	if len(rejections) > 0 {
		summary += "; 风控拒绝 " + strings.Join(openRejectionText(rejections), "; ")
	}
	fullDecision := &decision.FullDecision{
		UserPrompt:        "",
		CoTTrace:          summary,
		Decisions:         allDecisions,
		Timestamp:         now,
		WaitReasonSummary: waitReasonSummary(diagnostics, rejections, allDecisions),
		AICallAttempted:   false,
		AICallSucceeded:   false,
		OpenRejections:    rejections,
	}
	e.applyDecisionMetadata(ctx, fullDecision, diagnostics, rejections, accountGate)
	return fullDecision, nil
}

func (e *Engine) evaluateMainSignals(ctx *decision.Context, universe []StrategySymbol, now time.Time) ([]decision.Decision, []string, []decision.OpenRejection) {
	var strategyDecisions []decision.Decision
	var diagnostics []string
	var rejections []decision.OpenRejection
	noNewClosedCount := 0
	skipSet := e.fastSkipSuppressed(ctx)
	for _, symbol := range universe {
		data := ctx.MarketDataMap[symbol.Symbol]
		if data == nil || len(data.Klines) == 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 数据不足", symbol.Symbol))
			continue
		}
		previewSignals, previewDecisions, previewDiagnostics, previewRejections := e.evaluatePreviewSignals(ctx, symbol.Symbol, data, now)
		diagnostics = append(diagnostics, previewDiagnostics...)
		rejections = append(rejections, previewRejections...)
		signals, diag := e.analyzeMainSignal(ctx.TraderID, symbol.Symbol, data, now)
		for _, msg := range diag {
			if strings.Contains(msg, "无新闭合K线") && !symbol.HasPosition {
				noNewClosedCount++
				continue
			}
			diagnostics = append(diagnostics, msg)
		}
		e.setLatestSignals(ctx.TraderID, symbol.Symbol, append(append([]ChanlunSignal(nil), signals...), previewSignals...), append(append([]string(nil), diag...), previewDiagnostics...))
		for _, signal := range signals {
			if signal.SourceLayer == "" || signal.SourceLayer == "main_signal" {
				signal.SourceLayer = "structure"
			}
			signal.Tier = tierForStrategySymbol(symbol)
			if reason, skipped := skipSet[signal.StructureKey]; skipped {
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s suppressed_fast_skip: %s", signal.Symbol, signal.SignalType, reason))
				continue
			}
			if e.Policy.DefectFixPackEnabled {
				if reason, skipped := e.StateStore.FastSkipReason(ctx.TraderID, market.Normalize(signal.Symbol), signal.StructureKey); skipped {
					diagnostics = append(diagnostics, fmt.Sprintf("%s %s suppressed_fast_skip: %s", signal.Symbol, signal.SignalType, reason))
					continue
				}
			}
			e.StateStore.StoreConfirmedSignal(ctx.TraderID, signal.Symbol, signal, false)
			e.reconcilePreviewMarkers(ctx.TraderID, signal.Symbol, signal)
			signal.EntryPath = e.decideEntryPath(signal)
			if rejection, skipDiagnostics := e.shouldSkipBeforeEntry(ctx, signal, data, now); rejection != nil {
				diagnostics = append(diagnostics, skipDiagnostics...)
				rejections = append(rejections, *rejection)
				continue
			}
			var openable bool
			var timingDiagnostics []string
			if signal.EntryPath == EntryPathDirectStructure {
				openable, timingDiagnostics = e.prepareDirectStructureEntry(ctx, &signal, data, now)
			} else {
				openable, timingDiagnostics = e.prepareStructureEntry(ctx, &signal, data, now)
			}
			diagnostics = append(diagnostics, timingDiagnostics...)
			if !openable {
				continue
			}
			d := e.signalToMainDecision(ctx, signal)
			if d.Action == "" {
				continue
			}
			if e.StateStore.HasExecutedSignal(ctx.TraderID, signal.Symbol, d.SignalID) {
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s 已处理过signal_id=%s", signal.Symbol, signal.SignalType, d.SignalID))
				continue
			}
			if msg, suppressed := e.suppressedSignalDiagnostic(ctx, d); suppressed {
				diagnostics = append(diagnostics, msg)
				continue
			}
			guarded, rejection, guardDiagnostics := e.applyProgrammaticSignalGuard(ctx, signal, d, data, now)
			diagnostics = append(diagnostics, guardDiagnostics...)
			if rejection != nil {
				rejections = append(rejections, *rejection)
				continue
			}
			d = guarded
			strategyDecisions = append(strategyDecisions, d)
		}
		strategyDecisions = append(strategyDecisions, previewDecisions...)
	}
	if noNewClosedCount > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("主信号层%d个无持仓候选等待%s新闭合K线", noNewClosedCount, e.Policy.Timeframes.Trade))
	}
	return strategyDecisions, diagnostics, rejections
}

func (e *Engine) evaluatePreviewSignals(ctx *decision.Context, symbol string, data *market.Data, now time.Time) ([]ChanlunSignal, []decision.Decision, []string, []decision.OpenRejection) {
	policy := e.Policy.PreviewSignals
	if !policy.Enabled {
		return nil, nil, nil, nil
	}
	tradeTF := firstNonEmptyString(policy.TradeTimeframe, e.Policy.Timeframes.Trade)
	componentTF := firstNonEmptyString(policy.ComponentTimeframe, ComponentTimeframe(tradeTF))
	if tradeTF == "" || componentTF == "" {
		return nil, nil, nil, nil
	}
	tradeKlines := data.Klines[tradeTF]
	if len(tradeKlines) < 30 {
		return nil, nil, nil, nil
	}
	components, phase, componentCount, componentDiagnostics := previewClosedComponents(data, tradeTF, componentTF, policy.WatchAfterClosedComponents)
	if len(components) == 0 {
		return nil, nil, componentDiagnostics, nil
	}
	synthetic := syntheticTradeKlineFromComponents(components)
	if synthetic.CloseTime <= 0 {
		return nil, nil, componentDiagnostics, nil
	}
	previewKlines := append(append([]market.Kline(nil), tradeKlines...), synthetic)
	configHash := fmt.Sprintf("%s|%s|%d", e.Policy.ConfigHash, phase, synthetic.CloseTime)
	signals := e.detectSignalsFromKlines(ctx.TraderID, symbol, tradeTF, componentTF, previewKlines, data.Klines[componentTF], data, now, configHash)
	var diagnostics []string
	diagnostics = append(diagnostics, componentDiagnostics...)
	if len(signals) == 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s 预览层%d根%s暂未形成买卖点", symbol, tradeTF, componentCount, componentTF))
		return nil, nil, diagnostics, nil
	}
	var previewSignals []ChanlunSignal
	var previewDecisions []decision.Decision
	var rejections []decision.OpenRejection
	previewObservationCounts := map[string]int{}
	skipSet := e.fastSkipSuppressed(ctx)
	for _, signal := range signals {
		parentID := signal.SignalID
		triggerType := previewTriggerType(phase, componentTF, componentCount, policy)
		triggerID := StableEntryTriggerID(ctx.TraderID, symbol, parentID, triggerType, componentTF, synthetic.CloseTime, e.Policy.ConfigHash)
		signal.SourceLayer = "preview_signal"
		signal.Status = "watchlist"
		signal.ParentSignalID = parentID
		signal.ParentStructureKey = signal.StructureKey
		signal.EntryTriggerID = triggerID
		signal.EntryTriggerType = triggerType
		signal.EntryTriggerTF = componentTF
		signal.EntryTriggerClose = synthetic.CloseTime
		signal.EntryWindowState = "watchlist"
		signal.EntryReference = currentPriceForGuard(data, tradeTF)
		signal.TriggerConfidence = signal.Confidence
		signal.PreviewPhase = phase
		signal.PreviewSourceTF = componentTF
		signal.PreviewComponents = componentCount
		signal.PreviewConfirmed = false
		signal.DecisionCloseTime = synthetic.CloseTime
		signal.SignalID = triggerID
		signal.LifecycleKey = fmt.Sprintf("preview:%s:%s:%d", signal.StructureKey, phase, parentTradeCandleClose(synthetic.CloseTime, tradeTF))
		if reason, skipped := skipSet[signal.StructureKey]; skipped {
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s suppressed_fast_skip: %s", signal.Symbol, signal.SignalType, reason))
			continue
		}
		if e.Policy.DefectFixPackEnabled {
			if reason, skipped := e.StateStore.FastSkipReason(ctx.TraderID, market.Normalize(signal.Symbol), signal.StructureKey); skipped {
				diagnostics = append(diagnostics, fmt.Sprintf("%s %s suppressed_fast_skip: %s", signal.Symbol, signal.SignalType, reason))
				continue
			}
		}
		previewSignals = append(previewSignals, signal)
		previewObservationCounts[phase]++
		if !policy.AllowPilotOpen || componentCount < policy.PilotAfterClosedComponents {
			continue
		}
		minConfidence := e.effectivePilotMinConfidence(ctx, signal.SignalType)
		if signal.Confidence < minConfidence {
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s pilot跳过: 置信度%d低于%d", symbol, signal.SignalType, signal.Confidence, minConfidence))
			continue
		}
		d := e.signalToMainDecision(ctx, signal)
		if d.Action == "" {
			continue
		}
		if e.StateStore.HasExecutedSignal(ctx.TraderID, signal.Symbol, d.SignalID) {
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s preview signal_id=%s已处理", signal.Symbol, signal.SignalType, d.SignalID))
			continue
		}
		if msg, suppressed := e.suppressedSignalDiagnostic(ctx, d); suppressed {
			diagnostics = append(diagnostics, msg)
			continue
		}
		if gate := e.pilotSizeGate(ctx); gate.RejectPilot {
			rejection := e.rejectProgrammaticSignal(ctx, d, signal, data, now, "pilot_size_below_min_notional", gate.Reason, map[string]any{
				"required_min_notional": gate.RequiredMinNotional,
				"max_allowed_notional":  gate.MaxAllowedNotional,
				"account_too_small":     gate.AccountTooSmall,
			})
			rejections = append(rejections, rejection)
			diagnostics = append(diagnostics, gate.Reason)
			continue
		}
		e.applyPreviewPilotSizing(ctx, data, &d)
		guarded, rejection, guardDiagnostics := e.applyProgrammaticSignalGuard(ctx, signal, d, data, now)
		diagnostics = append(diagnostics, guardDiagnostics...)
		if rejection != nil {
			rejections = append(rejections, *rejection)
			continue
		}
		previewDecisions = append(previewDecisions, guarded)
	}
	for _, phaseKey := range sortedStringKeys(previewObservationCounts) {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s 预览层%s观察到%d个结构，默认仅观察并按生命周期折叠", symbol, tradeTF, phaseKey, previewObservationCounts[phaseKey]))
	}
	return previewSignals, previewDecisions, diagnostics, rejections
}

func (e *Engine) effectivePilotMinConfidence(ctx *decision.Context, signalType string) int {
	base := e.Policy.PreviewSignals.PilotMinConfidence
	if value := e.Policy.PreviewSignals.PilotMinConfidenceBySignal[strings.ToLower(strings.TrimSpace(signalType))]; value > 0 {
		base = value
	}
	if base <= 0 {
		base = 70
	}
	if e.Policy.PreviewSignals.PilotMinConfidenceUseP75 && e.StateStore != nil && ctx != nil {
		samples := e.StateStore.ConfidenceWindow(ctx.TraderID, signalType, 7*24*time.Hour)
		if len(samples) >= 30 {
			sort.Ints(samples)
			p75 := samples[(len(samples)*3)/4]
			base = clampInt(p75, e.Policy.PreviewSignals.P75Floor, e.Policy.PreviewSignals.P75Ceiling)
		}
	}
	if ctx != nil && ctx.FrequencyPolicy != nil {
		mode := strings.ToLower(strings.TrimSpace(firstNonEmptyString(ctx.FrequencyPolicy.EffectiveMode, ctx.FrequencyPolicy.Mode)))
		switch mode {
		case "loosen":
			drop := ctx.FrequencyPolicy.LoosenMode.PilotConfidenceDrop
			if drop <= 0 {
				drop = 10
			}
			floor := ctx.FrequencyPolicy.LoosenMode.HardFloorPilotConfidence
			if floor <= 0 {
				floor = 60
			}
			base = maxInt(base-drop, floor)
		case "safe":
			base = minInt(base+10, 95)
		}
	}
	if ctx != nil && ctx.LossMode != nil && ctx.LossMode.Active {
		base = minInt(base+10, 95)
	}
	return clampInt(base, 1, 100)
}

type AccountSizeDecision struct {
	HoldOnly             bool
	RejectPilot          bool
	AccountTooSmall      bool
	Reason               string
	RequiredMinNotional  float64
	MaxAllowedNotional   float64
	PilotPositionSizeUSD float64
}

func (e *Engine) accountSizeGate(ctx *decision.Context) AccountSizeDecision {
	if ctx == nil || !e.Policy.DefectFixPackEnabled {
		return AccountSizeDecision{}
	}
	minNotional := e.Policy.MinPilotNotionalUSD
	if minNotional <= 0 {
		minNotional = 30
	}
	maxPct := e.Policy.MaxPilotNotionalPct
	if maxPct <= 0 || maxPct > 1 {
		maxPct = 0.6
	}
	lev := maxInt(ctx.BTCETHLeverage, ctx.AltcoinLeverage)
	if lev <= 0 {
		lev = 1
	}
	availableNotional := ctx.Account.AvailableBalance * float64(lev)
	maxAllowed := availableNotional * maxPct
	result := AccountSizeDecision{
		RequiredMinNotional: minNotional,
		MaxAllowedNotional:  maxAllowed,
	}
	if availableNotional < minNotional {
		result.HoldOnly = true
		result.AccountTooSmall = true
		result.Reason = fmt.Sprintf("account_too_small: 可用名义%.2f低于最小试单%.2f", availableNotional, minNotional)
		return result
	}
	if maxAllowed < minNotional {
		result.RejectPilot = true
		result.Reason = fmt.Sprintf("pilot_size_below_min_notional: max_allowed_notional %.2f < min_notional %.2f", maxAllowed, minNotional)
		return result
	}
	result.PilotPositionSizeUSD = maxAllowed
	return result
}

func (e *Engine) pilotSizeGate(ctx *decision.Context) AccountSizeDecision {
	gate := e.accountSizeGate(ctx)
	if gate.HoldOnly {
		gate.RejectPilot = true
	}
	return gate
}

func (e *Engine) applyCandidateGovernor(ctx *decision.Context) []string {
	if ctx == nil || !e.Policy.DefectFixPackEnabled || !e.Policy.CandidateGovernor.Enabled {
		return nil
	}
	policy := e.Policy.CandidateGovernor
	allowNonCrypto := map[string]bool{}
	for _, symbol := range policy.AllowNonCryptoSymbols {
		allowNonCrypto[market.Normalize(symbol)] = true
	}
	core := map[string]bool{}
	for _, symbol := range policy.CoreSymbolsMustAppear {
		core[market.Normalize(symbol)] = true
	}
	if len(core) == 0 {
		core["BTCUSDT"] = true
		core["ETHUSDT"] = true
	}
	maxSpread := policy.MaxQuoteSpreadBps
	if maxSpread <= 0 {
		maxSpread = 20
	}
	seen := map[string]bool{}
	var out []decision.CandidateCoin
	var diagnostics []string
	for _, coin := range ctx.CandidateCoins {
		coin.Symbol = market.Normalize(coin.Symbol)
		if coin.Symbol == "" {
			continue
		}
		if !isCryptoUSDT(coin.Symbol) && !allowNonCrypto[coin.Symbol] {
			coin.IncludedInPrompt = false
			coin.FilterReason = "non_crypto_symbol"
			coin.Errors = appendCandidateError(coin.Errors, "non_crypto_symbol")
			diagnostics = append(diagnostics, fmt.Sprintf("%s candidate_governor剔除: non_crypto_symbol", coin.Symbol))
			out = append(out, coin)
			continue
		}
		if ctx.QuoteSpreadProvider != nil {
			quote, exec, err := ctx.QuoteSpreadProvider(coin.Symbol)
			if err == nil {
				if spread := quoteSpreadBps(quote, exec); spread > maxSpread {
					coin.IncludedInPrompt = false
					coin.FilterReason = "quote_spread_too_high"
					coin.Errors = appendCandidateError(coin.Errors, "quote_spread_too_high")
					diagnostics = append(diagnostics, fmt.Sprintf("%s candidate_governor剔除: quote_spread_too_high %.2fbps > %.2fbps", coin.Symbol, spread, maxSpread))
					out = append(out, coin)
					continue
				}
			}
		}
		seen[coin.Symbol] = true
		out = append(out, coin)
	}
	for symbol := range core {
		if symbol == "" || seen[symbol] {
			continue
		}
		out = append(out, decision.CandidateCoin{
			Symbol:           symbol,
			Sources:          []string{"core"},
			Tier:             "core",
			IncludedInPrompt: true,
		})
		diagnostics = append(diagnostics, fmt.Sprintf("%s candidate_governor强制保留core symbol", symbol))
	}
	ctx.CandidateCoins = out
	return diagnostics
}

func appendCandidateError(values []string, item string) []string {
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

func isCryptoUSDT(symbol string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if !(strings.HasSuffix(normalized, "USDT") || strings.HasSuffix(normalized, "USDC")) {
		return false
	}
	for _, prefix := range []string{"XAU", "XAG", "CL", "COPPER", "NG", "SI"} {
		if strings.HasPrefix(normalized, prefix) {
			return false
		}
	}
	return true
}

func quoteSpreadBps(quoteMid, execMid float64) float64 {
	if quoteMid <= 0 || execMid <= 0 {
		return 0
	}
	diff := quoteMid - execMid
	if diff < 0 {
		diff = -diff
	}
	return diff / quoteMid * 10000
}

func (e *Engine) fastSkipSuppressed(ctx *decision.Context) map[string]string {
	if ctx == nil || e.StateStore == nil || !e.Policy.DefectFixPackEnabled {
		return nil
	}
	return e.StateStore.FastSkipSet(ctx.TraderID)
}

func (e *Engine) decideEntryPath(signal ChanlunSignal) string {
	if e.Policy.EntryTiming.DirectStructureOpen &&
		signal.Confidence >= e.Policy.EntryTiming.DirectStructureMinConfidence {
		return EntryPathDirectStructure
	}
	return EntryPathPreviewThenTrigger
}

func (e *Engine) loosenModeController(ctx *decision.Context, accountTooSmall bool) string {
	mode := "normal"
	if ctx != nil && ctx.FrequencyPolicy != nil {
		configMode := strings.ToLower(strings.TrimSpace(firstNonEmptyString(ctx.FrequencyPolicy.EffectiveMode, ctx.FrequencyPolicy.Mode, "normal")))
		if configMode != "" {
			mode = configMode
		}
	}
	if ctx == nil || e.StateStore == nil || !e.Policy.DefectFixPackEnabled || ctx.FrequencyPolicy == nil || !ctx.FrequencyPolicy.LoosenMode.Enabled {
		e.setActiveMode(mode)
		return mode
	}
	now := e.now()
	policy := ctx.FrequencyPolicy.LoosenMode
	if policy.InactivityWindowMinutes <= 0 {
		policy.InactivityWindowMinutes = 720
	}
	if policy.MaxDurationHours <= 0 {
		policy.MaxDurationHours = 24
	}
	state := e.StateStore.LoosenState(ctx.TraderID)
	lossActive := ctx.LossMode != nil && ctx.LossMode.Active
	safeActive := mode == "safe" || mode == "loss"
	if accountTooSmall || lossActive || safeActive || (ctx.FrequencyState != nil && ctx.FrequencyState.OpenCount24h > 0) ||
		(state.Active && !state.ExpiresAt.IsZero() && !state.ExpiresAt.After(now)) {
		if state.Active {
			state = LoosenState{}
			e.StateStore.SetLoosenState(ctx.TraderID, state)
		}
		if lossActive {
			mode = "loss"
		}
		e.setActiveMode(mode)
		return mode
	}
	inactiveFor := time.Duration(ctx.RuntimeMinutes) * time.Minute
	if ctx.FrequencyState != nil && !ctx.FrequencyState.LastOpenAt.IsZero() {
		inactiveFor = now.Sub(ctx.FrequencyState.LastOpenAt)
	}
	if state.Active {
		ctx.FrequencyPolicy.EffectiveMode = "loosen"
		e.setLoosenAdjustments(policy)
		e.setActiveMode("loosen")
		return "loosen"
	}
	if inactiveFor >= time.Duration(policy.InactivityWindowMinutes)*time.Minute {
		state = LoosenState{
			Active:    true,
			EnteredAt: now,
			ExpiresAt: now.Add(time.Duration(policy.MaxDurationHours) * time.Hour),
		}
		e.StateStore.SetLoosenState(ctx.TraderID, state)
		ctx.FrequencyPolicy.EffectiveMode = "loosen"
		e.setLoosenAdjustments(policy)
		e.setActiveMode("loosen")
		return "loosen"
	}
	e.setActiveMode(mode)
	return mode
}

func (e *Engine) setActiveMode(mode string) {
	if mode == "" {
		mode = "normal"
	}
	e.mu.Lock()
	e.activeMode = mode
	if mode != "loosen" {
		e.loosenMinRRDelta = 0
		e.loosenChaseBump = 0
	}
	e.mu.Unlock()
}

func (e *Engine) setLoosenAdjustments(policy decision.LoosenModePolicy) {
	delta := policy.MinNetRRDelta
	if delta == 0 {
		delta = -0.4
	}
	bump := policy.MaxChaseRatioBump
	if bump == 0 {
		bump = 0.05
	}
	e.mu.Lock()
	e.loosenMinRRDelta = delta
	e.loosenChaseBump = bump
	e.mu.Unlock()
}

func (e *Engine) activeRuntimeMode() string {
	e.mu.RLock()
	mode := e.activeMode
	e.mu.RUnlock()
	if mode == "" {
		return "normal"
	}
	return mode
}

func (e *Engine) activeLoosenAdjustments() (float64, float64) {
	e.mu.RLock()
	delta := e.loosenMinRRDelta
	bump := e.loosenChaseBump
	e.mu.RUnlock()
	if delta == 0 {
		delta = -0.4
	}
	if bump == 0 {
		bump = 0.05
	}
	return delta, bump
}

type entryWindowEvaluation struct {
	State          string
	ReasonCode     string
	Reason         string
	CurrentPrice   float64
	RemainingNetRR float64
	ChaseRatio     float64
	ChaseATR       float64
	EntryZoneLow   float64
	EntryZoneHigh  float64
	Valid          bool
	Invalidated    bool
}

func (e *Engine) prepareStructureEntry(ctx *decision.Context, signal *ChanlunSignal, data *market.Data, now time.Time) (bool, []string) {
	if signal == nil {
		return false, nil
	}
	if signal.SourceLayer != "" && signal.SourceLayer != "main_signal" && signal.SourceLayer != "structure" {
		return true, nil
	}
	if signal.SourceLayer == "" || signal.SourceLayer == "main_signal" {
		signal.SourceLayer = "structure"
	}
	if !e.Policy.EntryTiming.Enabled {
		return true, nil
	}
	action := e.candidateActionForSignal(ctx, *signal)
	if action == "" {
		return true, nil
	}
	signalClose := firstPositiveInt64(signal.SignalCloseTime, signal.TriggerCloseTime, signal.SegmentEndTime)
	decisionClose := signal.DecisionCloseTime
	if decisionClose == 0 {
		decisionClose = latestKlineClose(data, e.Policy.Timeframes.Trade)
	}
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	ageCandles := signalAgeCandles(signalClose, decisionClose, e.Policy.Timeframes.Trade)
	if signal.EntryPath == "" {
		signal.EntryPath = e.decideEntryPath(*signal)
	}
	window := e.evaluateStructureEntryWindow(ctx, *signal, data, ageCandles)
	e.applyEntryWindowMetadata(signal, window)
	if signal.Diagnostics.Metrics == nil {
		signal.Diagnostics.Metrics = map[string]any{}
	}
	signal.Diagnostics.Metrics["source_layer"] = "structure"
	signal.Diagnostics.Metrics["entry_path"] = signal.EntryPath
	signal.Diagnostics.Metrics["entry_age_candles"] = ageCandles
	signal.Diagnostics.Metrics["entry_direct_open"] = e.Policy.EntryTiming.DirectStructureOpen
	signal.Diagnostics.Metrics["entry_require_fresh_trigger"] = e.Policy.EntryTiming.RequireFreshTrigger
	signal.Diagnostics.Metrics["latest_trade_close_time"] = decisionClose
	if !window.Valid {
		signal.Status = "invalidated"
		signal.ReasonCode = window.ReasonCode
		signal.EntryInvalidated = true
		if signal.EntryInvalidReason == "" {
			signal.EntryInvalidReason = window.ReasonCode
		}
		e.storeStructureMarker(ctx, *signal, "invalidated", window.Reason)
		e.storeStructureSuppression(ctx, *signal, action, window.ReasonCode, now, window, signalClose, decisionClose)
		return false, []string{fmt.Sprintf("%s %s 结构信号不进入开仓: %s", signal.Symbol, signal.SignalType, window.Reason)}
	}
	maxAge := e.Policy.EntryTiming.DirectOpenMaxAgeCandles
	freshEnough := ageCandles <= maxAge
	directAllowed := signal.EntryPath == EntryPathDirectStructure && e.Policy.EntryTiming.DirectStructureOpen && freshEnough
	if e.Policy.EntryTiming.RequireFreshTrigger && !freshEnough {
		directAllowed = false
	}
	if !directAllowed {
		if triggered, triggerDiagnostics := e.tryPullbackRetestEntryTrigger(ctx, signal, data, window); triggered {
			return true, triggerDiagnostics
		}
		subAgeCandles := signalAgeCandles(signalClose, decisionClose, e.Policy.EntryTiming.TriggerTimeframe)
		if maxNoTrigger := e.Policy.EntryTiming.MaxNoTriggerSubCandles; e.Policy.DefectFixPackEnabled && maxNoTrigger > 0 && subAgeCandles >= maxNoTrigger {
			reasonCode := "entry_window_missed_no_trigger"
			reason := fmt.Sprintf("%s %s 入场窗口终结: 连续%d根%s未出现fresh entry trigger", signal.Symbol, signal.SignalType, subAgeCandles, e.Policy.EntryTiming.TriggerTimeframe)
			signal.Status = "invalidated"
			signal.ReasonCode = reasonCode
			signal.EntryWindowState = reasonCode
			signal.EntryInvalidated = true
			signal.EntryInvalidReason = reasonCode
			e.storeStructureMarker(ctx, *signal, "invalidated", reason)
			e.storeStructureSuppression(ctx, *signal, action, reasonCode, now, window, signalClose, decisionClose)
			e.StateStore.TerminateLifecycle(ctx.TraderID, market.Normalize(signal.Symbol), signal.StructureKey, reasonCode, signal.SignalID, lifecycleExpiry(now, e.Policy.Timeframes.Trade, e.Policy.SignalFreshness.MaxLifetimeCandles))
			return false, []string{reason}
		}
		reasonCode := "waiting_for_fresh_entry_trigger"
		reason := fmt.Sprintf("%s %s 作为结构背景保留，等待%s fresh entry trigger，结构年龄%d根%s", signal.Symbol, signal.SignalType, e.Policy.EntryTiming.TriggerTimeframe, ageCandles, e.Policy.Timeframes.Trade)
		if !e.Policy.EntryTiming.DirectStructureOpen {
			reasonCode = "structure_background_only"
			reason = fmt.Sprintf("%s %s 作为结构背景保留，direct_structure_open关闭，等待%s fresh entry trigger", signal.Symbol, signal.SignalType, e.Policy.EntryTiming.TriggerTimeframe)
		}
		signal.Status = "background"
		signal.ReasonCode = reasonCode
		signal.EntryWindowState = reasonCode
		e.storeStructureMarker(ctx, *signal, "background", reason)
		e.storeStructureSuppression(ctx, *signal, action, reasonCode, now, window, signalClose, decisionClose)
		return false, []string{reason}
	}
	triggerClose := signalClose
	if triggerClose == 0 {
		triggerClose = decisionClose
	}
	parentID := signal.SignalID
	triggerID := StableEntryTriggerID(ctx.TraderID, signal.Symbol, parentID, "new_structure_segment", e.Policy.Timeframes.Trade, triggerClose, e.Policy.ConfigHash)
	signal.ParentSignalID = parentID
	signal.ParentStructureKey = signal.StructureKey
	signal.EntryTriggerID = triggerID
	signal.EntryTriggerType = "new_structure_segment"
	signal.EntryTriggerTF = e.Policy.Timeframes.Trade
	signal.EntryTriggerClose = triggerClose
	signal.EntryReference = window.CurrentPrice
	signal.TriggerConfidence = signal.Confidence
	signal.SourceLayer = "entry_trigger"
	signal.Status = "ready"
	signal.LifecycleKey = "entry_trigger:" + triggerID
	signal.EntryWindowState = "entry_trigger_ready"
	if signal.Diagnostics.Metrics == nil {
		signal.Diagnostics.Metrics = map[string]any{}
	}
	signal.Diagnostics.Metrics["parent_signal_id"] = parentID
	signal.Diagnostics.Metrics["entry_trigger_id"] = triggerID
	signal.Diagnostics.Metrics["entry_trigger_type"] = signal.EntryTriggerType
	signal.Diagnostics.Metrics["entry_trigger_close_time"] = triggerClose
	e.storeEntryTriggerMarker(ctx, *signal, fmt.Sprintf("%s %s new_structure_segment ready", signal.Symbol, signal.SignalType))
	return true, []string{fmt.Sprintf("%s %s fresh entry trigger ready: %s", signal.Symbol, signal.SignalType, triggerID)}
}

func (e *Engine) prepareDirectStructureEntry(ctx *decision.Context, signal *ChanlunSignal, data *market.Data, now time.Time) (bool, []string) {
	if signal == nil {
		return false, nil
	}
	if signal.SourceLayer == "" || signal.SourceLayer == "main_signal" {
		signal.SourceLayer = "structure"
	}
	if !e.Policy.EntryTiming.Enabled {
		return true, nil
	}
	action := e.candidateActionForSignal(ctx, *signal)
	if action == "" {
		return true, nil
	}
	signalClose := firstPositiveInt64(signal.SignalCloseTime, signal.TriggerCloseTime, signal.SegmentEndTime)
	decisionClose := signal.DecisionCloseTime
	if decisionClose == 0 {
		decisionClose = latestKlineClose(data, e.Policy.Timeframes.Trade)
	}
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	ageCandles := signalAgeCandles(signalClose, decisionClose, e.Policy.Timeframes.Trade)
	signal.EntryPath = EntryPathDirectStructure
	window := e.evaluateStructureEntryWindow(ctx, *signal, data, ageCandles)
	e.applyEntryWindowMetadata(signal, window)
	if signal.Diagnostics.Metrics == nil {
		signal.Diagnostics.Metrics = map[string]any{}
	}
	signal.Diagnostics.Metrics["source_layer"] = "structure"
	signal.Diagnostics.Metrics["entry_path"] = signal.EntryPath
	signal.Diagnostics.Metrics["entry_age_candles"] = ageCandles
	signal.Diagnostics.Metrics["entry_direct_open"] = true
	signal.Diagnostics.Metrics["entry_require_fresh_trigger"] = false
	signal.Diagnostics.Metrics["latest_trade_close_time"] = decisionClose
	if !window.Valid {
		signal.Status = "invalidated"
		signal.ReasonCode = window.ReasonCode
		signal.EntryInvalidated = true
		if signal.EntryInvalidReason == "" {
			signal.EntryInvalidReason = window.ReasonCode
		}
		e.storeStructureMarker(ctx, *signal, "invalidated", window.Reason)
		e.storeStructureSuppression(ctx, *signal, action, window.ReasonCode, now, window, signalClose, decisionClose)
		if window.Invalidated {
			e.StateStore.TerminateLifecycle(ctx.TraderID, market.Normalize(signal.Symbol), signal.StructureKey, window.ReasonCode, signal.SignalID, lifecycleExpiry(now, e.Policy.Timeframes.Trade, e.Policy.SignalFreshness.MaxLifetimeCandles))
		}
		return false, []string{fmt.Sprintf("%s %s direct_structure不进入开仓: %s", signal.Symbol, signal.SignalType, window.Reason)}
	}
	signal.ParentSignalID = signal.SignalID
	signal.ParentStructureKey = signal.StructureKey
	signal.EntryReference = window.CurrentPrice
	signal.TriggerConfidence = signal.Confidence
	signal.Status = "ready"
	signal.EntryWindowState = "direct_structure_ready"
	signal.LifecycleKey = "direct_structure:" + signal.StructureKey
	e.storeStructureMarker(ctx, *signal, "ready", fmt.Sprintf("%s %s direct_structure ready", signal.Symbol, signal.SignalType))
	return true, []string{fmt.Sprintf("%s %s direct_structure ready", signal.Symbol, signal.SignalType)}
}

func (e *Engine) tryPullbackRetestEntryTrigger(ctx *decision.Context, signal *ChanlunSignal, data *market.Data, window entryWindowEvaluation) (bool, []string) {
	if ctx == nil || signal == nil || data == nil || !window.Valid {
		return false, nil
	}
	if !entryTriggerTypeAllowed(e.Policy.EntryTiming, "pullback_retest_resume") {
		return false, nil
	}
	if minConfidence := e.Policy.EntryTiming.MinTriggerConfidence; minConfidence > 0 && signal.Confidence < minConfidence {
		return false, []string{fmt.Sprintf("%s %s pullback_retest_resume等待: 置信度%d低于%d", signal.Symbol, signal.SignalType, signal.Confidence, minConfidence)}
	}
	tradeTF := firstNonEmptyString(e.Policy.Timeframes.Trade, "1h")
	triggerTF := firstNonEmptyString(e.Policy.EntryTiming.TriggerTimeframe, e.Policy.Timeframes.Sub, ComponentTimeframe(tradeTF))
	components, phase, componentCount, componentDiagnostics := previewClosedComponents(data, tradeTF, triggerTF, 2)
	if len(components) == 0 {
		return false, componentDiagnostics
	}
	entryZone := e.entryZoneForSymbol(signal.Symbol)
	pattern, ok := detectPullbackRetestResume(*signal, components, entryZone.MaxChaseRatio)
	if !ok {
		return false, []string{fmt.Sprintf("%s %s 等待%s pullback_retest_resume，当前%s闭合组件%d根", signal.Symbol, signal.SignalType, triggerTF, triggerTF, componentCount)}
	}
	triggerClose := components[len(components)-1].CloseTime
	if triggerClose <= 0 {
		return false, nil
	}
	parentID := signal.SignalID
	triggerID := StableEntryTriggerID(ctx.TraderID, signal.Symbol, parentID, "pullback_retest_resume", triggerTF, triggerClose, e.Policy.ConfigHash)
	signal.ParentSignalID = parentID
	signal.ParentStructureKey = signal.StructureKey
	signal.EntryTriggerID = triggerID
	signal.EntryTriggerType = "pullback_retest_resume"
	signal.EntryTriggerTF = triggerTF
	signal.EntryTriggerClose = triggerClose
	signal.EntryReference = window.CurrentPrice
	signal.TriggerCloseTime = triggerClose
	signal.DecisionCloseTime = triggerClose
	signal.TriggerConfidence = signal.Confidence
	signal.SourceLayer = "entry_trigger"
	signal.Status = "ready"
	signal.LifecycleKey = "entry_trigger:" + triggerID
	signal.EntryWindowState = "entry_trigger_ready"
	signal.RemainingNetRR = window.RemainingNetRR
	if signal.Diagnostics.Metrics == nil {
		signal.Diagnostics.Metrics = map[string]any{}
	}
	signal.Diagnostics.Metrics["parent_signal_id"] = parentID
	signal.Diagnostics.Metrics["entry_trigger_id"] = triggerID
	signal.Diagnostics.Metrics["entry_trigger_type"] = signal.EntryTriggerType
	signal.Diagnostics.Metrics["entry_trigger_timeframe"] = triggerTF
	signal.Diagnostics.Metrics["entry_trigger_close_time"] = triggerClose
	signal.Diagnostics.Metrics["entry_trigger_phase"] = phase
	signal.Diagnostics.Metrics["entry_trigger_components"] = componentCount
	for key, value := range pattern {
		signal.Diagnostics.Metrics[key] = value
	}
	e.storeEntryTriggerMarker(ctx, *signal, fmt.Sprintf("%s %s pullback_retest_resume ready", signal.Symbol, signal.SignalType))
	return true, []string{fmt.Sprintf("%s %s pullback_retest_resume fresh entry trigger ready: %s", signal.Symbol, signal.SignalType, triggerID)}
}

func entryTriggerTypeAllowed(policy decision.ProgrammaticEntryTimingPolicy, triggerType string) bool {
	if triggerType == "" {
		return false
	}
	if len(policy.AllowedTriggerTypes) == 0 {
		return triggerType == "pullback_retest_resume"
	}
	for _, allowed := range policy.AllowedTriggerTypes {
		if strings.EqualFold(strings.TrimSpace(allowed), triggerType) {
			return true
		}
	}
	return false
}

func detectPullbackRetestResume(signal ChanlunSignal, components []market.Kline, maxChaseRatio float64) (map[string]any, bool) {
	if len(components) < 2 {
		return nil, false
	}
	if maxChaseRatio <= 0 {
		maxChaseRatio = 0.35
	}
	prev := components[len(components)-2]
	last := components[len(components)-1]
	width := signal.TakeProfit - signal.StopLoss
	if width < 0 {
		width = -width
	}
	if width <= 0 || signal.Price <= 0 {
		return nil, false
	}
	switch signal.Direction {
	case SideLong:
		zoneUpper := signal.Price + width*maxChaseRatio
		retested := prev.Low <= zoneUpper && prev.Close <= prev.Open
		resumed := last.Close > last.Open && last.Close > prev.Close
		if !retested || !resumed {
			return nil, false
		}
		return map[string]any{
			"pullback_retest_level": prev.Low,
			"pullback_resume_close": last.Close,
			"entry_zone_upper":      zoneUpper,
		}, true
	case SideShort:
		zoneLower := signal.Price - width*maxChaseRatio
		retested := prev.High >= zoneLower && prev.Close >= prev.Open
		resumed := last.Close < last.Open && last.Close < prev.Close
		if !retested || !resumed {
			return nil, false
		}
		return map[string]any{
			"pullback_retest_level": prev.High,
			"pullback_resume_close": last.Close,
			"entry_zone_lower":      zoneLower,
		}, true
	default:
		return nil, false
	}
}

func (e *Engine) candidateActionForSignal(ctx *decision.Context, signal ChanlunSignal) string {
	if ctx == nil {
		return ""
	}
	positionSide := positionSideForSymbol(ctx.Positions, signal.Symbol)
	switch {
	case positionSide == "" && signal.Direction == SideLong && e.Policy.AllowLong:
		return "open_long"
	case positionSide == "" && signal.Direction == SideShort && e.Policy.AllowShort:
		return "open_short"
	case positionSide == SideLong && signal.Direction == SideLong:
		return "add_long"
	case positionSide == SideShort && signal.Direction == SideShort:
		return "add_short"
	default:
		return ""
	}
}

func (e *Engine) evaluateStructureEntryWindow(ctx *decision.Context, signal ChanlunSignal, data *market.Data, ageCandlesValues ...int) entryWindowEvaluation {
	ageCandles := 1
	if len(ageCandlesValues) > 0 {
		ageCandles = ageCandlesValues[0]
	}
	currentPrice := currentPriceForGuard(data, e.Policy.Timeframes.Trade)
	entryZone := e.entryZoneForSymbol(signal.Symbol)
	result := entryWindowEvaluation{
		State:        "entry_window_valid",
		ReasonCode:   "entry_trigger_ready",
		CurrentPrice: currentPrice,
		Valid:        true,
	}
	if currentPrice <= 0 {
		result.State = "entry_window_unknown"
		result.ReasonCode = "entry_window_unknown"
		result.Reason = "缺少当前价，无法验证入场窗口"
		result.Valid = false
		return result
	}
	switch signal.Direction {
	case SideLong:
		if signal.TakeProfit > 0 && currentPrice >= signal.TakeProfit {
			result.State = "target_already_crossed"
			result.ReasonCode = "target_already_crossed"
			result.Reason = fmt.Sprintf("做多结构已越过止盈目标，当前价%.6f >= 止盈%.6f", currentPrice, signal.TakeProfit)
			result.Valid = false
			result.Invalidated = true
			return result
		}
	case SideShort:
		if signal.TakeProfit > 0 && currentPrice <= signal.TakeProfit {
			result.State = "target_already_crossed"
			result.ReasonCode = "target_already_crossed"
			result.Reason = fmt.Sprintf("做空结构已越过止盈目标，当前价%.6f <= 止盈%.6f", currentPrice, signal.TakeProfit)
			result.Valid = false
			result.Invalidated = true
			return result
		}
	default:
		result.State = "entry_window_invalid"
		result.ReasonCode = "invalid_direction"
		result.Reason = "缺少有效方向"
		result.Valid = false
		return result
	}
	if invalidProgrammaticOpenStructure(signal.Direction, currentPrice, signal.StopLoss, signal.TakeProfit) {
		relation := "止损 < 当前价 < 止盈"
		if signal.Direction == SideShort {
			relation = "止损 > 当前价 > 止盈"
		}
		result.State = "entry_window_invalid"
		result.ReasonCode = "invalid_stop_take_profit_structure"
		result.Reason = fmt.Sprintf("%s止损/止盈结构不合法，要求%s，当前价%.6f 止损%.6f 止盈%.6f", chineseSide(signal.Direction), relation, currentPrice, signal.StopLoss, signal.TakeProfit)
		result.Valid = false
		result.Invalidated = true
		return result
	}
	if chaseRatio, ok := entryChaseRatio(signal.Direction, signal.Price, currentPrice, signal.StopLoss, signal.TakeProfit); ok {
		if signal.Diagnostics.Metrics == nil {
			signal.Diagnostics.Metrics = map[string]any{}
		}
		maxChase := e.effectiveMaxChaseRatio(signal.Symbol, signal.Tier, ageCandles)
		chaseATR := entryChaseATR(signal.Direction, signal.Price, currentPrice, market.GetATR(data, e.Policy.EntryTiming.TriggerTimeframe))
		zoneLow, zoneHigh := entryZoneBounds(signal.Direction, signal.Price, signal.StopLoss, maxChase)
		result.ChaseRatio = chaseRatio
		result.ChaseATR = chaseATR
		result.EntryZoneLow = zoneLow
		result.EntryZoneHigh = zoneHigh
		atrPass := entryZone.MaxChaseATRMultiplier > 0 && chaseATR > 0 && chaseATR <= entryZone.MaxChaseATRMultiplier
		ratioPass := chaseRatio <= maxChase
		if !ratioPass && !atrPass {
			result.State = "entry_window_missed"
			result.ReasonCode = "entry_chase_ratio_too_high"
			result.Reason = fmt.Sprintf("入场追价比例%.2f超过阈值%.2f，ATR追价%.2f超过阈值%.2f", chaseRatio, maxChase, chaseATR, entryZone.MaxChaseATRMultiplier)
			result.Valid = false
			return result
		}
	}
	if rr, ok := remainingNetRRForDecision(signal.Direction, currentPrice, signal.StopLoss, signal.TakeProfit, tradingCostPct(ctx)); ok {
		result.RemainingNetRR = rr
		minRR := e.minRemainingNetRRForSignal(signal.SignalType, e.Policy.Timeframes.Trade, signal.Symbol, signal.Tier)
		if rr < minRR {
			result.State = "entry_window_missed"
			result.ReasonCode = "remaining_net_rr_too_low"
			result.Reason = fmt.Sprintf("剩余净RR %.2f低于阈值%.2f，当前价%.6f 止损%.6f 止盈%.6f", rr, minRR, currentPrice, signal.StopLoss, signal.TakeProfit)
			result.Valid = false
			return result
		}
	}
	return result
}

func (e *Engine) entryZoneForSymbol(symbol string) decision.ProgrammaticEntryZonePolicy {
	zone := e.Policy.EntryTiming.EntryZone
	normalized := market.Normalize(symbol)
	if override, ok := zone.SymbolOverrides[normalized]; ok {
		if override.MaxChaseRatio > 0 {
			zone.MaxChaseRatio = override.MaxChaseRatio
		}
		if override.MinRemainingNetRR > 0 {
			zone.MinRemainingNetRR = override.MinRemainingNetRR
		}
	}
	if zone.MaxChaseRatio <= 0 {
		zone.MaxChaseRatio = 0.35
	}
	if zone.MinRemainingNetRR <= 0 {
		zone.MinRemainingNetRR = e.Policy.SignalFreshness.MinRemainingNetRR
	}
	if zone.MinRemainingNetRR <= 0 {
		zone.MinRemainingNetRR = e.Policy.TakeProfit.MinNetRR
	}
	if zone.MinRemainingNetRR <= 0 {
		zone.MinRemainingNetRR = 2.5
	}
	return zone
}

func (e *Engine) effectiveMaxChaseRatio(symbol, tier string, ageCandles int) float64 {
	zone := e.Policy.EntryTiming.EntryZone
	maxChase := zone.MaxChaseRatio
	if maxChase <= 0 {
		maxChase = 0.35
	}
	if tier != "" {
		if override, ok := zone.TierOverrides[strings.ToLower(strings.TrimSpace(tier))]; ok && override.MaxChaseRatio > 0 {
			maxChase = override.MaxChaseRatio
		}
	}
	if override, ok := zone.SymbolOverrides[market.Normalize(symbol)]; ok && override.MaxChaseRatio > 0 {
		maxChase = override.MaxChaseRatio
	}
	if ageCandles == 0 && zone.FreshAgeChaseRelax > 0 {
		maxChase += zone.FreshAgeChaseRelax
	}
	if e.activeRuntimeMode() == "loosen" {
		_, bump := e.activeLoosenAdjustments()
		maxChase += bump
	}
	if maxChase > 1 {
		return 1
	}
	return maxChase
}

func (e *Engine) minRemainingNetRRForSymbol(symbol string) float64 {
	return e.entryZoneForSymbol(symbol).MinRemainingNetRR
}

func (e *Engine) minRemainingNetRRForSignal(signalType, timeframe, symbol, tier string) float64 {
	zone := e.entryZoneForSymbol(symbol)
	normalizedType := strings.ToLower(strings.TrimSpace(signalType))
	normalizedTF := strings.ToLower(strings.TrimSpace(timeframe))
	if normalizedType != "" && normalizedTF != "" {
		if value := zone.SignalTypeMinRR[normalizedType+"@"+normalizedTF]; value > 0 {
			return e.applyLoosenMinRR(value)
		}
		if strings.HasPrefix(normalizedType, "buy") {
			if value := zone.SignalTypeMinRR["buy*@"+normalizedTF]; value > 0 {
				return e.applyLoosenMinRR(value)
			}
		}
		if strings.HasPrefix(normalizedType, "sell") {
			if value := zone.SignalTypeMinRR["sell*@"+normalizedTF]; value > 0 {
				return e.applyLoosenMinRR(value)
			}
		}
		if value := zone.SignalTypeMinRR["*@"+normalizedTF]; value > 0 {
			return e.applyLoosenMinRR(value)
		}
	}
	if value := zone.SignalTypeMinRR[normalizedType]; value > 0 {
		return e.applyLoosenMinRR(value)
	}
	if strings.HasPrefix(normalizedType, "buy") {
		if value := zone.SignalTypeMinRR["buy*"]; value > 0 {
			return e.applyLoosenMinRR(value)
		}
	}
	if strings.HasPrefix(normalizedType, "sell") {
		if value := zone.SignalTypeMinRR["sell*"]; value > 0 {
			return e.applyLoosenMinRR(value)
		}
	}
	if override, ok := e.Policy.EntryTiming.EntryZone.SymbolOverrides[market.Normalize(symbol)]; ok && override.MinRemainingNetRR > 0 {
		return e.applyLoosenMinRR(override.MinRemainingNetRR)
	}
	if tier != "" {
		if override, ok := zone.TierOverrides[strings.ToLower(strings.TrimSpace(tier))]; ok && override.MinRemainingNetRR > 0 {
			return e.applyLoosenMinRR(override.MinRemainingNetRR)
		}
	}
	if zone.MinRemainingNetRR > 0 {
		return e.applyLoosenMinRR(zone.MinRemainingNetRR)
	}
	return e.applyLoosenMinRR(e.minRemainingNetRRForSymbol(symbol))
}

func (e *Engine) applyLoosenMinRR(value float64) float64 {
	if e.activeRuntimeMode() == "loosen" {
		delta, _ := e.activeLoosenAdjustments()
		value += delta
		if value < 1 {
			value = 1
		}
	}
	return value
}

func (e *Engine) applyEntryWindowMetadata(signal *ChanlunSignal, window entryWindowEvaluation) {
	if signal == nil {
		return
	}
	signal.EntryWindowState = window.State
	signal.EntryReference = window.CurrentPrice
	signal.EntryInvalidated = !window.Valid && window.Invalidated
	signal.EntryInvalidReason = window.ReasonCode
	signal.RemainingNetRR = window.RemainingNetRR
	if signal.Diagnostics.Metrics == nil {
		signal.Diagnostics.Metrics = map[string]any{}
	}
	signal.Diagnostics.Metrics["entry_window_state"] = window.State
	signal.Diagnostics.Metrics["entry_window_reason"] = window.ReasonCode
	signal.Diagnostics.Metrics["current_price"] = window.CurrentPrice
	if window.RemainingNetRR > 0 {
		signal.Diagnostics.Metrics["remaining_net_rr"] = window.RemainingNetRR
	}
	if window.ChaseRatio > 0 {
		signal.Diagnostics.Metrics["chase_ratio"] = window.ChaseRatio
	}
	if window.ChaseATR > 0 {
		signal.Diagnostics.Metrics["chase_ratio_atr"] = window.ChaseATR
	}
	if window.EntryZoneLow > 0 {
		signal.Diagnostics.Metrics["entry_zone_low"] = window.EntryZoneLow
	}
	if window.EntryZoneHigh > 0 {
		signal.Diagnostics.Metrics["entry_zone_high"] = window.EntryZoneHigh
	}
}

func (e *Engine) storeStructureMarker(ctx *decision.Context, signal ChanlunSignal, status, reason string) {
	if ctx == nil || signal.SignalID == "" || signal.Symbol == "" {
		return
	}
	marker := signalToMarker(signal, "structure", status, "", reason)
	e.StateStore.StoreSignalMarker(ctx.TraderID, market.Normalize(signal.Symbol), marker)
}

func (e *Engine) storeEntryTriggerMarker(ctx *decision.Context, signal ChanlunSignal, reason string) {
	if ctx == nil || signal.EntryTriggerID == "" || signal.Symbol == "" {
		return
	}
	marker := signalToMarker(signal, "entry_trigger", "ready", "", reason)
	e.StateStore.StoreSignalMarker(ctx.TraderID, market.Normalize(signal.Symbol), marker)
}

func (e *Engine) storeStructureSuppression(ctx *decision.Context, signal ChanlunSignal, action, reasonCode string, now time.Time, window entryWindowEvaluation, signalClose, decisionClose int64) {
	if ctx == nil || signal.SignalID == "" || action == "" || reasonCode == "" {
		return
	}
	e.StateStore.StoreSignalSuppression(ctx.TraderID, market.Normalize(signal.Symbol), SignalSuppression{
		SignalID:          signal.SignalID,
		StructureKey:      signal.StructureKey,
		Action:            action,
		ReasonCode:        reasonCode,
		SuppressedAt:      now,
		LastSeenAt:        now,
		SeenCount:         1,
		ParentSignalID:    signal.ParentSignalID,
		EntryTriggerID:    signal.EntryTriggerID,
		EntryWindowState:  signal.EntryWindowState,
		SignalCloseTime:   signalClose,
		DecisionCloseTime: decisionClose,
		FreshnessState:    "background",
		CurrentPrice:      window.CurrentPrice,
		StopLoss:          signal.StopLoss,
		TakeProfit:        signal.TakeProfit,
	})
}

func entryChaseRatio(direction string, signalPrice, currentPrice, stopLoss, takeProfit float64) (float64, bool) {
	risk := signalPrice - stopLoss
	if risk < 0 {
		risk = -risk
	}
	if risk <= 0 || signalPrice <= 0 || currentPrice <= 0 || takeProfit <= 0 {
		return 0, false
	}
	switch direction {
	case SideLong:
		if currentPrice <= signalPrice {
			return 0, true
		}
		return (currentPrice - signalPrice) / risk, true
	case SideShort:
		if currentPrice >= signalPrice {
			return 0, true
		}
		return (signalPrice - currentPrice) / risk, true
	default:
		return 0, false
	}
}

func entryChaseATR(direction string, signalPrice, currentPrice, atr float64) float64 {
	if signalPrice <= 0 || currentPrice <= 0 || atr <= 0 {
		return 0
	}
	switch direction {
	case SideLong:
		if currentPrice <= signalPrice {
			return 0
		}
		return (currentPrice - signalPrice) / atr
	case SideShort:
		if currentPrice >= signalPrice {
			return 0
		}
		return (signalPrice - currentPrice) / atr
	default:
		return 0
	}
}

func entryZoneBounds(direction string, signalPrice, stopLoss, maxChaseRatio float64) (float64, float64) {
	risk := signalPrice - stopLoss
	if risk < 0 {
		risk = -risk
	}
	if risk <= 0 || signalPrice <= 0 {
		return 0, 0
	}
	switch direction {
	case SideLong:
		return signalPrice, signalPrice + risk*maxChaseRatio
	case SideShort:
		return signalPrice - risk*maxChaseRatio, signalPrice
	default:
		return 0, 0
	}
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (e *Engine) suppressedSignalDiagnostic(ctx *decision.Context, d decision.Decision) (string, bool) {
	if ctx == nil || d.SignalID == "" || d.Action == "" {
		return "", false
	}
	suppression, ok := e.StateStore.SuppressedSignalForAction(ctx.TraderID, market.Normalize(d.Symbol), d.SignalID, d.Action)
	if !ok {
		return "", false
	}
	reasonCode := suppression.ReasonCode
	if reasonCode == "" {
		reasonCode = "suppressed"
	}
	return fmt.Sprintf("%s %s signal_id=%s 已因%s抑制，跳过重复开仓", d.Symbol, d.Action, d.SignalID, reasonCode), true
}

func (e *Engine) detectSignalsFromKlines(traderID, symbol, tradeTF, triggerTF string, tradeKlines, centerKlines []market.Kline, data *market.Data, now time.Time, configHash string) []ChanlunSignal {
	candles := marketKlinesToCandles(tradeTF, tradeKlines)
	normalized := NormalizeInclusion(candles)
	fractals := FindFractals(normalized, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
	strokes := BuildStrokes(fractals, normalized, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
	segments := BuildSegments(strokes, e.Policy.Structure.Strictness)
	centerSegments := segments
	if len(centerKlines) > 0 {
		subCandles := NormalizeInclusion(marketKlinesToCandles(triggerTF, centerKlines))
		subFractals := FindFractals(subCandles, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
		subStrokes := BuildStrokes(subFractals, subCandles, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
		centerSegments = BuildSegments(subStrokes, e.Policy.Structure.Strictness)
	}
	if len(segments) < 3 {
		return nil
	}
	centers := BuildCenters(centerSegments, tradeTF)
	hist := extendFloatSeries(macdHistForTF(data, tradeTF), len(candles))
	shortEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.ShortPeriod)
	longEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.LongPeriod)
	maKiss := DetectMAKiss(shortEMA, longEMA, e.Policy.MovingAverage.KissDistancePct, e.Policy.MovingAverage.WetKissBars)
	signals := DetectSignals(SignalInput{
		TraderID:          traderID,
		Symbol:            symbol,
		AnalysisTF:        tradeTF,
		TriggerTF:         triggerTF,
		Centers:           centers,
		Segments:          segments,
		MACDHist:          hist,
		ConfigHash:        configHash,
		Now:               now,
		EnabledSignal:     enabledSignalMap(e.Policy.EnabledSignals),
		DivergenceRatio:   e.Policy.Divergence.Ratio,
		PriceTolerancePct: e.Policy.Divergence.PriceTolerancePct,
		RequireBZeroAxis:  e.Policy.Divergence.RequireBZeroAxis,
		MAKiss:            maKiss,
		MarketData:        data,
		ADXTimeframe:      "1h",
	})
	lastClosed := tradeKlines[len(tradeKlines)-1].CloseTime
	filtered := signals[:0]
	for i := range signals {
		if e.Policy.DefectFixPackEnabled && signals[i].IsBornInvalid() {
			if signals[i].Diagnostics.Metrics == nil {
				signals[i].Diagnostics.Metrics = map[string]any{}
			}
			signals[i].Diagnostics.Metrics["signal_invalid_at_birth"] = true
			e.StateStore.StoreConfidenceSample(traderID, market.Normalize(symbol), signals[i].SignalType, signals[i].Confidence, now)
			continue
		}
		signalClose := signals[i].SignalCloseTime
		if signalClose == 0 {
			signalClose = signals[i].TriggerCloseTime
		}
		if signalClose == 0 {
			signalClose = signals[i].SegmentEndTime
		}
		signals[i].SignalCloseTime = signalClose
		signals[i].TriggerCloseTime = signalClose
		signals[i].DecisionCloseTime = lastClosed
		e.StateStore.StoreConfidenceSample(traderID, market.Normalize(symbol), signals[i].SignalType, signals[i].Confidence, now)
		filtered = append(filtered, signals[i])
	}
	return filtered
}

func previewClosedComponents(data *market.Data, tradeTF, componentTF string, watchAfter int) ([]market.Kline, string, int, []string) {
	if data == nil {
		return nil, "", 0, nil
	}
	tradeKlines := data.Klines[tradeTF]
	componentKlines := data.Klines[componentTF]
	if len(tradeKlines) == 0 || len(componentKlines) == 0 {
		return nil, "", 0, nil
	}
	tradeDuration := timeframeDuration(tradeTF)
	componentDuration := timeframeDuration(componentTF)
	if tradeDuration <= 0 || componentDuration <= 0 || tradeDuration <= componentDuration {
		return nil, "", 0, nil
	}
	componentsPerTrade := int(tradeDuration / componentDuration)
	if componentsPerTrade <= 1 {
		return nil, "", 0, nil
	}
	lastTradeClose := tradeKlines[len(tradeKlines)-1].CloseTime
	var components []market.Kline
	for _, kline := range componentKlines {
		if kline.CloseTime > lastTradeClose {
			components = append(components, kline)
		}
	}
	if len(components) >= componentsPerTrade {
		return nil, "", len(components), []string{fmt.Sprintf("%s已闭合%d根%s，等待%s正式K线确认", tradeTF, len(components), componentTF, tradeTF)}
	}
	if watchAfter <= 0 {
		watchAfter = 2
	}
	if len(components) < watchAfter {
		return nil, "", len(components), nil
	}
	phase := fmt.Sprintf("preview_%dx%s", len(components), componentTF)
	return components, phase, len(components), nil
}

func syntheticTradeKlineFromComponents(components []market.Kline) market.Kline {
	if len(components) == 0 {
		return market.Kline{}
	}
	out := market.Kline{
		OpenTime:  components[0].OpenTime,
		CloseTime: components[len(components)-1].CloseTime,
		Open:      components[0].Open,
		High:      components[0].High,
		Low:       components[0].Low,
		Close:     components[len(components)-1].Close,
	}
	for _, kline := range components {
		if kline.High > out.High {
			out.High = kline.High
		}
		if kline.Low < out.Low {
			out.Low = kline.Low
		}
		out.Volume += kline.Volume
	}
	return out
}

func previewSignalID(baseID, phase, sourceTF string, decisionClose int64) string {
	return fmt.Sprintf("%s|%s|%s|%d", baseID, phase, sourceTF, decisionClose)
}

func previewTriggerType(phase, sourceTF string, componentCount int, policy decision.ProgrammaticPreviewSignalsPolicy) string {
	if componentCount >= policy.PilotAfterClosedComponents {
		return fmt.Sprintf("preview_%dx%s_pilot", componentCount, sourceTF)
	}
	return fmt.Sprintf("%s_watchlist", phase)
}

func extendFloatSeries(values []float64, target int) []float64 {
	if target <= 0 {
		return nil
	}
	out := append([]float64(nil), values...)
	fill := 0.0
	if len(out) > 0 {
		fill = out[len(out)-1]
	}
	for len(out) < target {
		out = append(out, fill)
	}
	if len(out) > target {
		return out[len(out)-target:]
	}
	return out
}

func (e *Engine) applyPreviewPilotSizing(ctx *decision.Context, data *market.Data, d *decision.Decision) {
	if ctx == nil || d == nil || data == nil {
		return
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	fraction := e.Policy.PreviewSignals.PilotRiskFraction
	if fraction <= 0 || fraction > 1 {
		fraction = 0.3
	}
	riskPct := ctx.MaxRiskPerTrade
	if ctx.EffectiveMaxRiskPerTrade > 0 && (riskPct == 0 || ctx.EffectiveMaxRiskPerTrade < riskPct) {
		riskPct = ctx.EffectiveMaxRiskPerTrade
	}
	if riskPct <= 0 {
		riskPct = 0.02
	}
	riskPct *= fraction
	feeSlippagePct := 0.002
	if ctx.StrategyRiskPolicy != nil && ctx.StrategyRiskPolicy.FeeSlippagePct > 0 {
		feeSlippagePct = ctx.StrategyRiskPolicy.FeeSlippagePct
	}
	currentPrice := currentPriceForGuard(data, e.Policy.Timeframes.Trade)
	if currentPrice <= 0 {
		return
	}
	sizing := decision.CalculatePositionSizing(decision.PositionSizingInput{
		AccountEquity:            ctx.Account.TotalEquity,
		AvailableBalance:         ctx.Account.AvailableBalance,
		CurrentPrice:             currentPrice,
		StopLoss:                 d.StopLoss,
		Leverage:                 d.Leverage,
		EffectiveRiskPct:         riskPct,
		FeeSlippagePct:           feeSlippagePct,
		MinOrderValueUSDT:        10,
		ProfileName:              "preview_pilot",
		RequestedPositionSizeUSD: d.PositionSizeUSD,
	})
	if sizing.PositionSizeUSD > 0 {
		if gate := e.accountSizeGate(ctx); gate.MaxAllowedNotional > 0 {
			d.StrategyMetadata["pilot_max_allowed_notional"] = gate.MaxAllowedNotional
			d.StrategyMetadata["pilot_min_notional_usd"] = gate.RequiredMinNotional
			if sizing.PositionSizeUSD > gate.MaxAllowedNotional {
				sizing.PositionSizeUSD = gate.MaxAllowedNotional
			}
		}
		d.PositionSizeUSD = sizing.PositionSizeUSD
		d.RequestedPositionSizeUSD = sizing.PositionSizeUSD
		d.StrategyMetadata["pilot_position_size_usd"] = sizing.PositionSizeUSD
		d.StrategyMetadata["pilot_effective_risk_pct"] = riskPct
	}
	d.StrategyMetadata["pilot_risk_fraction"] = fraction
}

func (e *Engine) reconcilePreviewMarkers(traderID, symbol string, confirmed ChanlunSignal) {
	if traderID == "" || symbol == "" || confirmed.SignalID == "" {
		return
	}
	tradeDurationMillis := int64(timeframeDuration(e.Policy.Timeframes.Trade) / time.Millisecond)
	for _, marker := range e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers) {
		if marker.SourceLayer != "preview_signal" || marker.PreviewConfirmed {
			continue
		}
		if marker.SignalType != confirmed.SignalType || marker.Direction != confirmed.Direction {
			continue
		}
		if marker.DecisionCloseTime > 0 && confirmed.DecisionCloseTime > 0 {
			if marker.DecisionCloseTime > confirmed.DecisionCloseTime {
				continue
			}
			if tradeDurationMillis > 0 && marker.DecisionCloseTime <= confirmed.DecisionCloseTime-tradeDurationMillis {
				continue
			}
		}
		marker.Status = "confirmed"
		marker.PreviewConfirmed = true
		marker.Reason = "confirmed_by_1h:" + confirmed.SignalID
		e.StateStore.StoreSignalMarker(traderID, symbol, marker)
	}
}

func (e *Engine) shouldSkipBeforeEntry(ctx *decision.Context, signal ChanlunSignal, data *market.Data, now time.Time) (*decision.OpenRejection, []string) {
	if ctx == nil || !e.Policy.EntryTiming.EntryZone.TheoreticalRRUnreachableSkip {
		return nil, nil
	}
	if e.candidateActionForSignal(ctx, signal) == "" {
		return nil, nil
	}
	minRR := e.minRemainingNetRRForSignal(signal.SignalType, e.Policy.Timeframes.Trade, signal.Symbol, signal.Tier)
	theoreticalRR, ok := theoreticalNetRRForSignal(signal, tradingCostPct(ctx))
	if !ok || theoreticalRR >= minRR {
		return nil, nil
	}
	d := e.signalToMainDecision(ctx, signal)
	if d.Action == "" {
		return nil, nil
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	grossRR, _ := grossRRForDecision(signal.Direction, signal.Price, signal.StopLoss, signal.TakeProfit)
	d.StrategyMetadata["gross_rr"] = grossRR
	d.StrategyMetadata["structure_rr"] = grossRR
	d.StrategyMetadata["theoretical_max_rr"] = theoreticalRR
	d.StrategyMetadata["fee_slippage_pct"] = tradingCostPct(ctx)
	d.StrategyMetadata["min_remaining_net_rr"] = minRR
	reason := fmt.Sprintf("%s %s 理论净RR %.2f低于阈值%.2f，跳过入场生命周期", signal.Symbol, signal.SignalType, theoreticalRR, minRR)
	rejection := e.rejectProgrammaticSignal(ctx, d, signal, data, now, "theoretical_rr_unreachable", reason, map[string]any{
		"theoretical_max_rr":   theoreticalRR,
		"min_remaining_net_rr": minRR,
	})
	return &rejection, []string{reason}
}

func (e *Engine) applyProgrammaticSignalGuard(ctx *decision.Context, signal ChanlunSignal, d decision.Decision, data *market.Data, now time.Time) (decision.Decision, *decision.OpenRejection, []string) {
	if !decision.IsOpenLikeAction(d.Action) || !e.Policy.SignalFreshness.Enabled {
		return d, nil, nil
	}
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	signalClose := signal.SignalCloseTime
	if signalClose == 0 {
		signalClose = signal.TriggerCloseTime
	}
	if signalClose == 0 {
		signalClose = signal.SegmentEndTime
	}
	freshnessClose := signalClose
	if signal.EntryTriggerClose > 0 {
		freshnessClose = signal.EntryTriggerClose
	}
	decisionClose := signal.DecisionCloseTime
	if decisionClose == 0 {
		decisionClose, _ = metadataInt64(d.StrategyMetadata, "decision_close_time")
	}
	if decisionClose == 0 {
		decisionClose = latestKlineClose(data, e.Policy.Timeframes.Trade)
	}
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	if decisionClose > 0 && freshnessClose > 0 && decisionClose < freshnessClose {
		decisionClose = freshnessClose
	}
	ageCandles := signalAgeCandles(freshnessClose, decisionClose, e.Policy.Timeframes.Trade)
	softAge, maxLifetime := e.signalFreshnessLimits(signal.SignalType)
	freshnessState := "fresh"
	if ageCandles > maxLifetime {
		freshnessState = "expired"
	} else if ageCandles > softAge {
		freshnessState = "aged"
	}
	currentPrice := currentPriceForGuard(data, e.Policy.Timeframes.Trade)
	d.StrategyMetadata["freshness_state"] = freshnessState
	d.StrategyMetadata["age_candles"] = ageCandles
	d.StrategyMetadata["soft_age_candles"] = softAge
	d.StrategyMetadata["max_lifetime_candles"] = maxLifetime
	d.StrategyMetadata["current_price"] = currentPrice
	d.StrategyMetadata["signal_close_time"] = signalClose
	d.StrategyMetadata["trigger_close_time"] = freshnessClose
	if signal.EntryTriggerClose > 0 {
		d.StrategyMetadata["entry_trigger_close_time"] = signal.EntryTriggerClose
	}
	d.StrategyMetadata["decision_close_time"] = decisionClose
	minRemainingNetRR := e.minRemainingNetRRForSignal(signal.SignalType, e.Policy.Timeframes.Trade, d.Symbol, signal.Tier)
	d.StrategyMetadata["min_remaining_net_rr"] = minRemainingNetRR
	guardDirection := signal.Direction
	if guardDirection == "" {
		guardDirection = directionForAction(d.Action)
	}
	if d.Explanation != nil {
		if d.Explanation.Details == nil {
			d.Explanation.Details = map[string]any{}
		}
		d.Explanation.Details["freshness_state"] = freshnessState
		d.Explanation.Details["age_candles"] = ageCandles
	}
	var diagnostics []string
	if freshnessState == "aged" {
		decay := (ageCandles - softAge) * e.Policy.SignalFreshness.ConfidenceDecayPerAgedCandle
		if decay > 0 {
			before := d.Confidence
			d.Confidence = clampConfidence(d.Confidence - decay)
			d.StrategyMetadata["confidence_before_freshness_decay"] = before
			d.StrategyMetadata["confidence_decay"] = decay
			d.Reasoning += fmt.Sprintf("；信号老化%d根%s，置信度衰减%d", ageCandles, e.Policy.Timeframes.Trade, decay)
			diagnostics = append(diagnostics, fmt.Sprintf("%s %s 信号老化%d根%s: confidence %d→%d", d.Symbol, signal.SignalType, ageCandles, e.Policy.Timeframes.Trade, before, d.Confidence))
		}
	}
	reject := func(reasonCode, reason string, extra map[string]any) (decision.Decision, *decision.OpenRejection, []string) {
		rejection := e.rejectProgrammaticSignal(ctx, d, signal, data, now, reasonCode, reason, extra)
		return decision.Decision{}, &rejection, diagnostics
	}
	if currentPrice <= 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s 无当前价，跳过时效guard价格结构检查", d.Symbol, signal.SignalType))
		return d, nil, diagnostics
	}
	if e.Policy.SignalFreshness.MissedTargetGuard {
		switch guardDirection {
		case SideLong:
			if d.TakeProfit > 0 && currentPrice >= d.TakeProfit {
				reason := fmt.Sprintf("%s %s 被拒: 做多信号已越过止盈目标，当前价%.6f >= 止盈%.6f，信号年龄%d根%s", d.Symbol, d.Action, currentPrice, d.TakeProfit, ageCandles, e.Policy.Timeframes.Trade)
				return reject("target_already_crossed", reason, map[string]any{"target_already_crossed": true})
			}
		case SideShort:
			if d.TakeProfit > 0 && currentPrice <= d.TakeProfit {
				reason := fmt.Sprintf("%s %s 被拒: 做空信号已越过止盈目标，当前价%.6f <= 止盈%.6f，信号年龄%d根%s", d.Symbol, d.Action, currentPrice, d.TakeProfit, ageCandles, e.Policy.Timeframes.Trade)
				return reject("target_already_crossed", reason, map[string]any{"target_already_crossed": true})
			}
		}
	}
	if invalidProgrammaticOpenStructure(guardDirection, currentPrice, d.StopLoss, d.TakeProfit) {
		relation := "止损 < 当前价 < 止盈"
		if guardDirection == SideShort {
			relation = "止损 > 当前价 > 止盈"
		}
		reason := fmt.Sprintf("%s %s 被拒: %s止损/止盈结构不合法，要求%s，当前价%.6f 止损%.6f 止盈%.6f，信号年龄%d根%s", d.Symbol, d.Action, chineseSide(guardDirection), relation, currentPrice, d.StopLoss, d.TakeProfit, ageCandles, e.Policy.Timeframes.Trade)
		return reject("invalid_stop_take_profit_structure", reason, nil)
	}
	if freshnessState == "expired" {
		reason := fmt.Sprintf("%s %s 被拒: 信号已过期，年龄%d根%s超过硬上限%d根", d.Symbol, d.Action, ageCandles, e.Policy.Timeframes.Trade, maxLifetime)
		return reject("signal_expired", reason, nil)
	}
	costPct := tradingCostPct(ctx)
	if grossRR, ok := grossRRForDecision(guardDirection, currentPrice, d.StopLoss, d.TakeProfit); ok {
		d.StrategyMetadata["gross_rr"] = grossRR
		d.StrategyMetadata["structure_rr"] = grossRR
		d.StrategyMetadata["fee_slippage_pct"] = costPct
		d.StrategyMetadata["theoretical_max_rr"] = grossRR
	}
	remainingNetRR, ok := remainingNetRRForDecision(guardDirection, currentPrice, d.StopLoss, d.TakeProfit, costPct)
	if ok {
		d.StrategyMetadata["remaining_net_rr"] = remainingNetRR
		if d.Explanation != nil {
			d.Explanation.Details["remaining_net_rr"] = remainingNetRR
		}
		if remainingNetRR < minRemainingNetRR {
			reason := fmt.Sprintf("%s %s 被拒: 剩余净RR %.2f低于阈值%.2f，当前价%.6f 止损%.6f 止盈%.6f", d.Symbol, d.Action, remainingNetRR, minRemainingNetRR, currentPrice, d.StopLoss, d.TakeProfit)
			return reject("remaining_net_rr_too_low", reason, map[string]any{"remaining_net_rr": remainingNetRR})
		}
	}
	return d, nil, diagnostics
}

func (e *Engine) rejectProgrammaticSignal(ctx *decision.Context, d decision.Decision, signal ChanlunSignal, data *market.Data, now time.Time, reasonCode, reason string, extra map[string]any) decision.OpenRejection {
	if d.StrategyMetadata == nil {
		d.StrategyMetadata = map[string]any{}
	}
	d.StrategyMetadata["guard_reason_code"] = reasonCode
	if d.Explanation != nil {
		d.Explanation.ReasonCode = reasonCode
	}
	diagnostics := map[string]any{
		"reason_code":          reasonCode,
		"structure_key":        d.StrategyMetadata["structure_key"],
		"parent_structure_key": d.StrategyMetadata["parent_structure_key"],
		"freshness_state":      metadataString(d.StrategyMetadata, "freshness_state"),
		"age_candles":          metadataInt(d.StrategyMetadata, "age_candles"),
		"current_price":        currentPriceForGuard(data, e.Policy.Timeframes.Trade),
		"stop_loss":            d.StopLoss,
		"take_profit":          d.TakeProfit,
		"signal_close_time":    d.StrategyMetadata["signal_close_time"],
		"trigger_close_time":   d.StrategyMetadata["trigger_close_time"],
		"entry_trigger_id":     d.StrategyMetadata["entry_trigger_id"],
		"parent_signal_id":     d.StrategyMetadata["parent_signal_id"],
		"entry_window_state":   d.StrategyMetadata["entry_window_state"],
		"decision_close_time":  d.StrategyMetadata["decision_close_time"],
		"gross_rr":             d.StrategyMetadata["gross_rr"],
		"fee_slippage_pct":     d.StrategyMetadata["fee_slippage_pct"],
		"structure_rr":         d.StrategyMetadata["structure_rr"],
		"theoretical_max_rr":   d.StrategyMetadata["theoretical_max_rr"],
		"chase_ratio":          d.StrategyMetadata["chase_ratio"],
		"chase_ratio_atr":      d.StrategyMetadata["chase_ratio_atr"],
		"entry_zone_low":       d.StrategyMetadata["entry_zone_low"],
		"entry_zone_high":      d.StrategyMetadata["entry_zone_high"],
	}
	for key, value := range extra {
		diagnostics[key] = value
		d.StrategyMetadata[key] = value
	}
	if e.StateStore.HasSuppressedSignal(ctx.TraderID, d.Symbol, d.SignalID, d.Action, reasonCode) {
		reason = fmt.Sprintf("%s %s 已因%s抑制，跳过重复开仓: signal_id=%s", d.Symbol, d.Action, reasonCode, d.SignalID)
	}
	if structureKey := metadataString(d.StrategyMetadata, "structure_key"); structureKey != "" &&
		e.StateStore.HasSuppressedStructure(ctx.TraderID, d.Symbol, structureKey, d.Action, reasonCode) {
		reason = fmt.Sprintf("%s %s 同一结构已因%s抑制，跳过重复开仓: structure_key=%s", d.Symbol, d.Action, reasonCode, structureKey)
	}
	rejection := decision.NewOpenRejectionFromDecision(d, reason)
	rejection.GateState = "blocked"
	rejection.GateReasons = []string{reasonCode}
	rejection.GateDiagnostics = diagnostics
	if marker, ok := e.decisionToMarker(d, "rejected"); ok {
		marker.Reason = reason
		marker.FreshnessState = metadataString(d.StrategyMetadata, "freshness_state")
		marker.AgeCandles = metadataInt(d.StrategyMetadata, "age_candles")
		e.StateStore.StoreSignalMarker(ctx.TraderID, market.Normalize(d.Symbol), marker)
	}
	e.StateStore.StoreSignalSuppression(ctx.TraderID, market.Normalize(d.Symbol), SignalSuppression{
		SignalID:          d.SignalID,
		StructureKey:      metadataString(d.StrategyMetadata, "structure_key"),
		Action:            d.Action,
		ReasonCode:        reasonCode,
		SuppressedAt:      now,
		LastSeenAt:        now,
		SeenCount:         1,
		ParentSignalID:    metadataString(d.StrategyMetadata, "parent_signal_id"),
		EntryTriggerID:    metadataString(d.StrategyMetadata, "entry_trigger_id"),
		EntryWindowState:  metadataString(d.StrategyMetadata, "entry_window_state"),
		SignalCloseTime:   metadataInt64Value(d.StrategyMetadata, "signal_close_time"),
		DecisionCloseTime: metadataInt64Value(d.StrategyMetadata, "decision_close_time"),
		FreshnessState:    metadataString(d.StrategyMetadata, "freshness_state"),
		CurrentPrice:      currentPriceForGuard(data, e.Policy.Timeframes.Trade),
		StopLoss:          d.StopLoss,
		TakeProfit:        d.TakeProfit,
	})
	structureKey := metadataString(d.StrategyMetadata, "structure_key")
	if e.Policy.DefectFixPackEnabled && structureKey != "" {
		switch reasonCode {
		case "invalid_stop_take_profit_structure", "target_already_crossed", "signal_expired", "theoretical_rr_unreachable":
			e.StateStore.TerminateLifecycle(ctx.TraderID, market.Normalize(d.Symbol), structureKey, reasonCode, d.SignalID, lifecycleExpiry(now, e.Policy.Timeframes.Trade, e.Policy.SignalFreshness.MaxLifetimeCandles))
		}
		threshold := e.Policy.SuppressionPermanentThreshold
		if threshold <= 0 {
			threshold = 5
		}
		if e.StateStore.StructureSuppressionSeenCount(ctx.TraderID, market.Normalize(d.Symbol), structureKey) > threshold {
			e.StateStore.MarkPermanentSkip(ctx.TraderID, market.Normalize(d.Symbol), structureKey)
		}
	}
	return rejection
}

func (e *Engine) signalFreshnessLimits(signalType string) (int, int) {
	soft := e.Policy.SignalFreshness.SoftAgeCandles
	maxLifetime := e.Policy.SignalFreshness.MaxLifetimeCandles
	if value := e.Policy.SignalFreshness.SoftAgeBySignalType[signalType]; value > 0 {
		soft = value
	}
	if value := e.Policy.SignalFreshness.MaxLifetimeBySignalType[signalType]; value > 0 {
		maxLifetime = value
	}
	if soft <= 0 {
		soft = 2
	}
	if maxLifetime < soft {
		maxLifetime = soft
	}
	return soft, maxLifetime
}

func invalidProgrammaticOpenStructure(direction string, currentPrice, stopLoss, takeProfit float64) bool {
	if currentPrice <= 0 || stopLoss <= 0 || takeProfit <= 0 {
		return true
	}
	switch direction {
	case SideLong:
		return stopLoss >= currentPrice || takeProfit <= currentPrice
	case SideShort:
		return stopLoss <= currentPrice || takeProfit >= currentPrice
	default:
		return true
	}
}

func remainingNetRRForDecision(direction string, currentPrice, stopLoss, takeProfit, tradingCostPct float64) (float64, bool) {
	if invalidProgrammaticOpenStructure(direction, currentPrice, stopLoss, takeProfit) {
		return 0, false
	}
	var riskPct, rewardPct float64
	switch direction {
	case SideLong:
		riskPct = (currentPrice - stopLoss) / currentPrice * 100
		rewardPct = (takeProfit - currentPrice) / currentPrice * 100
	case SideShort:
		riskPct = (stopLoss - currentPrice) / currentPrice * 100
		rewardPct = (currentPrice - takeProfit) / currentPrice * 100
	default:
		return 0, false
	}
	if riskPct <= 0 {
		return 0, false
	}
	return (rewardPct - tradingCostPct) / riskPct, true
}

func theoreticalNetRRForSignal(signal ChanlunSignal, tradingCostPct float64) (float64, bool) {
	return remainingNetRRForDecision(signal.Direction, signal.Price, signal.StopLoss, signal.TakeProfit, tradingCostPct)
}

func grossRRForDecision(direction string, currentPrice, stopLoss, takeProfit float64) (float64, bool) {
	if invalidProgrammaticOpenStructure(direction, currentPrice, stopLoss, takeProfit) {
		return 0, false
	}
	var risk, reward float64
	switch direction {
	case SideLong:
		risk = currentPrice - stopLoss
		reward = takeProfit - currentPrice
	case SideShort:
		risk = stopLoss - currentPrice
		reward = currentPrice - takeProfit
	default:
		return 0, false
	}
	if risk <= 0 {
		return 0, false
	}
	return reward / risk, true
}

func tradingCostPct(ctx *decision.Context) float64 {
	if ctx != nil && ctx.StrategyRiskPolicy != nil && ctx.StrategyRiskPolicy.FeeSlippagePct > 0 {
		return ctx.StrategyRiskPolicy.FeeSlippagePct * 100
	}
	return 0.2
}

func signalAgeCandles(signalClose, decisionClose int64, timeframe string) int {
	if signalClose <= 0 || decisionClose <= signalClose {
		return 0
	}
	duration := timeframeDuration(timeframe)
	if duration <= 0 {
		duration = time.Hour
	}
	return int((decisionClose - signalClose) / int64(duration/time.Millisecond))
}

func currentPriceForGuard(data *market.Data, tradeTF string) float64 {
	if data == nil {
		return 0
	}
	if data.CurrentPrice > 0 {
		return data.CurrentPrice
	}
	if close := latestKlinePrice(data, "3m"); close > 0 {
		return close
	}
	if close := latestKlinePrice(data, tradeTF); close > 0 {
		return close
	}
	return latestKlinePrice(data, "1h")
}

func latestKlinePrice(data *market.Data, timeframe string) float64 {
	if data == nil || len(data.Klines[timeframe]) == 0 {
		return 0
	}
	return data.Klines[timeframe][len(data.Klines[timeframe])-1].Close
}

func latestKlineClose(data *market.Data, timeframe string) int64 {
	if data == nil || len(data.Klines[timeframe]) == 0 {
		return 0
	}
	return data.Klines[timeframe][len(data.Klines[timeframe])-1].CloseTime
}

func timeframeDuration(timeframe string) time.Duration {
	switch timeframe {
	case "3m":
		return 3 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		return 0
	}
}

func lifecycleExpiry(now time.Time, timeframe string, maxLifetimeCandles int) time.Time {
	duration := timeframeDuration(timeframe)
	if duration <= 0 || maxLifetimeCandles <= 0 {
		return time.Time{}
	}
	return now.Add(duration * time.Duration(maxLifetimeCandles))
}

func chineseSide(direction string) string {
	if direction == SideShort {
		return "做空"
	}
	return "做多"
}

func (e *Engine) validateProgrammaticDecisions(ctx *decision.Context, strategyDecisions []decision.Decision, prep *decision.CyclePreparation) ([]decision.Decision, []decision.OpenRejection) {
	var riskReducing []decision.Decision
	var openLike []decision.Decision
	for _, d := range strategyDecisions {
		if decision.IsOpenLikeAction(d.Action) {
			openLike = append(openLike, d)
			continue
		}
		riskReducing = append(riskReducing, d)
	}
	validRiskReducing, rrRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: "programmatic"})
	var rejections []decision.OpenRejection
	rejections = append(rejections, rrRejections...)
	if prep != nil && prep.RiskIncreaseBlocked {
		for _, d := range openLike {
			reason := fmt.Sprintf("%s %s 被拒绝: %s", d.Symbol, d.Action, prep.StopReason)
			rejections = append(rejections, decision.NewOpenRejectionFromDecision(d, reason))
		}
		e.markRejectedStrategyDecisions(ctx, strategyDecisions, validRiskReducing, rejections)
		return validRiskReducing, rejections
	}
	validOpenLike, openRejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
		Source:   "programmatic",
		AllowAdd: true,
	})
	rejections = append(rejections, openRejections...)
	valid := append(validRiskReducing, validOpenLike...)
	e.markRejectedStrategyDecisions(ctx, strategyDecisions, valid, rejections)
	return valid, rejections
}

func (e *Engine) markRejectedStrategyDecisions(ctx *decision.Context, candidates, valid []decision.Decision, rejections []decision.OpenRejection) {
	if ctx == nil {
		return
	}
	validIDs := map[string]bool{}
	for _, d := range valid {
		if d.SignalID != "" {
			validIDs[d.SignalID] = true
		}
	}
	reasonBySymbolAction := map[string]string{}
	reasonBySignalID := map[string]string{}
	for _, rejection := range rejections {
		if rejection.SignalID != "" {
			reasonBySignalID[rejection.SignalID] = rejection.Reason
		}
		reasonBySymbolAction[market.Normalize(rejection.Symbol)+"|"+rejection.Action] = rejection.Reason
	}
	for _, d := range candidates {
		if d.SignalID == "" || validIDs[d.SignalID] {
			continue
		}
		reason := reasonBySignalID[d.SignalID]
		if reason == "" {
			reason = reasonBySymbolAction[market.Normalize(d.Symbol)+"|"+d.Action]
		}
		if reason == "" {
			reason = "程序化动作被验证层拒绝"
		}
		if marker, ok := e.decisionToMarker(d, "rejected"); ok {
			marker.Reason = reason
			e.StateStore.StoreSignalMarker(ctx.TraderID, market.Normalize(d.Symbol), marker)
		} else {
			e.StateStore.UpdateSignalMarkerStatus(ctx.TraderID, market.Normalize(d.Symbol), d.SignalID, "rejected", reason)
		}
	}
}

func (e *Engine) applyDecisionMetadata(ctx *decision.Context, fullDecision *decision.FullDecision, diagnostics []string, rejections []decision.OpenRejection, accountGate AccountSizeDecision) {
	if fullDecision == nil {
		return
	}
	fullDecision.DecisionMode = "programmatic"
	fullDecision.StrategyName = e.Policy.StrategyName
	fullDecision.StrategyVersion = e.Policy.StrategyVersion
	fullDecision.ConfigHash = e.Policy.ConfigHash
	fullDecision.StrategyParams = map[string]any{
		"timeframes":          e.Policy.Timeframes,
		"history_depth":       e.Policy.HistoryDepth,
		"symbol_pool":         e.Policy.SymbolPool,
		"moving_average":      e.Policy.MovingAverage,
		"structure":           e.Policy.Structure,
		"divergence":          e.Policy.Divergence,
		"adx":                 e.Policy.ADX,
		"position":            e.Policy.Position,
		"position_management": e.Policy.PositionManagement,
		"take_profit":         e.Policy.TakeProfit,
		"signal_freshness":    e.Policy.SignalFreshness,
		"preview_signals":     e.Policy.PreviewSignals,
		"entry_timing":        e.Policy.EntryTiming,
		"candidate_governor":  e.Policy.CandidateGovernor,
	}
	strategyDiagnostics := map[string]any{}
	if len(diagnostics) > 0 {
		mainMessages, positionMessages := splitLayerDiagnostics(diagnostics)
		strategyDiagnostics = map[string]any{
			"messages": append([]string(nil), diagnostics...),
			"main_signal": map[string]any{
				"trade_timeframe": e.Policy.Timeframes.Trade,
				"next_close_time": nextCloseTime(e.now(), e.Policy.Timeframes.Trade).Format(time.RFC3339),
				"messages":        mainMessages,
			},
			"position_management": map[string]any{
				"enabled":  e.Policy.PositionManagement.Enabled,
				"messages": positionMessages,
			},
		}
	}
	if ctx != nil {
		strategyDiagnostics["per_candidate"] = e.buildPerCandidateDiagnostics(ctx, rejections)
		strategyDiagnostics["confidence_histogram"] = e.buildConfidenceHistogram(ctx)
		strategyDiagnostics["signal_quality_breakdown"] = e.buildSignalQualityBreakdown(ctx.TraderID)
		strategyDiagnostics["risk_state"] = e.buildStrategyRiskDiagnostics(ctx, accountGate)
		strategyDiagnostics["account_state"] = map[string]any{
			"account_too_small":  accountGate.AccountTooSmall,
			"total_realized_24h": ctx.Account.TotalRealized24h,
		}
	}
	if len(strategyDiagnostics) > 0 {
		fullDecision.StrategyDiagnostics = strategyDiagnostics
	}
}

func (e *Engine) buildPerCandidateDiagnostics(ctx *decision.Context, rejections []decision.OpenRejection) []map[string]any {
	if ctx == nil {
		return nil
	}
	reasonBySymbol := map[string]string{}
	for _, rejection := range rejections {
		reason := ""
		if code, _ := rejection.GateDiagnostics["reason_code"].(string); code != "" {
			reason = code
		}
		if reason == "" && len(rejection.GateReasons) > 0 {
			reason = rejection.GateReasons[0]
		}
		if reason == "" {
			reason = rejection.Reason
		}
		if reason != "" {
			reasonBySymbol[market.Normalize(rejection.Symbol)] = reason
		}
	}
	out := make([]map[string]any, 0, len(ctx.CandidateCoins))
	for _, coin := range ctx.CandidateCoins {
		symbol := market.Normalize(coin.Symbol)
		item := map[string]any{
			"symbol":               symbol,
			"terminal_reason_code": reasonBySymbol[symbol],
			"included_in_prompt":   coin.IncludedInPrompt,
			"filter_reason":        coin.FilterReason,
			"errors":               append([]string(nil), coin.Errors...),
		}
		if report := e.latestSignalReportSnapshot(ctx.TraderID, symbol); report != nil && len(report.Signals) > 0 {
			signal := report.Signals[len(report.Signals)-1]
			item["signal_type"] = signal.SignalType
			item["confidence"] = signal.Confidence
			item["structure_key"] = signal.StructureKey
		}
		if item["terminal_reason_code"] == "" {
			if coin.FilterReason != "" {
				item["terminal_reason_code"] = coin.FilterReason
			} else {
				item["terminal_reason_code"] = "no_signal"
			}
		}
		out = append(out, item)
	}
	return out
}

func (e *Engine) latestSignalReportSnapshot(traderID, symbol string) *SignalReport {
	if e == nil {
		return nil
	}
	symbol = market.Normalize(symbol)
	e.mu.RLock()
	report := e.latestSignals[traderID+"|"+symbol]
	e.mu.RUnlock()
	if report == nil {
		return nil
	}
	copied := *report
	copied.Signals = append([]ChanlunSignal(nil), report.Signals...)
	copied.SignalMarkers = append([]SignalMarker(nil), report.SignalMarkers...)
	return &copied
}

func (e *Engine) buildConfidenceHistogram(ctx *decision.Context) map[string]map[string]any {
	if ctx == nil || e.StateStore == nil {
		return nil
	}
	result := map[string]map[string]any{}
	for _, signalType := range []string{SignalBuy1, SignalBuy2, SignalBuy3, SignalSell1, SignalSell2, SignalSell3} {
		samples := e.StateStore.ConfidenceWindow(ctx.TraderID, signalType, 7*24*time.Hour)
		if len(samples) == 0 {
			continue
		}
		sort.Ints(samples)
		result[signalType] = map[string]any{
			"count":     len(samples),
			"p25":       percentileInt(samples, 25),
			"p50":       percentileInt(samples, 50),
			"p75":       percentileInt(samples, 75),
			"threshold": e.effectivePilotMinConfidence(ctx, signalType),
		}
	}
	return result
}

func percentileInt(sorted []int, pct int) int {
	if len(sorted) == 0 {
		return 0
	}
	if pct <= 0 {
		return sorted[0]
	}
	if pct >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := (len(sorted) - 1) * pct / 100
	return sorted[idx]
}

func (e *Engine) buildSignalQualityBreakdown(traderID string) map[string]int {
	result := map[string]int{}
	if traderID == "" {
		return result
	}
	e.mu.RLock()
	reports := make([]*SignalReport, 0, len(e.latestSignals))
	prefix := traderID + "|"
	for key, report := range e.latestSignals {
		if strings.HasPrefix(key, prefix) && report != nil {
			reports = append(reports, report)
		}
	}
	e.mu.RUnlock()
	for _, report := range reports {
		for _, signal := range report.Signals {
			key := signal.SignalType
			if key == "" {
				key = "unknown"
			}
			result[key]++
			if signal.Diagnostics.Metrics != nil {
				if value, _ := signal.Diagnostics.Metrics["signal_invalid_at_birth"].(bool); value {
					result["born_invalid"]++
				}
			}
		}
	}
	return result
}

func (e *Engine) buildStrategyRiskDiagnostics(ctx *decision.Context, accountGate AccountSizeDecision) map[string]any {
	out := map[string]any{
		"active_mode":       e.activeRuntimeMode(),
		"account_too_small": accountGate.AccountTooSmall,
	}
	if e.StateStore != nil && ctx != nil {
		stats := e.StateStore.SuppressionStats(ctx.TraderID)
		out["suppressions"] = map[string]any{
			"total_active":       stats.TotalActive,
			"by_reason":          stats.ByReason,
			"oldest_age_candles": stats.OldestAgeCandles,
			"permanent_skip":     stats.PermanentSkip,
		}
	}
	if ctx != nil && ctx.FrequencyState != nil {
		out["open_count_24h"] = ctx.FrequencyState.OpenCount24h
		out["open_rejected_24h"] = ctx.FrequencyState.OpenRejected24h
		out["signal_count_24h"] = ctx.FrequencyState.SignalCount24h
		if !ctx.FrequencyState.LastOpenAt.IsZero() {
			out["last_open_at"] = ctx.FrequencyState.LastOpenAt.Format(time.RFC3339)
			out["inactivity_minutes"] = int(e.now().Sub(ctx.FrequencyState.LastOpenAt).Minutes())
		} else {
			out["inactivity_minutes"] = ctx.RuntimeMinutes
		}
		if !ctx.FrequencyState.LastCloseAt.IsZero() {
			out["last_close_at"] = ctx.FrequencyState.LastCloseAt.Format(time.RFC3339)
		}
		if ctx.FrequencyState.OpenRejected24h >= 10 && ctx.FrequencyState.OpenCount24h == 0 {
			out["warnings"] = map[string]bool{"runaway_rejection_loop": true}
		}
	}
	if ctx != nil && ctx.FrequencyPolicy != nil && ctx.FrequencyPolicy.GateEffectivenessReportOnly {
		out["gate_effectiveness"] = map[string]any{
			"report_only":        true,
			"would_reject_count": 0,
		}
	}
	return out
}

func (e *Engine) LatestSignals(traderID, symbol string) (*SignalReport, bool) {
	return e.LatestSignalsWithOptions(traderID, symbol, SignalReportOptions{})
}

func (e *Engine) LatestSignalsWithOptions(traderID, symbol string, opts SignalReportOptions) (*SignalReport, bool) {
	symbol = market.Normalize(symbol)
	var markers []SignalMarker
	if e.StateStore != nil {
		e.StateStore.CompactSignalMarkers(traderID, symbol)
		markers = e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers)
	}
	e.mu.RLock()
	report, ok := e.latestSignals[traderID+"|"+symbol]
	e.mu.RUnlock()
	if !ok {
		return nil, false
	}
	copied := *report
	copied.Signals = append([]ChanlunSignal{}, report.Signals...)
	copied.SignalMarkers = mergeSignalMarkers(markers, report.SignalMarkers)
	applySignalReportOptions(&copied, opts)
	return &copied, true
}

func (e *Engine) EmptySignalReport(traderID, symbol string) *SignalReport {
	return e.EmptySignalReportWithOptions(traderID, symbol, SignalReportOptions{})
}

func (e *Engine) EmptySignalReportWithOptions(traderID, symbol string, opts SignalReportOptions) *SignalReport {
	symbol = market.Normalize(symbol)
	var markers []SignalMarker
	if e.StateStore != nil {
		e.StateStore.CompactSignalMarkers(traderID, symbol)
		markers = e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers)
	}
	report := &SignalReport{
		TraderID:           traderID,
		Symbol:             symbol,
		DecisionMode:       "programmatic",
		StrategyName:       e.Policy.StrategyName,
		StrategyVersion:    e.Policy.StrategyVersion,
		ConfigHash:         e.Policy.ConfigHash,
		TradeTimeframe:     e.Policy.Timeframes.Trade,
		ComponentTimeframe: e.Policy.Timeframes.Sub,
		MicroTimeframe:     e.Policy.Timeframes.Micro,
		Signals:            []ChanlunSignal{},
		SignalMarkers:      markers,
		LatestDiagnostics: map[string]any{
			"messages": []string{"暂无该标的的程序化策略信号"},
		},
	}
	applySignalReportOptions(report, opts)
	return report
}

type ProgrammaticExecutionResult struct {
	TraderID                 string
	Decision                 decision.Decision
	Success                  bool
	FinalAction              string
	RequestedClosePercentage float64
	ExecutedClosePercentage  float64
	ExecutedQuantity         float64
	PositionQuantityBefore   float64
	Price                    float64
	Error                    string
	ExecutedAt               time.Time
}

func (e *Engine) OnExecutionResult(result ProgrammaticExecutionResult) {
	d := result.Decision
	if d.StrategyMode != "programmatic" || d.SignalID == "" || d.Symbol == "" {
		return
	}
	symbol := market.Normalize(d.Symbol)
	status := "executed"
	if !result.Success {
		status = "failed"
	}
	reason := result.Error
	if reason == "" {
		reason = d.Reasoning
	}
	if marker, ok := e.decisionToMarker(d, status); ok {
		marker.FinalAction = result.FinalAction
		marker.TradeIntent = deriveTradeIntent(marker.Action, marker.FinalAction, marker.PositionSide, marker.Direction)
		marker.Reason = reason
		e.StateStore.StoreSignalMarker(result.TraderID, symbol, marker)
	}
	if !result.Success {
		_ = e.StateStore.Save()
		return
	}
	finalAction := firstNonEmptyString(result.FinalAction, d.Action)
	if finalAction == "hold" || finalAction == "partial_close_skipped" {
		e.StateStore.UpdateSignalMarkerStatus(result.TraderID, symbol, d.SignalID, "rejected", reason)
		_ = e.StateStore.Save()
		return
	}
	if decision.IsOpenLikeAction(finalAction) {
		e.StateStore.MarkExecuted(result.TraderID, symbol, d.SignalID, finalAction)
	}
	rule := metadataString(d.StrategyMetadata, "rule")
	side := metadataString(d.StrategyMetadata, "side")
	if side == "" {
		side = directionForAction(finalAction)
	}
	if side != "" && rule != "" {
		e.StateStore.MarkPositionSignal(result.TraderID, symbol, side, rule, d.SignalID)
	}
	switch finalAction {
	case "partial_close":
		if result.ExecutedQuantity > 0 || result.ExecutedClosePercentage > 0 {
			e.StateStore.RecordProgrammaticPartialClose(ProgrammaticPartialCloseRecord{
				TraderID:                 result.TraderID,
				Symbol:                   symbol,
				Side:                     side,
				Rule:                     rule,
				SignalID:                 d.SignalID,
				RequestedClosePercentage: result.RequestedClosePercentage,
				ExecutedClosePercentage:  result.ExecutedClosePercentage,
				ExecutedQuantity:         result.ExecutedQuantity,
				PositionQuantityBefore:   result.PositionQuantityBefore,
				Price:                    result.Price,
				Estimated:                result.ExecutedQuantity <= 0,
				ExecutedAt:               result.ExecutedAt,
				PeakPrice:                metadataFloat64(d.StrategyMetadata, "peak_price"),
				PeakPnLPct:               metadataFloat64(d.StrategyMetadata, "peak_pnl_pct"),
				PeakR:                    metadataFloat64(d.StrategyMetadata, "peak_r"),
			})
		}
	case "close_long", "close_short":
		e.StateStore.RecordProgrammaticFullClose(result.TraderID, symbol, side)
	}
	_ = e.StateStore.Save()
}

func metadataFloat64(values map[string]any, key string) float64 {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func metadataInt(values map[string]any, key string) int {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

func metadataBool(values map[string]any, key string) bool {
	if len(values) == 0 {
		return false
	}
	switch value := values[key].(type) {
	case bool:
		return value
	default:
		return false
	}
}

func metadataInt64Value(values map[string]any, key string) int64 {
	value, _ := metadataInt64(values, key)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (e *Engine) SymbolUniverse(traderID string) []StrategySymbol {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]StrategySymbol(nil), e.symbolUniverse[traderID]...)
}

func (e *Engine) analyzeMainSignal(traderID, symbol string, data *market.Data, now time.Time) ([]ChanlunSignal, []string) {
	tradeTF := e.Policy.Timeframes.Trade
	subTF := ComponentTimeframe(tradeTF)
	if subTF == "" {
		subTF = e.Policy.Timeframes.Sub
	}
	tradeKlines := data.Klines[tradeTF]
	if len(tradeKlines) < 30 {
		return nil, []string{fmt.Sprintf("%s %s K线不足", symbol, tradeTF)}
	}
	lastClosed := tradeKlines[len(tradeKlines)-1].CloseTime
	symbolState := e.StateStore.SymbolState(traderID, symbol)
	if !e.Policy.State.Bootstrap && symbolState.LastAnalyzedClosedKline[tradeTF] == lastClosed {
		return nil, []string{fmt.Sprintf("%s %s 无新闭合K线", symbol, tradeTF)}
	}
	candles := marketKlinesToCandles(tradeTF, tradeKlines)
	normalized := NormalizeInclusion(candles)
	fractals := FindFractals(normalized, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
	strokes := BuildStrokes(fractals, normalized, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
	segments := BuildSegments(strokes, e.Policy.Structure.Strictness)
	centerSegments := segments
	if subTF != "" && len(data.Klines[subTF]) > 0 {
		subCandles := NormalizeInclusion(marketKlinesToCandles(subTF, data.Klines[subTF]))
		subFractals := FindFractals(subCandles, e.Policy.Structure.LeftBars, e.Policy.Structure.RightBars)
		subStrokes := BuildStrokes(subFractals, subCandles, e.Policy.Structure.MinStrokeBars, e.Policy.Structure.MinSwingPct, e.Policy.Structure.ATRMultiplier)
		centerSegments = BuildSegments(subStrokes, e.Policy.Structure.Strictness)
	}
	centers := BuildCenters(centerSegments, tradeTF)
	if len(segments) < 3 {
		e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
		return nil, []string{fmt.Sprintf("%s 无足够走势段", symbol)}
	}
	hist := macdHistForTF(data, tradeTF)
	shortEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.ShortPeriod)
	longEMA := emaSeriesFromCandles(candles, e.Policy.MovingAverage.LongPeriod)
	maKiss := DetectMAKiss(shortEMA, longEMA, e.Policy.MovingAverage.KissDistancePct, e.Policy.MovingAverage.WetKissBars)
	signals := DetectSignals(SignalInput{
		TraderID:          traderID,
		Symbol:            symbol,
		AnalysisTF:        tradeTF,
		TriggerTF:         e.Policy.Timeframes.Sub,
		Centers:           centers,
		Segments:          segments,
		MACDHist:          hist,
		ConfigHash:        e.Policy.ConfigHash,
		Now:               now,
		EnabledSignal:     enabledSignalMap(e.Policy.EnabledSignals),
		DivergenceRatio:   e.Policy.Divergence.Ratio,
		PriceTolerancePct: e.Policy.Divergence.PriceTolerancePct,
		RequireBZeroAxis:  e.Policy.Divergence.RequireBZeroAxis,
		MAKiss:            maKiss,
		MarketData:        data,
		ADXTimeframe:      "1h",
	})
	var timeDiagnostics []string
	filtered := signals[:0]
	for i := range signals {
		if e.Policy.DefectFixPackEnabled && signals[i].IsBornInvalid() {
			if signals[i].Diagnostics.Metrics == nil {
				signals[i].Diagnostics.Metrics = map[string]any{}
			}
			signals[i].Diagnostics.Metrics["signal_invalid_at_birth"] = true
			e.StateStore.StoreConfidenceSample(traderID, market.Normalize(symbol), signals[i].SignalType, signals[i].Confidence, now)
			timeDiagnostics = append(timeDiagnostics, fmt.Sprintf("%s %s 出生即无效，已丢弃", symbol, signals[i].SignalType))
			continue
		}
		signalClose := signals[i].SignalCloseTime
		if signalClose == 0 {
			signalClose = signals[i].TriggerCloseTime
		}
		if signalClose == 0 {
			signalClose = signals[i].SegmentEndTime
		}
		signals[i].SignalCloseTime = signalClose
		signals[i].TriggerCloseTime = signalClose
		signals[i].DecisionCloseTime = lastClosed
		if signalClose > 0 && lastClosed > 0 && lastClosed < signalClose {
			timeDiagnostics = append(timeDiagnostics, fmt.Sprintf("%s %s 时间锚点异常: decision_close_time早于signal_close_time", symbol, signals[i].SignalType))
			signals[i].DecisionCloseTime = signalClose
		}
		e.StateStore.StoreConfidenceSample(traderID, market.Normalize(symbol), signals[i].SignalType, signals[i].Confidence, now)
		filtered = append(filtered, signals[i])
	}
	signals = filtered
	if len(signals) == 0 {
		e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
		return nil, append([]string{fmt.Sprintf("%s 无买卖点信号", symbol)}, timeDiagnostics...)
	}
	e.StateStore.SetLastAnalyzedClosedKline(traderID, symbol, tradeTF, lastClosed)
	diagnostics := []string{fmt.Sprintf("%s 识别到%d个信号", symbol, len(signals))}
	diagnostics = append(diagnostics, timeDiagnostics...)
	return signals, diagnostics
}

func (e *Engine) signalToMainDecision(ctx *decision.Context, signal ChanlunSignal) decision.Decision {
	action := ""
	positionSide := positionSideForSymbol(ctx.Positions, signal.Symbol)
	switch {
	case positionSide == "" && signal.Direction == SideLong && e.Policy.AllowLong:
		action = "open_long"
	case positionSide == "" && signal.Direction == SideShort && e.Policy.AllowShort:
		action = "open_short"
	case positionSide == SideLong && signal.Direction == SideLong:
		action = "add_long"
	case positionSide == SideShort && signal.Direction == SideShort:
		action = "add_short"
	}
	if action == "" {
		return decision.Decision{}
	}
	signalClose := signal.SignalCloseTime
	if signalClose == 0 {
		signalClose = signal.TriggerCloseTime
	}
	if signalClose == 0 {
		signalClose = signal.SegmentEndTime
	}
	triggerClose := firstPositiveInt64(signal.EntryTriggerClose, signal.TriggerCloseTime, signalClose)
	decisionClose := signal.DecisionCloseTime
	if decisionClose > 0 && signalClose > 0 && decisionClose < signalClose {
		decisionClose = signalClose
	}
	if decisionClose > 0 && triggerClose > 0 && decisionClose < triggerClose {
		decisionClose = triggerClose
	}
	layer := signal.SourceLayer
	if layer == "" {
		layer = "main_signal"
	}
	reasonPrefix := "程序化缠论"
	if layer == "preview_signal" {
		reasonPrefix = "程序化缠论预览"
	} else if layer == "entry_trigger" {
		reasonPrefix = "程序化缠论入场触发"
	}
	decisionSignalID := signal.SignalID
	if signal.EntryTriggerID != "" && decision.IsOpenLikeAction(action) {
		decisionSignalID = signal.EntryTriggerID
	}
	d := decision.Decision{
		Symbol:          signal.Symbol,
		Action:          action,
		Leverage:        leverageForSymbol(ctx, signal.Symbol),
		StopLoss:        signal.StopLoss,
		TakeProfit:      signal.TakeProfit,
		Confidence:      signal.Confidence,
		Reasoning:       fmt.Sprintf("%s%s信号: %s %s", reasonPrefix, signal.SignalType, signal.AnalysisTF, signal.CenterID),
		PositionSizeUSD: 0,
		StrategyMode:    "programmatic",
		StrategyName:    e.Policy.StrategyName,
		StrategyVersion: e.Policy.StrategyVersion,
		ConfigHash:      e.Policy.ConfigHash,
		SignalID:        decisionSignalID,
		SignalType:      signal.SignalType,
		SignalTimeframe: signal.AnalysisTF,
		StructureTarget: signal.StructureTarget,
		StrategyMetadata: map[string]any{
			"layer":                     layer,
			"rule":                      signal.SignalType,
			"signal_type":               signal.SignalType,
			"entry_path":                signal.EntryPath,
			"tier":                      signal.Tier,
			"center_id":                 signal.CenterID,
			"trigger_timeframe":         signal.TriggerTF,
			"level":                     signal.Level,
			"signal_close_time":         signalClose,
			"decision_close_time":       decisionClose,
			"trigger_close_time":        triggerClose,
			"segment_start_time":        signal.SegmentStartTime,
			"segment_end_time":          signal.SegmentEndTime,
			"trade_intent":              action,
			"structure_key":             signal.StructureKey,
			"lifecycle_key":             signal.LifecycleKey,
			"source_signal_id":          signal.SignalID,
			"parent_signal_id":          signal.ParentSignalID,
			"parent_structure_key":      signal.ParentStructureKey,
			"reason_code":               signal.ReasonCode,
			"entry_trigger_id":          signal.EntryTriggerID,
			"entry_trigger_type":        signal.EntryTriggerType,
			"entry_trigger_timeframe":   signal.EntryTriggerTF,
			"entry_trigger_close_time":  signal.EntryTriggerClose,
			"entry_window_state":        signal.EntryWindowState,
			"entry_reference_price":     signal.EntryReference,
			"entry_invalidated":         signal.EntryInvalidated,
			"entry_invalidation_reason": signal.EntryInvalidReason,
			"remaining_net_rr":          signal.RemainingNetRR,
			"trigger_confidence":        signal.TriggerConfidence,
			"preview_phase":             signal.PreviewPhase,
			"preview_source_tf":         signal.PreviewSourceTF,
			"preview_components":        signal.PreviewComponents,
			"preview_confirmed":         signal.PreviewConfirmed,
		},
		StrategyDiagnosis: map[string]any{
			"diagnostics": signal.Diagnostics,
		},
	}
	d.Explanation = &decision.DecisionExplanation{
		Summary:        d.Reasoning,
		Layer:          layer,
		Rule:           signal.SignalType,
		ReasonCode:     "chanlun_signal_detected",
		Timeframe:      signal.AnalysisTF,
		SignalType:     signal.SignalType,
		SignalID:       decisionSignalID,
		TriggerPrice:   signal.Price,
		ReferencePrice: signal.StructureTarget,
		Details: map[string]any{
			"center_id":                 signal.CenterID,
			"entry_path":                signal.EntryPath,
			"tier":                      signal.Tier,
			"trigger_timeframe":         signal.TriggerTF,
			"signal_close_time":         signalClose,
			"decision_close_time":       decisionClose,
			"trigger_close_time":        triggerClose,
			"segment_start_time":        signal.SegmentStartTime,
			"segment_end_time":          signal.SegmentEndTime,
			"trade_intent":              action,
			"structure_key":             signal.StructureKey,
			"lifecycle_key":             signal.LifecycleKey,
			"level":                     signal.Level,
			"source_signal_id":          signal.SignalID,
			"parent_signal_id":          signal.ParentSignalID,
			"parent_structure_key":      signal.ParentStructureKey,
			"reason_code":               signal.ReasonCode,
			"entry_trigger_id":          signal.EntryTriggerID,
			"entry_trigger_type":        signal.EntryTriggerType,
			"entry_trigger_timeframe":   signal.EntryTriggerTF,
			"entry_trigger_close_time":  signal.EntryTriggerClose,
			"entry_window_state":        signal.EntryWindowState,
			"entry_reference_price":     signal.EntryReference,
			"entry_invalidated":         signal.EntryInvalidated,
			"entry_invalidation_reason": signal.EntryInvalidReason,
			"remaining_net_rr":          signal.RemainingNetRR,
			"trigger_confidence":        signal.TriggerConfidence,
			"preview_phase":             signal.PreviewPhase,
			"preview_source_tf":         signal.PreviewSourceTF,
			"preview_components":        signal.PreviewComponents,
			"preview_confirmed":         signal.PreviewConfirmed,
		},
	}
	for _, key := range []string{"chase_ratio", "chase_ratio_atr", "entry_zone_low", "entry_zone_high"} {
		if value, ok := signal.Diagnostics.Metrics[key]; ok {
			d.StrategyMetadata[key] = value
			d.Explanation.Details[key] = value
		}
	}
	if action == "partial_close" {
		d.ClosePercentage = e.Policy.Position.PartialClosePct
	}
	if decision.IsAddAction(action) {
		if value := positionValueForSymbolSide(ctx.Positions, signal.Symbol, signal.Direction); value > 0 {
			multiplier := e.Policy.Position.AddSizeMultiplier
			if multiplier <= 0 {
				multiplier = 0.5
			}
			d.PositionSizeUSD = value * multiplier
		}
	}
	return d
}

func isReduceSignal(signalType string) bool {
	switch signalType {
	case SignalBuy2, SignalBuy3, SignalSell2, SignalSell3:
		return true
	default:
		return false
	}
}

func ResolveProgrammaticSymbols(candidates []decision.CandidateCoin, positions []decision.PositionInfo, policy decision.ProgrammaticStrategyPolicy) []StrategySymbol {
	base := map[string]StrategySymbol{}
	for _, coin := range candidates {
		symbol := market.Normalize(coin.Symbol)
		if symbol == "" {
			continue
		}
		if !coin.IncludedInPrompt && strings.TrimSpace(coin.FilterReason) != "" {
			continue
		}
		base[symbol] = StrategySymbol{Symbol: symbol, Sources: append([]string(nil), coin.Sources...), Selected: true}
	}
	custom := map[string]bool{}
	for _, symbol := range policy.SymbolPool.Symbols {
		custom[market.Normalize(symbol)] = true
	}
	core := map[string]bool{}
	for _, symbol := range policy.SymbolPool.CoreSymbols {
		core[market.Normalize(symbol)] = true
	}
	for _, symbol := range policy.CandidateGovernor.CoreSymbolsMustAppear {
		core[market.Normalize(symbol)] = true
	}
	mode := policy.SymbolPool.Mode
	if mode == "" {
		mode = "append"
	}
	result := map[string]StrategySymbol{}
	switch mode {
	case "override":
		for symbol := range custom {
			result[symbol] = StrategySymbol{Symbol: symbol, Sources: []string{"custom"}, Selected: true}
		}
		for symbol := range core {
			result[symbol] = StrategySymbol{Symbol: symbol, Sources: []string{"core"}, Selected: true}
		}
	case "filter":
		for symbol, item := range base {
			if custom[symbol] {
				item.Sources = appendSource(item.Sources, "custom")
				result[symbol] = item
			}
		}
	default:
		for symbol, item := range base {
			result[symbol] = item
		}
		for symbol := range custom {
			item := result[symbol]
			item.Symbol = symbol
			item.Selected = true
			item.Sources = appendSource(item.Sources, "custom")
			result[symbol] = item
		}
	}
	for _, pos := range positions {
		symbol := market.Normalize(pos.Symbol)
		item := result[symbol]
		item.Symbol = symbol
		item.Selected = true
		item.HasPosition = true
		item.Sources = appendSource(item.Sources, "position")
		result[symbol] = item
	}
	symbols := make([]string, 0, len(result))
	for symbol := range result {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	out := make([]StrategySymbol, 0, len(symbols))
	for _, symbol := range symbols {
		out = append(out, result[symbol])
	}
	return out
}

func (e *Engine) setLatestSignals(traderID, symbol string, signals []ChanlunSignal, diagnostics []string) {
	markers := make([]SignalMarker, 0, len(signals))
	for _, signal := range signals {
		marker := signalToMarker(signal, "", "", "", "")
		markers = append(markers, marker)
		e.StateStore.StoreSignalMarker(traderID, symbol, marker)
	}
	markers = mergeSignalMarkers(e.StateStore.RecentSignalMarkers(traderID, symbol, maxRecentSignalMarkers), markers)
	markerSummary := buildSignalMarkerSummary(markers, markers)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.latestSignals[traderID+"|"+symbol] = &SignalReport{
		TraderID:           traderID,
		Symbol:             symbol,
		DecisionMode:       "programmatic",
		StrategyName:       e.Policy.StrategyName,
		StrategyVersion:    e.Policy.StrategyVersion,
		ConfigHash:         e.Policy.ConfigHash,
		TradeTimeframe:     e.Policy.Timeframes.Trade,
		ComponentTimeframe: e.Policy.Timeframes.Sub,
		MicroTimeframe:     e.Policy.Timeframes.Micro,
		Signals:            append([]ChanlunSignal{}, signals...),
		SignalMarkers:      markers,
		LatestDiagnostics: map[string]any{
			"messages":         diagnostics,
			"marker_lifecycle": markerSummary,
		},
	}
}

func signalToMarker(signal ChanlunSignal, sourceLayer, status, action, reason string) SignalMarker {
	if sourceLayer == "" {
		sourceLayer = signal.SourceLayer
	}
	if sourceLayer == "" {
		sourceLayer = "main_signal"
	}
	if status == "" {
		status = signal.Status
	}
	if status == "" {
		status = "detected"
	}
	timeframe := signal.AnalysisTF
	if sourceLayer == "position_management" && signal.TriggerTF != "" {
		timeframe = signal.TriggerTF
	}
	if sourceLayer == "entry_trigger" && signal.EntryTriggerTF != "" {
		timeframe = signal.EntryTriggerTF
	}
	structureClose := signal.SignalCloseTime
	if structureClose == 0 {
		structureClose = signal.TriggerCloseTime
	}
	if structureClose == 0 {
		structureClose = signal.SegmentEndTime
	}
	closeTime := structureClose
	if sourceLayer == "entry_trigger" && signal.EntryTriggerClose > 0 {
		closeTime = signal.EntryTriggerClose
	}
	decisionClose := signal.DecisionCloseTime
	displayClose := closeTime
	if (sourceLayer == "preview_signal" || sourceLayer == "entry_trigger") && decisionClose > 0 {
		displayClose = decisionClose
	}
	freshnessState := signal.EntryWindowState
	if freshnessState == "" {
		switch status {
		case "background":
			freshnessState = "background"
		case "invalidated":
			freshnessState = "invalidated"
		}
	}
	ageCandles := signalAgeCandles(structureClose, decisionClose, timeframe)
	signalID := signal.SignalID
	if sourceLayer == "entry_trigger" && signal.EntryTriggerID != "" {
		signalID = signal.EntryTriggerID
	}
	marker := SignalMarker{
		Symbol:             signal.Symbol,
		Timeframe:          timeframe,
		CloseTime:          closeTime,
		SignalCloseTime:    structureClose,
		DecisionCloseTime:  decisionClose,
		DisplayCloseTime:   displayClose,
		SignalType:         signal.SignalType,
		Direction:          signal.Direction,
		Level:              signal.Level,
		SourceLayer:        sourceLayer,
		Status:             status,
		SignalID:           signalID,
		StructureKey:       signal.StructureKey,
		LifecycleKey:       signal.LifecycleKey,
		ParentStructureKey: signal.ParentStructureKey,
		ReasonCode:         firstNonEmptyString(signal.ReasonCode, signal.EntryInvalidReason, signal.EntryWindowState),
		Action:             action,
		TradeIntent:        deriveTradeIntent(action, "", "", signal.Direction),
		Price:              signal.Price,
		Reason:             reason,
		ParentSignalID:     signal.ParentSignalID,
		EntryTriggerID:     signal.EntryTriggerID,
		EntryTriggerType:   signal.EntryTriggerType,
		EntryTriggerTF:     signal.EntryTriggerTF,
		EntryTriggerClose:  signal.EntryTriggerClose,
		EntryWindowState:   signal.EntryWindowState,
		EntryReference:     signal.EntryReference,
		EntryInvalidated:   signal.EntryInvalidated,
		EntryInvalidReason: signal.EntryInvalidReason,
		RemainingNetRR:     signal.RemainingNetRR,
		FreshnessState:     freshnessState,
		AgeCandles:         ageCandles,
		PreviewPhase:       signal.PreviewPhase,
		PreviewSourceTF:    signal.PreviewSourceTF,
		PreviewComponents:  signal.PreviewComponents,
		PreviewConfirmed:   signal.PreviewConfirmed,
	}
	return canonicalizeSignalMarker(marker)
}

func (e *Engine) decisionToMarker(d decision.Decision, status string) (SignalMarker, bool) {
	if d.SignalID == "" || d.Symbol == "" {
		return SignalMarker{}, false
	}
	layer := metadataString(d.StrategyMetadata, "layer")
	if layer == "" {
		layer = "position_management"
	}
	rule := metadataString(d.StrategyMetadata, "rule")
	signalType := d.SignalType
	if signalType == "" {
		signalType = metadataString(d.StrategyMetadata, "signal_type")
	}
	if signalType == "" {
		signalType = rule
	}
	timeframe := d.SignalTimeframe
	if timeframe == "" {
		timeframe = metadataString(d.StrategyMetadata, "timeframe")
	}
	if timeframe == "" {
		timeframe = metadataString(d.StrategyMetadata, "structure_timeframe")
	}
	if timeframe == "" {
		timeframe = e.Policy.Timeframes.Trade
	}
	signalClose, _ := metadataInt64Any(d.StrategyMetadata, "signal_close_time", "trigger_close_time", "segment_end_time")
	decisionClose, _ := metadataInt64(d.StrategyMetadata, "decision_close_time")
	metadataDisplayClose, _ := metadataInt64(d.StrategyMetadata, "display_close_time")
	freshnessState := metadataString(d.StrategyMetadata, "freshness_state")
	ageCandles := metadataInt(d.StrategyMetadata, "age_candles")
	previewPhase := metadataString(d.StrategyMetadata, "preview_phase")
	previewSourceTF := firstNonEmptyString(metadataString(d.StrategyMetadata, "preview_source_tf"), metadataString(d.StrategyMetadata, "preview_source_timeframe"))
	previewComponents := metadataInt(d.StrategyMetadata, "preview_components")
	previewConfirmed := metadataBool(d.StrategyMetadata, "preview_confirmed")
	structureKey := metadataString(d.StrategyMetadata, "structure_key")
	lifecycleKey := metadataString(d.StrategyMetadata, "lifecycle_key")
	parentSignalID := metadataString(d.StrategyMetadata, "parent_signal_id")
	parentStructureKey := metadataString(d.StrategyMetadata, "parent_structure_key")
	reasonCode := firstNonEmptyString(metadataString(d.StrategyMetadata, "reason_code"), metadataString(d.StrategyMetadata, "guard_reason_code"))
	entryTriggerID := metadataString(d.StrategyMetadata, "entry_trigger_id")
	entryTriggerType := metadataString(d.StrategyMetadata, "entry_trigger_type")
	entryTriggerTF := metadataString(d.StrategyMetadata, "entry_trigger_timeframe")
	entryTriggerClose, _ := metadataInt64(d.StrategyMetadata, "entry_trigger_close_time")
	entryWindowState := metadataString(d.StrategyMetadata, "entry_window_state")
	entryReference := metadataFloat64(d.StrategyMetadata, "entry_reference_price")
	entryInvalidated := metadataBool(d.StrategyMetadata, "entry_invalidated")
	entryInvalidReason := metadataString(d.StrategyMetadata, "entry_invalidation_reason")
	remainingNetRR := metadataFloat64(d.StrategyMetadata, "remaining_net_rr")
	closeTime := signalClose
	displayClose := signalClose
	if metadataDisplayClose > 0 {
		displayClose = metadataDisplayClose
	}
	if isTradeActionMarker(d, status) && decisionClose > 0 {
		if signalClose == 0 || decisionClose >= signalClose {
			displayClose = decisionClose
		}
	}
	direction := metadataString(d.StrategyMetadata, "side")
	if direction == "" {
		direction = directionForAction(d.Action)
	}
	positionSide := derivePositionSide(d, "")
	tradeIntent := metadataString(d.StrategyMetadata, "trade_intent")
	if tradeIntent == "" {
		tradeIntent = deriveTradeIntent(d.Action, "", positionSide, direction)
	}
	price := d.StopLoss
	if d.Action == "update_stop_loss" {
		price = d.NewStopLoss
	}
	marker := SignalMarker{
		Symbol:             market.Normalize(d.Symbol),
		Timeframe:          timeframe,
		CloseTime:          closeTime,
		SignalCloseTime:    signalClose,
		DecisionCloseTime:  decisionClose,
		DisplayCloseTime:   displayClose,
		SignalType:         signalType,
		Direction:          direction,
		Level:              timeframe,
		SourceLayer:        layer,
		Status:             status,
		SignalID:           d.SignalID,
		StructureKey:       structureKey,
		LifecycleKey:       lifecycleKey,
		ParentStructureKey: parentStructureKey,
		ReasonCode:         reasonCode,
		Action:             d.Action,
		TradeIntent:        tradeIntent,
		PositionSide:       positionSide,
		Price:              price,
		Reason:             d.Reasoning,
		ParentSignalID:     parentSignalID,
		EntryTriggerID:     entryTriggerID,
		EntryTriggerType:   entryTriggerType,
		EntryTriggerTF:     entryTriggerTF,
		EntryTriggerClose:  entryTriggerClose,
		EntryWindowState:   entryWindowState,
		EntryReference:     entryReference,
		EntryInvalidated:   entryInvalidated,
		EntryInvalidReason: entryInvalidReason,
		RemainingNetRR:     remainingNetRR,
		FreshnessState:     freshnessState,
		AgeCandles:         ageCandles,
		PreviewPhase:       previewPhase,
		PreviewSourceTF:    previewSourceTF,
		PreviewComponents:  previewComponents,
		PreviewConfirmed:   previewConfirmed,
	}
	return canonicalizeSignalMarker(marker), true
}

func derivePositionSide(d decision.Decision, finalAction string) string {
	if side := normalizeSide(metadataString(d.StrategyMetadata, "side")); side != "" {
		return side
	}
	if side := normalizeSide(metadataString(d.StrategyMetadata, "position_side")); side != "" {
		return side
	}
	if side := directionForAction(finalAction); side != "" {
		return side
	}
	if side := directionForAction(d.Action); side != "" {
		return side
	}
	if side := normalizeSide(d.SignalType); side != "" {
		return side
	}
	return ""
}

func isTradeActionMarker(d decision.Decision, status string) bool {
	if strings.TrimSpace(d.Action) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "rejected", "executed", "failed":
		return true
	default:
		return false
	}
}

func deriveTradeIntent(action, finalAction, positionSide, direction string) string {
	effective := effectiveMarkerAction(action, finalAction)
	switch effective {
	case "open_long", "open_short", "add_long", "add_short", "close_long", "close_short":
		return effective
	case "partial_close_skipped":
		return "reduce_skipped"
	case "partial_close":
		side := normalizeSide(positionSide)
		if side == "" {
			side = normalizeSide(direction)
		}
		switch side {
		case SideLong:
			return "reduce_long"
		case SideShort:
			return "reduce_short"
		}
	}
	return ""
}

func effectiveMarkerAction(action, finalAction string) string {
	if finalAction != "" {
		return finalAction
	}
	return action
}

func normalizeSide(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SideLong, "buy", "bull", "bullish":
		return SideLong
	case SideShort, "sell", "bear", "bearish":
		return SideShort
	default:
		return ""
	}
}

func metadataInt64(values map[string]any, key string) (int64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	switch value := values[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		return int64(value), true
	default:
		return 0, false
	}
}

func metadataInt64Any(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := metadataInt64(values, key); ok && value != 0 {
			return value, true
		}
	}
	return 0, false
}

func directionForAction(action string) string {
	switch action {
	case "open_long", "add_long", "close_long":
		return SideLong
	case "open_short", "add_short", "close_short":
		return SideShort
	default:
		return ""
	}
}

func mergeSignalMarkers(first, second []SignalMarker) []SignalMarker {
	var out []SignalMarker
	for _, marker := range append(append([]SignalMarker(nil), first...), second...) {
		if marker.SignalID == "" {
			continue
		}
		out = upsertSignalMarker(out, marker)
	}
	if len(out) > maxRecentSignalMarkers {
		out = out[len(out)-maxRecentSignalMarkers:]
	}
	return out
}

func (e *Engine) setUniverse(traderID string, universe []StrategySymbol) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.symbolUniverse[traderID] = append([]StrategySymbol(nil), universe...)
}

func (e *Engine) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

func marketKlinesToCandles(timeframe string, klines []market.Kline) []Candle {
	candles := make([]Candle, 0, len(klines))
	for _, k := range klines {
		candles = append(candles, Candle{
			Timeframe: timeframe,
			OpenTime:  k.OpenTime,
			CloseTime: k.CloseTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
		})
	}
	return candles
}

func macdHistForTF(data *market.Data, timeframe string) []float64 {
	switch timeframe {
	case "15m":
		if data.MidTermSeries15m != nil {
			return data.MidTermSeries15m.MACDHist
		}
	case "1h":
		if data.MidTermSeries1h != nil {
			return data.MidTermSeries1h.MACDHist
		}
	case "4h":
		if data.LongerTermContext != nil {
			return data.LongerTermContext.MACDHist
		}
	}
	return nil
}

func enabledSignalMap(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func emaSeriesFromCandles(candles []Candle, period int) []float64 {
	if period <= 0 || len(candles) == 0 {
		return nil
	}
	alpha := 2.0 / float64(period+1)
	out := make([]float64, len(candles))
	out[0] = candles[0].Close
	for i := 1; i < len(candles); i++ {
		out[i] = alpha*candles[i].Close + (1-alpha)*out[i-1]
	}
	return out
}

func positionSideForSymbol(positions []decision.PositionInfo, symbol string) string {
	for _, pos := range positions {
		if market.Normalize(pos.Symbol) == market.Normalize(symbol) {
			return strings.ToLower(pos.Side)
		}
	}
	return ""
}

func positionValueForSymbolSide(positions []decision.PositionInfo, symbol, side string) float64 {
	for _, pos := range positions {
		if market.Normalize(pos.Symbol) != market.Normalize(symbol) || strings.ToLower(pos.Side) != side {
			continue
		}
		price := pos.MarkPrice
		if price <= 0 {
			price = pos.EntryPrice
		}
		return pos.Quantity * price
	}
	return 0
}

func leverageForSymbol(ctx *decision.Context, symbol string) int {
	if symbol == "BTCUSDT" || symbol == "ETHUSDT" {
		return ctx.BTCETHLeverage
	}
	return ctx.AltcoinLeverage
}

func appendSource(values []string, source string) []string {
	for _, value := range values {
		if value == source {
			return values
		}
	}
	return append(values, source)
}

func tierForStrategySymbol(symbol StrategySymbol) string {
	for _, source := range symbol.Sources {
		switch strings.ToLower(strings.TrimSpace(source)) {
		case "core":
			return "core"
		case "trend":
			return "trend"
		}
	}
	if symbol.HasPosition {
		return "position"
	}
	return ""
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func splitLayerDiagnostics(values []string) ([]string, []string) {
	var mainMessages []string
	var positionMessages []string
	for _, value := range values {
		if strings.Contains(value, "持仓") || strings.Contains(value, "保本") || strings.Contains(value, "回撤") || strings.Contains(value, "结构") || strings.Contains(value, "短差") {
			positionMessages = append(positionMessages, value)
			continue
		}
		mainMessages = append(mainMessages, value)
	}
	return mainMessages, positionMessages
}

func sortedStringKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nextCloseTime(now time.Time, timeframe string) time.Time {
	duration := time.Hour
	switch timeframe {
	case "15m":
		duration = 15 * time.Minute
	case "4h":
		duration = 4 * time.Hour
	}
	truncated := now.Truncate(duration)
	if truncated.Equal(now) {
		return now
	}
	return truncated.Add(duration)
}

func openRejectionText(rejections []decision.OpenRejection) []string {
	result := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		text := rejection.Reason
		if text == "" {
			text = strings.Join(rejection.GateReasons, ",")
		}
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func waitReasonSummary(diagnostics []string, rejections []decision.OpenRejection, decisions []decision.Decision) string {
	hasWaitOnly := true
	for _, d := range decisions {
		if d.Action != "" && d.Action != "wait" {
			hasWaitOnly = false
			break
		}
	}
	if !hasWaitOnly {
		return ""
	}
	joined := strings.ToLower(strings.Join(diagnostics, " ; "))
	for _, rejection := range rejections {
		joined += " ; " + strings.ToLower(strings.Join(rejection.GateReasons, " "))
		joined += " ; " + strings.ToLower(rejection.Reason)
		if code, _ := rejection.GateDiagnostics["reason_code"].(string); code != "" {
			joined += " ; " + strings.ToLower(code)
		}
	}
	priorities := []struct {
		code     string
		patterns []string
	}{
		{"suppressed", []string{"suppressed_fast_skip", "已因", "同一结构已因"}},
		{"permanent_skip", []string{"permanent_skip"}},
		{"theoretical_rr_unreachable", []string{"theoretical_rr_unreachable"}},
		{"invalid_structure", []string{"invalid_stop_take_profit_structure", "结构不合法", "signal_invalid_at_birth"}},
		{"target_already_crossed", []string{"target_already_crossed", "越过止盈"}},
		{"chase_too_high", []string{"chase", "追价"}},
		{"structure_rr_too_low", []string{"structure_rr_below_threshold", "remaining_net_rr_too_low", "净rr"}},
		{"pilot_below_threshold", []string{"pilot跳过", "置信度"}},
		{"no_trigger", []string{"fresh entry trigger", "no_trigger", "等待"}},
	}
	for _, item := range priorities {
		for _, pattern := range item.patterns {
			if strings.Contains(joined, strings.ToLower(pattern)) {
				return item.code
			}
		}
	}
	if strings.Contains(joined, "无买卖点") || strings.Contains(joined, "未发现可执行信号") {
		return "no_signal"
	}
	return ""
}

func init() {
	log.SetFlags(log.Flags())
}
