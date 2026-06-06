package pool

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"nofx/market"
)

func withDynamicPoolStubs(t *testing.T, marketFn func(string) (*market.Data, error), volumeFn func(int) ([]volumeTicker, error)) {
	t.Helper()
	oldMarketFn := getMarketDataForDynamicPool
	oldVolumeFn := fetchExchangeVolumeTickers
	oldDynamicConfig := dynamicCandidatePoolConfig
	oldDefaultCoins := append([]string(nil), defaultMainstreamCoins...)
	t.Cleanup(func() {
		getMarketDataForDynamicPool = oldMarketFn
		fetchExchangeVolumeTickers = oldVolumeFn
		dynamicCandidatePoolConfig = oldDynamicConfig
		defaultMainstreamCoins = oldDefaultCoins
	})
	getMarketDataForDynamicPool = marketFn
	fetchExchangeVolumeTickers = volumeFn
}

func dynamicTestMarketData(symbol string) (*market.Data, error) {
	values := map[string]struct {
		oi          float64
		adx         float64
		volumeRatio float64
		bollWidth   float64
	}{
		"BTCUSDT": {oi: 500_000_000, adx: 28, volumeRatio: 1.1, bollWidth: 4.0},
		"ETHUSDT": {oi: 300_000_000, adx: 24, volumeRatio: 1.0, bollWidth: 4.0},
		"SOLUSDT": {oi: 120_000_000, adx: 31, volumeRatio: 1.7, bollWidth: 5.0},
		"BNBUSDT": {oi: 80_000_000, adx: 35, volumeRatio: 1.8, bollWidth: 5.0},
		"LOWUSDT": {oi: 3_000_000, adx: 30, volumeRatio: 1.2, bollWidth: 4.0},
	}
	v, ok := values[symbol]
	if !ok {
		return nil, fmt.Errorf("unexpected symbol %s", symbol)
	}
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   100,
		CurrentEMA20:   101,
		CurrentEMA50:   100,
		CurrentADX:     v.adx,
		CurrentDIPlus:  25,
		CurrentDIMinus: 15,
		CurrentRSI14:   55,
		BollingerWidth: v.bollWidth,
		OIValueUSD:     v.oi,
		FundingRate:    0.0001,
		LongerTermContext: &market.LongerTermData{
			VolumeRatio: v.volumeRatio,
		},
	}, nil
}

func dynamicTestConfig(t *testing.T) DynamicCandidatePoolConfig {
	return normalizeDynamicCandidatePoolConfig(DynamicCandidatePoolConfig{
		Enabled:                 true,
		RefreshHour:             8,
		TTLHours:                24,
		MinPoolSize:             2,
		MaxPoolSize:             5,
		PromptCandidateLimit:    4,
		CoreSymbols:             []string{"BTCUSDT", "ETHUSDT"},
		MinOIValueUSD:           15_000_000,
		MinQuoteVolume24hUSD:    20_000_000,
		CooldownDaysAfterLosses: 2,
		ExchangeVolumeTopLimit:  3,
		SnapshotPath:            t.TempDir() + "/dynamic_candidate_pool.json",
	})
}

func TestDynamicCandidatePool_DefaultSourceNotMislabelledAsAI500(t *testing.T) {
	resetConfig(t)
	withDynamicPoolStubs(t, dynamicTestMarketData, func(limit int) ([]volumeTicker, error) {
		return []volumeTicker{{Symbol: "BNBUSDT", QuoteVolume24hUSD: 120_000_000}}, nil
	})
	defaultMainstreamCoins = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	coinPoolConfig.UseDefaultCoins = true

	cfg := dynamicTestConfig(t)
	SetDynamicCandidatePoolConfig(cfg)

	merged, err := GetDynamicMergedCoinPool(20, nil, nil)
	if err != nil {
		t.Fatalf("GetDynamicMergedCoinPool 失败: %v", err)
	}
	if len(merged.AllSymbols) == 0 {
		t.Fatal("动态候选池为空")
	}

	sources := strings.Join(merged.SymbolSources["BTCUSDT"], ",")
	if strings.Contains(sources, "ai500") {
		t.Fatalf("默认币来源不应标记为 ai500: %v", merged.SymbolSources["BTCUSDT"])
	}
	if !strings.Contains(sources, "default") || !strings.Contains(sources, "core") {
		t.Fatalf("BTCUSDT 应包含 default/core 来源: %v", merged.SymbolSources["BTCUSDT"])
	}
	if _, ok := merged.DynamicCandidates["BNBUSDT"]; !ok {
		t.Fatalf("交易所成交额来源候选 BNBUSDT 未进入动态详情: %+v", merged.DynamicCandidates)
	}
}

