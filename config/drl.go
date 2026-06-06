package config

import (
	"fmt"
	"strings"
)

const (
	defaultDRLObservationWindow = 60
	defaultDRLTimeframe         = "4h"
	defaultDRLActionThreshold   = 0.1
	defaultDRLMaxPositionPct    = 0.3
	defaultDRLLeverage          = 5
	defaultDRLStopLossATRMult   = 2.0
	defaultDRLTakeProfitATRMult = 3.0
	defaultDRLMaxDrawdownPct    = 20.0
	defaultDRLRetrainIntervalH  = 168
	defaultDRLValidationMinDA   = 0.55
	defaultDRLMonteCarloPaths   = 2000
	defaultDRLStressTestDelta   = 0.3
	defaultDRLStablecoinRatio   = 0.3
)

// DRLStrategyConfig 深度强化学习策略配置。
type DRLStrategyConfig struct {
	ModelPath             string           `json:"model_path"`                       // ONNX模型文件路径
	ModelVersion          string           `json:"model_version,omitempty"`          // 模型版本标识
	ObservationWindow     int              `json:"observation_window,omitempty"`     // 观测窗口长度，默认60
	Timeframe             string           `json:"timeframe,omitempty"`              // 主时间框架，默认4h
	Symbols               []string         `json:"symbols,omitempty"`                // DRL关注标的
	Features              DRLFeatureConfig `json:"features,omitempty"`               // 特征工程配置
	ActionThreshold       float64          `json:"action_threshold,omitempty"`       // 动作阈值，默认0.1
	MaxPositionPct        float64          `json:"max_position_pct,omitempty"`       // 最大仓位比例，默认0.3
	DefaultLeverage       int              `json:"default_leverage,omitempty"`       // 默认杠杆，默认5
	StopLossATRMult       float64          `json:"stop_loss_atr_mult,omitempty"`     // ATR止损倍数，默认2.0
	TakeProfitATRMult     float64          `json:"take_profit_atr_mult,omitempty"`   // ATR止盈倍数，默认3.0
	MaxDrawdownPct        float64          `json:"max_drawdown_pct,omitempty"`       // 最大回撤阈值，默认20
	AutoRetrain           bool             `json:"auto_retrain,omitempty"`           // 是否自动重训练
	RetrainIntervalH      int              `json:"retrain_interval_hours,omitempty"` // 重训练间隔小时，默认168
	ValidationMinDA       float64          `json:"validation_min_da,omitempty"`      // 最低方向准确性，默认0.55
	MonteCarloEnabled     bool             `json:"monte_carlo_enabled,omitempty"`    // 是否启用蒙特卡洛
	MonteCarloPaths       int              `json:"monte_carlo_paths,omitempty"`      // 蒙特卡洛路径数，默认2000
	StressTestEnabled     bool             `json:"stress_test_enabled,omitempty"`    // 是否启用压力测试
	StressTestDelta       float64          `json:"stress_test_delta,omitempty"`      // 价格冲击幅度，默认0.3
	StablecoinHedge       bool             `json:"stablecoin_hedge,omitempty"`       // 是否启用稳定币避险模拟
	StablecoinRatio       float64          `json:"stablecoin_ratio,omitempty"`       // 稳定币比例，默认0.3
	PredictionEnhancement bool             `json:"prediction_enhancement,omitempty"` // 外部预测增强预留开关
}

// DRLFeatureConfig 控制DRL观测向量中的技术指标特征。
type DRLFeatureConfig struct {
	IncludeMACD      *bool   `json:"include_macd,omitempty"`      // 默认true
	IncludeEMA       *bool   `json:"include_ema,omitempty"`       // 默认true
	IncludeRSI       *bool   `json:"include_rsi,omitempty"`       // 默认true
	IncludeATR       *bool   `json:"include_atr,omitempty"`       // 默认true
	IncludeCCI       *bool   `json:"include_cci,omitempty"`       // 默认true
	IncludeBollinger *bool   `json:"include_bollinger,omitempty"` // 默认true
	EMAShortPeriod   int     `json:"ema_short_period,omitempty"`  // 默认12
	EMALongPeriod    int     `json:"ema_long_period,omitempty"`   // 默认26
	RSIPeriod        int     `json:"rsi_period,omitempty"`        // 默认14
	ATRPeriod        int     `json:"atr_period,omitempty"`        // 默认14
	CCIPeriod        int     `json:"cci_period,omitempty"`        // 默认20
	BollingerPeriod  int     `json:"bollinger_period,omitempty"`  // 默认20
	BollingerStdDev  float64 `json:"bollinger_std_dev,omitempty"` // 默认2.0
}

