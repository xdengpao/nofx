package optimize

import (
	"time"

	"nofx/backtest"
	"nofx/decision"
	"nofx/logger"
	"nofx/strategy/chanlun"
)

type ReplayReportInput struct {
	Report   logger.ReplayReport `json:"report"`
	Path     string              `json:"path"`
	RunID    string              `json:"run_id,omitempty"`
	DataHash string              `json:"data_hash,omitempty"`
	TraderID string              `json:"trader_id"`
	Exchange string              `json:"exchange"`
	From     string              `json:"from,omitempty"`
	To       string              `json:"to,omitempty"`
}

type BacktestRunInput struct {
	Artifacts *RunArtifacts `json:"-"`
	RunID     string        `json:"run_id,omitempty"`
	DataHash  string        `json:"data_hash,omitempty"`
	TraderID  string        `json:"trader_id"`
	Exchange  string        `json:"exchange"`
}

type RunArtifacts struct {
	RunID      string
	OutputDir  string
	Report     backtest.Report
	Trades     []backtest.TradeLifecycle
	Executions []backtest.ExecutionEvent
	Signals    []backtest.SignalOutcome
	Rejections []decision.OpenRejection
	Equity     []backtest.EquityPoint
	Markers    map[string][]chanlun.SignalMarker
	Structures []StructureSnapshot
	Metrics    *RunMetrics
}

type StructureSnapshot = backtest.StructureSnapshot

type DiagnosisInterval struct {
	ReplayFrom   string `json:"replay_from"`
	ReplayTo     string `json:"replay_to"`
	BacktestFrom string `json:"backtest_from"`
	BacktestTo   string `json:"backtest_to"`
	WarmupFrom   string `json:"warmup_from"`
	Timezone     string `json:"timezone"`
}

type DefectCatalog struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	DiagnosisInterval DiagnosisInterval `json:"diagnosis_interval"`
	Defects           []DefectEntry     `json:"defects"`
	Summary           DefectSummary     `json:"summary"`
}

type DefectEntry struct {
	DefectCode                    string            `json:"defect_code"`
	DescriptionZH                 string            `json:"description_zh"`
	EvidenceRefs                  []EvidenceRef     `json:"evidence_refs"`
	AffectedTraders               []string          `json:"affected_traders"`
	AffectedExchanges             []string          `json:"affected_exchanges"`
	AffectedSymbols               []string          `json:"affected_symbols"`
	AffectedSides                 []string          `json:"affected_sides"`
	AffectedSignalTypes           []string          `json:"affected_signal_types"`
	PrimaryMetric                 string            `json:"primary_metric"`
	MetricDeltaVsHealthySubset    float64           `json:"metric_delta_vs_healthy_subset"`
	SampleCountByTraderExchange   map[string]int    `json:"sample_count_by_trader_exchange"`
	DirectionDistByTraderExchange map[string]string `json:"direction_distribution_by_trader_exchange"`
}

