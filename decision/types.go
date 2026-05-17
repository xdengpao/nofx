package decision

import (
	"regexp"
	"sync"
	"time"
)

// ============================================================================
// 核心数据结构
// ============================================================================

// PositionInfo 持仓信息
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"`
	StopLoss         float64 `json:"stop_loss,omitempty"`
	TakeProfit       float64 `json:"take_profit,omitempty"`
}

// AccountInfo 账户信息
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`
	AvailableBalance float64 `json:"available_balance"`
	TotalPnL         float64 `json:"total_pnl"`
	TotalPnLPct      float64 `json:"total_pnl_pct"`
	MarginUsed       float64 `json:"margin_used"`
	MarginUsedPct    float64 `json:"margin_used_pct"`
	PositionCount    int     `json:"position_count"`
}

// CandidateCoin 候选币种
type CandidateCoin struct {
	Symbol           string   `json:"symbol"`
	Sources          []string `json:"sources"`
	Score            float64  `json:"score,omitempty"`
	Tier             string   `json:"tier,omitempty"`
	PoolScore        float64  `json:"pool_score,omitempty"`
	PoolReasons      []string `json:"pool_reasons,omitempty"`
	MarketState      string   `json:"market_state,omitempty"`
	StateConfidence  int      `json:"state_confidence,omitempty"`
	DataQuality      string   `json:"data_quality,omitempty"` // ok, warn, insufficient
	FilterReason     string   `json:"filter_reason,omitempty"`
	IncludedInPrompt bool     `json:"included_in_prompt"`
	Warnings         []string `json:"warnings,omitempty"`
}

// OITopData 持仓量增长Top数据
type OITopData struct {
	Rank              int
	OIDeltaPercent    float64
	OIDeltaValue      float64
	PriceDeltaPercent float64
	NetLong           float64
	NetShort          float64
}

// CorrelationData 相关性数据
type CorrelationData struct {
	Symbol     string  `json:"symbol"`
	BTCCorr    float64 `json:"btc_correlation"`
	IsHighCorr bool    `json:"is_high_corr"`
	RiskWeight float64 `json:"risk_weight"`
}

// CircuitBreakerState 熔断状态
type CircuitBreakerState struct {
	IsTriggered       bool      `json:"is_triggered"`
	TriggerReason     string    `json:"trigger_reason"`
	TriggerTime       time.Time `json:"trigger_time"`
	CooldownMinutes   int       `json:"cooldown_minutes"`
	ConsecutiveLosses int       `json:"consecutive_losses"`
	DailyLoss         float64   `json:"daily_loss"`
}

// ============================================================================
// 失效条件结构化定义
// ============================================================================

// InvalidationConditionType 失效条件类型
type InvalidationConditionType string

const (
	ICT_EMA_CROSS_DOWN InvalidationConditionType = "ema_cross_down"
	ICT_EMA_CROSS_UP   InvalidationConditionType = "ema_cross_up"
	ICT_PRICE_BELOW    InvalidationConditionType = "price_below"
	ICT_PRICE_ABOVE    InvalidationConditionType = "price_above"
	ICT_RSI_ABOVE      InvalidationConditionType = "rsi_above"
	ICT_RSI_BELOW      InvalidationConditionType = "rsi_below"
	ICT_ADX_BELOW      InvalidationConditionType = "adx_below"
	ICT_MACD_CROSS     InvalidationConditionType = "macd_cross"
	ICT_TREND_REVERSAL InvalidationConditionType = "trend_reversal"
	ICT_CUSTOM         InvalidationConditionType = "custom"
)

// ParsedInvalidationCondition 解析后的失效条件
type ParsedInvalidationCondition struct {
	Type       InvalidationConditionType `json:"type"`
	Timeframe  string                    `json:"timeframe"`
	Indicator  string                    `json:"indicator"`
	Indicator2 string                    `json:"indicator2"`
	Threshold  float64                   `json:"threshold"`
	Direction  string                    `json:"direction"`
	RawText    string                    `json:"raw_text"`
	IsValid    bool                      `json:"is_valid"`
	ParseError string                    `json:"parse_error"`
}

// InvalidationConditionParser 失效条件解析器
type InvalidationConditionParser struct {
	patterns map[InvalidationConditionType]*regexp.Regexp
}

// ============================================================================
// 交易计划
// ============================================================================

// TradePlan 交易计划
type TradePlan struct {
	ID                          string                       `json:"id"`
	TraderID                    string                       `json:"trader_id,omitempty"`
	Symbol                      string                       `json:"symbol"`
	Direction                   string                       `json:"direction"`
	EntryPrice                  float64                      `json:"entry_price"`
	StopLoss                    float64                      `json:"stop_loss"`
	TakeProfit                  float64                      `json:"take_profit"`
	PositionSizeUSD             float64                      `json:"position_size_usd"`
	Leverage                    int                          `json:"leverage"`
	EntryReason                 string                       `json:"entry_reason"`
	InvalidationCondition       string                       `json:"invalidation_condition"`
	ParsedInvalidationCondition *ParsedInvalidationCondition `json:"parsed_invalidation_condition,omitempty"`
	InvalidationPrice           float64                      `json:"invalidation_price"`
	MinHoldMinutes              int                          `json:"min_hold_minutes"`
	CreatedAt                   time.Time                    `json:"created_at"`
	Status                      string                       `json:"status"`
	Confidence                  int                          `json:"confidence"`
	RiskUSD                     float64                      `json:"risk_usd"`

	// 分批止盈相关
	ExecutedTranches    map[int]bool `json:"executed_tranches,omitempty"`
	LastExecutedTranche int          `json:"last_executed_tranche"`
	TotalClosedPercent  float64      `json:"total_closed_percent"`

	// 移动止损相关
	TrailingStopActive bool    `json:"trailing_stop_active"`
	CurrentStopLoss    float64 `json:"current_stop_loss"`

	// 实际成交信息
	ActualQuantity float64 `json:"actual_quantity,omitempty"`
	ActualEntry    float64 `json:"actual_entry,omitempty"`

	// 峰值追踪
	EntryATR       float64 `json:"entry_atr,omitempty"`
	PeakPrice      float64 `json:"peak_price,omitempty"`
	PeakPnLPercent float64 `json:"peak_pnl_percent,omitempty"`

	// 动态止盈追踪
	OriginalTakeProfit float64   `json:"original_take_profit,omitempty"`
	LastTPAdjustTime   time.Time `json:"last_tp_adjust_time,omitempty"`
	LastTPSyncTime     time.Time `json:"last_tp_sync_time,omitempty"`

	// 🆕 新增：连续亏损追踪
	ConsecutiveLosses int `json:"consecutive_losses,omitempty"`

	// 策略风险规范化字段
	ProfileName            string  `json:"profile_name,omitempty"`
	InitialRiskDistance    float64 `json:"initial_risk_distance,omitempty"`
	InitialRiskDistancePct float64 `json:"initial_risk_distance_pct,omitempty"`
	InitialATR             float64 `json:"initial_atr,omitempty"`
	InitialATRTimeframe    string  `json:"initial_atr_timeframe,omitempty"`
	EffectiveStopLoss      float64 `json:"effective_stop_loss,omitempty"`
	EffectiveTakeProfit    float64 `json:"effective_take_profit,omitempty"`
	ExchangeFullTakeProfit float64 `json:"exchange_full_take_profit,omitempty"`
	ExchangeFullTPMode     string  `json:"exchange_full_tp_mode,omitempty"`
	ExchangeFullTPMinRR    float64 `json:"exchange_full_tp_min_rr,omitempty"`
	FeeSlippagePct         float64 `json:"fee_slippage_pct,omitempty"`
	MinNetRR               float64 `json:"min_net_rr,omitempty"`

	// 程序化策略元数据
	StrategyMode      string         `json:"strategy_mode,omitempty"`
	StrategyName      string         `json:"strategy_name,omitempty"`
	StrategyVersion   string         `json:"strategy_version,omitempty"`
	ConfigHash        string         `json:"config_hash,omitempty"`
	SignalID          string         `json:"signal_id,omitempty"`
	SignalType        string         `json:"signal_type,omitempty"`
	SignalTimeframe   string         `json:"signal_timeframe,omitempty"`
	StructureTarget   float64        `json:"structure_target,omitempty"`
	AddCount          int            `json:"add_count,omitempty"`
	AverageEntry      float64        `json:"average_entry,omitempty"`
	LastAddTime       time.Time      `json:"last_add_time,omitempty"`
	StrategyMetadata  map[string]any `json:"strategy_metadata,omitempty"`
	StrategyDiagnosis map[string]any `json:"strategy_diagnostics,omitempty"`
}

// TradePlanManager 交易计划管理器
type TradePlanManager struct {
	plans              map[string]*TradePlan
	positionStartTimes map[string]int64
	mu                 sync.RWMutex
	filePath           string
	autoSave           bool
	lastSaveErr        error
}

// ============================================================================
// 决策相关
// ============================================================================

// Decision AI的交易决策
type Decision struct {
	Symbol                   string                 `json:"symbol"`
	Action                   string                 `json:"action"`
	Leverage                 int                    `json:"leverage,omitempty"`
	PositionSizeUSD          float64                `json:"position_size_usd,omitempty"`
	RequestedPositionSizeUSD float64                `json:"requested_position_size_usd,omitempty"`
	AdjustedPositionSizeUSD  float64                `json:"adjusted_position_size_usd,omitempty"`
	SizingAdjusted           bool                   `json:"sizing_adjusted,omitempty"`
	SizingReason             string                 `json:"sizing_reason,omitempty"`
	StopDistancePct          float64                `json:"stop_distance_pct,omitempty"`
	StopDistanceRatio        float64                `json:"stop_distance_ratio,omitempty"`
	StopDistancePercent      float64                `json:"stop_distance_percent,omitempty"`
	EffectiveRiskPct         float64                `json:"effective_risk_pct,omitempty"`
	StopLoss                 float64                `json:"stop_loss,omitempty"`
	TakeProfit               float64                `json:"take_profit,omitempty"`
	RequestedStopLoss        float64                `json:"requested_stop_loss,omitempty"`
	RequestedTakeProfit      float64                `json:"requested_take_profit,omitempty"`
	EffectiveStopLoss        float64                `json:"effective_stop_loss,omitempty"`
	EffectiveTakeProfit      float64                `json:"effective_take_profit,omitempty"`
	ExchangeFullTakeProfit   float64                `json:"exchange_full_take_profit,omitempty"`
	ExchangeFullTPMode       string                 `json:"exchange_full_tp_mode,omitempty"`
	TakeProfitRatio          float64                `json:"take_profit_ratio,omitempty"`
	TakeProfitPercent        float64                `json:"take_profit_percent,omitempty"`
	NetRR                    float64                `json:"net_rr,omitempty"`
	ProfileName              string                 `json:"profile_name,omitempty"`
	FeeSlippageReserveUSD    float64                `json:"fee_slippage_reserve_usd,omitempty"`
	TotalRiskUSD             float64                `json:"total_risk_usd,omitempty"`
	TotalRiskPct             float64                `json:"total_risk_pct,omitempty"`
	RiskCapReason            string                 `json:"risk_cap_reason,omitempty"`
	NewStopLoss              float64                `json:"new_stop_loss,omitempty"`
	NewTakeProfit            float64                `json:"new_take_profit,omitempty"`
	ClosePercentage          float64                `json:"close_percentage,omitempty"`
	Confidence               int                    `json:"confidence,omitempty"`
	RiskUSD                  float64                `json:"risk_usd,omitempty"`
	Reasoning                string                 `json:"reasoning"`
	InvalidationPrice        float64                `json:"invalidation_price,omitempty"`
	InvalidationCondition    string                 `json:"invalidation_condition,omitempty"`
	MinHoldMinutes           int                    `json:"min_hold_minutes,omitempty"`
	TrancheIndex             int                    `json:"tranche_index,omitempty"`
	RiskNormalization        *OpenRiskNormalization `json:"risk_normalization,omitempty"`
	StrategyMode             string                 `json:"strategy_mode,omitempty"`
	StrategyName             string                 `json:"strategy_name,omitempty"`
	StrategyVersion          string                 `json:"strategy_version,omitempty"`
	ConfigHash               string                 `json:"config_hash,omitempty"`
	SignalID                 string                 `json:"signal_id,omitempty"`
	SignalType               string                 `json:"signal_type,omitempty"`
	SignalTimeframe          string                 `json:"signal_timeframe,omitempty"`
	StructureTarget          float64                `json:"structure_target,omitempty"`
	StrategyMetadata         map[string]any         `json:"strategy_metadata,omitempty"`
	StrategyDiagnosis        map[string]any         `json:"strategy_diagnostics,omitempty"`
	Explanation              *DecisionExplanation   `json:"explanation,omitempty"`
}

// DecisionExplanation 是交易动作的人类可读和机器可消费原因说明。
type DecisionExplanation struct {
	Summary        string           `json:"summary,omitempty"`
	Layer          string           `json:"layer,omitempty"`
	Rule           string           `json:"rule,omitempty"`
	ReasonCode     string           `json:"reason_code,omitempty"`
	Timeframe      string           `json:"timeframe,omitempty"`
	SignalType     string           `json:"signal_type,omitempty"`
	SignalID       string           `json:"signal_id,omitempty"`
	TriggerPrice   float64          `json:"trigger_price,omitempty"`
	ReferencePrice float64          `json:"reference_price,omitempty"`
	Threshold      float64          `json:"threshold,omitempty"`
	CooldownStatus map[string]any   `json:"cooldown_status,omitempty"`
	BudgetStatus   map[string]any   `json:"budget_status,omitempty"`
	RiskChecks     []map[string]any `json:"risk_checks,omitempty"`
	Details        map[string]any   `json:"details,omitempty"`
}

// FullDecision AI的完整决策
type FullDecision struct {
	UserPrompt          string          `json:"user_prompt"`
	CoTTrace            string          `json:"cot_trace"`
	Decisions           []Decision      `json:"decisions"`
	Timestamp           time.Time       `json:"timestamp"`
	AICallAttempted     bool            `json:"ai_call_attempted,omitempty"`
	AICallSucceeded     bool            `json:"ai_call_succeeded,omitempty"`
	AIFailureReason     string          `json:"ai_failure_reason,omitempty"`
	OpenRejections      []OpenRejection `json:"open_rejections,omitempty"`
	DecisionMode        string          `json:"decision_mode,omitempty"`
	StrategyName        string          `json:"strategy_name,omitempty"`
	StrategyVersion     string          `json:"strategy_version,omitempty"`
	ConfigHash          string          `json:"config_hash,omitempty"`
	StrategyParams      map[string]any  `json:"strategy_params,omitempty"`
	StrategyDiagnostics map[string]any  `json:"strategy_diagnostics,omitempty"`
}

// OpenRejection 记录开仓建议被确定性风控拒绝的原因。
type OpenRejection struct {
	Symbol            string                    `json:"symbol"`
	Action            string                    `json:"action"`
	Reason            string                    `json:"reason"`
	GateState         string                    `json:"gate_state,omitempty"`
	GateReasons       []string                  `json:"gate_reasons,omitempty"`
	GateDiagnostics   map[string]any            `json:"gate_diagnostics,omitempty"`
	Simulations       []OpenFrequencySimulation `json:"simulations,omitempty"`
	StrategyMode      string                    `json:"strategy_mode,omitempty"`
	StrategyName      string                    `json:"strategy_name,omitempty"`
	StrategyVersion   string                    `json:"strategy_version,omitempty"`
	ConfigHash        string                    `json:"config_hash,omitempty"`
	SignalID          string                    `json:"signal_id,omitempty"`
	SignalType        string                    `json:"signal_type,omitempty"`
	SignalTimeframe   string                    `json:"signal_timeframe,omitempty"`
	SignalCloseTime   int64                     `json:"signal_close_time,omitempty"`
	DecisionCloseTime int64                     `json:"decision_close_time,omitempty"`
	TradeIntent       string                    `json:"trade_intent,omitempty"`
	StrategyMetadata  map[string]any            `json:"strategy_metadata,omitempty"`
}

// OpenFrequencySimulation 记录软风控放宽的只观测模拟结果。
type OpenFrequencySimulation struct {
	Scenario        string         `json:"scenario"`
	Source          string         `json:"source,omitempty"` // structured, text_inferred
	WouldAllow      bool           `json:"would_allow"`
	Reason          string         `json:"reason,omitempty"`
	OriginalState   string         `json:"original_state,omitempty"`
	SimulatedState  string         `json:"simulated_state,omitempty"`
	MinConfidence   int            `json:"min_confidence,omitempty"`
	EffectiveRisk   float64        `json:"effective_risk,omitempty"`
	AdjustedSizeUSD float64        `json:"adjusted_size_usd,omitempty"`
	Diagnostics     map[string]any `json:"diagnostics,omitempty"`
}

// FrequencyPolicy 是开仓频率相关的运行时策略。
type FrequencyPolicy struct {
	Mode                    string  `json:"mode"`
	EffectiveMode           string  `json:"effective_mode,omitempty"`
	AnalysisIntervalMin     int     `json:"analysis_interval_min"`
	PromptCandidateLimit    int     `json:"prompt_candidate_limit"`
	DailyOpenLimit          int     `json:"daily_open_limit,omitempty"`
	RollbackWindowHours     int     `json:"rollback_window_hours,omitempty"`
	RollbackMinProfitFactor float64 `json:"rollback_min_profit_factor,omitempty"`
	RollbackMaxDrawdownPct  float64 `json:"rollback_max_drawdown_pct,omitempty"`
	HighADXReportOnly       bool    `json:"high_adx_report_only"`
	RRReportOnly            bool    `json:"rr_report_only"`
	RollingGateReportOnly   bool    `json:"rolling_gate_report_only"`
}

// FrequencyState 是开仓频率策略的可观测运行时状态。
type FrequencyState struct {
	OpenCount24h       int     `json:"open_count_24h"`
	ClosedTrades24h    int     `json:"closed_trades_24h"`
	ProfitFactor24h    float64 `json:"profit_factor_24h"`
	Drawdown24hPct     float64 `json:"drawdown_24h_pct"`
	AutoRollbackActive bool    `json:"auto_rollback_active"`
	AutoRollbackReason string  `json:"auto_rollback_reason,omitempty"`
}

// LossModeState 是去重后亏损模式的确定性风控状态。
type LossModeState struct {
	Active          bool      `json:"active"`
	Reason          string    `json:"reason,omitempty"`
	CooldownUntil   time.Time `json:"cooldown_until,omitempty"`
	MaxRiskPerTrade float64   `json:"max_risk_per_trade,omitempty"`
	MaxPositions    int       `json:"max_positions,omitempty"`
	DailyOpenLimit  int       `json:"daily_open_limit,omitempty"`
	MinConfidence   int       `json:"min_confidence,omitempty"`
}

// StrategyRiskPolicy 是 ATR/ADX/profile 风控的运行时策略。
type StrategyRiskPolicy struct {
	Legacy                   bool
	Enabled                  bool
	RollbackLegacyValidation bool
	FeeSlippagePct           float64
	DefaultMinNetRR          float64
	ADXTimeframe             string
	Profiles                 []InstrumentProfile
	SafeMode                 StrategySafeMode
}

type StrategySafeMode struct {
	MaxRiskPct      float64
	MaxPositions    int
	DailyOpenLimit  int
	RequireHours    int
	MinProfitFactor float64
}

type InstrumentProfile struct {
	Name                string
	Symbols             []string
	MatchQuote          string
	MatchType           string
	MinStopPct          float64
	FallbackStopPct     float64
	ATRMultiplier       float64
	ATRTimeframe        string
	MinNetRR            float64
	MaxRiskPct          float64
	RegimeRiskCapPct    float64
	MinADX              float64
	AllowLong           bool
	AllowShort          bool
	MaxSameSideHighCorr int
	MaxSameSideLossPct  float64
	MinOrderValueUSDT   float64
	ExchangeFullTPMode  string
	ExchangeFullTPMinRR float64
}

// ProgrammaticStrategyPolicy 是程序化策略的运行时配置。
type ProgrammaticStrategyPolicy struct {
	DecisionMode       string
	StrategyName       string
	StrategyVersion    string
	ConfigHash         string
	AllowLong          bool
	AllowShort         bool
	EnabledSignals     []string
	Timeframes         ProgrammaticTimeframesPolicy
	HistoryDepth       ProgrammaticHistoryDepth
	SymbolPool         ProgrammaticSymbolPoolPolicy
	MovingAverage      ProgrammaticMAPolicy
	Structure          ProgrammaticStructurePolicy
	Divergence         ProgrammaticDivergencePolicy
	ADX                ProgrammaticADXPolicy
	Position           ProgrammaticPositionPolicy
	PositionManagement ProgrammaticPositionManagementPolicy
	TakeProfit         ProgrammaticTPPolicy
	State              ProgrammaticStatePolicy
}

type ProgrammaticTimeframesPolicy struct {
	Higher string
	Trade  string
	Sub    string
	Micro  string
}

type ProgrammaticHistoryDepth struct {
	M3  int
	M15 int
	H1  int
	H4  int
}

type ProgrammaticSymbolPoolPolicy struct {
	Mode        string
	Symbols     []string
	CoreSymbols []string
}

type ProgrammaticMAPolicy struct {
	ShortPeriod     int
	LongPeriod      int
	KissDistancePct float64
	WetKissBars     int
}

type ProgrammaticStructurePolicy struct {
	Strictness    string
	LeftBars      int
	RightBars     int
	MinStrokeBars int
	MinSwingPct   float64
	ATRMultiplier float64
	Bootstrap     bool
}

type ProgrammaticDivergencePolicy struct {
	Ratio                       float64
	PriceTolerancePct           float64
	PriceToleranceATRMultiplier float64
	RequireBZeroAxis            bool
}

type ProgrammaticADXPolicy struct {
	Period         int
	MinADX         float64
	MicroADXFilter bool
}

type ProgrammaticPositionPolicy struct {
	MaxAddCount       int
	AddSizeMultiplier float64
	PartialClosePct   float64
	AllowReversal     bool
}

type ProgrammaticPositionManagementPolicy struct {
	Enabled           bool
	Timeframes        ProgrammaticManagementTFPolicy
	Breakeven         ProgrammaticBreakevenPolicy
	FloatingDrawdown  ProgrammaticFloatingDrawdownPolicy
	StructureBreak    ProgrammaticStructureBreakPolicy
	ShortTrade        ProgrammaticShortTradePolicy
	PartialCloseGuard ProgrammaticPartialCloseGuardPolicy
}

type ProgrammaticManagementTFPolicy struct {
	Structure string
	Micro     string
}

type ProgrammaticBreakevenPolicy struct {
	Enabled          bool
	TriggerProfitPct float64
	TriggerR         float64
	BufferRatio      float64
}

type ProgrammaticFloatingDrawdownPolicy struct {
	Enabled             bool
	ActivationProfitPct float64
	ActivationR         float64
	DrawdownRatio       float64
	Action              string
}

type ProgrammaticStructureBreakPolicy struct {
	Enabled                 bool
	ConfirmBars             int
	Action                  string
	PartialCloseGuardAction string
}

type ProgrammaticShortTradePolicy struct {
	Enabled         bool
	PartialClosePct float64
}

type ProgrammaticPartialCloseGuardPolicy struct {
	CooldownMinutes     int
	MaxCountPerPosition int
	MaxTotalRatio       float64
	CooldownEnabled     bool
}

type ProgrammaticTPPolicy struct {
	Mode         string
	FallbackMode string
	MinNetRR     float64
}

type ProgrammaticStatePolicy struct {
	Path      string
	Bootstrap bool
}

type OpenRiskNormalization struct {
	ProfileName             string   `json:"profile_name,omitempty"`
	ATRTimeframe            string   `json:"atr_timeframe,omitempty"`
	ATRValue                float64  `json:"atr_value,omitempty"`
	RequestedStopLoss       float64  `json:"requested_stop_loss,omitempty"`
	RequestedTakeProfit     float64  `json:"requested_take_profit,omitempty"`
	EffectiveStopLoss       float64  `json:"effective_stop_loss,omitempty"`
	EffectiveTakeProfit     float64  `json:"effective_take_profit,omitempty"`
	ExchangeFullTakeProfit  float64  `json:"exchange_full_take_profit,omitempty"`
	ExchangeFullTPMode      string   `json:"exchange_full_tp_mode,omitempty"`
	StopDistanceRatio       float64  `json:"stop_distance_ratio,omitempty"`
	StopDistancePct         float64  `json:"stop_distance_pct,omitempty"`
	StopDistancePercent     float64  `json:"stop_distance_percent,omitempty"`
	MinStopDistanceRatio    float64  `json:"min_stop_distance_ratio,omitempty"`
	MinTakeProfitRatio      float64  `json:"min_take_profit_ratio,omitempty"`
	MinNetRR                float64  `json:"min_net_rr,omitempty"`
	TakeProfitRatio         float64  `json:"take_profit_ratio,omitempty"`
	TakeProfitPercent       float64  `json:"take_profit_percent,omitempty"`
	NetRR                   float64  `json:"net_rr,omitempty"`
	RewrittenStop           bool     `json:"rewritten_stop,omitempty"`
	RewrittenTakeProfit     bool     `json:"rewritten_take_profit,omitempty"`
	RewrittenExchangeFullTP bool     `json:"rewritten_exchange_full_tp,omitempty"`
	DegradedATR             bool     `json:"degraded_atr,omitempty"`
	Reasons                 []string `json:"reasons,omitempty"`
}

// EvaluationResult 评估结果
type EvaluationResult struct {
	Action            string
	Reason            string
	NewStopLoss       float64
	NewTakeProfit     float64
	ClosePercentage   float64
	IsHardStop        bool
	IsPlanInvalidated bool
	TrancheIndex      int
	ShouldUpdatePeak  bool
}

// ============================================================================
// 统计相关
// ============================================================================

// TradeStatistics 交易统计
type TradeStatistics struct {
	TotalTrades       int       `json:"total_trades"`
	WinningTrades     int       `json:"winning_trades"`
	LosingTrades      int       `json:"losing_trades"`
	TotalPnL          float64   `json:"total_pnl"`
	AverageWin        float64   `json:"average_win"`
	AverageLoss       float64   `json:"average_loss"`
	WinRate           float64   `json:"win_rate"`
	ProfitFactor      float64   `json:"profit_factor"`
	SharpeRatio       float64   `json:"sharpe_ratio"`
	SortinoRatio      float64   `json:"sortino_ratio"`
	MaxDrawdown       float64   `json:"max_drawdown"`
	AverageHoldTime   float64   `json:"average_hold_time_minutes"`
	ConsecutiveWins   int       `json:"consecutive_wins"`
	ConsecutiveLosses int       `json:"consecutive_losses"`
	MaxConsecLosses   int       `json:"max_consec_losses"`
	LastUpdated       time.Time `json:"last_updated"`
}

// ClosedTradeRecord 已平仓交易记录
type ClosedTradeRecord struct {
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	Source         string    `json:"source,omitempty"`
	CloseReason    string    `json:"close_reason"`
	EntryPrice     float64   `json:"entry_price"`
	ExitPrice      float64   `json:"exit_price"`
	Quantity       float64   `json:"quantity"`
	Leverage       int       `json:"leverage"`
	RealizedPnL    float64   `json:"realized_pnl"`
	PnLPercent     float64   `json:"pnl_percent"`
	HoldingMinutes int64     `json:"holding_minutes"`
	EntryTime      time.Time `json:"entry_time"`
	ExitTime       time.Time `json:"exit_time"`
	Commission     float64   `json:"commission"`
	Direction      string    `json:"direction"`
	PnLUSD         float64   `json:"pnl_usd"`
	ExitReason     string    `json:"exit_reason"`
	PeakPnLPercent float64   `json:"peak_pnl_percent"`
	ClosedAt       time.Time `json:"closed_at"`
}

// ClosedPositionInput 描述一次已确认的平仓事件。
type ClosedPositionInput struct {
	TraderID            string
	Symbol              string
	Side                string
	Source              string
	EntryPrice          float64
	ExitPrice           float64
	Quantity            float64
	Leverage            int
	PnLPercent          float64
	PnLUSD              float64
	Commission          float64
	Reason              string
	EntryTime           time.Time
	CloseTime           time.Time
	HoldingMinutes      float64
	HasExchangeMetadata bool
}

// PersistentData 持久化数据结构
type PersistentData struct {
	Plans              map[string]*TradePlan `json:"plans"`
	PositionStartTimes map[string]int64      `json:"position_start_times,omitempty"`
	Statistics         *TradeStatistics      `json:"statistics"`
	Returns            []float64             `json:"returns"`
	ClosedTrades       []ClosedTradeRecord   `json:"closed_trades"`
	CircuitBreaker     *CircuitBreakerState  `json:"circuit_breaker,omitempty"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

