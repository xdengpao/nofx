package drl

import (
	"fmt"
	"log"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	Config                 DRLEngineConfig
	Backend                InferenceBackend
	FeatureBuilder         *FeatureBuilder
	Mapper                 *ActionMapper
	Diagnostics            *DiagnosticsCollector
	Lifecycle              *ModelLifecycleManager
	BackendFactory         BackendFactory
	ValidationDataProvider ValidationDataProvider
	MarketDataProvider     func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error)
	DisableOITopFetch      bool
	inferErrorMu           sync.Mutex
	consecutiveInferErrors int
	clock                  func() time.Time
}

type EngineOption func(*Engine)

func WithMarketDataProvider(provider func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error)) EngineOption {
	return func(e *Engine) {
		e.MarketDataProvider = provider
	}
}

func WithClock(clock func() time.Time) EngineOption {
	return func(e *Engine) {
		e.clock = clock
	}
}

func WithDisableOITopFetch(disabled bool) EngineOption {
	return func(e *Engine) {
		e.DisableOITopFetch = disabled
	}
}

func WithBackendFactory(factory BackendFactory) EngineOption {
	return func(e *Engine) {
		if factory != nil {
			e.BackendFactory = factory
		}
	}
}

func WithValidationDataProvider(provider ValidationDataProvider) EngineOption {
	return func(e *Engine) {
		e.ValidationDataProvider = provider
	}
}

func NewEngine(cfg config.DRLStrategyConfig) (*Engine, error) {
	return NewEngineWithBackend(cfg, newDefaultBackend())
}

func NewEngineWithBackend(cfg config.DRLStrategyConfig, backend InferenceBackend, opts ...EngineOption) (*Engine, error) {
	engineCfg, err := engineConfigFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if backend == nil {
		backend = NewStubBackend(0)
	}
	if err := backend.Load(engineCfg.ModelPath, engineCfg.InputShape); err != nil {
		return nil, err
	}
	e := &Engine{
		Config:         engineCfg,
		Backend:        backend,
		BackendFactory: newDefaultBackend,
		FeatureBuilder: NewFeatureBuilder(engineCfg),
		Mapper:         NewActionMapper(engineCfg),
		Diagnostics:    NewDiagnosticsCollector(engineCfg),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	if e.Config.AutoRetrain {
		e.Lifecycle = NewModelLifecycleManager(e, &e.Config)
		e.Lifecycle.StartScheduler()
	}
	return e, nil
}

func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if e == nil {
		return nil, fmt.Errorf("DRL策略引擎未初始化")
	}
	if ctx == nil {
		return nil, fmt.Errorf("DRL策略上下文为空")
	}
	now := e.now()
	symbols := e.symbolsForContext(ctx)
	prep, err := decision.PrepareCycleContext(ctx, decision.CyclePreparationOptions{
		MarketSymbols:           symbols,
		MarketHistoryDepth:      e.Config.MarketHistoryDepth(),
		ClosedKlinesOnly:        true,
		AllowRiskReducingOnHalt: true,
		MarketDataProvider:      e.MarketDataProvider,
		DisableOITopFetch:       e.DisableOITopFetch,
		Clock:                   e.now,
	})
	if err != nil {
		return nil, err
	}

	var candidates []decision.Decision
	var diagnostics []string
	var inferErrs []string
	for _, symbol := range symbols {
		data := ctx.MarketDataMap[market.Normalize(symbol)]
		if data == nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 无市场数据，跳过DRL推理", symbol))
			continue
		}
		klines := data.Klines[e.Config.Timeframe]
		if len(klines) == 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 缺少%s历史K线，跳过DRL推理", symbol, e.Config.Timeframe))
			continue
		}
		pos := findPosition(ctx.Positions, symbol)
		observation, buildErr := e.FeatureBuilder.Build(klines, ctx.Account, pos)
		if buildErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s 特征构建失败: %v", symbol, buildErr))
			continue
		}
		if err := e.Config.validateObservation(observation); err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s %v", symbol, err))
			continue
		}

		start := time.Now()
		rawAction, inferErr := e.infer(observation)
		elapsed := time.Since(start)
		mapped := "error"
		if inferErr != nil {
			e.recordInferError(inferErr)
			inferErrs = append(inferErrs, fmt.Sprintf("%s: %v", symbol, inferErr))
			e.Diagnostics.Record(symbol, e.FeatureBuilder.LastRaw, observation, e.FeatureBuilder.LastStats, rawAction, mapped, elapsed, inferErr)
			continue
		}
		e.recordInferSuccess()
		referencePrice := data.CurrentPrice
		if referencePrice <= 0 && len(klines) > 0 {
			referencePrice = klines[len(klines)-1].Close
		}
		atr := e.FeatureBuilder.LastStats.LastATR
		activeConfig := e.activeConfig()
		mappedDecisions := e.Mapper.Map(rawAction, symbol, pos, ctx.Account, atr, referencePrice, &activeConfig)
		if len(mappedDecisions) > 0 {
			mapped = mappedDecisions[0].Action
		}
		e.Diagnostics.Record(symbol, e.FeatureBuilder.LastRaw, observation, e.FeatureBuilder.LastStats, rawAction, mapped, elapsed, nil)
		candidates = append(candidates, mappedDecisions...)
		diagnostics = append(diagnostics, fmt.Sprintf("%s raw_action=%.4f mapped=%s", symbol, rawAction, mapped))
	}

	valid, rejections := e.validateDRLDecisions(ctx, candidates, prep)
	allDecisions := decision.MergePublicAndStrategyDecisionsWithContext(ctx, prep.PositionDecisions, valid)
	if len(allDecisions) == 0 {
		reason := "DRL策略未发现可执行信号"
		if prep.WaitDecision != nil && strings.TrimSpace(prep.WaitDecision.Reasoning) != "" {
			reason = prep.WaitDecision.Reasoning
		} else if len(inferErrs) > 0 {
			reason = "DRL推理失败: " + strings.Join(inferErrs, "; ")
		}
		allDecisions = []decision.Decision{{
			Symbol:          "ALL",
			Action:          "wait",
			Reasoning:       reason,
			StrategyMode:    StrategyMode,
			StrategyName:    StrategyName,
			StrategyVersion: e.activeModelVersion(),
		}}
	}
	summary := "DRL策略周期完成"
	if len(diagnostics) > 0 {
		summary += ": " + strings.Join(limitStrings(diagnostics, 8), "; ")
	}
	if len(rejections) > 0 {
		summary += "; 风控拒绝 " + strings.Join(openRejectionText(rejections), "; ")
	}
	full := &decision.FullDecision{
		UserPrompt:          "",
		CoTTrace:            summary,
		Decisions:           allDecisions,
		Timestamp:           now,
		WaitReasonSummary:   waitReasonSummary(allDecisions, rejections, inferErrs),
		AICallAttempted:     false,
		AICallSucceeded:     false,
		OpenRejections:      rejections,
		DecisionMode:        StrategyMode,
		StrategyName:        StrategyName,
		StrategyVersion:     e.activeModelVersion(),
		StrategyParams:      e.strategyParams(),
		StrategyDiagnostics: e.Diagnostics.StrategyDiagnostics(),
	}
	if len(inferErrs) > 0 && len(candidates) == 0 {
		return full, fmt.Errorf("DRL推理失败: %s", strings.Join(inferErrs, "; "))
	}
	return full, nil
}

