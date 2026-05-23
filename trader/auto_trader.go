package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"nofx/config"
	"nofx/decision"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/pool"
	"nofx/strategy/chanlun"
	"nofx/strategy/chanlunv2"
	"strconv"
	"strings"
	"time"
)

// ChanlunV2EngineInterface 缠论 v2 引擎接口，由 strategy/chanlunv2 包实现
type ChanlunV2EngineInterface interface {
	GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error)
	SymbolUniverse(traderID string) []chanlun.StrategySymbol
	LatestSignalsWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) (*chanlun.SignalReport, bool)
	EmptySignalReportWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) *chanlun.SignalReport
}

// AutoTraderConfig 自动交易配置（简化版 - AI全权决策）
type AutoTraderConfig struct {
	// Trader标识
	ID           string // Trader唯一标识（用于日志目录等）
	Name         string // Trader显示名称
	AIModel      string // AI模型: "qwen" 或 "deepseek"
	DecisionMode string // ai、programmatic 或 chanlun_v2

	// 交易平台选择
	Exchange string // "binance", "hyperliquid" 或 "aster"

	// 币安API配置
	BinanceAPIKey    string
	BinanceSecretKey string

	// Hyperliquid配置
	HyperliquidPrivateKey string
	HyperliquidWalletAddr string
	HyperliquidTestnet    bool

	// Aster配置
	AsterUser       string // Aster主钱包地址
	AsterSigner     string // Aster API钱包地址
	AsterPrivateKey string // Aster API钱包私钥

	CoinPoolAPIURL string

	// AI配置
	UseQwen     bool
	DeepSeekKey string
	QwenKey     string

	// 自定义AI API配置
	CustomAPIURL    string
	CustomAPIKey    string
	CustomModelName string

	// 扫描配置
	ScanInterval time.Duration // 扫描间隔（建议3分钟）

	// 账户配置
	InitialBalance float64 // 初始金额（用于计算盈亏，需手动设置）

	// 杠杆配置
	BTCETHLeverage  int // BTC和ETH的杠杆倍数
	AltcoinLeverage int // 山寨币的杠杆倍数

	// 风险控制（仅作为提示，AI可自主决定）
	MaxDailyLoss               float64       // 最大日亏损百分比（提示）
	MaxDrawdown                float64       // 最大回撤百分比（提示）
	StopTradingTime            time.Duration // 触发风控后暂停时长
	MaxRiskPerTrade            float64       // 单笔风险预算
	TotalRiskBudget            float64       // 总风险预算
	AnalysisIntervalMin        int           // AI新机会分析间隔
	EnableEmergencyClose       bool          // 止损保护无法建立时是否紧急平仓
	FrequencyPolicy            decision.FrequencyPolicy
	StrategyRiskPolicy         decision.StrategyRiskPolicy
	ProgrammaticStrategyPolicy decision.ProgrammaticStrategyPolicy
	ChanlunV2StrategyConfig    config.ChanlunV2StrategyConfig
}

const (
	DefaultMaxRiskPerTrade   = 0.02
	DefaultTotalRiskBudget   = 0.08
	DefaultAnalysisInterval  = 15
	defaultAIBackoffInterval = time.Minute

	DefaultMarketKlineLimit = 240
	MaxMarketKlineLimit     = 1000
)

var getMarketData = market.Get

var marketKlineFetcher = market.GetKlines

type MarketKlineLimitResolution struct {
	Limit           int
	ConfiguredLimit int
	LimitSource     string
}

func SetMarketKlineFetcherForTest(fetcher func(symbol, timeframe string, limit int, closedOnly bool) ([]market.Kline, error)) func() {
	previous := marketKlineFetcher
	if fetcher == nil {
		marketKlineFetcher = market.GetKlines
	} else {
		marketKlineFetcher = fetcher
	}
	return func() {
		marketKlineFetcher = previous
	}
}

// PositionSnapshot 持仓快照（用于检测自动平仓）
type PositionSnapshot struct {
	Symbol     string
	Side       string
	Quantity   float64
	EntryPrice float64
	Leverage   int
}

// AutoTrader 自动交易器
type AutoTrader struct {
	id                    string // Trader唯一标识
	name                  string // Trader显示名称
	aiModel               string // AI模型名称
	exchange              string // 交易平台名称
	config                AutoTraderConfig
	trader                Trader // 使用Trader接口（支持多平台）
	mcpClient             *mcp.Client
	programmaticEngine    *chanlun.Engine
	chanlunV2Engine       ChanlunV2EngineInterface
	decisionLogger        *logger.DecisionLogger // 决策日志记录器
	initialBalance        float64
	dailyPnL              float64
	lastResetTime         time.Time
	stopUntil             time.Time
	isRunning             bool
	startTime             time.Time                    // 系统启动时间
	callCount             int                          // AI调用次数
	positionFirstSeenTime map[string]int64             // 持仓首次出现时间 (symbol_side -> timestamp毫秒)
	lastPositions         map[string]*PositionSnapshot // 上一个周期的持仓快照 (symbol_side -> snapshot)
	orderTracker          *OrderTracker                // 🆕 新增：订单追踪器
	lastOrderSyncTime     time.Time                    // 🆕 新增：上次订单同步时间
	lastAnalysisTime      time.Time                    // 上次 AI 新机会分析时间
	lastAIAttemptTime     time.Time                    // 上次 AI 调用尝试时间
	lastAISuccessTime     time.Time                    // 上次 AI 调用成功时间
	aiBackoffUntil        time.Time                    // AI 调用失败后的退避结束时间
	lastAIError           string                       // 最近一次 AI 失败原因
	consecutiveAIFails    int                          // 连续 AI 失败次数
	autoCloseDedupe       map[string]time.Time         // 自动平仓事件去重
	closedPositionDedupe  map[string]time.Time         // 已确认平仓的持仓生命周期去重
}

func (at *AutoTrader) GetTrader() Trader {
	return at.trader
}

