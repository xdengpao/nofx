package pool

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nofx/logger"
	"nofx/market"
)

// DynamicCandidatePoolConfig 控制动态候选池生成。
type DynamicCandidatePoolConfig struct {
	Enabled                 bool
	RefreshHour             int
	TTLHours                int
	MinPoolSize             int
	MaxPoolSize             int
	PromptCandidateLimit    int
	CoreSymbols             []string
	MinOIValueUSD           float64
	MinQuoteVolume24hUSD    float64
	CooldownDaysAfterLosses int
	ExchangeVolumeTopLimit  int
	SnapshotPath            string
	ShortSideCoverage       DynamicCandidateShortSideCoverageConfig
}

// DynamicCandidateShortSideCoverageConfig 控制 BTC 弱势时的 short-side 候选覆盖评估。
type DynamicCandidateShortSideCoverageConfig struct {
	Enabled               bool
	ReportOnly            bool
	MinPromptCount        int
	MaxPromptRatio        float64
	RiskOffScoreBoost     float64
	MinADX                float64
	MinRelativeWeakness1h float64
	MinRelativeWeakness4h float64
	RequireBearishDI      bool
	RequireBearishEMA     bool
	MaxAbsFundingRate     float64
}

// CoinPoolSourceConfig 描述评估命令使用的候选池数据源，避免 dry-run 污染包级配置。
type CoinPoolSourceConfig struct {
	CoinPoolAPIURL  string
	OITopAPIURL     string
	UseDefaultCoins bool
	DefaultCoins    []string
	CacheDir        string
	Timeout         time.Duration
	useGlobalConfig bool
}

// DynamicPoolPreviewOptions 控制动态候选池只读预览。
type DynamicPoolPreviewOptions struct {
	AI500Limit      int
	PositionSymbols []string
	Performance     *logger.PerformanceAnalysis
	PoolConfig      DynamicCandidatePoolConfig
	SourceConfig    CoinPoolSourceConfig
	SnapshotPath    string
	WriteSnapshot   bool
	ForceRefresh    bool
}

// MarketRegimeDiagnostics 记录 BTC regime 的结构化依据。
type MarketRegimeDiagnostics struct {
	Regime            string   `json:"regime"`
	BTCPriceChange1h  float64  `json:"btc_price_change_1h,omitempty"`
	BTCPriceChange4h  float64  `json:"btc_price_change_4h,omitempty"`
	BTCADX            float64  `json:"btc_adx,omitempty"`
	BTCDIPlus         float64  `json:"btc_di_plus,omitempty"`
	BTCDIMinus        float64  `json:"btc_di_minus,omitempty"`
	BTCEMA20          float64  `json:"btc_ema20,omitempty"`
	BTCEMA50          float64  `json:"btc_ema50,omitempty"`
	BTCBollingerWidth float64  `json:"btc_bollinger_width,omitempty"`
	Reasons           []string `json:"reasons,omitempty"`
}

// CandidateSideProfile 记录候选标的的方向倾向，只影响候选覆盖，不是交易信号。
type CandidateSideProfile struct {
	Bias               string   `json:"bias,omitempty"`
	ShortScore         float64  `json:"short_score,omitempty"`
	LongScore          float64  `json:"long_score,omitempty"`
	RelativeWeakness1h float64  `json:"relative_weakness_1h,omitempty"`
	RelativeWeakness4h float64  `json:"relative_weakness_4h,omitempty"`
	Reasons            []string `json:"reasons,omitempty"`
	ReportOnly         bool     `json:"report_only,omitempty"`
}

// ShortSideSummary 汇总 BTC 弱势下 short-side 候选覆盖。
type ShortSideSummary struct {
	Enabled        bool     `json:"enabled"`
	ReportOnly     bool     `json:"report_only"`
	BTCWeak        bool     `json:"btc_weak"`
	CandidateCount int      `json:"candidate_count,omitempty"`
	PromptCount    int      `json:"prompt_count,omitempty"`
	MinPromptCount int      `json:"min_prompt_count,omitempty"`
	MaxPromptRatio float64  `json:"max_prompt_ratio,omitempty"`
	Symbols        []string `json:"symbols,omitempty"`
	Reasons        []string `json:"reasons,omitempty"`
}

// DynamicCandidatePool 是每日动态候选池快照。
type DynamicCandidatePool struct {
	GeneratedAt       time.Time               `json:"generated_at"`
	ExpiresAt         time.Time               `json:"expires_at"`
	MarketRegime      string                  `json:"market_regime"`
	RegimeDiagnostics MarketRegimeDiagnostics `json:"regime_diagnostics,omitempty"`
	ShortSideSummary  ShortSideSummary        `json:"short_side_summary,omitempty"`
	Symbols           []DynamicCandidate      `json:"symbols"`
	Removed           []CandidateReject       `json:"removed,omitempty"`
	SourceStatus      map[string]string       `json:"source_status,omitempty"`
}

// DynamicCandidate 记录候选币的池内分层、评分和入池原因。
type DynamicCandidate struct {
	Symbol      string               `json:"symbol"`
	Tier        string               `json:"tier"`
	Score       float64              `json:"score"`
	Sources     []string             `json:"sources"`
	Reasons     []string             `json:"reasons,omitempty"`
	Metrics     CandidateMetrics     `json:"metrics"`
	SideProfile CandidateSideProfile `json:"side_profile,omitempty"`
}

// CandidateMetrics 记录动态评分使用的核心指标。
type CandidateMetrics struct {
	OIValueUSD        float64 `json:"oi_value_usd,omitempty"`
	QuoteVolume24hUSD float64 `json:"quote_volume_24h_usd,omitempty"`
	VolumeRatio       float64 `json:"volume_ratio,omitempty"`
	ADX               float64 `json:"adx,omitempty"`
	PriceChange1h     float64 `json:"price_change_1h,omitempty"`
	PriceChange4h     float64 `json:"price_change_4h,omitempty"`
	BollingerWidth    float64 `json:"bollinger_width,omitempty"`
	FundingRate       float64 `json:"funding_rate,omitempty"`
	WinRate           float64 `json:"win_rate,omitempty"`
	TotalPnL          float64 `json:"total_pnl,omitempty"`
	TotalTrades       int     `json:"total_trades,omitempty"`
}

// CandidateReject 记录候选币被剔除的原因。
type CandidateReject struct {
	Symbol  string           `json:"symbol"`
	Sources []string         `json:"sources,omitempty"`
	Reason  string           `json:"reason"`
	Metrics CandidateMetrics `json:"metrics,omitempty"`
}

type volumeTicker struct {
	Symbol            string
	QuoteVolume24hUSD float64
}

type binanceFuturesTicker24h struct {
	Symbol      string `json:"symbol"`
	QuoteVolume string `json:"quoteVolume"`
}

var dynamicCandidatePoolConfig = defaultDynamicCandidatePoolConfig()

