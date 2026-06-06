package pool

import (
	"fmt"
	"os"
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

func TestPreviewDynamicCandidatePool_DryRunUsesSourceConfigWithoutGlobalPollution(t *testing.T) {
	resetConfig(t)
	withDynamicPoolStubs(t, dynamicTestMarketData, func(limit int) ([]volumeTicker, error) {
		return []volumeTicker{{Symbol: "BNBUSDT", QuoteVolume24hUSD: 120_000_000}}, nil
	})
	defaultMainstreamCoins = []string{"DOGEUSDT"}
	coinPoolConfig.APIURL = "http://global.example.invalid/coins"
	coinPoolConfig.UseDefaultCoins = false
	oiTopConfig.APIURL = "http://global.example.invalid/oi"

	cfg := dynamicTestConfig(t)
	cfg.ShortSideCoverage = DynamicCandidateShortSideCoverageConfig{Enabled: true, ReportOnly: true}
	snapshotPath := t.TempDir() + "/dynamic_preview.json"

	snapshot, merged, err := PreviewDynamicCandidatePool(DynamicPoolPreviewOptions{
		AI500Limit:    10,
		PoolConfig:    cfg,
		SourceConfig:  CoinPoolSourceConfig{UseDefaultCoins: true, DefaultCoins: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}, CacheDir: t.TempDir()},
		SnapshotPath:  snapshotPath,
		WriteSnapshot: false,
	})
	if err != nil {
		t.Fatalf("PreviewDynamicCandidatePool 失败: %v", err)
	}
	if snapshot == nil || merged == nil || len(merged.AllSymbols) == 0 {
		t.Fatalf("preview应返回snapshot和merged pool: snapshot=%+v merged=%+v", snapshot, merged)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("write_snapshot=false 时不应写snapshot文件: err=%v", err)
	}
	if coinPoolConfig.APIURL != "http://global.example.invalid/coins" || coinPoolConfig.UseDefaultCoins {
		t.Fatalf("preview不应污染全局coin pool配置: %+v", coinPoolConfig)
	}
	if oiTopConfig.APIURL != "http://global.example.invalid/oi" {
		t.Fatalf("preview不应污染全局OI配置: %+v", oiTopConfig)
	}
	if _, ok := merged.DynamicCandidates["SOLUSDT"]; !ok {
		t.Fatalf("preview应使用传入source config中的默认币: %+v", merged.DynamicCandidates)
	}
}

