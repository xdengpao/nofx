package backtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"nofx/config"
	"nofx/decision"
	"nofx/market"
)

const (
	DefaultTimezone            = "Asia/Singapore"
	DefaultHistoryDBPath       = "backtest_data/nofx_history.sqlite"
	DefaultOutputDir           = "backtest_runs"
	DefaultSource              = "binance-futures"
	DefaultScanIntervalMinutes = 3
	DefaultInitialEquity       = 10_000
	DefaultTakerFeeBPS         = 5
	DefaultMakerFeeBPS         = 2
	DefaultSlippageBPS         = 3
	DefaultFundingMode         = "disabled"
	DefaultLiquidationMode     = "not_modelled"
	DefaultMarketOrderFill     = "next_3m_open"
	DefaultSameBarConflict     = "worst_case"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,30}USDT$`)

type BacktestConfig struct {
	BacktestFrom        string          `json:"backtest_from"`
	BacktestTo          string          `json:"backtest_to"`
	Timezone            string          `json:"timezone,omitempty"`
	HistoryDB           string          `json:"history_db,omitempty"`
	OutputDir           string          `json:"output_dir,omitempty"`
	Source              string          `json:"source,omitempty"`
	Exchange            string          `json:"exchange,omitempty"`
	Symbols             []string        `json:"symbols,omitempty"`
	InitialEquity       float64         `json:"initial_equity,omitempty"`
	ScanIntervalMinutes int             `json:"scan_interval_minutes,omitempty"`
	Costs               CostConfig      `json:"costs,omitempty"`
	Execution           ExecutionConfig `json:"execution,omitempty"`
	Data                DataConfig      `json:"data,omitempty"`
	Strategy            StrategyConfig  `json:"strategy,omitempty"`

	loc              *time.Location
	backtestFromTime time.Time
	backtestToTime   time.Time
	warmupFromTime   time.Time
	programmatic     decision.ProgrammaticStrategyPolicy
	configHash       string
}

type CostConfig struct {
	TakerFeeBPS float64 `json:"taker_fee_bps,omitempty"`
	MakerFeeBPS float64 `json:"maker_fee_bps,omitempty"`
	SlippageBPS float64 `json:"slippage_bps,omitempty"`
}

type ExecutionConfig struct {
	MarketOrderFill string `json:"market_order_fill,omitempty"`
	SameBarConflict string `json:"same_bar_conflict,omitempty"`
	FundingMode     string `json:"funding_mode,omitempty"`
	LiquidationMode string `json:"liquidation_mode,omitempty"`
}

type DataConfig struct {
	DataFrom            string           `json:"data_from,omitempty"`
	DataTo              string           `json:"data_to,omitempty"`
	Timeframes          []string         `json:"timeframes,omitempty"`
	AllowAutoFetch      bool             `json:"allow_auto_fetch,omitempty"`
	RateLimit           RateLimitProfile `json:"rate_limit,omitempty"`
	CandidatePoolMode   string           `json:"candidate_pool_mode,omitempty"`
	CandidatePoolFile   string           `json:"candidate_pool_file,omitempty"`
	DefaultLookbackDays int              `json:"default_lookback_days,omitempty"`
}

type StrategyConfig struct {
	DecisionMode         string                            `json:"decision_mode,omitempty"`
	ProgrammaticStrategy config.ProgrammaticStrategyConfig `json:"programmatic_strategy,omitempty"`
}

type BatchConfig struct {
	BaseConfig string                   `json:"base_config,omitempty"`
	Configs    []string                 `json:"configs,omitempty"`
	Matrix     map[string][]interface{} `json:"matrix,omitempty"`
	FailFast   bool                     `json:"fail_fast,omitempty"`
	OutputDir  string                   `json:"output_dir,omitempty"`
}

type RateLimitProfile struct {
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	Concurrency       int `json:"concurrency,omitempty"`
	PageLimit         int `json:"page_limit,omitempty"`
	MaxRetries        int `json:"max_retries,omitempty"`
	InitialBackoffMS  int `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS      int `json:"max_backoff_ms,omitempty"`
}