var getMarketDataForDynamicPool = market.Get
var fetchExchangeVolumeTickers = fetchBinanceFuturesVolumeTickers

const (
	// market.BollingerWidth is stored as a percentage, not a 0-1 ratio.
	dynamicBollingerWidthLowPct      = 1.5
	dynamicBollingerWidthNormalPct   = 8.0
	dynamicBollingerWidthElevatedPct = 14.0
	dynamicBollingerWidthHighPct     = 20.0
	dynamicBollingerWidthRegimePct   = 15.0
)

func defaultDynamicCandidatePoolConfig() DynamicCandidatePoolConfig {
	return DynamicCandidatePoolConfig{
		Enabled:                 false,
		RefreshHour:             8,
		TTLHours:                24,
		MinPoolSize:             15,
		MaxPoolSize:             30,
		PromptCandidateLimit:    8,
		CoreSymbols:             []string{"BTCUSDT", "ETHUSDT"},
		MinOIValueUSD:           15_000_000,
		MinQuoteVolume24hUSD:    20_000_000,
		CooldownDaysAfterLosses: 2,
		ExchangeVolumeTopLimit:  30,
		SnapshotPath:            "data/dynamic_candidate_pool.json",
		ShortSideCoverage:       defaultDynamicCandidateShortSideCoverageConfig(),
	}
}

func defaultDynamicCandidateShortSideCoverageConfig() DynamicCandidateShortSideCoverageConfig {
	return DynamicCandidateShortSideCoverageConfig{
		Enabled:               false,
		ReportOnly:            true,
		MinPromptCount:        3,
		MaxPromptRatio:        0.4,
		RiskOffScoreBoost:     8,
		MinADX:                18,
		MinRelativeWeakness1h: 0.5,
		MinRelativeWeakness4h: 1.0,
		MaxAbsFundingRate:     0.001,
	}
}

// SetDynamicCandidatePoolConfig 设置动态候选池配置。
func SetDynamicCandidatePoolConfig(cfg DynamicCandidatePoolConfig) {
	dynamicCandidatePoolConfig = normalizeDynamicCandidatePoolConfig(cfg)
	if dynamicCandidatePoolConfig.Enabled {
		log.Printf("✓ 已启用动态候选池: 每日%d点刷新, 快照=%s, pool=%d-%d, prompt=%d",
			dynamicCandidatePoolConfig.RefreshHour,
			dynamicCandidatePoolConfig.SnapshotPath,
			dynamicCandidatePoolConfig.MinPoolSize,
			dynamicCandidatePoolConfig.MaxPoolSize,
			dynamicCandidatePoolConfig.PromptCandidateLimit)
	} else {
		log.Printf("ℹ️ 动态候选池未启用，继续使用静态合并币种池")
	}
}

// IsDynamicCandidatePoolEnabled 返回动态候选池是否启用。
func IsDynamicCandidatePoolEnabled() bool {
	return dynamicCandidatePoolConfig.Enabled
}

func normalizeDynamicCandidatePoolConfig(cfg DynamicCandidatePoolConfig) DynamicCandidatePoolConfig {
	defaults := defaultDynamicCandidatePoolConfig()
	if cfg.RefreshHour <= 0 || cfg.RefreshHour > 23 {
		cfg.RefreshHour = defaults.RefreshHour
	}
	if cfg.TTLHours <= 0 {
		cfg.TTLHours = defaults.TTLHours
	}
	if cfg.MinPoolSize <= 0 {
		cfg.MinPoolSize = defaults.MinPoolSize
	}
	if cfg.MaxPoolSize <= 0 {
		cfg.MaxPoolSize = defaults.MaxPoolSize
	}
	if cfg.MaxPoolSize < cfg.MinPoolSize {
		cfg.MaxPoolSize = cfg.MinPoolSize
	}
	if cfg.PromptCandidateLimit <= 0 {
		cfg.PromptCandidateLimit = defaults.PromptCandidateLimit
	}
	if cfg.PromptCandidateLimit > cfg.MaxPoolSize {
		cfg.PromptCandidateLimit = cfg.MaxPoolSize
	}
	if len(cfg.CoreSymbols) == 0 {
		cfg.CoreSymbols = append([]string(nil), defaults.CoreSymbols...)
	}
	if cfg.MinOIValueUSD <= 0 {
		cfg.MinOIValueUSD = defaults.MinOIValueUSD
	}
	if cfg.MinQuoteVolume24hUSD <= 0 {
		cfg.MinQuoteVolume24hUSD = defaults.MinQuoteVolume24hUSD
	}
	if cfg.CooldownDaysAfterLosses <= 0 {
		cfg.CooldownDaysAfterLosses = defaults.CooldownDaysAfterLosses
	}
	if cfg.ExchangeVolumeTopLimit <= 0 {
		cfg.ExchangeVolumeTopLimit = defaults.ExchangeVolumeTopLimit
	}
	if cfg.SnapshotPath == "" {
		cfg.SnapshotPath = defaults.SnapshotPath
	}
	cfg.ShortSideCoverage = normalizeDynamicCandidateShortSideCoverageConfig(cfg.ShortSideCoverage)
	cfg.CoreSymbols = normalizeSymbolList(cfg.CoreSymbols)
	return cfg
}

func normalizeDynamicCandidateShortSideCoverageConfig(cfg DynamicCandidateShortSideCoverageConfig) DynamicCandidateShortSideCoverageConfig {
	defaults := defaultDynamicCandidateShortSideCoverageConfig()
	enabled := cfg.Enabled
	reportOnly := cfg.ReportOnly
	if cfg.MinPromptCount <= 0 {
		cfg.MinPromptCount = defaults.MinPromptCount
	}
	if cfg.MaxPromptRatio <= 0 || cfg.MaxPromptRatio > 1 {
		cfg.MaxPromptRatio = defaults.MaxPromptRatio
	}
	if cfg.RiskOffScoreBoost <= 0 || cfg.RiskOffScoreBoost > 50 {
		cfg.RiskOffScoreBoost = defaults.RiskOffScoreBoost
	}
	if cfg.MinADX <= 0 || cfg.MinADX > 100 {
		cfg.MinADX = defaults.MinADX
	}
	if cfg.MinRelativeWeakness1h < 0 || cfg.MinRelativeWeakness1h > 50 {
		cfg.MinRelativeWeakness1h = defaults.MinRelativeWeakness1h
	}
	if cfg.MinRelativeWeakness4h < 0 || cfg.MinRelativeWeakness4h > 100 {
		cfg.MinRelativeWeakness4h = defaults.MinRelativeWeakness4h
	}
	if cfg.MaxAbsFundingRate <= 0 || cfg.MaxAbsFundingRate > 0.01 {
		cfg.MaxAbsFundingRate = defaults.MaxAbsFundingRate
	}
	cfg.Enabled = enabled
	cfg.ReportOnly = reportOnly
	return cfg
}

