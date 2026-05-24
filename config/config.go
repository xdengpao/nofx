package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// TraderConfig 单个trader的配置
type TraderConfig struct {
	ID                   string                     `json:"id"`
	Name                 string                     `json:"name"`
	Enabled              bool                       `json:"enabled"`                 // 是否启用该trader
	AIModel              string                     `json:"ai_model"`                // "qwen" or "deepseek"
	DecisionMode         string                     `json:"decision_mode,omitempty"` // ai or programmatic or chanlun_v2
	ProgrammaticStrategy ProgrammaticStrategyConfig `json:"programmatic_strategy,omitempty"`
	ChanlunV2Strategy    ChanlunV2StrategyConfig    `json:"chanlun_v2_strategy,omitempty"`

	// 交易平台选择（二选一）
	Exchange string `json:"exchange"` // "binance" or "hyperliquid"

	// 币安配置
	BinanceAPIKey    string `json:"binance_api_key,omitempty"`
	BinanceSecretKey string `json:"binance_secret_key,omitempty"`

	// Hyperliquid配置
	HyperliquidPrivateKey string `json:"hyperliquid_private_key,omitempty"`
	HyperliquidWalletAddr string `json:"hyperliquid_wallet_addr,omitempty"`
	HyperliquidTestnet    bool   `json:"hyperliquid_testnet,omitempty"`

	// Aster配置
	AsterUser       string `json:"aster_user,omitempty"`        // Aster主钱包地址
	AsterSigner     string `json:"aster_signer,omitempty"`      // Aster API钱包地址
	AsterPrivateKey string `json:"aster_private_key,omitempty"` // Aster API钱包私钥

	// AI配置
	QwenKey     string `json:"qwen_key,omitempty"`
	DeepSeekKey string `json:"deepseek_key,omitempty"`

	// 自定义AI API配置（支持任何OpenAI格式的API）
	CustomAPIURL    string `json:"custom_api_url,omitempty"`
	CustomAPIKey    string `json:"custom_api_key,omitempty"`
	CustomModelName string `json:"custom_model_name,omitempty"`

	InitialBalance      float64                       `json:"initial_balance"`
	CapitalAllocation   TraderCapitalAllocationConfig `json:"capital_allocation,omitempty"`
	ScanIntervalMinutes int                           `json:"scan_interval_minutes"`
}

// TraderCapitalAllocationConfig 控制单个 trader 是否只按指定策略资金参与 sizing 和风险预算。
type TraderCapitalAllocationConfig struct {
	Enabled          bool    `json:"enabled,omitempty"`
	AllocatedBalance float64 `json:"allocated_balance,omitempty"`
}

// LeverageConfig 杠杆配置
type LeverageConfig struct {
	BTCETHLeverage  int `json:"btc_eth_leverage"` // BTC和ETH的杠杆倍数（主账户建议5-50，子账户≤5）
	AltcoinLeverage int `json:"altcoin_leverage"` // 山寨币的杠杆倍数（主账户建议5-20，子账户≤5）
}

// ChanlunV2StrategyConfig 基于 Rust 缠论库的 v2 策略配置
type ChanlunV2StrategyConfig struct {
	Timeframes         map[string]string                 `json:"timeframes,omitempty"`    // higher/trade/sub/micro → 4h/1h/15m/3m
	HistoryDepth       map[string]int                    `json:"history_depth,omitempty"` // timeframe → kline count (700-1000)
	SignalFreshness    ChanlunV2SignalFreshnessConfig    `json:"signal_freshness,omitempty"`
	EntryTiming        ChanlunV2EntryTimingConfig        `json:"entry_timing,omitempty"`
	PositionManagement ChanlunV2PositionManagementConfig `json:"position_management,omitempty"`
	LifecycleStatePath string                            `json:"lifecycle_state_path,omitempty"`
	Structure          map[string]any                    `json:"structure,omitempty"`
	Divergence         map[string]any                    `json:"divergence,omitempty"`
	Signals            map[string]any                    `json:"signals,omitempty"`
	Recursive          map[string]any                    `json:"recursive,omitempty"`
	MultiLevel         map[string]any                    `json:"multi_level,omitempty"`
	Position           map[string]any                    `json:"position,omitempty"`
	Risk               map[string]any                    `json:"risk,omitempty"`
}

// ChanlunV2SignalFreshnessConfig 控制缠论 V2 旧信号是否还能继续作为开仓候选。
type ChanlunV2SignalFreshnessConfig struct {
	Enabled                      *bool          `json:"enabled,omitempty"`
	SoftAgeCandles               int            `json:"soft_age_candles,omitempty"`
	MaxLifetimeCandles           int            `json:"max_lifetime_candles,omitempty"`
	SoftAgeBySignalType          map[string]int `json:"soft_age_by_signal_type,omitempty"`
	MaxLifetimeBySignalType      map[string]int `json:"max_lifetime_by_signal_type,omitempty"`
	MissedTargetGuard            *bool          `json:"missed_target_guard,omitempty"`
	ConfidenceDecayPerAgedCandle int            `json:"confidence_decay_per_aged_candle,omitempty"`
	MinRemainingNetRR            float64        `json:"min_remaining_net_rr,omitempty"`
}

// ChanlunV2EntryTimingConfig 控制 V2 结构信号到可执行入场触发的转换。
type ChanlunV2EntryTimingConfig struct {
	Enabled                 *bool                            `json:"enabled,omitempty"`
	DirectStructureOpen     bool                             `json:"direct_structure_open,omitempty"`
	DirectOpenMaxAgeCandles int                              `json:"direct_open_max_age_candles,omitempty"`
	WatchTimeframe          string                           `json:"watch_timeframe,omitempty"`
	WatchMaxCandles         int                              `json:"watch_max_candles,omitempty"`
	TriggerTimeframe        string                           `json:"trigger_timeframe,omitempty"`
	MaxTriggerAgeCandles    int                              `json:"max_trigger_age_candles,omitempty"`
	AllowedTriggerTypes     []string                         `json:"allowed_trigger_types,omitempty"`
	MinTriggerConfidence    int                              `json:"min_trigger_confidence,omitempty"`
	EntryZone               ChanlunV2EntryZoneConfig         `json:"entry_zone,omitempty"`
	ThirdPointQuality       ChanlunV2ThirdPointQualityConfig `json:"third_point_quality,omitempty"`
	PilotEnabled            bool                             `json:"pilot_enabled,omitempty"`
	PilotRiskFraction       float64                          `json:"pilot_risk_fraction,omitempty"`
}

type ChanlunV2EntryZoneConfig struct {
	Mode                  string  `json:"mode,omitempty"`
	MaxChaseRatio         float64 `json:"max_chase_ratio,omitempty"`
	MinRemainingNetRR     float64 `json:"min_remaining_net_rr,omitempty"`
	MaxChaseATRMultiplier float64 `json:"max_chase_atr_multiplier,omitempty"`
}

type ChanlunV2ThirdPointQualityConfig struct {
	Enabled                   *bool   `json:"enabled,omitempty"`
	QualityTimeframe          string  `json:"quality_timeframe,omitempty"`
	UseATRNormalization       *bool   `json:"use_atr_normalization,omitempty"`
	UseSymbolPercentiles      bool    `json:"use_symbol_percentiles,omitempty"`
	PercentileLookbackCandles int     `json:"percentile_lookback_candles,omitempty"`
	MaxSupportGapPercentile   float64 `json:"max_support_gap_percentile,omitempty"`
	MaxSupportGapPct          float64 `json:"max_support_gap_pct,omitempty"`
	MaxSupportGapATR          float64 `json:"max_support_gap_atr,omitempty"`
	MaxRetracementRatio       float64 `json:"max_retracement_ratio,omitempty"`
	MaxPullbackCandles        int     `json:"max_pullback_candles,omitempty"`
	RangePullbackCandles      int     `json:"range_pullback_candles,omitempty"`
	RangeRiskFraction         float64 `json:"range_risk_fraction,omitempty"`
}