// NewAutoTrader 创建自动交易器
func NewAutoTrader(config AutoTraderConfig) (*AutoTrader, error) {
	// 设置默认值
	if config.ID == "" {
		config.ID = "default_trader"
	}
	if config.Name == "" {
		config.Name = "Default Trader"
	}
	if config.AIModel == "" {
		if config.DecisionMode == "programmatic" {
			config.AIModel = "programmatic"
		} else if config.DecisionMode == "chanlun_v2" {
			config.AIModel = "chanlun_v2"
		} else if config.UseQwen {
			config.AIModel = "qwen"
		} else {
			config.AIModel = "deepseek"
		}
	}
	if config.DecisionMode == "" {
		config.DecisionMode = "ai"
	}
	if config.MaxRiskPerTrade <= 0 {
		config.MaxRiskPerTrade = DefaultMaxRiskPerTrade
	}
	if config.TotalRiskBudget <= 0 {
		config.TotalRiskBudget = DefaultTotalRiskBudget
	}
	if config.AnalysisIntervalMin <= 0 {
		config.AnalysisIntervalMin = DefaultAnalysisInterval
	}
	if config.FrequencyPolicy.Mode == "" {
		config.FrequencyPolicy = decision.FrequencyPolicy{
			Mode:                 "legacy",
			EffectiveMode:        "legacy",
			AnalysisIntervalMin:  config.AnalysisIntervalMin,
			PromptCandidateLimit: 8,
		}
	}
	if config.FrequencyPolicy.EffectiveMode == "" {
		config.FrequencyPolicy.EffectiveMode = config.FrequencyPolicy.Mode
	}
	if config.FrequencyPolicy.AnalysisIntervalMin <= 0 {
		config.FrequencyPolicy.AnalysisIntervalMin = config.AnalysisIntervalMin
	}
	config.AnalysisIntervalMin = config.FrequencyPolicy.AnalysisIntervalMin
	if config.StrategyRiskPolicy.ADXTimeframe == "" {
		config.StrategyRiskPolicy = decision.StrategyRiskPolicy{
			Legacy:                   true,
			Enabled:                  false,
			RollbackLegacyValidation: true,
			FeeSlippagePct:           0.002,
			DefaultMinNetRR:          2.5,
			ADXTimeframe:             "1h",
		}
	}

	var mcpClient *mcp.Client
	var programmaticEngine *chanlun.Engine
	var chanlunV2Engine ChanlunV2EngineInterface
	if config.DecisionMode == "programmatic" {
		config.ProgrammaticStrategyPolicy.DecisionMode = "programmatic"
		engine, engineErr := chanlun.NewEngine(config.ProgrammaticStrategyPolicy)
		if engineErr != nil {
			return nil, fmt.Errorf("初始化程序化策略引擎失败: %w", engineErr)
		}
		programmaticEngine = engine
		log.Printf("🧮 [%s] 使用程序化策略: %s %s",
			config.Name,
			programmaticEngine.Policy.StrategyName,
			programmaticEngine.Policy.StrategyVersion)
	} else if config.DecisionMode == "chanlun_v2" {
		v2Eng, v2Err := chanlunv2.NewEngine(config.ChanlunV2StrategyConfig)
		if v2Err != nil {
			return nil, fmt.Errorf("初始化缠论V2策略引擎失败: %w", v2Err)
		}
		chanlunV2Engine = v2Eng
		log.Printf("🧮 [%s] 使用缠论V2策略 (Rust引擎)", config.Name)
	} else {
		mcpClient = mcp.New()
		// 初始化AI
		if config.AIModel == "custom" {
			// 使用自定义API
			mcpClient.SetCustomAPI(config.CustomAPIURL, config.CustomAPIKey, config.CustomModelName)
			log.Printf("🤖 [%s] 使用自定义AI API: %s (模型: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)
		} else if config.UseQwen || config.AIModel == "qwen" {
			// 使用Qwen
			mcpClient.SetQwenAPIKey(config.QwenKey, "")
			log.Printf("🤖 [%s] 使用阿里云Qwen AI", config.Name)
		} else {
			// 默认使用DeepSeek
			mcpClient.SetDeepSeekAPIKey(config.DeepSeekKey)
			log.Printf("🤖 [%s] 使用DeepSeek AI", config.Name)
		}
	}

	// 初始化币种池API
	if config.CoinPoolAPIURL != "" {
		pool.SetCoinPoolAPI(config.CoinPoolAPIURL)
	}

	// 设置默认交易平台
	if config.Exchange == "" {
		config.Exchange = "binance"
	}

	// 根据配置创建对应的交易器
	var trader Trader
	var err error

	switch config.Exchange {
	case "binance":
		log.Printf("🏦 [%s] 使用币安合约交易", config.Name)
		trader = NewFuturesTrader(config.BinanceAPIKey, config.BinanceSecretKey)
	case "hyperliquid":
		log.Printf("🏦 [%s] 使用Hyperliquid交易", config.Name)
		trader, err = NewHyperliquidTrader(config.HyperliquidPrivateKey, config.HyperliquidWalletAddr, config.HyperliquidTestnet)
		if err != nil {
			return nil, fmt.Errorf("初始化Hyperliquid交易器失败: %w", err)
		}
	case "aster":
		log.Printf("🏦 [%s] 使用Aster交易", config.Name)
		trader, err = NewAsterTrader(config.AsterUser, config.AsterSigner, config.AsterPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("初始化Aster交易器失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的交易平台: %s", config.Exchange)
	}

	// 验证初始金额配置
	if config.InitialBalance <= 0 {
		return nil, fmt.Errorf("初始金额必须大于0，请在配置中设置InitialBalance")
	}

	// 初始化决策日志记录器（使用trader ID创建独立目录）
	logDir := fmt.Sprintf("decision_logs/%s", config.ID)
	decisionLogger := logger.NewDecisionLogger(logDir)

	at := &AutoTrader{
		id:                    config.ID,
		name:                  config.Name,
		aiModel:               config.AIModel,
		exchange:              config.Exchange,
		config:                config,
		trader:                trader,
		mcpClient:             mcpClient,
		programmaticEngine:    programmaticEngine,
		chanlunV2Engine:       chanlunV2Engine,
		decisionLogger:        decisionLogger,
		initialBalance:        config.InitialBalance,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		autoCloseDedupe:       make(map[string]time.Time),
	}
	// 🆕 初始化订单追踪器
	at.orderTracker = NewOrderTracker(at.trader)
	return at, nil
}

// Run 运行自动交易主循环
func (at *AutoTrader) Run() error {
	at.isRunning = true
	switch at.GetDecisionMode() {
	case "programmatic":
		log.Println("🚀 程序化策略自动交易系统启动")
	case "chanlun_v2":
		log.Println("🚀 缠论V2策略自动交易系统启动")
	default:
		log.Println("🚀 AI驱动自动交易系统启动")
	}
	log.Printf("💰 初始余额: %.2f USDT", at.initialBalance)
	log.Printf("⚙️  扫描间隔: %v", at.config.ScanInterval)
	switch at.GetDecisionMode() {
	case "programmatic":
		log.Println("🧮 程序化策略将生成开仓、加仓、减仓和平仓决策")
	case "chanlun_v2":
		log.Println("🧩 缠论V2策略将生成交易决策")
	default:
		log.Println("🤖 AI将全权决定杠杆、仓位大小、止损止盈等参数")
	}

	// ✅ 新增：启动时同步现有持仓的交易计划
	if err := at.syncExistingPositions(); err != nil {
		log.Printf("⚠️ 同步现有持仓计划失败: %v", err)
	}

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// 首次立即执行
	if err := at.runCycle(); err != nil {
		log.Printf("❌ 执行失败: %v", err)
	}

	for at.isRunning {
		select {
		case <-ticker.C:
			if err := at.runCycle(); err != nil {
				log.Printf("❌ 执行失败: %v", err)
			}
		}
	}

	return nil
}

// ✅ 新增：同步现有持仓的交易计划
func (at *AutoTrader) syncExistingPositions() error {
	log.Println("🔄 正在同步现有持仓的交易计划...")

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	if len(positions) == 0 {
		log.Println("  ℹ️ 当前无持仓，无需同步")
		return nil
	}

	// 转换为 decision.PositionInfo 格式
	var positionInfos []decision.PositionInfo
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		startTime := at.resolvePositionStartTime(symbol, side, extractPositionTimestampMillis(pos))

		positionInfos = append(positionInfos, decision.PositionInfo{
			Symbol:     symbol,
			Side:       side,
			EntryPrice: entryPrice,
			MarkPrice:  markPrice,
			Quantity:   quantity,
			Leverage:   leverage,
			UpdateTime: startTime,
		})
	}

	// 获取市场数据
	marketDataMap := make(map[string]*market.Data)
	for _, pos := range positionInfos {
		data, err := getMarketData(pos.Symbol)
		if err != nil {
			log.Printf("  ⚠️ 获取 %s 市场数据失败: %v", pos.Symbol, err)
			continue
		}
		marketDataMap[pos.Symbol] = data
	}

	// 调用决策模块同步计划
	decision.SyncPlansFromPositionsScoped(at.id, positionInfos, marketDataMap)

	log.Printf("  ✅ 已同步 %d 个持仓的交易计划", len(positionInfos))
	return nil
}

// Stop 停止自动交易
func (at *AutoTrader) Stop() {
	at.isRunning = false
	log.Println("⏹ 自动交易系统停止")
}

// runCycle 运行一个交易周期。
func (at *AutoTrader) runCycle() error {
	at.callCount++

	cycleLabel := at.decisionModeLabel()
	log.Print("\n" + strings.Repeat("=", 70))
	log.Printf("⏰ %s - %s周期 #%d", time.Now().Format("2006-01-02 15:04:05"), cycleLabel, at.callCount)
	log.Print(strings.Repeat("=", 70))

	// 🆕 **关键步骤**: 在每个周期开始时检查自动成交的订单
	at.syncAutoClosedOrders()

	// 创建决策记录
	record := &logger.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
		DecisionMode: at.GetDecisionMode(),
	}

	// 1. 检查是否需要停止交易
	if time.Now().Before(at.stopUntil) {
		remaining := at.stopUntil.Sub(time.Now())
		log.Printf("⏸ 风险控制：暂停交易中，剩余 %.0f 分钟", remaining.Minutes())
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("风险控制暂停中，剩余 %.0f 分钟", remaining.Minutes())
		at.decisionLogger.LogDecision(record)
		return nil
	}

	// 2. 重置日盈亏（每天重置）
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		log.Println("📅 日盈亏已重置")
	}

	// 3. 收集交易上下文
	ctx, err := at.buildTradingContext()
	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("构建交易上下文失败: %v", err)
		at.decisionLogger.LogDecision(record)
		return fmt.Errorf("构建交易上下文失败: %w", err)
	}

	// 3.1 检测自动平仓（止损/止盈触发）
	autoClosedActions := at.detectAutoClosedPositions(ctx.Positions)
	autoClosedActions = append(autoClosedActions, at.reconcileStaleTradePlans(ctx.Positions)...)
	for _, action := range autoClosedActions {
		log.Printf("[AUTO-CLOSE] 检测到自动平仓: %s %s (价格: %.4f)", action.Symbol, action.Action, action.Price)
		record.Decisions = append(record.Decisions, action)
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("[AUTO-CLOSE] 自动平仓: %s %s (止损/止盈触发)", action.Symbol, action.Action))
	}
	at.updatePositionSnapshots(ctx.Positions)

	// 保存账户状态快照
	record.AccountState = logger.AccountSnapshot{
		TotalBalance:          ctx.Account.TotalEquity,
		AvailableBalance:      ctx.Account.AvailableBalance,
		TotalUnrealizedProfit: ctx.Account.TotalPnL,
		PositionCount:         ctx.Account.PositionCount,
		MarginUsedPct:         ctx.Account.MarginUsedPct,
		CostBasis:             ctx.Account.CostBasis,
		RealizedPnL:           ctx.Account.RealizedPnL,
		PnLSource:             ctx.Account.PnLSource,
		TotalRealized24h:      ctx.Account.TotalRealized24h,
	}

	// 保存持仓快照
	for _, pos := range ctx.Positions {
		record.Positions = append(record.Positions, logger.PositionSnapshot{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			PositionAmt:      pos.Quantity,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedProfit: pos.UnrealizedPnL,
			Leverage:         float64(pos.Leverage),
			LiquidationPrice: pos.LiquidationPrice,
		})
	}

	at.fillCandidateSnapshots(record, ctx)

	log.Printf("📊 账户净值: %.2f USDT | 可用: %.2f USDT | 持仓: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// 4. 按 trader 决策模式获取完整决策
	log.Println(at.decisionModeActionLog())
	fullDecision, err := at.getFullDecision(ctx)

	// 即使有错误，也保存思维链、决策和输入prompt（用于debug）
	if fullDecision != nil {
		record.InputPrompt = fullDecision.UserPrompt
		record.CoTTrace = fullDecision.CoTTrace
		record.DecisionMode = firstNonEmpty(fullDecision.DecisionMode, at.GetDecisionMode())
		record.StrategyName = fullDecision.StrategyName
		record.StrategyVersion = fullDecision.StrategyVersion
		record.ConfigHash = fullDecision.ConfigHash
		record.WaitReasonSummary = fullDecision.WaitReasonSummary
		record.StrategyParams = copyAnyMap(fullDecision.StrategyParams)
		record.StrategyDiagnostics = copyAnyMap(fullDecision.StrategyDiagnostics)
		if len(fullDecision.Decisions) > 0 {
			decisionJSON, _ := json.MarshalIndent(fullDecision.Decisions, "", "  ")
			record.DecisionJSON = string(decisionJSON)
		}
		at.appendOpenRejectionsToRecord(record, fullDecision.OpenRejections)
		record.RiskState = at.buildRiskStateSnapshot(ctx, fullDecision.OpenRejections)
		applyStrategyDiagnosticsToRecord(record)
		at.fillCandidateSnapshots(record, ctx)
		at.applyAICallState(fullDecision)
	}

	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("获取%s失败: %v", cycleLabel, err)

		// 打印AI思维链（即使有错误）
		if fullDecision != nil && fullDecision.CoTTrace != "" {
			log.Print("\n" + strings.Repeat("-", 70))
			log.Printf("💭 %s分析摘要（错误情况）:", cycleLabel)
			log.Println(strings.Repeat("-", 70))
			log.Println(fullDecision.CoTTrace)
			log.Print(strings.Repeat("-", 70) + "\n")
		}

		at.decisionLogger.LogDecision(record)
		return fmt.Errorf("获取%s失败: %w", cycleLabel, err)
	}

	// 5. 打印分析摘要
	log.Print("\n" + strings.Repeat("-", 70))
	log.Printf("💭 %s分析摘要:", cycleLabel)
	log.Println(strings.Repeat("-", 70))
	log.Println(fullDecision.CoTTrace)
	log.Print(strings.Repeat("-", 70) + "\n")

	// 6. 打印决策
	log.Printf("📋 %s决策列表 (%d 个):\n", cycleLabel, len(fullDecision.Decisions))
	for i, d := range fullDecision.Decisions {
		log.Printf("  [%d] %s: %s - %s", i+1, d.Symbol, d.Action, d.Reasoning)
		if decision.IsOpenLikeAction(d.Action) {
			log.Printf("      杠杆: %dx | 仓位: %.2f USDT | 止损: %.4f | 止盈: %.4f",
				d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
		}
	}
	log.Println()

	// 7. 对决策排序：确保先平仓后开仓（防止仓位叠加超限）
	sortedDecisions := sortDecisionsByPriority(fullDecision.Decisions)

	log.Println("🔄 执行顺序（已优化）: 先平仓→后开仓")
	for i, d := range sortedDecisions {
		log.Printf("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	log.Println()

	// 执行决策并记录结果
	for _, d := range sortedDecisions {
		actionRecord := logger.DecisionAction{
			Action:    d.Action,
			Symbol:    d.Symbol,
			Quantity:  0,
			Leverage:  d.Leverage,
			Price:     0,
			Timestamp: time.Now(),
			Success:   false,
			Reasoning: d.Reasoning,
		}
		applyDecisionSizingToActionRecord(&d, &actionRecord)

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			log.Printf("❌ 执行决策失败 (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("❌ %s %s 失败: %v", d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("✓ %s %s 成功", d.Symbol, d.Action))
			// 成功执行后短暂延迟
			time.Sleep(1 * time.Second)
		}
		at.reportProgrammaticExecutionResult(&d, &actionRecord)

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 8. 保存决策记录
	if err := at.decisionLogger.LogDecision(record); err != nil {
		log.Printf("⚠ 保存决策记录失败: %v", err)
	}

	return nil
}

func (at *AutoTrader) decisionModeLabel() string {
	mode := at.GetDecisionMode()
	switch mode {
	case "ai":
		return "AI决策"
	case "programmatic":
		return "程序化策略"
	case "chanlun_v2":
		return "缠论V2策略"
	default:
		return fmt.Sprintf("策略(%s)", mode)
	}
}

func (at *AutoTrader) decisionModeActionLog() string {
	switch at.GetDecisionMode() {
	case "programmatic":
		return "🧮 正在运行程序化策略分析并决策..."
	case "chanlun_v2":
		return "🧩 正在运行缠论V2策略分析并决策..."
	default:
		return "🤖 正在请求AI分析并决策..."
	}
}

func (at *AutoTrader) getFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if at.config.DecisionMode == "programmatic" {
		if at.programmaticEngine == nil {
			return nil, fmt.Errorf("程序化策略引擎未初始化")
		}
		return at.programmaticEngine.GetFullDecision(ctx)
	}
	if at.config.DecisionMode == "chanlun_v2" {
		if at.chanlunV2Engine == nil {
			return nil, fmt.Errorf("缠论V2策略引擎未初始化")
		}
		return at.chanlunV2Engine.GetFullDecision(ctx)
	}
	return decision.GetFullDecision(ctx, at.mcpClient)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func copyAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func applyStrategyDiagnosticsToRecord(record *logger.DecisionRecord) {
	if record == nil || len(record.StrategyDiagnostics) == 0 {
		return
	}
	if accountState, ok := record.StrategyDiagnostics["account_state"].(map[string]any); ok {
		record.AccountState.AccountTooSmall = actionRecordMetadataBool(accountState, "account_too_small")
		record.AccountState.TotalRealized24h = actionRecordMetadataFloat(accountState, "total_realized_24h")
	}
	if record.RiskState == nil {
		return
	}
	if riskState, ok := record.StrategyDiagnostics["risk_state"].(map[string]any); ok {
		if value, _ := riskState["active_mode"].(string); value != "" {
			record.RiskState.ActiveMode = value
		}
		if value := actionRecordMetadataFloat(riskState, "inactivity_minutes"); value > 0 {
			record.RiskState.InactivityMinutes = int(value)
		}
		if value, _ := riskState["last_open_at"].(string); value != "" {
			record.RiskState.LastOpenAt = value
		}
		if value, _ := riskState["last_close_at"].(string); value != "" {
			record.RiskState.LastCloseAt = value
		}
		if value := actionRecordMetadataFloat(riskState, "open_count_24h"); value > 0 {
			record.RiskState.OpenCount24h = int(value)
		}
		if value := actionRecordMetadataFloat(riskState, "open_rejected_24h"); value > 0 {
			record.RiskState.OpenRejected24h = int(value)
		}
		if value := actionRecordMetadataFloat(riskState, "signal_count_24h"); value > 0 {
			record.RiskState.SignalCount24h = int(value)
		}
		if value, ok := riskState["gate_effectiveness"].(map[string]any); ok {
			record.RiskState.GateEffectiveness = copyAnyMap(value)
		}
		if value, ok := riskState["suppressions"].(map[string]any); ok {
			record.RiskState.Suppressions = copyAnyMap(value)
		}
		if value, ok := riskState["warnings"].(map[string]bool); ok {
			record.RiskState.Warnings = value
		} else if value, ok := riskState["warnings"].(map[string]any); ok {
			warnings := map[string]bool{}
			for key, raw := range value {
				if enabled, _ := raw.(bool); enabled {
					warnings[key] = true
				}
			}
			if len(warnings) > 0 {
				record.RiskState.Warnings = warnings
			}
		}
	}
}

func applyDecisionSizingToActionRecord(d *decision.Decision, actionRecord *logger.DecisionAction) {
	if d == nil || actionRecord == nil {
		return
	}
	actionRecord.RiskUSD = d.RiskUSD
	actionRecord.RequestedPositionSizeUSD = d.RequestedPositionSizeUSD
	actionRecord.AdjustedPositionSizeUSD = d.AdjustedPositionSizeUSD
	actionRecord.SizingAdjusted = d.SizingAdjusted
	actionRecord.SizingReason = d.SizingReason
	actionRecord.StopDistancePct = d.StopDistancePct
	actionRecord.StopDistanceRatio = d.StopDistanceRatio
	actionRecord.StopDistancePercent = d.StopDistancePercent
	actionRecord.TakeProfitRatio = d.TakeProfitRatio
	actionRecord.TakeProfitPercent = d.TakeProfitPercent
	actionRecord.RequestedStopLoss = d.RequestedStopLoss
	actionRecord.RequestedTakeProfit = d.RequestedTakeProfit
	actionRecord.EffectiveStopLoss = d.EffectiveStopLoss
	actionRecord.EffectiveTakeProfit = d.EffectiveTakeProfit
	actionRecord.ExchangeFullTakeProfit = d.ExchangeFullTakeProfit
	actionRecord.ExchangeFullTPMode = d.ExchangeFullTPMode
	actionRecord.NetRR = d.NetRR
	actionRecord.ProfileName = d.ProfileName
	actionRecord.FeeSlippageReserveUSD = d.FeeSlippageReserveUSD
	actionRecord.TotalRiskUSD = d.TotalRiskUSD
	actionRecord.TotalRiskPct = d.TotalRiskPct
	actionRecord.RiskCapReason = d.RiskCapReason
	actionRecord.RiskNormalization = d.RiskNormalization
	actionRecord.EffectiveRiskPct = d.EffectiveRiskPct
	actionRecord.StrategyMode = d.StrategyMode
	actionRecord.StrategyName = d.StrategyName
	actionRecord.StrategyVersion = d.StrategyVersion
	actionRecord.ConfigHash = d.ConfigHash
	actionRecord.SignalID = d.SignalID
	actionRecord.SignalType = d.SignalType
	actionRecord.SignalTimeframe = d.SignalTimeframe
	actionRecord.StructureTarget = d.StructureTarget
	actionRecord.TradeIntent = metadataStringValue(d.StrategyMetadata, "trade_intent")
	if actionRecord.TradeIntent == "" {
		actionRecord.TradeIntent = d.Action
	}
	actionRecord.SignalCloseTime = metadataInt64ValueAny(d.StrategyMetadata, "signal_close_time", "trigger_close_time", "segment_end_time")
	actionRecord.DecisionCloseTime = metadataInt64ValueAny(d.StrategyMetadata, "decision_close_time")
	actionRecord.EvaluationCloseTime = metadataInt64ValueAny(d.StrategyMetadata, "evaluation_close_time", "decision_close_time")
	actionRecord.ActionTimestamp = metadataInt64ValueAny(d.StrategyMetadata, "action_timestamp")
	actionRecord.FreshnessState = metadataStringValue(d.StrategyMetadata, "freshness_state")
	actionRecord.AgeCandles = metadataIntValue(d.StrategyMetadata, "age_candles")
	actionRecord.StaleReason = metadataStringValue(d.StrategyMetadata, "stale_reason")
	actionRecord.StrategyMetadata = copyAnyMap(d.StrategyMetadata)
	actionRecord.StrategyDiagnostics = copyAnyMap(d.StrategyDiagnosis)
	actionRecord.RequestedClosePercentage = d.ClosePercentage
	actionRecord.FinalAction = d.Action
	actionRecord.Explanation = d.Explanation
}

func (at *AutoTrader) reportProgrammaticExecutionResult(d *decision.Decision, actionRecord *logger.DecisionAction) {
	if at == nil || at.programmaticEngine == nil || d == nil || actionRecord == nil || d.StrategyMode != "programmatic" {
		return
	}
	finalAction := actionRecord.FinalAction
	if strings.TrimSpace(finalAction) == "" {
		finalAction = d.Action
	}
	at.programmaticEngine.OnExecutionResult(chanlun.ProgrammaticExecutionResult{
		TraderID:                 at.id,
		Decision:                 *d,
		Success:                  actionRecord.Success,
		FinalAction:              finalAction,
		RequestedClosePercentage: actionRecord.RequestedClosePercentage,
		ExecutedClosePercentage:  actionRecord.ExecutedClosePercentage,
		ExecutedQuantity:         actionRecord.CloseQuantity,
		PositionQuantityBefore:   actionRecordMetadataFloat(actionRecord.StrategyMetadata, "position_quantity_before"),
		Price:                    actionRecord.Price,
		Error:                    actionRecord.Error,
		ExecutedAt:               actionRecord.Timestamp,
	})
}

func actionRecordMetadataFloat(values map[string]any, key string) float64 {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func actionRecordMetadataBool(values map[string]any, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func metadataStringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func metadataIntValue(values map[string]any, key string) int {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case int32:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	default:
		return 0
	}
}

func metadataInt64ValueAny(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := metadataInt64Value(values, key); ok && value != 0 {
			return value
		}
	}
	return 0
}

func metadataInt64Value(values map[string]any, key string) (int64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	switch value := values[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case float64:
		return int64(value), true
	case float32:
		return int64(value), true
	default:
		return 0, false
	}
}

func (at *AutoTrader) applyAICallState(fullDecision *decision.FullDecision) {
	if fullDecision == nil || !fullDecision.AICallAttempted {
		return
	}

	attemptTime := fullDecision.Timestamp
	if attemptTime.IsZero() {
		attemptTime = time.Now()
	}

	at.lastAIAttemptTime = attemptTime

	if fullDecision.AICallSucceeded {
		at.lastAnalysisTime = attemptTime
		at.lastAISuccessTime = attemptTime
		at.consecutiveAIFails = 0
		at.aiBackoffUntil = time.Time{}
		at.lastAIError = ""
		return
	}

	at.consecutiveAIFails++
	at.lastAIError = fullDecision.AIFailureReason
	backoff := at.config.ScanInterval
	if backoff <= 0 {
		backoff = defaultAIBackoffInterval
	}
	at.aiBackoffUntil = attemptTime.Add(backoff)
}

func (at *AutoTrader) appendOpenRejectionsToRecord(record *logger.DecisionRecord, rejections []decision.OpenRejection) {
	if record == nil {
		return
	}
	for _, rejection := range rejections {
		reason := strings.TrimSpace(rejection.Reason)
		if reason == "" {
			reason = strings.Join(rejection.GateReasons, "; ")
		}
		actionTime := time.Now()
		actionTimestamp := rejection.ActionTimestamp
		if actionTimestamp <= 0 {
			actionTimestamp = actionTime.UnixMilli()
		}
		record.Decisions = append(record.Decisions, logger.DecisionAction{
			Action:              "open_rejected",
			Symbol:              rejection.Symbol,
			Timestamp:           actionTime,
			Success:             false,
			Error:               reason,
			Reasoning:           reason,
			GateState:           rejection.GateState,
			GateReasons:         append([]string(nil), rejection.GateReasons...),
			GateDiagnostics:     copyGateDiagnostics(rejection.GateDiagnostics),
			Simulations:         copyOpenFrequencySimulations(rejection.Simulations),
			StrategyMode:        rejection.StrategyMode,
			StrategyName:        rejection.StrategyName,
			StrategyVersion:     rejection.StrategyVersion,
			ConfigHash:          rejection.ConfigHash,
			SignalID:            rejection.SignalID,
			SignalType:          rejection.SignalType,
			SignalTimeframe:     rejection.SignalTimeframe,
			TradeIntent:         rejection.TradeIntent,
			SignalCloseTime:     rejection.SignalCloseTime,
			DecisionCloseTime:   rejection.DecisionCloseTime,
			EvaluationCloseTime: rejection.EvaluationCloseTime,
			ActionTimestamp:     actionTimestamp,
			FreshnessState:      rejection.FreshnessState,
			AgeCandles:          rejection.AgeCandles,
			StaleReason:         rejection.StaleReason,
			StrategyMetadata:    copyAnyMap(rejection.StrategyMetadata),
		})
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("⚠ %s %s 被开仓门控拒绝: %s", rejection.Symbol, rejection.Action, reason))
	}
}

func (at *AutoTrader) buildRiskStateSnapshot(ctx *decision.Context, rejections []decision.OpenRejection) *logger.RiskStateSnapshot {
	if ctx == nil {
		return nil
	}
	usedRisk, _ := decision.CalculateTotalRisk(ctx)
	remainingRisk := ctx.TotalRiskBudget - usedRisk
	if remainingRisk < 0 {
		remainingRisk = 0
	}
	snapshot := &logger.RiskStateSnapshot{
		TraderID:                 ctx.TraderID,
		Exchange:                 ctx.Exchange,
		MaxRiskPerTrade:          ctx.MaxRiskPerTrade,
		EffectiveMaxRiskPerTrade: ctx.EffectiveMaxRiskPerTrade,
		TotalRiskBudget:          ctx.TotalRiskBudget,
		RemainingRiskBudget:      remainingRisk,
		MaxDailyLossPct:          ctx.MaxDailyLossPct,
		MaxAccountDrawdownPct:    ctx.MaxAccountDrawdownPct,
		ConsecutiveAIFails:       ctx.ConsecutiveAIFails,
		FrequencyPolicy:          copyFrequencyPolicySnapshot(ctx.FrequencyPolicy),
		FrequencyState:           copyFrequencyStateSnapshot(ctx.FrequencyState),
		LossMode:                 copyLossModeSnapshot(ctx.LossMode),
	}
	if ctx.FrequencyPolicy != nil {
		snapshot.ActiveMode = firstNonEmpty(ctx.FrequencyPolicy.EffectiveMode, ctx.FrequencyPolicy.Mode)
	}
	if ctx.FrequencyState != nil {
		snapshot.OpenCount24h = ctx.FrequencyState.OpenCount24h
		snapshot.OpenRejected24h = ctx.FrequencyState.OpenRejected24h
		snapshot.SignalCount24h = ctx.FrequencyState.SignalCount24h
		if !ctx.FrequencyState.LastOpenAt.IsZero() {
			snapshot.LastOpenAt = ctx.FrequencyState.LastOpenAt.Format(time.RFC3339)
			snapshot.InactivityMinutes = int(time.Since(ctx.FrequencyState.LastOpenAt).Minutes())
		} else if ctx.RuntimeMinutes > 0 {
			snapshot.InactivityMinutes = ctx.RuntimeMinutes
		}
		if !ctx.FrequencyState.LastCloseAt.IsZero() {
			snapshot.LastCloseAt = ctx.FrequencyState.LastCloseAt.Format(time.RFC3339)
		}
	}
	if snapshot.OpenRejected24h >= 10 && snapshot.OpenCount24h == 0 {
		snapshot.Warnings = map[string]bool{"runaway_rejection_loop": true}
	}
	if !ctx.AIBackoffUntil.IsZero() {
		snapshot.AIBackoffUntil = ctx.AIBackoffUntil.Format(time.RFC3339)
	}
	for _, rejection := range rejections {
		if len(rejection.GateDiagnostics) > 0 {
			snapshot.OpenGateDiagnostics = append(snapshot.OpenGateDiagnostics, copyGateDiagnostics(rejection.GateDiagnostics))
		}
		if rejection.Reason != "" {
			snapshot.OpenGateReasons = append(snapshot.OpenGateReasons, rejection.Reason)
			continue
		}
		snapshot.OpenGateReasons = append(snapshot.OpenGateReasons, rejection.GateReasons...)
	}
	return snapshot
}

func copyOpenFrequencySimulations(source []decision.OpenFrequencySimulation) []logger.OpenFrequencySimulationSnapshot {
	if len(source) == 0 {
		return nil
	}
	copied := make([]logger.OpenFrequencySimulationSnapshot, 0, len(source))
	for _, sim := range source {
		copied = append(copied, logger.OpenFrequencySimulationSnapshot{
			Scenario:        sim.Scenario,
			Source:          sim.Source,
			WouldAllow:      sim.WouldAllow,
			Reason:          sim.Reason,
			OriginalState:   sim.OriginalState,
			SimulatedState:  sim.SimulatedState,
			MinConfidence:   sim.MinConfidence,
			EffectiveRisk:   sim.EffectiveRisk,
			AdjustedSizeUSD: sim.AdjustedSizeUSD,
			Diagnostics:     copyGateDiagnostics(sim.Diagnostics),
		})
	}
	return copied
}

func copyFrequencyPolicySnapshot(policy *decision.FrequencyPolicy) *logger.FrequencyPolicySnapshot {
	if policy == nil {
		return nil
	}
	return &logger.FrequencyPolicySnapshot{
		Mode:                        policy.Mode,
		EffectiveMode:               policy.EffectiveMode,
		AnalysisIntervalMin:         policy.AnalysisIntervalMin,
		PromptCandidateLimit:        policy.PromptCandidateLimit,
		DailyOpenLimit:              policy.DailyOpenLimit,
		RollbackWindowHours:         policy.RollbackWindowHours,
		RollbackMinProfitFactor:     policy.RollbackMinProfitFactor,
		RollbackMaxDrawdownPct:      policy.RollbackMaxDrawdownPct,
		HighADXReportOnly:           policy.HighADXReportOnly,
		RRReportOnly:                policy.RRReportOnly,
		RollingGateReportOnly:       policy.RollingGateReportOnly,
		GateEffectivenessReportOnly: policy.GateEffectivenessReportOnly,
		LoosenMode: logger.LoosenModeSnapshot{
			Enabled:                  policy.LoosenMode.Enabled,
			InactivityWindowMinutes:  policy.LoosenMode.InactivityWindowMinutes,
			PilotConfidenceDrop:      policy.LoosenMode.PilotConfidenceDrop,
			MinNetRRDelta:            policy.LoosenMode.MinNetRRDelta,
			MaxChaseRatioBump:        policy.LoosenMode.MaxChaseRatioBump,
			MaxDurationHours:         policy.LoosenMode.MaxDurationHours,
			HardFloorPilotConfidence: policy.LoosenMode.HardFloorPilotConfidence,
		},
	}
}

func copyFrequencyStateSnapshot(state *decision.FrequencyState) *logger.FrequencyStateSnapshot {
	if state == nil {
		return nil
	}
	snapshot := &logger.FrequencyStateSnapshot{
		OpenCount24h:       state.OpenCount24h,
		ClosedTrades24h:    state.ClosedTrades24h,
		ProfitFactor24h:    state.ProfitFactor24h,
		Drawdown24hPct:     state.Drawdown24hPct,
		AutoRollbackActive: state.AutoRollbackActive,
		AutoRollbackReason: state.AutoRollbackReason,
		OpenRejected24h:    state.OpenRejected24h,
		SignalCount24h:     state.SignalCount24h,
	}
	if !state.LastOpenAt.IsZero() {
		snapshot.LastOpenAt = state.LastOpenAt.Format(time.RFC3339)
	}
	if !state.LastCloseAt.IsZero() {
		snapshot.LastCloseAt = state.LastCloseAt.Format(time.RFC3339)
	}
	return snapshot
}

func copyLossModeSnapshot(state *decision.LossModeState) *logger.LossModeSnapshot {
	if state == nil {
		return nil
	}
	snapshot := &logger.LossModeSnapshot{
		Active:          state.Active,
		Reason:          state.Reason,
		MaxRiskPerTrade: state.MaxRiskPerTrade,
		MaxPositions:    state.MaxPositions,
		DailyOpenLimit:  state.DailyOpenLimit,
		MinConfidence:   state.MinConfidence,
	}
	if !state.CooldownUntil.IsZero() {
		snapshot.CooldownUntil = state.CooldownUntil.Format(time.RFC3339)
	}
	return snapshot
}

func copyGateDiagnostics(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func (at *AutoTrader) fillCandidateSnapshots(record *logger.DecisionRecord, ctx *decision.Context) {
	if record == nil || ctx == nil {
		return
	}
	record.CandidateCoins = record.CandidateCoins[:0]
	record.CandidateDetails = record.CandidateDetails[:0]
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
		record.CandidateDetails = append(record.CandidateDetails, logger.CandidateSnapshot{
			Symbol:           coin.Symbol,
			Sources:          append([]string(nil), coin.Sources...),
			Score:            coin.Score,
			Tier:             coin.Tier,
			PoolScore:        coin.PoolScore,
			PoolReasons:      append([]string(nil), coin.PoolReasons...),
			MarketState:      coin.MarketState,
			StateConfidence:  coin.StateConfidence,
			DataQuality:      coin.DataQuality,
			FilterReason:     coin.FilterReason,
			IncludedInPrompt: coin.IncludedInPrompt,
			Warnings:         append([]string(nil), coin.Warnings...),
			Errors:           append([]string(nil), coin.Errors...),
		})
	}
}

// 🆕 syncAutoClosedOrders 同步自动成交的订单
func (at *AutoTrader) syncAutoClosedOrders() {
	log.Println("🔄 检查自动成交订单...")

	autoClosedOrders := at.orderTracker.CheckAutoClosedOrders()

	if len(autoClosedOrders) == 0 {
		log.Println("  ℹ️ 无自动成交订单")
		return
	}

	for _, order := range autoClosedOrders {
		if !at.claimAutoCloseEvent(order.Symbol, order.Side, order.OrderID, order.CloseTime) {
			log.Printf("📋 [AUTO-CLOSE] 跳过重复事件: %s %s order=%d", order.Symbol, order.Side, order.OrderID)
			continue
		}
		log.Printf("📋 [AUTO-CLOSE] 检测到自动平仓:")
		log.Printf("  • 币种: %s %s", order.Symbol, order.Side)
		log.Printf("  • 原因: %s", order.CloseReason)
		log.Printf("  • 入场价: %.4f → 出场价: %.4f", order.EntryPrice, order.ExitPrice)
		log.Printf("  • 盈亏: %.4f USDT (%.2f%%)", order.RealizedPnL, order.PnLPercent)
		log.Printf("  • 持仓时间: %.1f 分钟", order.HoldTimeMinutes)
		log.Printf("  • 手续费: %.4f USDT", order.Commission)

		at.handleAutoCloseEvent(autoCloseEvent{
			Symbol:          order.Symbol,
			Side:            order.Side,
			Source:          autoCloseSourceOrderTracker,
			OrderID:         order.OrderID,
			EntryPrice:      order.EntryPrice,
			ExitPrice:       order.ExitPrice,
			Quantity:        order.Quantity,
			Leverage:        order.Leverage,
			RealizedPnL:     order.RealizedPnL,
			PnLPercent:      order.PnLPercent,
			HoldTimeMinutes: order.HoldTimeMinutes,
			Commission:      order.Commission,
			CloseReason:     order.CloseReason,
			CloseTime:       order.CloseTime,
		}, true)
	}

	at.lastOrderSyncTime = time.Now()
}

// 🆕 logAutoClosedOrder 记录自动平仓到日志
func (at *AutoTrader) logAutoClosedOrder(order AutoClosedOrder) {
	action := "auto_close_long"
	if order.Side == "short" {
		action = "auto_close_short"
	}

	actionRecord := logger.DecisionAction{
		Action:    action,
		Symbol:    order.Symbol,
		Quantity:  order.Quantity,
		Price:     order.ExitPrice,
		OrderID:   order.OrderID,
		Timestamp: order.CloseTime,
		Success:   true,
		Error:     "",
	}

	// 创建单独的记录
	record := &logger.DecisionRecord{
		ExecutionLog: []string{
			fmt.Sprintf("[AUTO-CLOSE] %s %s 触发: %s", order.Symbol, order.Side, order.CloseReason),
			fmt.Sprintf("盈亏: %.4f USDT (%.2f%%)", order.RealizedPnL, order.PnLPercent),
		},
		Decisions: []logger.DecisionAction{actionRecord},
		Success:   true,
	}

	if err := at.decisionLogger.LogDecision(record); err != nil {
		log.Printf("⚠️ 记录自动平仓日志失败: %v", err)
	}
}

// buildTradingContext 构建交易上下文
func (at *AutoTrader) buildTradingContext() (*decision.Context, error) {
	// 1. 获取账户信息
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("获取账户余额失败: %w", err)
	}

	// 获取账户字段
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = 钱包余额 + 未实现盈亏
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// 2. 获取持仓信息
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var positionInfos []decision.PositionInfo
	totalMarginUsed := 0.0
	totalUnrealizedPnL := 0.0

	// 当前持仓的key集合（用于清理已平仓的记录）
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // 空仓数量为负，转为正数
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnL += unrealizedPnl
		liquidationPrice := pos["liquidationPrice"].(float64)

		// 计算占用保证金（估算）
		leverage := 10 // 默认值，实际应该从持仓信息获取
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// 计算盈亏百分比
		pnlPct := 0.0
		if side == "long" {
			pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// 跟踪持仓首次出现时间
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true
		updateTime := at.resolvePositionStartTime(symbol, side, extractPositionTimestampMillis(pos))

		positionInfos = append(positionInfos, decision.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnl,
			UnrealizedPnLPct: pnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
		})
	}

	// 清理已平仓的持仓记录，并撤销孤儿委托单
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			// 仓位消失了（可能被止损/止盈触发，或被强平）
			// 提取币种名称（key 格式：BTCUSDT_long 或 SOLUSDT_short）
			parts := strings.Split(key, "_")
			if len(parts) == 2 {
				symbol := parts[0]
				side := parts[1]
				log.Printf("⚠️ 检测到仓位消失: %s %s → 自动撤销委托单", symbol, side)

				// 撤销该币种的所有委托单（清理孤儿止损/止盈单）
				if err := at.trader.CancelAllOrders(symbol); err != nil {
					log.Printf("  ⚠️ 撤销 %s 委托单失败: %v", symbol, err)
				} else {
					log.Printf("  ✓ 已撤销 %s 的所有委托单", symbol)
				}
			}

			delete(at.positionFirstSeenTime, key)
		}
	}

	// 3. 分析历史表现（最近100个周期，避免长期持仓的交易记录丢失）
	// 假设每3分钟一个周期，100个周期 = 5小时，足够覆盖大部分交易
	performance, err := at.decisionLogger.AnalyzePerformance(100)
	if err != nil {
		log.Printf("⚠️  分析历史表现失败: %v", err)
		// 不影响主流程，继续执行（但设置performance为nil以避免传递错误数据）
		performance = nil
	}
	frequencyRecords := at.loadRecentDecisionRecords(500)
	frequencyState := at.buildFrequencyState(frequencyRecords, totalEquity)
	totalRealized24h := totalRealizedPnLSince(frequencyRecords, time.Now().Add(-24*time.Hour))
	frequencyPolicy := at.effectiveFrequencyPolicy(frequencyState)

	// 4. 获取候选币种池（动态候选池优先，失败时回退 AI500 + OI Top）
	// 无论有没有持仓，都分析相同数量的币种（让AI看到所有好机会）
	// AI会根据保证金使用率和现有持仓情况，自己决定是否要换仓
	const ai500Limit = 20 // AI500取前20个评分最高的币种

	positionSymbols := make([]string, 0, len(positionInfos))
	for _, pos := range positionInfos {
		positionSymbols = append(positionSymbols, pos.Symbol)
	}

	mergedPool, err := pool.GetDynamicMergedCoinPool(ai500Limit, positionSymbols, performance)
	if err != nil {
		return nil, fmt.Errorf("获取候选币种池失败: %w", err)
	}

	// 构建候选币种列表（包含来源信息）
	var candidateCoins []decision.CandidateCoin
	for _, symbol := range mergedPool.AllSymbols {
		sources := mergedPool.SymbolSources[symbol]
		coin := decision.CandidateCoin{
			Symbol:  symbol,
			Sources: sources,
		}
		if detail, ok := mergedPool.DynamicCandidates[symbol]; ok {
			coin.Tier = detail.Tier
			coin.PoolScore = detail.Score
			coin.PoolReasons = append([]string(nil), detail.Reasons...)
		}
		candidateCoins = append(candidateCoins, coin)
	}

	if pool.IsDynamicCandidatePoolEnabled() {
		log.Printf("📋 候选币种池: 动态候选池(regime=%s) = 总计%d个候选币种",
			mergedPool.MarketRegime, len(candidateCoins))
	} else {
		log.Printf("📋 合并币种池: AI500前%d + OI_Top20 = 总计%d个候选币种",
			ai500Limit, len(candidateCoins))
	}

	// 5. 计算交易盈亏：不要把充值/初始投入误算为利润。
	accountPnL := computeAccountPnLSummary(frequencyRecords, totalEquity, totalUnrealizedPnL, at.initialBalance)

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	baseMaxRiskPerTrade := at.config.MaxRiskPerTrade
	if baseMaxRiskPerTrade <= 0 {
		baseMaxRiskPerTrade = DefaultMaxRiskPerTrade
	}
	effectiveMaxRiskPerTrade := baseMaxRiskPerTrade
	var performanceGates *logger.RollingPerformanceSnapshot
	if performance != nil && performance.Rolling != nil {
		performanceGates = performance.Rolling
	}
	lossMode := decision.BuildLossModeState(performanceGates, time.Now())
	if lossMode != nil && lossMode.Active && lossMode.MaxRiskPerTrade > 0 && lossMode.MaxRiskPerTrade < effectiveMaxRiskPerTrade {
		effectiveMaxRiskPerTrade = lossMode.MaxRiskPerTrade
	}
	var executionQuality *logger.ExecutionQualityStats
	if performance != nil {
		executionQuality = &performance.Execution
	}

	// 6. 构建上下文
	ctx := &decision.Context{
		CurrentTime:              time.Now().Format("2006-01-02 15:04:05"),
		TraderID:                 at.id,
		Exchange:                 at.exchange,
		DecisionMode:             at.GetDecisionMode(),
		RuntimeMinutes:           int(time.Since(at.startTime).Minutes()),
		CallCount:                at.callCount,
		BTCETHLeverage:           at.config.BTCETHLeverage,  // 使用配置的杠杆倍数
		AltcoinLeverage:          at.config.AltcoinLeverage, // 使用配置的杠杆倍数
		MaxRiskPerTrade:          baseMaxRiskPerTrade,
		EffectiveMaxRiskPerTrade: effectiveMaxRiskPerTrade,
		TotalRiskBudget:          at.config.TotalRiskBudget,
		MaxDailyLossPct:          at.config.MaxDailyLoss,
		MaxAccountDrawdownPct:    decision.NormalizeAccountDrawdownPct(at.config.MaxDrawdown),
		LastAnalysisTime:         at.lastAnalysisTime,
		LastAIAttemptTime:        at.lastAIAttemptTime,
		LastAISuccessTime:        at.lastAISuccessTime,
		AIBackoffUntil:           at.aiBackoffUntil,
		LastAIError:              at.lastAIError,
		ConsecutiveAIFails:       at.consecutiveAIFails,
		AnalysisIntervalMin:      frequencyPolicy.AnalysisIntervalMin,
		FrequencyPolicy:          &frequencyPolicy,
		FrequencyState:           &frequencyState,
		LossMode:                 lossMode,
		StrategyRiskPolicy:       &at.config.StrategyRiskPolicy,
		Account: decision.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			TotalPnL:         accountPnL.TotalPnL,
			TotalPnLPct:      accountPnL.TotalPnLPct,
			CostBasis:        accountPnL.CostBasis,
			RealizedPnL:      accountPnL.RealizedPnL,
			PnLSource:        accountPnL.Source,
			TotalRealized24h: totalRealized24h,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:           positionInfos,
		CandidateCoins:      candidateCoins,
		QuoteSpreadProvider: at.quoteSpreadProvider(),
		Performance:         performance, // 添加历史表现分析
		PerformanceGates:    performanceGates,
		ExecutionQuality:    executionQuality,
	}
	// 注入全局熔断状态，确保每个周期的 Context 包含当前熔断状态
	ctx.CircuitBreaker = decision.GetCircuitBreakerState()

	return ctx, nil
}