// GetDynamicMergedCoinPool 返回动态候选池；未启用或刷新失败时回退旧合并逻辑。
func GetDynamicMergedCoinPool(ai500Limit int, positionSymbols []string, performance *logger.PerformanceAnalysis) (*MergedCoinPool, error) {
	cfg := dynamicCandidatePoolConfig
	if !cfg.Enabled {
		return GetMergedCoinPool(ai500Limit)
	}

	snapshot, err := loadOrRefreshDynamicCandidatePool(ai500Limit, positionSymbols, performance, cfg)
	if err != nil {
		log.Printf("⚠️ 动态候选池不可用，回退静态合并池: %v", err)
		return GetMergedCoinPool(ai500Limit)
	}

	selected := selectPromptCandidates(snapshot, positionSymbols, cfg)
	if len(selected) == 0 {
		log.Printf("⚠️ 动态候选池为空，回退静态合并池")
		return GetMergedCoinPool(ai500Limit)
	}

	log.Printf("📋 动态候选池: regime=%s, 快照候选=%d, 本轮候选=%d",
		snapshot.MarketRegime, len(snapshot.Symbols), len(selected))
	if snapshot.ShortSideSummary.Enabled {
		log.Printf("📋 short-side候选覆盖: btc_weak=%v, report_only=%v, candidates=%d, prompt=%d",
			snapshot.ShortSideSummary.BTCWeak,
			snapshot.ShortSideSummary.ReportOnly,
			snapshot.ShortSideSummary.CandidateCount,
			snapshot.ShortSideSummary.PromptCount)
	}

	return mergedPoolFromDynamicSnapshot(snapshot, selected, cfg, positionSymbols), nil
}

func loadOrRefreshDynamicCandidatePool(ai500Limit int, positionSymbols []string, performance *logger.PerformanceAnalysis, cfg DynamicCandidatePoolConfig) (*DynamicCandidatePool, error) {
	existing, loadErr := loadDynamicCandidatePoolSnapshot(cfg.SnapshotPath)
	if loadErr == nil && existing != nil && dynamicSnapshotFresh(existing, cfg) && len(existing.Symbols) >= cfg.MinPoolSize {
		return existing, nil
	}

	refreshed, refreshErr := refreshDynamicCandidatePool(ai500Limit, positionSymbols, performance, cfg)
	if refreshErr == nil {
		if err := saveDynamicCandidatePoolSnapshot(cfg.SnapshotPath, refreshed); err != nil {
			log.Printf("⚠️ 保存动态候选池快照失败: %v", err)
		}
		logDynamicPoolDiff(existing, refreshed)
		return refreshed, nil
	}

	if existing != nil && len(existing.Symbols) > 0 {
		log.Printf("⚠️ 动态候选池刷新失败，使用旧快照: %v", refreshErr)
		return existing, nil
	}
	if loadErr != nil {
		return nil, fmt.Errorf("读取快照失败: %v; 刷新失败: %w", loadErr, refreshErr)
	}
	return nil, refreshErr
}

func refreshDynamicCandidatePool(ai500Limit int, positionSymbols []string, performance *logger.PerformanceAnalysis, cfg DynamicCandidatePoolConfig) (*DynamicCandidatePool, error) {
	return refreshDynamicCandidatePoolWithSources(ai500Limit, positionSymbols, performance, cfg, dynamicSourceConfigFromGlobals())
}

func refreshDynamicCandidatePoolWithSources(ai500Limit int, positionSymbols []string, performance *logger.PerformanceAnalysis, cfg DynamicCandidatePoolConfig, sourceCfg CoinPoolSourceConfig) (*DynamicCandidatePool, error) {
	cfg = normalizeDynamicCandidatePoolConfig(cfg)
	sourceCfg = normalizeCoinPoolSourceConfig(sourceCfg)
	sourceStatus := make(map[string]string)
	sources := make(map[string]map[string]bool)

	addSource := func(symbol, source string) {
		symbol = normalizeDynamicSymbol(symbol)
		if symbol == "" {
			return
		}
		if sources[symbol] == nil {
			sources[symbol] = make(map[string]bool)
		}
		sources[symbol][source] = true
	}

	for _, symbol := range cfg.CoreSymbols {
		addSource(symbol, "core")
	}
	defaultCoins := defaultCoinsFromSource(sourceCfg)
	for _, symbol := range defaultCoins {
		addSource(symbol, "default")
	}
	sourceStatus["default"] = fmt.Sprintf("ok:%d", len(defaultCoins))

	for _, symbol := range positionSymbols {
		addSource(symbol, "position")
	}
	if len(positionSymbols) > 0 {
		sourceStatus["position"] = fmt.Sprintf("ok:%d", len(positionSymbols))
	}

	if !sourceCfg.UseDefaultCoins && strings.TrimSpace(sourceCfg.CoinPoolAPIURL) != "" {
		ai500Symbols, err := getTopRatedCoinsFromSource(ai500Limit, sourceCfg)
		if err != nil {
			sourceStatus["ai500"] = "error:" + err.Error()
		} else {
			sourceStatus["ai500"] = fmt.Sprintf("ok:%d", len(ai500Symbols))
			for _, symbol := range ai500Symbols {
				addSource(symbol, "ai500")
			}
		}
	} else if sourceCfg.UseDefaultCoins {
		sourceStatus["ai500"] = "skipped:use_default_coins"
	} else {
		sourceStatus["ai500"] = "skipped:no_api_url"
	}

	oiSymbols, err := getOITopSymbolsFromSource(sourceCfg)
	if err != nil {
		sourceStatus["oi_top"] = "error:" + err.Error()
	} else {
		sourceStatus["oi_top"] = fmt.Sprintf("ok:%d", len(oiSymbols))
		for _, symbol := range oiSymbols {
			addSource(symbol, "oi_top")
		}
	}

	volumeBySymbol := make(map[string]float64)
	volumeTickers, err := fetchExchangeVolumeTickers(cfg.ExchangeVolumeTopLimit)
	if err != nil {
		sourceStatus["exchange_volume_top"] = "error:" + err.Error()
	} else {
		sourceStatus["exchange_volume_top"] = fmt.Sprintf("ok:%d", len(volumeTickers))
		for _, ticker := range volumeTickers {
			symbol := normalizeDynamicSymbol(ticker.Symbol)
			if symbol == "" {
				continue
			}
			volumeBySymbol[symbol] = ticker.QuoteVolume24hUSD
			addSource(symbol, "exchange_volume_top")
		}
	}

	now := time.Now()
	btcData, _ := getMarketDataForDynamicPool("BTCUSDT")
	regime, regimeDiagnostics := detectDynamicMarketRegimeWithDiagnostics(btcData)

	coreSet := makeStringSet(cfg.CoreSymbols)
	positionSet := makeStringSet(positionSymbols)
	candidates := make([]DynamicCandidate, 0, len(sources))
	rejects := make([]CandidateReject, 0)

	for symbol, sourceSet := range sources {
		sourceList := sortedSourceList(sourceSet)
		data, err := getMarketDataForDynamicPool(symbol)
		if err != nil || data == nil {
			rejects = append(rejects, CandidateReject{Symbol: symbol, Sources: sourceList, Reason: fmt.Sprintf("市场数据获取失败: %v", err)})
			continue
		}

		metrics := metricsFromMarketData(data, volumeBySymbol[symbol], performance)
		forced := coreSet[symbol] || positionSet[symbol]
		if reason := hardRejectReason(symbol, metrics, data, performance, forced, cfg); reason != "" {
			rejects = append(rejects, CandidateReject{Symbol: symbol, Sources: sourceList, Reason: reason, Metrics: metrics})
			continue
		}

		score, reasons := scoreDynamicCandidate(symbol, data, metrics, performance, forced, regime)
		sideProfile := scoreDirectionalProfile(symbol, data, btcData, metrics, cfg.ShortSideCoverage)
		if cfg.ShortSideCoverage.Enabled && !cfg.ShortSideCoverage.ReportOnly && isBTCWeakRegime(regime, regimeDiagnostics) && sideProfile.Bias == "short" {
			score += cfg.ShortSideCoverage.RiskOffScoreBoost
			reasons = append(reasons, fmt.Sprintf("BTC弱势short-side候选加分 %.1f", cfg.ShortSideCoverage.RiskOffScoreBoost))
		}
		tier := classifyDynamicTier(symbol, data, metrics, coreSet, positionSet)
		candidates = append(candidates, DynamicCandidate{
			Symbol:      symbol,
			Tier:        tier,
			Score:       clamp(score, 0, 100),
			Sources:     sourceList,
			Reasons:     reasons,
			Metrics:     metrics,
			SideProfile: sideProfile,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Symbol < candidates[j].Symbol
		}
		return candidates[i].Score > candidates[j].Score
	})

	candidates = enforceDynamicPoolBounds(candidates, cfg, coreSet, positionSet)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("动态候选池刷新后为空")
	}
	shortSideSummary := buildShortSideSummary(candidates, positionSymbols, cfg, regime, regimeDiagnostics)

	return &DynamicCandidatePool{
		GeneratedAt:       now,
		ExpiresAt:         nextDynamicRefreshTime(now, cfg),
		MarketRegime:      regime,
		RegimeDiagnostics: regimeDiagnostics,
		ShortSideSummary:  shortSideSummary,
		Symbols:           candidates,
		Removed:           rejects,
		SourceStatus:      sourceStatus,
	}, nil
}

