package backtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"nofx/config"
)

func TestBacktestConfigNormalizeValid(t *testing.T) {
	cfg := &BacktestConfig{
		BacktestFrom:  "2026-01-01",
		BacktestTo:    "2026-01-10",
		Symbols:       []string{"ethusdt", "BTCUSDT", "ETHUSDT"},
		InitialEquity: 1000,
		Strategy:      StrategyConfig{ProgrammaticStrategy: minimalStrategyConfig()},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("合法配置不应失败: %v", err)
	}
	if cfg.Timezone != DefaultTimezone {
		t.Fatalf("timezone默认值错误: %s", cfg.Timezone)
	}
	if got := strings.Join(cfg.Symbols, ","); got != "BTCUSDT,ETHUSDT" {
		t.Fatalf("symbols未归一化去重: %s", got)
	}
	if !cfg.WarmupFromTime().Before(cfg.BacktestFromTime()) {
		t.Fatalf("warmup_from应早于backtest_from")
	}
	if cfg.ProgrammaticPolicy().Timeframes.Trade != "1h" {
		t.Fatalf("应使用程序化策略默认trade级别: %s", cfg.ProgrammaticPolicy().Timeframes.Trade)
	}
	if cfg.ConfigHash() == "" {
		t.Fatal("应生成config hash")
	}
}

func TestBacktestConfigRejectsInvalidRange(t *testing.T) {
	cfg := &BacktestConfig{
		BacktestFrom: "2026-01-10",
		BacktestTo:   "2026-01-01",
		Symbols:      []string{"BTCUSDT"},
		Strategy:     StrategyConfig{ProgrammaticStrategy: minimalStrategyConfig()},
	}
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("非法时间段应失败")
	}
}

func TestBacktestConfigRejectsInvalidTimezone(t *testing.T) {
	cfg := &BacktestConfig{
		BacktestFrom: "2026-01-01",
		BacktestTo:   "2026-01-02",
		Timezone:     "Mars/Base",
		Symbols:      []string{"BTCUSDT"},
		Strategy:     StrategyConfig{ProgrammaticStrategy: minimalStrategyConfig()},
	}
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("非法timezone应失败")
	}
}

func TestBacktestConfigValidatesHistoryCoverage(t *testing.T) {
	cfg := &BacktestConfig{
		BacktestFrom: "2026-01-01",
		BacktestTo:   "2026-01-02",
		Symbols:      []string{"BTCUSDT"},
		Strategy:     StrategyConfig{ProgrammaticStrategy: minimalStrategyConfig()},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("配置归一化失败: %v", err)
	}
	checker := fakeCoverageChecker{missing: "BTCUSDT|1h"}
	err := cfg.ValidateHistoryCoverage(context.Background(), checker)
	if err == nil || !strings.Contains(err.Error(), "BTCUSDT 1h") {
		t.Fatalf("应报告缺失历史覆盖，got %v", err)
	}
}

func TestParseConfigTimeDateUsesLocation(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Singapore")
	got, err := ParseConfigTime("2026-01-01", loc)
	if err != nil {
		t.Fatalf("解析日期失败: %v", err)
	}
	if got.Location() != loc || got.Hour() != 0 {
		t.Fatalf("日期应按配置timezone解析: %s", got)
	}
}

func minimalStrategyConfig() config.ProgrammaticStrategyConfig {
	return config.ProgrammaticStrategyConfig{}
}

type fakeCoverageChecker struct {
	missing string
}

func (f fakeCoverageChecker) HasKlineCoverage(_ context.Context, _ string, symbol string, timeframe string, _ time.Time, _ time.Time) (bool, string, error) {
	if f.missing == symbol+"|"+timeframe {
		return false, "fixture missing", nil
	}
	return true, "", nil
}