func (at *AutoTrader) quoteSpreadProvider() decision.QuoteSpreadProvider {
	if at == nil || at.trader == nil {
		return nil
	}
	return func(symbol string) (float64, float64, error) {
		quoteMid := 0.0
		if data, err := market.Get(symbol); err == nil && data != nil {
			quoteMid = data.CurrentPrice
			if quoteMid <= 0 {
				quoteMid = latestCloseFromMarketData(data)
			}
		}
		execMid, err := at.trader.GetMarketPrice(symbol)
		if err != nil {
			return quoteMid, 0, err
		}
		if quoteMid <= 0 {
			quoteMid = execMid
		}
		return quoteMid, execMid, nil
	}
}

func latestCloseFromMarketData(data *market.Data) float64 {
	if data == nil {
		return 0
	}
	for _, timeframe := range []string{"3m", "15m", "1h", "4h"} {
		klines := data.Klines[timeframe]
		if len(klines) > 0 && klines[len(klines)-1].Close > 0 {
			return klines[len(klines)-1].Close
		}
	}
	return 0
}

func (at *AutoTrader) loadRecentDecisionRecords(limit int) []*logger.DecisionRecord {
	if at == nil || at.decisionLogger == nil {
		return nil
	}
	if limit <= 0 {
		limit = 500
	}
	records, err := at.decisionLogger.GetLatestRecords(limit)
	if err != nil {
		log.Printf("⚠️ 读取开仓频率状态日志失败: %v", err)
		return nil
	}
	return records
}

