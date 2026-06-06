package main

import (
	"encoding/json"
	"nofx/config"
	"nofx/manager"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================================
// loadAndValidateConfig 测试
// ============================================================================

func TestLoadAndValidateConfig_ValidConfig(t *testing.T) {
	// 创建临时配置文件
	tmpFile := createTempConfigFile(t, validConfigJSON())

	cfg, err := loadAndValidateConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载有效配置失败: %v", err)
	}
	if cfg == nil {
		t.Fatal("配置不应为nil")
	}
	if len(cfg.Traders) != 1 {
		t.Fatalf("期望1个trader，实际: %d", len(cfg.Traders))
	}
	if cfg.Traders[0].ID != "test-trader" {
		t.Errorf("期望trader ID为'test-trader'，实际: %s", cfg.Traders[0].ID)
	}
}

func TestLoadAndValidateConfig_FileNotExist(t *testing.T) {
	_, err := loadAndValidateConfig("nonexistent_config_12345.json")
	if err == nil {
		t.Fatal("不存在的配置文件应返回错误")
	}
}

func TestLoadAndValidateConfig_InvalidJSON(t *testing.T) {
	tmpFile := createTempFile(t, "invalid json {{{")

	_, err := loadAndValidateConfig(tmpFile)
	if err == nil {
		t.Fatal("无效JSON应返回错误")
	}
}

func TestLoadAndValidateConfig_NoEnabledTraders(t *testing.T) {
	cfg := validConfigMap()
	traders := cfg["traders"].([]map[string]interface{})
	traders[0]["enabled"] = false
	cfg["traders"] = traders

	tmpFile := createTempConfigFromMap(t, cfg)

	_, err := loadAndValidateConfig(tmpFile)
	if err == nil {
		t.Fatal("没有启用的trader应返回错误")
	}
}

func TestLoadAndValidateConfig_DefaultCoinPoolEnabled(t *testing.T) {
	// 当 UseDefaultCoins=false 且 CoinPoolAPIURL 为空时，应自动启用
	cfg := validConfigMap()
	cfg["use_default_coins"] = false
	cfg["coin_pool_api_url"] = ""

	tmpFile := createTempConfigFromMap(t, cfg)

	result, err := loadAndValidateConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if !result.UseDefaultCoins {
		t.Error("UseDefaultCoins 应被自动设置为 true")
	}
}

func TestLoadAndValidateConfig_CommandLineArg(t *testing.T) {
	// 验证默认配置文件常量
	if DefaultConfigFile != "config.json" {
		t.Errorf("默认配置文件应为'config.json'，实际: %s", DefaultConfigFile)
	}
}

// ============================================================================
// ensureDataDir 测试
// ============================================================================

func TestEnsureDataDir_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "test_data_dir")

	err := ensureDataDir(testDir)
	if err != nil {
		t.Fatalf("创建数据目录失败: %v", err)
	}

	info, err := os.Stat(testDir)
	if err != nil {
		t.Fatalf("目录不存在: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("路径应为目录")
	}
}

func TestEnsureDataDir_ExistingDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// 对已存在的目录调用不应报错
	err := ensureDataDir(tmpDir)
	if err != nil {
		t.Fatalf("已存在的目录不应报错: %v", err)
	}
}

func TestEnsureDataDir_NestedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")

	err := ensureDataDir(nestedDir)
	if err != nil {
		t.Fatalf("创建嵌套目录失败: %v", err)
	}

	info, err := os.Stat(nestedDir)
	if err != nil {
		t.Fatalf("嵌套目录不存在: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("路径应为目录")
	}
}

func TestEnsureDataDir_DefaultConstant(t *testing.T) {
	if DefaultDataDir != "./data" {
		t.Errorf("默认数据目录应为'./data'，实际: %s", DefaultDataDir)
	}
}

// ============================================================================
// initializeModules 测试
// ============================================================================

func TestInitializeModules_ValidConfig(t *testing.T) {
	cfg := &config.Config{
		UseDefaultCoins: true,
		DefaultCoins:    []string{"BTCUSDT", "ETHUSDT"},
		Leverage: config.LeverageConfig{
			BTCETHLeverage:  5,
			AltcoinLeverage: 5,
		},
	}

	err := initializeModules(cfg)
	if err != nil {
		t.Fatalf("初始化模块失败: %v", err)
	}
}

// ============================================================================
// setupTraderManager 测试
// ============================================================================

func TestSetupTraderManager_NoEnabledTraders(t *testing.T) {
	cfg := &config.Config{
		Traders: []config.TraderConfig{
			{
				ID:      "disabled-1",
				Name:    "Disabled Trader",
				Enabled: false,
			},
		},
	}

	tm, err := setupTraderManager(cfg)
	if err != nil {
		t.Fatalf("设置TraderManager不应失败: %v", err)
	}
	if tm == nil {
		t.Fatal("TraderManager不应为nil")
	}
	// 没有启用的trader，所以应该没有trader被添加
	ids := tm.GetTraderIDs()
	if len(ids) != 0 {
		t.Errorf("期望0个trader，实际: %d", len(ids))
	}
}