func loadDynamicCandidatePoolSnapshot(path string) (*DynamicCandidatePool, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot DynamicCandidatePool
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func dynamicSourceConfigFromGlobals() CoinPoolSourceConfig {
	return CoinPoolSourceConfig{
		CoinPoolAPIURL:  coinPoolConfig.APIURL,
		OITopAPIURL:     oiTopConfig.APIURL,
		UseDefaultCoins: coinPoolConfig.UseDefaultCoins,
		DefaultCoins:    append([]string(nil), defaultMainstreamCoins...),
		CacheDir:        coinPoolConfig.CacheDir,
		Timeout:         coinPoolConfig.Timeout,
		useGlobalConfig: true,
	}
}

func normalizeCoinPoolSourceConfig(cfg CoinPoolSourceConfig) CoinPoolSourceConfig {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = "coin_pool_cache"
	}
	cfg.DefaultCoins = normalizeSymbolList(cfg.DefaultCoins)
	if len(cfg.DefaultCoins) == 0 {
		cfg.DefaultCoins = normalizeSymbolList(defaultMainstreamCoins)
	}
	return cfg
}

func defaultCoinsFromSource(cfg CoinPoolSourceConfig) []string {
	cfg = normalizeCoinPoolSourceConfig(cfg)
	return append([]string(nil), cfg.DefaultCoins...)
}

func getTopRatedCoinsFromSource(limit int, cfg CoinPoolSourceConfig) ([]string, error) {
	if cfg.useGlobalConfig {
		return GetTopRatedCoins(limit)
	}
	if limit <= 0 {
		limit = 20
	}
	coins, err := fetchCoinPoolFromSource(cfg)
	if err != nil {
		return nil, err
	}
	available := make([]CoinInfo, 0, len(coins))
	for _, coin := range coins {
		if coin.IsAvailable {
			available = append(available, coin)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("没有可用的币种")
	}
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].Score == available[j].Score {
			return normalizeSymbol(available[i].Pair) < normalizeSymbol(available[j].Pair)
		}
		return available[i].Score > available[j].Score
	})
	if limit > len(available) {
		limit = len(available)
	}
	symbols := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		symbols = append(symbols, normalizeSymbol(available[i].Pair))
	}
	return symbols, nil
}

func fetchCoinPoolFromSource(cfg CoinPoolSourceConfig) ([]CoinInfo, error) {
	cfg = normalizeCoinPoolSourceConfig(cfg)
	if cfg.UseDefaultCoins {
		return convertSymbolsToCoins(cfg.DefaultCoins), nil
	}
	if strings.TrimSpace(cfg.CoinPoolAPIURL) == "" {
		return nil, fmt.Errorf("未配置币种池API URL")
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Get(cfg.CoinPoolAPIURL)
	if err != nil {
		return nil, fmt.Errorf("请求币种池API失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取币种池响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("币种池API返回错误 (status %d): %s", resp.StatusCode, string(body))
	}
	var response CoinPoolAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("币种池JSON解析失败: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("币种池API返回失败状态")
	}
	if len(response.Data.Coins) == 0 {
		return nil, fmt.Errorf("币种列表为空")
	}
	coins := response.Data.Coins
	for i := range coins {
		coins[i].IsAvailable = true
	}
	return coins, nil
}

func getOITopSymbolsFromSource(cfg CoinPoolSourceConfig) ([]string, error) {
	if cfg.useGlobalConfig {
		return GetOITopSymbols()
	}
	positions, err := fetchOITopPositionsFromSource(cfg)
	if err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(positions))
	for _, pos := range positions {
		if pos.OIDeltaValue < oiMinValueUSD {
			continue
		}
		symbols = append(symbols, normalizeSymbol(pos.Symbol))
	}
	return symbols, nil
}

func fetchOITopPositionsFromSource(cfg CoinPoolSourceConfig) ([]OIPosition, error) {
	cfg = normalizeCoinPoolSourceConfig(cfg)
	if strings.TrimSpace(cfg.OITopAPIURL) == "" {
		return []OIPosition{}, nil
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Get(cfg.OITopAPIURL)
	if err != nil {
		return nil, fmt.Errorf("请求OI Top API失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取OI Top响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OI Top API返回错误 (status %d): %s", resp.StatusCode, string(body))
	}
	var response OITopAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("OI Top JSON解析失败: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("OI Top API返回失败状态")
	}
	return response.Data.Positions, nil
}

// PreviewDynamicCandidatePool 返回动态候选池 dry-run 预览，不修改运行时全局配置。
func PreviewDynamicCandidatePool(opts DynamicPoolPreviewOptions) (*DynamicCandidatePool, *MergedCoinPool, error) {
	cfg := normalizeDynamicCandidatePoolConfig(opts.PoolConfig)
	cfg.Enabled = true
	if opts.SnapshotPath != "" {
		cfg.SnapshotPath = opts.SnapshotPath
	} else if opts.WriteSnapshot {
		cfg.SnapshotPath = filepath.Join(os.TempDir(), "nofx_dynamic_candidate_pool_preview.json")
	}
	ai500Limit := opts.AI500Limit
	if ai500Limit <= 0 {
		ai500Limit = 20
	}
	snapshot, err := refreshDynamicCandidatePoolWithSources(ai500Limit, opts.PositionSymbols, opts.Performance, cfg, opts.SourceConfig)
	if err != nil {
		return nil, nil, err
	}
	if opts.WriteSnapshot {
		if err := saveDynamicCandidatePoolSnapshot(cfg.SnapshotPath, snapshot); err != nil {
			return nil, nil, err
		}
	}
	selected := selectPromptCandidates(snapshot, opts.PositionSymbols, cfg)
	merged := mergedPoolFromDynamicSnapshot(snapshot, selected, cfg, opts.PositionSymbols)
	return snapshot, merged, nil
}

func saveDynamicCandidatePoolSnapshot(path string, snapshot *DynamicCandidatePool) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, data, 0644)
}

