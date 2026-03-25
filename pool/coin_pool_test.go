package pool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

func testParams() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	params.Rng.Seed(42)
	return params
}

// setupCoinServer 创建返回指定币种列表的测试 HTTP 服务器
func setupCoinServer(t *testing.T, coins []CoinInfo) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CoinPoolAPIResponse{Success: true}
		resp.Data.Coins = coins
		resp.Data.Count = len(coins)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("编码响应失败: %v", err)
		}
	}))
}

// setupOIServer 创建返回指定 OI 持仓列表的测试 HTTP 服务器
func setupOIServer(t *testing.T, positions []OIPosition) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := OITopAPIResponse{Success: true}
		resp.Data.Positions = positions
		resp.Data.Count = len(positions)
		resp.Data.Exchange = "binance"
		resp.Data.TimeRange = "1h"
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("编码 OI 响应失败: %v", err)
		}
	}))
}

// resetConfig 重置全局配置，使用临时目录作为缓存目录
func resetConfig(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	coinPoolConfig.APIURL = ""
	coinPoolConfig.UseDefaultCoins = false
	coinPoolConfig.CacheDir = tmpDir
	oiTopConfig.APIURL = ""
	oiTopConfig.CacheDir = tmpDir
}

// ============================================================================
// normalizeSymbol 单元测试
// ============================================================================

func TestNormalizeSymbol_AlreadyUpperUSDT(t *testing.T) {
	if got := normalizeSymbol("BTCUSDT"); got != "BTCUSDT" {
		t.Errorf("期望 BTCUSDT，得到 %s", got)
	}
}

func TestNormalizeSymbol_LowercaseAddsUSDT(t *testing.T) {
	got := normalizeSymbol("btc")
	if !strings.HasSuffix(got, "USDT") {
		t.Errorf("期望以 USDT 结尾，得到 %s", got)
	}
	if got != strings.ToUpper(got) {
		t.Errorf("期望全大写，得到 %s", got)
	}
}

func TestNormalizeSymbol_WithSpaces(t *testing.T) {
	got := normalizeSymbol(" eth ")
	if strings.Contains(got, " ") {
		t.Errorf("期望无空格，得到 %s", got)
	}
}

func TestNormalizeSymbol_AlreadyHasUSDT(t *testing.T) {
	got := normalizeSymbol("ethusdt")
	if got != "ETHUSDT" {
		t.Errorf("期望 ETHUSDT，得到 %s", got)
	}
}

// ============================================================================
// GetTopRatedCoins 单元测试
// ============================================================================

func TestGetTopRatedCoins_ReturnsLimitCount(t *testing.T) {
	resetConfig(t)
	coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
		{Pair: "SOLUSDT", Score: 70, IsAvailable: true},
		{Pair: "BNBUSDT", Score: 60, IsAvailable: true},
		{Pair: "XRPUSDT", Score: 50, IsAvailable: true},
	}
	srv := setupCoinServer(t, coins)
	defer srv.Close()
	coinPoolConfig.APIURL = srv.URL

	result, err := GetTopRatedCoins(3)
	if err != nil {
		t.Fatalf("GetTopRatedCoins 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("期望 3 个币种，得到 %d", len(result))
	}
}

func TestGetTopRatedCoins_SortedByScoreDesc(t *testing.T) {
	resetConfig(t)
	coins := []CoinInfo{
		{Pair: "XRPUSDT", Score: 50, IsAvailable: true},
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
	}
	srv := setupCoinServer(t, coins)
	defer srv.Close()
	coinPoolConfig.APIURL = srv.URL

	result, err := GetTopRatedCoins(3)
	if err != nil {
		t.Fatalf("GetTopRatedCoins 返回错误: %v", err)
	}
	if result[0] != "BTCUSDT" {
		t.Errorf("期望第一个为 BTCUSDT（评分最高），得到 %s", result[0])
	}
}

func TestGetTopRatedCoins_LimitExceedsAvailable(t *testing.T) {
	resetConfig(t)
	coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
	}
	srv := setupCoinServer(t, coins)
	defer srv.Close()
	coinPoolConfig.APIURL = srv.URL

	result, err := GetTopRatedCoins(10)
	if err != nil {
		t.Fatalf("GetTopRatedCoins 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("期望 2 个币种（不超过可用数量），得到 %d", len(result))
	}
}

// ============================================================================
// GetCoinPool 重试与降级逻辑单元测试
// ============================================================================

