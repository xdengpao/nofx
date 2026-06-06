package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type BatchRunner struct {
	Config BatchConfig
}

type BatchResult struct {
	Runs   []RunResult       `json:"runs"`
	Errors map[string]string `json:"errors,omitempty"`
}

func (b *BatchRunner) Run(ctx context.Context) (BatchResult, error) {
	result := BatchResult{Errors: map[string]string{}}
	configs, err := b.expandConfigs()
	if err != nil {
		return result, err
	}
	for name, cfg := range configs {
		runner, err := NewRunner(cfg, nil)
		if err != nil {
			result.Errors[name] = err.Error()
			if b.Config.FailFast {
				return result, err
			}
			continue
		}
		run, err := runner.Run(ctx)
		if err != nil {
			result.Errors[name] = err.Error()
			if b.Config.FailFast {
				return result, err
			}
			continue
		}
		result.Runs = append(result.Runs, run)
	}
	return result, nil
}

func LoadBatchConfig(path string) (*BatchConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取批量回测配置失败: %w", err)
	}
	var cfg BatchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析批量回测配置失败: %w", err)
	}
	return &cfg, nil
}

func (b *BatchRunner) expandConfigs() (map[string]*BacktestConfig, error) {
	out := map[string]*BacktestConfig{}
	for _, path := range b.Config.Configs {
		cfg, err := LoadConfig(path)
		if err != nil {
			return nil, err
		}
		out[filepath.Base(path)] = cfg
	}
	if b.Config.BaseConfig != "" {
		base, err := LoadConfig(b.Config.BaseConfig)
		if err != nil {
			return nil, err
		}
		if values, ok := b.Config.Matrix["programmatic_strategy.timeframes.trade"]; ok && len(values) > 0 {
			for _, value := range values {
				trade, _ := value.(string)
				if trade == "" {
					continue
				}
				copied := *base
				copied.Strategy.ProgrammaticStrategy.Timeframes.Trade = trade
				if err := copied.NormalizeAndValidate(); err != nil {
					return nil, err
				}
				out["trade_"+trade] = &copied
			}
		} else if len(out) == 0 {
			out["base"] = base
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("批量回测没有可运行配置")
	}
	return out, nil
}
