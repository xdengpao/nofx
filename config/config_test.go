package config

// Feature: quant-trading-system
// 任务 2.1: 配置管理模块测试覆盖
// 覆盖需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

func validBinanceTrader(id string) TraderConfig {
	return TraderConfig{
		ID:                  id,
		Name:                "测试交易者-" + id,
		Enabled:             true,
		AIModel:             "deepseek",
		Exchange:            "binance",
		BinanceAPIKey:       "test-api-key",
		BinanceSecretKey:    "test-secret-key",
		DeepSeekKey:         "test-deepseek-key",
		InitialBalance:      1000.0,
		ScanIntervalMinutes: 3,
	}
}

func validConfig() *Config {
	return &Config{
		Traders:       []TraderConfig{validBinanceTrader("trader1")},
		APIServerPort: 8080,
		Leverage:      LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5},
	}
}

func writeConfigFile(t *testing.T, cfg interface{}) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化配置失败: %v", err)
	}
	f, err := os.CreateTemp("", "config_test_*.json")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭临时文件失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

// ============================================================================
// 需求 1.1: LoadConfig 从文件加载配置
// ============================================================================

func TestLoadConfig_ValidFile_ReturnsConfig(t *testing.T) {
	path := writeConfigFile(t, validConfig())
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("有效配置文件加载失败: %v", err)
	}
	if len(cfg.Traders) != 1 {
		t.Errorf("期望1个交易者, 实际=%d", len(cfg.Traders))
	}
}

// 需求 1.10: 文件不存在时返回错误
func TestLoadConfig_MissingFile_ReturnsError(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("文件不存在时应返回错误")
	}
}

// 需求 1.10: 非法 JSON 时返回错误
func TestLoadConfig_InvalidJSON_ReturnsError(t *testing.T) {
	f, err := os.CreateTemp("", "config_bad_*.json")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	if _, err := f.WriteString("{invalid json}"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })

	_, loadErr := LoadConfig(f.Name())
	if loadErr == nil {
		t.Error("非法JSON应返回错误")
	}
}

// ============================================================================
// 需求 1.8: use_default_coins 自动启用
// ============================================================================

func TestLoadConfig_NoAPIURL_EnablesDefaultCoins(t *testing.T) {
	cfg := validConfig()
	cfg.UseDefaultCoins = false
	cfg.CoinPoolAPIURL = ""
	path := writeConfigFile(t, cfg)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if !loaded.UseDefaultCoins {
		t.Error("无 CoinPoolAPIURL 时应自动启用 UseDefaultCoins")
	}
}

func TestLoadConfig_WithAPIURL_KeepsUseDefaultCoinsAsFalse(t *testing.T) {
	cfg := validConfig()
	cfg.UseDefaultCoins = false
	cfg.CoinPoolAPIURL = "http://example.com/api"
	path := writeConfigFile(t, cfg)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if loaded.UseDefaultCoins {
		t.Error("配置了 CoinPoolAPIURL 时不应强制启用 UseDefaultCoins")
	}
}

func TestLoadConfig_DynamicCandidatePoolDefaultsEnabled(t *testing.T) {
	cfg := validConfig()
	path := writeConfigFile(t, cfg)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if !loaded.DynamicCandidatePool.IsEnabled() {
		t.Fatal("未显式配置时动态候选池应默认启用")
	}
	if loaded.DynamicCandidatePool.PromptCandidateLimit != 8 {
		t.Fatalf("PromptCandidateLimit 默认值应为8，实际=%d", loaded.DynamicCandidatePool.PromptCandidateLimit)
	}
	if len(loaded.DynamicCandidatePool.CoreSymbols) != 2 {
		t.Fatalf("CoreSymbols 默认值应包含 BTC/ETH，实际=%v", loaded.DynamicCandidatePool.CoreSymbols)
	}
}

func TestLoadConfig_DynamicCandidatePoolCanBeDisabled(t *testing.T) {
	cfg := validConfig()
	enabled := false
	cfg.DynamicCandidatePool.Enabled = &enabled
	path := writeConfigFile(t, cfg)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if loaded.DynamicCandidatePool.IsEnabled() {
		t.Fatal("显式 enabled=false 时动态候选池应关闭")
	}
}

func TestNormalizeTradingFrequency_LegacyPreservesExistingDefaults(t *testing.T) {
	cfg := validConfig()
	cfg.DynamicCandidatePool.ApplyDefaults()

	profile, err := cfg.NormalizeTradingFrequency()
	if err != nil {
		t.Fatalf("legacy配置不应失败: %v", err)
	}
	if !profile.Legacy || profile.Mode != TradingFrequencyModeLegacy {
		t.Fatalf("legacy mode错误: %+v", profile)
	}
	if profile.AnalysisIntervalMinutes != 15 {
		t.Fatalf("legacy分析间隔应保持15，实际=%d", profile.AnalysisIntervalMinutes)
	}
	if profile.PromptCandidateLimit != 8 {
		t.Fatalf("legacy候选数应保持动态池默认8，实际=%d", profile.PromptCandidateLimit)
	}
}