func TestGetCoinPool_UseDefaultCoins(t *testing.T) {
	resetConfig(t)
	coinPoolConfig.UseDefaultCoins = true

	coins, err := GetCoinPool()
	if err != nil {
		t.Fatalf("GetCoinPool 返回错误: %v", err)
	}
	if len(coins) == 0 {
		t.Error("期望返回默认币种列表，得到空列表")
	}
}

func TestGetCoinPool_EmptyAPIURLFallsBackToDefault(t *testing.T) {
	resetConfig(t)

	coins, err := GetCoinPool()
	if err != nil {
		t.Fatalf("GetCoinPool 返回错误: %v", err)
	}
	if len(coins) == 0 {
		t.Error("期望回退到默认币种列表，得到空列表")
	}
}

func TestGetCoinPool_APIFailureFallsBackToCache(t *testing.T) {
	resetConfig(t)
	tmpDir := t.TempDir()
	coinPoolConfig.CacheDir = tmpDir

	cachedCoins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
	}
	cache := CoinPoolCache{Coins: cachedCoins, SourceType: "api"}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatalf("序列化缓存失败: %v", err)
	}
	cachePath := filepath.Join(tmpDir, "latest.json")
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("写入缓存文件失败: %v", err)
	}

	coinPoolConfig.APIURL = "http://127.0.0.1:19999/nonexistent"

	coins, err := GetCoinPool()
	if err != nil {
		t.Fatalf("GetCoinPool 返回错误: %v", err)
	}
	if len(coins) == 0 {
		t.Error("期望从缓存加载币种，得到空列表")
	}
}

func TestGetCoinPool_AllFailFallsBackToDefault(t *testing.T) {
	resetConfig(t)
	coinPoolConfig.APIURL = "http://127.0.0.1:19999/nonexistent"

	coins, err := GetCoinPool()
	if err != nil {
		t.Fatalf("GetCoinPool 返回错误: %v", err)
	}
	if len(coins) == 0 {
		t.Error("期望回退到默认主流币种列表，得到空列表")
	}
}

// ============================================================================
// GetMergedCoinPool 单元测试
// ============================================================================

func TestGetMergedCoinPool_DeduplicatesSymbols(t *testing.T) {
	resetConfig(t)

	ai500Coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
	}
	oiPositions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1, OIDeltaValue: 20_000_000},
		{Symbol: "SOLUSDT", Rank: 2, OIDeltaValue: 20_000_000},
	}

	coinSrv := setupCoinServer(t, ai500Coins)
	defer coinSrv.Close()
	oiSrv := setupOIServer(t, oiPositions)
	defer oiSrv.Close()

	coinPoolConfig.APIURL = coinSrv.URL
	oiTopConfig.APIURL = oiSrv.URL

	merged, err := GetMergedCoinPool(10)
	if err != nil {
		t.Fatalf("GetMergedCoinPool 返回错误: %v", err)
	}

	seen := make(map[string]int)
	for _, sym := range merged.AllSymbols {
		seen[sym]++
	}
	for sym, count := range seen {
		if count > 1 {
			t.Errorf("符号 %s 出现了 %d 次，期望只出现 1 次", sym, count)
		}
	}
}

func TestGetMergedCoinPool_SourcesTracked(t *testing.T) {
	resetConfig(t)

	ai500Coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
	}
	oiPositions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1, OIDeltaValue: 20_000_000},
		{Symbol: "ETHUSDT", Rank: 2, OIDeltaValue: 20_000_000},
	}

	coinSrv := setupCoinServer(t, ai500Coins)
	defer coinSrv.Close()
	oiSrv := setupOIServer(t, oiPositions)
	defer oiSrv.Close()

	coinPoolConfig.APIURL = coinSrv.URL
	oiTopConfig.APIURL = oiSrv.URL

	merged, err := GetMergedCoinPool(10)
	if err != nil {
		t.Fatalf("GetMergedCoinPool 返回错误: %v", err)
	}

	sources := merged.SymbolSources["BTCUSDT"]
	hasAI500, hasOITop := false, false
	for _, s := range sources {
		if s == "ai500" {
			hasAI500 = true
		}
		if s == "oi_top" {
			hasOITop = true
		}
	}
	if !hasAI500 || !hasOITop {
		t.Errorf("BTCUSDT 来源应包含 ai500 和 oi_top，实际: %v", sources)
	}
}