// ChanlunV2PositionManagementConfig 控制 V2 多层风险降低动作。
type ChanlunV2PositionManagementConfig struct {
	Enabled                         *bool   `json:"enabled,omitempty"`
	BreakevenEnabled                *bool   `json:"breakeven_enabled,omitempty"`
	BreakevenTriggerR               float64 `json:"breakeven_trigger_r,omitempty"`
	BreakevenBufferPct              float64 `json:"breakeven_buffer_pct,omitempty"`
	PartialTakeProfitEnabled        *bool   `json:"partial_take_profit_enabled,omitempty"`
	PartialTakeProfitR              float64 `json:"partial_take_profit_r,omitempty"`
	PartialTakeProfitPct            float64 `json:"partial_take_profit_pct,omitempty"`
	StructureBreakEnabled           *bool   `json:"structure_break_enabled,omitempty"`
	StructureBreakTimeframe         string  `json:"structure_break_timeframe,omitempty"`
	StructureBreakConfirmBars       int     `json:"structure_break_confirm_bars,omitempty"`
	FloatingDrawdownEnabled         *bool   `json:"floating_drawdown_enabled,omitempty"`
	FloatingDrawdownActivationR     float64 `json:"floating_drawdown_activation_r,omitempty"`
	FloatingDrawdownPct             float64 `json:"floating_drawdown_pct,omitempty"`
	ReverseSignalCloseEnabled       *bool   `json:"reverse_signal_close_enabled,omitempty"`
	ReverseSignalMinConfidence      int     `json:"reverse_signal_min_confidence,omitempty"`
	PartialCloseCooldownMinutes     int     `json:"partial_close_cooldown_minutes,omitempty"`
	MaxPartialCloseCountPerPosition int     `json:"max_partial_close_count_per_position,omitempty"`
	MaxTotalPartialClosePct         float64 `json:"max_total_partial_close_pct,omitempty"`
}

// DynamicCandidatePoolConfig 动态候选池配置。
type DynamicCandidatePoolConfig struct {
	Enabled                 *bool    `json:"enabled,omitempty"`
	RefreshHour             int      `json:"refresh_hour"`
	TTLHours                int      `json:"ttl_hours"`
	MinPoolSize             int      `json:"min_pool_size"`
	MaxPoolSize             int      `json:"max_pool_size"`
	PromptCandidateLimit    int      `json:"prompt_candidate_limit"`
	CoreSymbols             []string `json:"core_symbols"`
	MinOIValueUSD           float64  `json:"min_oi_value_usd"`
	MinQuoteVolume24hUSD    float64  `json:"min_quote_volume_24h_usd"`
	CooldownDaysAfterLosses int      `json:"cooldown_days_after_losses"`
	ExchangeVolumeTopLimit  int      `json:"exchange_volume_top_limit"`
	SnapshotPath            string   `json:"snapshot_path"`
}

// TradingFrequencyReportOnlyConfig 控制只观测、不实盘生效的频率优化诊断。
type TradingFrequencyReportOnlyConfig struct {
	HighADX           bool `json:"high_adx,omitempty"`
	RRThreshold       bool `json:"rr_threshold,omitempty"`
	RollingGate       bool `json:"rolling_gate,omitempty"`
	GateEffectiveness bool `json:"gate_effectiveness,omitempty"`
}

type TradingFrequencyLoosenModeConfig struct {
	Enabled                  *bool   `json:"enabled,omitempty"`
	InactivityWindowMinutes  int     `json:"inactivity_window_minutes,omitempty"`
	PilotConfidenceDrop      int     `json:"pilot_confidence_drop,omitempty"`
	MinNetRRDelta            float64 `json:"min_net_rr_delta,omitempty"`
	MaxChaseRatioBump        float64 `json:"max_chase_ratio_bump,omitempty"`
	MaxDurationHours         int     `json:"max_duration_hours,omitempty"`
	HardFloorPilotConfidence int     `json:"hard_floor_pilot_confidence,omitempty"`
}

// TradingFrequencyConfig 控制开仓频率灰度档位。
type TradingFrequencyConfig struct {
	Mode                    string                           `json:"mode,omitempty"` // safe, balanced, active
	AnalysisIntervalMinutes int                              `json:"analysis_interval_minutes,omitempty"`
	PromptCandidateLimit    int                              `json:"prompt_candidate_limit,omitempty"`
	DailyOpenLimit          int                              `json:"daily_open_limit,omitempty"`
	RollbackWindowHours     int                              `json:"rollback_window_hours,omitempty"`
	RollbackMinProfitFactor float64                          `json:"rollback_min_profit_factor,omitempty"`
	RollbackMaxDrawdownPct  float64                          `json:"rollback_max_drawdown_pct,omitempty"`
	ReportOnly              TradingFrequencyReportOnlyConfig `json:"report_only,omitempty"`
	LoosenMode              TradingFrequencyLoosenModeConfig `json:"loosen_mode,omitempty"`
}

type TradingFrequencyLoosenModeProfile struct {
	Enabled                  bool
	InactivityWindowMinutes  int
	PilotConfidenceDrop      int
	MinNetRRDelta            float64
	MaxChaseRatioBump        float64
	MaxDurationHours         int
	HardFloorPilotConfidence int
}

// TradingFrequencyProfile 是配置归一化后的运行时策略。
type TradingFrequencyProfile struct {
	Legacy                      bool
	Mode                        string
	EffectiveMode               string
	AnalysisIntervalMinutes     int
	PromptCandidateLimit        int
	DailyOpenLimit              int
	RollbackWindowHours         int
	RollbackMinProfitFactor     float64
	RollbackMaxDrawdownPct      float64
	HighADXReportOnly           bool
	RRReportOnly                bool
	RollingGateReportOnly       bool
	GateEffectivenessReportOnly bool
	LoosenMode                  TradingFrequencyLoosenModeProfile
}

// StrategyRiskConfig 控制 ATR/ADX/profile 风控灰度。
type StrategyRiskConfig struct {
	Enabled                  *bool                     `json:"enabled,omitempty"`
	RollbackLegacyValidation bool                      `json:"rollback_legacy_validation,omitempty"`
	FeeSlippagePct           float64                   `json:"fee_slippage_pct,omitempty"`
	DefaultMinNetRR          float64                   `json:"default_min_net_rr,omitempty"`
	ADXTimeframe             string                    `json:"adx_timeframe,omitempty"`
	Profiles                 []InstrumentProfileConfig `json:"profiles,omitempty"`
	SafeMode                 StrategySafeModeConfig    `json:"safe_mode,omitempty"`
}

// InstrumentProfileConfig 是品种级风控覆盖项。百分数字段可写 1.0 或 0.01，都会归一化为 ratio 0.01。
type InstrumentProfileConfig struct {
	Name                string   `json:"name"`
	Symbols             []string `json:"symbols,omitempty"`
	MatchQuote          string   `json:"match_quote,omitempty"`
	MatchType           string   `json:"match_type,omitempty"`
	MinStopPct          float64  `json:"min_stop_pct,omitempty"`
	FallbackStopPct     float64  `json:"fallback_stop_pct,omitempty"`
	ATRMultiplier       float64  `json:"atr_multiplier,omitempty"`
	ATRTimeframe        string   `json:"atr_timeframe,omitempty"`
	MinNetRR            float64  `json:"min_net_rr,omitempty"`
	MaxRiskPct          float64  `json:"max_risk_pct,omitempty"`
	RegimeRiskCapPct    float64  `json:"regime_risk_cap_pct,omitempty"`
	MinADX              float64  `json:"min_adx,omitempty"`
	AllowLong           *bool    `json:"allow_long,omitempty"`
	AllowShort          *bool    `json:"allow_short,omitempty"`
	MaxSameSideHighCorr int      `json:"max_same_side_high_corr,omitempty"`
	MaxSameSideLossPct  float64  `json:"max_same_side_loss_pct,omitempty"`
	MinOrderValueUSDT   float64  `json:"min_order_value_usdt,omitempty"`
	ExchangeFullTPMode  string   `json:"exchange_full_tp_mode,omitempty"`
	ExchangeFullTPMinRR float64  `json:"exchange_full_tp_min_rr,omitempty"`
}