type HistoryCoverageChecker interface {
	HasKlineCoverage(ctx context.Context, source, symbol, timeframe string, from, to time.Time) (bool, string, error)
}

func LoadConfig(path string) (*BacktestConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取回测配置失败: %w", err)
	}
	var cfg BacktestConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析回测配置失败: %w", err)
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *BacktestConfig) NormalizeAndValidate() error {
	if c == nil {
		return fmt.Errorf("回测配置为空")
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = DefaultTimezone
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("timezone无效: %s", c.Timezone)
	}
	c.loc = loc

	from, err := ParseConfigTime(c.BacktestFrom, loc)
	if err != nil {
		return fmt.Errorf("backtest_from无效: %w", err)
	}
	to, err := ParseConfigTime(c.BacktestTo, loc)
	if err != nil {
		return fmt.Errorf("backtest_to无效: %w", err)
	}
	if !from.Before(to) {
		return fmt.Errorf("backtest_from必须早于backtest_to，回测区间采用[from,to)")
	}
	c.backtestFromTime = from
	c.backtestToTime = to

	if strings.TrimSpace(c.HistoryDB) == "" {
		c.HistoryDB = DefaultHistoryDBPath
	}
	if strings.TrimSpace(c.OutputDir) == "" {
		c.OutputDir = DefaultOutputDir
	}
	if strings.TrimSpace(c.Source) == "" {
		c.Source = DefaultSource
	}
	if strings.TrimSpace(c.Exchange) == "" {
		c.Exchange = defaultExchangeForSource(c.Source)
	}
	if c.InitialEquity <= 0 {
		c.InitialEquity = DefaultInitialEquity
	}
	if c.ScanIntervalMinutes <= 0 {
		c.ScanIntervalMinutes = DefaultScanIntervalMinutes
	}
	if c.ScanIntervalMinutes < 1 || c.ScanIntervalMinutes > 240 {
		return fmt.Errorf("scan_interval_minutes必须在1-240之间: %d", c.ScanIntervalMinutes)
	}
	if len(c.Symbols) == 0 {
		c.Symbols = append([]string(nil), c.Strategy.ProgrammaticStrategy.SymbolPool.Symbols...)
	}
	c.Symbols = normalizeSymbols(c.Symbols)
	if len(c.Symbols) == 0 {
		return fmt.Errorf("symbols不能为空，回测默认不使用动态线上选币")
	}
	for _, symbol := range c.Symbols {
		if !symbolPattern.MatchString(symbol) {
			return fmt.Errorf("symbol格式无效: %s", symbol)
		}
	}

	c.Costs = normalizeCostConfig(c.Costs)
	if err := validateCostConfig(c.Costs); err != nil {
		return err
	}
	c.Execution = normalizeExecutionConfig(c.Execution)
	if err := validateExecutionConfig(c.Execution); err != nil {
		return err
	}
	c.Data = normalizeDataConfig(c.Data)
	if err := c.validateDataConfig(); err != nil {
		return err
	}

	policy, err := normalizeProgrammaticPolicy(c.Strategy)
	if err != nil {
		return err
	}
	c.programmatic = policy
	if c.programmatic.State.Path == "" {
		c.programmatic.State.Path = "state/programmatic_strategy_state.json"
	}
	c.warmupFromTime = c.calculateWarmupFrom()
	hash, err := hashConfigSnapshot(c.sanitizedSnapshot())
	if err != nil {
		return err
	}
	c.configHash = hash
	if c.programmatic.ConfigHash == "" || c.programmatic.ConfigHash == "default" {
		c.programmatic.ConfigHash = hash
	}
	return nil
}

func ParseConfigTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("时间不能为空")
	}
	if loc == nil {
		loc = time.Local
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.In(loc), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", value, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("不支持的时间格式: %s", value)
}

func (c *BacktestConfig) BacktestFromTime() time.Time { return c.backtestFromTime }
func (c *BacktestConfig) BacktestToTime() time.Time   { return c.backtestToTime }
func (c *BacktestConfig) WarmupFromTime() time.Time   { return c.warmupFromTime }
func (c *BacktestConfig) Location() *time.Location    { return c.loc }
func (c *BacktestConfig) ProgrammaticPolicy() decision.ProgrammaticStrategyPolicy {
	return c.programmatic
}
func (c *BacktestConfig) ConfigHash() string { return c.configHash }

func (c *BacktestConfig) ValidateHistoryCoverage(ctx context.Context, checker HistoryCoverageChecker) error {
	if checker == nil {
		return nil
	}
	for _, symbol := range c.Symbols {
		for _, timeframe := range RequiredTimeframes() {
			ok, detail, err := checker.HasKlineCoverage(ctx, c.Source, symbol, timeframe, c.warmupFromTime, c.backtestToTime)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%s %s 历史数据覆盖不足: %s", symbol, timeframe, detail)
			}
		}
	}
	return nil
}

func RequiredTimeframes() []string {
	return []string{"3m", "15m", "1h", "4h"}
}

func (c *BacktestConfig) calculateWarmupFrom() time.Time {
	depth := c.programmatic.HistoryDepth
	maxDuration := time.Duration(depth.M3) * 3 * time.Minute
	candidates := []time.Duration{
		time.Duration(depth.M15) * 15 * time.Minute,
		time.Duration(depth.H1) * time.Hour,
		time.Duration(depth.H4) * 4 * time.Hour,
		14 * 4 * time.Hour * 3,
		60 * 24 * time.Hour,
	}
	for _, candidate := range candidates {
		if candidate > maxDuration {
			maxDuration = candidate
		}
	}
	return c.backtestFromTime.Add(-maxDuration)
}