func (at *AutoTrader) buildFrequencyState(records []*logger.DecisionRecord, accountEquity float64) decision.FrequencyState {
	policy := at.config.FrequencyPolicy
	windowHours := policy.RollbackWindowHours
	if windowHours <= 0 {
		windowHours = 24
	}
	since := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	outcomes, _ := logger.BuildTradeOutcomes(records)
	stats := logger.BuildRecentClosedTradeStats(outcomes, since)
	state := decision.FrequencyState{
		OpenCount24h:    logger.CountSuccessfulOpens(records, since, ""),
		ClosedTrades24h: stats.ClosedTrades,
		ProfitFactor24h: stats.ProfitFactor,
	}
	state.LastOpenAt = lastSuccessfulOpenAt(records)
	state.LastCloseAt = lastCloseAt(records)
	state.OpenRejected24h = countOpenRejected(records, since)
	state.SignalCount24h = countSignals24h(records, since)
	if accountEquity > 0 {
		state.Drawdown24hPct = stats.MaxDrawdownUSD / accountEquity * 100
	}

	if policy.Mode == "active" {
		minPF := policy.RollbackMinProfitFactor
		if minPF <= 0 {
			minPF = 0.8
		}
		maxDD := policy.RollbackMaxDrawdownPct
		if maxDD <= 0 {
			maxDD = 2.0
		}
		if stats.ClosedTrades >= 2 && stats.ProfitFactor < minPF {
			state.AutoRollbackActive = true
			state.AutoRollbackReason = fmt.Sprintf("最近%d小时闭合交易PF %.2f < %.2f，运行时回退到safe开仓行为",
				windowHours, stats.ProfitFactor, minPF)
		} else if state.Drawdown24hPct >= maxDD {
			state.AutoRollbackActive = true
			state.AutoRollbackReason = fmt.Sprintf("最近%d小时回撤 %.2f%% >= %.2f%%，运行时回退到safe开仓行为",
				windowHours, state.Drawdown24hPct, maxDD)
		}
	}
	return state
}

func lastSuccessfulOpenAt(records []*logger.DecisionRecord) time.Time {
	var latest time.Time
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, action := range record.Decisions {
			if !isOpenActionName(action.Action) || !action.Success {
				continue
			}
			actionTime := action.Timestamp
			if actionTime.IsZero() {
				actionTime = record.Timestamp
			}
			if latest.IsZero() || actionTime.After(latest) {
				latest = actionTime
			}
		}
	}
	return latest
}

func lastCloseAt(records []*logger.DecisionRecord) time.Time {
	var latest time.Time
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, action := range record.Decisions {
			if !strings.HasPrefix(action.Action, "close_") && action.Action != "partial_close" {
				continue
			}
			actionTime := action.Timestamp
			if actionTime.IsZero() {
				actionTime = record.Timestamp
			}
			if latest.IsZero() || actionTime.After(latest) {
				latest = actionTime
			}
		}
	}
	return latest
}

func countOpenRejected(records []*logger.DecisionRecord, since time.Time) int {
	count := 0
	for _, record := range records {
		if record == nil || (!since.IsZero() && record.Timestamp.Before(since)) {
			continue
		}
		for _, action := range record.Decisions {
			if action.Action == "open_rejected" {
				count++
			}
		}
	}
	return count
}

func countSignals24h(records []*logger.DecisionRecord, since time.Time) int {
	count := 0
	for _, record := range records {
		if record == nil || (!since.IsZero() && record.Timestamp.Before(since)) {
			continue
		}
		if diag, ok := record.StrategyDiagnostics["per_candidate"].([]any); ok {
			count += len(diag)
			continue
		}
		count += len(record.CandidateDetails)
	}
	return count
}

func totalRealizedPnLSince(records []*logger.DecisionRecord, since time.Time) float64 {
	events, _ := logger.BuildTradeEvents(records)
	total := 0.0
	for _, event := range events {
		if !since.IsZero() && event.CloseTime.Before(since) {
			continue
		}
		total += event.PnL
	}
	return total
}

type accountPnLSummary struct {
	TotalPnL    float64
	TotalPnLPct float64
	RealizedPnL float64
	CostBasis   float64
	Source      string
}

func computeAccountPnLSummary(records []*logger.DecisionRecord, totalEquity, totalUnrealizedPnL, configuredInitialBalance float64) accountPnLSummary {
	events, _ := logger.BuildTradeEvents(records)
	realizedPnL := 0.0
	for _, event := range events {
		realizedPnL += event.PnL
	}
	totalPnL := realizedPnL + totalUnrealizedPnL
	if math.Abs(totalPnL) < 1e-8 {
		totalPnL = 0
	}

	costBasis := totalEquity - totalPnL
	source := "trade_logs_plus_unrealized"
	if len(events) == 0 && totalPnL == 0 {
		source = "current_equity_cost_basis_no_trades"
	}
	if costBasis <= 0 {
		if configuredInitialBalance > 0 {
			costBasis = configuredInitialBalance
			source += "_configured_initial_fallback"
		} else if totalEquity > 0 {
			costBasis = totalEquity
			source += "_equity_fallback"
		}
	}

	totalPnLPct := 0.0
	if costBasis > 0 {
		totalPnLPct = totalPnL / costBasis * 100
	}
	if math.Abs(totalPnLPct) < 1e-8 {
		totalPnLPct = 0
	}
	return accountPnLSummary{
		TotalPnL:    totalPnL,
		TotalPnLPct: totalPnLPct,
		RealizedPnL: realizedPnL,
		CostBasis:   costBasis,
		Source:      source,
	}
}

func isOpenActionName(action string) bool {
	return action == "open_long" || action == "open_short" || action == "add_long" || action == "add_short"
}

func (at *AutoTrader) effectiveFrequencyPolicy(state decision.FrequencyState) decision.FrequencyPolicy {
	policy := at.config.FrequencyPolicy
	if policy.Mode == "" {
		policy.Mode = "legacy"
	}
	if policy.EffectiveMode == "" {
		policy.EffectiveMode = policy.Mode
	}
	if policy.AnalysisIntervalMin <= 0 {
		policy.AnalysisIntervalMin = at.config.AnalysisIntervalMin
	}
	if policy.AnalysisIntervalMin <= 0 {
		policy.AnalysisIntervalMin = DefaultAnalysisInterval
	}
	if state.AutoRollbackActive && policy.Mode == "active" {
		policy.EffectiveMode = "safe"
		policy.AnalysisIntervalMin = DefaultAnalysisInterval
	}
	return policy
}

// 🆕 新增：更新持仓快照
func (at *AutoTrader) updatePositionSnapshots(positions []decision.PositionInfo) {
	newSnapshots := make(map[string]*PositionSnapshot)

	for _, pos := range positions {
		posKey := pos.Symbol + "_" + pos.Side
		newSnapshots[posKey] = &PositionSnapshot{
			Symbol:     pos.Symbol,
			Side:       pos.Side,
			Quantity:   pos.Quantity,
			EntryPrice: pos.EntryPrice,
			Leverage:   pos.Leverage,
		}
	}

	at.lastPositions = newSnapshots
}

func (at *AutoTrader) resolvePositionStartTime(symbol, side string, exchangeStartTimes ...int64) int64 {
	if at.positionFirstSeenTime == nil {
		at.positionFirstSeenTime = make(map[string]int64)
	}

	posKey := symbol + "_" + side
	if startTime, exists := at.positionFirstSeenTime[posKey]; exists && startTime > 0 {
		return startTime
	}

	now := time.Now()
	nowMs := now.UnixMilli()
	startTime := nowMs
	source := "current_time"
	hasPlanStart := false
	if plan := decision.GetPlanByScope(at.id, symbol, side); plan != nil && !plan.CreatedAt.IsZero() && plan.CreatedAt.Before(now) {
		startTime = plan.CreatedAt.UnixMilli()
		source = "trade_plan"
		hasPlanStart = true
	}
	if !hasPlanStart {
		if persisted := decision.GetPositionStartTimeScoped(at.id, symbol, side); persisted > 0 && persisted <= nowMs {
			startTime = persisted
			source = "persisted_position_start"
		}
		for _, exchangeStartTime := range exchangeStartTimes {
			if exchangeStartTime <= 0 || exchangeStartTime > nowMs {
				continue
			}
			if startTime == nowMs || exchangeStartTime < startTime {
				startTime = exchangeStartTime
				source = "exchange_position_time"
			}
		}
	}

	at.positionFirstSeenTime[posKey] = startTime
	decision.SetPositionStartTimeScoped(at.id, symbol, side, startTime)
	if source != "trade_plan" {
		log.Printf("📌 持仓开始时间恢复: %s %s source=%s time=%s",
			symbol, side, source, time.UnixMilli(startTime).Format(time.RFC3339))
	}
	return startTime
}