func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	if e.Lifecycle != nil {
		return e.Lifecycle.Close()
	}
	if e.Backend == nil {
		return nil
	}
	return e.Backend.Close()
}

func (e *Engine) Status() Status {
	if e == nil || e.Diagnostics == nil {
		return Status{}
	}
	return e.Diagnostics.Status()
}

func (e *Engine) FeatureSnapshot() map[string]any {
	if e == nil || e.Diagnostics == nil {
		return nil
	}
	return e.Diagnostics.FeatureSnapshot()
}

func (e *Engine) symbolsForContext(ctx *decision.Context) []string {
	seen := map[string]bool{}
	add := func(symbol string) {
		normalized := market.Normalize(symbol)
		if normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
	}
	for _, symbol := range e.Config.Symbols {
		add(symbol)
	}
	for _, pos := range ctx.Positions {
		add(pos.Symbol)
	}
	if len(seen) == 0 {
		for _, coin := range ctx.CandidateCoins {
			add(coin.Symbol)
		}
	}
	symbols := make([]string, 0, len(seen))
	for symbol := range seen {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func (e *Engine) validateDRLDecisions(ctx *decision.Context, strategyDecisions []decision.Decision, prep *decision.CyclePreparation) ([]decision.Decision, []decision.OpenRejection) {
	var riskReducing []decision.Decision
	var openLike []decision.Decision
	for _, d := range strategyDecisions {
		if decision.IsOpenLikeAction(d.Action) {
			openLike = append(openLike, d)
			continue
		}
		riskReducing = append(riskReducing, d)
	}
	validRiskReducing, rrRejections := decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, decision.RiskReducingValidationOptions{Source: StrategyMode})
	rejections := append([]decision.OpenRejection(nil), rrRejections...)
	if prep != nil && prep.RiskIncreaseBlocked {
		for _, d := range openLike {
			reason := fmt.Sprintf("%s %s 被拒绝: %s", d.Symbol, d.Action, prep.StopReason)
			rejections = append(rejections, decision.NewOpenRejectionFromDecision(d, reason))
		}
		return validRiskReducing, rejections
	}
	validOpenLike, openRejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
		Source:   StrategyMode,
		AllowAdd: true,
	})
	rejections = append(rejections, openRejections...)
	valid := append(validRiskReducing, validOpenLike...)
	return valid, rejections
}