func dynamicSnapshotFresh(snapshot *DynamicCandidatePool, cfg DynamicCandidatePoolConfig) bool {
	if snapshot == nil || len(snapshot.Symbols) == 0 {
		return false
	}
	now := time.Now()
	if !snapshot.ExpiresAt.IsZero() && now.After(snapshot.ExpiresAt) {
		return false
	}
	if cfg.TTLHours > 0 && now.Sub(snapshot.GeneratedAt) > time.Duration(cfg.TTLHours)*time.Hour {
		return false
	}
	return true
}

func nextDynamicRefreshTime(now time.Time, cfg DynamicCandidatePoolConfig) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), cfg.RefreshHour, 0, 0, 0, now.Location())
	if !now.Before(next) {
		next = next.Add(24 * time.Hour)
	}
	ttlExpiry := now.Add(time.Duration(cfg.TTLHours) * time.Hour)
	if ttlExpiry.Before(next) {
		return ttlExpiry
	}
	return next
}

func fetchBinanceFuturesVolumeTickers(limit int) ([]volumeTicker, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://fapi.binance.com/fapi/v1/ticker/24hr")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ticker 24hr status=%d", resp.StatusCode)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var raw []binanceFuturesTicker24h
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return parseBinanceFuturesVolumeTickers(raw, limit), nil
}

func parseBinanceFuturesVolumeTickers(raw []binanceFuturesTicker24h, limit int) []volumeTicker {
	tickers := make([]volumeTicker, 0, len(raw))
	for _, item := range raw {
		rawSymbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if !strings.HasSuffix(rawSymbol, "USDT") {
			continue
		}
		symbol := normalizeDynamicSymbol(rawSymbol)
		quoteVolume, _ := strconv.ParseFloat(item.QuoteVolume, 64)
		if quoteVolume <= 0 {
			continue
		}
		tickers = append(tickers, volumeTicker{Symbol: symbol, QuoteVolume24hUSD: quoteVolume})
	}
	sort.SliceStable(tickers, func(i, j int) bool {
		return tickers[i].QuoteVolume24hUSD > tickers[j].QuoteVolume24hUSD
	})
	if limit > 0 && len(tickers) > limit {
		tickers = tickers[:limit]
	}
	return tickers
}

func metricsFromMarketData(data *market.Data, quoteVolume float64, performance *logger.PerformanceAnalysis) CandidateMetrics {
	metrics := CandidateMetrics{
		OIValueUSD:        data.OIValueUSD,
		QuoteVolume24hUSD: quoteVolume,
		ADX:               data.CurrentADX,
		PriceChange1h:     data.PriceChange1h,
		PriceChange4h:     data.PriceChange4h,
		BollingerWidth:    data.BollingerWidth,
		FundingRate:       data.FundingRate,
	}
	if data.LongerTermContext != nil {
		metrics.VolumeRatio = data.LongerTermContext.VolumeRatio
	}
	if performance != nil && performance.SymbolStats != nil {
		if stats := performance.SymbolStats[data.Symbol]; stats != nil {
			metrics.WinRate = stats.WinRate
			metrics.TotalPnL = stats.TotalPnL
			metrics.TotalTrades = stats.TotalTrades
		}
	}
	return metrics
}

func hardRejectReason(symbol string, metrics CandidateMetrics, data *market.Data, performance *logger.PerformanceAnalysis, forced bool, cfg DynamicCandidatePoolConfig) string {
	if data.CurrentPrice <= 0 {
		return "当前价格缺失"
	}
	if forced {
		return ""
	}
	if metrics.OIValueUSD > 0 && metrics.OIValueUSD < cfg.MinOIValueUSD {
		return fmt.Sprintf("OI价值过低 %.2fM USD < %.2fM", metrics.OIValueUSD/1_000_000, cfg.MinOIValueUSD/1_000_000)
	}
	if metrics.QuoteVolume24hUSD > 0 && metrics.QuoteVolume24hUSD < cfg.MinQuoteVolume24hUSD {
		return fmt.Sprintf("24h成交额过低 %.2fM USD < %.2fM", metrics.QuoteVolume24hUSD/1_000_000, cfg.MinQuoteVolume24hUSD/1_000_000)
	}
	if math.Abs(metrics.FundingRate) > 0.001 {
		return fmt.Sprintf("资金费率过度拥挤 %.4f%%", metrics.FundingRate*100)
	}
	if metrics.BollingerWidth > dynamicBollingerWidthHighPct {
		return fmt.Sprintf("波动率异常 %.4f", metrics.BollingerWidth)
	}
	if performance != nil && performance.SymbolStats != nil {
		if stats := performance.SymbolStats[symbol]; stats != nil && stats.TotalTrades >= 2 && stats.TotalPnL < 0 && stats.WinRate < 35 {
			return fmt.Sprintf("历史表现冷却: trades=%d win_rate=%.1f%% pnl=%.2f", stats.TotalTrades, stats.WinRate, stats.TotalPnL)
		}
	}
	return ""
}