// StrategySafeModeConfig 控制严格策略首次上线的保守上限。
type StrategySafeModeConfig struct {
	MaxRiskPct      float64 `json:"max_risk_pct,omitempty"`
	MaxPositions    int     `json:"max_positions,omitempty"`
	DailyOpenLimit  int     `json:"daily_open_limit,omitempty"`
	RequireHours    int     `json:"require_hours,omitempty"`
	MinProfitFactor float64 `json:"min_profit_factor,omitempty"`
}

// StrategyRiskProfile 是归一化后的运行时配置，百分数字段均为 ratio。
type StrategyRiskProfile struct {
	Legacy                   bool
	Enabled                  bool
	RollbackLegacyValidation bool
	FeeSlippagePct           float64
	DefaultMinNetRR          float64
	ADXTimeframe             string
	Profiles                 []InstrumentProfileProfile
	SafeMode                 StrategySafeModeProfile
}

type InstrumentProfileProfile struct {
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

type StrategySafeModeProfile struct {
	MaxRiskPct      float64
	MaxPositions    int
	DailyOpenLimit  int
	RequireHours    int
	MinProfitFactor float64
}

const (
	TradingFrequencyModeLegacy   = "legacy"
	TradingFrequencyModeSafe     = "safe"
	TradingFrequencyModeBalanced = "balanced"
	TradingFrequencyModeActive   = "active"

	StrategyRiskTPModeAlgorithmicFull = "algorithmic_full"
	StrategyRiskTPModeLegacyAI        = "legacy_ai"
	StrategyRiskTPModeFinalRTarget    = "final_r_target"

	minAnalysisIntervalMinutes = 9
	minPromptCandidateLimit    = 8
	defaultRollbackWindowHours = 24
	defaultRollbackMinPF       = 0.8
	defaultRollbackDrawdownPct = 2.0
	defaultStrategyFeeSlipPct  = 0.002
	defaultStrategyMinNetRR    = 2.5
	defaultStrategyADXTF       = "1h"
	defaultMinStopFloorPct     = 0.01
)

// Config 总配置
type Config struct {
	Traders              []TraderConfig             `json:"traders"`
	UseDefaultCoins      bool                       `json:"use_default_coins"` // 是否使用默认主流币种列表
	DefaultCoins         []string                   `json:"default_coins"`     // 默认主流币种池
	CoinPoolAPIURL       string                     `json:"coin_pool_api_url"`
	OITopAPIURL          string                     `json:"oi_top_api_url"`
	DynamicCandidatePool DynamicCandidatePoolConfig `json:"dynamic_candidate_pool"`
	TradingFrequency     *TradingFrequencyConfig    `json:"trading_frequency,omitempty"`
	StrategyRisk         *StrategyRiskConfig        `json:"strategy_risk,omitempty"`
	APIServerPort        int                        `json:"api_server_port"`
	MaxDailyLoss         float64                    `json:"max_daily_loss"`
	MaxDrawdown          float64                    `json:"max_drawdown"`
	StopTradingMinutes   int                        `json:"stop_trading_minutes"`
	Leverage             LeverageConfig             `json:"leverage"` // 杠杆配置
}

// LoadConfig 从文件加载配置
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 设置默认值：如果use_default_coins未设置（为false）且没有配置coin_pool_api_url，则默认使用默认币种列表
	if !config.UseDefaultCoins && config.CoinPoolAPIURL == "" {
		config.UseDefaultCoins = true
	}

	// 设置默认币种池
	if len(config.DefaultCoins) == 0 {
		config.DefaultCoins = []string{
			"BTCUSDT",
			"ETHUSDT",
			"SOLUSDT",
			"BNBUSDT",
			"XRPUSDT",
			"DOGEUSDT",
			"ADAUSDT",
			"HYPEUSDT",
		}
	}

	config.DynamicCandidatePool.ApplyDefaults()

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	if profile, err := config.NormalizeTradingFrequency(); err != nil {
		return nil, fmt.Errorf("开仓频率配置失败: %w", err)
	} else if !profile.Legacy {
		config.DynamicCandidatePool.PromptCandidateLimit = profile.PromptCandidateLimit
	}

	return &config, nil
}

// ApplyDefaults 设置动态候选池默认值。
func (c *DynamicCandidatePoolConfig) ApplyDefaults() {
	if c.Enabled == nil {
		enabled := true
		c.Enabled = &enabled
	}
	if c.RefreshHour <= 0 || c.RefreshHour > 23 {
		c.RefreshHour = 8
	}
	if c.TTLHours <= 0 {
		c.TTLHours = 24
	}
	if c.MinPoolSize <= 0 {
		c.MinPoolSize = 15
	}
	if c.MaxPoolSize <= 0 {
		c.MaxPoolSize = 30
	}
	if c.MaxPoolSize < c.MinPoolSize {
		c.MaxPoolSize = c.MinPoolSize
	}
	if c.PromptCandidateLimit <= 0 {
		c.PromptCandidateLimit = 8
	}
	if c.PromptCandidateLimit > c.MaxPoolSize {
		c.PromptCandidateLimit = c.MaxPoolSize
	}
	if len(c.CoreSymbols) == 0 {
		c.CoreSymbols = []string{"BTCUSDT", "ETHUSDT"}
	}
	if c.MinOIValueUSD <= 0 {
		c.MinOIValueUSD = 15_000_000
	}
	if c.MinQuoteVolume24hUSD <= 0 {
		c.MinQuoteVolume24hUSD = 20_000_000
	}
	if c.CooldownDaysAfterLosses <= 0 {
		c.CooldownDaysAfterLosses = 2
	}
	if c.ExchangeVolumeTopLimit <= 0 {
		c.ExchangeVolumeTopLimit = 30
	}
	if c.SnapshotPath == "" {
		c.SnapshotPath = "data/dynamic_candidate_pool.json"
	}
}