func TestSetupTraderManager_InvalidExchange(t *testing.T) {
	// 启用的trader但使用无效的交易所类型，AddTrader 应失败
	cfg := &config.Config{
		Traders: []config.TraderConfig{
			{
				ID:             "bad-trader",
				Name:           "Bad Trader",
				Enabled:        true,
				AIModel:        "deepseek",
				Exchange:       "unknown_exchange",
				DeepSeekKey:    "test-key",
				InitialBalance: 1000,
			},
		},
		Leverage: config.LeverageConfig{
			BTCETHLeverage:  5,
			AltcoinLeverage: 5,
		},
	}

	_, err := setupTraderManager(cfg)
	if err == nil {
		t.Fatal("无效的交易所类型应返回错误")
	}
}

func TestSetupTraderManager_WithEnabledTrader(t *testing.T) {
	// Binance trader 可以用测试 API key 创建，验证 AddTrader 成功
	cfg := &config.Config{
		Traders: []config.TraderConfig{
			{
				ID:               "test-trader-1",
				Name:             "Test Trader 1",
				Enabled:          true,
				AIModel:          "deepseek",
				Exchange:         "binance",
				BinanceAPIKey:    "test-key",
				BinanceSecretKey: "test-secret",
				DeepSeekKey:      "test-deepseek-key",
				InitialBalance:   1000,
			},
		},
		Leverage: config.LeverageConfig{
			BTCETHLeverage:  5,
			AltcoinLeverage: 5,
		},
	}

	tm, err := setupTraderManager(cfg)
	if err != nil {
		t.Fatalf("设置TraderManager失败: %v", err)
	}
	ids := tm.GetTraderIDs()
	if len(ids) != 1 {
		t.Errorf("期望1个trader，实际: %d", len(ids))
	}
}

func TestSetupTraderManager_DuplicateTraderID(t *testing.T) {
	// 重复的 trader ID 应返回错误
	cfg := &config.Config{
		Traders: []config.TraderConfig{
			{
				ID:               "dup-trader",
				Name:             "Trader A",
				Enabled:          true,
				AIModel:          "deepseek",
				Exchange:         "binance",
				BinanceAPIKey:    "key-a",
				BinanceSecretKey: "secret-a",
				DeepSeekKey:      "ds-key-a",
				InitialBalance:   1000,
			},
			{
				ID:               "dup-trader",
				Name:             "Trader B",
				Enabled:          true,
				AIModel:          "deepseek",
				Exchange:         "binance",
				BinanceAPIKey:    "key-b",
				BinanceSecretKey: "secret-b",
				DeepSeekKey:      "ds-key-b",
				InitialBalance:   1000,
			},
		},
		Leverage: config.LeverageConfig{
			BTCETHLeverage:  5,
			AltcoinLeverage: 5,
		},
	}

	_, err := setupTraderManager(cfg)
	if err == nil {
		t.Fatal("重复的trader ID应返回错误")
	}
}

// ============================================================================
// gracefulShutdown 测试
// ============================================================================

func TestGracefulShutdown_CompletesWithinTimeout(t *testing.T) {
	tm := manager.NewTraderManager()

	start := time.Now()
	gracefulShutdown(tm, 5*time.Second)
	elapsed := time.Since(start)

	// 没有trader时应该很快完成
	if elapsed > 3*time.Second {
		t.Errorf("优雅退出耗时过长: %v", elapsed)
	}
}

func TestGracefulShutdown_DefaultTimeout(t *testing.T) {
	if GracefulShutdownTime != 10*time.Second {
		t.Errorf("默认优雅退出超时应为10秒，实际: %v", GracefulShutdownTime)
	}
}

func TestGracefulShutdown_EmptyManager(t *testing.T) {
	tm := manager.NewTraderManager()

	// 确保空的 TraderManager 不会 panic
	done := make(chan struct{})
	go func() {
		defer close(done)
		gracefulShutdown(tm, 2*time.Second)
	}()

	select {
	case <-done:
		// 正常完成
	case <-time.After(5 * time.Second):
		t.Fatal("优雅退出超时")
	}
}

// ============================================================================
// 系统常量验证
// ============================================================================

func TestSystemConstants(t *testing.T) {
	if DefaultConfigFile != "config.json" {
		t.Errorf("DefaultConfigFile 应为 'config.json'")
	}
	if DefaultDataDir != "./data" {
		t.Errorf("DefaultDataDir 应为 './data'")
	}
	if GracefulShutdownTime != 10*time.Second {
		t.Errorf("GracefulShutdownTime 应为 10 秒")
	}
	if APIServerStartTimeout != 5*time.Second {
		t.Errorf("APIServerStartTimeout 应为 5 秒")
	}
}