func extractPositionTimestampMillis(pos map[string]interface{}) int64 {
	for _, key := range []string{"openTime", "entryTime", "positionTime", "createTime", "updateTime"} {
		if ts, ok := normalizePositionTimestampMillis(pos[key]); ok {
			return ts
		}
	}
	return 0
}

func normalizePositionTimestampMillis(value interface{}) (int64, bool) {
	var ts int64
	switch v := value.(type) {
	case int64:
		ts = v
	case int:
		ts = int64(v)
	case float64:
		ts = int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		ts = parsed
	case string:
		if v == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			floatParsed, floatErr := strconv.ParseFloat(v, 64)
			if floatErr != nil {
				return 0, false
			}
			parsed = int64(floatParsed)
		}
		ts = parsed
	default:
		return 0, false
	}
	if ts <= 0 {
		return 0, false
	}

	switch {
	case ts > 1_000_000_000_000_000:
		ts = ts / 1_000_000
	case ts > 10_000_000_000_000:
		ts = ts / 1_000
	case ts < 10_000_000_000:
		ts = ts * 1_000
	}
	return ts, true
}

// executeDecisionWithRecord 执行AI决策并记录详细信息
func (at *AutoTrader) executeDecisionWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "add_long":
		return at.executeAddLongWithRecord(decision, actionRecord)
	case "add_short":
		return at.executeAddShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "update_stop_loss":
		return at.executeUpdateStopLossWithRecord(decision, actionRecord)
	case "update_take_profit":
		return at.executeUpdateTakeProfitWithRecord(decision, actionRecord)
	case "partial_close":
		return at.executePartialCloseWithRecord(decision, actionRecord)
	case "hold", "wait":
		// 无需执行，仅记录
		return nil
	default:
		return fmt.Errorf("未知的action: %s", decision.Action)
	}
}

// executeOpenLongWithRecord 执行开多仓并记录详细信息
func (at *AutoTrader) executeOpenLongWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	return at.executeOpenLikeWithRecord(d, actionRecord, "long", "open")
}

// executeOpenShortWithRecord 执行开空仓并记录详细信息
func (at *AutoTrader) executeOpenShortWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	return at.executeOpenLikeWithRecord(d, actionRecord, "short", "open")
}

func (at *AutoTrader) executeAddLongWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	return at.executeOpenLikeWithRecord(d, actionRecord, "long", "add")
}

func (at *AutoTrader) executeAddShortWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	return at.executeOpenLikeWithRecord(d, actionRecord, "short", "add")
}

func (at *AutoTrader) executeOpenLikeWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction, side, intent string) error {
	if intent == "add" {
		log.Printf("  ➕ 加%s仓: %s", sideNameCN(side), d.Symbol)
	} else {
		log.Printf("  %s 开%s仓: %s", sideIcon(side), sideNameCN(side), d.Symbol)
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}
	if intent == "add" {
		if err := at.validateAddExecution(d, side, positions); err != nil {
			return err
		}
	}

	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	if d.PositionSizeUSD <= 0 {
		return fmt.Errorf("仓位大小必须>0")
	}

	quantity := d.PositionSizeUSD / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice
	actionRecord.RiskUSD = d.RiskUSD

	preflight := EvaluateExecutionPreflight(ExecutionPreflightInput{
		Symbol:        d.Symbol,
		Side:          side,
		Quantity:      quantity,
		Price:         marketData.CurrentPrice,
		Leverage:      d.Leverage,
		MinOrderValue: calibratedOpenMinOrderValueUSDT(at.exchange, d.Symbol),
		Positions:     positions,
		Intent:        intent,
	})
	if !preflight.Allowed {
		applyPreflightToActionRecord(preflight, actionRecord)
		return fmt.Errorf("%spreflight失败: %s", openIntentCN(intent), strings.Join(preflight.Reasons, "; "))
	}

	var order map[string]interface{}
	if side == "long" {
		order, err = at.trader.OpenLong(d.Symbol, quantity, d.Leverage)
	} else {
		order, err = at.trader.OpenShort(d.Symbol, quantity, d.Leverage)
	}
	if err != nil {
		return err
	}
	orderID := extractOrderID(order)
	actionRecord.OrderID = orderID
	log.Printf("  ✓ %s成功，订单ID: %v, 数量: %.4f", openIntentCN(intent), orderID, quantity)

	if at.orderTracker != nil {
		at.orderTracker.TrackNewPosition(d.Symbol, side, orderID, marketData.CurrentPrice, quantity, d.Leverage)
	}

	posKey := d.Symbol + "_" + side
	if at.positionFirstSeenTime == nil {
		at.positionFirstSeenTime = make(map[string]int64)
	}
	if _, exists := at.positionFirstSeenTime[posKey]; !exists {
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}

	protectiveQuantity := quantity
	if intent == "add" {
		protectiveQuantity += sameSidePositionQuantity(positions, d.Symbol, side)
		if err := at.resyncProtectiveOrdersBeforeAdd(d.Symbol); err != nil {
			return err
		}
	}
	protective := at.setProtectiveOrdersWithRecord(d, side, protectiveQuantity, actionRecord)
	if protective.stopLossErr != nil {
		if !at.config.EnableEmergencyClose {
			if err := at.persistOpenLikePlan(d, intent, marketData.CurrentPrice, quantity); err != nil {
				return err
			}
		}
		return at.handleUnprotectedOpen(d, side, actionRecord)
	}
	if protective.takeProfitErr != nil {
		log.Printf("  ⚠ 设置止盈失败: %v", protective.takeProfitErr)
	}

	return at.persistOpenLikePlan(d, intent, marketData.CurrentPrice, quantity)
}

func (at *AutoTrader) validateAddExecution(d *decision.Decision, side string, positions []map[string]interface{}) error {
	maxAddCount := at.config.ProgrammaticStrategyPolicy.Position.MaxAddCount
	if maxAddCount > 0 {
		if plan := decision.GetPlanByScope(at.id, d.Symbol, side); plan != nil && plan.AddCount >= maxAddCount {
			return fmt.Errorf("%s %s 加仓次数已达上限(%d)", d.Symbol, side, maxAddCount)
		}
	}
	if sameSidePositionQuantity(positions, d.Symbol, side) <= 0 {
		return fmt.Errorf("%s 没有%s仓位，不能加仓", d.Symbol, side)
	}
	return nil
}

func (at *AutoTrader) resyncProtectiveOrdersBeforeAdd(symbol string) error {
	if err := at.trader.CancelStopLossOrders(symbol); err != nil {
		return fmt.Errorf("加仓前取消旧止损单失败: %w", err)
	}
	if err := at.trader.CancelTakeProfitOrders(symbol); err != nil {
		return fmt.Errorf("加仓前取消旧止盈单失败: %w", err)
	}
	return nil
}

func (at *AutoTrader) persistOpenLikePlan(d *decision.Decision, intent string, price, quantity float64) error {
	if intent == "add" {
		return decision.OnPositionAddedScoped(at.id, d, price, quantity)
	}
	return decision.OnPositionOpenedScoped(at.id, d, price, quantity)
}

func sameSidePositionQuantity(positions []map[string]interface{}, symbol, side string) float64 {
	for _, pos := range positions {
		posSymbol, _ := pos["symbol"].(string)
		posSide, _ := pos["side"].(string)
		if posSymbol != symbol || posSide != side {
			continue
		}
		amount, _ := pos["positionAmt"].(float64)
		return math.Abs(amount)
	}
	return 0
}

func extractOrderID(order map[string]interface{}) int64 {
	value, ok := order["orderId"]
	if !ok || value == nil {
		return 0
	}
	switch id := value.(type) {
	case int64:
		return id
	case int:
		return int64(id)
	case int32:
		return int64(id)
	case float64:
		return int64(id)
	case float32:
		return int64(id)
	case json.Number:
		parsed, err := id.Int64()
		if err == nil {
			return parsed
		}
		parsedFloat, err := id.Float64()
		if err == nil {
			return int64(parsedFloat)
		}
	case string:
		text := strings.TrimSpace(id)
		if text == "" {
			return 0
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			return parsed
		}
		parsedFloat, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return int64(parsedFloat)
		}
	}
	return 0
}

func (at *AutoTrader) enrichCloseFillMetadata(d *decision.Decision, actionRecord *logger.DecisionAction, orderID int64, fallbackQuantity float64, fallbackPrice float64) {
	if at == nil || at.trader == nil || d == nil || actionRecord == nil {
		return
	}
	if actionRecord.StrategyMetadata == nil {
		actionRecord.StrategyMetadata = map[string]any{}
	}
	meta := actionRecord.StrategyMetadata
	meta["reconciled"] = false
	meta["reconciliation_status"] = "estimated_from_decision_log"
	if orderID <= 0 {
		meta["reconciliation_reason"] = "交易所未返回有效订单ID，使用决策日志估算"
		return
	}
	if at.exchange != "aster" {
		meta["reconciliation_status"] = "unsupported"
		meta["reconciliation_reason"] = fmt.Sprintf("%s 暂未启用成交明细对账，使用决策日志估算", at.exchange)
		return
	}

	start := actionRecord.Timestamp.Add(-2 * time.Minute).UnixMilli()
	if actionRecord.Timestamp.IsZero() {
		start = time.Now().Add(-5 * time.Minute).UnixMilli()
	}
	end := time.Now().Add(2 * time.Minute).UnixMilli()
	trades, err := at.trader.GetTradeHistory(d.Symbol, start, end, 50)
	if err == nil {
		fillQty, quoteQty, realizedPnL, commission := aggregateTradesForOrder(trades, orderID)
		if fillQty > 0 {
			avgPrice := fallbackPrice
			if quoteQty > 0 {
				avgPrice = quoteQty / fillQty
			}
			meta["reconciled"] = true
			meta["reconciliation_status"] = "matched"
			meta["filled_quantity"] = fillQty
			meta["avg_fill_price"] = avgPrice
			meta["realized_pnl"] = realizedPnL
			meta["commission"] = commission
			actionRecord.CloseQuantity = fillQty
			actionRecord.Quantity = fillQty
			if avgPrice > 0 {
				actionRecord.Price = avgPrice
			}
			return
		}
	} else {
		meta["reconciliation_reason"] = fmt.Sprintf("查询成交明细失败: %v", err)
	}

	order, orderErr := at.trader.GetOrderStatus(d.Symbol, orderID)
	if orderErr == nil && order != nil {
		meta["order_status"] = order.Status
		if order.ExecutedQty > 0 {
			avgPrice := order.AvgPrice
			if avgPrice <= 0 {
				avgPrice = fallbackPrice
			}
			meta["filled_quantity"] = order.ExecutedQty
			meta["avg_fill_price"] = avgPrice
			meta["realized_pnl"] = order.RealizedPnL
			meta["commission"] = order.Commission
			if strings.EqualFold(order.Status, "FILLED") {
				meta["reconciled"] = true
				meta["reconciliation_status"] = "matched"
			} else {
				meta["reconciliation_status"] = "partial_or_pending"
			}
			actionRecord.CloseQuantity = order.ExecutedQty
			actionRecord.Quantity = order.ExecutedQty
			if avgPrice > 0 {
				actionRecord.Price = avgPrice
			}
			return
		}
		meta["reconciliation_status"] = "pending"
		meta["reconciliation_reason"] = fmt.Sprintf("订单状态为%s，暂未查询到成交数量", order.Status)
		return
	}
	if orderErr != nil && meta["reconciliation_reason"] == nil {
		meta["reconciliation_reason"] = fmt.Sprintf("查询订单状态失败: %v", orderErr)
	}
	meta["reconciliation_status"] = "pending"
	if fallbackQuantity > 0 {
		meta["filled_quantity"] = fallbackQuantity
	}
	if fallbackPrice > 0 {
		meta["avg_fill_price"] = fallbackPrice
	}
}

func aggregateTradesForOrder(trades []TradeRecord, orderID int64) (qty, quoteQty, realizedPnL, commission float64) {
	for _, trade := range trades {
		if trade.OrderID != orderID {
			continue
		}
		qty += trade.Qty
		if trade.QuoteQty > 0 {
			quoteQty += trade.QuoteQty
		} else {
			quoteQty += trade.Qty * trade.Price
		}
		realizedPnL += trade.RealizedPnL
		commission += trade.Commission
	}
	return qty, quoteQty, realizedPnL, commission
}

func markPartialCloseProtectionFailure(actionRecord *logger.DecisionAction, reason string) {
	if actionRecord == nil {
		return
	}
	actionRecord.StopLossSet = boolPtr(false)
	actionRecord.ProtectionError = reason
	actionRecord.ExecutionRisk = "high"
	actionRecord.HighRisk = true
	actionRecord.HighRiskReason = "部分平仓后剩余仓位保护单未能建立"
}

func sideIcon(side string) string {
	if side == "short" {
		return "📉"
	}
	return "📈"
}

func sideNameCN(side string) string {
	if side == "short" {
		return "空"
	}
	return "多"
}

func openIntentCN(intent string) string {
	if intent == "add" {
		return "加仓"
	}
	return "开仓"
}

// executeCloseLongWithRecord 执行平多仓
func (at *AutoTrader) executeCloseLongWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🔄 平多仓: %s", d.Symbol)

	// ✅ 新增：获取平仓前的持仓信息（用于计算盈亏）
	var pnlPercent float64
	var pnlUSD float64
	var holdTimeMinutes float64
	positions, _ := at.trader.GetPositions()
	for _, pos := range positions {
		if pos["symbol"] == d.Symbol && pos["side"] == "long" {
			// 计算盈亏百分比
			entryPrice := pos["entryPrice"].(float64)
			markPrice := pos["markPrice"].(float64)
			leverage := 10
			if lev, ok := pos["leverage"].(float64); ok {
				leverage = int(lev)
			}
			pnlPercent = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100

			// 🆕 直接使用交易所返回的未实现盈亏
			if unrealizedPnl, ok := pos["unRealizedProfit"].(float64); ok {
				pnlUSD = unrealizedPnl
			}
			// 计算持仓时间
			posKey := d.Symbol + "_long"
			if startTime, ok := at.positionFirstSeenTime[posKey]; ok {
				holdTimeMinutes = float64(time.Now().UnixMilli()-startTime) / (1000 * 60)
			}
			break
		}
	}

	// 获取当前价格
	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 平仓
	order, err := at.trader.CloseLong(d.Symbol, 0)
	if err != nil {
		return err
	}

	actionRecord.OrderID = extractOrderID(order)
	// 🆕 停止追踪（手动平仓）
	at.orderTracker.StopTracking(d.Symbol, "long")

	// ✅ 新增：调用平仓回调（更新统计和夏普比率）

	decision.OnPositionClosedScoped(at.id, d.Symbol, "long", marketData.CurrentPrice, pnlPercent, pnlUSD, d.Reasoning)
	at.markPositionLifecycleClosed(d.Symbol, "long", time.Now())

	log.Printf("  ✓ 平仓成功 (盈亏: %.2f%%, 持仓: %.0f分钟)", pnlPercent, holdTimeMinutes)
	return nil
}