// IsEnabled 返回动态候选池是否启用。
func (c DynamicCandidatePoolConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// NormalizeChanlunV2StrategyConfig 填充缠论 V2 运行时默认配置。
func NormalizeChanlunV2StrategyConfig(cfg ChanlunV2StrategyConfig) ChanlunV2StrategyConfig {
	cfg.SignalFreshness = NormalizeChanlunV2SignalFreshness(cfg.SignalFreshness)
	cfg.EntryTiming = NormalizeChanlunV2EntryTiming(cfg.EntryTiming)
	cfg.PositionManagement = NormalizeChanlunV2PositionManagement(cfg.PositionManagement)
	return cfg
}

// NormalizeChanlunV2SignalFreshness 返回保守的新鲜度策略，旧配置缺省时自动启用。
func NormalizeChanlunV2SignalFreshness(cfg ChanlunV2SignalFreshnessConfig) ChanlunV2SignalFreshnessConfig {
	if cfg.Enabled == nil {
		cfg.Enabled = boolPtr(true)
	}
	if cfg.MissedTargetGuard == nil {
		cfg.MissedTargetGuard = boolPtr(true)
	}
	if cfg.SoftAgeCandles <= 0 || cfg.SoftAgeCandles > 48 {
		cfg.SoftAgeCandles = 1
	}
	if cfg.MaxLifetimeCandles <= 0 || cfg.MaxLifetimeCandles > 96 || cfg.MaxLifetimeCandles < cfg.SoftAgeCandles {
		cfg.MaxLifetimeCandles = 2
		if cfg.MaxLifetimeCandles < cfg.SoftAgeCandles {
			cfg.MaxLifetimeCandles = cfg.SoftAgeCandles
		}
	}
	if cfg.ConfidenceDecayPerAgedCandle <= 0 || cfg.ConfidenceDecayPerAgedCandle > 20 {
		cfg.ConfidenceDecayPerAgedCandle = 10
	}
	if cfg.MinRemainingNetRR < 0 {
		cfg.MinRemainingNetRR = 0
	}
	cfg.SoftAgeBySignalType = normalizeChanlunV2SignalAgeOverrides(cfg.SoftAgeBySignalType, 48)
	cfg.MaxLifetimeBySignalType = normalizeChanlunV2SignalAgeOverrides(cfg.MaxLifetimeBySignalType, 96)
	for signalType, soft := range cfg.SoftAgeBySignalType {
		if maxLife, ok := cfg.MaxLifetimeBySignalType[signalType]; ok && maxLife < soft {
			cfg.MaxLifetimeBySignalType[signalType] = soft
		}
	}
	return cfg
}

func normalizeChanlunV2SignalAgeOverrides(values map[string]int, maxAllowed int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	out := map[string]int{}
	for key, value := range values {
		signalType := strings.ToLower(strings.TrimSpace(key))
		if !isValidProgrammaticSignalType(signalType) || value <= 0 || value > maxAllowed {
			continue
		}
		out[signalType] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeChanlunV2EntryTiming 返回 V2 结构到入场触发的保守默认配置。
func NormalizeChanlunV2EntryTiming(cfg ChanlunV2EntryTimingConfig) ChanlunV2EntryTimingConfig {
	if cfg.Enabled == nil {
		cfg.Enabled = boolPtr(true)
	}
	cfg.WatchTimeframe = normalizeChanlunV2Timeframe(cfg.WatchTimeframe, "15m")
	if cfg.WatchMaxCandles <= 0 || cfg.WatchMaxCandles > 96 {
		cfg.WatchMaxCandles = 8
	}
	cfg.TriggerTimeframe = normalizeChanlunV2Timeframe(cfg.TriggerTimeframe, cfg.WatchTimeframe)
	if cfg.MaxTriggerAgeCandles <= 0 || cfg.MaxTriggerAgeCandles > 16 {
		cfg.MaxTriggerAgeCandles = 1
	}
	if cfg.DirectOpenMaxAgeCandles < 0 || cfg.DirectOpenMaxAgeCandles > 16 {
		cfg.DirectOpenMaxAgeCandles = 0
	}
	cfg.AllowedTriggerTypes = normalizeChanlunV2TriggerTypes(cfg.AllowedTriggerTypes)
	if cfg.MinTriggerConfidence <= 0 || cfg.MinTriggerConfidence > 100 {
		cfg.MinTriggerConfidence = 65
	}
	cfg.EntryZone = NormalizeChanlunV2EntryZone(cfg.EntryZone)
	cfg.ThirdPointQuality = NormalizeChanlunV2ThirdPointQuality(cfg.ThirdPointQuality)
	if cfg.PilotRiskFraction < 0 || cfg.PilotRiskFraction > 1 {
		cfg.PilotRiskFraction = 0
	}
	return cfg
}

func NormalizeChanlunV2EntryZone(cfg ChanlunV2EntryZoneConfig) ChanlunV2EntryZoneConfig {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = "structure_range"
	}
	if cfg.Mode != "structure_range" && cfg.Mode != "atr" {
		cfg.Mode = "structure_range"
	}
	if cfg.MaxChaseRatio <= 0 || cfg.MaxChaseRatio > 1 {
		cfg.MaxChaseRatio = 0.35
	}
	if cfg.MinRemainingNetRR <= 0 {
		cfg.MinRemainingNetRR = 2.5
	}
	if cfg.MinRemainingNetRR < 1 {
		cfg.MinRemainingNetRR = 1
	}
	if cfg.MaxChaseATRMultiplier <= 0 || cfg.MaxChaseATRMultiplier > 10 {
		cfg.MaxChaseATRMultiplier = 0.6
	}
	return cfg
}

func NormalizeChanlunV2ThirdPointQuality(cfg ChanlunV2ThirdPointQualityConfig) ChanlunV2ThirdPointQualityConfig {
	if cfg.Enabled == nil {
		cfg.Enabled = boolPtr(true)
	}
	cfg.QualityTimeframe = strings.ToLower(strings.TrimSpace(cfg.QualityTimeframe))
	switch cfg.QualityTimeframe {
	case "watch", "trade", "trigger", "3m", "15m", "1h", "4h":
	default:
		cfg.QualityTimeframe = "watch"
	}
	if cfg.UseATRNormalization == nil {
		cfg.UseATRNormalization = boolPtr(true)
	}
	if cfg.PercentileLookbackCandles <= 0 || cfg.PercentileLookbackCandles > 5000 {
		cfg.PercentileLookbackCandles = 480
	}
	if cfg.MaxSupportGapPercentile <= 0 || cfg.MaxSupportGapPercentile > 100 {
		cfg.MaxSupportGapPercentile = 70
	}
	if cfg.MaxSupportGapPct <= 0 || cfg.MaxSupportGapPct > 20 {
		cfg.MaxSupportGapPct = 1.0
	}
	if cfg.MaxSupportGapATR <= 0 || cfg.MaxSupportGapATR > 10 {
		cfg.MaxSupportGapATR = 0.6
	}
	if cfg.MaxRetracementRatio <= 0 || cfg.MaxRetracementRatio > 1 {
		cfg.MaxRetracementRatio = 0.55
	}
	if cfg.MaxPullbackCandles <= 0 || cfg.MaxPullbackCandles > 96 {
		cfg.MaxPullbackCandles = 5
	}
	if cfg.RangePullbackCandles <= 0 || cfg.RangePullbackCandles < cfg.MaxPullbackCandles || cfg.RangePullbackCandles > 192 {
		cfg.RangePullbackCandles = 8
	}
	if cfg.RangeRiskFraction <= 0 || cfg.RangeRiskFraction > 1 {
		cfg.RangeRiskFraction = 0.5
	}
	return cfg
}

func NormalizeChanlunV2PositionManagement(cfg ChanlunV2PositionManagementConfig) ChanlunV2PositionManagementConfig {
	if cfg.Enabled == nil {
		cfg.Enabled = boolPtr(true)
	}
	if cfg.BreakevenEnabled == nil {
		cfg.BreakevenEnabled = boolPtr(true)
	}
	if cfg.BreakevenTriggerR <= 0 || cfg.BreakevenTriggerR > 20 {
		cfg.BreakevenTriggerR = 1.0
	}
	if cfg.BreakevenBufferPct < 0 || cfg.BreakevenBufferPct > 10 {
		cfg.BreakevenBufferPct = 0.05
	}
	if cfg.PartialTakeProfitEnabled == nil {
		cfg.PartialTakeProfitEnabled = boolPtr(true)
	}
	if cfg.PartialTakeProfitR <= 0 || cfg.PartialTakeProfitR > 50 {
		cfg.PartialTakeProfitR = 1.5
	}
	if cfg.PartialTakeProfitPct <= 0 || cfg.PartialTakeProfitPct > 100 {
		cfg.PartialTakeProfitPct = 50
	}
	if cfg.StructureBreakEnabled == nil {
		cfg.StructureBreakEnabled = boolPtr(true)
	}
	cfg.StructureBreakTimeframe = normalizeChanlunV2Timeframe(cfg.StructureBreakTimeframe, "15m")
	if cfg.StructureBreakConfirmBars <= 0 || cfg.StructureBreakConfirmBars > 10 {
		cfg.StructureBreakConfirmBars = 2
	}
	if cfg.FloatingDrawdownEnabled == nil {
		cfg.FloatingDrawdownEnabled = boolPtr(true)
	}
	if cfg.FloatingDrawdownActivationR <= 0 || cfg.FloatingDrawdownActivationR > 50 {
		cfg.FloatingDrawdownActivationR = 1.5
	}
	if cfg.FloatingDrawdownPct <= 0 || cfg.FloatingDrawdownPct > 100 {
		cfg.FloatingDrawdownPct = 40
	}
	if cfg.ReverseSignalCloseEnabled == nil {
		cfg.ReverseSignalCloseEnabled = boolPtr(true)
	}
	if cfg.ReverseSignalMinConfidence <= 0 || cfg.ReverseSignalMinConfidence > 100 {
		cfg.ReverseSignalMinConfidence = 60
	}
	if cfg.PartialCloseCooldownMinutes < 0 || cfg.PartialCloseCooldownMinutes > 1440 {
		cfg.PartialCloseCooldownMinutes = 15
	} else if cfg.PartialCloseCooldownMinutes == 0 {
		cfg.PartialCloseCooldownMinutes = 15
	}
	if cfg.MaxPartialCloseCountPerPosition <= 0 || cfg.MaxPartialCloseCountPerPosition > 10 {
		cfg.MaxPartialCloseCountPerPosition = 2
	}
	if cfg.MaxTotalPartialClosePct <= 0 || cfg.MaxTotalPartialClosePct > 100 {
		cfg.MaxTotalPartialClosePct = 50
	}
	return cfg
}

func normalizeChanlunV2Timeframe(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !isSupportedProgrammaticTimeframe(value) {
		value = fallback
	}
	if !isSupportedProgrammaticTimeframe(value) {
		return "15m"
	}
	return value
}

func normalizeChanlunV2TriggerTypes(values []string) []string {
	allowed := map[string]bool{
		"pullback_retest_resume": true,
		"breakout_continuation":  true,
		"micro_reversal_confirm": true,
	}
	if len(values) == 0 {
		return []string{"pullback_retest_resume", "breakout_continuation", "micro_reversal_confirm"}
	}
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		kind := strings.ToLower(strings.TrimSpace(value))
		if !allowed[kind] || seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	if len(out) == 0 {
		return []string{"pullback_retest_resume", "breakout_continuation", "micro_reversal_confirm"}
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}

// NormalizeTradingFrequency 归一化开仓频率配置。旧配置缺少 trading_frequency 时保持现状。
func (c *Config) NormalizeTradingFrequency() (TradingFrequencyProfile, error) {
	promptLimit := c.DynamicCandidatePool.PromptCandidateLimit
	if promptLimit <= 0 {
		promptLimit = minPromptCandidateLimit
	}
	maxPromptLimit := c.DynamicCandidatePool.MaxPoolSize
	if maxPromptLimit <= 0 {
		maxPromptLimit = 30
	}

	if c.TradingFrequency == nil {
		return TradingFrequencyProfile{
			Legacy:                  true,
			Mode:                    TradingFrequencyModeLegacy,
			EffectiveMode:           TradingFrequencyModeLegacy,
			AnalysisIntervalMinutes: 15,
			PromptCandidateLimit:    promptLimit,
		}, nil
	}

	tf := c.TradingFrequency
	mode := tf.Mode
	if mode == "" {
		mode = TradingFrequencyModeBalanced
	}

	profile := TradingFrequencyProfile{
		Mode:                    mode,
		EffectiveMode:           mode,
		RollbackWindowHours:     defaultRollbackWindowHours,
		RollbackMinProfitFactor: defaultRollbackMinPF,
		RollbackMaxDrawdownPct:  defaultRollbackDrawdownPct,
		LoosenMode:              defaultTradingFrequencyLoosenMode(),
	}

	switch mode {
	case TradingFrequencyModeSafe:
		profile.AnalysisIntervalMinutes = 15
		profile.PromptCandidateLimit = minPromptCandidateLimit
	case TradingFrequencyModeBalanced:
		profile.AnalysisIntervalMinutes = 12
		profile.PromptCandidateLimit = 10
		profile.HighADXReportOnly = true
		profile.RRReportOnly = true
		profile.RollingGateReportOnly = true
	case TradingFrequencyModeActive:
		profile.AnalysisIntervalMinutes = 9
		profile.PromptCandidateLimit = 12
		profile.DailyOpenLimit = 4
		profile.HighADXReportOnly = true
		profile.RRReportOnly = true
		profile.RollingGateReportOnly = true
	default:
		return TradingFrequencyProfile{}, fmt.Errorf("trading_frequency.mode必须是 safe、balanced 或 active: %q", mode)
	}

	if tf.AnalysisIntervalMinutes > 0 {
		profile.AnalysisIntervalMinutes = tf.AnalysisIntervalMinutes
	}
	if profile.AnalysisIntervalMinutes < minAnalysisIntervalMinutes {
		return TradingFrequencyProfile{}, fmt.Errorf("analysis_interval_minutes不能低于%d分钟: %d", minAnalysisIntervalMinutes, profile.AnalysisIntervalMinutes)
	}

	if tf.PromptCandidateLimit > 0 {
		profile.PromptCandidateLimit = tf.PromptCandidateLimit
	}
	if profile.PromptCandidateLimit < minPromptCandidateLimit {
		return TradingFrequencyProfile{}, fmt.Errorf("prompt_candidate_limit不能低于%d: %d", minPromptCandidateLimit, profile.PromptCandidateLimit)
	}
	if profile.PromptCandidateLimit > maxPromptLimit {
		return TradingFrequencyProfile{}, fmt.Errorf("prompt_candidate_limit不能超过dynamic_candidate_pool.max_pool_size(%d): %d", maxPromptLimit, profile.PromptCandidateLimit)
	}

	if tf.DailyOpenLimit > 0 {
		profile.DailyOpenLimit = tf.DailyOpenLimit
	}
	if tf.DailyOpenLimit < 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("daily_open_limit不能为负数: %d", tf.DailyOpenLimit)
	}

	if tf.RollbackWindowHours > 0 {
		profile.RollbackWindowHours = tf.RollbackWindowHours
	}
	if tf.RollbackWindowHours < 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_window_hours不能为负数: %d", tf.RollbackWindowHours)
	}
	if profile.RollbackWindowHours <= 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_window_hours必须大于0")
	}
	if tf.RollbackMinProfitFactor > 0 {
		profile.RollbackMinProfitFactor = tf.RollbackMinProfitFactor
	}
	if tf.RollbackMinProfitFactor < 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_min_profit_factor不能为负数: %.4f", tf.RollbackMinProfitFactor)
	}
	if profile.RollbackMinProfitFactor <= 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_min_profit_factor必须大于0")
	}
	if tf.RollbackMaxDrawdownPct > 0 {
		profile.RollbackMaxDrawdownPct = tf.RollbackMaxDrawdownPct
	}
	if tf.RollbackMaxDrawdownPct < 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_max_drawdown_pct不能为负数: %.4f", tf.RollbackMaxDrawdownPct)
	}
	if profile.RollbackMaxDrawdownPct <= 0 {
		return TradingFrequencyProfile{}, fmt.Errorf("rollback_max_drawdown_pct必须大于0")
	}

	if tf.ReportOnly.HighADX {
		profile.HighADXReportOnly = true
	}
	if tf.ReportOnly.RRThreshold {
		profile.RRReportOnly = true
	}
	if tf.ReportOnly.RollingGate {
		profile.RollingGateReportOnly = true
	}
	if tf.ReportOnly.GateEffectiveness {
		profile.GateEffectivenessReportOnly = true
	}
	loosenMode, err := normalizeTradingFrequencyLoosenMode(tf.LoosenMode)
	if err != nil {
		return TradingFrequencyProfile{}, err
	}
	profile.LoosenMode = loosenMode

	return profile, nil
}

