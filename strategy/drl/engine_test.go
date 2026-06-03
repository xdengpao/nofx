package drl

import (
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"os"
	"testing"
	"time"
)

func TestNewEngineRequiresModelFile(t *testing.T) {
	_, err := NewEngine(config.DRLStrategyConfig{ModelPath: "missing/drl-model.onnx"})
	if err == nil {
		t.Fatal("DRL engine启动时应校验模型文件存在")
	}
}

func TestEngineGetFullDecisionUsesPreparationAndStubBackend(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := config.DRLStrategyConfig{
		ModelPath:         modelPath,
		ModelVersion:      "test-v1",
		ObservationWindow: 10,
		Timeframe:         "4h",
		Symbols:           []string{"ETHUSDT"},
		ActionThreshold:   0.1,
		MaxPositionPct:    0.3,
		DefaultLeverage:   5,
		StopLossATRMult:   2,
		TakeProfitATRMult: 3,
	}
	seen := map[string]bool{}
	provider := func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error) {
		if !opts.ClosedKlinesOnly {
			t.Fatalf("DRL行情准备必须使用闭合K线")
		}
		if !opts.AllowRiskReducingOnHalt {
			t.Fatalf("DRL行情准备必须允许风险降低动作")
		}
		if opts.MarketHistoryDepth["4h"] < cfg.ObservationWindow {
			t.Fatalf("DRL行情准备必须按MarketHistoryDepth拉足历史K线: %+v", opts.MarketHistoryDepth)
		}
		seen[market.Normalize(symbol)] = true
		return makeMarketData(t, symbol, 120), nil
	}
	engine, err := NewEngineWithBackend(cfg, NewStubBackend(0), WithMarketDataProvider(provider), WithDisableOITopFetch(true), WithClock(func() time.Time {
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}))
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	defer engine.Close()

	ctx := &decision.Context{
		TraderID:     "drl-test",
		DecisionMode: config.DecisionModeDRL,
		Account: decision.AccountInfo{
			TotalEquity:      1000,
			AvailableBalance: 800,
			SizingEquity:     1000,
		},
	}
	full, err := engine.GetFullDecision(ctx)
	if err != nil {
		t.Fatalf("DRL决策不应失败: %v", err)
	}
	if full.DecisionMode != StrategyMode || full.StrategyName != StrategyName || full.StrategyVersion != "test-v1" {
		t.Fatalf("FullDecision策略标识错误: %+v", full)
	}
	if len(full.Decisions) == 0 || full.Decisions[0].Action != "wait" {
		t.Fatalf("stub输出0应映射为wait: %+v", full.Decisions)
	}
	if !seen["ETHUSDT"] || !seen["BTCUSDT"] {
		t.Fatalf("PrepareCycleContext应获取DRL标的和BTC上下文: %+v", seen)
	}
	status := engine.Status()
	if status.InferenceCount != 1 || status.ModelPath != modelPath || status.ModelVersion != "test-v1" {
		t.Fatalf("DRL状态统计异常: %+v", status)
	}
}

func writeTempModel(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "model_*.onnx")
	if err != nil {
		t.Fatalf("创建模型fixture失败: %v", err)
	}
	if _, err := f.WriteString("stub"); err != nil {
		t.Fatalf("写模型fixture失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭模型fixture失败: %v", err)
	}
	return f.Name()
}

func makeMarketData(t *testing.T, symbol string, n int) *market.Data {
	t.Helper()
	klines := makeTestKlines(n)
	data, err := market.BuildDataFromKlines(symbol, market.KlineBundle{
		M3:  klines,
		M15: klines,
		H1:  klines,
		H4:  klines,
	}, market.BuildDataOptions{EnrichmentMode: "disabled"})
	if err != nil {
		t.Fatalf("构造market data失败: %v", err)
	}
	return data
}
