package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"nofx/api"
	"nofx/config"
	"nofx/decision"
	"nofx/manager"
	"nofx/pool"
	"nofx/trader"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// 版本信息（可通过 -ldflags 注入）
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// 应用配置常量
const (
	DefaultConfigFile     = "config.json"
	DefaultDataDir        = "./data"
	GracefulShutdownTime  = 10 * time.Second
	APIServerStartTimeout = 5 * time.Second
)

func main() {
	// 设置日志格式
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)

	// 打印启动信息
	printBanner()
	printSystemInfo()

	// 解析命令行参数
	configFile := DefaultConfigFile
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	// 加载配置
	cfg, err := loadAndValidateConfig(configFile)
	if err != nil {
		log.Fatalf("❌ 配置加载失败: %v", err)
	}

	// 初始化数据目录
	if err := ensureDataDir(DefaultDataDir); err != nil {
		log.Fatalf("❌ 创建数据目录失败: %v", err)
	}

	// 初始化各模块
	if err := initializeModules(cfg); err != nil {
		log.Fatalf("❌ 模块初始化失败: %v", err)
	}

	// 创建并配置TraderManager
	traderManager, err := setupTraderManager(cfg)
	if err != nil {
		log.Fatalf("❌ TraderManager设置失败: %v", err)
	}

	// 打印参赛者信息
	printContestants(cfg)
	printTradingMode(cfg)

	// 创建上下文用于优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动API服务器
	apiServer := api.NewServer(traderManager, cfg.APIServerPort)
	apiServerErr := make(chan error, 1)
	go func() {
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			apiServerErr <- err
		}
	}()

	// 检查API服务器是否成功启动
	select {
	case err := <-apiServerErr:
		log.Fatalf("❌ API服务器启动失败: %v", err)
	case <-time.After(APIServerStartTimeout):
		log.Printf("✓ API服务器启动成功，端口: %d", cfg.APIServerPort)
	}

	// 启动所有trader
	traderManager.StartAll()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	// 等待退出信号
	select {
	case sig := <-sigChan:
		log.Printf("📛 收到信号 %v，开始优雅退出...", sig)
	case err := <-apiServerErr:
		log.Printf("❌ API服务器运行时错误: %v，开始退出...", err)
	case <-ctx.Done():
		log.Println("📛 上下文取消，开始退出...")
	}

	// 执行优雅退出
	gracefulShutdown(traderManager, GracefulShutdownTime)

	fmt.Println()
	fmt.Println("👋 感谢使用AI交易竞赛系统！")
}

// printBanner 打印启动横幅
func printBanner() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║    🏆 AI模型交易竞赛系统 - Qwen vs DeepSeek               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// printSystemInfo 打印系统信息
func printSystemInfo() {
	log.Printf("📊 系统信息:")
	log.Printf("   版本: %s | 构建时间: %s | Git: %s", Version, BuildTime, GitCommit)
	log.Printf("   Go版本: %s | OS: %s | Arch: %s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	log.Printf("   CPU核心: %d | Goroutines: %d", runtime.NumCPU(), runtime.NumGoroutine())
	fmt.Println()
}

// loadAndValidateConfig 加载并验证配置
func loadAndValidateConfig(configFile string) (*config.Config, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configFile)
	}

	log.Printf("📋 加载配置文件: %s", configFile)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, err
	}

	// 统计启用的trader数量
	enabledCount := 0
	for _, t := range cfg.Traders {
		if t.Enabled {
			enabledCount++
		}
	}

	if enabledCount == 0 {
		return nil, fmt.Errorf("没有启用的trader，请在config.json中设置至少一个trader的enabled=true")
	}

	log.Printf("✓ 配置加载成功，共%d个trader（%d个启用）", len(cfg.Traders), enabledCount)
	return cfg, nil
}

// ensureDataDir 确保数据目录存在
func ensureDataDir(dataDir string) error {
	absPath, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	log.Printf("✓ 数据目录已就绪: %s", absPath)
	return nil
}