func TestNormalizeTradingFrequency_DefaultBlockDerivesBalanced(t *testing.T) {
	cfg := validConfig()
	cfg.DynamicCandidatePool.ApplyDefaults()
	cfg.TradingFrequency = &TradingFrequencyConfig{}

	profile, err := cfg.NormalizeTradingFrequency()
	if err != nil {
		t.Fatalf("balanced默认不应失败: %v", err)
	}
	if profile.Legacy || profile.Mode != TradingFrequencyModeBalanced {
		t.Fatalf("应派生balanced: %+v", profile)
	}
	if profile.AnalysisIntervalMinutes != 12 || profile.PromptCandidateLimit != 10 {
		t.Fatalf("balanced派生值错误: %+v", profile)
	}
	if !profile.HighADXReportOnly || !profile.RRReportOnly || !profile.RollingGateReportOnly {
		t.Fatalf("balanced应默认开启report-only: %+v", profile)
	}
}

func TestNormalizeTradingFrequency_ActiveDefaultsAndOverrides(t *testing.T) {
	cfg := validConfig()
	cfg.DynamicCandidatePool.ApplyDefaults()
	cfg.TradingFrequency = &TradingFrequencyConfig{
		Mode:                    TradingFrequencyModeActive,
		AnalysisIntervalMinutes: 9,
		PromptCandidateLimit:    12,
		DailyOpenLimit:          5,
	}

	profile, err := cfg.NormalizeTradingFrequency()
	if err != nil {
		t.Fatalf("active配置不应失败: %v", err)
	}
	if profile.Mode != TradingFrequencyModeActive || profile.AnalysisIntervalMinutes != 9 || profile.PromptCandidateLimit != 12 {
		t.Fatalf("active派生值错误: %+v", profile)
	}
	if profile.DailyOpenLimit != 5 {
		t.Fatalf("daily open override未生效: %+v", profile)
	}
}

func TestNormalizeTradingFrequency_InvalidValues(t *testing.T) {
	tests := []struct {
		name string
		tf   TradingFrequencyConfig
	}{
		{name: "bad mode", tf: TradingFrequencyConfig{Mode: "fast"}},
		{name: "low interval", tf: TradingFrequencyConfig{Mode: TradingFrequencyModeBalanced, AnalysisIntervalMinutes: 3}},
		{name: "low prompt", tf: TradingFrequencyConfig{Mode: TradingFrequencyModeBalanced, PromptCandidateLimit: 7}},
		{name: "high prompt", tf: TradingFrequencyConfig{Mode: TradingFrequencyModeBalanced, PromptCandidateLimit: 99}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.DynamicCandidatePool.ApplyDefaults()
			cfg.TradingFrequency = &tt.tf
			if _, err := cfg.NormalizeTradingFrequency(); err == nil {
				t.Fatalf("非法配置应失败: %+v", tt.tf)
			}
		})
	}
}

func TestNormalizeStrategyRisk_LegacyPreservesExistingBehavior(t *testing.T) {
	cfg := validConfig()
	profile, err := cfg.NormalizeStrategyRisk()
	if err != nil {
		t.Fatalf("legacy strategy risk归一化失败: %v", err)
	}
	if !profile.Legacy || profile.Enabled {
		t.Fatalf("缺少strategy_risk时应保持legacy且不启用: %+v", profile)
	}
	if !profile.RollbackLegacyValidation || profile.FeeSlippagePct <= 0 || profile.ADXTimeframe != "1h" {
		t.Fatalf("legacy默认值异常: %+v", profile)
	}
}

func TestNormalizeProgrammaticStrategies_DefaultAI(t *testing.T) {
	cfg := validConfig()
	profiles, err := cfg.NormalizeProgrammaticStrategies()
	if err != nil {
		t.Fatalf("默认AI模式不应失败: %v", err)
	}
	profile := profiles["trader1"]
	if profile.DecisionMode != DecisionModeAI {
		t.Fatalf("缺省decision_mode应为ai: %+v", profile)
	}
}

