package config

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	DecisionModeAI           = "ai"
	DecisionModeProgrammatic = "programmatic"

	defaultProgrammaticStrategyName    = "chanlun_programmatic"
	defaultProgrammaticStrategyVersion = "v1"
	defaultProgrammaticStatePath       = "data/programmatic_strategy_state.json"
)

var programmaticSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,30}USDT$`)

type ProgrammaticStrategyConfig struct {
	StrategyName    string                       `json:"strategy_name,omitempty"`
	StrategyVersion string                       `json:"strategy_version,omitempty"`
	AllowLong       *bool                        `json:"allow_long,omitempty"`
	AllowShort      *bool                        `json:"allow_short,omitempty"`
	EnabledSignals  []string                     `json:"enabled_signals,omitempty"`
	Timeframes      ProgrammaticTimeframesConfig `json:"timeframes,omitempty"`
	HistoryDepth    ProgrammaticHistoryDepth     `json:"history_depth,omitempty"`
	SymbolPool      ProgrammaticSymbolPoolConfig `json:"symbol_pool,omitempty"`
	MovingAverage   ProgrammaticMAConfig         `json:"moving_average,omitempty"`
	Structure       ProgrammaticStructureConfig  `json:"structure,omitempty"`
	Divergence      ProgrammaticDivergenceConfig `json:"divergence,omitempty"`
	ADX             ProgrammaticADXConfig        `json:"adx,omitempty"`
	Position        ProgrammaticPositionConfig   `json:"position,omitempty"`
	TakeProfit      ProgrammaticTPConfig         `json:"take_profit,omitempty"`
	State           ProgrammaticStateConfig      `json:"state,omitempty"`
}

type ProgrammaticTimeframesConfig struct {
	Higher string `json:"higher,omitempty"`
	Trade  string `json:"trade,omitempty"`
	Sub    string `json:"sub,omitempty"`
	Micro  string `json:"micro,omitempty"`
}

type ProgrammaticHistoryDepth struct {
	M3  int `json:"3m,omitempty"`
	M15 int `json:"15m,omitempty"`
	H1  int `json:"1h,omitempty"`
	H4  int `json:"4h,omitempty"`
}

type ProgrammaticSymbolPoolConfig struct {
	Mode        string   `json:"mode,omitempty"`
	Symbols     []string `json:"symbols,omitempty"`
	CoreSymbols []string `json:"core_symbols,omitempty"`
}

type ProgrammaticMAConfig struct {
	ShortPeriod     int     `json:"short_period,omitempty"`
	LongPeriod      int     `json:"long_period,omitempty"`
	KissDistancePct float64 `json:"kiss_distance_pct,omitempty"`
	WetKissBars     int     `json:"wet_kiss_bars,omitempty"`
}

type ProgrammaticStructureConfig struct {
	Strictness    string  `json:"strictness,omitempty"`
	LeftBars      int     `json:"left_bars,omitempty"`
	RightBars     int     `json:"right_bars,omitempty"`
	MinStrokeBars int     `json:"min_stroke_bars,omitempty"`
	MinSwingPct   float64 `json:"min_swing_pct,omitempty"`
	ATRMultiplier float64 `json:"atr_multiplier,omitempty"`
	Bootstrap     bool    `json:"bootstrap,omitempty"`
}

type ProgrammaticDivergenceConfig struct {
	Ratio                       float64 `json:"ratio,omitempty"`
	PriceTolerancePct           float64 `json:"price_tolerance_pct,omitempty"`
	PriceToleranceATRMultiplier float64 `json:"price_tolerance_atr_multiplier,omitempty"`
	RequireBZeroAxis            bool    `json:"require_b_zero_axis,omitempty"`
}

type ProgrammaticADXConfig struct {
	Period         int     `json:"period,omitempty"`
	MinADX         float64 `json:"min_adx,omitempty"`
	MicroADXFilter bool    `json:"micro_adx_filter,omitempty"`
}

type ProgrammaticPositionConfig struct {
	MaxAddCount       int     `json:"max_add_count,omitempty"`
	AddSizeMultiplier float64 `json:"add_size_multiplier,omitempty"`
	PartialClosePct   float64 `json:"partial_close_pct,omitempty"`
	AllowReversal     bool    `json:"allow_reversal,omitempty"`
}

type ProgrammaticTPConfig struct {
	Mode         string  `json:"mode,omitempty"`
	FallbackMode string  `json:"fallback_mode,omitempty"`
	MinNetRR     float64 `json:"min_net_rr,omitempty"`
}

type ProgrammaticStateConfig struct {
	Path      string `json:"path,omitempty"`
	Bootstrap bool   `json:"bootstrap,omitempty"`
}

type ProgrammaticStrategyProfile struct {
	DecisionMode    string
	StrategyName    string
	StrategyVersion string
	ConfigHash      string
	AllowLong       bool
	AllowShort      bool
	EnabledSignals  []string
	Timeframes      ProgrammaticTimeframesProfile
	HistoryDepth    ProgrammaticHistoryDepth
	SymbolPool      ProgrammaticSymbolPoolProfile
	MovingAverage   ProgrammaticMAProfile
	Structure       ProgrammaticStructureProfile
	Divergence      ProgrammaticDivergenceProfile
	ADX             ProgrammaticADXProfile
	Position        ProgrammaticPositionProfile
	TakeProfit      ProgrammaticTPProfile
	State           ProgrammaticStateProfile
}

type ProgrammaticTimeframesProfile struct {
	Higher string
	Trade  string
	Sub    string
	Micro  string
}

type ProgrammaticSymbolPoolProfile struct {
	Mode        string
	Symbols     []string
	CoreSymbols []string
}

type ProgrammaticMAProfile struct {
	ShortPeriod     int
	LongPeriod      int
	KissDistancePct float64
	WetKissBars     int
}

type ProgrammaticStructureProfile struct {
	Strictness    string
	LeftBars      int
	RightBars     int
	MinStrokeBars int
	MinSwingPct   float64
	ATRMultiplier float64
	Bootstrap     bool
}

type ProgrammaticDivergenceProfile struct {
	Ratio                       float64
	PriceTolerancePct           float64
	PriceToleranceATRMultiplier float64
	RequireBZeroAxis            bool
}

type ProgrammaticADXProfile struct {
	Period         int
	MinADX         float64
	MicroADXFilter bool
}

type ProgrammaticPositionProfile struct {
	MaxAddCount       int
	AddSizeMultiplier float64
	PartialClosePct   float64
	AllowReversal     bool
}

type ProgrammaticTPProfile struct {
	Mode         string
	FallbackMode string
	MinNetRR     float64
}

type ProgrammaticStateProfile struct {
	Path      string
	Bootstrap bool
}

func (c *Config) NormalizeProgrammaticStrategies() (map[string]ProgrammaticStrategyProfile, error) {
	profiles := make(map[string]ProgrammaticStrategyProfile, len(c.Traders))
	for i := range c.Traders {
		trader := &c.Traders[i]
		mode, err := normalizeDecisionMode(trader.DecisionMode)
		if err != nil {
			return nil, fmt.Errorf("trader[%d]: %w", i, err)
		}
		trader.DecisionMode = mode
		if mode == DecisionModeAI {
			profiles[trader.ID] = ProgrammaticStrategyProfile{DecisionMode: DecisionModeAI}
			continue
		}
		profile, err := normalizeProgrammaticStrategyConfig(trader.ProgrammaticStrategy)
		if err != nil {
			return nil, fmt.Errorf("trader[%d] programmatic_strategy: %w", i, err)
		}
		profile.DecisionMode = DecisionModeProgrammatic
		profile.ConfigHash = hashProgrammaticProfile(profile)
		profiles[trader.ID] = profile
	}
	return profiles, nil
}

func normalizeDecisionMode(mode string) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return DecisionModeAI, nil
	}
	switch mode {
	case DecisionModeAI, DecisionModeProgrammatic:
		return mode, nil
	default:
		return "", fmt.Errorf("decision_mode必须是 ai 或 programmatic: %q", mode)
	}
}

func normalizeProgrammaticStrategyConfig(cfg ProgrammaticStrategyConfig) (ProgrammaticStrategyProfile, error) {
	allowLong := true
	if cfg.AllowLong != nil {
		allowLong = *cfg.AllowLong
	}
	allowShort := true
	if cfg.AllowShort != nil {
		allowShort = *cfg.AllowShort
	}

	strategyName := strings.TrimSpace(cfg.StrategyName)
	if strategyName == "" {
		strategyName = defaultProgrammaticStrategyName
	}
	strategyVersion := strings.TrimSpace(cfg.StrategyVersion)
	if strategyVersion == "" {
		strategyVersion = defaultProgrammaticStrategyVersion
	}

	adxPeriod := cfg.ADX.Period
	if adxPeriod <= 0 {
		adxPeriod = 14
	}
	if adxPeriod < 2 || adxPeriod > 100 {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("adx.period必须在2-100之间: %d", adxPeriod)
	}

	timeframes, err := normalizeProgrammaticTimeframes(cfg.Timeframes)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	historyDepth, err := normalizeProgrammaticHistoryDepth(cfg.HistoryDepth, adxPeriod)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	symbolPool, err := normalizeProgrammaticSymbolPool(cfg.SymbolPool)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	ma, err := normalizeProgrammaticMA(cfg.MovingAverage)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	structure, err := normalizeProgrammaticStructure(cfg.Structure)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	divergence, err := normalizeProgrammaticDivergence(cfg.Divergence)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	position, err := normalizeProgrammaticPosition(cfg.Position)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	tp, err := normalizeProgrammaticTP(cfg.TakeProfit)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	signals, err := normalizeEnabledSignals(cfg.EnabledSignals)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	statePath := strings.TrimSpace(cfg.State.Path)
	if statePath == "" {
		statePath = defaultProgrammaticStatePath
	}

	minADX := cfg.ADX.MinADX
	if minADX <= 0 {
		minADX = 20
	}
	if minADX < 0 || minADX > 100 {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("adx.min_adx必须在0-100之间: %.2f", minADX)
	}

	return ProgrammaticStrategyProfile{
		StrategyName:    strategyName,
		StrategyVersion: strategyVersion,
		AllowLong:       allowLong,
		AllowShort:      allowShort,
		EnabledSignals:  signals,
		Timeframes:      timeframes,
		HistoryDepth:    historyDepth,
		SymbolPool:      symbolPool,
		MovingAverage:   ma,
		Structure:       structure,
		Divergence:      divergence,
		ADX: ProgrammaticADXProfile{
			Period:         adxPeriod,
			MinADX:         minADX,
			MicroADXFilter: cfg.ADX.MicroADXFilter,
		},
		Position:   position,
		TakeProfit: tp,
		State: ProgrammaticStateProfile{
			Path:      statePath,
			Bootstrap: cfg.State.Bootstrap,
		},
	}, nil
}

func normalizeProgrammaticTimeframes(cfg ProgrammaticTimeframesConfig) (ProgrammaticTimeframesProfile, error) {
	profile := ProgrammaticTimeframesProfile{
		Higher: defaultString(cfg.Higher, "4h"),
		Trade:  defaultString(cfg.Trade, "1h"),
		Sub:    defaultString(cfg.Sub, "15m"),
		Micro:  defaultString(cfg.Micro, "3m"),
	}
	for name, tf := range map[string]string{
		"timeframes.higher": profile.Higher,
		"timeframes.trade":  profile.Trade,
		"timeframes.sub":    profile.Sub,
		"timeframes.micro":  profile.Micro,
	} {
		if !isSupportedProgrammaticTimeframe(tf) {
			return ProgrammaticTimeframesProfile{}, fmt.Errorf("%s必须是 3m、15m、1h 或 4h: %q", name, tf)
		}
	}
	return profile, nil
}

func normalizeProgrammaticHistoryDepth(cfg ProgrammaticHistoryDepth, adxPeriod int) (ProgrammaticHistoryDepth, error) {
	depth := ProgrammaticHistoryDepth{M3: 240, M15: 192, H1: 240, H4: 180}
	if cfg.M3 > 0 {
		depth.M3 = cfg.M3
	}
	if cfg.M15 > 0 {
		depth.M15 = cfg.M15
	}
	if cfg.H1 > 0 {
		depth.H1 = cfg.H1
	}
	if cfg.H4 > 0 {
		depth.H4 = cfg.H4
	}
	minDepth := adxPeriod * 2
	for name, value := range map[string]int{"3m": depth.M3, "15m": depth.M15, "1h": depth.H1, "4h": depth.H4} {
		if value < minDepth {
			return ProgrammaticHistoryDepth{}, fmt.Errorf("history_depth.%s不能低于ADX最低样本%d: %d", name, minDepth, value)
		}
	}
	return depth, nil
}

func normalizeProgrammaticSymbolPool(cfg ProgrammaticSymbolPoolConfig) (ProgrammaticSymbolPoolProfile, error) {
	mode := strings.TrimSpace(strings.ToLower(cfg.Mode))
	if mode == "" {
		mode = "append"
	}
	switch mode {
	case "append", "override", "filter":
	default:
		return ProgrammaticSymbolPoolProfile{}, fmt.Errorf("symbol_pool.mode必须是 append、override 或 filter: %q", mode)
	}
	symbols, err := normalizeSymbolList(cfg.Symbols, "symbol_pool.symbols")
	if err != nil {
		return ProgrammaticSymbolPoolProfile{}, err
	}
	coreSymbols, err := normalizeSymbolList(cfg.CoreSymbols, "symbol_pool.core_symbols")
	if err != nil {
		return ProgrammaticSymbolPoolProfile{}, err
	}
	return ProgrammaticSymbolPoolProfile{Mode: mode, Symbols: symbols, CoreSymbols: coreSymbols}, nil
}

func normalizeProgrammaticMA(cfg ProgrammaticMAConfig) (ProgrammaticMAProfile, error) {
	shortPeriod := cfg.ShortPeriod
	if shortPeriod <= 0 {
		shortPeriod = 20
	}
	longPeriod := cfg.LongPeriod
	if longPeriod <= 0 {
		longPeriod = 50
	}
	if shortPeriod < 2 || longPeriod < 3 || shortPeriod >= longPeriod {
		return ProgrammaticMAProfile{}, fmt.Errorf("moving_average.short_period必须小于long_period且均大于1: %d/%d", shortPeriod, longPeriod)
	}
	kissDistance, err := normalizePercentRatio(cfg.KissDistancePct, 0.0015, "moving_average.kiss_distance_pct")
	if err != nil {
		return ProgrammaticMAProfile{}, err
	}
	wetKissBars := cfg.WetKissBars
	if wetKissBars <= 0 {
		wetKissBars = 5
	}
	return ProgrammaticMAProfile{
		ShortPeriod:     shortPeriod,
		LongPeriod:      longPeriod,
		KissDistancePct: kissDistance,
		WetKissBars:     wetKissBars,
	}, nil
}

func normalizeProgrammaticStructure(cfg ProgrammaticStructureConfig) (ProgrammaticStructureProfile, error) {
	strictness := strings.TrimSpace(strings.ToLower(cfg.Strictness))
	if strictness == "" {
		strictness = "enhanced"
	}
	switch strictness {
	case "enhanced", "pivot", "confirm_both":
	default:
		return ProgrammaticStructureProfile{}, fmt.Errorf("structure.strictness必须是 enhanced、pivot 或 confirm_both: %q", strictness)
	}
	leftBars := defaultPositiveInt(cfg.LeftBars, 2)
	rightBars := defaultPositiveInt(cfg.RightBars, 2)
	minStrokeBars := defaultPositiveInt(cfg.MinStrokeBars, 5)
	minSwingPct, err := normalizePercentRatio(cfg.MinSwingPct, 0.003, "structure.min_swing_pct")
	if err != nil {
		return ProgrammaticStructureProfile{}, err
	}
	atrMultiplier := cfg.ATRMultiplier
	if atrMultiplier <= 0 {
		atrMultiplier = 0.5
	}
	return ProgrammaticStructureProfile{
		Strictness:    strictness,
		LeftBars:      leftBars,
		RightBars:     rightBars,
		MinStrokeBars: minStrokeBars,
		MinSwingPct:   minSwingPct,
		ATRMultiplier: atrMultiplier,
		Bootstrap:     cfg.Bootstrap,
	}, nil
}

func normalizeProgrammaticDivergence(cfg ProgrammaticDivergenceConfig) (ProgrammaticDivergenceProfile, error) {
	ratio := cfg.Ratio
	if ratio <= 0 {
		ratio = 0.8
	}
	if ratio <= 0 || ratio > 1 {
		return ProgrammaticDivergenceProfile{}, fmt.Errorf("divergence.ratio必须在0-1之间: %.4f", ratio)
	}
	tolerance, err := normalizePercentRatio(cfg.PriceTolerancePct, 0.001, "divergence.price_tolerance_pct")
	if err != nil {
		return ProgrammaticDivergenceProfile{}, err
	}
	atrMultiplier := cfg.PriceToleranceATRMultiplier
	if atrMultiplier <= 0 {
		atrMultiplier = 0.2
	}
	return ProgrammaticDivergenceProfile{
		Ratio:                       ratio,
		PriceTolerancePct:           tolerance,
		PriceToleranceATRMultiplier: atrMultiplier,
		RequireBZeroAxis:            cfg.RequireBZeroAxis,
	}, nil
}

func normalizeProgrammaticPosition(cfg ProgrammaticPositionConfig) (ProgrammaticPositionProfile, error) {
	maxAddCount := cfg.MaxAddCount
	if maxAddCount <= 0 {
		maxAddCount = 2
	}
	if maxAddCount < 0 || maxAddCount > 10 {
		return ProgrammaticPositionProfile{}, fmt.Errorf("position.max_add_count必须在0-10之间: %d", maxAddCount)
	}
	addSizeMultiplier := cfg.AddSizeMultiplier
	if addSizeMultiplier <= 0 {
		addSizeMultiplier = 0.5
	}
	if addSizeMultiplier <= 0 || addSizeMultiplier > 1 {
		return ProgrammaticPositionProfile{}, fmt.Errorf("position.add_size_multiplier必须在0-1之间: %.4f", addSizeMultiplier)
	}
	partialClosePct := cfg.PartialClosePct
	if partialClosePct <= 0 {
		partialClosePct = 30
	}
	if partialClosePct <= 0 || partialClosePct > 100 {
		return ProgrammaticPositionProfile{}, fmt.Errorf("position.partial_close_pct必须在0-100之间: %.2f", partialClosePct)
	}
	return ProgrammaticPositionProfile{
		MaxAddCount:       maxAddCount,
		AddSizeMultiplier: addSizeMultiplier,
		PartialClosePct:   partialClosePct,
		AllowReversal:     cfg.AllowReversal,
	}, nil
}

func normalizeProgrammaticTP(cfg ProgrammaticTPConfig) (ProgrammaticTPProfile, error) {
	mode := strings.TrimSpace(strings.ToLower(cfg.Mode))
	if mode == "" {
		mode = "structure"
	}
	if mode != "structure" {
		return ProgrammaticTPProfile{}, fmt.Errorf("take_profit.mode当前仅支持 structure: %q", mode)
	}
	fallbackMode := strings.TrimSpace(strings.ToLower(cfg.FallbackMode))
	if fallbackMode == "" {
		fallbackMode = "reject"
	}
	switch fallbackMode {
	case "reject", "rr_target":
	default:
		return ProgrammaticTPProfile{}, fmt.Errorf("take_profit.fallback_mode必须是 reject 或 rr_target: %q", fallbackMode)
	}
	minRR := cfg.MinNetRR
	if minRR <= 0 {
		minRR = defaultStrategyMinNetRR
	}
	if minRR < 1 {
		return ProgrammaticTPProfile{}, fmt.Errorf("take_profit.min_net_rr不能低于1: %.4f", minRR)
	}
	return ProgrammaticTPProfile{Mode: mode, FallbackMode: fallbackMode, MinNetRR: minRR}, nil
}

func normalizeEnabledSignals(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"buy1", "buy2", "buy3", "sell1", "sell2", "sell3"}, nil
	}
	allowed := map[string]bool{
		"buy1": true, "buy2": true, "buy3": true,
		"sell1": true, "sell2": true, "sell3": true,
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if !allowed[value] {
			return nil, fmt.Errorf("enabled_signals包含不支持的信号: %q", value)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result, nil
}

func normalizeSymbolList(values []string, field string) ([]string, error) {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		symbol := normalizeProgrammaticSymbol(value)
		if !programmaticSymbolPattern.MatchString(symbol) {
			return nil, fmt.Errorf("%s包含无效symbol: %q", field, value)
		}
		if !seen[symbol] {
			seen[symbol] = true
			result = append(result, symbol)
		}
	}
	sort.Strings(result)
	return result, nil
}

func normalizeProgrammaticSymbol(value string) string {
	symbol := strings.ToUpper(strings.TrimSpace(value))
	symbol = strings.ReplaceAll(symbol, "/", "")
	symbol = strings.ReplaceAll(symbol, "-", "")
	symbol = strings.ReplaceAll(symbol, "_", "")
	if symbol != "" && !strings.HasSuffix(symbol, "USDT") {
		symbol += "USDT"
	}
	return symbol
}

func hashProgrammaticProfile(profile ProgrammaticStrategyProfile) string {
	profile.ConfigHash = ""
	data, _ := json.Marshal(profile)
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])[:12]
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func defaultPositiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func isSupportedProgrammaticTimeframe(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "3m", "15m", "1h", "4h":
		return true
	default:
		return false
	}
}