// ============================================================================
// 启动流程顺序验证
// ============================================================================

func TestStartupSequence_ConfigThenDataDirThenModules(t *testing.T) {
	// 验证启动流程：加载配置 → 创建数据目录 → 初始化模块 → 创建 TraderManager

	// Step 1: 加载配置
	tmpFile := createTempConfigFile(t, validConfigJSON())
	cfg, err := loadAndValidateConfig(tmpFile)
	if err != nil {
		t.Fatalf("Step 1 - 加载配置失败: %v", err)
	}

	// Step 2: 创建数据目录
	tmpDataDir := filepath.Join(t.TempDir(), "data")
	err = ensureDataDir(tmpDataDir)
	if err != nil {
		t.Fatalf("Step 2 - 创建数据目录失败: %v", err)
	}

	// Step 3: 初始化模块
	err = initializeModules(cfg)
	if err != nil {
		t.Fatalf("Step 3 - 初始化模块失败: %v", err)
	}

	// Step 4: 创建 TraderManager（禁用trader避免真实连接）
	cfg.Traders[0].Enabled = false
	tm, err := setupTraderManager(cfg)
	if err != nil {
		t.Fatalf("Step 4 - 创建TraderManager失败: %v", err)
	}
	if tm == nil {
		t.Fatal("TraderManager不应为nil")
	}
}

func TestTradingModeTitleReflectsEnabledDecisionModes(t *testing.T) {
	cases := []struct {
		name    string
		traders []config.TraderConfig
		want    string
	}{
		{
			name: "ai only",
			traders: []config.TraderConfig{{
				Enabled:      true,
				DecisionMode: config.DecisionModeAI,
			}},
			want: "🤖 AI决策模式:",
		},
		{
			name: "programmatic only",
			traders: []config.TraderConfig{{
				Enabled:      true,
				DecisionMode: config.DecisionModeProgrammatic,
			}},
			want: "🧮 程序化策略决策模式:",
		},
		{
			name: "chanlun v2 only",
			traders: []config.TraderConfig{{
				Enabled:      true,
				DecisionMode: config.DecisionModeChanlunV2,
			}},
			want: "🧩 缠论V2策略决策模式:",
		},
		{
			name: "strategy mix",
			traders: []config.TraderConfig{
				{Enabled: true, DecisionMode: config.DecisionModeProgrammatic},
				{Enabled: true, DecisionMode: config.DecisionModeChanlunV2},
			},
			want: "🧭 策略决策模式:",
		},
		{
			name: "ai and strategy mix",
			traders: []config.TraderConfig{
				{Enabled: true, DecisionMode: config.DecisionModeAI},
				{Enabled: true, DecisionMode: config.DecisionModeChanlunV2},
			},
			want: "🧭 多决策模式:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tradingModeTitle(&config.Config{Traders: tc.traders})
			if got != tc.want {
				t.Fatalf("tradingModeTitle()=%q want=%q", got, tc.want)
			}
		})
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func validConfigJSON() string {
	return `{
		"traders": [
			{
				"id": "test-trader",
				"name": "Test Trader",
				"enabled": true,
				"ai_model": "deepseek",
				"exchange": "binance",
				"binance_api_key": "test-api-key",
				"binance_secret_key": "test-secret-key",
				"deepseek_key": "test-deepseek-key",
				"initial_balance": 1000,
				"scan_interval_minutes": 3
			}
		],
		"use_default_coins": true,
		"default_coins": ["BTCUSDT", "ETHUSDT"],
		"api_server_port": 9999,
		"leverage": {
			"btc_eth_leverage": 5,
			"altcoin_leverage": 5
		}
	}`
}

func validConfigMap() map[string]interface{} {
	return map[string]interface{}{
		"traders": []map[string]interface{}{
			{
				"id":                    "test-trader",
				"name":                  "Test Trader",
				"enabled":               true,
				"ai_model":              "deepseek",
				"exchange":              "binance",
				"binance_api_key":       "test-api-key",
				"binance_secret_key":    "test-secret-key",
				"deepseek_key":          "test-deepseek-key",
				"initial_balance":       1000.0,
				"scan_interval_minutes": 3,
			},
		},
		"use_default_coins": true,
		"default_coins":     []string{"BTCUSDT", "ETHUSDT"},
		"api_server_port":   9999,
		"leverage": map[string]interface{}{
			"btc_eth_leverage": 5,
			"altcoin_leverage": 5,
		},
	}
}

func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}
	return tmpFile
}

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	return tmpFile
}

func createTempConfigFromMap(t *testing.T, cfg map[string]interface{}) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化配置失败: %v", err)
	}
	return createTempConfigFile(t, string(data))
}