// initializeModules 初始化各个模块
func initializeModules(cfg *config.Config) error {
	frequencyProfile, err := cfg.NormalizeTradingFrequency()
	if err != nil {
		return err
	}

	// 1. 设置币种池
	pool.SetDefaultCoins(cfg.DefaultCoins)
	pool.SetUseDefaultCoins(cfg.UseDefaultCoins)

	if cfg.UseDefaultCoins {
		log.Printf("✓ 已启用默认主流币种列表（共%d个币种）", len(cfg.DefaultCoins))
	}

	if cfg.CoinPoolAPIURL != "" {
		pool.SetCoinPoolAPI(cfg.CoinPoolAPIURL)
		log.Printf("✓ 已配置AI500币种池API")
	}

	if cfg.OITopAPIURL != "" {
		pool.SetOITopAPI(cfg.OITopAPIURL)
		log.Printf("✓ 已配置OI Top API")
	}

	promptCandidateLimit := cfg.DynamicCandidatePool.PromptCandidateLimit
	if !frequencyProfile.Legacy {
		promptCandidateLimit = frequencyProfile.PromptCandidateLimit
	}
	pool.SetDynamicCandidatePoolConfig(pool.DynamicCandidatePoolConfig{
		Enabled:                 cfg.DynamicCandidatePool.IsEnabled(),
		RefreshHour:             cfg.DynamicCandidatePool.RefreshHour,
		TTLHours:                cfg.DynamicCandidatePool.TTLHours,
		MinPoolSize:             cfg.DynamicCandidatePool.MinPoolSize,
		MaxPoolSize:             cfg.DynamicCandidatePool.MaxPoolSize,
		PromptCandidateLimit:    promptCandidateLimit,
		CoreSymbols:             cfg.DynamicCandidatePool.CoreSymbols,
		MinOIValueUSD:           cfg.DynamicCandidatePool.MinOIValueUSD,
		MinQuoteVolume24hUSD:    cfg.DynamicCandidatePool.MinQuoteVolume24hUSD,
		CooldownDaysAfterLosses: cfg.DynamicCandidatePool.CooldownDaysAfterLosses,
		ExchangeVolumeTopLimit:  cfg.DynamicCandidatePool.ExchangeVolumeTopLimit,
		SnapshotPath:            cfg.DynamicCandidatePool.SnapshotPath,
	})

	// 2. 初始化决策模块
	decisionConfig := &decision.Config{
		MaxRiskPerTrade:       0.02,
		TotalRiskBudget:       0.08,
		MaxAccountDrawdownPct: cfg.MaxDrawdown,
		AnalysisIntervalMin:   frequencyProfile.AnalysisIntervalMinutes,
		BTCETHLeverage:        cfg.Leverage.BTCETHLeverage,
		AltcoinLeverage:       cfg.Leverage.AltcoinLeverage,
		DataDir:               DefaultDataDir,
		RiskFreeRate:          0.0,
	}

	if err := decision.Initialize(decisionConfig); err != nil {
		return fmt.Errorf("初始化决策模块失败: %w", err)
	}
	log.Printf("✓ 决策模块初始化成功")

	return nil
}

// setupTraderManager 设置并配置TraderManager
func setupTraderManager(cfg *config.Config) (*manager.TraderManager, error) {
	traderManager := manager.NewTraderManager()
	frequencyProfile, err := cfg.NormalizeTradingFrequency()
	if err != nil {
		return nil, err
	}
	strategyRiskProfile, err := cfg.NormalizeStrategyRisk()
	if err != nil {
		return nil, err
	}

	// 设置自动平仓回调
	traderManager.SetAutoCloseCallback(handleAutoClose)

	// 添加所有启用的trader
	for i, traderCfg := range cfg.Traders {
		if !traderCfg.Enabled {
			log.Printf("⏭️  [%d/%d] 跳过未启用的 %s", i+1, len(cfg.Traders), traderCfg.Name)
			continue
		}

		log.Printf("📦 [%d/%d] 初始化 %s (%s模型)...",
			i+1, len(cfg.Traders), traderCfg.Name, strings.ToUpper(traderCfg.AIModel))

		err := traderManager.AddTraderWithPolicies(
			traderCfg,
			cfg.CoinPoolAPIURL,
			cfg.MaxDailyLoss,
			cfg.MaxDrawdown,
			cfg.StopTradingMinutes,
			cfg.Leverage,
			frequencyProfile,
			strategyRiskProfile,
		)
		if err != nil {
			return nil, fmt.Errorf("添加trader '%s' 失败: %w", traderCfg.Name, err)
		}
	}

	return traderManager, nil
}