func defaultTradingFrequencyLoosenMode() TradingFrequencyLoosenModeProfile {
	return TradingFrequencyLoosenModeProfile{
		Enabled:                  true,
		InactivityWindowMinutes:  720,
		PilotConfidenceDrop:      10,
		MinNetRRDelta:            -0.4,
		MaxChaseRatioBump:        0.05,
		MaxDurationHours:         24,
		HardFloorPilotConfidence: 60,
	}
}

func normalizeTradingFrequencyLoosenMode(cfg TradingFrequencyLoosenModeConfig) (TradingFrequencyLoosenModeProfile, error) {
	profile := defaultTradingFrequencyLoosenMode()
	if cfg.Enabled != nil {
		profile.Enabled = *cfg.Enabled
	}
	if cfg.InactivityWindowMinutes > 0 {
		profile.InactivityWindowMinutes = cfg.InactivityWindowMinutes
	}
	if profile.InactivityWindowMinutes < 60 || profile.InactivityWindowMinutes > 7*24*60 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.inactivity_window_minutes必须在60-10080之间: %d", profile.InactivityWindowMinutes)
	}
	if cfg.PilotConfidenceDrop > 0 {
		profile.PilotConfidenceDrop = cfg.PilotConfidenceDrop
	}
	if profile.PilotConfidenceDrop < 0 || profile.PilotConfidenceDrop > 50 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.pilot_confidence_drop必须在0-50之间: %d", profile.PilotConfidenceDrop)
	}
	if cfg.MinNetRRDelta != 0 {
		profile.MinNetRRDelta = cfg.MinNetRRDelta
	}
	if profile.MinNetRRDelta < -5 || profile.MinNetRRDelta > 5 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.min_net_rr_delta必须在-5到5之间: %.4f", profile.MinNetRRDelta)
	}
	if cfg.MaxChaseRatioBump != 0 {
		profile.MaxChaseRatioBump = cfg.MaxChaseRatioBump
	}
	if profile.MaxChaseRatioBump < 0 || profile.MaxChaseRatioBump > 0.5 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.max_chase_ratio_bump必须在0-0.5之间: %.4f", profile.MaxChaseRatioBump)
	}
	if cfg.MaxDurationHours > 0 {
		profile.MaxDurationHours = cfg.MaxDurationHours
	}
	if profile.MaxDurationHours < 1 || profile.MaxDurationHours > 168 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.max_duration_hours必须在1-168之间: %d", profile.MaxDurationHours)
	}
	if cfg.HardFloorPilotConfidence > 0 {
		profile.HardFloorPilotConfidence = cfg.HardFloorPilotConfidence
	}
	if profile.HardFloorPilotConfidence < 1 || profile.HardFloorPilotConfidence > 100 {
		return TradingFrequencyLoosenModeProfile{}, fmt.Errorf("trading_frequency.loosen_mode.hard_floor_pilot_confidence必须在1-100之间: %d", profile.HardFloorPilotConfidence)
	}
	return profile, nil
}