type EvidenceRef struct {
	Source         string `json:"source"`
	Path           string `json:"path"`
	RunID          string `json:"run_id,omitempty"`
	DataHash       string `json:"data_hash,omitempty"`
	TraderID       string `json:"trader_id"`
	Exchange       string `json:"exchange,omitempty"`
	Symbol         string `json:"symbol,omitempty"`
	Side           string `json:"side,omitempty"`
	SignalID       string `json:"signal_id,omitempty"`
	EntryTriggerID string `json:"entry_trigger_id,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	TimeFrom       string `json:"time_from,omitempty"`
	TimeTo         string `json:"time_to,omitempty"`
}

type DefectSummary struct {
	TotalDefects            int            `json:"total_defects"`
	ByDefectCode            map[string]int `json:"by_defect_code"`
	ByTraderExchange        map[string]int `json:"by_trader_exchange"`
	BySignalType            map[string]int `json:"by_signal_type"`
	RejectedNoEvidenceCount int            `json:"rejected_no_evidence_count,omitempty"`
}

type RunMetrics struct {
	RunID              string  `json:"run_id"`
	TraderID           string  `json:"trader_id,omitempty"`
	Exchange           string  `json:"exchange,omitempty"`
	ConfigHash         string  `json:"config_hash"`
	DataHash           string  `json:"data_hash"`
	Timezone           string  `json:"timezone"`
	MetricsSource      string  `json:"metrics_source,omitempty"`
	SymbolSetHash      string  `json:"symbol_set_hash,omitempty"`
	InitialEquity      float64 `json:"initial_equity"`
	FeeModelHash       string  `json:"fee_model_hash,omitempty"`
	SlippageModelHash  string  `json:"slippage_model_hash,omitempty"`
	FundingMode        string  `json:"funding_mode,omitempty"`
	LiquidationMode    string  `json:"liquidation_mode,omitempty"`
	ExecutionModelHash string  `json:"execution_model_hash,omitempty"`

	WinRate                       float64 `json:"win_rate"`
	ProfitFactor                  float64 `json:"profit_factor"`
	NetPnL                        float64 `json:"net_pnl"`
	NetPnLPct                     float64 `json:"net_pnl_pct"`
	MaxDrawdownPct                float64 `json:"max_drawdown_pct"`
	AverageR                      float64 `json:"average_r"`
	AvgHoldMinutes                float64 `json:"avg_hold_minutes"`
	FeeRatio                      float64 `json:"fee_ratio"`
	SlippageRatio                 float64 `json:"slippage_ratio"`
	RejectionRate                 float64 `json:"rejection_rate"`
	MinNotionalRejectionRate      float64 `json:"min_notional_rejection_rate"`
	CircuitBreakerFrequencyPerDay float64 `json:"circuit_breaker_frequency_per_day"`
	TradeCount                    int     `json:"trade_count"`
	RejectionCount                int     `json:"rejection_count"`

	SignalToExecDelay DelayStats                `json:"signal_to_exec_delay"`
	BySymbol          map[string]*BucketMetrics `json:"by_symbol"`
	BySide            map[string]*BucketMetrics `json:"by_side"`
	BySignalType      map[string]*BucketMetrics `json:"by_signal_type"`
	ByMarketState     map[string]*BucketMetrics `json:"by_market_state"`
	ByATRProfile      map[string]*BucketMetrics `json:"by_atr_profile"`
	ByADXRange        map[string]*BucketMetrics `json:"by_adx_range"`
	BySymbolCategory  map[string]*BucketMetrics `json:"by_symbol_category"`
	ByTraderExchange  map[string]*BucketMetrics `json:"by_trader_exchange"`
	BootstrapCIs      map[string]BootstrapCI    `json:"bootstrap_cis,omitempty"`
	CIStatus          string                    `json:"ci_status,omitempty"`
}

type DelayStats struct {
	SignalToDecisionMS float64 `json:"signal_to_decision_ms_avg"`
	DecisionToFillMS   float64 `json:"decision_to_fill_ms_avg"`
	P50MS              float64 `json:"p50_ms,omitempty"`
	P95MS              float64 `json:"p95_ms,omitempty"`
	SampleCount        int     `json:"sample_count"`
}

type BootstrapCI struct {
	Metric      string     `json:"metric"`
	Interval    [2]float64 `json:"interval,omitempty"`
	Status      string     `json:"status"`
	Iterations  int        `json:"iterations,omitempty"`
	SampleCount int        `json:"sample_count"`
	Seed        int64      `json:"seed,omitempty"`
}

type BucketMetrics struct {
	TradeCount                    int     `json:"trade_count"`
	RejectionCount                int     `json:"rejection_count,omitempty"`
	WinRate                       float64 `json:"win_rate"`
	ProfitFactor                  float64 `json:"profit_factor"`
	NetPnL                        float64 `json:"net_pnl"`
	NetPnLPct                     float64 `json:"net_pnl_pct,omitempty"`
	MaxDrawdownPct                float64 `json:"max_drawdown_pct,omitempty"`
	AverageR                      float64 `json:"average_r,omitempty"`
	RejectionRate                 float64 `json:"rejection_rate,omitempty"`
	MinNotionalRejectionRate      float64 `json:"min_notional_rejection_rate,omitempty"`
	CircuitBreakerFrequencyPerDay float64 `json:"circuit_breaker_frequency_per_day,omitempty"`
	SampleCount                   int     `json:"sample_count"`
}

type MetricComparison struct {
	Metric             string     `json:"metric"`
	BaselineValue      float64    `json:"baseline_value"`
	CandidateValue     float64    `json:"candidate_value"`
	AbsoluteDelta      float64    `json:"absolute_delta"`
	RelativeDelta      float64    `json:"relative_delta"`
	Threshold          float64    `json:"threshold"`
	ConfidenceInterval [2]float64 `json:"confidence_interval,omitempty"`
	CIStatus           string     `json:"ci_status,omitempty"`
	Passed             bool       `json:"passed"`
}

type GateInput struct {
	Baseline            *RunMetrics
	Candidate           *RunMetrics
	Config              *OptimizationConfig
	Policy              *CommittedGatePolicy
	ByTraderExchange    map[string]*RunMetrics
	RequirementCoverage *RequirementCoverageSummary
}

type GateResult struct {
	Passed                  bool               `json:"passed"`
	Verdict                 string             `json:"verdict"`
	Reasons                 []string           `json:"reasons"`
	Comparisons             []MetricComparison `json:"comparisons"`
	ByTraderExchangeVerdict map[string]string  `json:"by_trader_exchange_verdict,omitempty"`
	OverrideReason          string             `json:"override_reason,omitempty"`
	OverrideBy              string             `json:"override_by,omitempty"`
	PolicyRef               string             `json:"policy_ref,omitempty"`
	PolicyCommit            string             `json:"policy_commit,omitempty"`
}

type GradualRolloutConfig struct {
	DryRunEnabled              bool     `json:"dry_run_enabled"`
	MaxPositionRatio           float64  `json:"max_position_ratio"`
	RollingWindowTrades        int      `json:"rolling_window_trades"`
	NetPnLDegradationThreshold float64  `json:"net_pnl_degradation_threshold"`
	MaxDrawdownThreshold       float64  `json:"max_drawdown_threshold"`
	RejectionRateThreshold     float64  `json:"rejection_rate_threshold"`
	TraderRolloutOrder         []string `json:"trader_rollout_order,omitempty"`
}

type OptimizationProposal struct {
	ProposalID        string               `json:"proposal_id"`
	DefectCodes       []string             `json:"defect_codes"`
	EvidenceRefs      []EvidenceRef        `json:"evidence_refs"`
	ChangedModules    []string             `json:"changed_modules"`
	ConfigChanges     []ConfigChange       `json:"config_changes"`
	BaselineRunID     string               `json:"baseline_run_id"`
	CandidateRunID    string               `json:"candidate_run_id"`
	DataHash          string               `json:"data_hash"`
	GateResult        string               `json:"gate_result"`
	Rollout           GradualRolloutConfig `json:"rollout"`
	RollbackSwitches  []string             `json:"rollback_switches"`
	ManualReviewNotes []string             `json:"manual_review_notes,omitempty"`
}

type ConfigChange struct {
	Path          string `json:"path"`
	OldValue      any    `json:"old_value,omitempty"`
	NewValue      any    `json:"new_value,omitempty"`
	DefaultValue  any    `json:"default_value,omitempty"`
	RollbackValue any    `json:"rollback_value,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type RequirementCoverageSummary struct {
	Items []RequirementCoverageItem `json:"items"`
}