// executeCloseShortWithRecord - 同样修改
func (at *AutoTrader) executeCloseShortWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🔄 平空仓: %s", d.Symbol)

	// ✅ 新增：获取平仓前的持仓信息
	var pnlPercent float64
	var pnlUSD float64
	var holdTimeMinutes float64
	positions, _ := at.trader.GetPositions()
	for _, pos := range positions {
		if pos["symbol"] == d.Symbol && pos["side"] == "short" {
			entryPrice := pos["entryPrice"].(float64)
			markPrice := pos["markPrice"].(float64)
			leverage := 10
			if lev, ok := pos["leverage"].(float64); ok {
				leverage = int(lev)
			}
			pnlPercent = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
			// 🆕 直接使用交易所返回的未实现盈亏
			if unrealizedPnl, ok := pos["unRealizedProfit"].(float64); ok {
				pnlUSD = unrealizedPnl
			}

			posKey := d.Symbol + "_short"
			if startTime, ok := at.positionFirstSeenTime[posKey]; ok {
				holdTimeMinutes = float64(time.Now().UnixMilli()-startTime) / (1000 * 60)
			}
			break
		}
	}

	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	order, err := at.trader.CloseShort(d.Symbol, 0)
	if err != nil {
		return err
	}
	// 🆕 停止追踪（手动平仓）
	at.orderTracker.StopTracking(d.Symbol, "short")

	actionRecord.OrderID = extractOrderID(order)

	// ✅ 新增：调用平仓回调
	decision.OnPositionClosedScoped(at.id, d.Symbol, "short", marketData.CurrentPrice, pnlPercent, pnlUSD, d.Reasoning)
	at.markPositionLifecycleClosed(d.Symbol, "short", time.Now())

	log.Printf("  ✓ 平仓成功 (盈亏: %.2f%%, 持仓: %.0f分钟)", pnlPercent, holdTimeMinutes)
	return nil
}

//// executeCloseLongWithRecord 执行平多仓并记录详细信息
//func (at *AutoTrader) executeCloseLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
//	log.Printf("  🔄 平多仓: %s", decision.Symbol)
//
//	// 获取当前价格
//	marketData, err := getMarketData(decision.Symbol)
//	if err != nil {
//		return err
//	}
//	actionRecord.Price = marketData.CurrentPrice
//
//	// 平仓
//	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = 全部平仓
//	if err != nil {
//		return err
//	}
//
//	// 记录订单ID
//	if orderID, ok := order["orderId"].(int64); ok {
//		actionRecord.OrderID = orderID
//	}
//
//	log.Printf("  ✓ 平仓成功")
//	return nil
//}

//// executeCloseShortWithRecord 执行平空仓并记录详细信息
//func (at *AutoTrader) executeCloseShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
//	log.Printf("  🔄 平空仓: %s", decision.Symbol)
//
//	// 获取当前价格
//	marketData, err := getMarketData(decision.Symbol)
//	if err != nil {
//		return err
//	}
//	actionRecord.Price = marketData.CurrentPrice
//
//	// 平仓
//	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = 全部平仓
//	if err != nil {
//		return err
//	}
//
//	// 记录订单ID
//	if orderID, ok := order["orderId"].(int64); ok {
//		actionRecord.OrderID = orderID
//	}
//
//	log.Printf("  ✓ 平仓成功")
//	return nil
//}

// queryHyperliquidTakeProfitOrder 查询 Hyperliquid 的现有止盈单价格
func (at *AutoTrader) queryHyperliquidTakeProfitOrder(symbol, positionSide string, entryPrice float64) float64 {
	hyperliquidTrader, ok := at.trader.(*HyperliquidTrader)
	if !ok {
		return 0.0
	}

	openOrders, err := hyperliquidTrader.exchange.Info().OpenOrders(hyperliquidTrader.ctx, hyperliquidTrader.walletAddr)
	if err != nil {
		log.Printf("  ⚠️ 查询挂单失败，无法恢复原止盈单: %v", err)
		return 0.0
	}

	coin := convertSymbolToHyperliquid(symbol)
	for _, order := range openOrders {
		if order.Coin == coin {
			// 判断是否为止盈单：
			// 空单：买入挂单 + 价格低于成本 = 止盈单
			// 多单：卖出挂单 + 价格高于成本 = 止盈单
			if positionSide == "SHORT" && order.Side == "B" && order.LimitPx < entryPrice {
				log.Printf("  🔍 检测到原有止盈单: %.4f", order.LimitPx)
				return order.LimitPx
			} else if positionSide == "LONG" && order.Side == "A" && order.LimitPx > entryPrice {
				log.Printf("  🔍 检测到原有止盈单: %.4f", order.LimitPx)
				return order.LimitPx
			}
		}
	}

	return 0.0
}

// queryHyperliquidStopLossOrder 查询 Hyperliquid 的现有止损单价格
func (at *AutoTrader) queryHyperliquidStopLossOrder(symbol, positionSide string, entryPrice float64) float64 {
	hyperliquidTrader, ok := at.trader.(*HyperliquidTrader)
	if !ok {
		return 0.0
	}

	openOrders, err := hyperliquidTrader.exchange.Info().OpenOrders(hyperliquidTrader.ctx, hyperliquidTrader.walletAddr)
	if err != nil {
		log.Printf("  ⚠️ 查询挂单失败，无法恢复原止损单: %v", err)
		return 0.0
	}

	coin := convertSymbolToHyperliquid(symbol)
	for _, order := range openOrders {
		if order.Coin == coin {
			// 判断是否为止损单：
			// 空单：买入挂单 + 价格高于成本 = 止损单
			// 多单：卖出挂单 + 价格低于成本 = 止损单
			if positionSide == "SHORT" && order.Side == "B" && order.LimitPx > entryPrice {
				log.Printf("  🔍 检测到原有止损单: %.4f", order.LimitPx)
				return order.LimitPx
			} else if positionSide == "LONG" && order.Side == "A" && order.LimitPx < entryPrice {
				log.Printf("  🔍 检测到原有止损单: %.4f", order.LimitPx)
				return order.LimitPx
			}
		}
	}

	return 0.0
}

// checkDualSidePosition 检查是否存在双向持仓（防御性检查）
func (at *AutoTrader) checkDualSidePosition(symbol, positionSide string, positions []map[string]interface{}) {
	for _, pos := range positions {
		posSymbol, ok := pos["symbol"].(string)
		if !ok {
			continue
		}
		posSide, ok := pos["side"].(string)
		if !ok {
			continue
		}
		posAmt, ok := pos["positionAmt"].(float64)
		if !ok {
			continue
		}
		if posSymbol == symbol && posAmt != 0 && strings.ToUpper(posSide) != positionSide {
			oppositeSide := strings.ToUpper(posSide)
			log.Printf("  🚨 警告：检测到 %s 存在双向持仓（%s + %s），这违反了策略规则",
				symbol, positionSide, oppositeSide)
			log.Printf("  🚨 取消订单将影响两个方向的持仓，请检查是否为用户手动操作导致")
			log.Printf("  🚨 建议：手动平掉其中一个方向的持仓，或检查系统是否有BUG")
			return
		}
	}
}

// restoreTakeProfitOrder 恢复止盈单（P0修复：防止TP单丢失）
func (at *AutoTrader) restoreTakeProfitOrder(symbol, positionSide string, quantity, price float64) {
	if price <= 0 {
		if _, ok := at.trader.(*HyperliquidTrader); ok {
			log.Printf("  ⚠️ 警告：调整止损后未找到原止盈单")
			log.Printf("  → 可能情况：1) 原本就没有止盈单 2) 止盈单已触发 3) 查询失败")
		}
		return
	}

	log.Printf("  🔄 重新设置原止盈单: %.4f", price)
	if err := at.trader.SetTakeProfit(symbol, positionSide, quantity, price); err != nil {
		log.Printf("  ⚠ 重新设置止盈单失败: %v", err)
		log.Printf("  🚨🚨🚨 严重警告：止盈单恢复失败，当前持仓无止盈保护！")
	} else {
		log.Printf("  ✅ 止盈单已恢复")
	}
}

// restoreStopLossOrder 恢复止损单（P0修复：防止SL单丢失）
func (at *AutoTrader) restoreStopLossOrder(symbol, positionSide string, quantity, price float64) {
	if price <= 0 {
		if _, ok := at.trader.(*HyperliquidTrader); ok {
			log.Printf("  ⚠️ 警告：调整止盈后未找到原止损单")
			log.Printf("  → 可能情况：1) 原本就没有止损单 2) 止损单已触发 3) 查询失败")
		}
		return
	}

	log.Printf("  🔄 重新设置原止损单: %.4f", price)
	if err := at.trader.SetStopLoss(symbol, positionSide, quantity, price); err != nil {
		log.Printf("  ⚠ 重新设置止损单失败: %v", err)
		log.Printf("  🚨🚨🚨 严重警告：止损单恢复失败，当前持仓无止损保护！")
	} else {
		log.Printf("  ✅ 止损单已恢复")
	}
}

// executeUpdateStopLossWithRecord 执行调整止损并记录详细信息
func (at *AutoTrader) executeUpdateStopLossWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🎯 调整止损: %s → %.2f", d.Symbol, d.NewStopLoss)

	// 获取当前价格
	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, ok := pos["symbol"].(string)
		if !ok {
			continue
		}
		posAmt, ok := pos["positionAmt"].(float64)
		if !ok {
			continue
		}
		if symbol == d.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", d.Symbol)
	}

	// 获取持仓方向和数量
	side, ok := targetPosition["side"].(string)
	if !ok || side == "" {
		return fmt.Errorf("failed to parse position side")
	}
	positionSide := strings.ToUpper(side)

	positionAmt, ok := targetPosition["positionAmt"].(float64)
	if !ok {
		return fmt.Errorf("failed to parse position amount")
	}

	// 验证新止损价格合理性
	if positionSide == "LONG" && d.NewStopLoss >= marketData.CurrentPrice {
		return fmt.Errorf("多单止损必须低于当前价格 (当前: %.2f, 新止损: %.2f)", marketData.CurrentPrice, d.NewStopLoss)
	}
	if positionSide == "SHORT" && d.NewStopLoss <= marketData.CurrentPrice {
		return fmt.Errorf("空单止损必须高于当前价格 (当前: %.2f, 新止损: %.2f)", marketData.CurrentPrice, d.NewStopLoss)
	}

	// ⚠️ 防御性检查：检测是否存在双向持仓
	at.checkDualSidePosition(d.Symbol, positionSide, positions)

	// ============ P1 修复：保本价硬约束（防止过早移动止损） ============
	entryPrice := targetPosition["entryPrice"].(float64)

	// 🔍 Step 1: 计算当前利润百分比（基于价格变化）
	var profitPercent float64
	if positionSide == "LONG" {
		profitPercent = (marketData.CurrentPrice - entryPrice) / entryPrice * 100
	} else { // SHORT
		profitPercent = (entryPrice - marketData.CurrentPrice) / entryPrice * 100
	}

	// 🔍 Step 2: 判断新止损价是否接近保本价（±0.5%）
	distanceToEntry := math.Abs(d.NewStopLoss-entryPrice) / entryPrice
	isBreakevenStopLoss := distanceToEntry < 0.005 // 0.5% threshold

	// 🔍 Step 3: 如果利润不足最低要求且尝试设置保本价，拒绝执行
	if profitPercent < breakevenMoveMinPriceProfitPct && isBreakevenStopLoss {
		log.Printf("  🚫 拒绝调整止损：当前利润仅 %.2f%%，未达到 %.1f%% 最低要求",
			profitPercent, breakevenMoveMinPriceProfitPct)
		log.Printf("  📊 入场价: %.4f | 当前价: %.4f | 尝试设置止损: %.4f (距离入场价 %.2f%%)",
			entryPrice, marketData.CurrentPrice, d.NewStopLoss, distanceToEntry*100)
		log.Printf("  💡 建议：等待利润达到 %.1f%% 以上后再移动止损至保本价", breakevenMoveMinPriceProfitPct)
		return fmt.Errorf("利润不足 %.1f%% (当前 %.2f%%)，不允许移动止损至保本价",
			breakevenMoveMinPriceProfitPct, profitPercent)
	}

	// 📊 记录当前利润状态（通过检查时）
	if isBreakevenStopLoss {
		log.Printf("  ✅ 保本价检查通过：当前利润 %.2f%% ≥ %.1f%%，允许移动止损至保本价",
			profitPercent, breakevenMoveMinPriceProfitPct)
	} else {
		log.Printf("  📊 当前利润: %.2f%% | 入场价: %.4f | 新止损: %.4f (距离入场价 %.2f%%)",
			profitPercent, entryPrice, d.NewStopLoss, distanceToEntry*100)
	}
	// ===================================================

	// ============ P0 修复：记录并恢复止盈单 ============
	// 🔍 Step 1: 查询现有止盈单价格
	oldTakeProfitPrice := at.queryHyperliquidTakeProfitOrder(d.Symbol, positionSide, entryPrice)
	// ===================================================

	// 🔄 Step 2: 取消旧的止损单（Hyperliquid 会连止盈单一起删）
	// 注意：如果存在双向持仓，这会删除两个方向的止损单
	if err := at.trader.CancelStopLossOrders(d.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧止损单失败: %v", err)
		// 不中断执行，继续设置新止损
	}

	// ✅ Step 3: 调用交易所 API 修改止损
	quantity := math.Abs(positionAmt)
	err = at.trader.SetStopLoss(d.Symbol, positionSide, quantity, d.NewStopLoss)
	if err != nil {
		return fmt.Errorf("修改止损失败: %w", err)
	}

	// ✅ Step 4: 恢复原有止盈单（防止裸奔）
	at.restoreTakeProfitOrder(d.Symbol, positionSide, quantity, oldTakeProfitPrice)

	decision.OnStopLossUpdatedScoped(at.id, d.Symbol, "", d.NewStopLoss)
	log.Printf("  ✓ 止损已调整: %.2f (当前价格: %.2f)", d.NewStopLoss, marketData.CurrentPrice)
	return nil
}

// executeUpdateTakeProfitWithRecord 执行调整止盈并记录详细信息
func (at *AutoTrader) executeUpdateTakeProfitWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🎯 调整止盈: %s → %.2f", d.Symbol, d.NewTakeProfit)

	// 获取当前价格
	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, ok := pos["symbol"].(string)
		if !ok {
			continue
		}
		posAmt, ok := pos["positionAmt"].(float64)
		if !ok {
			continue
		}
		if symbol == d.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", d.Symbol)
	}

	// 获取持仓方向和数量
	side, ok := targetPosition["side"].(string)
	if !ok || side == "" {
		return fmt.Errorf("failed to parse position side")
	}
	positionSide := strings.ToUpper(side)

	positionAmt, ok := targetPosition["positionAmt"].(float64)
	if !ok {
		return fmt.Errorf("failed to parse position amount")
	}

	// 验证新止盈价格合理性
	if positionSide == "LONG" && d.NewTakeProfit <= marketData.CurrentPrice {
		return fmt.Errorf("多单止盈必须高于当前价格 (当前: %.2f, 新止盈: %.2f)", marketData.CurrentPrice, d.NewTakeProfit)
	}
	if positionSide == "SHORT" && d.NewTakeProfit >= marketData.CurrentPrice {
		return fmt.Errorf("空单止盈必须低于当前价格 (当前: %.2f, 新止盈: %.2f)", marketData.CurrentPrice, d.NewTakeProfit)
	}

	// ⚠️ 防御性检查：检测是否存在双向持仓
	at.checkDualSidePosition(d.Symbol, positionSide, positions)

	// ============ P0 修复：记录并恢复止损单 ============
	// 🔍 Step 1: 查询现有止损单价格
	entryPrice := targetPosition["entryPrice"].(float64)
	oldStopLossPrice := at.queryHyperliquidStopLossOrder(d.Symbol, positionSide, entryPrice)
	// ===================================================

	// 🔄 Step 2: 取消旧的止盈单（Hyperliquid 会连止损单一起删）
	// 注意：如果存在双向持仓，这会删除两个方向的止盈单
	if err := at.trader.CancelTakeProfitOrders(d.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧止盈单失败: %v", err)
		// 不中断执行，继续设置新止盈
	}

	// ✅ Step 3: 调用交易所 API 修改止盈
	quantity := math.Abs(positionAmt)
	err = at.trader.SetTakeProfit(d.Symbol, positionSide, quantity, d.NewTakeProfit)
	if err != nil {
		return fmt.Errorf("修改止盈失败: %w", err)
	}

	// ✅ Step 4: 恢复原有止损单（防止裸奔）
	at.restoreStopLossOrder(d.Symbol, positionSide, quantity, oldStopLossPrice)

	decision.OnTakeProfitUpdatedScoped(at.id, d.Symbol, "", d.NewTakeProfit)
	log.Printf("  ✓ 止盈已调整: %.2f (当前价格: %.2f)", d.NewTakeProfit, marketData.CurrentPrice)
	return nil
}