func scoreDynamicCandidate(symbol string, data *market.Data, metrics CandidateMetrics, performance *logger.PerformanceAnalysis, forced bool, regime string) (float64, []string) {
	liquidity := scoreLiquidity(metrics)
	trend := scoreTrend(data)
	volume := scoreVolume(metrics)
	volatility := scoreVolatility(metrics)
	funding := scoreFunding(metrics)
	history := scoreHistory(symbol, performance)

	score := liquidity*0.25 + trend*0.25 + volume*0.15 + volatility*0.15 + funding*0.10 + history*0.10
	reasons := []string{
		fmt.Sprintf("流动性/OI %.0f", liquidity),
		fmt.Sprintf("趋势 %.0f", trend),
		fmt.Sprintf("成交量 %.0f", volume),
		fmt.Sprintf("波动 %.0f", volatility),
	}

	switch regime {
	case "risk_off", "high_volatility":
		if forced {
			score += 8
			reasons = append(reasons, "风险状态下核心/持仓保留")
		} else {
			score -= 12
			reasons = append(reasons, "风险状态下降低山寨优先级")
		}
	case "trend_up":
		if trend >= 70 {
			score += 6
			reasons = append(reasons, "趋势市提高趋势币权重")
		}
	case "range":
		if volatility >= 70 && funding >= 70 {
			score += 4
			reasons = append(reasons, "震荡市提高稳态候选权重")
		}
	}

	if forced {
		score += 10
		reasons = append(reasons, "核心/持仓强制保留")
	}
	return clamp(score, 0, 100), reasons
}

func scoreLiquidity(metrics CandidateMetrics) float64 {
	oi := normalizeRange(metrics.OIValueUSD, 15_000_000, 150_000_000)
	quote := normalizeRange(metrics.QuoteVolume24hUSD, 20_000_000, 500_000_000)
	if metrics.QuoteVolume24hUSD <= 0 {
		return oi
	}
	if metrics.OIValueUSD <= 0 {
		return quote
	}
	return oi*0.55 + quote*0.45
}

func scoreTrend(data *market.Data) float64 {
	adx := normalizeRange(data.CurrentADX, 15, 45)
	directionBonus := 0.0
	if data.CurrentDIPlus > data.CurrentDIMinus {
		directionBonus += 10
	}
	if data.CurrentEMA20 > 0 && data.CurrentEMA50 > 0 && data.CurrentEMA20 > data.CurrentEMA50 {
		directionBonus += 10
	}
	if data.PriceChange1h > 0 && data.PriceChange4h > 0 {
		directionBonus += 8
	}
	if data.PriceChange1h > 4 || data.PriceChange4h > 12 {
		directionBonus -= 12
	}
	return clamp(adx+directionBonus, 0, 100)
}

func scoreVolume(metrics CandidateMetrics) float64 {
	if metrics.VolumeRatio <= 0 {
		return 55
	}
	if metrics.VolumeRatio < 0.5 {
		return 30
	}
	if metrics.VolumeRatio <= 1.0 {
		return 45 + (metrics.VolumeRatio-0.5)*30
	}
	if metrics.VolumeRatio <= 2.0 {
		return 60 + (metrics.VolumeRatio-1.0)*35
	}
	return 100
}

func scoreVolatility(metrics CandidateMetrics) float64 {
	bw := metrics.BollingerWidth
	if bw <= 0 {
		return 55
	}
	if bw < dynamicBollingerWidthLowPct {
		return 35
	}
	if bw <= dynamicBollingerWidthNormalPct {
		return 90
	}
	if bw <= dynamicBollingerWidthElevatedPct {
		return 70
	}
	if bw <= dynamicBollingerWidthHighPct {
		return 35
	}
	return 10
}

func scoreFunding(metrics CandidateMetrics) float64 {
	absFunding := math.Abs(metrics.FundingRate)
	if absFunding <= 0.0002 {
		return 100
	}
	if absFunding <= 0.0005 {
		return 75
	}
	if absFunding <= 0.001 {
		return 45
	}
	return 0
}

func scoreHistory(symbol string, performance *logger.PerformanceAnalysis) float64 {
	if performance == nil || performance.SymbolStats == nil {
		return 60
	}
	stats := performance.SymbolStats[symbol]
	if stats == nil || stats.TotalTrades == 0 {
		return 60
	}
	score := stats.WinRate
	if stats.TotalPnL > 0 {
		score += 15
	} else if stats.TotalPnL < 0 {
		score -= 20
	}
	if stats.TotalTrades < 3 {
		score = score*0.5 + 30
	}
	return clamp(score, 0, 100)
}

func classifyDynamicTier(symbol string, data *market.Data, metrics CandidateMetrics, coreSet, positionSet map[string]bool) string {
	if positionSet[symbol] {
		return "position"
	}
	if coreSet[symbol] {
		return "core"
	}
	if metrics.VolumeRatio >= 1.5 {
		return "volume_breakout"
	}
	if data.CurrentADX >= 25 {
		return "trend"
	}
	return "defensive"
}

func detectDynamicMarketRegime(btc *market.Data) string {
	regime, _ := detectDynamicMarketRegimeWithDiagnostics(btc)
	return regime
}

func detectDynamicMarketRegimeWithDiagnostics(btc *market.Data) (string, MarketRegimeDiagnostics) {
	diag := MarketRegimeDiagnostics{Regime: "unknown"}
	if btc == nil {
		diag.Reasons = []string{"BTC市场数据缺失"}
		return "unknown", diag
	}
	diag.BTCPriceChange1h = btc.PriceChange1h
	diag.BTCPriceChange4h = btc.PriceChange4h
	diag.BTCADX = btc.CurrentADX
	diag.BTCDIPlus = btc.CurrentDIPlus
	diag.BTCDIMinus = btc.CurrentDIMinus
	diag.BTCEMA20 = btc.CurrentEMA20
	diag.BTCEMA50 = btc.CurrentEMA50
	diag.BTCBollingerWidth = btc.BollingerWidth
	set := func(regime string, reasons ...string) (string, MarketRegimeDiagnostics) {
		diag.Regime = regime
		diag.Reasons = append(diag.Reasons, reasons...)
		return regime, diag
	}
	if btc.PriceChange1h <= -5 || btc.PriceChange4h <= -7 {
		return set("risk_off", "BTC短线跌幅达到risk_off阈值")
	}
	if btc.BollingerWidth >= dynamicBollingerWidthRegimePct {
		return set("high_volatility", "BTC布林带宽度达到高波动阈值")
	}
	if btc.PriceChange1h <= -3 {
		return set("risk_off", "BTC 1h跌幅达到risk_off阈值")
	}
	if btc.CurrentDIPlus < btc.CurrentDIMinus && btc.CurrentADX >= 20 {
		return set("risk_off", "BTC DI-强于DI+且ADX确认趋势")
	}
	if btc.CurrentADX >= 25 && btc.CurrentDIPlus > btc.CurrentDIMinus && btc.CurrentEMA20 >= btc.CurrentEMA50 {
		return set("trend_up", "BTC ADX/DI/EMA确认上行趋势")
	}
	if btc.CurrentADX > 0 && btc.CurrentADX < 20 {
		return set("range", "BTC ADX低于震荡阈值")
	}
	return set("neutral", "BTC未触发明确趋势或风险状态")
}