// NormalizeStrategyRisk 归一化策略风控配置。旧配置缺少 strategy_risk 时保持 legacy 行为。
func (c *Config) NormalizeStrategyRisk() (StrategyRiskProfile, error) {
	if c.StrategyRisk == nil {
		return StrategyRiskProfile{
			Legacy:                   true,
			Enabled:                  false,
			RollbackLegacyValidation: true,
			FeeSlippagePct:           defaultStrategyFeeSlipPct,
			DefaultMinNetRR:          defaultStrategyMinNetRR,
			ADXTimeframe:             defaultStrategyADXTF,
			Profiles:                 defaultStrategyProfiles(defaultStrategyMinNetRR),
			SafeMode:                 defaultStrategySafeMode(),
		}, nil
	}

	cfg := c.StrategyRisk
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}

	feeSlippage, err := normalizePercentRatio(cfg.FeeSlippagePct, defaultStrategyFeeSlipPct, "strategy_risk.fee_slippage_pct")
	if err != nil {
		return StrategyRiskProfile{}, err
	}
	minRR := cfg.DefaultMinNetRR
	if minRR <= 0 {
		minRR = defaultStrategyMinNetRR
	}
	if minRR < 1 {
		return StrategyRiskProfile{}, fmt.Errorf("strategy_risk.default_min_net_rr不能低于1: %.4f", minRR)
	}

	adxTimeframe := cfg.ADXTimeframe
	if adxTimeframe == "" {
		adxTimeframe = defaultStrategyADXTF
	}
	if !isSupportedStrategyTimeframe(adxTimeframe) {
		return StrategyRiskProfile{}, fmt.Errorf("strategy_risk.adx_timeframe必须是 15m、1h 或 4h: %q", adxTimeframe)
	}

	profiles, err := normalizeInstrumentProfiles(cfg.Profiles, minRR)
	if err != nil {
		return StrategyRiskProfile{}, err
	}
	safeMode, err := normalizeStrategySafeMode(cfg.SafeMode)
	if err != nil {
		return StrategyRiskProfile{}, err
	}

	return StrategyRiskProfile{
		Legacy:                   false,
		Enabled:                  enabled,
		RollbackLegacyValidation: cfg.RollbackLegacyValidation,
		FeeSlippagePct:           feeSlippage,
		DefaultMinNetRR:          minRR,
		ADXTimeframe:             adxTimeframe,
		Profiles:                 profiles,
		SafeMode:                 safeMode,
	}, nil
}

func normalizeInstrumentProfiles(overrides []InstrumentProfileConfig, defaultMinRR float64) ([]InstrumentProfileProfile, error) {
	profiles := defaultStrategyProfiles(defaultMinRR)
	indexByName := make(map[string]int, len(profiles))
	for i, profile := range profiles {
		indexByName[profile.Name] = i
	}

	for _, override := range overrides {
		if override.Name == "" {
			return nil, fmt.Errorf("strategy_risk.profiles.name不能为空")
		}
		base := InstrumentProfileProfile{
			Name:                override.Name,
			MatchType:           "custom",
			AllowLong:           true,
			AllowShort:          true,
			MinStopPct:          defaultMinStopFloorPct,
			FallbackStopPct:     0.02,
			ATRMultiplier:       2.0,
			ATRTimeframe:        defaultStrategyADXTF,
			MinNetRR:            defaultMinRR,
			MaxRiskPct:          0.005,
			RegimeRiskCapPct:    0.005,
			MinADX:              25,
			MaxSameSideHighCorr: 1,
			MaxSameSideLossPct:  0.02,
			MinOrderValueUSDT:   10,
			ExchangeFullTPMode:  StrategyRiskTPModeAlgorithmicFull,
			ExchangeFullTPMinRR: defaultMinRR,
		}
		if idx, ok := indexByName[override.Name]; ok {
			base = profiles[idx]
		}
		merged, err := applyInstrumentProfileOverride(base, override, defaultMinRR)
		if err != nil {
			return nil, err
		}
		if idx, ok := indexByName[merged.Name]; ok {
			profiles[idx] = merged
		} else {
			indexByName[merged.Name] = len(profiles)
			profiles = append(profiles, merged)
		}
	}
	return profiles, nil
}