func TestGetMergedCoinPool_ContainsUnionOfBothSources(t *testing.T) {
	resetConfig(t)

	ai500Coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
	}
	oiPositions := []OIPosition{
		{Symbol: "SOLUSDT", Rank: 1, OIDeltaValue: 20_000_000},
		{Symbol: "BNBUSDT", Rank: 2, OIDeltaValue: 20_000_000},
	}

	coinSrv := setupCoinServer(t, ai500Coins)
	defer coinSrv.Close()
	oiSrv := setupOIServer(t, oiPositions)
	defer oiSrv.Close()

	coinPoolConfig.APIURL = coinSrv.URL
	oiTopConfig.APIURL = oiSrv.URL

	merged, err := GetMergedCoinPool(10)
	if err != nil {
		t.Fatalf("GetMergedCoinPool 返回错误: %v", err)
	}

	expected := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"}
	symSet := make(map[string]bool)
	for _, s := range merged.AllSymbols {
		symSet[s] = true
	}
	for _, e := range expected {
		if !symSet[e] {
			t.Errorf("期望 AllSymbols 包含 %s，但未找到", e)
		}
	}
}

// ============================================================================
// Property 30: 币种评分排序
// Feature: quant-trading-system, Property 30: 币种评分排序
// ============================================================================

