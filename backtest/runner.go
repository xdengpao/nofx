package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nofx/decision"
	"nofx/historydb"
	"nofx/market"
	"nofx/strategy/chanlun"
)

type Runner struct {
	Config      *BacktestConfig
	Store       *historydb.Store
	Clock       time.Time
	Progress    Progress
	CurrentTime time.Time
}

type Progress struct {
	RunID            string    `json:"run_id"`
	Status           string    `json:"status"`
	CurrentTime      time.Time `json:"current_time"`
	BacktestFrom     time.Time `json:"backtest_from"`
	BacktestTo       time.Time `json:"backtest_to"`
	Cycles           int       `json:"cycles"`
	Executions       int       `json:"executions"`
	Signals          int       `json:"signals"`
	Rejections       int       `json:"rejections"`
	Error            string    `json:"error,omitempty"`
	OutputDir        string    `json:"output_dir,omitempty"`
	CompletedPercent float64   `json:"completed_percent,omitempty"`
}

type RunResult struct {
	RunID     string   `json:"run_id"`
	OutputDir string   `json:"output_dir"`
	Report    Report   `json:"report"`
	Progress  Progress `json:"progress"`
}

func NewRunner(cfg *BacktestConfig, store *historydb.Store) (*Runner, error) {
	if cfg == nil {
		return nil, fmt.Errorf("回测配置为空")
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	if store == nil {
		opened, err := historydb.Open(cfg.HistoryDB)
		if err != nil {
			return nil, err
		}
		store = opened
	}
	return &Runner{Config: cfg, Store: store, Clock: cfg.BacktestFromTime()}, nil
}

func (r *Runner) Run(ctx context.Context) (RunResult, error) {
	runID := fmt.Sprintf("bt_%s", time.Now().UTC().Format("20060102_150405"))
	runDir := filepath.Join(r.Config.OutputDir, runID)
	stateDir := filepath.Join(runDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return RunResult{}, err
	}
	if err := decision.InitializeBacktestRuntime(stateDir); err != nil {
		return RunResult{}, err
	}
	if err := r.Config.ValidateHistoryCoverage(ctx, r.Store); err != nil && !r.Config.Data.AllowAutoFetch {
		return RunResult{}, err
	}
	snapshot := r.Config.SanitizedSnapshot()
	snapshotData, _ := json.MarshalIndent(snapshot, "", "  ")
	_ = os.WriteFile(filepath.Join(runDir, "config_snapshot.json"), snapshotData, 0644)

	broker := NewPaperBroker(r.Config.InitialEquity, r.Config.Costs, r.Config.Execution)
	provider := &HistoricalMarketDataProvider{
		Store:  r.Store,
		Source: r.Config.Source,
		Clock: func() time.Time {
			return r.CurrentTime
		},
	}
	policy := r.Config.ProgrammaticPolicy()
	policy.State.Path = filepath.Join(stateDir, "programmatic_strategy_state.json")
	engine, err := chanlun.NewEngine(policy)
	if err != nil {
		return RunResult{}, err
	}
	engine.Clock = func() time.Time { return r.CurrentTime }
	engine.MarketDataProvider = provider.GetMarketData
	engine.DisableOITopFetch = true

	var equity []EquityPoint
	var rejections []decision.OpenRejection
	markers := map[string][]chanlun.SignalMarker{}
	lastEquityPeak := broker.Account.Equity
	r.Progress = Progress{RunID: runID, Status: "running", BacktestFrom: r.Config.BacktestFromTime(), BacktestTo: r.Config.BacktestToTime(), OutputDir: runDir}

	for current := r.Config.BacktestFromTime(); current.Before(r.Config.BacktestToTime()); current = current.Add(time.Duration(r.Config.ScanIntervalMinutes) * time.Minute) {
		if err := ctx.Err(); err != nil {
			r.Progress.Status = "cancelled"
			r.Progress.Error = err.Error()
			break
		}
		r.CurrentTime = current
		r.Progress.CurrentTime = current
		r.Progress.Cycles++

		for _, symbol := range r.Config.Symbols {
			bar, ok, err := r.lastBar(ctx, symbol, current)
			if err != nil {
				return RunResult{}, err
			}
			if ok {
				broker.ProcessBar(symbol, bar, current)
			}
		}

		decisionCtx := r.buildDecisionContext(broker, current)
		full, err := engine.GetFullDecision(decisionCtx)
		if err != nil {
			r.Progress.Error = err.Error()
			continue
		}
		rejections = append(rejections, full.OpenRejections...)
		for _, d := range full.Decisions {
			if d.Symbol == "ALL" || d.Action == "wait" || d.Action == "hold" {
				continue
			}
			broker.SubmitDecision(d, current)
		}
		for _, symbol := range r.Config.Symbols {
			report, ok := engine.LatestSignals("backtest", symbol)
			if !ok {
				report = engine.EmptySignalReport("backtest", symbol)
			}
			key := fmt.Sprintf("%s_%s", symbol, policy.Timeframes.Trade)
			markers[key] = report.SignalMarkers
		}
		broker.recomputeAccount()
		if broker.Account.Equity > lastEquityPeak {
			lastEquityPeak = broker.Account.Equity
		}
		drawdown := 0.0
		if lastEquityPeak > 0 {
			drawdown = (lastEquityPeak - broker.Account.Equity) / lastEquityPeak * 100
		}
		equity = append(equity, EquityPoint{
			Timestamp:     current,
			Equity:        broker.Account.Equity,
			Cash:          broker.Account.Cash,
			UnrealizedPnL: broker.Account.UnrealizedPnL,
			RealizedPnL:   broker.Account.RealizedPnL,
			DrawdownPct:   drawdown,
		})
		r.Progress.Executions = len(broker.Executions)
		r.Progress.Rejections = len(rejections)
		r.Progress.Signals = countMarkers(markers)
		r.Progress.CompletedPercent = current.Sub(r.Config.BacktestFromTime()).Seconds() / r.Config.BacktestToTime().Sub(r.Config.BacktestFromTime()).Seconds() * 100
	}

	for _, symbol := range r.Config.Symbols {
		if bar, ok, err := r.lastBar(ctx, symbol, r.Config.BacktestToTime()); err == nil && ok {
			broker.MarkToMarket(symbol, bar.Close, r.Config.BacktestToTime())
		}
	}
	trades := make([]TradeLifecycle, 0, len(broker.Lifecycles))
	for _, lifecycle := range broker.Lifecycles {
		trades = append(trades, *lifecycle)
	}
	signals := signalOutcomesFromMarkers(markers)
	report := Report{
		RunID:                    runID,
		GeneratedAt:              time.Now().UTC(),
		GitCommit:                gitCommit(),
		ConfigHash:               r.Config.ConfigHash(),
		Timezone:                 r.Config.Timezone,
		WarmupFrom:               r.Config.WarmupFromTime(),
		BacktestFrom:             r.Config.BacktestFromTime(),
		BacktestTo:               r.Config.BacktestToTime(),
		MarketDataSource:         r.Config.Source,
		ExecutionModel:           r.Config.Execution.MarketOrderFill,
		InstrumentMetadataSource: "historydb",
		FundingMode:              "disabled",
		OIMode:                   "disabled",
		LiquidationMode:          "not_modelled",
		CandidatePoolMode:        defaultString(r.Config.Data.CandidatePoolMode, "static_symbols"),
		Assumptions: []string{
			"funding_mode=disabled",
			"oi_mode=disabled",
			"liquidation_mode=not_modelled",
			"market orders fill at next available 3m open",
		},
		ConfigSnapshot:   snapshot,
		Summary:          BuildSummary(r.Config.InitialEquity, broker.Account, trades, broker.Executions, signals, rejections, equity),
		BySymbol:         buildSymbolStats(trades),
		BySignalType:     buildSignalStats(signals),
		RejectionBuckets: buildRejectionBuckets(rejections),
		Cancelled:        r.Progress.Status == "cancelled",
	}
	if r.Progress.Status == "running" {
		r.Progress.Status = "completed"
	}
	artifacts := ReportArtifacts{
		Report:     report,
		Trades:     trades,
		Executions: broker.Executions,
		Signals:    signals,
		Rejections: rejections,
		Equity:     equity,
		Markers:    markers,
	}
	if err := WriteArtifacts(runDir, artifacts); err != nil {
		return RunResult{}, err
	}
	return RunResult{RunID: runID, OutputDir: runDir, Report: report, Progress: r.Progress}, nil
}

func (r *Runner) lastBar(ctx context.Context, symbol string, current time.Time) (market.Kline, bool, error) {
	klines, err := r.Store.LastClosedKlines(ctx, r.Config.Source, symbol, "3m", 1, current)
	if err != nil {
		return market.Kline{}, false, err
	}
	if len(klines) == 0 {
		return market.Kline{}, false, nil
	}
	return klines[0], true, nil
}

func (r *Runner) buildDecisionContext(broker *PaperBroker, now time.Time) *decision.Context {
	frequency := decision.FrequencyPolicy{Mode: "legacy", EffectiveMode: "legacy", AnalysisIntervalMin: r.Config.ScanIntervalMinutes, PromptCandidateLimit: len(r.Config.Symbols)}
	candidates := make([]decision.CandidateCoin, 0, len(r.Config.Symbols))
	for _, symbol := range r.Config.Symbols {
		candidates = append(candidates, decision.CandidateCoin{Symbol: symbol, Sources: []string{"backtest"}, IncludedInPrompt: true})
	}
	return &decision.Context{
		CurrentTime:              now.Format("2006-01-02 15:04:05"),
		TraderID:                 "backtest",
		Exchange:                 "backtest",
		RuntimeMinutes:           int(now.Sub(r.Config.BacktestFromTime()).Minutes()),
		Account:                  broker.AccountInfo(),
		Positions:                broker.DecisionPositions(now),
		CandidateCoins:           candidates,
		BTCETHLeverage:           5,
		AltcoinLeverage:          5,
		MaxRiskPerTrade:          0.02,
		EffectiveMaxRiskPerTrade: 0.02,
		TotalRiskBudget:          0.08,
		MaxDailyLossPct:          100,
		MaxAccountDrawdownPct:    100,
		AnalysisIntervalMin:      r.Config.ScanIntervalMinutes,
		FrequencyPolicy:          &frequency,
		FrequencyState:           &decision.FrequencyState{},
		StrategyRiskPolicy:       &decision.StrategyRiskPolicy{Legacy: true, RollbackLegacyValidation: true, FeeSlippagePct: 0.002, DefaultMinNetRR: 2.5, ADXTimeframe: "1h"},
	}
}

func countMarkers(markers map[string][]chanlun.SignalMarker) int {
	count := 0
	seen := map[string]bool{}
	for _, list := range markers {
		for _, marker := range list {
			key := marker.SignalID + "|" + marker.Status
			if !seen[key] {
				seen[key] = true
				count++
			}
		}
	}
	return count
}

func signalOutcomesFromMarkers(markers map[string][]chanlun.SignalMarker) []SignalOutcome {
	seen := map[string]bool{}
	var out []SignalOutcome
	for _, list := range markers {
		for _, marker := range list {
			key := marker.SignalID + "|" + marker.Status + "|" + marker.Action
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SignalOutcome{
				SignalID:          marker.SignalID,
				Symbol:            marker.Symbol,
				Timeframe:         marker.Timeframe,
				SignalType:        marker.SignalType,
				Direction:         marker.Direction,
				Status:            marker.Status,
				TradeIntent:       marker.TradeIntent,
				PositionSide:      marker.PositionSide,
				SignalCloseTime:   marker.SignalCloseTime,
				DecisionCloseTime: marker.DecisionCloseTime,
				DisplayCloseTime:  marker.DisplayCloseTime,
				Price:             marker.Price,
				Reason:            marker.Reason,
			})
		}
	}
	return out
}