// NormalizeDRLStrategy 归一化并校验DRL策略配置。
func NormalizeDRLStrategy(cfg DRLStrategyConfig) (DRLStrategyConfig, error) {
	cfg.ModelPath = strings.TrimSpace(cfg.ModelPath)
	cfg.ModelVersion = strings.TrimSpace(cfg.ModelVersion)
	cfg.Timeframe = strings.ToLower(strings.TrimSpace(cfg.Timeframe))
	if cfg.Timeframe == "" {
		cfg.Timeframe = defaultDRLTimeframe
	}
	if cfg.ObservationWindow <= 0 {
		cfg.ObservationWindow = defaultDRLObservationWindow
	}
	if cfg.ActionThreshold <= 0 {
		cfg.ActionThreshold = defaultDRLActionThreshold
	}
	if cfg.MaxPositionPct <= 0 {
		cfg.MaxPositionPct = defaultDRLMaxPositionPct
	}
	if cfg.DefaultLeverage <= 0 {
		cfg.DefaultLeverage = defaultDRLLeverage
	}
	if cfg.StopLossATRMult <= 0 {
		cfg.StopLossATRMult = defaultDRLStopLossATRMult
	}
	if cfg.TakeProfitATRMult <= 0 {
		cfg.TakeProfitATRMult = defaultDRLTakeProfitATRMult
	}
	if cfg.MaxDrawdownPct <= 0 {
		cfg.MaxDrawdownPct = defaultDRLMaxDrawdownPct
	}
	if cfg.RetrainIntervalH <= 0 {
		cfg.RetrainIntervalH = defaultDRLRetrainIntervalH
	}
	if cfg.ValidationMinDA <= 0 {
		cfg.ValidationMinDA = defaultDRLValidationMinDA
	}
	if cfg.MonteCarloPaths <= 0 {
		cfg.MonteCarloPaths = defaultDRLMonteCarloPaths
	}
	if cfg.StressTestDelta <= 0 {
		cfg.StressTestDelta = defaultDRLStressTestDelta
	}
	if cfg.StablecoinRatio <= 0 {
		cfg.StablecoinRatio = defaultDRLStablecoinRatio
	}
	cfg.Features = normalizeDRLFeatures(cfg.Features)
	symbols, err := normalizeDRLSymbols(cfg.Symbols)
	if err != nil {
		return cfg, err
	}
	cfg.Symbols = symbols

	if cfg.ModelPath == "" {
		return cfg, fmt.Errorf("model_path不能为空")
	}
	if cfg.ObservationWindow < 10 || cfg.ObservationWindow > 200 {
		return cfg, fmt.Errorf("observation_window必须在[10,200]范围内")
	}
	if cfg.ActionThreshold <= 0 || cfg.ActionThreshold >= 1 {
		return cfg, fmt.Errorf("action_threshold必须在(0,1)范围内")
	}
	if cfg.MaxPositionPct <= 0 || cfg.MaxPositionPct > 1 {
		return cfg, fmt.Errorf("max_position_pct必须在(0,1]范围内")
	}
	if !isDRLTimeframeSupported(cfg.Timeframe) {
		return cfg, fmt.Errorf("timeframe必须是3m、15m、1h或4h: %s", cfg.Timeframe)
	}
	if cfg.DefaultLeverage <= 0 {
		return cfg, fmt.Errorf("default_leverage必须大于0")
	}
	if cfg.StopLossATRMult <= 0 || cfg.TakeProfitATRMult <= 0 {
		return cfg, fmt.Errorf("stop_loss_atr_mult和take_profit_atr_mult必须大于0")
	}
	if cfg.ValidationMinDA <= 0 || cfg.ValidationMinDA > 1 {
		return cfg, fmt.Errorf("validation_min_da必须在(0,1]范围内")
	}
	if cfg.StressTestDelta <= 0 || cfg.StressTestDelta > 1 {
		return cfg, fmt.Errorf("stress_test_delta必须在(0,1]范围内")
	}
	if cfg.StablecoinRatio <= 0 || cfg.StablecoinRatio > 1 {
		return cfg, fmt.Errorf("stablecoin_ratio必须在(0,1]范围内")
	}
	return cfg, nil
}

func normalizeDRLFeatures(cfg DRLFeatureConfig) DRLFeatureConfig {
	ensureBool := func(value **bool) {
		if *value == nil {
			v := true
			*value = &v
		}
	}
	ensureBool(&cfg.IncludeMACD)
	ensureBool(&cfg.IncludeEMA)
	ensureBool(&cfg.IncludeRSI)
	ensureBool(&cfg.IncludeATR)
	ensureBool(&cfg.IncludeCCI)
	ensureBool(&cfg.IncludeBollinger)
	if cfg.EMAShortPeriod <= 0 {
		cfg.EMAShortPeriod = 12
	}
	if cfg.EMALongPeriod <= 0 {
		cfg.EMALongPeriod = 26
	}
	if cfg.RSIPeriod <= 0 {
		cfg.RSIPeriod = 14
	}
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.CCIPeriod <= 0 {
		cfg.CCIPeriod = 20
	}
	if cfg.BollingerPeriod <= 0 {
		cfg.BollingerPeriod = 20
	}
	if cfg.BollingerStdDev <= 0 {
		cfg.BollingerStdDev = 2.0
	}
	return cfg
}

func normalizeDRLSymbols(symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if !programmaticSymbolPattern.MatchString(symbol) {
			return nil, fmt.Errorf("symbols包含非法交易对: %s", symbol)
		}
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		normalized = append(normalized, symbol)
	}
	return normalized, nil
}

func isDRLTimeframeSupported(timeframe string) bool {
	switch timeframe {
	case "3m", "15m", "1h", "4h":
		return true
	default:
		return false
	}
}