func (e *Engine) strategyParams() map[string]any {
	return map[string]any{
		"model_path":           e.activeModelPath(),
		"model_version":        e.activeModelVersion(),
		"observation_window":   e.Config.ObservationWindow,
		"timeframe":            e.Config.Timeframe,
		"action_threshold":     e.Config.ActionThreshold,
		"max_position_pct":     e.Config.MaxPositionPct,
		"default_leverage":     e.Config.DefaultLeverage,
		"stop_loss_atr_mult":   e.Config.StopLossATRMult,
		"take_profit_atr_mult": e.Config.TakeProfitATRMult,
	}
}

func (e *Engine) activeBackend() InferenceBackend {
	if e == nil {
		return nil
	}
	if e.Lifecycle != nil {
		return e.Lifecycle.GetBackendForInference()
	}
	return e.Backend
}

func (e *Engine) infer(observation []float32) (float32, error) {
	if e == nil {
		return 0, fmt.Errorf("DRL策略引擎未初始化")
	}
	if e.Lifecycle != nil {
		return e.Lifecycle.Infer(observation)
	}
	backend := e.activeBackend()
	if backend == nil {
		return 0, fmt.Errorf("DRL推理后端未初始化")
	}
	return backend.Infer(observation)
}

func (e *Engine) recordInferError(err error) {
	if e == nil || err == nil {
		return
	}
	e.inferErrorMu.Lock()
	e.consecutiveInferErrors++
	count := e.consecutiveInferErrors
	e.inferErrorMu.Unlock()
	if count < 3 || e.Lifecycle == nil {
		return
	}
	reason := fmt.Sprintf("连续%d次DRL推理失败: %v", count, err)
	if rollbackErr := e.Lifecycle.Rollback(reason); rollbackErr != nil {
		log.Printf("DRL模型回滚处理异常: %v", rollbackErr)
	}
	e.inferErrorMu.Lock()
	e.consecutiveInferErrors = 0
	e.inferErrorMu.Unlock()
}

func (e *Engine) recordInferSuccess() {
	if e == nil {
		return
	}
	e.inferErrorMu.Lock()
	e.consecutiveInferErrors = 0
	e.inferErrorMu.Unlock()
}

func (e *Engine) activeConfig() DRLEngineConfig {
	if e == nil {
		return DRLEngineConfig{}
	}
	cfg := e.Config
	cfg.ModelPath = e.activeModelPath()
	cfg.ModelVersion = e.activeModelVersion()
	return cfg
}

func (e *Engine) activeModelVersion() string {
	if e == nil {
		return ""
	}
	if e.Lifecycle != nil {
		if version := strings.TrimSpace(e.Lifecycle.ModelVersion()); version != "" {
			return version
		}
	}
	if strings.TrimSpace(e.Config.ModelVersion) != "" {
		return e.Config.ModelVersion
	}
	return "default"
}

func (e *Engine) activeModelPath() string {
	if e == nil {
		return ""
	}
	if e.Lifecycle != nil {
		if path := strings.TrimSpace(e.Lifecycle.ModelPath()); path != "" {
			return path
		}
	}
	return e.Config.ModelPath
}

func (e *Engine) now() time.Time {
	if e != nil && e.clock != nil {
		return e.clock()
	}
	return time.Now()
}

func findPosition(positions []decision.PositionInfo, symbol string) *decision.PositionInfo {
	normalized := market.Normalize(symbol)
	for i := range positions {
		if market.Normalize(positions[i].Symbol) == normalized {
			return &positions[i]
		}
	}
	return nil
}

func limitStrings(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return values
	}
	return values[:max]
}

func openRejectionText(rejections []decision.OpenRejection) []string {
	out := make([]string, 0, len(rejections))
	for _, rejection := range rejections {
		if strings.TrimSpace(rejection.Reason) != "" {
			out = append(out, rejection.Reason)
			continue
		}
		out = append(out, strings.TrimSpace(rejection.Symbol+" "+rejection.Action))
	}
	return out
}

func waitReasonSummary(decisions []decision.Decision, rejections []decision.OpenRejection, inferErrs []string) string {
	if len(inferErrs) > 0 {
		return "DRL推理失败: " + strings.Join(limitStrings(inferErrs, 3), "; ")
	}
	if len(rejections) > 0 {
		return "DRL风控拒绝: " + strings.Join(limitStrings(openRejectionText(rejections), 3), "; ")
	}
	for _, d := range decisions {
		if d.Action == "wait" && strings.TrimSpace(d.Reasoning) != "" {
			return d.Reasoning
		}
	}
	return ""
}