func TestProperty30_TopRatedCoinsOrdering(t *testing.T) {
	properties := gopter.NewProperties(testParams())

	properties.Property("GetTopRatedCoins 返回恰好 limit 个币种且按评分降序", prop.ForAll(
		func(scores []float64) bool {
			if len(scores) < 2 {
				return true
			}
			limit := len(scores) / 2
			if limit < 1 {
				limit = 1
			}

			resetConfig(t)
			coins := make([]CoinInfo, len(scores))
			for i, s := range scores {
				coins[i] = CoinInfo{
					Pair:        "COIN" + string(rune('A'+i%26)) + "USDT",
					Score:       s,
					IsAvailable: true,
				}
			}

			srv := setupCoinServer(t, coins)
			defer srv.Close()
			coinPoolConfig.APIURL = srv.URL

			result, err := GetTopRatedCoins(limit)
			if err != nil {
				return false
			}
			if len(result) != limit {
				return false
			}

			// 构建评分映射，验证降序
			scoreMap := make(map[string]float64)
			for _, c := range coins {
				scoreMap[normalizeSymbol(c.Pair)] = c.Score
			}
			for i := 1; i < len(result); i++ {
				if scoreMap[result[i-1]] < scoreMap[result[i]] {
					return false
				}
			}
			return true
		},
		gen.SliceOfN(6, gen.Float64Range(1, 100)),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 31: 币种池合并去重
// Feature: quant-trading-system, Property 31: 币种池合并去重
// ============================================================================

func TestProperty31_MergedPoolDeduplication(t *testing.T) {
	properties := gopter.NewProperties(testParams())

	properties.Property("合并后 AllSymbols 包含 A∪B 所有唯一元素且无重复", prop.ForAll(
		func(aSymbols []string, bSymbols []string) bool {
			if len(aSymbols) == 0 && len(bSymbols) == 0 {
				return true
			}

			resetConfig(t)

			ai500Coins := make([]CoinInfo, len(aSymbols))
			for i, s := range aSymbols {
				ai500Coins[i] = CoinInfo{
					Pair:        normalizeSymbol(s),
					Score:       float64(i + 1),
					IsAvailable: true,
				}
			}

			oiPositions := make([]OIPosition, len(bSymbols))
			for i, s := range bSymbols {
				oiPositions[i] = OIPosition{
					Symbol:       normalizeSymbol(s),
					Rank:         i + 1,
					OIDeltaValue: 20_000_000, // 超过15M过滤阈值
				}
			}

			coinSrv := setupCoinServer(t, ai500Coins)
			defer coinSrv.Close()
			oiSrv := setupOIServer(t, oiPositions)
			defer oiSrv.Close()

			coinPoolConfig.APIURL = coinSrv.URL
			oiTopConfig.APIURL = oiSrv.URL

			merged, err := GetMergedCoinPool(len(aSymbols) + 1)
			if err != nil {
				return false
			}

			// 构建期望并集
			expected := make(map[string]bool)
			for _, s := range aSymbols {
				expected[normalizeSymbol(s)] = true
			}
			for _, s := range bSymbols {
				expected[normalizeSymbol(s)] = true
			}

			// 验证包含所有期望元素
			got := make(map[string]bool)
			for _, s := range merged.AllSymbols {
				got[s] = true
			}
			for sym := range expected {
				if !got[sym] {
					return false
				}
			}

			// 验证无重复
			seen := make(map[string]int)
			for _, s := range merged.AllSymbols {
				seen[s]++
				if seen[s] > 1 {
					return false
				}
			}
			return true
		},
		gen.SliceOfN(3, gen.OneConstOf("BTC", "ETH", "SOL", "BNB", "XRP")),
		gen.SliceOfN(3, gen.OneConstOf("BTC", "ETH", "ADA", "DOT", "AVAX")),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 32: 币种符号标准化
// Feature: quant-trading-system, Property 32: 币种符号标准化
// ============================================================================

func TestProperty32_NormalizeSymbol(t *testing.T) {
	properties := gopter.NewProperties(testParams())

	properties.Property("normalizeSymbol 产生全大写且以 USDT 结尾的字符串", prop.ForAll(
		func(input string) bool {
			if len(input) == 0 {
				return true
			}
			result := normalizeSymbol(input)

			if !strings.HasSuffix(result, "USDT") {
				return false
			}
			if result != strings.ToUpper(result) {
				return false
			}
			if strings.Contains(result, " ") {
				return false
			}
			return true
		},
		gen.OneConstOf("btc", "ETH", "sol", "BNBusdt", "xrpUSDT", "ADA", "dot"),
	))

	properties.TestingRun(t)
}

// ============================================================================
// 缓存保存与加载单元测试
// ============================================================================

func TestSaveCoinPoolCache_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	coinPoolConfig.CacheDir = tmpDir

	coins := []CoinInfo{
		{Pair: "BTCUSDT", Score: 90, IsAvailable: true},
		{Pair: "ETHUSDT", Score: 80, IsAvailable: true},
	}

	if err := saveCoinPoolCache(coins); err != nil {
		t.Fatalf("saveCoinPoolCache 失败: %v", err)
	}

	loaded, err := loadCoinPoolCache()
	if err != nil {
		t.Fatalf("loadCoinPoolCache 失败: %v", err)
	}
	if len(loaded) != len(coins) {
		t.Errorf("期望 %d 个币种，得到 %d", len(coins), len(loaded))
	}
}

func TestLoadCoinPoolCache_FileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	coinPoolConfig.CacheDir = tmpDir

	_, err := loadCoinPoolCache()
	if err == nil {
		t.Error("期望缓存文件不存在时返回错误")
	}
}

func TestSaveOITopCache_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	oiTopConfig.CacheDir = tmpDir

	positions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1, CurrentOI: 1000},
	}

	if err := saveOITopCache(positions); err != nil {
		t.Fatalf("saveOITopCache 失败: %v", err)
	}

	loaded, err := loadOITopCache()
	if err != nil {
		t.Fatalf("loadOITopCache 失败: %v", err)
	}
	if len(loaded) != len(positions) {
		t.Errorf("期望 %d 个持仓，得到 %d", len(positions), len(loaded))
	}
}

// ============================================================================
// GetOITopPositions 降级逻辑单元测试
// ============================================================================

func TestGetOITopPositions_EmptyAPIURLReturnsEmpty(t *testing.T) {
	resetConfig(t)

	positions, err := GetOITopPositions()
	if err != nil {
		t.Fatalf("GetOITopPositions 返回错误: %v", err)
	}
	if len(positions) != 0 {
		t.Errorf("期望空列表，得到 %d 个持仓", len(positions))
	}
}

func TestGetOITopPositions_APIFailureFallsBackToCache(t *testing.T) {
	tmpDir := t.TempDir()
	oiTopConfig.CacheDir = tmpDir

	cachedPositions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1},
	}
	cache := OITopCache{Positions: cachedPositions, SourceType: "api"}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatalf("序列化 OI 缓存失败: %v", err)
	}
	cachePath := filepath.Join(tmpDir, "oi_top_latest.json")
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("写入 OI 缓存文件失败: %v", err)
	}

	oiTopConfig.APIURL = "http://127.0.0.1:19999/nonexistent"

	positions, err := GetOITopPositions()
	if err != nil {
		t.Fatalf("GetOITopPositions 返回错误: %v", err)
	}
	if len(positions) == 0 {
		t.Error("期望从缓存加载 OI 持仓，得到空列表")
	}
}

// ============================================================================
// convertSymbolsToCoins 单元测试
// ============================================================================