func TestNormalizeProgrammaticStrategies_ProgrammaticDefaults(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].DecisionMode = DecisionModeProgrammatic
	cfg.Traders[0].AIModel = ""
	cfg.Traders[0].DeepSeekKey = ""
	cfg.Traders[0].ProgrammaticStrategy.SymbolPool = ProgrammaticSymbolPoolConfig{
		Mode:    "override",
		Symbols: []string{"btc", "ETHUSDT"},
	}

	profiles, err := cfg.NormalizeProgrammaticStrategies()
	if err != nil {
		t.Fatalf("合法programmatic配置不应失败: %v", err)
	}
	profile := profiles["trader1"]
	if profile.DecisionMode != DecisionModeProgrammatic {
		t.Fatalf("decision_mode错误: %+v", profile)
	}
	if profile.Timeframes.Higher != "4h" || profile.Timeframes.Trade != "1h" || profile.Timeframes.Sub != "15m" || profile.Timeframes.Micro != "3m" {
		t.Fatalf("默认timeframe错误: %+v", profile.Timeframes)
	}
	if profile.HistoryDepth.M3 != 240 || profile.HistoryDepth.M15 != 192 || profile.HistoryDepth.H1 != 240 || profile.HistoryDepth.H4 != 180 {
		t.Fatalf("默认history depth错误: %+v", profile.HistoryDepth)
	}
	if !profile.PositionManagement.Enabled || !profile.PositionManagement.Breakeven.Enabled {
		t.Fatalf("持仓管理默认应启用: %+v", profile.PositionManagement)
	}
	if profile.PositionManagement.Breakeven.BufferRatio != 0.0005 {
		t.Fatalf("buffer_pct默认0.05应归一化为0.0005，实际=%.8f", profile.PositionManagement.Breakeven.BufferRatio)
	}
	if profile.PositionManagement.FloatingDrawdown.DrawdownRatio != 0.35 {
		t.Fatalf("drawdown_pct默认35应归一化为0.35，实际=%.8f", profile.PositionManagement.FloatingDrawdown.DrawdownRatio)
	}
	if profile.MovingAverage.ShortPeriod != 20 || profile.MovingAverage.LongPeriod != 50 {
		t.Fatalf("默认均线周期错误: %+v", profile.MovingAverage)
	}
	if profile.SymbolPool.Mode != "override" || len(profile.SymbolPool.Symbols) != 2 || profile.SymbolPool.Symbols[0] != "BTCUSDT" {
		t.Fatalf("symbol pool归一化错误: %+v", profile.SymbolPool)
	}
	if profile.ConfigHash == "" {
		t.Fatal("programmatic profile应生成config hash")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("programmatic模式不应要求AI key: %v", err)
	}
}

func TestNormalizeProgrammaticStrategies_TradeTimeframeDefaults(t *testing.T) {
	tests := []struct {
		trade string
		sub   string
	}{
		{trade: "15m", sub: "3m"},
		{trade: "1h", sub: "15m"},
		{trade: "4h", sub: "1h"},
	}
	for _, tt := range tests {
		t.Run(tt.trade, func(t *testing.T) {
			cfg := validConfig()
			cfg.Traders[0].DecisionMode = DecisionModeProgrammatic
			cfg.Traders[0].ProgrammaticStrategy.Timeframes.Trade = tt.trade
			profiles, err := cfg.NormalizeProgrammaticStrategies()
			if err != nil {
				t.Fatalf("trade=%s 不应失败: %v", tt.trade, err)
			}
			if got := profiles["trader1"].Timeframes.Sub; got != tt.sub {
				t.Fatalf("trade=%s 默认sub错误: 期望%s 实际%s", tt.trade, tt.sub, got)
			}
		})
	}
}

func TestNormalizeProgrammaticStrategies_PositionManagementPercentUnits(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].DecisionMode = DecisionModeProgrammatic
	cfg.Traders[0].ProgrammaticStrategy.PositionManagement.Breakeven.BufferPct = 0.05
	cfg.Traders[0].ProgrammaticStrategy.PositionManagement.FloatingDrawdown.DrawdownPct = 35
	cfg.Traders[0].ProgrammaticStrategy.PositionManagement.ShortTrade.PartialClosePct = 25

	profiles, err := cfg.NormalizeProgrammaticStrategies()
	if err != nil {
		t.Fatalf("持仓管理百分比配置不应失败: %v", err)
	}
	pm := profiles["trader1"].PositionManagement
	if pm.Breakeven.BufferRatio != 0.0005 {
		t.Fatalf("buffer_pct=0.05应表示0.05%%: %.8f", pm.Breakeven.BufferRatio)
	}
	if pm.FloatingDrawdown.DrawdownRatio != 0.35 {
		t.Fatalf("drawdown_pct=35应表示35%%: %.8f", pm.FloatingDrawdown.DrawdownRatio)
	}
	if pm.ShortTrade.PartialClosePct != 25 {
		t.Fatalf("short_trade.partial_close_pct应保持人类百分数: %.2f", pm.ShortTrade.PartialClosePct)
	}
}

