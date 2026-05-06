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
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"`
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

	// 🆕 新增：连续亏损追踪
	ConsecutiveLosses int `json:"consecutive_losses,omitempty"`
}

// TradePlanManager 交易计划管理器
type TradePlanManager struct {
	plans       map[string]*TradePlan
	mu          sync.RWMutex
	filePath    string
	autoSave    bool
	lastSaveErr error
}

// ============================================================================
// 决策相关
// ============================================================================

// Decision AI的交易决策
type Decision struct {
	Symbol                string  `json:"symbol"`
	Action                string  `json:"action"`
	Leverage              int     `json:"leverage,omitempty"`
	PositionSizeUSD       float64 `json:"position_size_usd,omitempty"`
	StopLoss              float64 `json:"stop_loss,omitempty"`
	TakeProfit            float64 `json:"take_profit,omitempty"`
	NewStopLoss           float64 `json:"new_stop_loss,omitempty"`
	NewTakeProfit         float64 `json:"new_take_profit,omitempty"`
	ClosePercentage       float64 `json:"close_percentage,omitempty"`
	Confidence            int     `json:"confidence,omitempty"`
	RiskUSD               float64 `json:"risk_usd,omitempty"`
	Reasoning             string  `json:"reasoning"`
	InvalidationPrice     float64 `json:"invalidation_price,omitempty"`
	InvalidationCondition string  `json:"invalidation_condition,omitempty"`
	MinHoldMinutes        int     `json:"min_hold_minutes,omitempty"`
	TrancheIndex          int     `json:"tranche_index,omitempty"`
}

// FullDecision AI的完整决策
type FullDecision struct {
	UserPrompt string     `json:"user_prompt"`
	CoTTrace   string     `json:"cot_trace"`
	Decisions  []Decision `json:"decisions"`
	Timestamp  time.Time  `json:"timestamp"`
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

// PersistentData 持久化数据结构
type PersistentData struct {
	Plans          map[string]*TradePlan `json:"plans"`
	Statistics     *TradeStatistics      `json:"statistics"`
	Returns        []float64             `json:"returns"`
	ClosedTrades   []ClosedTradeRecord   `json:"closed_trades"`
	CircuitBreaker *CircuitBreakerState  `json:"circuit_breaker,omitempty"`
	UpdatedAt      time.Time             `json:"updated_at"`
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
