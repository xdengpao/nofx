package optimize

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	DefaultMaxDrawdownRelativeTolerance       = 1.10
	DefaultProfitFactorRelativeFloor          = 0.95
	DefaultRejectionRateAbsoluteTolerance     = 0.10
	DefaultMinNotionalRejectionTolerance      = 0.05
	DefaultReplayBacktestConsistencyTolerance = 0.05
	DefaultCircuitBreakerFrequencyTolerance   = 0.05
	DefaultBootstrapIterations                = 1000
)

type OptimizationConfig struct {
	MaxDrawdownRelativeTolerance       float64 `json:"max_drawdown_relative_tolerance"`
	ProfitFactorRelativeFloor          float64 `json:"profit_factor_relative_floor"`
	RejectionRateAbsoluteTolerance     float64 `json:"rejection_rate_absolute_tolerance"`
	MinNotionalRejectionTolerance      float64 `json:"min_notional_rejection_tolerance"`
	ReplayBacktestConsistencyTolerance float64 `json:"replay_backtest_consistency_tolerance"`
	CircuitBreakerFrequencyTolerance   float64 `json:"circuit_breaker_frequency_tolerance"`
	BootstrapIterations                int     `json:"bootstrap_iterations"`
	BootstrapSeed                      int64   `json:"bootstrap_seed,omitempty"`

	ReplayFrom   string `json:"replay_from"`
	ReplayTo     string `json:"replay_to"`
	BacktestFrom string `json:"backtest_from"`
	BacktestTo   string `json:"backtest_to"`
	WarmupFrom   string `json:"warmup_from"`
	Timezone     string `json:"timezone"`

	PolicyRef      string `json:"policy_ref,omitempty"`
	PolicyCommit   string `json:"policy_commit,omitempty"`
	OverrideBy     string `json:"override_by,omitempty"`
	OverrideReason string `json:"override_reason,omitempty"`
}

type CommittedGatePolicy struct {
	PolicyRef    string             `json:"policy_ref"`
	PolicyCommit string             `json:"policy_commit"`
	ApprovedBy   string             `json:"approved_by,omitempty"`
	ApprovedAt   string             `json:"approved_at,omitempty"`
	Thresholds   OptimizationConfig `json:"thresholds"`
}

func LoadOptimizationConfig(path string) (*OptimizationConfig, error) {
	cfg := DefaultOptimizationConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取优化配置失败: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析优化配置失败: %w", err)
	}
	ApplyOptimizationDefaults(cfg)
	if err := ValidateOptimizationConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultOptimizationConfig() *OptimizationConfig {
	cfg := &OptimizationConfig{}
	ApplyOptimizationDefaults(cfg)
	return cfg
}

func ApplyOptimizationDefaults(cfg *OptimizationConfig) {
	if cfg.MaxDrawdownRelativeTolerance <= 0 {
		cfg.MaxDrawdownRelativeTolerance = DefaultMaxDrawdownRelativeTolerance
	}
	if cfg.ProfitFactorRelativeFloor <= 0 {
		cfg.ProfitFactorRelativeFloor = DefaultProfitFactorRelativeFloor
	}
	if cfg.RejectionRateAbsoluteTolerance <= 0 {
		cfg.RejectionRateAbsoluteTolerance = DefaultRejectionRateAbsoluteTolerance
	}
	if cfg.MinNotionalRejectionTolerance <= 0 {
		cfg.MinNotionalRejectionTolerance = DefaultMinNotionalRejectionTolerance
	}
	if cfg.ReplayBacktestConsistencyTolerance <= 0 {
		cfg.ReplayBacktestConsistencyTolerance = DefaultReplayBacktestConsistencyTolerance
	}
	if cfg.CircuitBreakerFrequencyTolerance <= 0 {
		cfg.CircuitBreakerFrequencyTolerance = DefaultCircuitBreakerFrequencyTolerance
	}
	if cfg.BootstrapIterations <= 0 {
		cfg.BootstrapIterations = DefaultBootstrapIterations
	}
}

func ValidateOptimizationConfig(cfg *OptimizationConfig) error {
	if cfg == nil {
		return fmt.Errorf("优化配置为空")
	}
	if hasThresholdOverride(cfg) && (strings.TrimSpace(cfg.PolicyRef) == "" || strings.TrimSpace(cfg.PolicyCommit) == "") {
		return ErrUncommittedPolicyOverride
	}
	return nil
}

func hasThresholdOverride(cfg *OptimizationConfig) bool {
	return cfg.MaxDrawdownRelativeTolerance != DefaultMaxDrawdownRelativeTolerance ||
		cfg.ProfitFactorRelativeFloor != DefaultProfitFactorRelativeFloor ||
		cfg.RejectionRateAbsoluteTolerance != DefaultRejectionRateAbsoluteTolerance ||
		cfg.MinNotionalRejectionTolerance != DefaultMinNotionalRejectionTolerance ||
		cfg.ReplayBacktestConsistencyTolerance != DefaultReplayBacktestConsistencyTolerance ||
		cfg.CircuitBreakerFrequencyTolerance != DefaultCircuitBreakerFrequencyTolerance
}