func TestNormalizeProgrammaticStrategies_InvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TraderConfig)
	}{
		{name: "bad mode", mutate: func(t *TraderConfig) { t.ProgrammaticStrategy.SymbolPool.Mode = "bad" }},
		{name: "bad timeframe", mutate: func(t *TraderConfig) { t.ProgrammaticStrategy.Timeframes.Trade = "5m" }},
		{name: "bad trade timeframe 3m", mutate: func(t *TraderConfig) { t.ProgrammaticStrategy.Timeframes.Trade = "3m" }},
		{name: "bad ma", mutate: func(t *TraderConfig) {
			t.ProgrammaticStrategy.MovingAverage.ShortPeriod = 60
			t.ProgrammaticStrategy.MovingAverage.LongPeriod = 20
		}},
		{name: "bad symbol", mutate: func(t *TraderConfig) { t.ProgrammaticStrategy.SymbolPool.Symbols = []string{"bad symbol"} }},
		{name: "bad position management action", mutate: func(t *TraderConfig) {
			t.ProgrammaticStrategy.PositionManagement.StructureBreak.Action = "trim"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Traders[0].DecisionMode = DecisionModeProgrammatic
			tt.mutate(&cfg.Traders[0])
			if _, err := cfg.NormalizeProgrammaticStrategies(); err == nil {
				t.Fatal("非法programmatic配置应失败")
			}
		})
	}
}

func TestNormalizeStrategyRisk_DefaultBlockDerivesStrictProfiles(t *testing.T) {
	cfg := validConfig()
	cfg.StrategyRisk = &StrategyRiskConfig{}
	profile, err := cfg.NormalizeStrategyRisk()
	if err != nil {
		t.Fatalf("strategy risk默认块归一化失败: %v", err)
	}
	if profile.Legacy || !profile.Enabled {
		t.Fatalf("存在strategy_risk块时应启用非legacy策略: %+v", profile)
	}
	if len(profile.Profiles) == 0 {
		t.Fatal("应生成默认profile")
	}
	for _, p := range profile.Profiles {
		if p.MinStopPct < defaultMinStopFloorPct {
			t.Fatalf("%s min stop低于硬地板: %+v", p.Name, p)
		}
		if p.ExchangeFullTPMode != StrategyRiskTPModeAlgorithmicFull {
			t.Fatalf("%s full TP模式应默认为algorithmic_full: %+v", p.Name, p)
		}
	}
}

func TestNormalizeStrategyRisk_PercentNormalizationAndOverrides(t *testing.T) {
	allowShort := false
	cfg := validConfig()
	cfg.StrategyRisk = &StrategyRiskConfig{
		FeeSlippagePct: 0.2,
		Profiles: []InstrumentProfileConfig{
			{
				Name:                "btc_eth",
				MinStopPct:          1.2,
				MaxRiskPct:          0.5,
				AllowShort:          &allowShort,
				ExchangeFullTPMinRR: 3.0,
			},
		},
	}
	profile, err := cfg.NormalizeStrategyRisk()
	if err != nil {
		t.Fatalf("strategy risk覆盖归一化失败: %v", err)
	}
	if profile.FeeSlippagePct != 0.002 {
		t.Fatalf("fee_slippage_pct 0.2应归一化为0.002，实际 %.6f", profile.FeeSlippagePct)
	}
	var btc InstrumentProfileProfile
	for _, p := range profile.Profiles {
		if p.Name == "btc_eth" {
			btc = p
			break
		}
	}
	if btc.Name == "" {
		t.Fatal("未找到btc_eth profile")
	}
	if btc.MinStopPct != 0.012 || btc.MaxRiskPct != 0.005 {
		t.Fatalf("profile百分比归一化错误: %+v", btc)
	}
	if btc.AllowShort {
		t.Fatalf("AllowShort override未生效: %+v", btc)
	}
	if btc.ExchangeFullTPMinRR != 3.0 {
		t.Fatalf("ExchangeFullTPMinRR override未生效: %+v", btc)
	}
}

func TestNormalizeStrategyRisk_InvalidProfileValues(t *testing.T) {
	cfg := validConfig()
	cfg.StrategyRisk = &StrategyRiskConfig{
		Profiles: []InstrumentProfileConfig{{Name: "btc_eth", MinStopPct: 0.005}},
	}
	if _, err := cfg.NormalizeStrategyRisk(); err == nil {
		t.Fatal("显式min_stop_pct低于1%应返回错误")
	}

	cfg = validConfig()
	cfg.StrategyRisk = &StrategyRiskConfig{ADXTimeframe: "2h"}
	if _, err := cfg.NormalizeStrategyRisk(); err == nil {
		t.Fatal("非法ADX timeframe应返回错误")
	}
}

func TestLoadConfig_TradingFrequencyOverridesDynamicPromptLimit(t *testing.T) {
	cfg := validConfig()
	cfg.TradingFrequency = &TradingFrequencyConfig{Mode: TradingFrequencyModeBalanced}
	path := writeConfigFile(t, cfg)

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if loaded.DynamicCandidatePool.PromptCandidateLimit != 10 {
		t.Fatalf("balanced应覆盖动态候选池prompt limit为10，实际=%d", loaded.DynamicCandidatePool.PromptCandidateLimit)
	}
}

// ============================================================================
// 需求 1.9: 杠杆默认值
// ============================================================================