type RequirementCoverageItem struct {
	Requirement string        `json:"requirement"`
	Status      string        `json:"status"`
	Severity    string        `json:"severity,omitempty"`
	Evidence    []EvidenceRef `json:"evidence,omitempty"`
	Notes       string        `json:"notes,omitempty"`
}

type CanonicalLogFields struct {
	StructureKey      string  `json:"structure_key"`
	SignalID          string  `json:"signal_id"`
	ParentSignalID    string  `json:"parent_signal_id"`
	EntryTriggerID    string  `json:"entry_trigger_id"`
	SourceLayer       string  `json:"source_layer"`
	SignalType        string  `json:"signal_type"`
	AnalysisTimeframe string  `json:"analysis_timeframe"`
	TriggerTimeframe  string  `json:"trigger_timeframe"`
	StructureTarget   float64 `json:"structure_target"`
	SignalCloseTime   int64   `json:"signal_close_time"`
	DecisionCloseTime int64   `json:"decision_close_time"`
	AgeCandles        int     `json:"age_candles"`
	FreshnessState    string  `json:"freshness_state"`
	ReasonCode        string  `json:"reason_code"`
	ConfigHash        string  `json:"config_hash"`
	StrategyVersion   string  `json:"strategy_version"`
}

type FieldResolutionError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}