// ============================================================================
// 配置相关
// ============================================================================

// Config 配置结构
type Config struct {
	MaxRiskPerTrade       float64 `json:"max_risk_per_trade"`
	TotalRiskBudget       float64 `json:"total_risk_budget"`
	MaxAccountDrawdownPct float64 `json:"max_account_drawdown_pct"`
	AnalysisIntervalMin   int     `json:"analysis_interval_min"`
	BTCETHLeverage        int     `json:"btc_eth_leverage"`
	AltcoinLeverage       int     `json:"altcoin_leverage"`
	DataDir               string  `json:"data_dir"`
	RiskFreeRate          float64 `json:"risk_free_rate"`
}

// SharpeConfig 夏普比率配置
type SharpeConfig struct {
	RiskFreeRate     float64
	AnnualizeFactor  float64
	MinTradesForCalc int
}

// OpenPositionResult 开仓结果
type OpenPositionResult struct {
	EntryPrice float64
	Quantity   float64
	OrderID    string
	Timestamp  time.Time
}

// ============================================================================
// 市场状态
// ============================================================================

// MarketRegime 市场状态
type MarketRegime string

const (
	RegimeTrending  MarketRegime = "TRENDING"
	RegimeRanging   MarketRegime = "RANGING"
	RegimeVolatile  MarketRegime = "VOLATILE"
	RegimeCrash     MarketRegime = "CRASH"
	RegimeUncertain MarketRegime = "UNCERTAIN"
)

// InvalidationCheckContext 失效条件检查上下文
type InvalidationCheckContext struct {
	EMA20        float64
	EMA50        float64
	RSI14        float64
	ADX14        float64
	DIPlus       float64
	DIMinus      float64
	MACD         float64
	MACDSignal   float64
	MACDHist     float64
	CurrentPrice float64
	VWAP         float64
	BBUpper      float64
	BBLower      float64
	Timeframe    string
}