func applyInstrumentProfileOverride(base InstrumentProfileProfile, override InstrumentProfileConfig, defaultMinRR float64) (InstrumentProfileProfile, error) {
	base.Name = override.Name
	if len(override.Symbols) > 0 {
		base.Symbols = append([]string(nil), override.Symbols...)
	}
	if override.MatchQuote != "" {
		base.MatchQuote = override.MatchQuote
	}
	if override.MatchType != "" {
		base.MatchType = override.MatchType
	}
	var err error
	if override.MinStopPct > 0 {
		base.MinStopPct, err = normalizePercentRatio(override.MinStopPct, base.MinStopPct, "strategy_risk.profiles.min_stop_pct")
		if err != nil {
			return base, err
		}
		if base.MinStopPct < defaultMinStopFloorPct {
			return base, fmt.Errorf("strategy_risk.profiles[%s].min_stop_pct不能低于1%%", base.Name)
		}
	}
	if override.FallbackStopPct > 0 {
		base.FallbackStopPct, err = normalizePercentRatio(override.FallbackStopPct, base.FallbackStopPct, "strategy_risk.profiles.fallback_stop_pct")
		if err != nil {
			return base, err
		}
	}
	if base.FallbackStopPct < base.MinStopPct {
		base.FallbackStopPct = base.MinStopPct
	}
	if override.ATRMultiplier > 0 {
		base.ATRMultiplier = override.ATRMultiplier
	}
	if base.ATRMultiplier <= 0 {
		return base, fmt.Errorf("strategy_risk.profiles[%s].atr_multiplier必须大于0", base.Name)
	}
	if override.ATRTimeframe != "" {
		if !isSupportedStrategyTimeframe(override.ATRTimeframe) {
			return base, fmt.Errorf("strategy_risk.profiles[%s].atr_timeframe必须是 15m、1h 或 4h", base.Name)
		}
		base.ATRTimeframe = override.ATRTimeframe
	}
	if override.MinNetRR > 0 {
		base.MinNetRR = override.MinNetRR
	}
	if base.MinNetRR <= 0 {
		base.MinNetRR = defaultMinRR
	}
	if base.MinNetRR < 1 {
		return base, fmt.Errorf("strategy_risk.profiles[%s].min_net_rr不能低于1", base.Name)
	}
	if override.MaxRiskPct > 0 {
		base.MaxRiskPct, err = normalizePercentRatio(override.MaxRiskPct, base.MaxRiskPct, "strategy_risk.profiles.max_risk_pct")
		if err != nil {
			return base, err
		}
	}
	if override.RegimeRiskCapPct > 0 {
		base.RegimeRiskCapPct, err = normalizePercentRatio(override.RegimeRiskCapPct, base.RegimeRiskCapPct, "strategy_risk.profiles.regime_risk_cap_pct")
		if err != nil {
			return base, err
		}
	}
	if override.MinADX > 0 {
		base.MinADX = override.MinADX
	}
	if override.AllowLong != nil {
		base.AllowLong = *override.AllowLong
	}
	if override.AllowShort != nil {
		base.AllowShort = *override.AllowShort
	}
	if override.MaxSameSideHighCorr > 0 {
		base.MaxSameSideHighCorr = override.MaxSameSideHighCorr
	}
	if override.MaxSameSideLossPct > 0 {
		base.MaxSameSideLossPct, err = normalizePercentRatio(override.MaxSameSideLossPct, base.MaxSameSideLossPct, "strategy_risk.profiles.max_same_side_loss_pct")
		if err != nil {
			return base, err
		}
	}
	if override.MinOrderValueUSDT > 0 {
		base.MinOrderValueUSDT = override.MinOrderValueUSDT
	}
	if override.ExchangeFullTPMode != "" {
		if !isSupportedExchangeFullTPMode(override.ExchangeFullTPMode) {
			return base, fmt.Errorf("strategy_risk.profiles[%s].exchange_full_tp_mode无效: %q", base.Name, override.ExchangeFullTPMode)
		}
		base.ExchangeFullTPMode = override.ExchangeFullTPMode
	}
	if override.ExchangeFullTPMinRR > 0 {
		base.ExchangeFullTPMinRR = override.ExchangeFullTPMinRR
	}
	if base.ExchangeFullTPMinRR < base.MinNetRR {
		base.ExchangeFullTPMinRR = base.MinNetRR
	}
	if base.MinOrderValueUSDT <= 0 {
		base.MinOrderValueUSDT = 10
	}
	return base, nil
}

func normalizeStrategySafeMode(cfg StrategySafeModeConfig) (StrategySafeModeProfile, error) {
	safe := defaultStrategySafeMode()
	var err error
	if cfg.MaxRiskPct > 0 {
		safe.MaxRiskPct, err = normalizePercentRatio(cfg.MaxRiskPct, safe.MaxRiskPct, "strategy_risk.safe_mode.max_risk_pct")
		if err != nil {
			return safe, err
		}
	}
	if cfg.MaxPositions > 0 {
		safe.MaxPositions = cfg.MaxPositions
	}
	if cfg.DailyOpenLimit > 0 {
		safe.DailyOpenLimit = cfg.DailyOpenLimit
	}
	if cfg.RequireHours > 0 {
		safe.RequireHours = cfg.RequireHours
	}
	if cfg.MinProfitFactor > 0 {
		safe.MinProfitFactor = cfg.MinProfitFactor
	}
	return safe, nil
}

func defaultStrategyProfiles(defaultMinRR float64) []InstrumentProfileProfile {
	return []InstrumentProfileProfile{
		{
			Name: "btc_eth", Symbols: []string{"BTCUSDT", "ETHUSDT"}, MatchType: "btc_eth",
			MinStopPct: defaultMinStopFloorPct, FallbackStopPct: 0.015, ATRMultiplier: 1.5, ATRTimeframe: "1h",
			MinNetRR: defaultMinRR, MaxRiskPct: 0.005, RegimeRiskCapPct: 0.005, MinADX: 20,
			AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 2, MaxSameSideLossPct: 0.02, MinOrderValueUSDT: 10,
			ExchangeFullTPMode: StrategyRiskTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultMinRR,
		},
		{
			Name: "major_alt", Symbols: []string{"SOLUSDT", "BNBUSDT", "BCHUSDT", "XRPUSDT", "LTCUSDT", "ADAUSDT"}, MatchType: "major_alt",
			MinStopPct: 0.015, FallbackStopPct: 0.02, ATRMultiplier: 2.0, ATRTimeframe: "1h",
			MinNetRR: defaultMinRR, MaxRiskPct: 0.005, RegimeRiskCapPct: 0.005, MinADX: 22,
			AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 2, MaxSameSideLossPct: 0.02, MinOrderValueUSDT: 10,
			ExchangeFullTPMode: StrategyRiskTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultMinRR,
		},
		{
			Name: "high_beta_alt", Symbols: []string{"DOGEUSDT", "HYPEUSDT", "ZECUSDT", "ASTERUSDT"}, MatchType: "high_beta_alt",
			MinStopPct: 0.02, FallbackStopPct: 0.025, ATRMultiplier: 2.5, ATRTimeframe: "1h",
			MinNetRR: defaultMinRR, MaxRiskPct: 0.005, RegimeRiskCapPct: 0.005, MinADX: 25,
			AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 1, MaxSameSideLossPct: 0.02, MinOrderValueUSDT: 10,
			ExchangeFullTPMode: StrategyRiskTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultMinRR,
		},
		{
			Name: "non_crypto", Symbols: []string{"XAG", "XAGUSD", "XAGUSDT"}, MatchType: "non_crypto",
			MinStopPct: 0.015, FallbackStopPct: 0.02, ATRMultiplier: 2.0, ATRTimeframe: "1h",
			MinNetRR: defaultMinRR, MaxRiskPct: 0.0025, RegimeRiskCapPct: 0.0025, MinADX: 25,
			AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 1, MaxSameSideLossPct: 0.015, MinOrderValueUSDT: 10,
			ExchangeFullTPMode: StrategyRiskTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultMinRR,
		},
		{
			Name: "default", MatchType: "default",
			MinStopPct: 0.02, FallbackStopPct: 0.025, ATRMultiplier: 2.5, ATRTimeframe: "1h",
			MinNetRR: defaultMinRR, MaxRiskPct: 0.005, RegimeRiskCapPct: 0.005, MinADX: 25,
			AllowLong: true, AllowShort: true, MaxSameSideHighCorr: 1, MaxSameSideLossPct: 0.02, MinOrderValueUSDT: 10,
			ExchangeFullTPMode: StrategyRiskTPModeAlgorithmicFull, ExchangeFullTPMinRR: defaultMinRR,
		},
	}
}

