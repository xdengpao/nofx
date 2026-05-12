package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TraderConfig 单个trader的配置
type TraderConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`  // 是否启用该trader
	AIModel string `json:"ai_model"` // "qwen" or "deepseek"

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

	InitialBalance      float64 `json:"initial_balance"`
	ScanIntervalMinutes int     `json:"scan_interval_minutes"`
}

// LeverageConfig 杠杆配置
type LeverageConfig struct {
	BTCETHLeverage  int `json:"btc_eth_leverage"` // BTC和ETH的杠杆倍数（主账户建议5-50，子账户≤5）
	AltcoinLeverage int `json:"altcoin_leverage"` // 山寨币的杠杆倍数（主账户建议5-20，子账户≤5）
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
	HighADX     bool `json:"high_adx,omitempty"`
	RRThreshold bool `json:"rr_threshold,omitempty"`
	RollingGate bool `json:"rolling_gate,omitempty"`
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
}

// TradingFrequencyProfile 是配置归一化后的运行时策略。
type TradingFrequencyProfile struct {
	Legacy                  bool
	Mode                    string
	EffectiveMode           string
	AnalysisIntervalMinutes int
	PromptCandidateLimit    int
	DailyOpenLimit          int
	RollbackWindowHours     int
	RollbackMinProfitFactor float64
	RollbackMaxDrawdownPct  float64
	HighADXReportOnly       bool
	RRReportOnly            bool
	RollingGateReportOnly   bool
}

const (
	TradingFrequencyModeLegacy   = "legacy"
	TradingFrequencyModeSafe     = "safe"
	TradingFrequencyModeBalanced = "balanced"
	TradingFrequencyModeActive   = "active"

	minAnalysisIntervalMinutes = 9
	minPromptCandidateLimit    = 8
	defaultRollbackWindowHours = 24
	defaultRollbackMinPF       = 0.8
	defaultRollbackDrawdownPct = 2.0
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

	return profile, nil
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
		if trader.AIModel != "qwen" && trader.AIModel != "deepseek" && trader.AIModel != "custom" {
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
		if trader.InitialBalance <= 0 {
			return fmt.Errorf("trader[%d]: initial_balance必须大于0", i)
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

	return nil
}

// GetScanInterval 获取扫描间隔
func (tc *TraderConfig) GetScanInterval() time.Duration {
	return time.Duration(tc.ScanIntervalMinutes) * time.Minute
}