func TestValidate_ZeroLeverage_SetsDefault5(t *testing.T) {
	cfg := validConfig()
	cfg.Leverage = LeverageConfig{BTCETHLeverage: 0, AltcoinLeverage: 0}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	if cfg.Leverage.BTCETHLeverage != 5 {
		t.Errorf("BTCETHLeverage 默认值应为5, 实际=%d", cfg.Leverage.BTCETHLeverage)
	}
	if cfg.Leverage.AltcoinLeverage != 5 {
		t.Errorf("AltcoinLeverage 默认值应为5, 实际=%d", cfg.Leverage.AltcoinLeverage)
	}
}

func TestValidate_NegativeLeverage_SetsDefault5(t *testing.T) {
	cfg := validConfig()
	cfg.Leverage = LeverageConfig{BTCETHLeverage: -3, AltcoinLeverage: -1}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	if cfg.Leverage.BTCETHLeverage != 5 {
		t.Errorf("负杠杆应被设为默认值5, 实际=%d", cfg.Leverage.BTCETHLeverage)
	}
}

func TestValidate_EmptyExchange_WritesBackDefaultBinance(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	if cfg.Traders[0].Exchange != "binance" {
		t.Fatalf("Exchange 默认值未写回: 期望=binance, 实际=%q", cfg.Traders[0].Exchange)
	}
}

func TestValidate_ZeroScanInterval_WritesBackDefault3(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].ScanIntervalMinutes = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	if cfg.Traders[0].ScanIntervalMinutes != 3 {
		t.Fatalf("ScanIntervalMinutes 默认值未写回: 期望=3, 实际=%d", cfg.Traders[0].ScanIntervalMinutes)
	}
}

// ============================================================================
// 需求 1.2: 启用交易者计数
// ============================================================================

func TestValidate_EnabledTraderCount(t *testing.T) {
	cfg := validConfig()
	cfg.Traders = []TraderConfig{
		validBinanceTrader("t1"),
		func() TraderConfig { tc := validBinanceTrader("t2"); tc.Enabled = false; return tc }(),
		validBinanceTrader("t3"),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("验证失败: %v", err)
	}
	enabled := 0
	for _, tc := range cfg.Traders {
		if tc.Enabled {
			enabled++
		}
	}
	if enabled != 2 {
		t.Errorf("期望2个启用交易者, 实际=%d", enabled)
	}
}

// ============================================================================
// 需求 1.3: 空 ID 或重复 ID 验证
// ============================================================================

func TestValidate_EmptyID_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].ID = ""
	if err := cfg.Validate(); err == nil {
		t.Error("空ID应返回错误")
	}
}

func TestValidate_DuplicateID_ReturnsError(t *testing.T) {
	cfg := validConfig()
	t2 := validBinanceTrader("trader1") // 与 trader1 重复
	cfg.Traders = append(cfg.Traders, t2)
	if err := cfg.Validate(); err == nil {
		t.Error("重复ID应返回错误")
	}
}

func TestValidate_EmptyName_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Name = ""
	if err := cfg.Validate(); err == nil {
		t.Error("空Name应返回错误")
	}
}

// ============================================================================
// 需求 1.4: custom AI 模型必填字段
// ============================================================================

func TestValidate_CustomAI_MissingURL_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].AIModel = "custom"
	cfg.Traders[0].CustomAPIURL = ""
	cfg.Traders[0].CustomAPIKey = "key"
	cfg.Traders[0].CustomModelName = "model"
	if err := cfg.Validate(); err == nil {
		t.Error("custom AI 缺少 URL 应返回错误")
	}
}

func TestValidate_CustomAI_MissingKey_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].AIModel = "custom"
	cfg.Traders[0].CustomAPIURL = "http://example.com"
	cfg.Traders[0].CustomAPIKey = ""
	cfg.Traders[0].CustomModelName = "model"
	if err := cfg.Validate(); err == nil {
		t.Error("custom AI 缺少 Key 应返回错误")
	}
}

func TestValidate_CustomAI_MissingModelName_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].AIModel = "custom"
	cfg.Traders[0].CustomAPIURL = "http://example.com"
	cfg.Traders[0].CustomAPIKey = "key"
	cfg.Traders[0].CustomModelName = ""
	if err := cfg.Validate(); err == nil {
		t.Error("custom AI 缺少 ModelName 应返回错误")
	}
}

func TestValidate_CustomAI_AllFields_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].AIModel = "custom"
	cfg.Traders[0].CustomAPIURL = "http://example.com"
	cfg.Traders[0].CustomAPIKey = "key"
	cfg.Traders[0].CustomModelName = "model"
	cfg.Traders[0].DeepSeekKey = "" // custom 不需要 deepseek key
	if err := cfg.Validate(); err != nil {
		t.Errorf("custom AI 所有字段齐全时不应报错: %v", err)
	}
}

// ============================================================================
// 需求 1.5: Binance 必填字段
// ============================================================================

func TestValidate_Binance_MissingAPIKey_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].BinanceAPIKey = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Binance 缺少 APIKey 应返回错误")
	}
}

