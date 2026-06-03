package drl

import (
	"fmt"
	"nofx/config"
	"strings"
)

const (
	StrategyMode    = "drl"
	StrategyName    = "drl_ppo"
	featuresPerStep = 16
	accountFeatures = 3
)

// DRLEngineConfig 是DRL运行时配置。
type DRLEngineConfig struct {
	ModelPath         string
	ModelVersion      string
	ObservationWindow int
	Timeframe         string
	Symbols           []string
	Features          config.DRLFeatureConfig
	ActionThreshold   float64
	MaxPositionPct    float64
	DefaultLeverage   int
	StopLossATRMult   float64
	TakeProfitATRMult float64
	MaxDrawdownPct    float64
	AutoRetrain       bool
	RetrainIntervalH  int
	ValidationMinDA   float64
	InputShape        []int64
}

// Observation 表示一次模型推理输入。
type Observation struct {
	Symbol   string
	Values   []float32
	Stats    FeatureStats
	Position bool
}

// InferenceResult 表示一次模型推理和动作映射结果。
type InferenceResult struct {
	Symbol       string
	RawAction    float32
	MappedAction string
	Confidence   int
}

// FeatureStats 记录观测向量构建的诊断信息。
type FeatureStats struct {
	Dimension         int     `json:"dimension"`
	Window            int     `json:"window"`
	FeaturePerStep    int     `json:"feature_per_step"`
	MissingRows       int     `json:"missing_rows"`
	ZeroPadded        bool    `json:"zero_padded"`
	LastClose         float64 `json:"last_close,omitempty"`
	LastATR           float64 `json:"last_atr,omitempty"`
	AvailableKlines   int     `json:"available_klines"`
	AccountFeatureDim int     `json:"account_feature_dim"`
}

func engineConfigFromConfig(cfg config.DRLStrategyConfig) (DRLEngineConfig, error) {
	normalized, err := config.NormalizeDRLStrategy(cfg)
	if err != nil {
		return DRLEngineConfig{}, err
	}
	modelVersion := strings.TrimSpace(normalized.ModelVersion)
	if modelVersion == "" {
		modelVersion = "default"
	}
	out := DRLEngineConfig{
		ModelPath:         normalized.ModelPath,
		ModelVersion:      modelVersion,
		ObservationWindow: normalized.ObservationWindow,
		Timeframe:         normalized.Timeframe,
		Symbols:           append([]string(nil), normalized.Symbols...),
		Features:          normalized.Features,
		ActionThreshold:   normalized.ActionThreshold,
		MaxPositionPct:    normalized.MaxPositionPct,
		DefaultLeverage:   normalized.DefaultLeverage,
		StopLossATRMult:   normalized.StopLossATRMult,
		TakeProfitATRMult: normalized.TakeProfitATRMult,
		MaxDrawdownPct:    normalized.MaxDrawdownPct,
		AutoRetrain:       normalized.AutoRetrain,
		RetrainIntervalH:  normalized.RetrainIntervalH,
		ValidationMinDA:   normalized.ValidationMinDA,
	}
	out.InputShape = []int64{1, int64(out.ObservationDimension())}
	return out, nil
}

func (c DRLEngineConfig) ObservationDimension() int {
	return c.ObservationWindow*featuresPerStep + accountFeatures
}

func (c DRLEngineConfig) MarketHistoryDepth() map[string]int {
	depth := c.ObservationWindow + 80
	if depth < 120 {
		depth = 120
	}
	if depth > 1000 {
		depth = 1000
	}
	return map[string]int{
		"3m":  depth,
		"15m": depth,
		"1h":  depth,
		"4h":  depth,
	}
}

func (c DRLEngineConfig) validateObservation(values []float32) error {
	if len(values) != c.ObservationDimension() {
		return fmt.Errorf("DRL观测维度错误: got=%d want=%d", len(values), c.ObservationDimension())
	}
	return nil
}