const (
	minPartialCloseOrderValueUSDT  = 5.0
	minRemainingPositionValueUSDT  = 10.0
	smallPositionFullCloseUSDT     = 25.0
	breakevenMoveMinPriceProfitPct = 1.0
)

type partialCloseMode string

const (
	partialCloseModeNormal partialCloseMode = "normal"
	partialCloseModeFull   partialCloseMode = "full"
	partialCloseModeSkip   partialCloseMode = "skip"
)

type partialClosePlan struct {
	Mode                 partialCloseMode
	CurrentPositionValue float64
	CloseQuantity        float64
	CloseValue           float64
	RemainingQuantity    float64
	RemainingValue       float64
}

func effectivePlanStopLoss(traderID, symbol, side string) float64 {
	plan := decision.GetPlanByScope(traderID, symbol, side)
	if plan == nil {
		return 0
	}
	if plan.CurrentStopLoss > 0 {
		return plan.CurrentStopLoss
	}
	return plan.StopLoss
}

func resolveProtectiveStopLoss(requestedStopLoss, fallbackStopLoss float64) float64 {
	if requestedStopLoss > 0 {
		return requestedStopLoss
	}
	return fallbackStopLoss
}

func determinePartialClosePlan(totalQuantity, closePercentage, markPrice, minPartialCloseValue, minRemainingValue float64) partialClosePlan {
	if minPartialCloseValue <= 0 {
		minPartialCloseValue = minPartialCloseOrderValueUSDT
	}
	if minRemainingValue <= 0 {
		minRemainingValue = minRemainingPositionValueUSDT
	}
	closeQuantity := totalQuantity * (closePercentage / 100.0)
	remainingQuantity := totalQuantity - closeQuantity
	if remainingQuantity < 0 {
		remainingQuantity = 0
	}

	plan := partialClosePlan{
		Mode:                 partialCloseModeNormal,
		CurrentPositionValue: totalQuantity * markPrice,
		CloseQuantity:        closeQuantity,
		CloseValue:           closeQuantity * markPrice,
		RemainingQuantity:    remainingQuantity,
		RemainingValue:       remainingQuantity * markPrice,
	}

	if plan.RemainingValue > 0 && plan.RemainingValue <= minRemainingValue {
		plan.Mode = partialCloseModeFull
		plan.CloseQuantity = totalQuantity
		plan.CloseValue = plan.CurrentPositionValue
		plan.RemainingQuantity = 0
		plan.RemainingValue = 0
		return plan
	}

	if plan.CloseValue > 0 && plan.CloseValue < minPartialCloseValue {
		if plan.CurrentPositionValue <= smallPositionFullCloseUSDT && math.Abs(closePercentage-20.0) < 0.0001 {
			plan.Mode = partialCloseModeFull
			plan.CloseQuantity = totalQuantity
			plan.CloseValue = plan.CurrentPositionValue
			plan.RemainingQuantity = 0
			plan.RemainingValue = 0
			return plan
		}
		plan.Mode = partialCloseModeSkip
	}

	return plan
}

// executePartialCloseWithRecord 执行部分平仓并记录详细信息
func (at *AutoTrader) executePartialCloseWithRecord(d *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  📊 部分平仓: %s %.1f%%", d.Symbol, d.ClosePercentage)
	actionRecord.FinalAction = "partial_close"
	actionRecord.RequestedClosePercentage = d.ClosePercentage

	// 验证百分比范围
	if d.ClosePercentage <= 0 || d.ClosePercentage > 100 {
		return fmt.Errorf("平仓百分比必须在 0-100 之间，当前: %.1f", d.ClosePercentage)
	}

	// 获取当前价格
	marketData, err := getMarketData(d.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, ok := pos["symbol"].(string)
		if !ok {
			continue
		}
		posAmt, ok := pos["positionAmt"].(float64)
		if !ok {
			continue
		}
		if symbol == d.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", d.Symbol)
	}

	// 获取持仓方向和数量
	side, ok := targetPosition["side"].(string)
	if !ok || side == "" {
		return fmt.Errorf("failed to parse position side")
	}
	positionSide := strings.ToUpper(side)

	positionAmt, ok := targetPosition["positionAmt"].(float64)
	if !ok {
		return fmt.Errorf("failed to parse position amount")
	}
	fallbackStopLoss := effectivePlanStopLoss(at.id, d.Symbol, side)

	// 计算平仓数量
	totalQuantity := math.Abs(positionAmt)
	closeQuantity := totalQuantity * (d.ClosePercentage / 100.0)
	actionRecord.Quantity = closeQuantity
	actionRecord.CloseQuantity = closeQuantity
	if actionRecord.StrategyMetadata == nil {
		actionRecord.StrategyMetadata = map[string]any{}
	}
	actionRecord.StrategyMetadata["position_quantity_before"] = totalQuantity
	actionRecord.StrategyMetadata["side"] = side

	// ✅ Layer 2: 最小仓位检查（防止产生小额剩余）
	markPrice, ok := targetPosition["markPrice"].(float64)
	if !ok || markPrice <= 0 {
		return fmt.Errorf("failed to parse mark price, cannot perform minimum position check")
	}

	minPartialCloseValue := calibratedPartialCloseMinValueUSDT(at.exchange, d.Symbol)
	minRemainingValue := maxFloat(minRemainingPositionValueUSDT, minPartialCloseValue)
	plan := determinePartialClosePlan(totalQuantity, d.ClosePercentage, markPrice, minPartialCloseValue, minRemainingValue)
	closeQuantity = plan.CloseQuantity
	remainingQuantity := plan.RemainingQuantity
	actionRecord.Quantity = closeQuantity
	actionRecord.CloseQuantity = closeQuantity

	if plan.Mode == partialCloseModeFull {
		log.Printf("⚠️ 检测到 partial_close 后剩余仓位 %.2f USDT < %.0f USDT",
			plan.RemainingValue, minRemainingValue)
		log.Printf("  → 当前仓位价值: %.2f USDT, 平仓 %.1f%%, 剩余: %.2f USDT",
			plan.CurrentPositionValue, d.ClosePercentage, plan.RemainingValue)
		log.Printf("  → 自动修正为全部平仓，避免产生无法平仓的小额剩余")

		// 🔄 自动修正为全部平仓
		if positionSide == "LONG" {
			d.Action = "close_long"
			actionRecord.FinalAction = "close_long"
			actionRecord.ExecutedClosePercentage = 100
			actionRecord.CloseQuantity = totalQuantity
			actionRecord.Quantity = totalQuantity
			log.Printf("  ✓ 已修正为: close_long")
			return at.executeCloseLongWithRecord(d, actionRecord)
		} else {
			d.Action = "close_short"
			actionRecord.FinalAction = "close_short"
			actionRecord.ExecutedClosePercentage = 100
			actionRecord.CloseQuantity = totalQuantity
			actionRecord.Quantity = totalQuantity
			log.Printf("  ✓ 已修正为: close_short")
			return at.executeCloseShortWithRecord(d, actionRecord)
		}
	}

	if plan.Mode == partialCloseModeSkip {
		log.Printf("⚠️ 跳过 partial_close: 本次平仓名义额 %.2f USDT < %.2f USDT",
			plan.CloseValue, minPartialCloseValue)
		log.Printf("  → 当前仓位价值: %.2f USDT, 平仓 %.1f%%, 预计剩余: %.2f USDT",
			plan.CurrentPositionValue, d.ClosePercentage, plan.RemainingValue)
		actionRecord.Quantity = 0
		actionRecord.CloseQuantity = 0
		actionRecord.ExecutedClosePercentage = 0
		actionRecord.FinalAction = "partial_close_skipped"
		actionRecord.Reasoning = fmt.Sprintf("跳过小额部分平仓: 名义额 %.2f USDT < %.2f USDT",
			plan.CloseValue, minPartialCloseValue)

		stopLossForPlan := 0.0
		if d.NewStopLoss > 0 {
			if err := at.trader.SetStopLoss(d.Symbol, positionSide, totalQuantity, d.NewStopLoss); err != nil {
				log.Printf("  ⚠️ 小额部分平仓跳过后设置保护止损失败: %v", err)
			} else {
				stopLossForPlan = d.NewStopLoss
				log.Printf("  ✓ 小额部分平仓跳过后已更新保护止损: %.4f", d.NewStopLoss)
			}
		}

		decision.OnPartialCloseScoped(at.id, d.Symbol, side, d.TrancheIndex, 0, stopLossForPlan)
		return nil
	}

	// 执行平仓
	var order map[string]interface{}
	if positionSide == "LONG" {
		order, err = at.trader.CloseLong(d.Symbol, closeQuantity)
	} else {
		order, err = at.trader.CloseShort(d.Symbol, closeQuantity)
	}

	if err != nil {
		return fmt.Errorf("部分平仓失败: %w", err)
	}

	actionRecord.OrderID = extractOrderID(order)

	log.Printf("  ✓ 部分平仓成功: 平仓 %.4f (%.1f%%), 剩余 %.4f",
		closeQuantity, d.ClosePercentage, remainingQuantity)
	actionRecord.FinalAction = "partial_close"
	actionRecord.CloseQuantity = closeQuantity
	if totalQuantity > 0 {
		actionRecord.ExecutedClosePercentage = closeQuantity / totalQuantity * 100
	}
	at.enrichCloseFillMetadata(d, actionRecord, actionRecord.OrderID, closeQuantity, actionRecord.Price)

	// 🔧 FIX: 部分平仓后重新设置止盈止损（基于剩余数量）
	// 币安会自动取消原来的止盈止损订单（因为数量不匹配），所以必须重新设置。
	protectiveStopLoss := resolveProtectiveStopLoss(d.NewStopLoss, fallbackStopLoss)
	if remainingQuantity > 0 {
		log.Printf("  🎯 更新剩余仓位的止盈止损...")

		if protectiveStopLoss <= 0 {
			markPartialCloseProtectionFailure(actionRecord, "部分平仓已执行，但无法确定剩余仓位保护止损")
			decision.OnPartialCloseScoped(at.id, d.Symbol, side, d.TrancheIndex, d.ClosePercentage, 0)
			return nil
		}

		if err := at.trader.SetStopLoss(d.Symbol, positionSide, remainingQuantity, protectiveStopLoss); err != nil {
			markPartialCloseProtectionFailure(actionRecord, fmt.Sprintf("部分平仓已执行，但设置剩余仓位保护止损失败: %v", err))
			decision.OnPartialCloseScoped(at.id, d.Symbol, side, d.TrancheIndex, d.ClosePercentage, 0)
			return nil
		}
		log.Printf("  ✓ 已设置保护止损: %.4f (数量: %.4f)", protectiveStopLoss, remainingQuantity)

		// 设置新止盈（基于剩余数量）
		if d.NewTakeProfit > 0 {
			if err := at.trader.SetTakeProfit(d.Symbol, positionSide, remainingQuantity, d.NewTakeProfit); err != nil {
				log.Printf("  ⚠️ 设置新止盈失败: %v", err)
			} else {
				log.Printf("  ✓ 已设置新止盈: %.4f (数量: %.4f)", d.NewTakeProfit, remainingQuantity)
			}
		}
	}

	decision.OnPartialCloseScoped(at.id, d.Symbol, side, d.TrancheIndex, d.ClosePercentage, protectiveStopLoss)
	return nil
}

// GetID 获取trader ID
func (at *AutoTrader) GetID() string {
	return at.id
}

// GetName 获取trader名称
func (at *AutoTrader) GetName() string {
	return at.name
}

// GetAIModel 获取AI模型
func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

// GetDecisionMode 获取决策模式。
func (at *AutoTrader) GetDecisionMode() string {
	if at.config.DecisionMode == "" {
		return "ai"
	}
	return at.config.DecisionMode
}

// GetDecisionLogger 获取决策日志记录器
func (at *AutoTrader) GetDecisionLogger() *logger.DecisionLogger {
	return at.decisionLogger
}

// GetStrategySymbols 返回程序化策略最近一次解析出的分析标的池。
func (at *AutoTrader) GetStrategySymbols() []chanlun.StrategySymbol {
	if at.programmaticEngine != nil {
		return at.programmaticEngine.SymbolUniverse(at.id)
	}
	if at.chanlunV2Engine != nil {
		return at.chanlunV2Engine.SymbolUniverse(at.id)
	}
	return nil
}

// GetLatestStrategySignals 返回程序化策略最近一次分析出的 symbol 信号报告。
func (at *AutoTrader) GetLatestStrategySignals(symbol string) (*chanlun.SignalReport, bool) {
	return at.GetLatestStrategySignalsWithOptions(symbol, chanlun.SignalReportOptions{})
}

// GetLatestStrategySignalsWithOptions 返回带过滤/视图参数的程序化策略信号报告。
func (at *AutoTrader) GetLatestStrategySignalsWithOptions(symbol string, opts chanlun.SignalReportOptions) (*chanlun.SignalReport, bool) {
	normalized := market.Normalize(symbol)
	if at.programmaticEngine != nil {
		if report, ok := at.programmaticEngine.LatestSignalsWithOptions(at.id, normalized, opts); ok {
			return report, true
		}
		return at.programmaticEngine.EmptySignalReportWithOptions(at.id, normalized, opts), true
	}
	if at.chanlunV2Engine != nil {
		if report, ok := at.chanlunV2Engine.LatestSignalsWithOptions(at.id, normalized, opts); ok {
			return report, true
		}
		return at.chanlunV2Engine.EmptySignalReportWithOptions(at.id, normalized, opts), true
	}
	return nil, false
}

// GetMarketKlines 返回闭合 K 线，供策略检查区使用。
func (at *AutoTrader) GetMarketKlines(symbol, timeframe string, limit int) ([]market.Kline, error) {
	return marketKlineFetcher(symbol, timeframe, limit, true)
}

func (at *AutoTrader) ResolveMarketKlineLimit(timeframe string, explicitLimit int) MarketKlineLimitResolution {
	if explicitLimit > 0 {
		source := "query"
		limit := explicitLimit
		if limit > MaxMarketKlineLimit {
			limit = MaxMarketKlineLimit
			source = "query_capped"
		}
		return MarketKlineLimitResolution{Limit: limit, ConfiguredLimit: limit, LimitSource: source}
	}

	if at != nil && at.config.DecisionMode == "programmatic" {
		if configured, ok := at.programmaticHistoryDepthForTimeframe(timeframe); ok && configured > 0 {
			source := "programmatic_history_depth"
			limit := configured
			if limit > MaxMarketKlineLimit {
				limit = MaxMarketKlineLimit
				source = "programmatic_history_depth_capped"
			}
			return MarketKlineLimitResolution{Limit: limit, ConfiguredLimit: configured, LimitSource: source}
		}
	}
	if at != nil && at.config.DecisionMode == "chanlun_v2" {
		if configured, ok := at.chanlunV2HistoryDepthForTimeframe(timeframe); ok && configured > 0 {
			source := "chanlun_v2_history_depth"
			limit := configured
			if limit > MaxMarketKlineLimit {
				limit = MaxMarketKlineLimit
				source = "chanlun_v2_history_depth_capped"
			}
			return MarketKlineLimitResolution{Limit: limit, ConfiguredLimit: configured, LimitSource: source}
		}
	}

	return MarketKlineLimitResolution{
		Limit:           DefaultMarketKlineLimit,
		ConfiguredLimit: 0,
		LimitSource:     "default",
	}
}