func scoreDirectionalProfile(symbol string, data, btc *market.Data, metrics CandidateMetrics, cfg DynamicCandidateShortSideCoverageConfig) CandidateSideProfile {
	cfg = normalizeDynamicCandidateShortSideCoverageConfig(cfg)
	if !cfg.Enabled || data == nil {
		return CandidateSideProfile{}
	}
	profile := CandidateSideProfile{Bias: "neutral", ReportOnly: cfg.ReportOnly}
	if btc != nil {
		profile.RelativeWeakness1h = btc.PriceChange1h - data.PriceChange1h
		profile.RelativeWeakness4h = btc.PriceChange4h - data.PriceChange4h
	}
	adxScore := normalizeRange(data.CurrentADX, cfg.MinADX, 45)
	shortScore := adxScore
	longScore := adxScore
	reasons := []string{}
	if data.CurrentDIMinus > data.CurrentDIPlus {
		shortScore += 18
		reasons = append(reasons, "DI-强于DI+")
	} else if data.CurrentDIPlus > data.CurrentDIMinus {
		longScore += 18
	}
	if data.CurrentEMA20 > 0 && data.CurrentEMA50 > 0 {
		if data.CurrentEMA20 < data.CurrentEMA50 {
			shortScore += 14
			reasons = append(reasons, "EMA20低于EMA50")
		} else if data.CurrentEMA20 > data.CurrentEMA50 {
			longScore += 14
		}
	}
	if btc != nil {
		if profile.RelativeWeakness1h >= cfg.MinRelativeWeakness1h {
			shortScore += 10
			reasons = append(reasons, fmt.Sprintf("1h相对BTC更弱%.2f%%", profile.RelativeWeakness1h))
		}
		if profile.RelativeWeakness4h >= cfg.MinRelativeWeakness4h {
			shortScore += 12
			reasons = append(reasons, fmt.Sprintf("4h相对BTC更弱%.2f%%", profile.RelativeWeakness4h))
		}
	}
	if data.PriceChange1h < 0 && data.PriceChange4h < 0 {
		shortScore += 8
		reasons = append(reasons, "1h/4h同步走弱")
	} else if data.PriceChange1h > 0 && data.PriceChange4h > 0 {
		longScore += 8
	}
	if data.CurrentADX >= cfg.MinADX {
		reasons = append(reasons, fmt.Sprintf("ADX %.1f达到方向候选阈值%.1f", data.CurrentADX, cfg.MinADX))
	} else {
		shortScore -= 12
	}
	if math.Abs(metrics.FundingRate) > cfg.MaxAbsFundingRate {
		shortScore -= 18
		reasons = append(reasons, fmt.Sprintf("资金费率拥挤 %.4f%%", metrics.FundingRate*100))
	}
	if metrics.BollingerWidth > dynamicBollingerWidthHighPct {
		shortScore -= 20
		reasons = append(reasons, "波动异常，降低方向候选权重")
	}
	if cfg.RequireBearishDI && data.CurrentDIMinus <= data.CurrentDIPlus {
		shortScore = math.Min(shortScore, 45)
		reasons = append(reasons, "未满足DI空头要求")
	}
	if cfg.RequireBearishEMA && !(data.CurrentEMA20 > 0 && data.CurrentEMA50 > 0 && data.CurrentEMA20 < data.CurrentEMA50) {
		shortScore = math.Min(shortScore, 45)
		reasons = append(reasons, "未满足EMA空头要求")
	}
	profile.ShortScore = clamp(shortScore, 0, 100)
	profile.LongScore = clamp(longScore, 0, 100)
	if profile.ShortScore >= 60 && profile.ShortScore >= profile.LongScore+8 {
		profile.Bias = "short"
	}
	if profile.LongScore >= 60 && profile.LongScore >= profile.ShortScore+8 {
		profile.Bias = "long"
	}
	if profile.Bias == "short" {
		reasons = append([]string{market.Normalize(symbol) + " short-side候选"}, reasons...)
	}
	profile.Reasons = reasons
	return profile
}

func isBTCWeakRegime(regime string, diag MarketRegimeDiagnostics) bool {
	switch strings.ToLower(strings.TrimSpace(regime)) {
	case "risk_off", "high_volatility":
		return true
	}
	if diag.BTCPriceChange1h <= -3 || diag.BTCPriceChange4h <= -5 {
		return true
	}
	return diag.BTCDIMinus > diag.BTCDIPlus && diag.BTCADX >= 20
}

func enforceDynamicPoolBounds(candidates []DynamicCandidate, cfg DynamicCandidatePoolConfig, coreSet, positionSet map[string]bool) []DynamicCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	maxSize := cfg.MaxPoolSize
	if maxSize <= 0 || maxSize > len(candidates) {
		maxSize = len(candidates)
	}
	selected := make([]DynamicCandidate, 0, maxSize)
	used := make(map[string]bool)
	appendIfPresent := func(symbol string) {
		for _, candidate := range candidates {
			if candidate.Symbol == symbol && !used[candidate.Symbol] {
				selected = append(selected, candidate)
				used[candidate.Symbol] = true
				return
			}
		}
	}
	for symbol := range coreSet {
		appendIfPresent(symbol)
	}
	for symbol := range positionSet {
		appendIfPresent(symbol)
	}
	for _, candidate := range candidates {
		if len(selected) >= maxSize {
			break
		}
		if used[candidate.Symbol] {
			continue
		}
		selected = append(selected, candidate)
		used[candidate.Symbol] = true
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Tier == "core" && selected[j].Tier != "core" {
			return true
		}
		if selected[j].Tier == "core" && selected[i].Tier != "core" {
			return false
		}
		return selected[i].Score > selected[j].Score
	})
	return selected
}

func selectPromptCandidates(snapshot *DynamicCandidatePool, positionSymbols []string, cfg DynamicCandidatePoolConfig) []string {
	if snapshot == nil {
		return nil
	}
	if cfg.ShortSideCoverage.Enabled && !cfg.ShortSideCoverage.ReportOnly && isBTCWeakRegime(snapshot.MarketRegime, snapshot.RegimeDiagnostics) {
		return selectPromptCandidatesWithCoverage(snapshot, positionSymbols, cfg)
	}
	limit := cfg.PromptCandidateLimit
	if limit <= 0 {
		limit = len(snapshot.Symbols)
	}
	selected := make([]string, 0, limit+len(positionSymbols))
	used := make(map[string]bool)

	add := func(symbol string) {
		symbol = normalizeDynamicSymbol(symbol)
		if symbol == "" || used[symbol] {
			return
		}
		selected = append(selected, symbol)
		used[symbol] = true
	}

	for _, core := range cfg.CoreSymbols {
		add(core)
	}
	for _, symbol := range positionSymbols {
		add(symbol)
	}
	for _, candidate := range snapshot.Symbols {
		if len(selected) >= limit+len(positionSymbols) {
			break
		}
		add(candidate.Symbol)
	}
	return selected
}