func defaultStrategySafeMode() StrategySafeModeProfile {
	return StrategySafeModeProfile{
		MaxRiskPct:      0.005,
		MaxPositions:    1,
		DailyOpenLimit:  1,
		RequireHours:    24,
		MinProfitFactor: 1.0,
	}
}

func normalizePercentRatio(value, fallback float64, field string) (float64, error) {
	if value == 0 {
		return fallback, nil
	}
	if value < 0 {
		return 0, fmt.Errorf("%s不能为负数: %.4f", field, value)
	}
	if value >= 0.1 {
		value = value / 100
	}
	return value, nil
}

func isSupportedStrategyTimeframe(value string) bool {
	return value == "15m" || value == "1h" || value == "4h"
}

func isSupportedExchangeFullTPMode(value string) bool {
	switch value {
	case StrategyRiskTPModeAlgorithmicFull, StrategyRiskTPModeLegacyAI, StrategyRiskTPModeFinalRTarget:
		return true
	default:
		return false
	}
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	if len(c.Traders) == 0 {
		return fmt.Errorf("至少需要配置一个trader")
	}

	traderIDs := make(map[string]bool)
	for i := range c.Traders {
		trader := &c.Traders[i]
		if trader.ID == "" {
			return fmt.Errorf("trader[%d]: ID不能为空", i)
		}
		if traderIDs[trader.ID] {
			return fmt.Errorf("trader[%d]: ID '%s' 重复", i, trader.ID)
		}
		traderIDs[trader.ID] = true

		if trader.Name == "" {
			return fmt.Errorf("trader[%d]: Name不能为空", i)
		}
		mode, err := normalizeDecisionMode(trader.DecisionMode)
		if err != nil {
			return fmt.Errorf("trader[%d]: %w", i, err)
		}
		trader.DecisionMode = mode
		if mode == DecisionModeProgrammatic {
			if trader.AIModel != "" && trader.AIModel != "qwen" && trader.AIModel != "deepseek" && trader.AIModel != "custom" {
				return fmt.Errorf("trader[%d]: programmatic模式下ai_model如配置必须是 'qwen', 'deepseek' 或 'custom'", i)
			}
		} else if mode == DecisionModeChanlunV2 {
			if trader.AIModel != "" && trader.AIModel != "qwen" && trader.AIModel != "deepseek" && trader.AIModel != "custom" {
				return fmt.Errorf("trader[%d]: chanlun_v2模式下ai_model如配置必须是 'qwen', 'deepseek' 或 'custom'", i)
			}
			trader.ChanlunV2Strategy = NormalizeChanlunV2StrategyConfig(trader.ChanlunV2Strategy)
		} else if trader.AIModel != "qwen" && trader.AIModel != "deepseek" && trader.AIModel != "custom" {
			return fmt.Errorf("trader[%d]: ai_model必须是 'qwen', 'deepseek' 或 'custom'", i)
		}

		// 验证交易平台配置
		if trader.Exchange == "" {
			trader.Exchange = "binance" // 默认使用币安
		}
		if trader.Exchange != "binance" && trader.Exchange != "hyperliquid" && trader.Exchange != "aster" {
			return fmt.Errorf("trader[%d]: exchange必须是 'binance', 'hyperliquid' 或 'aster'", i)
		}

		// 根据平台验证对应的密钥
		if trader.Exchange == "binance" {
			if trader.BinanceAPIKey == "" || trader.BinanceSecretKey == "" {
				return fmt.Errorf("trader[%d]: 使用币安时必须配置binance_api_key和binance_secret_key", i)
			}
		} else if trader.Exchange == "hyperliquid" {
			if trader.HyperliquidPrivateKey == "" {
				return fmt.Errorf("trader[%d]: 使用Hyperliquid时必须配置hyperliquid_private_key", i)
			}
		} else if trader.Exchange == "aster" {
			if trader.AsterUser == "" || trader.AsterSigner == "" || trader.AsterPrivateKey == "" {
				return fmt.Errorf("trader[%d]: 使用Aster时必须配置aster_user, aster_signer和aster_private_key", i)
			}
		}

		if mode == DecisionModeAI {
			if trader.AIModel == "qwen" && trader.QwenKey == "" {
				return fmt.Errorf("trader[%d]: 使用Qwen时必须配置qwen_key", i)
			}
			if trader.AIModel == "deepseek" && trader.DeepSeekKey == "" {
				return fmt.Errorf("trader[%d]: 使用DeepSeek时必须配置deepseek_key", i)
			}
			if trader.AIModel == "custom" {
				if trader.CustomAPIURL == "" {
					return fmt.Errorf("trader[%d]: 使用自定义API时必须配置custom_api_url", i)
				}
				if trader.CustomAPIKey == "" {
					return fmt.Errorf("trader[%d]: 使用自定义API时必须配置custom_api_key", i)
				}
				if trader.CustomModelName == "" {
					return fmt.Errorf("trader[%d]: 使用自定义API时必须配置custom_model_name", i)
				}
			}
		}
		if trader.InitialBalance <= 0 {
			return fmt.Errorf("trader[%d]: initial_balance必须大于0", i)
		}
		if trader.CapitalAllocation.Enabled && trader.CapitalAllocation.AllocatedBalance <= 0 {
			return fmt.Errorf("trader[%d]: capital_allocation.allocated_balance必须大于0", i)
		}
		if trader.ScanIntervalMinutes <= 0 {
			trader.ScanIntervalMinutes = 3 // 默认3分钟
		}
	}

	if c.APIServerPort <= 0 {
		c.APIServerPort = 8080 // 默认8080端口
	}

	// 设置杠杆默认值（适配币安子账户限制，最大5倍）
	if c.Leverage.BTCETHLeverage <= 0 {
		c.Leverage.BTCETHLeverage = 5 // 默认5倍（安全值，适配子账户）
	}
	if c.Leverage.BTCETHLeverage > 5 {
		fmt.Printf("⚠️  警告: BTC/ETH杠杆设置为%dx，如果使用子账户可能会失败（子账户限制≤5x）\n", c.Leverage.BTCETHLeverage)
	}
	if c.Leverage.AltcoinLeverage <= 0 {
		c.Leverage.AltcoinLeverage = 5 // 默认5倍（安全值，适配子账户）
	}
	if c.Leverage.AltcoinLeverage > 5 {
		fmt.Printf("⚠️  警告: 山寨币杠杆设置为%dx，如果使用子账户可能会失败（子账户限制≤5x）\n", c.Leverage.AltcoinLeverage)
	}

	c.DynamicCandidatePool.ApplyDefaults()

	if _, err := c.NormalizeTradingFrequency(); err != nil {
		return err
	}
	if _, err := c.NormalizeStrategyRisk(); err != nil {
		return err
	}
	if _, err := c.NormalizeProgrammaticStrategies(); err != nil {
		return err
	}

	return nil
}

// GetScanInterval 获取扫描间隔
func (tc *TraderConfig) GetScanInterval() time.Duration {
	return time.Duration(tc.ScanIntervalMinutes) * time.Minute
}