func TestRefreshDynamicCandidatePool_FiltersLowLiquidity(t *testing.T) {
	resetConfig(t)
	withDynamicPoolStubs(t, dynamicTestMarketData, func(limit int) ([]volumeTicker, error) {
		return []volumeTicker{{Symbol: "LOWUSDT", QuoteVolume24hUSD: 5_000_000}}, nil
	})
	defaultMainstreamCoins = []string{"BTCUSDT", "ETHUSDT", "LOWUSDT"}
	coinPoolConfig.UseDefaultCoins = true

	snapshot, err := refreshDynamicCandidatePool(20, nil, nil, dynamicTestConfig(t))
	if err != nil {
		t.Fatalf("刷新动态候选池失败: %v", err)
	}
	for _, candidate := range snapshot.Symbols {
		if candidate.Symbol == "LOWUSDT" {
			t.Fatalf("低流动性 LOWUSDT 不应进入候选池: %+v", candidate)
		}
	}
	foundReject := false
	for _, reject := range snapshot.Removed {
		if reject.Symbol == "LOWUSDT" && strings.Contains(reject.Reason, "OI价值过低") {
			foundReject = true
		}
	}
	if !foundReject {
		t.Fatalf("LOWUSDT 应记录低 OI 剔除原因: %+v", snapshot.Removed)
	}
}

func TestDynamicCandidatePool_UsesFreshSnapshotWhenAvailable(t *testing.T) {
	resetConfig(t)
	cfg := dynamicTestConfig(t)
	cfg.MinPoolSize = 1
	cfg.CoreSymbols = []string{"BTCUSDT"}
	SetDynamicCandidatePoolConfig(cfg)

	snapshot := &DynamicCandidatePool{
		GeneratedAt:  time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
		MarketRegime: "range",
		Symbols: []DynamicCandidate{{
			Symbol:  "BTCUSDT",
			Tier:    "core",
			Score:   90,
			Sources: []string{"core", "default"},
		}},
	}
	if err := saveDynamicCandidatePoolSnapshot(cfg.SnapshotPath, snapshot); err != nil {
		t.Fatalf("保存测试快照失败: %v", err)
	}

	withDynamicPoolStubs(t, func(symbol string) (*market.Data, error) {
		return nil, fmt.Errorf("不应刷新行情")
	}, func(limit int) ([]volumeTicker, error) {
		return nil, fmt.Errorf("不应刷新成交额")
	})

	merged, err := GetDynamicMergedCoinPool(20, nil, nil)
	if err != nil {
		t.Fatalf("读取动态快照失败: %v", err)
	}
	if len(merged.AllSymbols) != 1 || merged.AllSymbols[0] != "BTCUSDT" {
		t.Fatalf("应直接使用新鲜快照，实际: %+v", merged.AllSymbols)
	}
	if merged.MarketRegime != "range" {
		t.Fatalf("market_regime 未从快照传递: %s", merged.MarketRegime)
	}
}

func TestParseBinanceFuturesVolumeTickers_SkipsNonUSDTPairs(t *testing.T) {
	tickers := parseBinanceFuturesVolumeTickers([]binanceFuturesTicker24h{
		{Symbol: "ETHUSDC", QuoteVolume: "900"},
		{Symbol: "SOLUSDC", QuoteVolume: "800"},
		{Symbol: "BTCUSDT", QuoteVolume: "1000"},
		{Symbol: " xrpUSDT ", QuoteVolume: "700"},
		{Symbol: "BADUSDT", QuoteVolume: "0"},
	}, 10)

	if len(tickers) != 2 {
		t.Fatalf("只应保留真实 USDT 交易对，实际: %+v", tickers)
	}
	if tickers[0].Symbol != "BTCUSDT" || tickers[0].QuoteVolume24hUSD != 1000 {
		t.Fatalf("BTCUSDT 应按成交额排序在前: %+v", tickers)
	}
	if tickers[1].Symbol != "XRPUSDT" || tickers[1].QuoteVolume24hUSD != 700 {
		t.Fatalf("应规范化并保留 XRPUSDT: %+v", tickers)
	}
	for _, ticker := range tickers {
		if strings.Contains(ticker.Symbol, "USDCUSDT") {
			t.Fatalf("不应把 USDC 合约误拼成 USDT 合约: %+v", tickers)
		}
	}
}