// handleAutoClose 处理自动平仓事件
func handleAutoClose(traderID string, order trader.AutoClosedOrder) {
	// 更新决策模块的统计数据
	decision.AddReturn(order.PnLPercent)
	decision.UpdateStatistics(order.PnLPercent, order.HoldTimeMinutes)

	// 移除交易计划
	decision.OnPositionClosedSimple(order.Symbol, order.CloseReason)

	// 记录日志
	pnlEmoji := "🟢"
	if order.RealizedPnL < 0 {
		pnlEmoji = "🔴"
	}

	log.Printf("%s [%s] 自动平仓记录已更新: %s %s, 盈亏: %.2f%%, 持仓: %.1f分钟",
		pnlEmoji, traderID, order.Symbol, order.Side, order.PnLPercent, order.HoldTimeMinutes)
}

// printContestants 打印参赛者信息
func printContestants(cfg *config.Config) {
	fmt.Println()
	fmt.Println("🏁 竞赛参赛者:")

	for _, traderCfg := range cfg.Traders {
		if !traderCfg.Enabled {
			continue
		}

		exchangeIcon := map[string]string{
			"binance":     "🔸",
			"hyperliquid": "🔹",
			"aster":       "⭐",
		}

		icon := exchangeIcon[traderCfg.Exchange]
		if icon == "" {
			icon = "•"
		}

		fmt.Printf("  %s %s (%s @ %s) - 初始资金: %.0f USDT\n",
			icon,
			traderCfg.Name,
			strings.ToUpper(traderCfg.AIModel),
			strings.Title(traderCfg.Exchange),
			traderCfg.InitialBalance)
	}
}

// printTradingMode 打印交易模式信息
func printTradingMode(cfg *config.Config) {
	fmt.Println()
	fmt.Println("🤖 AI全权决策模式:")
	fmt.Printf("  • 杠杆倍数: 山寨币最高 %dx | BTC/ETH最高 %dx\n",
		cfg.Leverage.AltcoinLeverage, cfg.Leverage.BTCETHLeverage)
	fmt.Println("  • 单笔风险: ≤ 账户净值的 2%")
	fmt.Println("  • 总风险预算: ≤ 账户净值的 8%")
	fmt.Println("  • 最大持仓: 3 个")
	fmt.Println()

	// 风险配置
	if cfg.MaxDailyLoss > 0 {
		fmt.Printf("  📉 日最大亏损限制: %.1f%%\n", formatPercentConfig(cfg.MaxDailyLoss))
	}
	if cfg.MaxDrawdown > 0 {
		fmt.Printf("  📉 最大回撤限制: %.1f%%\n", formatPercentConfig(cfg.MaxDrawdown))
	}

	fmt.Println()
	fmt.Println("⚠️  风险提示: AI自动交易有风险，建议小额资金测试！")
	fmt.Println()
	fmt.Println("按 Ctrl+C 停止运行")
	fmt.Println(strings.Repeat("═", 60))
	fmt.Println()
}

func formatPercentConfig(value float64) float64 {
	if value <= 1 {
		return value * 100
	}
	return value
}

// gracefulShutdown 优雅退出
func gracefulShutdown(traderManager *manager.TraderManager, timeout time.Duration) {
	fmt.Println()
	log.Println("🔄 开始优雅退出...")

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 使用channel来追踪完成状态
	done := make(chan struct{})

	go func() {
		defer close(done)

		// 1. 停止所有trader
		log.Println("  ⏹  停止所有Trader...")
		traderManager.StopAll()

		// 2. 保存决策模块数据
		log.Println("  💾 保存决策数据...")
		if err := decision.Shutdown(); err != nil {
			log.Printf("  ⚠️ 保存决策数据失败: %v", err)
		} else {
			log.Println("  ✓ 决策数据已保存")
		}

		// 3. 打印最终统计
		printFinalStats()
	}()

	// 等待完成或超时
	select {
	case <-done:
		log.Println("✓ 优雅退出完成")
	case <-ctx.Done():
		log.Println("⚠️ 退出超时，强制结束")
	}
}

// printFinalStats 打印最终统计信息
func printFinalStats() {
	stats := decision.GetStatistics()
	if stats.TotalTrades > 0 {
		fmt.Println()
		fmt.Println("📊 本次运行统计:")
		fmt.Printf("   总交易数: %d | 胜率: %.1f%%\n", stats.TotalTrades, stats.WinRate*100)
		fmt.Printf("   总盈亏: %.2f%% | 夏普比率: %.2f\n", stats.TotalPnL, stats.SharpeRatio)
		fmt.Printf("   平均持仓时间: %.0f 分钟\n", stats.AverageHoldTime)
	}
}