func (at *AutoTrader) programmaticHistoryDepthForTimeframe(timeframe string) (int, bool) {
	if at == nil {
		return 0, false
	}
	depth := at.config.ProgrammaticStrategyPolicy.HistoryDepth
	switch strings.ToLower(strings.TrimSpace(timeframe)) {
	case "3m":
		return depth.M3, depth.M3 > 0
	case "15m":
		return depth.M15, depth.M15 > 0
	case "1h":
		return depth.H1, depth.H1 > 0
	case "4h":
		return depth.H4, depth.H4 > 0
	default:
		return 0, false
	}
}

func (at *AutoTrader) chanlunV2HistoryDepthForTimeframe(timeframe string) (int, bool) {
	if at == nil {
		return 0, false
	}
	for key, depth := range at.config.ChanlunV2StrategyConfig.HistoryDepth {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(timeframe)) && depth > 0 {
			return depth, true
		}
	}
	return 0, false
}

// GetStatus 获取系统状态（用于API）
func (at *AutoTrader) GetStatus() map[string]interface{} {
	aiProvider := "DeepSeek"
	if at.config.DecisionMode == "programmatic" {
		aiProvider = "Programmatic"
	} else if at.config.DecisionMode == "chanlun_v2" {
		aiProvider = "Chanlun V2"
	} else if at.config.UseQwen {
		aiProvider = "Qwen"
	}
	frequencyState := at.buildFrequencyState(at.loadRecentDecisionRecords(500), 0)
	frequencyPolicy := at.effectiveFrequencyPolicy(frequencyState)

	return map[string]interface{}{
		"trader_id":        at.id,
		"trader_name":      at.name,
		"ai_model":         at.aiModel,
		"decision_mode":    at.GetDecisionMode(),
		"exchange":         at.exchange,
		"is_running":       at.isRunning,
		"start_time":       at.startTime.Format(time.RFC3339),
		"runtime_minutes":  int(time.Since(at.startTime).Minutes()),
		"call_count":       at.callCount,
		"initial_balance":  at.initialBalance,
		"scan_interval":    at.config.ScanInterval.String(),
		"stop_until":       at.stopUntil.Format(time.RFC3339),
		"last_reset_time":  at.lastResetTime.Format(time.RFC3339),
		"ai_provider":      aiProvider,
		"frequency_policy": frequencyPolicy,
		"frequency_state":  frequencyState,
		"strategy_risk_policy": map[string]interface{}{
			"legacy":                     at.config.StrategyRiskPolicy.Legacy,
			"enabled":                    at.config.StrategyRiskPolicy.Enabled,
			"rollback_legacy_validation": at.config.StrategyRiskPolicy.RollbackLegacyValidation,
			"adx_timeframe":              at.config.StrategyRiskPolicy.ADXTimeframe,
			"default_min_net_rr":         at.config.StrategyRiskPolicy.DefaultMinNetRR,
			"fee_slippage_pct":           at.config.StrategyRiskPolicy.FeeSlippagePct,
			"profile_count":              len(at.config.StrategyRiskPolicy.Profiles),
			"profiles":                   strategyRiskProfileNames(at.config.StrategyRiskPolicy.Profiles),
			"profile_defaults":           strategyRiskProfileSummaries(at.config.StrategyRiskPolicy.Profiles),
		},
	}
}

func strategyRiskProfileNames(profiles []decision.InstrumentProfile) []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Name != "" {
			names = append(names, profile.Name)
		}
	}
	return names
}

func strategyRiskProfileSummaries(profiles []decision.InstrumentProfile) []map[string]interface{} {
	summaries := make([]map[string]interface{}, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Name == "" {
			continue
		}
		summaries = append(summaries, map[string]interface{}{
			"name":                    profile.Name,
			"match_type":              profile.MatchType,
			"min_stop_pct":            profile.MinStopPct,
			"fallback_stop_pct":       profile.FallbackStopPct,
			"atr_multiplier":          profile.ATRMultiplier,
			"atr_timeframe":           profile.ATRTimeframe,
			"min_net_rr":              profile.MinNetRR,
			"max_risk_pct":            profile.MaxRiskPct,
			"min_adx":                 profile.MinADX,
			"allow_long":              profile.AllowLong,
			"allow_short":             profile.AllowShort,
			"max_same_side_high_corr": profile.MaxSameSideHighCorr,
			"exchange_full_tp_mode":   profile.ExchangeFullTPMode,
			"exchange_full_tp_min_rr": profile.ExchangeFullTPMinRR,
		})
	}
	return summaries
}

// GetAccountInfo 获取账户信息（用于API）
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("获取余额失败: %w", err)
	}

	// 获取账户字段
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = 钱包余额 + 未实现盈亏
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// 获取持仓计算总保证金
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	totalMarginUsed := 0.0
	totalUnrealizedPnL := 0.0
	for _, pos := range positions {
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnL += unrealizedPnl

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	records := at.loadRecentDecisionRecords(10000)
	accountPnL := computeAccountPnLSummary(records, totalEquity, totalUnrealizedPnL, at.initialBalance)

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	return map[string]interface{}{
		// 核心字段
		"total_equity":      totalEquity,           // 账户净值 = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // 钱包余额（不含未实现盈亏）
		"unrealized_profit": totalUnrealizedProfit, // 未实现盈亏（从API）
		"available_balance": availableBalance,      // 可用余额

		// 盈亏统计
		"total_pnl":            accountPnL.TotalPnL,    // 交易盈亏 = 已实现 + 未实现，不把投入本金算作利润
		"total_pnl_pct":        accountPnL.TotalPnLPct, // 相对成本基准的交易收益率
		"cost_basis":           accountPnL.CostBasis,   // 成本基准 = 当前净值 - 交易盈亏
		"realized_pnl":         accountPnL.RealizedPnL, // 决策日志可对账的已实现盈亏
		"pnl_source":           accountPnL.Source,
		"total_unrealized_pnl": totalUnrealizedPnL, // 未实现盈亏（从持仓计算）
		"initial_balance":      at.initialBalance,  // 初始余额
		"daily_pnl":            at.dailyPnL,        // 日盈亏

		// 持仓信息
		"position_count":  len(positions),  // 持仓数量
		"margin_used":     totalMarginUsed, // 保证金占用
		"margin_used_pct": marginUsedPct,   // 保证金使用率
	}, nil
}

// GetPositions 获取持仓列表（用于API）
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		pnlPct := 0.0
		if side == "long" {
			pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		marginUsed := (quantity * markPrice) / float64(leverage)

		stopLossPrice := 0.0
		takeProfitPrice := 0.0
		if plan := decision.GetPlanByScope(at.id, symbol, side); plan != nil && (plan.Status == "" || strings.EqualFold(plan.Status, "ACTIVE")) {
			stopLossPrice = plan.CurrentStopLoss
			if stopLossPrice <= 0 {
				stopLossPrice = plan.StopLoss
			}
			takeProfitPrice = plan.TakeProfit
		}

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"stop_loss_price":    stopLossPrice,
			"take_profit_price":  takeProfitPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		})
	}

	return result, nil
}

// sortDecisionsByPriority 对决策排序：先平仓，再开仓，最后hold/wait
// 这样可以避免换仓时仓位叠加超限
func sortDecisionsByPriority(decisions []decision.Decision) []decision.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// 定义优先级
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short", "partial_close":
			return 1 // 最高优先级：先平仓（包括部分平仓）
		case "update_stop_loss", "update_take_profit":
			return 2 // 调整持仓止盈止损
		case "add_long", "add_short":
			return 3 // 加仓低于风险降低动作，高于普通新开仓
		case "open_long", "open_short":
			return 4 // 次优先级：后开仓
		case "hold", "wait":
			return 5 // 最低优先级：观望
		default:
			return 999 // 未知动作放最后
		}
	}

	// 复制决策列表
	sorted := make([]decision.Decision, len(decisions))
	copy(sorted, decisions)

	// 按优先级排序
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// detectAutoClosedPositions 检测自动平仓的持仓（止损/止盈触发）
func (at *AutoTrader) detectAutoClosedPositions(currentPositions []decision.PositionInfo) []logger.DecisionAction {
	var autoClosedActions []logger.DecisionAction
	now := time.Now()

	// 创建当前持仓的map便于查找
	currentPosMap := make(map[string]bool)
	for _, pos := range currentPositions {
		posKey := pos.Symbol + "_" + pos.Side
		currentPosMap[posKey] = true
	}

	// 检查上一个周期的持仓，哪些现在消失了
	for posKey, lastPos := range at.lastPositions {
		if !currentPosMap[posKey] {
			if at.wasPositionLifecycleRecentlyClosed(lastPos.Symbol, lastPos.Side, now) {
				log.Printf("[AUTO-CLOSE] 跳过近期已确认平仓的快照事件: %s %s", lastPos.Symbol, lastPos.Side)
				delete(at.lastPositions, posKey)
				continue
			}

			// 这个持仓消失了，说明被自动平仓了（止损/止盈触发）
			// 获取当前价格作为平仓价格的近似值
			marketData, err := getMarketData(lastPos.Symbol)
			closePrice := 0.0
			if err == nil {
				closePrice = marketData.CurrentPrice
			} else {
				// 如果无法获取当前价格，使用入场价作为fallback
				closePrice = lastPos.EntryPrice
			}

			pnlUSD, pnlPercent := autoClosePnL(lastPos.Side, lastPos.EntryPrice, closePrice, lastPos.Quantity, lastPos.Leverage)

			// 确定是平多仓还是平空仓
			action := "auto_close_long"
			if lastPos.Side == "short" {
				action = "auto_close_short"
			}

			if !at.claimAutoCloseEvent(lastPos.Symbol, lastPos.Side, 0, now) {
				log.Printf("[AUTO-CLOSE] 跳过重复快照事件: %s %s", lastPos.Symbol, lastPos.Side)
				delete(at.lastPositions, posKey)
				continue
			}

			// 创建自动平仓记录
			autoClosedAction := at.handleAutoCloseEvent(autoCloseEvent{
				Symbol:      lastPos.Symbol,
				Side:        lastPos.Side,
				Source:      autoCloseSourceSnapshot,
				ExitPrice:   closePrice,
				EntryPrice:  lastPos.EntryPrice,
				Quantity:    lastPos.Quantity,
				Leverage:    lastPos.Leverage,
				RealizedPnL: pnlUSD,
				PnLPercent:  pnlPercent,
				CloseReason: at.inferAutoCloseReason(lastPos.Symbol, lastPos.Side, closePrice),
				CloseTime:   now,
			}, false)
			autoClosedAction.Action = action

			autoClosedActions = append(autoClosedActions, autoClosedAction)
			delete(at.lastPositions, posKey)
			log.Printf("[AUTO-CLOSE] 检测到自动平仓: %s %s @ %.4f (可能由止损/止盈触发)",
				lastPos.Symbol, action, closePrice)
		}
	}

	return autoClosedActions
}

func (at *AutoTrader) reconcileStaleTradePlans(currentPositions []decision.PositionInfo) []logger.DecisionAction {
	currentPosMap := make(map[string]bool)
	for _, pos := range currentPositions {
		currentPosMap[pos.Symbol+"_"+pos.Side] = true
	}

	var actions []logger.DecisionAction
	for _, plan := range decision.GetAllPlans() {
		if plan == nil || (plan.TraderID != "" && plan.TraderID != at.id) {
			continue
		}
		if plan.Status != "" && !strings.EqualFold(plan.Status, "ACTIVE") {
			continue
		}

		side := plan.Direction
		if side == "" {
			continue
		}
		if currentPosMap[plan.Symbol+"_"+side] {
			continue
		}
		if !at.claimAutoCloseEvent(plan.Symbol, side, 0, time.Now()) {
			continue
		}

		marketPrice := 0.0
		if marketData, err := getMarketData(plan.Symbol); err == nil {
			marketPrice = marketData.CurrentPrice
		}
		closePrice, closeReason := planAutoClosePriceAndReason(plan, marketPrice)

		entryPrice := plan.ActualEntry
		if entryPrice <= 0 {
			entryPrice = plan.EntryPrice
		}
		quantity := plan.ActualQuantity
		if quantity <= 0 && entryPrice > 0 {
			quantity = plan.PositionSizeUSD / entryPrice
		}
		pnlUSD, pnlPercent := autoClosePnL(side, entryPrice, closePrice, quantity, plan.Leverage)

		action := at.handleAutoCloseEvent(autoCloseEvent{
			Symbol:      plan.Symbol,
			Side:        side,
			Source:      autoCloseSourceStalePlan,
			EntryPrice:  entryPrice,
			ExitPrice:   closePrice,
			Quantity:    quantity,
			Leverage:    plan.Leverage,
			RealizedPnL: pnlUSD,
			PnLPercent:  pnlPercent,
			CloseReason: closeReason,
			CloseTime:   time.Now(),
		}, false)
		actions = append(actions, action)
		log.Printf("[AUTO-CLOSE] 清理无持仓交易计划: %s %s reason=%s exit=%.4f", plan.Symbol, side, closeReason, closePrice)
	}

	return actions
}

func planAutoClosePriceAndReason(plan *decision.TradePlan, marketPrice float64) (float64, string) {
	closePrice := marketPrice
	if closePrice <= 0 {
		closePrice = plan.EntryPrice
	}

	stopLoss := plan.CurrentStopLoss
	if stopLoss <= 0 {
		stopLoss = plan.StopLoss
	}

	if plan.Direction == "long" {
		if stopLoss > 0 && marketPrice > 0 && marketPrice <= stopLoss {
			return stopLoss, "STOP_LOSS"
		}
		fullTP, fullTPReason := planFullTakeProfitAndReason(plan)
		if fullTP > 0 && marketPrice > 0 && marketPrice >= fullTP {
			return fullTP, fullTPReason
		}
	} else {
		if stopLoss > 0 && marketPrice > 0 && marketPrice >= stopLoss {
			return stopLoss, "STOP_LOSS"
		}
		fullTP, fullTPReason := planFullTakeProfitAndReason(plan)
		if fullTP > 0 && marketPrice > 0 && marketPrice <= fullTP {
			return fullTP, fullTPReason
		}
	}

	return closePrice, "AUTO_CLOSE_DETECTED"
}

func (at *AutoTrader) inferAutoCloseReason(symbol, side string, closePrice float64) string {
	plan := decision.GetPlanByScope(at.id, symbol, side)
	if plan == nil || closePrice <= 0 {
		return "AUTO_CLOSE_DETECTED"
	}

	stopLoss := plan.CurrentStopLoss
	if stopLoss <= 0 {
		stopLoss = plan.StopLoss
	}

	if side == "long" {
		if stopLoss > 0 && closePrice <= stopLoss {
			return "STOP_LOSS"
		}
		fullTP, fullTPReason := planFullTakeProfitAndReason(plan)
		if fullTP > 0 && closePrice >= fullTP {
			return fullTPReason
		}
	} else {
		if stopLoss > 0 && closePrice >= stopLoss {
			return "STOP_LOSS"
		}
		fullTP, fullTPReason := planFullTakeProfitAndReason(plan)
		if fullTP > 0 && closePrice <= fullTP {
			return fullTPReason
		}
	}

	return "AUTO_CLOSE_DETECTED"
}

func planFullTakeProfitAndReason(plan *decision.TradePlan) (float64, string) {
	if plan == nil {
		return 0, "TAKE_PROFIT"
	}
	if plan.ExchangeFullTakeProfit > 0 {
		return plan.ExchangeFullTakeProfit, "EXCHANGE_FULL_TP"
	}
	return plan.TakeProfit, "TAKE_PROFIT"
}
