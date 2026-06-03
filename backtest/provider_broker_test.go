package backtest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/config"
	"nofx/decision"
	"nofx/historydb"
	"nofx/market"
)

func TestHistoricalMarketDataProviderFiltersFutureKlines(t *testing.T) {
	store := openBacktestStore(t)
	defer store.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeAllTimeframes(t, store, "BTCUSDT", start, 90)
	asOf := start.Add(48 * time.Hour)
	provider := &HistoricalMarketDataProvider{Store: store, Source: DefaultSource, Clock: func() time.Time { return asOf }}
	data, err := provider.GetMarketData("BTCUSDT", decision.CyclePreparationOptions{
		MarketHistoryDepth: map[string]int{"3m": 50, "15m": 30, "1h": 20, "4h": 10},
	})
	if err != nil {
		t.Fatalf("provider构建失败: %v", err)
	}
	for tf, klines := range data.Klines {
		if len(klines) == 0 {
			t.Fatalf("%s 无K线", tf)
		}
		if klines[len(klines)-1].CloseTime > asOf.UnixMilli() {
			t.Fatalf("%s 返回未来K线", tf)
		}
	}
	if data.OIValueUSD != 0 || data.FundingRate != 0 {
		t.Fatalf("回测provider不得填充实时OI/funding")
	}
}

func TestPaperBrokerOpenPartialCloseAndFullClose(t *testing.T) {
	broker := NewPaperBroker(1000, CostConfig{TakerFeeBPS: 5, SlippageBPS: 1}, ExecutionConfig{})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	open := decision.Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 5, PositionSizeUSD: 100, StopLoss: 95, TakeProfit: 120, Reasoning: "test open"}
	broker.SubmitDecision(open, now)
	broker.ProcessBar("BTCUSDT", market.Kline{Open: 100, High: 101, Low: 99, Close: 100, CloseTime: now.UnixMilli()}, now)
	if len(broker.Positions) != 1 {
		t.Fatalf("应开仓")
	}
	partial := decision.Decision{Symbol: "BTCUSDT", Action: "partial_close", ClosePercentage: 50, Reasoning: "test partial"}
	broker.SubmitDecision(partial, now)
	broker.ProcessBar("BTCUSDT", market.Kline{Open: 110, High: 111, Low: 109, Close: 110, CloseTime: now.Add(3 * time.Minute).UnixMilli()}, now.Add(3*time.Minute))
	if len(broker.Positions) != 1 || broker.Positions["BTCUSDT"].Quantity <= 0 {
		t.Fatalf("减仓后应保留剩余持仓")
	}
	closeAll := decision.Decision{Symbol: "BTCUSDT", Action: "close_long", Reasoning: "test close"}
	broker.SubmitDecision(closeAll, now)
	broker.ProcessBar("BTCUSDT", market.Kline{Open: 112, High: 113, Low: 111, Close: 112, CloseTime: now.Add(6 * time.Minute).UnixMilli()}, now.Add(6*time.Minute))
	if len(broker.Positions) != 0 {
		t.Fatalf("应全平")
	}
	if broker.Account.RealizedPnL <= 0 {
		t.Fatalf("应产生正向已实现盈亏")
	}
}

func TestPaperBrokerUsesPreflightForMinNotional(t *testing.T) {
	broker := NewPaperBrokerWithExchange(1000, CostConfig{}, ExecutionConfig{}, "binance")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	open := decision.Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 5, PositionSizeUSD: 20, StopLoss: 95, TakeProfit: 120, Reasoning: "too small"}
	broker.SubmitDecision(open, now)
	broker.ProcessBar("BTCUSDT", market.Kline{Open: 100, High: 101, Low: 99, Close: 100, CloseTime: now.UnixMilli()}, now)
	if len(broker.Positions) != 0 {
		t.Fatalf("低于Binance BTCUSDT最小名义额的开仓应被拒绝")
	}
	if len(broker.OpenRejections) != 1 {
		t.Fatalf("preflight拒绝应进入OpenRejections，got %d", len(broker.OpenRejections))
	}
	if !strings.Contains(broker.OpenRejections[0].Reason, "名义额") {
		t.Fatalf("拒绝原因应包含名义额: %s", broker.OpenRejections[0].Reason)
	}
}