func selectPromptCandidatesWithCoverage(snapshot *DynamicCandidatePool, positionSymbols []string, cfg DynamicCandidatePoolConfig) []string {
	if snapshot == nil {
		return nil
	}
	limit := cfg.PromptCandidateLimit
	if limit <= 0 {
		limit = len(snapshot.Symbols)
	}
	selected := make([]string, 0, limit+len(positionSymbols))
	used := make(map[string]bool)
	add := func(symbol string) {
		symbol = normalizeDynamicSymbol(symbol)
		if symbol == "" || used[symbol] {
			return
		}
		selected = append(selected, symbol)
		used[symbol] = true
	}
	for _, core := range cfg.CoreSymbols {
		add(core)
	}
	for _, symbol := range positionSymbols {
		add(symbol)
	}
	maxShort := int(math.Ceil(float64(limit) * cfg.ShortSideCoverage.MaxPromptRatio))
	if maxShort <= 0 {
		maxShort = 1
	}
	minShort := cfg.ShortSideCoverage.MinPromptCount
	if minShort > maxShort {
		minShort = maxShort
	}
	shortAdded := 0
	for _, candidate := range snapshot.Symbols {
		if len(selected) >= limit+len(positionSymbols) || shortAdded >= minShort {
			break
		}
		if used[candidate.Symbol] || candidate.SideProfile.Bias != "short" {
			continue
		}
		add(candidate.Symbol)
		shortAdded++
	}
	for _, candidate := range snapshot.Symbols {
		if len(selected) >= limit+len(positionSymbols) {
			break
		}
		add(candidate.Symbol)
	}
	return selected
}

func buildShortSideSummary(candidates []DynamicCandidate, positionSymbols []string, cfg DynamicCandidatePoolConfig, regime string, diag MarketRegimeDiagnostics) ShortSideSummary {
	summary := ShortSideSummary{
		Enabled:        cfg.ShortSideCoverage.Enabled,
		ReportOnly:     cfg.ShortSideCoverage.ReportOnly,
		BTCWeak:        isBTCWeakRegime(regime, diag),
		MinPromptCount: cfg.ShortSideCoverage.MinPromptCount,
		MaxPromptRatio: cfg.ShortSideCoverage.MaxPromptRatio,
	}
	if !cfg.ShortSideCoverage.Enabled {
		return summary
	}
	for _, candidate := range candidates {
		if candidate.SideProfile.Bias != "short" {
			continue
		}
		summary.CandidateCount++
		summary.Symbols = append(summary.Symbols, candidate.Symbol)
		if len(candidate.SideProfile.Reasons) > 0 && len(summary.Reasons) < 8 {
			summary.Reasons = append(summary.Reasons, fmt.Sprintf("%s: %s", candidate.Symbol, strings.Join(candidate.SideProfile.Reasons, ", ")))
		}
	}
	selected := selectPromptCandidates(&DynamicCandidatePool{
		MarketRegime:      regime,
		RegimeDiagnostics: diag,
		Symbols:           candidates,
	}, positionSymbols, cfg)
	selectedSet := makeStringSet(selected)
	for _, candidate := range candidates {
		if candidate.SideProfile.Bias == "short" && selectedSet[candidate.Symbol] {
			summary.PromptCount++
		}
	}
	return summary
}

func mergedPoolFromDynamicSnapshot(snapshot *DynamicCandidatePool, selected []string, cfg DynamicCandidatePoolConfig, positionSymbols []string) *MergedCoinPool {
	detailMap := make(map[string]DynamicCandidate, len(snapshot.Symbols))
	for _, candidate := range snapshot.Symbols {
		detailMap[candidate.Symbol] = candidate
	}
	sources := make(map[string][]string, len(selected))
	dynamicDetails := make(map[string]DynamicCandidate, len(selected))
	coreSet := makeStringSet(cfg.CoreSymbols)
	positionSet := makeStringSet(positionSymbols)
	for _, symbol := range selected {
		if detail, ok := detailMap[symbol]; ok {
			sources[symbol] = append([]string(nil), detail.Sources...)
			dynamicDetails[symbol] = detail
			continue
		}
		if coreSet[symbol] {
			sources[symbol] = []string{"core"}
			dynamicDetails[symbol] = DynamicCandidate{
				Symbol:  symbol,
				Tier:    "core",
				Sources: []string{"core"},
				Reasons: []string{"核心币强制纳入上下文"},
			}
			continue
		}
		if positionSet[symbol] {
			sources[symbol] = []string{"position"}
			dynamicDetails[symbol] = DynamicCandidate{
				Symbol:  symbol,
				Tier:    "position",
				Sources: []string{"position"},
				Reasons: []string{"当前持仓强制纳入上下文"},
			}
		}
	}
	return &MergedCoinPool{
		AllSymbols:        selected,
		SymbolSources:     sources,
		DynamicCandidates: dynamicDetails,
		MarketRegime:      snapshot.MarketRegime,
		RegimeDiagnostics: snapshot.RegimeDiagnostics,
		ShortSideSummary:  snapshot.ShortSideSummary,
	}
}

func logDynamicPoolDiff(previous, current *DynamicCandidatePool) {
	if current == nil {
		return
	}
	prevSet := make(map[string]bool)
	if previous != nil {
		for _, candidate := range previous.Symbols {
			prevSet[candidate.Symbol] = true
		}
	}
	var added []string
	for _, candidate := range current.Symbols {
		if !prevSet[candidate.Symbol] {
			added = append(added, candidate.Symbol)
		}
	}
	log.Printf("📊 动态候选池刷新完成: regime=%s, 候选=%d, 剔除=%d, 新增=%v",
		current.MarketRegime, len(current.Symbols), len(current.Removed), added)
	for i, candidate := range current.Symbols {
		if i >= 10 {
			break
		}
		log.Printf("  #%d %s tier=%s score=%.1f sources=%v reasons=%v",
			i+1, candidate.Symbol, candidate.Tier, candidate.Score, candidate.Sources, candidate.Reasons)
	}
}

func sortedSourceList(sourceSet map[string]bool) []string {
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func normalizeSymbolList(symbols []string) []string {
	result := make([]string, 0, len(symbols))
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		normalized := normalizeDynamicSymbol(symbol)
		if normalized == "" || seen[normalized] {
			continue
		}
		result = append(result, normalized)
		seen[normalized] = true
	}
	return result
}

func makeStringSet(symbols []string) map[string]bool {
	set := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		normalized := normalizeDynamicSymbol(symbol)
		if normalized != "" {
			set[normalized] = true
		}
	}
	return set
}

func normalizeDynamicSymbol(symbol string) string {
	if strings.TrimSpace(symbol) == "" {
		return ""
	}
	return normalizeSymbol(symbol)
}

func normalizeRange(value, min, max float64) float64 {
	if value <= min {
		return 0
	}
	if value >= max {
		return 100
	}
	return (value - min) / (max - min) * 100
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