func TestConvertSymbolsToCoins_AllAvailable(t *testing.T) {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	coins := convertSymbolsToCoins(symbols)

	if len(coins) != len(symbols) {
		t.Errorf("期望 %d 个币种，得到 %d", len(symbols), len(coins))
	}
	for _, c := range coins {
		if !c.IsAvailable {
			t.Errorf("期望 %s 的 IsAvailable=true", c.Pair)
		}
	}
}

// 确保 sort 包被使用（用于 Property 30 的排序验证辅助）
var _ = sort.Slice

// ============================================================================
// 重试机制单元测试（使用 httptest 计数实际请求次数）
// ============================================================================

func TestGetCoinPool_RetryCount(t *testing.T) {
	resetConfig(t)

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "服务器错误", http.StatusInternalServerError)
	}))
	defer srv.Close()

	coinPoolConfig.APIURL = srv.URL

	coins, err := GetCoinPool()
	if err != nil {
		t.Fatalf("GetCoinPool 返回错误: %v", err)
	}
	// 应回退到默认币种
	if len(coins) == 0 {
		t.Error("期望回退到默认币种列表，得到空列表")
	}
	// 应恰好重试 3 次
	if requestCount != 3 {
		t.Errorf("期望恰好 3 次请求（含重试），实际 %d 次", requestCount)
	}
}

func TestGetOITopPositions_RetryCount(t *testing.T) {
	resetConfig(t)

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "服务器错误", http.StatusInternalServerError)
	}))
	defer srv.Close()

	oiTopConfig.APIURL = srv.URL

	positions, err := GetOITopPositions()
	if err != nil {
		t.Fatalf("GetOITopPositions 返回错误: %v", err)
	}
	// OI Top 失败时返回空列表（非错误）
	if len(positions) != 0 {
		t.Errorf("期望空列表，得到 %d 个持仓", len(positions))
	}
	// 应恰好重试 3 次
	if requestCount != 3 {
		t.Errorf("期望恰好 3 次请求（含重试），实际 %d 次", requestCount)
	}
}

// ============================================================================
// OI 价值过滤单元测试（需求 7.10）
// ============================================================================

func TestGetOITopSymbols_FiltersLowOIValue(t *testing.T) {
	resetConfig(t)

	positions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1, OIDeltaValue: 50_000_000}, // 50M — 保留
		{Symbol: "ETHUSDT", Rank: 2, OIDeltaValue: 14_999_999}, // 14.9M — 过滤
		{Symbol: "SOLUSDT", Rank: 3, OIDeltaValue: 15_000_000}, // 恰好 15M — 保留
		{Symbol: "XRPUSDT", Rank: 4, OIDeltaValue: 0},          // 0 — 过滤
	}

	srv := setupOIServer(t, positions)
	defer srv.Close()
	oiTopConfig.APIURL = srv.URL

	symbols, err := GetOITopSymbols()
	if err != nil {
		t.Fatalf("GetOITopSymbols 返回错误: %v", err)
	}

	// 期望保留 BTCUSDT 和 SOLUSDT
	if len(symbols) != 2 {
		t.Errorf("期望 2 个符号（过滤后），得到 %d: %v", len(symbols), symbols)
	}

	symSet := make(map[string]bool)
	for _, s := range symbols {
		symSet[s] = true
	}
	if !symSet["BTCUSDT"] {
		t.Error("期望 BTCUSDT 在结果中（OI=50M）")
	}
	if !symSet["SOLUSDT"] {
		t.Error("期望 SOLUSDT 在结果中（OI=15M，恰好达到阈值）")
	}
	if symSet["ETHUSDT"] {
		t.Error("期望 ETHUSDT 被过滤（OI=14.9M < 15M）")
	}
	if symSet["XRPUSDT"] {
		t.Error("期望 XRPUSDT 被过滤（OI=0）")
	}
}

func TestGetOITopSymbols_AllBelowThresholdReturnsEmpty(t *testing.T) {
	resetConfig(t)

	positions := []OIPosition{
		{Symbol: "BTCUSDT", Rank: 1, OIDeltaValue: 1_000_000}, // 1M — 过滤
		{Symbol: "ETHUSDT", Rank: 2, OIDeltaValue: 5_000_000}, // 5M — 过滤
	}

	srv := setupOIServer(t, positions)
	defer srv.Close()
	oiTopConfig.APIURL = srv.URL

	symbols, err := GetOITopSymbols()
	if err != nil {
		t.Fatalf("GetOITopSymbols 返回错误: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("期望空列表（全部低于阈值），得到 %d 个: %v", len(symbols), symbols)
	}
}