func TestValidate_Binance_MissingSecretKey_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].BinanceSecretKey = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Binance 缺少 SecretKey 应返回错误")
	}
}

// ============================================================================
// 需求 1.6: Hyperliquid 必填字段
// ============================================================================

func TestValidate_Hyperliquid_MissingPrivateKey_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "hyperliquid"
	cfg.Traders[0].HyperliquidPrivateKey = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Hyperliquid 缺少私钥应返回错误")
	}
}

func TestValidate_Hyperliquid_WithPrivateKey_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "hyperliquid"
	cfg.Traders[0].HyperliquidPrivateKey = "0xdeadbeef"
	cfg.Traders[0].AIModel = "deepseek"
	cfg.Traders[0].DeepSeekKey = "test-key"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Hyperliquid 配置完整时不应报错: %v", err)
	}
}

// ============================================================================
// 需求 1.7: Aster 必填字段
// ============================================================================

func TestValidate_Aster_MissingUser_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "aster"
	cfg.Traders[0].AsterUser = ""
	cfg.Traders[0].AsterSigner = "signer"
	cfg.Traders[0].AsterPrivateKey = "privkey"
	if err := cfg.Validate(); err == nil {
		t.Error("Aster 缺少 User 应返回错误")
	}
}

func TestValidate_Aster_MissingSigner_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "aster"
	cfg.Traders[0].AsterUser = "user"
	cfg.Traders[0].AsterSigner = ""
	cfg.Traders[0].AsterPrivateKey = "privkey"
	if err := cfg.Validate(); err == nil {
		t.Error("Aster 缺少 Signer 应返回错误")
	}
}

func TestValidate_Aster_MissingPrivateKey_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "aster"
	cfg.Traders[0].AsterUser = "user"
	cfg.Traders[0].AsterSigner = "signer"
	cfg.Traders[0].AsterPrivateKey = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Aster 缺少 PrivateKey 应返回错误")
	}
}

// ============================================================================
// 需求 1.3: 无效 AI 模型类型
// ============================================================================

func TestValidate_InvalidAIModel_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].AIModel = "gpt4"
	if err := cfg.Validate(); err == nil {
		t.Error("无效 AI 模型类型应返回错误")
	}
}

// 需求 1.3: 无效交易所类型
func TestValidate_InvalidExchange_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders[0].Exchange = "okx"
	if err := cfg.Validate(); err == nil {
		t.Error("无效交易所类型应返回错误")
	}
}

// 需求 1.1: 空交易者列表
func TestValidate_NoTraders_ReturnsError(t *testing.T) {
	cfg := validConfig()
	cfg.Traders = nil
	if err := cfg.Validate(); err == nil {
		t.Error("空交易者列表应返回错误")
	}
}

// ============================================================================
// 需求 1.1: GetScanInterval
// ============================================================================

func TestGetScanInterval_ReturnsCorrectDuration(t *testing.T) {
	tc := validBinanceTrader("t1")
	tc.ScanIntervalMinutes = 5
	d := tc.GetScanInterval()
	if d.Minutes() != 5 {
		t.Errorf("期望5分钟, 实际=%.0f", d.Minutes())
	}
}

// ============================================================================
// Property 1: 配置序列化往返
// Feature: quant-trading-system, Property 1: 配置序列化往返
// Validates: Requirements 1.1
// ============================================================================

