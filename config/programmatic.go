package config

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

const (
	DecisionModeAI           = "ai"
	DecisionModeProgrammatic = "programmatic"
	DecisionModeChanlunV2    = "chanlun_v2"

	defaultProgrammaticStrategyName    = "chanlun_programmatic"
	defaultProgrammaticStrategyVersion = "v1"
	defaultProgrammaticStatePath       = "data/programmatic_strategy_state.json"
)

var programmaticSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,30}(USDT|USDC)$`)

type ProgrammaticStrategyConfig struct {
	StrategyName                  string                               `json:"strategy_name,omitempty"`
	StrategyVersion               string                               `json:"strategy_version,omitempty"`
	DefectFixPackEnabled          *bool                                `json:"defect_fix_pack_enabled,omitempty"`
	SuppressionPermanentThreshold int                                  `json:"suppression_permanent_threshold,omitempty"`
	MaxPilotNotionalPct           float64                              `json:"max_pilot_notional_pct,omitempty"`
	MinPilotNotionalUSD           float64                              `json:"min_pilot_notional_usd,omitempty"`
	AllowLong                     *bool                                `json:"allow_long,omitempty"`
	AllowShort                    *bool                                `json:"allow_short,omitempty"`
	EnabledSignals                []string                             `json:"enabled_signals,omitempty"`
	Timeframes                    ProgrammaticTimeframesConfig         `json:"timeframes,omitempty"`
	HistoryDepth                  ProgrammaticHistoryDepth             `json:"history_depth,omitempty"`
	SymbolPool                    ProgrammaticSymbolPoolConfig         `json:"symbol_pool,omitempty"`
	MovingAverage                 ProgrammaticMAConfig                 `json:"moving_average,omitempty"`
	Structure                     ProgrammaticStructureConfig          `json:"structure,omitempty"`
	Divergence                    ProgrammaticDivergenceConfig         `json:"divergence,omitempty"`
	ADX                           ProgrammaticADXConfig                `json:"adx,omitempty"`
	Position                      ProgrammaticPositionConfig           `json:"position,omitempty"`
	PositionManagement            ProgrammaticPositionManagementConfig `json:"position_management,omitempty"`
	TakeProfit                    ProgrammaticTPConfig                 `json:"take_profit,omitempty"`
	SignalFreshness               ProgrammaticSignalFreshnessConfig    `json:"signal_freshness,omitempty"`
	PreviewSignals                ProgrammaticPreviewSignalsConfig     `json:"preview_signals,omitempty"`
	EntryTiming                   ProgrammaticEntryTimingConfig        `json:"entry_timing,omitempty"`
	CandidateGovernor             ProgrammaticCandidateGovernorConfig  `json:"candidate_governor,omitempty"`
	State                         ProgrammaticStateConfig              `json:"state,omitempty"`
}

type ProgrammaticCandidateGovernorConfig struct {
	Enabled               *bool    `json:"enabled,omitempty"`
	AllowNonCryptoSymbols []string `json:"allow_non_crypto_symbols,omitempty"`
	MaxQuoteSpreadBps     float64  `json:"max_quote_spread_bps,omitempty"`
	CoreSymbolsMustAppear []string `json:"core_symbols_must_appear,omitempty"`
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

type ProgrammaticPositionManagementConfig struct {
	Enabled          *bool                              `json:"enabled,omitempty"`
	Timeframes       ProgrammaticManagementTFConfig     `json:"timeframes,omitempty"`
	Breakeven        ProgrammaticBreakevenConfig        `json:"breakeven,omitempty"`
	FloatingDrawdown ProgrammaticFloatingDrawdownConfig `json:"floating_drawdown,omitempty"`
	StructureBreak   ProgrammaticStructureBreakConfig   `json:"structure_break,omitempty"`
	ShortTrade       ProgrammaticShortTradeConfig       `json:"short_trade,omitempty"`

	PartialCloseCooldownMinutes     *int    `json:"partial_close_cooldown_minutes,omitempty"`
	MaxPartialCloseCountPerPosition int     `json:"max_partial_close_count_per_position,omitempty"`
	MaxTotalPartialClosePct         float64 `json:"max_total_partial_close_pct,omitempty"`
}

type ProgrammaticManagementTFConfig struct {
	Structure string `json:"structure,omitempty"`
	Micro     string `json:"micro,omitempty"`
}

type ProgrammaticBreakevenConfig struct {
	Enabled          *bool   `json:"enabled,omitempty"`
	TriggerProfitPct float64 `json:"trigger_profit_pct,omitempty"`
	TriggerR         float64 `json:"trigger_r,omitempty"`
	BufferPct        float64 `json:"buffer_pct,omitempty"`
}

type ProgrammaticFloatingDrawdownConfig struct {
	Enabled             *bool   `json:"enabled,omitempty"`
	ActivationProfitPct float64 `json:"activation_profit_pct,omitempty"`
	ActivationR         float64 `json:"activation_r,omitempty"`
	DrawdownPct         float64 `json:"drawdown_pct,omitempty"`
	Action              string  `json:"action,omitempty"`
}

type ProgrammaticStructureBreakConfig struct {
	Enabled                 *bool  `json:"enabled,omitempty"`
	ConfirmBars             int    `json:"confirm_bars,omitempty"`
	Action                  string `json:"action,omitempty"`
	PartialCloseGuardAction string `json:"partial_close_guard_action,omitempty"`
}

type ProgrammaticShortTradeConfig struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	PartialClosePct float64 `json:"partial_close_pct,omitempty"`
}

type ProgrammaticTPConfig struct {
	Mode         string  `json:"mode,omitempty"`
	FallbackMode string  `json:"fallback_mode,omitempty"`
	MinNetRR     float64 `json:"min_net_rr,omitempty"`
}

type ProgrammaticSignalFreshnessConfig struct {
	Enabled                      *bool          `json:"enabled,omitempty"`
	SoftAgeCandles               int            `json:"soft_age_candles,omitempty"`
	MaxLifetimeCandles           int            `json:"max_lifetime_candles,omitempty"`
	SoftAgeBySignalType          map[string]int `json:"soft_age_by_signal_type,omitempty"`
	MaxLifetimeBySignalType      map[string]int `json:"max_lifetime_by_signal_type,omitempty"`
	MissedTargetGuard            *bool          `json:"missed_target_guard,omitempty"`
	ConfidenceDecayPerAgedCandle int            `json:"confidence_decay_per_aged_candle,omitempty"`
	MinRemainingNetRR            float64        `json:"min_remaining_net_rr,omitempty"`
}

type ProgrammaticPreviewSignalsConfig struct {
	Enabled                      *bool          `json:"enabled,omitempty"`
	ComponentTimeframe           string         `json:"component_timeframe,omitempty"`
	TradeTimeframe               string         `json:"trade_timeframe,omitempty"`
	WatchAfterClosedComponents   int            `json:"watch_after_closed_components,omitempty"`
	PilotAfterClosedComponents   int            `json:"pilot_after_closed_components,omitempty"`
	AllowPilotOpen               bool           `json:"allow_pilot_open,omitempty"`
	PilotRiskFraction            float64        `json:"pilot_risk_fraction,omitempty"`
	PilotMinConfidence           int            `json:"pilot_min_confidence,omitempty"`
	PilotMinConfidenceConfigured bool           `json:"-"`
	PilotMinConfidenceBySignal   map[string]int `json:"pilot_min_confidence_by_signal_type,omitempty"`
	PilotMinConfidenceUseP75     *bool          `json:"pilot_min_confidence_use_p75,omitempty"`
	P75Floor                     int            `json:"pilot_min_confidence_p75_floor,omitempty"`
	P75Ceiling                   int            `json:"pilot_min_confidence_p75_ceiling,omitempty"`
	RequireConfirmedUpgrade      *bool          `json:"require_confirmed_upgrade,omitempty"`
}

func (c *ProgrammaticPreviewSignalsConfig) UnmarshalJSON(data []byte) error {
	type alias ProgrammaticPreviewSignalsConfig
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if _, ok := raw["pilot_min_confidence"]; ok {
		out.PilotMinConfidenceConfigured = true
	}
	*c = ProgrammaticPreviewSignalsConfig(out)
	return nil
}

type ProgrammaticEntryTimingConfig struct {
	Enabled                        *bool                        `json:"enabled,omitempty"`
	DirectStructureOpen            bool                         `json:"direct_structure_open,omitempty"`
	DirectStructureOpenConfigured  bool                         `json:"-"`
	DirectStructureMinConfidence   int                          `json:"direct_structure_min_confidence,omitempty"`
	DirectOpenMaxAgeCandles        int                          `json:"direct_open_max_age_candles,omitempty"`
	MaxNoTriggerSubCandles         int                          `json:"max_no_trigger_sub_candles,omitempty"`
	RequireFreshTrigger            *bool                        `json:"require_fresh_trigger,omitempty"`
	TriggerTimeframe               string                       `json:"trigger_timeframe,omitempty"`
	AllowedTriggerTypes            []string                     `json:"allowed_trigger_types,omitempty"`
	EntryZone                      ProgrammaticEntryZoneConfig  `json:"entry_zone,omitempty"`
	MaxTriggerAgeCandles           int                          `json:"max_trigger_age_candles,omitempty"`
	MinTriggerConfidence           int                          `json:"min_trigger_confidence,omitempty"`
	Pilot                          ProgrammaticEntryPilotConfig `json:"pilot,omitempty"`
	ContinuationAfterTargetCrossed string                       `json:"continuation_after_target_crossed,omitempty"`
}

func (c *ProgrammaticEntryTimingConfig) UnmarshalJSON(data []byte) error {
	type alias ProgrammaticEntryTimingConfig
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if _, ok := raw["direct_structure_open"]; ok {
		out.DirectStructureOpenConfigured = true
	}
	*c = ProgrammaticEntryTimingConfig(out)
	return nil
}

type ProgrammaticEntryZoneConfig struct {
	Mode                         string                                         `json:"mode,omitempty"`
	MaxChaseRatio                float64                                        `json:"max_chase_ratio,omitempty"`
	MinRemainingNetRR            float64                                        `json:"min_remaining_net_rr,omitempty"`
	MaxChaseATRMultiplier        float64                                        `json:"max_chase_atr_multiplier,omitempty"`
	FreshAgeChaseRelax           float64                                        `json:"fresh_age_chase_relax,omitempty"`
	SignalTypeMinRR              map[string]float64                             `json:"signal_type_min_rr,omitempty"`
	TierOverrides                map[string]ProgrammaticEntryZoneOverrideConfig `json:"tier_overrides,omitempty"`
	SymbolOverrides              map[string]ProgrammaticEntryZoneOverrideConfig `json:"symbol_overrides,omitempty"`
	TheoreticalRRUnreachableSkip *bool                                          `json:"theoretical_rr_unreachable_skip,omitempty"`
}

type ProgrammaticEntryZoneOverrideConfig struct {
	MaxChaseRatio     float64 `json:"max_chase_ratio,omitempty"`
	MinRemainingNetRR float64 `json:"min_remaining_net_rr,omitempty"`
}

type ProgrammaticEntryPilotConfig struct {
	Enabled                 bool    `json:"enabled,omitempty"`
	RiskFraction            float64 `json:"risk_fraction,omitempty"`
	MinConfidence           int     `json:"min_confidence,omitempty"`
	MinConfidenceConfigured bool    `json:"-"`
}

func (c *ProgrammaticEntryPilotConfig) UnmarshalJSON(data []byte) error {
	type alias ProgrammaticEntryPilotConfig
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if _, ok := raw["min_confidence"]; ok {
		out.MinConfidenceConfigured = true
	}
	*c = ProgrammaticEntryPilotConfig(out)
	return nil
}

type ProgrammaticStateConfig struct {
	Path      string `json:"path,omitempty"`
	Bootstrap bool   `json:"bootstrap,omitempty"`
}

type ProgrammaticStrategyProfile struct {
	DecisionMode                  string
	StrategyName                  string
	StrategyVersion               string
	ConfigHash                    string
	DefectFixPackEnabled          bool
	SuppressionPermanentThreshold int
	MaxPilotNotionalPct           float64
	MinPilotNotionalUSD           float64
	AllowLong                     bool
	AllowShort                    bool
	EnabledSignals                []string
	Timeframes                    ProgrammaticTimeframesProfile
	HistoryDepth                  ProgrammaticHistoryDepth
	SymbolPool                    ProgrammaticSymbolPoolProfile
	MovingAverage                 ProgrammaticMAProfile
	Structure                     ProgrammaticStructureProfile
	Divergence                    ProgrammaticDivergenceProfile
	ADX                           ProgrammaticADXProfile
	Position                      ProgrammaticPositionProfile
	PositionManagement            ProgrammaticPositionManagementProfile
	TakeProfit                    ProgrammaticTPProfile
	SignalFreshness               ProgrammaticSignalFreshnessProfile
	PreviewSignals                ProgrammaticPreviewSignalsProfile
	EntryTiming                   ProgrammaticEntryTimingProfile
	CandidateGovernor             ProgrammaticCandidateGovernorProfile
	State                         ProgrammaticStateProfile
}

type ProgrammaticCandidateGovernorProfile struct {
	Enabled               bool
	AllowNonCryptoSymbols []string
	MaxQuoteSpreadBps     float64
	CoreSymbolsMustAppear []string
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

type ProgrammaticPositionManagementProfile struct {
	Enabled           bool
	Timeframes        ProgrammaticManagementTFProfile
	Breakeven         ProgrammaticBreakevenProfile
	FloatingDrawdown  ProgrammaticFloatingDrawdownProfile
	StructureBreak    ProgrammaticStructureBreakProfile
	ShortTrade        ProgrammaticShortTradeProfile
	PartialCloseGuard ProgrammaticPartialCloseGuardProfile
}

type ProgrammaticManagementTFProfile struct {
	Structure string
	Micro     string
}

type ProgrammaticBreakevenProfile struct {
	Enabled          bool
	TriggerProfitPct float64
	TriggerR         float64
	BufferRatio      float64
}

type ProgrammaticFloatingDrawdownProfile struct {
	Enabled             bool
	ActivationProfitPct float64
	ActivationR         float64
	DrawdownRatio       float64
	Action              string
}

type ProgrammaticStructureBreakProfile struct {
	Enabled                 bool
	ConfirmBars             int
	Action                  string
	PartialCloseGuardAction string
}

type ProgrammaticShortTradeProfile struct {
	Enabled         bool
	PartialClosePct float64
}

type ProgrammaticPartialCloseGuardProfile struct {
	CooldownMinutes     int
	MaxCountPerPosition int
	MaxTotalRatio       float64
	CooldownEnabled     bool
}

type ProgrammaticTPProfile struct {
	Mode         string
	FallbackMode string
	MinNetRR     float64
}

type ProgrammaticSignalFreshnessProfile struct {
	Enabled                      bool
	SoftAgeCandles               int
	MaxLifetimeCandles           int
	SoftAgeBySignalType          map[string]int
	MaxLifetimeBySignalType      map[string]int
	MissedTargetGuard            bool
	ConfidenceDecayPerAgedCandle int
	MinRemainingNetRR            float64
}

type ProgrammaticPreviewSignalsProfile struct {
	Enabled                    bool
	ComponentTimeframe         string
	TradeTimeframe             string
	WatchAfterClosedComponents int
	PilotAfterClosedComponents int
	AllowPilotOpen             bool
	PilotRiskFraction          float64
	PilotMinConfidence         int
	PilotMinConfidenceBySignal map[string]int
	PilotMinConfidenceUseP75   bool
	P75Floor                   int
	P75Ceiling                 int
	RequireConfirmedUpgrade    bool
}

type ProgrammaticEntryTimingProfile struct {
	Enabled                        bool
	DirectStructureOpen            bool
	DirectStructureMinConfidence   int
	DirectOpenMaxAgeCandles        int
	MaxNoTriggerSubCandles         int
	RequireFreshTrigger            bool
	TriggerTimeframe               string
	AllowedTriggerTypes            []string
	EntryZone                      ProgrammaticEntryZoneProfile
	MaxTriggerAgeCandles           int
	MinTriggerConfidence           int
	Pilot                          ProgrammaticEntryPilotProfile
	ContinuationAfterTargetCrossed string
}

type ProgrammaticEntryZoneProfile struct {
	Mode                         string
	MaxChaseRatio                float64
	MinRemainingNetRR            float64
	MaxChaseATRMultiplier        float64
	FreshAgeChaseRelax           float64
	SignalTypeMinRR              map[string]float64
	TierOverrides                map[string]ProgrammaticEntryZoneOverrideProfile
	SymbolOverrides              map[string]ProgrammaticEntryZoneOverrideProfile
	TheoreticalRRUnreachableSkip bool
}

type ProgrammaticEntryZoneOverrideProfile struct {
	MaxChaseRatio     float64
	MinRemainingNetRR float64
}

type ProgrammaticEntryPilotProfile struct {
	Enabled       bool
	RiskFraction  float64
	MinConfidence int
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
	case DecisionModeAI, DecisionModeProgrammatic, DecisionModeChanlunV2:
		return mode, nil
	default:
		return "", fmt.Errorf("decision_mode必须是 ai、programmatic 或 chanlun_v2: %q", mode)
	}
}

func normalizeProgrammaticStrategyConfig(cfg ProgrammaticStrategyConfig) (ProgrammaticStrategyProfile, error) {
	defectFixPackEnabled := true
	if cfg.DefectFixPackEnabled != nil {
		defectFixPackEnabled = *cfg.DefectFixPackEnabled
	}
	suppressionPermanentThreshold := cfg.SuppressionPermanentThreshold
	if suppressionPermanentThreshold <= 0 {
		suppressionPermanentThreshold = 5
	}
	if suppressionPermanentThreshold < 1 || suppressionPermanentThreshold > 100 {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("suppression_permanent_threshold必须在1-100之间: %d", suppressionPermanentThreshold)
	}
	maxPilotNotionalPct := cfg.MaxPilotNotionalPct
	if maxPilotNotionalPct <= 0 {
		maxPilotNotionalPct = 0.6
	}
	if maxPilotNotionalPct <= 0 || maxPilotNotionalPct > 1 {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("max_pilot_notional_pct必须在0-1之间: %.4f", maxPilotNotionalPct)
	}
	minPilotNotionalUSD := cfg.MinPilotNotionalUSD
	if minPilotNotionalUSD <= 0 {
		minPilotNotionalUSD = 30
	}
	if minPilotNotionalUSD < 1 || minPilotNotionalUSD > 10000 {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("min_pilot_notional_usd必须在1-10000之间: %.4f", minPilotNotionalUSD)
	}
	if cfg.PreviewSignals.PilotMinConfidenceConfigured && cfg.EntryTiming.Pilot.MinConfidenceConfigured &&
		cfg.PreviewSignals.PilotMinConfidence > 0 && cfg.EntryTiming.Pilot.MinConfidence > 0 &&
		cfg.PreviewSignals.PilotMinConfidence != cfg.EntryTiming.Pilot.MinConfidence {
		return ProgrammaticStrategyProfile{}, fmt.Errorf("pilot_min_confidence 配置冲突: preview_signals=%d entry_timing.pilot=%d", cfg.PreviewSignals.PilotMinConfidence, cfg.EntryTiming.Pilot.MinConfidence)
	}

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
	positionManagement, err := normalizeProgrammaticPositionManagement(cfg.PositionManagement, position)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	tp, err := normalizeProgrammaticTP(cfg.TakeProfit)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	freshness, err := normalizeProgrammaticSignalFreshness(cfg.SignalFreshness, tp.MinNetRR)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	preview, err := normalizeProgrammaticPreviewSignals(cfg.PreviewSignals, timeframes, defectFixPackEnabled)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	entryTiming, err := normalizeProgrammaticEntryTiming(cfg.EntryTiming, timeframes, tp.MinNetRR, defectFixPackEnabled)
	if err != nil {
		return ProgrammaticStrategyProfile{}, err
	}
	candidateGovernor, err := normalizeProgrammaticCandidateGovernor(cfg.CandidateGovernor, symbolPool)
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
		StrategyName:                  strategyName,
		StrategyVersion:               strategyVersion,
		DefectFixPackEnabled:          defectFixPackEnabled,
		SuppressionPermanentThreshold: suppressionPermanentThreshold,
		MaxPilotNotionalPct:           maxPilotNotionalPct,
		MinPilotNotionalUSD:           minPilotNotionalUSD,
		AllowLong:                     allowLong,
		AllowShort:                    allowShort,
		EnabledSignals:                signals,
		Timeframes:                    timeframes,
		HistoryDepth:                  historyDepth,
		SymbolPool:                    symbolPool,
		MovingAverage:                 ma,
		Structure:                     structure,
		Divergence:                    divergence,
		ADX: ProgrammaticADXProfile{
			Period:         adxPeriod,
			MinADX:         minADX,
			MicroADXFilter: cfg.ADX.MicroADXFilter,
		},
		Position:           position,
		PositionManagement: positionManagement,
		TakeProfit:         tp,
		SignalFreshness:    freshness,
		PreviewSignals:     preview,
		EntryTiming:        entryTiming,
		CandidateGovernor:  candidateGovernor,
		State: ProgrammaticStateProfile{
			Path:      statePath,
			Bootstrap: cfg.State.Bootstrap,
		},
	}, nil
}

func normalizeProgrammaticTimeframes(cfg ProgrammaticTimeframesConfig) (ProgrammaticTimeframesProfile, error) {
	trade := defaultString(cfg.Trade, "1h")
	if !isSupportedProgrammaticTradeTimeframe(trade) {
		return ProgrammaticTimeframesProfile{}, fmt.Errorf("timeframes.trade必须是 15m、1h 或 4h: %q", trade)
	}
	sub := defaultString(cfg.Sub, "")
	if sub == "" {
		sub = defaultSubTimeframeForTrade(trade)
	}
	profile := ProgrammaticTimeframesProfile{
		Higher: defaultString(cfg.Higher, "4h"),
		Trade:  trade,
		Sub:    sub,
		Micro:  defaultString(cfg.Micro, "3m"),
	}
	for name, tf := range map[string]string{
		"timeframes.higher": profile.Higher,
		"timeframes.sub":    profile.Sub,
		"timeframes.micro":  profile.Micro,
	} {
		if !isSupportedProgrammaticTimeframe(tf) {
			return ProgrammaticTimeframesProfile{}, fmt.Errorf("%s必须是 3m、15m、1h 或 4h: %q", name, tf)
		}
	}
	return profile, nil
}

func defaultSubTimeframeForTrade(trade string) string {
	switch trade {
	case "15m":
		return "3m"
	case "4h":
		return "1h"
	default:
		return "15m"
	}
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

func normalizeProgrammaticCandidateGovernor(cfg ProgrammaticCandidateGovernorConfig, symbolPool ProgrammaticSymbolPoolProfile) (ProgrammaticCandidateGovernorProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	maxSpread := cfg.MaxQuoteSpreadBps
	if maxSpread <= 0 {
		maxSpread = 20
	}
	if maxSpread < 0 || maxSpread > 10000 {
		return ProgrammaticCandidateGovernorProfile{}, fmt.Errorf("candidate_governor.max_quote_spread_bps必须在0-10000之间: %.4f", maxSpread)
	}
	allowNonCrypto := make([]string, 0, len(cfg.AllowNonCryptoSymbols))
	seenAllow := map[string]bool{}
	for _, raw := range cfg.AllowNonCryptoSymbols {
		symbol := normalizeProgrammaticSymbol(raw)
		if symbol == "" || !programmaticSymbolPattern.MatchString(symbol) {
			return ProgrammaticCandidateGovernorProfile{}, fmt.Errorf("candidate_governor.allow_non_crypto_symbols包含无效symbol: %q", raw)
		}
		if !seenAllow[symbol] {
			seenAllow[symbol] = true
			allowNonCrypto = append(allowNonCrypto, symbol)
		}
	}
	coreRaw := cfg.CoreSymbolsMustAppear
	if len(coreRaw) == 0 {
		coreRaw = symbolPool.CoreSymbols
	}
	if len(coreRaw) == 0 {
		coreRaw = []string{"BTCUSDT", "ETHUSDT"}
	}
	core, err := normalizeSymbolList(coreRaw, "candidate_governor.core_symbols_must_appear")
	if err != nil {
		return ProgrammaticCandidateGovernorProfile{}, err
	}
	sort.Strings(allowNonCrypto)
	return ProgrammaticCandidateGovernorProfile{
		Enabled:               enabled,
		AllowNonCryptoSymbols: allowNonCrypto,
		MaxQuoteSpreadBps:     maxSpread,
		CoreSymbolsMustAppear: core,
	}, nil
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

func normalizeProgrammaticPositionManagement(cfg ProgrammaticPositionManagementConfig, position ProgrammaticPositionProfile) (ProgrammaticPositionManagementProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	timeframes, err := normalizeProgrammaticManagementTimeframes(cfg.Timeframes)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	breakeven, err := normalizeProgrammaticBreakeven(cfg.Breakeven)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	drawdown, err := normalizeProgrammaticFloatingDrawdown(cfg.FloatingDrawdown)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	structureBreak, err := normalizeProgrammaticStructureBreak(cfg.StructureBreak)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	shortTrade, err := normalizeProgrammaticShortTrade(cfg.ShortTrade, position.PartialClosePct)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	partialCloseGuard, err := normalizeProgrammaticPartialCloseGuard(cfg)
	if err != nil {
		return ProgrammaticPositionManagementProfile{}, err
	}
	return ProgrammaticPositionManagementProfile{
		Enabled:           enabled,
		Timeframes:        timeframes,
		Breakeven:         breakeven,
		FloatingDrawdown:  drawdown,
		StructureBreak:    structureBreak,
		ShortTrade:        shortTrade,
		PartialCloseGuard: partialCloseGuard,
	}, nil
}

func normalizeProgrammaticManagementTimeframes(cfg ProgrammaticManagementTFConfig) (ProgrammaticManagementTFProfile, error) {
	profile := ProgrammaticManagementTFProfile{
		Structure: defaultString(cfg.Structure, "15m"),
		Micro:     defaultString(cfg.Micro, "3m"),
	}
	for name, tf := range map[string]string{
		"position_management.timeframes.structure": profile.Structure,
		"position_management.timeframes.micro":     profile.Micro,
	} {
		if !isSupportedProgrammaticTimeframe(tf) {
			return ProgrammaticManagementTFProfile{}, fmt.Errorf("%s必须是 3m、15m、1h 或 4h: %q", name, tf)
		}
	}
	return profile, nil
}

func normalizeProgrammaticBreakeven(cfg ProgrammaticBreakevenConfig) (ProgrammaticBreakevenProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	triggerProfit, err := normalizeHumanPercentValue(cfg.TriggerProfitPct, 1.0, "position_management.breakeven.trigger_profit_pct")
	if err != nil {
		return ProgrammaticBreakevenProfile{}, err
	}
	triggerR := cfg.TriggerR
	if triggerR <= 0 {
		triggerR = 1.0
	}
	if triggerR < 0 || triggerR > 20 {
		return ProgrammaticBreakevenProfile{}, fmt.Errorf("position_management.breakeven.trigger_r必须在0-20之间: %.4f", triggerR)
	}
	bufferRatio, err := normalizeHumanPercentRatio(cfg.BufferPct, 0.05, "position_management.breakeven.buffer_pct")
	if err != nil {
		return ProgrammaticBreakevenProfile{}, err
	}
	return ProgrammaticBreakevenProfile{
		Enabled:          enabled,
		TriggerProfitPct: triggerProfit,
		TriggerR:         triggerR,
		BufferRatio:      bufferRatio,
	}, nil
}

func normalizeProgrammaticFloatingDrawdown(cfg ProgrammaticFloatingDrawdownConfig) (ProgrammaticFloatingDrawdownProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	activationProfit, err := normalizeHumanPercentValue(cfg.ActivationProfitPct, 2.0, "position_management.floating_drawdown.activation_profit_pct")
	if err != nil {
		return ProgrammaticFloatingDrawdownProfile{}, err
	}
	activationR := cfg.ActivationR
	if activationR <= 0 {
		activationR = 1.5
	}
	if activationR < 0 || activationR > 50 {
		return ProgrammaticFloatingDrawdownProfile{}, fmt.Errorf("position_management.floating_drawdown.activation_r必须在0-50之间: %.4f", activationR)
	}
	drawdownRatio, err := normalizeHumanPercentRatio(cfg.DrawdownPct, 35, "position_management.floating_drawdown.drawdown_pct")
	if err != nil {
		return ProgrammaticFloatingDrawdownProfile{}, err
	}
	action := strings.TrimSpace(strings.ToLower(cfg.Action))
	if action == "" {
		action = "partial_close"
	}
	if action != "partial_close" && action != "close" {
		return ProgrammaticFloatingDrawdownProfile{}, fmt.Errorf("position_management.floating_drawdown.action必须是 partial_close 或 close: %q", action)
	}
	return ProgrammaticFloatingDrawdownProfile{
		Enabled:             enabled,
		ActivationProfitPct: activationProfit,
		ActivationR:         activationR,
		DrawdownRatio:       drawdownRatio,
		Action:              action,
	}, nil
}

func normalizeProgrammaticStructureBreak(cfg ProgrammaticStructureBreakConfig) (ProgrammaticStructureBreakProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	confirmBars := cfg.ConfirmBars
	if confirmBars <= 0 {
		confirmBars = 2
	}
	if confirmBars < 1 || confirmBars > 10 {
		return ProgrammaticStructureBreakProfile{}, fmt.Errorf("position_management.structure_break.confirm_bars必须在1-10之间: %d", confirmBars)
	}
	action := strings.TrimSpace(strings.ToLower(cfg.Action))
	if action == "" {
		action = "partial_close"
	}
	if action != "partial_close" && action != "close" {
		return ProgrammaticStructureBreakProfile{}, fmt.Errorf("position_management.structure_break.action必须是 partial_close 或 close: %q", action)
	}
	guardAction := strings.TrimSpace(strings.ToLower(cfg.PartialCloseGuardAction))
	if guardAction == "" {
		guardAction = "bypass_cooldown_clip_budget"
	}
	switch guardAction {
	case "respect_guard", "bypass_cooldown_clip_budget", "close_on_budget_exhausted":
	default:
		return ProgrammaticStructureBreakProfile{}, fmt.Errorf("position_management.structure_break.partial_close_guard_action必须是 respect_guard、bypass_cooldown_clip_budget 或 close_on_budget_exhausted: %q", guardAction)
	}
	return ProgrammaticStructureBreakProfile{
		Enabled:                 enabled,
		ConfirmBars:             confirmBars,
		Action:                  action,
		PartialCloseGuardAction: guardAction,
	}, nil
}

func normalizeProgrammaticShortTrade(cfg ProgrammaticShortTradeConfig, fallbackPct float64) (ProgrammaticShortTradeProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	partialClosePct := cfg.PartialClosePct
	if partialClosePct <= 0 {
		partialClosePct = fallbackPct
	}
	if partialClosePct <= 0 || partialClosePct > 100 {
		return ProgrammaticShortTradeProfile{}, fmt.Errorf("position_management.short_trade.partial_close_pct必须在0-100之间: %.2f", partialClosePct)
	}
	return ProgrammaticShortTradeProfile{Enabled: enabled, PartialClosePct: partialClosePct}, nil
}

func normalizeProgrammaticPartialCloseGuard(cfg ProgrammaticPositionManagementConfig) (ProgrammaticPartialCloseGuardProfile, error) {
	cooldownMinutes := 15
	cooldownEnabled := true
	if cfg.PartialCloseCooldownMinutes != nil {
		cooldownMinutes = *cfg.PartialCloseCooldownMinutes
	}
	if cooldownMinutes < 0 || cooldownMinutes > 1440 {
		return ProgrammaticPartialCloseGuardProfile{}, fmt.Errorf("position_management.partial_close_cooldown_minutes必须在0-1440之间: %d", cooldownMinutes)
	}
	if cooldownMinutes == 0 {
		cooldownEnabled = false
	}
	maxCount := cfg.MaxPartialCloseCountPerPosition
	if maxCount <= 0 {
		maxCount = 2
	}
	if maxCount < 1 || maxCount > 10 {
		return ProgrammaticPartialCloseGuardProfile{}, fmt.Errorf("position_management.max_partial_close_count_per_position必须在1-10之间: %d", maxCount)
	}
	maxTotalRatio, err := normalizeHumanPercentRatio(cfg.MaxTotalPartialClosePct, 50, "position_management.max_total_partial_close_pct")
	if err != nil {
		return ProgrammaticPartialCloseGuardProfile{}, err
	}
	if maxTotalRatio <= 0 || maxTotalRatio > 1 {
		return ProgrammaticPartialCloseGuardProfile{}, fmt.Errorf("position_management.max_total_partial_close_pct必须在1-100之间: %.2f", cfg.MaxTotalPartialClosePct)
	}
	return ProgrammaticPartialCloseGuardProfile{
		CooldownMinutes:     cooldownMinutes,
		MaxCountPerPosition: maxCount,
		MaxTotalRatio:       maxTotalRatio,
		CooldownEnabled:     cooldownEnabled,
	}, nil
}

func normalizeHumanPercentValue(value, fallback float64, field string) (float64, error) {
	if value == 0 {
		return fallback, nil
	}
	if value < 0 {
		return 0, fmt.Errorf("%s不能为负数: %.4f", field, value)
	}
	if value > 500 {
		return 0, fmt.Errorf("%s过大: %.4f", field, value)
	}
	return value, nil
}

func normalizeHumanPercentRatio(value, fallbackPct float64, field string) (float64, error) {
	pct, err := normalizeHumanPercentValue(value, fallbackPct, field)
	if err != nil {
		return 0, err
	}
	if pct > 100 {
		return 0, fmt.Errorf("%s不能超过100%%: %.4f", field, pct)
	}
	return pct / 100, nil
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

func normalizeProgrammaticSignalFreshness(cfg ProgrammaticSignalFreshnessConfig, fallbackMinRR float64) (ProgrammaticSignalFreshnessProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	missedTargetGuard := true
	if cfg.MissedTargetGuard != nil {
		missedTargetGuard = *cfg.MissedTargetGuard
	}
	softAge := cfg.SoftAgeCandles
	if softAge <= 0 {
		softAge = 2
	}
	maxLifetime := cfg.MaxLifetimeCandles
	if maxLifetime <= 0 {
		maxLifetime = 4
	}
	if softAge < 1 || softAge > 48 {
		return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.soft_age_candles必须在1-48之间: %d", softAge)
	}
	if maxLifetime < softAge || maxLifetime > 96 {
		return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.max_lifetime_candles必须在soft_age_candles和96之间: %d", maxLifetime)
	}
	softByType, err := normalizeSignalAgeOverrides(cfg.SoftAgeBySignalType, softAge, 48, "signal_freshness.soft_age_by_signal_type")
	if err != nil {
		return ProgrammaticSignalFreshnessProfile{}, err
	}
	maxByType, err := normalizeSignalAgeOverrides(cfg.MaxLifetimeBySignalType, maxLifetime, 96, "signal_freshness.max_lifetime_by_signal_type")
	if err != nil {
		return ProgrammaticSignalFreshnessProfile{}, err
	}
	for signalType, soft := range softByType {
		if maxValue, ok := maxByType[signalType]; ok && maxValue < soft {
			return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.%s max_lifetime不能小于soft_age: %d < %d", signalType, maxValue, soft)
		}
	}
	decay := cfg.ConfidenceDecayPerAgedCandle
	if decay < 0 {
		return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.confidence_decay_per_aged_candle不能为负数: %d", decay)
	}
	if decay == 0 {
		decay = 3
	}
	if decay > 20 {
		return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.confidence_decay_per_aged_candle必须在0-20之间: %d", decay)
	}
	minRR := cfg.MinRemainingNetRR
	if minRR <= 0 {
		minRR = fallbackMinRR
	}
	if minRR <= 0 {
		minRR = defaultStrategyMinNetRR
	}
	if minRR < 1 {
		return ProgrammaticSignalFreshnessProfile{}, fmt.Errorf("signal_freshness.min_remaining_net_rr不能低于1: %.4f", minRR)
	}
	return ProgrammaticSignalFreshnessProfile{
		Enabled:                      enabled,
		SoftAgeCandles:               softAge,
		MaxLifetimeCandles:           maxLifetime,
		SoftAgeBySignalType:          softByType,
		MaxLifetimeBySignalType:      maxByType,
		MissedTargetGuard:            missedTargetGuard,
		ConfidenceDecayPerAgedCandle: decay,
		MinRemainingNetRR:            minRR,
	}, nil
}

func normalizeSignalAgeOverrides(values map[string]int, fallback, maxAllowed int, field string) (map[string]int, error) {
	result := map[string]int{
		"buy1":  fallback,
		"sell1": fallback,
		"buy2":  fallback,
		"sell2": fallback,
		"buy3":  fallback,
		"sell3": fallback,
	}
	if strings.Contains(field, "soft_age") {
		result["buy3"] = 1
		result["sell3"] = 1
	} else if strings.Contains(field, "max_lifetime") {
		if fallback > 2 {
			result["buy3"] = 2
			result["sell3"] = 2
		}
	}
	allowed := map[string]bool{"buy1": true, "buy2": true, "buy3": true, "sell1": true, "sell2": true, "sell3": true}
	for key, value := range values {
		signalType := strings.ToLower(strings.TrimSpace(key))
		if !allowed[signalType] {
			return nil, fmt.Errorf("%s包含不支持的信号类型: %q", field, key)
		}
		if value < 1 || value > maxAllowed {
			return nil, fmt.Errorf("%s.%s必须在1-%d之间: %d", field, signalType, maxAllowed, value)
		}
		result[signalType] = value
	}
	return result, nil
}

func normalizePilotConfidenceOverrides(values map[string]int) (map[string]int, error) {
	if len(values) == 0 {
		return nil, nil
	}
	allowed := map[string]bool{"buy1": true, "buy2": true, "buy3": true, "sell1": true, "sell2": true, "sell3": true}
	result := make(map[string]int, len(values))
	for key, value := range values {
		signalType := strings.ToLower(strings.TrimSpace(key))
		if !allowed[signalType] {
			return nil, fmt.Errorf("preview_signals.pilot_min_confidence_by_signal_type包含不支持的信号类型: %q", key)
		}
		if value < 1 || value > 100 {
			return nil, fmt.Errorf("preview_signals.pilot_min_confidence_by_signal_type.%s必须在1-100之间: %d", signalType, value)
		}
		result[signalType] = value
	}
	return result, nil
}

func normalizeProgrammaticPreviewSignals(cfg ProgrammaticPreviewSignalsConfig, timeframes ProgrammaticTimeframesProfile, defectFixPackEnabled bool) (ProgrammaticPreviewSignalsProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	componentTF := defaultString(cfg.ComponentTimeframe, timeframes.Sub)
	tradeTF := defaultString(cfg.TradeTimeframe, timeframes.Trade)
	if componentTF == "" {
		componentTF = defaultSubTimeframeForTrade(tradeTF)
	}
	if !isSupportedProgrammaticTimeframe(componentTF) {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.component_timeframe必须是 3m、15m、1h 或 4h: %q", componentTF)
	}
	if !isSupportedProgrammaticTradeTimeframe(tradeTF) {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.trade_timeframe必须是 15m、1h 或 4h: %q", tradeTF)
	}
	watchAfter := cfg.WatchAfterClosedComponents
	if watchAfter <= 0 {
		watchAfter = 2
	}
	pilotAfter := cfg.PilotAfterClosedComponents
	if pilotAfter <= 0 {
		pilotAfter = 3
	}
	if watchAfter < 1 || watchAfter > 16 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.watch_after_closed_components必须在1-16之间: %d", watchAfter)
	}
	if pilotAfter < watchAfter || pilotAfter > 16 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.pilot_after_closed_components必须在watch_after和16之间: %d", pilotAfter)
	}
	pilotRisk := cfg.PilotRiskFraction
	if pilotRisk <= 0 {
		pilotRisk = 0.3
	}
	if pilotRisk <= 0 || pilotRisk > 1 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.pilot_risk_fraction必须在0-1之间: %.4f", pilotRisk)
	}
	if pilotRisk > 0.4 {
		log.Printf("⚠️ preview_signals.pilot_risk_fraction=%.2f 偏高，建议≤0.40", pilotRisk)
	}
	pilotConfidence := cfg.PilotMinConfidence
	if pilotConfidence <= 0 {
		if defectFixPackEnabled {
			pilotConfidence = 70
		} else {
			pilotConfidence = 90
		}
	}
	if pilotConfidence < 1 || pilotConfidence > 100 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.pilot_min_confidence必须在1-100之间: %d", pilotConfidence)
	}
	pilotBySignal, err := normalizePilotConfidenceOverrides(cfg.PilotMinConfidenceBySignal)
	if err != nil {
		return ProgrammaticPreviewSignalsProfile{}, err
	}
	useP75 := defectFixPackEnabled
	if cfg.PilotMinConfidenceUseP75 != nil {
		useP75 = *cfg.PilotMinConfidenceUseP75
	}
	p75Floor := cfg.P75Floor
	if p75Floor <= 0 {
		p75Floor = 65
	}
	p75Ceiling := cfg.P75Ceiling
	if p75Ceiling <= 0 {
		p75Ceiling = 85
	}
	if p75Floor < 1 || p75Floor > 100 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.pilot_min_confidence_p75_floor必须在1-100之间: %d", p75Floor)
	}
	if p75Ceiling < p75Floor || p75Ceiling > 100 {
		return ProgrammaticPreviewSignalsProfile{}, fmt.Errorf("preview_signals.pilot_min_confidence_p75_ceiling必须在floor和100之间: %d", p75Ceiling)
	}
	requireUpgrade := true
	if cfg.RequireConfirmedUpgrade != nil {
		requireUpgrade = *cfg.RequireConfirmedUpgrade
	}
	return ProgrammaticPreviewSignalsProfile{
		Enabled:                    enabled,
		ComponentTimeframe:         componentTF,
		TradeTimeframe:             tradeTF,
		WatchAfterClosedComponents: watchAfter,
		PilotAfterClosedComponents: pilotAfter,
		AllowPilotOpen:             cfg.AllowPilotOpen,
		PilotRiskFraction:          pilotRisk,
		PilotMinConfidence:         pilotConfidence,
		PilotMinConfidenceBySignal: pilotBySignal,
		PilotMinConfidenceUseP75:   useP75,
		P75Floor:                   p75Floor,
		P75Ceiling:                 p75Ceiling,
		RequireConfirmedUpgrade:    requireUpgrade,
	}, nil
}

func normalizeProgrammaticEntryTiming(cfg ProgrammaticEntryTimingConfig, timeframes ProgrammaticTimeframesProfile, fallbackMinRR float64, defectFixPackEnabled bool) (ProgrammaticEntryTimingProfile, error) {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	requireFreshTrigger := true
	if cfg.RequireFreshTrigger != nil {
		requireFreshTrigger = *cfg.RequireFreshTrigger
	}
	triggerTF := defaultString(cfg.TriggerTimeframe, timeframes.Sub)
	if triggerTF == "" {
		triggerTF = defaultSubTimeframeForTrade(timeframes.Trade)
	}
	if !isSupportedProgrammaticTimeframe(triggerTF) {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.trigger_timeframe必须是 3m、15m、1h 或 4h: %q", triggerTF)
	}
	directAge := cfg.DirectOpenMaxAgeCandles
	if directAge < 0 || directAge > 16 {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.direct_open_max_age_candles必须在0-16之间: %d", directAge)
	}
	directOpen := defectFixPackEnabled
	if cfg.DirectStructureOpenConfigured || cfg.DirectStructureOpen {
		directOpen = cfg.DirectStructureOpen
	}
	directMinConfidence := cfg.DirectStructureMinConfidence
	if directMinConfidence <= 0 {
		directMinConfidence = 70
	}
	if directMinConfidence < 1 || directMinConfidence > 100 {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.direct_structure_min_confidence必须在1-100之间: %d", directMinConfidence)
	}
	allowed, err := normalizeEntryTriggerTypes(cfg.AllowedTriggerTypes)
	if err != nil {
		return ProgrammaticEntryTimingProfile{}, err
	}
	entryZone, err := normalizeProgrammaticEntryZone(cfg.EntryZone, fallbackMinRR, defectFixPackEnabled)
	if err != nil {
		return ProgrammaticEntryTimingProfile{}, err
	}
	maxNoTriggerSubCandles := cfg.MaxNoTriggerSubCandles
	if maxNoTriggerSubCandles <= 0 {
		maxNoTriggerSubCandles = 3
	}
	if maxNoTriggerSubCandles < 1 || maxNoTriggerSubCandles > 16 {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.max_no_trigger_sub_candles必须在1-16之间: %d", maxNoTriggerSubCandles)
	}
	maxTriggerAge := cfg.MaxTriggerAgeCandles
	if maxTriggerAge <= 0 {
		maxTriggerAge = 1
	}
	if maxTriggerAge < 1 || maxTriggerAge > 16 {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.max_trigger_age_candles必须在1-16之间: %d", maxTriggerAge)
	}
	minConfidence := cfg.MinTriggerConfidence
	if minConfidence < 0 || minConfidence > 100 {
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.min_trigger_confidence必须在0-100之间: %d", minConfidence)
	}
	pilot, err := normalizeProgrammaticEntryPilot(cfg.Pilot, defectFixPackEnabled)
	if err != nil {
		return ProgrammaticEntryTimingProfile{}, err
	}
	continuation := strings.TrimSpace(strings.ToLower(cfg.ContinuationAfterTargetCrossed))
	if continuation == "" {
		continuation = "disabled"
	}
	switch continuation {
	case "disabled", "report_only", "separate_module":
	default:
		return ProgrammaticEntryTimingProfile{}, fmt.Errorf("entry_timing.continuation_after_target_crossed必须是 disabled、report_only 或 separate_module: %q", continuation)
	}
	return ProgrammaticEntryTimingProfile{
		Enabled:                        enabled,
		DirectStructureOpen:            directOpen,
		DirectStructureMinConfidence:   directMinConfidence,
		DirectOpenMaxAgeCandles:        directAge,
		MaxNoTriggerSubCandles:         maxNoTriggerSubCandles,
		RequireFreshTrigger:            requireFreshTrigger,
		TriggerTimeframe:               triggerTF,
		AllowedTriggerTypes:            allowed,
		EntryZone:                      entryZone,
		MaxTriggerAgeCandles:           maxTriggerAge,
		MinTriggerConfidence:           minConfidence,
		Pilot:                          pilot,
		ContinuationAfterTargetCrossed: continuation,
	}, nil
}

func normalizeEntryTriggerTypes(values []string) ([]string, error) {
	allowed := map[string]bool{
		"new_structure_segment":   true,
		"preview_2x15m_watchlist": true,
		"preview_3x15m_pilot":     true,
		"pullback_retest_resume":  true,
		"breakout_continuation":   true,
		"confirmed_1h_upgrade":    true,
	}
	if len(values) == 0 {
		return []string{"preview_2x15m_watchlist", "preview_3x15m_pilot", "pullback_retest_resume"}, nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		kind := strings.TrimSpace(strings.ToLower(value))
		if !allowed[kind] {
			return nil, fmt.Errorf("entry_timing.allowed_trigger_types包含不支持的类型: %q", value)
		}
		if !seen[kind] {
			seen[kind] = true
			result = append(result, kind)
		}
	}
	sort.Strings(result)
	return result, nil
}

func normalizeProgrammaticEntryZone(cfg ProgrammaticEntryZoneConfig, fallbackMinRR float64, defectFixPackEnabled bool) (ProgrammaticEntryZoneProfile, error) {
	mode := strings.TrimSpace(strings.ToLower(cfg.Mode))
	if mode == "" {
		mode = "structure_range"
	}
	switch mode {
	case "structure_range", "atr":
	default:
		return ProgrammaticEntryZoneProfile{}, fmt.Errorf("entry_timing.entry_zone.mode必须是 structure_range 或 atr: %q", mode)
	}
	maxChase := cfg.MaxChaseRatio
	if maxChase <= 0 {
		maxChase = 0.35
	}
	if maxChase <= 0 || maxChase > 1 {
		return ProgrammaticEntryZoneProfile{}, fmt.Errorf("entry_timing.entry_zone.max_chase_ratio必须在0-1之间: %.4f", maxChase)
	}
	minRR := cfg.MinRemainingNetRR
	if minRR <= 0 {
		if defectFixPackEnabled {
			minRR = 2.0
		} else {
			minRR = fallbackMinRR
		}
	}
	if minRR <= 0 {
		minRR = defaultStrategyMinNetRR
	}
	if minRR < 1 {
		return ProgrammaticEntryZoneProfile{}, fmt.Errorf("entry_timing.entry_zone.min_remaining_net_rr不能低于1: %.4f", minRR)
	}
	maxChaseATR := cfg.MaxChaseATRMultiplier
	if maxChaseATR <= 0 {
		maxChaseATR = 0.6
	}
	if maxChaseATR < 0 || maxChaseATR > 10 {
		return ProgrammaticEntryZoneProfile{}, fmt.Errorf("entry_timing.entry_zone.max_chase_atr_multiplier必须在0-10之间: %.4f", maxChaseATR)
	}
	freshRelax := cfg.FreshAgeChaseRelax
	if freshRelax <= 0 {
		freshRelax = 0.10
	}
	if freshRelax < 0 || freshRelax > 1 {
		return ProgrammaticEntryZoneProfile{}, fmt.Errorf("entry_timing.entry_zone.fresh_age_chase_relax必须在0-1之间: %.4f", freshRelax)
	}
	signalTypeRR, err := normalizeSignalTypeMinRR(cfg.SignalTypeMinRR, defectFixPackEnabled)
	if err != nil {
		return ProgrammaticEntryZoneProfile{}, err
	}
	tierOverrides, err := normalizeProgrammaticEntryZoneOverridesWithOptions(cfg.TierOverrides, "entry_timing.entry_zone.tier_overrides", false)
	if err != nil {
		return ProgrammaticEntryZoneProfile{}, err
	}
	overrides, err := normalizeProgrammaticEntryZoneOverrides(cfg.SymbolOverrides)
	if err != nil {
		return ProgrammaticEntryZoneProfile{}, err
	}
	theoreticalSkip := defectFixPackEnabled
	if cfg.TheoreticalRRUnreachableSkip != nil {
		theoreticalSkip = *cfg.TheoreticalRRUnreachableSkip
	}
	return ProgrammaticEntryZoneProfile{
		Mode:                         mode,
		MaxChaseRatio:                maxChase,
		MinRemainingNetRR:            minRR,
		MaxChaseATRMultiplier:        maxChaseATR,
		FreshAgeChaseRelax:           freshRelax,
		SignalTypeMinRR:              signalTypeRR,
		TierOverrides:                tierOverrides,
		SymbolOverrides:              overrides,
		TheoreticalRRUnreachableSkip: theoreticalSkip,
	}, nil
}

func normalizeProgrammaticEntryZoneOverrides(values map[string]ProgrammaticEntryZoneOverrideConfig) (map[string]ProgrammaticEntryZoneOverrideProfile, error) {
	return normalizeProgrammaticEntryZoneOverridesWithOptions(values, "entry_timing.entry_zone.symbol_overrides", true)
}

func normalizeProgrammaticEntryZoneOverridesWithOptions(values map[string]ProgrammaticEntryZoneOverrideConfig, field string, requireSymbol bool) (map[string]ProgrammaticEntryZoneOverrideProfile, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]ProgrammaticEntryZoneOverrideProfile, len(values))
	for rawKey, override := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if requireSymbol {
			key = normalizeProgrammaticSymbol(rawKey)
			if key == "" || !programmaticSymbolPattern.MatchString(key) {
				return nil, fmt.Errorf("%s包含无效symbol: %q", field, rawKey)
			}
		} else if key == "" {
			return nil, fmt.Errorf("%s包含空key", field)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("%s重复key: %s", field, key)
		}
		if override.MaxChaseRatio < 0 || override.MaxChaseRatio > 1 {
			return nil, fmt.Errorf("%s[%s].max_chase_ratio必须在0-1之间: %.4f", field, key, override.MaxChaseRatio)
		}
		if override.MinRemainingNetRR > 0 && override.MinRemainingNetRR < 1 {
			return nil, fmt.Errorf("%s[%s].min_remaining_net_rr不能低于1: %.4f", field, key, override.MinRemainingNetRR)
		}
		result[key] = ProgrammaticEntryZoneOverrideProfile{
			MaxChaseRatio:     override.MaxChaseRatio,
			MinRemainingNetRR: override.MinRemainingNetRR,
		}
	}
	return result, nil
}

func normalizeSignalTypeMinRR(values map[string]float64, defectFixPackEnabled bool) (map[string]float64, error) {
	result := map[string]float64{}
	if defectFixPackEnabled {
		result["buy1@1h"] = 2.0
		result["sell1@1h"] = 2.0
		result["buy2@1h"] = 1.6
		result["sell2@1h"] = 1.6
		result["buy3@1h"] = 1.4
		result["sell3@1h"] = 1.4
		result["buy1"] = 2.0
		result["sell1"] = 2.0
		result["buy2"] = 1.6
		result["sell2"] = 1.6
		result["buy3"] = 1.4
		result["sell3"] = 1.4
	}
	if len(values) == 0 {
		if len(result) == 0 {
			return nil, nil
		}
		return result, nil
	}
	for rawKey, value := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			return nil, fmt.Errorf("entry_timing.entry_zone.signal_type_min_rr包含空key")
		}
		if !isValidSignalTypeMinRRKey(key) {
			return nil, fmt.Errorf("entry_timing.entry_zone.signal_type_min_rr包含不支持的key: %q", rawKey)
		}
		if value < 1 || value > 20 {
			return nil, fmt.Errorf("entry_timing.entry_zone.signal_type_min_rr[%s]必须在1-20之间: %.4f", key, value)
		}
		result[key] = value
	}
	return result, nil
}

func isValidSignalTypeMinRRKey(key string) bool {
	if strings.HasSuffix(key, "@1h") || strings.HasSuffix(key, "@15m") || strings.HasSuffix(key, "@4h") {
		parts := strings.Split(key, "@")
		return len(parts) == 2 && isValidProgrammaticSignalTypePattern(parts[0])
	}
	return isValidProgrammaticSignalTypePattern(key)
}

func isValidProgrammaticSignalType(signalType string) bool {
	switch signalType {
	case "buy1", "buy2", "buy3", "sell1", "sell2", "sell3":
		return true
	default:
		return false
	}
}

func isValidProgrammaticSignalTypePattern(signalType string) bool {
	switch signalType {
	case "*", "buy*", "sell*":
		return true
	default:
		return isValidProgrammaticSignalType(signalType)
	}
}

func normalizeProgrammaticEntryPilot(cfg ProgrammaticEntryPilotConfig, defectFixPackEnabled bool) (ProgrammaticEntryPilotProfile, error) {
	risk := cfg.RiskFraction
	if risk <= 0 {
		risk = 0.3
	}
	if risk <= 0 || risk > 1 {
		return ProgrammaticEntryPilotProfile{}, fmt.Errorf("entry_timing.pilot.risk_fraction必须在0-1之间: %.4f", risk)
	}
	if risk > 0.4 {
		log.Printf("⚠️ entry_timing.pilot.risk_fraction=%.2f 偏高，建议≤0.40", risk)
	}
	confidence := cfg.MinConfidence
	if confidence <= 0 {
		if defectFixPackEnabled {
			confidence = 70
		} else {
			confidence = 90
		}
	}
	if confidence < 1 || confidence > 100 {
		return ProgrammaticEntryPilotProfile{}, fmt.Errorf("entry_timing.pilot.min_confidence必须在1-100之间: %d", confidence)
	}
	return ProgrammaticEntryPilotProfile{Enabled: cfg.Enabled, RiskFraction: risk, MinConfidence: confidence}, nil
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
	if symbol != "" && !strings.HasSuffix(symbol, "USDT") && !strings.HasSuffix(symbol, "USDC") {
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

func isSupportedProgrammaticTradeTimeframe(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "15m", "1h", "4h":
		return true
	default:
		return false
	}
}