func TestPreviewDynamicCandidatePool_ShortSideReportOnlyDoesNotBoostScores(t *testing.T) {
	resetConfig(t)
	withDynamicPoolStubs(t, weakShortSideMarketData, func(limit int) ([]volumeTicker, error) {
		return []volumeTicker{
			{Symbol: "SOLUSDT", QuoteVolume24hUSD: 180_000_000},
			{Symbol: "BNBUSDT", QuoteVolume24hUSD: 160_000_000},
		}, nil
	})

	cfg := dynamicTestConfig(t)
	cfg.CoreSymbols = []string{"BTCUSDT"}
	cfg.ShortSideCoverage = DynamicCandidateShortSideCoverageConfig{
		Enabled:        true,
		ReportOnly:     true,
		MinPromptCount: 2,
		MaxPromptRatio: 0.5,
	}
	snapshot, _, err := PreviewDynamicCandidatePool(DynamicPoolPreviewOptions{
		AI500Limit:    10,
		PoolConfig:    cfg,
		SourceConfig:  CoinPoolSourceConfig{UseDefaultCoins: true, DefaultCoins: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"}, CacheDir: t.TempDir()},
		WriteSnapshot: false,
	})
	if err != nil {
		t.Fatalf("PreviewDynamicCandidatePool 失败: %v", err)
	}
	if !snapshot.ShortSideSummary.Enabled || !snapshot.ShortSideSummary.ReportOnly || !snapshot.ShortSideSummary.BTCWeak {
		t.Fatalf("short-side report-only summary缺失: %+v", snapshot.ShortSideSummary)
	}
	if snapshot.ShortSideSummary.CandidateCount == 0 {
		t.Fatalf("BTC弱势时应识别short-side候选: %+v", snapshot.Symbols)
	}
	for _, candidate := range snapshot.Symbols {
		if candidate.SideProfile.Bias != "short" {
			continue
		}
		if !candidate.SideProfile.ReportOnly {
			t.Fatalf("Phase 1 short-side候选应保持report_only=true: %+v", candidate.SideProfile)
		}
		if strings.Contains(strings.Join(candidate.Reasons, ","), "short-side候选加分") {
			t.Fatalf("report_only=true时不应做short-side排序加分: %+v", candidate.Reasons)
		}
	}
}

func TestPreviewDynamicCandidatePool_ShortSideNonReportOnlyAddsPromptCoverage(t *testing.T) {
	resetConfig(t)
	withDynamicPoolStubs(t, weakShortSideMarketData, func(limit int) ([]volumeTicker, error) {
		return []volumeTicker{
			{Symbol: "SOLUSDT", QuoteVolume24hUSD: 180_000_000},
			{Symbol: "BNBUSDT", QuoteVolume24hUSD: 160_000_000},
		}, nil
	})

	cfg := dynamicTestConfig(t)
	cfg.CoreSymbols = []string{"BTCUSDT"}
	cfg.PromptCandidateLimit = 4
	cfg.ShortSideCoverage = DynamicCandidateShortSideCoverageConfig{
		Enabled:           true,
		ReportOnly:        false,
		MinPromptCount:    2,
		MaxPromptRatio:    0.5,
		RiskOffScoreBoost: 8,
	}
	snapshot, merged, err := PreviewDynamicCandidatePool(DynamicPoolPreviewOptions{
		AI500Limit:    10,
		PoolConfig:    cfg,
		SourceConfig:  CoinPoolSourceConfig{UseDefaultCoins: true, DefaultCoins: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"}, CacheDir: t.TempDir()},
		WriteSnapshot: false,
	})
	if err != nil {
		t.Fatalf("PreviewDynamicCandidatePool 失败: %v", err)
	}
	if snapshot.ShortSideSummary.ReportOnly || !snapshot.ShortSideSummary.BTCWeak {
		t.Fatalf("应处于BTC弱势非report-only short-side模式: %+v", snapshot.ShortSideSummary)
	}
	if snapshot.ShortSideSummary.PromptCount < 2 {
		t.Fatalf("非report-only时应满足short-side prompt保底: %+v selected=%v", snapshot.ShortSideSummary, merged.AllSymbols)
	}
	foundBoosted := false
	for _, candidate := range snapshot.Symbols {
		if candidate.SideProfile.Bias == "short" && strings.Contains(strings.Join(candidate.Reasons, ","), "short-side候选加分") {
			foundBoosted = true
			break
		}
	}
	if !foundBoosted {
		t.Fatalf("非report-only时BTC弱势short-side候选应获得排序加分: %+v", snapshot.Symbols)
	}
}

func weakShortSideMarketData(symbol string) (*market.Data, error) {
	values := map[string]struct {
		oi        float64
		adx       float64
		change1h  float64
		change4h  float64
		diPlus    float64
		diMinus   float64
		ema20     float64
		ema50     float64
		bollWidth float64
	}{
		"BTCUSDT": {oi: 600_000_000, adx: 28, change1h: -4.2, change4h: -6.3, diPlus: 12, diMinus: 30, ema20: 98, ema50: 102, bollWidth: 4.0},
		"ETHUSDT": {oi: 300_000_000, adx: 31, change1h: -7.0, change4h: -9.0, diPlus: 10, diMinus: 32, ema20: 92, ema50: 100, bollWidth: 4.5},
		"SOLUSDT": {oi: 140_000_000, adx: 29, change1h: -8.0, change4h: -10.0, diPlus: 11, diMinus: 34, ema20: 88, ema50: 98, bollWidth: 4.8},
		"BNBUSDT": {oi: 90_000_000, adx: 26, change1h: -6.5, change4h: -8.2, diPlus: 12, diMinus: 29, ema20: 91, ema50: 99, bollWidth: 4.2},
	}
	v, ok := values[symbol]
	if !ok {
		return nil, fmt.Errorf("unexpected symbol %s", symbol)
	}
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   100,
		CurrentEMA20:   v.ema20,
		CurrentEMA50:   v.ema50,
		CurrentADX:     v.adx,
		CurrentDIPlus:  v.diPlus,
		CurrentDIMinus: v.diMinus,
		BollingerWidth: v.bollWidth,
		OIValueUSD:     v.oi,
		FundingRate:    0.0001,
		PriceChange1h:  v.change1h,
		PriceChange4h:  v.change4h,
		LongerTermContext: &market.LongerTermData{
			VolumeRatio: 1.4,
		},
	}, nil
}