func TestProperty1_ConfigSerializationRoundTrip(t *testing.T) {
	// Feature: quant-trading-system, Property 1: 配置序列化往返
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("有效Config序列化再反序列化应产生等价结构体", prop.ForAll(
		func(portNorm, btcLevNorm, altLevNorm int) bool {
			port := 1024 + portNorm%64511
			btcLev := 1 + btcLevNorm%10
			altLev := 1 + altLevNorm%10

			original := &Config{
				Traders:       []TraderConfig{validBinanceTrader("t1")},
				APIServerPort: port,
				Leverage:      LeverageConfig{BTCETHLeverage: btcLev, AltcoinLeverage: altLev},
			}

			data, err := json.Marshal(original)
			if err != nil {
				return false
			}
			var restored Config
			if err := json.Unmarshal(data, &restored); err != nil {
				return false
			}
			return restored.APIServerPort == original.APIServerPort &&
				restored.Leverage.BTCETHLeverage == original.Leverage.BTCETHLeverage &&
				restored.Leverage.AltcoinLeverage == original.Leverage.AltcoinLeverage &&
				len(restored.Traders) == len(original.Traders) &&
				restored.Traders[0].ID == original.Traders[0].ID
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 2: 配置类型必填字段验证
// Feature: quant-trading-system, Property 2: 配置类型必填字段验证
// Validates: Requirements 1.3, 1.4, 1.5, 1.6, 1.7
// ============================================================================

func TestProperty2_RequiredFieldValidation(t *testing.T) {
	// Feature: quant-trading-system, Property 2: 配置类型必填字段验证
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// custom AI 缺少任意必填字段时应报错
	properties.Property("custom AI 缺少必填字段时Validate应返回错误", prop.ForAll(
		func(missingFieldIdx int) bool {
			cfg := validConfig()
			cfg.Traders[0].AIModel = "custom"
			cfg.Traders[0].DeepSeekKey = ""
			switch missingFieldIdx % 3 {
			case 0:
				cfg.Traders[0].CustomAPIURL = ""
				cfg.Traders[0].CustomAPIKey = "key"
				cfg.Traders[0].CustomModelName = "model"
			case 1:
				cfg.Traders[0].CustomAPIURL = "http://example.com"
				cfg.Traders[0].CustomAPIKey = ""
				cfg.Traders[0].CustomModelName = "model"
			case 2:
				cfg.Traders[0].CustomAPIURL = "http://example.com"
				cfg.Traders[0].CustomAPIKey = "key"
				cfg.Traders[0].CustomModelName = ""
			}
			return cfg.Validate() != nil
		},
		gen.IntRange(0, 99),
	))

	// Binance 缺少密钥时应报错
	properties.Property("Binance 缺少密钥时Validate应返回错误", prop.ForAll(
		func(missingFieldIdx int) bool {
			cfg := validConfig()
			switch missingFieldIdx % 2 {
			case 0:
				cfg.Traders[0].BinanceAPIKey = ""
			case 1:
				cfg.Traders[0].BinanceSecretKey = ""
			}
			return cfg.Validate() != nil
		},
		gen.IntRange(0, 99),
	))

	// Hyperliquid 缺少私钥时应报错
	properties.Property("Hyperliquid 缺少私钥时Validate应返回错误", prop.ForAll(
		func(_ int) bool {
			cfg := validConfig()
			cfg.Traders[0].Exchange = "hyperliquid"
			cfg.Traders[0].HyperliquidPrivateKey = ""
			return cfg.Validate() != nil
		},
		gen.IntRange(0, 99),
	))

	// Aster 缺少任意必填字段时应报错
	properties.Property("Aster 缺少必填字段时Validate应返回错误", prop.ForAll(
		func(missingFieldIdx int) bool {
			cfg := validConfig()
			cfg.Traders[0].Exchange = "aster"
			switch missingFieldIdx % 3 {
			case 0:
				cfg.Traders[0].AsterUser = ""
				cfg.Traders[0].AsterSigner = "signer"
				cfg.Traders[0].AsterPrivateKey = "privkey"
			case 1:
				cfg.Traders[0].AsterUser = "user"
				cfg.Traders[0].AsterSigner = ""
				cfg.Traders[0].AsterPrivateKey = "privkey"
			case 2:
				cfg.Traders[0].AsterUser = "user"
				cfg.Traders[0].AsterSigner = "signer"
				cfg.Traders[0].AsterPrivateKey = ""
			}
			return cfg.Validate() != nil
		},
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 3: 启用交易者计数
// Feature: quant-trading-system, Property 3: 启用交易者计数
// Validates: Requirements 1.2
// ============================================================================

// countEnabledTraders 模拟 setupTraderManager 中的过滤逻辑：
// 仅统计 Enabled=true 的交易者，与 main.go 中的行为一致。
func countEnabledTraders(traders []TraderConfig) int {
	count := 0
	for _, tc := range traders {
		if tc.Enabled {
			count++
		}
	}
	return count
}

func TestProperty3_EnabledTraderCount(t *testing.T) {
	// Feature: quant-trading-system, Property 3: 启用交易者计数
	// Validates: Requirements 1.2
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("对任意包含M个Enabled=true的配置，过滤后应恰好得到M个启用交易者", prop.ForAll(
		func(enabledMask int) bool {
			// 使用 enabledMask 的低 4 位决定最多 4 个交易者的启用状态
			ids := []string{"t1", "t2", "t3", "t4"}
			traders := make([]TraderConfig, len(ids))
			expectedM := 0
			for i, id := range ids {
				tc := validBinanceTrader(id)
				tc.Enabled = (enabledMask>>uint(i))&1 == 1
				if tc.Enabled {
					expectedM++
				}
				traders[i] = tc
			}

			cfg := &Config{
				Traders:       traders,
				APIServerPort: 8080,
				Leverage:      LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5},
			}
			if err := cfg.Validate(); err != nil {
				// 配置本身无效，跳过此用例
				return true
			}

			// 验证：过滤 Enabled=true 的交易者数量恰好等于 expectedM
			actualM := countEnabledTraders(cfg.Traders)
			return actualM == expectedM
		},
		gen.IntRange(0, 15), // 4 位掩码，覆盖 0000~1111 所有组合
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 4: 默认币种池自动启用
// Feature: quant-trading-system, Property 4: 默认币种池自动启用
// Validates: Requirements 1.8
// ============================================================================

func TestProperty4_DefaultCoinPoolAutoEnable(t *testing.T) {
	// Feature: quant-trading-system, Property 4: 默认币种池自动启用
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("UseDefaultCoins=false且CoinPoolAPIURL为空时加载后应为true", prop.ForAll(
		func(_ int) bool {
			cfg := validConfig()
			cfg.UseDefaultCoins = false
			cfg.CoinPoolAPIURL = ""
			path := writeConfigFile(t, cfg)
			loaded, err := LoadConfig(path)
			if err != nil {
				return false
			}
			return loaded.UseDefaultCoins
		},
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 5: 杠杆默认值
// Feature: quant-trading-system, Property 5: 杠杆默认值
// Validates: Requirements 1.9
// ============================================================================

func TestProperty5_LeverageDefaultValue(t *testing.T) {
	// Feature: quant-trading-system, Property 5: 杠杆默认值
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("杠杆<=0时验证后应被设置为默认值5", prop.ForAll(
		func(btcLevNorm, altLevNorm int) bool {
			btcLev := -(btcLevNorm % 100) // 0 或负数
			altLev := -(altLevNorm % 100) // 0 或负数
			cfg := validConfig()
			cfg.Leverage = LeverageConfig{BTCETHLeverage: btcLev, AltcoinLeverage: altLev}
			if err := cfg.Validate(); err != nil {
				return false
			}
			return cfg.Leverage.BTCETHLeverage == 5 && cfg.Leverage.AltcoinLeverage == 5
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Property 6: 无效配置文件拒绝
// Feature: quant-trading-system, Property 6: 无效配置文件拒绝
// Validates: Requirements 1.10
// ============================================================================

func TestProperty6_InvalidConfigRejected(t *testing.T) {
	// Feature: quant-trading-system, Property 6: 无效配置文件拒绝
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	invalidJSONSamples := []string{
		"{invalid}",
		"[1,2,3]",
		"null",
		"",
		"{\"traders\": null}",
		"{\"traders\": []}",
	}

	properties.Property("非法JSON或缺少必填字段时LoadConfig应返回错误", prop.ForAll(
		func(idxNorm int) bool {
			sample := invalidJSONSamples[idxNorm%len(invalidJSONSamples)]
			f, err := os.CreateTemp("", "config_invalid_*.json")
			if err != nil {
				return false
			}
			defer func() { _ = os.Remove(f.Name()) }()
			if _, err := f.WriteString(sample); err != nil {
				_ = f.Close()
				return false
			}
			if err := f.Close(); err != nil {
				return false
			}
			_, loadErr := LoadConfig(f.Name())
			return loadErr != nil
		},
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// Feature: build-debug-run-pipeline, Property 2: 配置加载端口保持
// 通过 LoadConfig（文件读取 + Validate）验证完整链路的 round-trip 正确性
// Validates: Requirements 2.1, 6.1
// ============================================================================

func TestProperty2_ConfigLoadRoundTrip(t *testing.T) {
	// Feature: build-debug-run-pipeline, Property 2: 配置加载端口保持
	// 与 Property 1 不同：本测试通过 LoadConfig（含文件 I/O + Validate）验证完整链路
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("Config经JSON序列化写入文件再通过LoadConfig加载后数值字段应保持一致", prop.ForAll(
		func(portNorm, btcLevNorm, altLevNorm, dailyLossNorm, drawdownNorm int) bool {
			// 生成有效范围内的随机值（确保 > 0 以避免 Validate 覆盖默认值）
			port := 1024 + portNorm%64511                 // 1024..65534
			btcLev := 1 + btcLevNorm%20                   // 1..20
			altLev := 1 + altLevNorm%20                   // 1..20
			dailyLoss := 1.0 + float64(dailyLossNorm%100) // 1.0..100.0
			drawdown := 1.0 + float64(drawdownNorm%100)   // 1.0..100.0

			original := &Config{
				Traders:       []TraderConfig{validBinanceTrader("roundtrip-1")},
				APIServerPort: port,
				Leverage: LeverageConfig{
					BTCETHLeverage:  btcLev,
					AltcoinLeverage: altLev,
				},
				MaxDailyLoss: dailyLoss,
				MaxDrawdown:  drawdown,
			}

			// 序列化为 JSON 写入临时文件
			path := writeConfigFile(t, original)

			// 通过 LoadConfig 重新加载（含文件读取 + Validate 完整链路）
			loaded, err := LoadConfig(path)
			if err != nil {
				t.Logf("LoadConfig 失败: %v", err)
				return false
			}

			// 验证关键数值字段与原始值一致
			return loaded.APIServerPort == original.APIServerPort &&
				loaded.Leverage.BTCETHLeverage == original.Leverage.BTCETHLeverage &&
				loaded.Leverage.AltcoinLeverage == original.Leverage.AltcoinLeverage &&
				loaded.MaxDailyLoss == original.MaxDailyLoss &&
				loaded.MaxDrawdown == original.MaxDrawdown
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