func normalizeSymbols(symbols []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := market.Normalize(symbol)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeCostConfig(cfg CostConfig) CostConfig {
	if cfg.TakerFeeBPS == 0 {
		cfg.TakerFeeBPS = DefaultTakerFeeBPS
	}
	if cfg.MakerFeeBPS == 0 {
		cfg.MakerFeeBPS = DefaultMakerFeeBPS
	}
	if cfg.SlippageBPS == 0 {
		cfg.SlippageBPS = DefaultSlippageBPS
	}
	return cfg
}

func validateCostConfig(cfg CostConfig) error {
	if cfg.TakerFeeBPS < 0 || cfg.MakerFeeBPS < 0 || cfg.SlippageBPS < 0 {
		return fmt.Errorf("手续费和滑点不能为负数")
	}
	if cfg.TakerFeeBPS > 100 || cfg.MakerFeeBPS > 100 || cfg.SlippageBPS > 500 {
		return fmt.Errorf("手续费或滑点配置过大")
	}
	return nil
}

func normalizeExecutionConfig(cfg ExecutionConfig) ExecutionConfig {
	if cfg.MarketOrderFill == "" {
		cfg.MarketOrderFill = DefaultMarketOrderFill
	}
	if cfg.SameBarConflict == "" {
		cfg.SameBarConflict = DefaultSameBarConflict
	}
	if cfg.FundingMode == "" {
		cfg.FundingMode = DefaultFundingMode
	}
	if cfg.LiquidationMode == "" {
		cfg.LiquidationMode = DefaultLiquidationMode
	}
	return cfg
}

func validateExecutionConfig(cfg ExecutionConfig) error {
	if cfg.MarketOrderFill != DefaultMarketOrderFill {
		return fmt.Errorf("market_order_fill首期仅支持%s", DefaultMarketOrderFill)
	}
	if cfg.SameBarConflict != DefaultSameBarConflict {
		return fmt.Errorf("same_bar_conflict首期仅支持%s", DefaultSameBarConflict)
	}
	if cfg.FundingMode != DefaultFundingMode {
		return fmt.Errorf("funding_mode首期仅支持disabled")
	}
	if cfg.LiquidationMode != DefaultLiquidationMode {
		return fmt.Errorf("liquidation_mode首期仅支持not_modelled")
	}
	return nil
}

func normalizeDataConfig(cfg DataConfig) DataConfig {
	if len(cfg.Timeframes) == 0 {
		cfg.Timeframes = RequiredTimeframes()
	}
	cfg.Timeframes = normalizeTimeframes(cfg.Timeframes)
	cfg.RateLimit = NormalizeRateLimitProfile(cfg.RateLimit)
	return cfg
}

func normalizeTimeframes(values []string) []string {
	allowed := map[string]bool{"3m": true, "15m": true, "1h": true, "4h": true}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if !allowed[value] || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (c *BacktestConfig) validateDataConfig() error {
	if len(c.Data.Timeframes) == 0 {
		return fmt.Errorf("data.timeframes不能为空")
	}
	if strings.TrimSpace(c.Data.DataFrom) != "" {
		from, err := ParseConfigTime(c.Data.DataFrom, c.loc)
		if err != nil {
			return fmt.Errorf("data_from无效: %w", err)
		}
		to := time.Time{}
		if strings.TrimSpace(c.Data.DataTo) != "" {
			to, err = ParseConfigTime(c.Data.DataTo, c.loc)
			if err != nil {
				return fmt.Errorf("data_to无效: %w", err)
			}
		}
		if !to.IsZero() && !from.Before(to) {
			return fmt.Errorf("data_from必须早于data_to")
		}
	} else if strings.TrimSpace(c.Data.DataTo) != "" && c.Data.DefaultLookbackDays <= 0 {
		return fmt.Errorf("只配置data_to时必须配置data.default_lookback_days")
	}
	return nil
}

func NormalizeRateLimitProfile(profile RateLimitProfile) RateLimitProfile {
	if profile.RequestsPerMinute <= 0 {
		profile.RequestsPerMinute = 120
	}
	if profile.Concurrency <= 0 {
		profile.Concurrency = 1
	}
	if profile.PageLimit <= 0 {
		profile.PageLimit = 1000
	}
	if profile.MaxRetries <= 0 {
		profile.MaxRetries = 3
	}
	if profile.InitialBackoffMS <= 0 {
		profile.InitialBackoffMS = 500
	}
	if profile.MaxBackoffMS <= 0 {
		profile.MaxBackoffMS = 5000
	}
	return profile
}

func normalizeProgrammaticPolicy(strategy StrategyConfig) (decision.ProgrammaticStrategyPolicy, error) {
	mode := strings.TrimSpace(strategy.DecisionMode)
	if mode == "" {
		mode = config.DecisionModeProgrammatic
	}
	if mode != config.DecisionModeProgrammatic {
		return decision.ProgrammaticStrategyPolicy{}, fmt.Errorf("回测首期仅支持programmatic策略")
	}
	temp := &config.Config{Traders: []config.TraderConfig{{
		ID:                   "backtest",
		Name:                 "Backtest",
		Enabled:              true,
		DecisionMode:         config.DecisionModeProgrammatic,
		ProgrammaticStrategy: strategy.ProgrammaticStrategy,
	}}}
	profiles, err := temp.NormalizeProgrammaticStrategies()
	if err != nil {
		return decision.ProgrammaticStrategyPolicy{}, err
	}
	return programmaticProfileToPolicy(profiles["backtest"]), nil
}

func programmaticProfileToPolicy(profile config.ProgrammaticStrategyProfile) decision.ProgrammaticStrategyPolicy {
	return decision.ProgrammaticStrategyPolicy{
		DecisionMode:    profile.DecisionMode,
		StrategyName:    profile.StrategyName,
		StrategyVersion: profile.StrategyVersion,
		ConfigHash:      profile.ConfigHash,
		AllowLong:       profile.AllowLong,
		AllowShort:      profile.AllowShort,
		EnabledSignals:  append([]string(nil), profile.EnabledSignals...),
		Timeframes: decision.ProgrammaticTimeframesPolicy{
			Higher: profile.Timeframes.Higher,
			Trade:  profile.Timeframes.Trade,
			Sub:    profile.Timeframes.Sub,
			Micro:  profile.Timeframes.Micro,
		},
		HistoryDepth: decision.ProgrammaticHistoryDepth{
			M3:  profile.HistoryDepth.M3,
			M15: profile.HistoryDepth.M15,
			H1:  profile.HistoryDepth.H1,
			H4:  profile.HistoryDepth.H4,
		},
		SymbolPool: decision.ProgrammaticSymbolPoolPolicy{
			Mode:        profile.SymbolPool.Mode,
			Symbols:     append([]string(nil), profile.SymbolPool.Symbols...),
			CoreSymbols: append([]string(nil), profile.SymbolPool.CoreSymbols...),
		},
		MovingAverage: decision.ProgrammaticMAPolicy{
			ShortPeriod:     profile.MovingAverage.ShortPeriod,
			LongPeriod:      profile.MovingAverage.LongPeriod,
			KissDistancePct: profile.MovingAverage.KissDistancePct,
			WetKissBars:     profile.MovingAverage.WetKissBars,
		},
		Structure: decision.ProgrammaticStructurePolicy{
			Strictness:    profile.Structure.Strictness,
			LeftBars:      profile.Structure.LeftBars,
			RightBars:     profile.Structure.RightBars,
			MinStrokeBars: profile.Structure.MinStrokeBars,
			MinSwingPct:   profile.Structure.MinSwingPct,
			ATRMultiplier: profile.Structure.ATRMultiplier,
			Bootstrap:     profile.Structure.Bootstrap,
		},
		Divergence: decision.ProgrammaticDivergencePolicy{
			Ratio:                       profile.Divergence.Ratio,
			PriceTolerancePct:           profile.Divergence.PriceTolerancePct,
			PriceToleranceATRMultiplier: profile.Divergence.PriceToleranceATRMultiplier,
			RequireBZeroAxis:            profile.Divergence.RequireBZeroAxis,
		},
		ADX: decision.ProgrammaticADXPolicy{
			Period:         profile.ADX.Period,
			MinADX:         profile.ADX.MinADX,
			MicroADXFilter: profile.ADX.MicroADXFilter,
		},
		Position: decision.ProgrammaticPositionPolicy{
			MaxAddCount:       profile.Position.MaxAddCount,
			AddSizeMultiplier: profile.Position.AddSizeMultiplier,
			PartialClosePct:   profile.Position.PartialClosePct,
			AllowReversal:     profile.Position.AllowReversal,
		},
		PositionManagement: decision.ProgrammaticPositionManagementPolicy{
			Enabled: profile.PositionManagement.Enabled,
			Timeframes: decision.ProgrammaticManagementTFPolicy{
				Structure: profile.PositionManagement.Timeframes.Structure,
				Micro:     profile.PositionManagement.Timeframes.Micro,
			},
			Breakeven: decision.ProgrammaticBreakevenPolicy{
				Enabled:          profile.PositionManagement.Breakeven.Enabled,
				TriggerProfitPct: profile.PositionManagement.Breakeven.TriggerProfitPct,
				TriggerR:         profile.PositionManagement.Breakeven.TriggerR,
				BufferRatio:      profile.PositionManagement.Breakeven.BufferRatio,
			},
			FloatingDrawdown: decision.ProgrammaticFloatingDrawdownPolicy{
				Enabled:             profile.PositionManagement.FloatingDrawdown.Enabled,
				ActivationProfitPct: profile.PositionManagement.FloatingDrawdown.ActivationProfitPct,
				ActivationR:         profile.PositionManagement.FloatingDrawdown.ActivationR,
				DrawdownRatio:       profile.PositionManagement.FloatingDrawdown.DrawdownRatio,
				Action:              profile.PositionManagement.FloatingDrawdown.Action,
			},
			StructureBreak: decision.ProgrammaticStructureBreakPolicy{
				Enabled:                 profile.PositionManagement.StructureBreak.Enabled,
				ConfirmBars:             profile.PositionManagement.StructureBreak.ConfirmBars,
				Action:                  profile.PositionManagement.StructureBreak.Action,
				PartialCloseGuardAction: profile.PositionManagement.StructureBreak.PartialCloseGuardAction,
			},
			ShortTrade: decision.ProgrammaticShortTradePolicy{
				Enabled:         profile.PositionManagement.ShortTrade.Enabled,
				PartialClosePct: profile.PositionManagement.ShortTrade.PartialClosePct,
			},
			PartialCloseGuard: decision.ProgrammaticPartialCloseGuardPolicy{
				CooldownMinutes:     profile.PositionManagement.PartialCloseGuard.CooldownMinutes,
				MaxCountPerPosition: profile.PositionManagement.PartialCloseGuard.MaxCountPerPosition,
				MaxTotalRatio:       profile.PositionManagement.PartialCloseGuard.MaxTotalRatio,
				CooldownEnabled:     profile.PositionManagement.PartialCloseGuard.CooldownEnabled,
			},
		},
		TakeProfit: decision.ProgrammaticTPPolicy{
			Mode:         profile.TakeProfit.Mode,
			FallbackMode: profile.TakeProfit.FallbackMode,
			MinNetRR:     profile.TakeProfit.MinNetRR,
		},
		State: decision.ProgrammaticStatePolicy{
			Path:      profile.State.Path,
			Bootstrap: profile.State.Bootstrap,
		},
	}
}

func (c *BacktestConfig) sanitizedSnapshot() map[string]any {
	return map[string]any{
		"backtest_from":         c.BacktestFrom,
		"backtest_to":           c.BacktestTo,
		"timezone":              c.Timezone,
		"history_db":            sanitizePath(c.HistoryDB),
		"output_dir":            sanitizePath(c.OutputDir),
		"source":                c.Source,
		"exchange":              c.Exchange,
		"symbols":               append([]string(nil), c.Symbols...),
		"initial_equity":        c.InitialEquity,
		"scan_interval_minutes": c.ScanIntervalMinutes,
		"costs":                 c.Costs,
		"execution":             c.Execution,
		"data": map[string]any{
			"timeframes":          append([]string(nil), c.Data.Timeframes...),
			"candidate_pool_mode": c.Data.CandidatePoolMode,
			"allow_auto_fetch":    c.Data.AllowAutoFetch,
		},
		"strategy": map[string]any{
			"decision_mode": c.programmatic.DecisionMode,
			"name":          c.programmatic.StrategyName,
			"version":       c.programmatic.StrategyVersion,
			"timeframes":    c.programmatic.Timeframes,
			"history_depth": c.programmatic.HistoryDepth,
		},
	}
}

func (c *BacktestConfig) SanitizedSnapshot() map[string]any {
	return c.sanitizedSnapshot()
}

func sanitizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(path), "key") || strings.Contains(strings.ToLower(path), "secret") {
		return "[redacted]"
	}
	return path
}

func defaultExchangeForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "binance", "binance-futures":
		return "binance"
	case "aster", "aster-dex":
		return "aster"
	case "hyperliquid":
		return "hyperliquid"
	default:
		return strings.ToLower(strings.TrimSpace(source))
	}
}

func hashConfigSnapshot(snapshot map[string]any) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12], nil
}