func TestRunnerGeneratesReportFiles(t *testing.T) {
	store := openBacktestStore(t)
	defer store.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeAllTimeframes(t, store, "BTCUSDT", start, 240)
	cfg := &BacktestConfig{
		BacktestFrom:  start.Add(8 * time.Hour).Format(time.RFC3339),
		BacktestTo:    start.Add(10 * time.Hour).Format(time.RFC3339),
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		HistoryDB:     store.Path(),
		Symbols:       []string{"BTCUSDT"},
		InitialEquity: 1000,
		Data:          DataConfig{AllowAutoFetch: true},
		Strategy:      StrategyConfig{ProgrammaticStrategy: minimalStrategyConfigForBacktest()},
	}
	runner, err := NewRunner(cfg, store)
	if err != nil {
		t.Fatalf("runner初始化失败: %v", err)
	}
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("runner执行失败: %v", err)
	}
	for _, file := range []string{"report.json", "trades.csv", "equity.csv", "signals.csv", "rejections.csv", "structures.json", "metrics.json"} {
		if result.OutputDir == "" {
			t.Fatal("缺少输出目录")
		}
		if _, err := os.Stat(filepath.Join(result.OutputDir, file)); err != nil {
			t.Fatalf("缺少输出文件%s: %v", file, err)
		}
	}
	if result.Report.FundingMode != "disabled" || result.Report.LiquidationMode != "not_modelled" {
		t.Fatalf("报告应记录v1假设")
	}
	if result.Report.DataHash == "" || len(result.Report.DataHashes) == 0 {
		t.Fatalf("报告应记录共同data_hash和分项hash")
	}
	if result.Report.Exchange != "binance" || result.Report.TraderID != "backtest" {
		t.Fatalf("报告应记录trader/exchange上下文: %+v", result.Report)
	}
	if _, ok := result.Report.Files["metrics"]; !ok {
		t.Fatalf("报告files应包含metrics: %+v", result.Report.Files)
	}
}

func TestRunnerGeneratesDRLReportMetrics(t *testing.T) {
	store := openBacktestStore(t)
	defer store.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeAllTimeframes(t, store, "BTCUSDT", start, 240)
	modelPath := filepath.Join(t.TempDir(), "ppo.onnx")
	if err := os.WriteFile(modelPath, []byte("stub"), 0o600); err != nil {
		t.Fatalf("写入DRL模型fixture失败: %v", err)
	}
	cfg := &BacktestConfig{
		BacktestFrom:  start.Add(48 * time.Hour).Format(time.RFC3339),
		BacktestTo:    start.Add(49 * time.Hour).Format(time.RFC3339),
		OutputDir:     filepath.Join(t.TempDir(), "runs"),
		HistoryDB:     store.Path(),
		InitialEquity: 1000,
		Data:          DataConfig{AllowAutoFetch: true},
		Strategy: StrategyConfig{
			DecisionMode: config.DecisionModeDRL,
			DRLStrategy: config.DRLStrategyConfig{
				ModelPath:         modelPath,
				ModelVersion:      "test-v1",
				ObservationWindow: 10,
				Timeframe:         "4h",
				Symbols:           []string{"BTCUSDT"},
				MonteCarloEnabled: true,
				MonteCarloPaths:   32,
				StressTestEnabled: true,
				StablecoinHedge:   true,
			},
		},
	}
	runner, err := NewRunner(cfg, store)
	if err != nil {
		t.Fatalf("DRL runner初始化失败: %v", err)
	}
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("DRL runner执行失败: %v", err)
	}
	if result.Report.DRLMetrics == nil {
		t.Fatalf("DRL报告应包含drl_metrics: %+v", result.Report)
	}
	if result.Report.DRLMonteCarlo == nil || result.Report.DRLStressTest == nil || result.Report.DRLHedgeComparison == nil {
		t.Fatalf("DRL报告应包含扩展风险结果: %+v", result.Report)
	}
	strategy, ok := result.Report.ConfigSnapshot["strategy"].(map[string]any)
	if !ok || strategy["decision_mode"] != config.DecisionModeDRL {
		t.Fatalf("DRL报告配置快照异常: %+v", result.Report.ConfigSnapshot)
	}
}

func openBacktestStore(t *testing.T) *historydb.Store {
	t.Helper()
	store, err := historydb.Open(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatalf("打开历史库失败: %v", err)
	}
	return store
}

func writeAllTimeframes(t *testing.T, store *historydb.Store, symbol string, start time.Time, count int) {
	t.Helper()
	for _, item := range []struct {
		tf   string
		step time.Duration
	}{
		{"3m", 3 * time.Minute},
		{"15m", 15 * time.Minute},
		{"1h", time.Hour},
		{"4h", 4 * time.Hour},
	} {
		if _, _, err := store.UpsertKlines(context.Background(), DefaultSource, symbol, item.tf, backtestKlines(start, count, item.step)); err != nil {
			t.Fatalf("写入%s失败: %v", item.tf, err)
		}
	}
}

func backtestKlines(start time.Time, n int, step time.Duration) []market.Kline {
	out := make([]market.Kline, 0, n)
	price := 100.0
	for i := 0; i < n; i++ {
		open := start.Add(time.Duration(i) * step)
		out = append(out, market.Kline{
			OpenTime:  open.UnixMilli(),
			CloseTime: open.Add(step).Add(-time.Millisecond).UnixMilli(),
			Open:      price,
			High:      price + 2,
			Low:       price - 2,
			Close:     price + 0.5,
			Volume:    1000,
		})
		price += 0.1
	}
	return out
}

func minimalStrategyConfigForBacktest() config.ProgrammaticStrategyConfig {
	return config.ProgrammaticStrategyConfig{}
}