func buildSymbolStats(trades []TradeLifecycle) map[string]SymbolStats {
	stats := map[string]SymbolStats{}
	wins := map[string]int{}
	for _, trade := range trades {
		item := stats[trade.Symbol]
		item.TradeCount++
		item.NetPnL += trade.RealizedPnL
		item.Fees += trade.Fees
		if trade.RealizedPnL >= 0 {
			wins[trade.Symbol]++
		}
		stats[trade.Symbol] = item
	}
	for symbol, item := range stats {
		if item.TradeCount > 0 {
			item.WinRate = float64(wins[symbol]) / float64(item.TradeCount) * 100
			stats[symbol] = item
		}
	}
	return stats
}

func buildSignalStats(signals []SignalOutcome) map[string]SignalStats {
	stats := map[string]SignalStats{}
	for _, signal := range signals {
		key := signal.SignalType
		item := stats[key]
		item.Count++
		switch strings.ToLower(signal.Status) {
		case "executed", "filled":
			item.Executed++
		case "rejected", "failed":
			item.Rejected++
		}
		stats[key] = item
	}
	return stats
}

func buildRejectionBuckets(rejections []decision.OpenRejection) map[string]int {
	buckets := map[string]int{}
	for _, rejection := range rejections {
		reason := rejection.Reason
		if reason == "" {
			reason = strings.Join(rejection.GateReasons, ";")
		}
		if reason == "" {
			reason = "unknown"
		}
		buckets[reason]++
	}
	return buckets
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
