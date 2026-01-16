package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"nofx/market"
	"nofx/mcp"
	"nofx/pool"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 核心数据结构
// ============================================================================

// PositionInfo 持仓信息
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"`
	StopLoss         float64 `json:"stop_loss,omitempty"`
	TakeProfit       float64 `json:"take_profit,omitempty"`
}

// AccountInfo 账户信息
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`
	AvailableBalance float64 `json:"available_balance"`
	TotalPnL         float64 `json:"total_pnl"`
	TotalPnLPct      float64 `json:"total_pnl_pct"`
	MarginUsed       float64 `json:"margin_used"`
	MarginUsedPct    float64 `json:"margin_used_pct"`
	PositionCount    int     `json:"position_count"`
}

// CandidateCoin 候选币种
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"`
}

// OITopData 持仓量增长Top数据
type OITopData struct {
	Rank              int
	OIDeltaPercent    float64
	OIDeltaValue      float64
	PriceDeltaPercent float64
	NetLong           float64
	NetShort          float64
}

// CorrelationData 相关性数据
type CorrelationData struct {
	Symbol     string  `json:"symbol"`
	BTCCorr    float64 `json:"btc_correlation"`
	IsHighCorr bool    `json:"is_high_corr"`
	RiskWeight float64 `json:"risk_weight"`
}

// CircuitBreakerState 熔断状态
type CircuitBreakerState struct {
	IsTriggered     bool      `json:"is_triggered"`
	TriggerReason   string    `json:"trigger_reason"`
	TriggerTime     time.Time `json:"trigger_time"`
	CooldownMinutes int       `json:"cooldown_minutes"`
}

// ============================================================================
// 🆕 失效条件结构化定义
// ============================================================================

// InvalidationConditionType 失效条件类型
type InvalidationConditionType string

const (
	ICT_EMA_CROSS_DOWN InvalidationConditionType = "ema_cross_down" // EMA死叉
	ICT_EMA_CROSS_UP   InvalidationConditionType = "ema_cross_up"   // EMA金叉
	ICT_PRICE_BELOW    InvalidationConditionType = "price_below"    // 价格跌破指标
	ICT_PRICE_ABOVE    InvalidationConditionType = "price_above"    // 价格突破指标
	ICT_RSI_ABOVE      InvalidationConditionType = "rsi_above"      // RSI超过阈值
	ICT_RSI_BELOW      InvalidationConditionType = "rsi_below"      // RSI低于阈值
	ICT_ADX_BELOW      InvalidationConditionType = "adx_below"      // ADX低于阈值
	ICT_MACD_CROSS     InvalidationConditionType = "macd_cross"     // MACD交叉
	ICT_TREND_REVERSAL InvalidationConditionType = "trend_reversal" // 趋势反转
	ICT_CUSTOM         InvalidationConditionType = "custom"         // 自定义条件
)

// ParsedInvalidationCondition 解析后的失效条件
type ParsedInvalidationCondition struct {
	Type       InvalidationConditionType `json:"type"`
	Timeframe  string                    `json:"timeframe"`   // 4H, 1H, 15m
	Indicator  string                    `json:"indicator"`   // EMA20, EMA50, RSI14等
	Indicator2 string                    `json:"indicator2"`  // 第二个指标（用于交叉）
	Threshold  float64                   `json:"threshold"`   // 阈值
	Direction  string                    `json:"direction"`   // long失效/short失效
	RawText    string                    `json:"raw_text"`    // 原始文本
	IsValid    bool                      `json:"is_valid"`    // 是否解析成功
	ParseError string                    `json:"parse_error"` // 解析错误信息
}

// InvalidationConditionParser 失效条件解析器
type InvalidationConditionParser struct {
	patterns map[InvalidationConditionType]*regexp.Regexp
}

// 全局解析器实例
var conditionParser *InvalidationConditionParser

// InitConditionParser 初始化条件解析器
func InitConditionParser() {
	conditionParser = &InvalidationConditionParser{
		patterns: make(map[InvalidationConditionType]*regexp.Regexp),
	}

	// 定义各种条件的正则表达式
	// 格式: "4H:EMA_CROSS_DOWN:EMA20:EMA50" 或 "4H:PRICE_BELOW:EMA50" 或 "1H:RSI_ABOVE:70"
	conditionParser.patterns[ICT_EMA_CROSS_DOWN] = regexp.MustCompile(`(?i)(\d+[HhMm]):?EMA_CROSS_DOWN:?(EMA\d+):?(EMA\d+)?`)
	conditionParser.patterns[ICT_EMA_CROSS_UP] = regexp.MustCompile(`(?i)(\d+[HhMm]):?EMA_CROSS_UP:?(EMA\d+):?(EMA\d+)?`)
	conditionParser.patterns[ICT_PRICE_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMm]):?PRICE_BELOW:?(EMA\d+|VWAP|BB_LOWER|\d+\.?\d*)`)
	conditionParser.patterns[ICT_PRICE_ABOVE] = regexp.MustCompile(`(?i)(\d+[HhMm]):?PRICE_ABOVE:?(EMA\d+|VWAP|BB_UPPER|\d+\.?\d*)`)
	conditionParser.patterns[ICT_RSI_ABOVE] = regexp.MustCompile(`(?i)(\d+[HhMm]):?RSI_ABOVE:?(\d+)`)
	conditionParser.patterns[ICT_RSI_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMm]):?RSI_BELOW:?(\d+)`)
	conditionParser.patterns[ICT_ADX_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMm]):?ADX_BELOW:?(\d+)`)
	conditionParser.patterns[ICT_MACD_CROSS] = regexp.MustCompile(`(?i)(\d+[HhMm]):?MACD_CROSS:?(UP|DOWN)`)
	conditionParser.patterns[ICT_TREND_REVERSAL] = regexp.MustCompile(`(?i)(\d+[HhMm]):?TREND_REVERSAL`)
}

// ParseInvalidationCondition 解析失效条件字符串
func ParseInvalidationCondition(condition string) *ParsedInvalidationCondition {
	if conditionParser == nil {
		InitConditionParser()
	}

	result := &ParsedInvalidationCondition{
		RawText: condition,
		IsValid: false,
	}

	if condition == "" {
		result.ParseError = "空条件"
		return result
	}

	// 清理输入
	condition = strings.TrimSpace(condition)

	// 尝试匹配各种模式
	for condType, pattern := range conditionParser.patterns {
		matches := pattern.FindStringSubmatch(condition)
		if len(matches) > 0 {
			result.Type = condType
			result.IsValid = true

			// 解析时间框架
			if len(matches) > 1 {
				result.Timeframe = strings.ToUpper(matches[1])
			}

			// 根据条件类型解析其他参数
			switch condType {
			case ICT_EMA_CROSS_DOWN, ICT_EMA_CROSS_UP:
				if len(matches) > 2 {
					result.Indicator = strings.ToUpper(matches[2])
				}
				if len(matches) > 3 && matches[3] != "" {
					result.Indicator2 = strings.ToUpper(matches[3])
				} else {
					// 默认与EMA50交叉
					result.Indicator2 = "EMA50"
				}

			case ICT_PRICE_BELOW, ICT_PRICE_ABOVE:
				if len(matches) > 2 {
					indicator := matches[2]
					// 检查是否是数字
					if val, err := strconv.ParseFloat(indicator, 64); err == nil {
						result.Threshold = val
						result.Indicator = "PRICE"
					} else {
						result.Indicator = strings.ToUpper(indicator)
					}
				}

			case ICT_RSI_ABOVE, ICT_RSI_BELOW, ICT_ADX_BELOW:
				if len(matches) > 2 {
					if val, err := strconv.ParseFloat(matches[2], 64); err == nil {
						result.Threshold = val
					}
				}
				result.Indicator = "RSI14"
				if condType == ICT_ADX_BELOW {
					result.Indicator = "ADX14"
				}

			case ICT_MACD_CROSS:
				if len(matches) > 2 {
					result.Direction = strings.ToUpper(matches[2])
				}

			case ICT_TREND_REVERSAL:
				// 趋势反转不需要额外参数
			}

			return result
		}
	}

	// 尝试解析自然语言格式（向后兼容）
	result = parseNaturalLanguageCondition(condition)
	return result
}

// parseNaturalLanguageCondition 解析自然语言格式的条件
func parseNaturalLanguageCondition(condition string) *ParsedInvalidationCondition {
	result := &ParsedInvalidationCondition{
		RawText: condition,
		Type:    ICT_CUSTOM,
		IsValid: false,
	}

	condLower := strings.ToLower(condition)

	// 检测时间框架
	timeframes := []string{"4h", "1h", "15m", "30m", "1d"}
	for _, tf := range timeframes {
		if strings.Contains(condLower, tf) {
			result.Timeframe = strings.ToUpper(tf)
			break
		}
	}

	// 检测EMA相关条件
	if strings.Contains(condLower, "ema") {
		// 提取EMA数字
		emaPattern := regexp.MustCompile(`ema\s*(\d+)`)
		emaMatches := emaPattern.FindAllStringSubmatch(condLower, -1)

		if len(emaMatches) >= 1 {
			result.Indicator = fmt.Sprintf("EMA%s", emaMatches[0][1])
		}
		if len(emaMatches) >= 2 {
			result.Indicator2 = fmt.Sprintf("EMA%s", emaMatches[1][1])
		}

		if strings.Contains(condLower, "死叉") || strings.Contains(condLower, "跌破") ||
			strings.Contains(condLower, "below") || strings.Contains(condLower, "下穿") {
			if result.Indicator2 != "" {
				result.Type = ICT_EMA_CROSS_DOWN
			} else {
				result.Type = ICT_PRICE_BELOW
			}
			result.IsValid = true
		} else if strings.Contains(condLower, "金叉") || strings.Contains(condLower, "突破") ||
			strings.Contains(condLower, "above") || strings.Contains(condLower, "上穿") {
			if result.Indicator2 != "" {
				result.Type = ICT_EMA_CROSS_UP
			} else {
				result.Type = ICT_PRICE_ABOVE
			}
			result.IsValid = true
		}
	}

	// 检测RSI相关条件
	if strings.Contains(condLower, "rsi") {
		rsiPattern := regexp.MustCompile(`rsi\s*[<>]?\s*(\d+)`)
		if matches := rsiPattern.FindStringSubmatch(condLower); len(matches) > 1 {
			if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
				result.Threshold = val
				result.Indicator = "RSI14"
				if strings.Contains(condLower, ">") || strings.Contains(condLower, "超过") ||
					strings.Contains(condLower, "above") {
					result.Type = ICT_RSI_ABOVE
				} else {
					result.Type = ICT_RSI_BELOW
				}
				result.IsValid = true
			}
		}
	}

	// 检测ADX相关条件
	if strings.Contains(condLower, "adx") {
		adxPattern := regexp.MustCompile(`adx\s*[<>]?\s*(\d+)`)
		if matches := adxPattern.FindStringSubmatch(condLower); len(matches) > 1 {
			if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
				result.Threshold = val
				result.Indicator = "ADX14"
				result.Type = ICT_ADX_BELOW
				result.IsValid = true
			}
		}
	}

	// 检测趋势反转
	if strings.Contains(condLower, "趋势反转") || strings.Contains(condLower, "trend reversal") {
		result.Type = ICT_TREND_REVERSAL
		result.IsValid = true
	}

	// 检测MACD交叉
	if strings.Contains(condLower, "macd") {
		if strings.Contains(condLower, "死叉") || strings.Contains(condLower, "下穿") {
			result.Type = ICT_MACD_CROSS
			result.Direction = "DOWN"
			result.IsValid = true
		} else if strings.Contains(condLower, "金叉") || strings.Contains(condLower, "上穿") {
			result.Type = ICT_MACD_CROSS
			result.Direction = "UP"
			result.IsValid = true
		}
	}

	if !result.IsValid {
		result.ParseError = "无法解析条件格式"
	}

	return result
}

// FormatInvalidationCondition 格式化输出失效条件（供显示用）
func FormatInvalidationCondition(condition string) string {
	parsed := ParseInvalidationCondition(condition)
	if !parsed.IsValid {
		return fmt.Sprintf("❓ %s (未能解析)", condition)
	}

	var formatted string
	switch parsed.Type {
	case ICT_EMA_CROSS_DOWN:
		formatted = fmt.Sprintf("📉 %s %s下穿%s (EMA死叉)", parsed.Timeframe, parsed.Indicator, parsed.Indicator2)
	case ICT_EMA_CROSS_UP:
		formatted = fmt.Sprintf("📈 %s %s上穿%s (EMA金叉)", parsed.Timeframe, parsed.Indicator, parsed.Indicator2)
	case ICT_PRICE_BELOW:
		if parsed.Threshold > 0 {
			formatted = fmt.Sprintf("📉 %s 价格跌破 %.4f", parsed.Timeframe, parsed.Threshold)
		} else {
			formatted = fmt.Sprintf("📉 %s 价格跌破 %s", parsed.Timeframe, parsed.Indicator)
		}
	case ICT_PRICE_ABOVE:
		if parsed.Threshold > 0 {
			formatted = fmt.Sprintf("📈 %s 价格突破 %.4f", parsed.Timeframe, parsed.Threshold)
		} else {
			formatted = fmt.Sprintf("📈 %s 价格突破 %s", parsed.Timeframe, parsed.Indicator)
		}
	case ICT_RSI_ABOVE:
		formatted = fmt.Sprintf("⚠️ %s RSI > %.0f", parsed.Timeframe, parsed.Threshold)
	case ICT_RSI_BELOW:
		formatted = fmt.Sprintf("⚠️ %s RSI < %.0f", parsed.Timeframe, parsed.Threshold)
	case ICT_ADX_BELOW:
		formatted = fmt.Sprintf("⚠️ %s ADX < %.0f (趋势减弱)", parsed.Timeframe, parsed.Threshold)
	case ICT_MACD_CROSS:
		if parsed.Direction == "DOWN" {
			formatted = fmt.Sprintf("📉 %s MACD死叉", parsed.Timeframe)
		} else {
			formatted = fmt.Sprintf("📈 %s MACD金叉", parsed.Timeframe)
		}
	case ICT_TREND_REVERSAL:
		formatted = fmt.Sprintf("🔄 %s 趋势反转", parsed.Timeframe)
	default:
		formatted = fmt.Sprintf("📋 %s", condition)
	}

	return formatted
}

// ============================================================================
// 🆕 优化1: 交易计划持久化到JSON文件
// ============================================================================

// TradePlan 交易计划
type TradePlan struct {
	ID                          string                       `json:"id"`
	Symbol                      string                       `json:"symbol"`
	Direction                   string                       `json:"direction"`
	EntryPrice                  float64                      `json:"entry_price"`
	StopLoss                    float64                      `json:"stop_loss"`
	TakeProfit                  float64                      `json:"take_profit"`
	PositionSizeUSD             float64                      `json:"position_size_usd"`
	Leverage                    int                          `json:"leverage"`
	EntryReason                 string                       `json:"entry_reason"`
	InvalidationCondition       string                       `json:"invalidation_condition"`
	ParsedInvalidationCondition *ParsedInvalidationCondition `json:"parsed_invalidation_condition,omitempty"`
	InvalidationPrice           float64                      `json:"invalidation_price"`
	MinHoldMinutes              int                          `json:"min_hold_minutes"`
	CreatedAt                   time.Time                    `json:"created_at"`
	Status                      string                       `json:"status"`
	Confidence                  int                          `json:"confidence"`
	RiskUSD                     float64                      `json:"risk_usd"`

	// 🔧 分批止盈相关 - 使用档位索引记录
	ExecutedTranches    map[int]bool `json:"executed_tranches,omitempty"`
	LastExecutedTranche int          `json:"last_executed_tranche"`
	TotalClosedPercent  float64      `json:"total_closed_percent"`

	// 移动止损相关
	TrailingStopActive bool    `json:"trailing_stop_active"`
	CurrentStopLoss    float64 `json:"current_stop_loss"`

	// 实际成交信息
	ActualQuantity float64 `json:"actual_quantity,omitempty"`
	ActualEntry    float64 `json:"actual_entry,omitempty"`

	// 🔧 修复: 峰值追踪 - 这些字段必须通过 UpdatePlan 方法更新
	EntryATR       float64 `json:"entry_atr,omitempty"`
	PeakPrice      float64 `json:"peak_price,omitempty"`
	PeakPnLPercent float64 `json:"peak_pnl_percent,omitempty"`

	// 🆕 新增: 动态止盈追踪
	OriginalTakeProfit float64   `json:"original_take_profit,omitempty"`
	LastTPAdjustTime   time.Time `json:"last_tp_adjust_time,omitempty"`
}

// TradePlanManager 交易计划管理器（带持久化）
type TradePlanManager struct {
	plans       map[string]*TradePlan
	mu          sync.RWMutex
	filePath    string
	autoSave    bool
	lastSaveErr error
}

// 全局计划管理器
var planManager *TradePlanManager

// 默认数据目录
const defaultDataDir = "./data"
const plansFileName = "trade_plans.json"

// InitPlanManager 初始化计划管理器
func InitPlanManager(dataDir string) error {
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	// 确保目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	filePath := filepath.Join(dataDir, plansFileName)

	planManager = &TradePlanManager{
		plans:    make(map[string]*TradePlan),
		filePath: filePath,
		autoSave: true,
	}
	// 初始化条件解析器
	InitConditionParser()

	// 尝试从文件加载
	if err := planManager.loadFromFile(); err != nil {
		log.Printf("⚠️ 加载交易计划失败（可能是首次运行）: %v", err)
	} else {
		log.Printf("📂 成功加载 %d 个交易计划", len(planManager.plans))
	}

	return nil
}

// ============================================================================
// 修改: decision/decision.go 中的 loadFromFile
// ============================================================================
// loadFromFile 从文件加载计划
func (m *TradePlanManager) loadFromFile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常的
		}
		return err
	}

	var persistentData PersistentData
	if err := json.Unmarshal(data, &persistentData); err != nil {
		return fmt.Errorf("解析JSON失败: %w", err)
	}

	if persistentData.Plans != nil {
		m.plans = persistentData.Plans

		// 🔧 确保所有计划的 ExecutedTranches 已初始化
		for _, plan := range m.plans {
			if plan.ExecutedTranches == nil {
				plan.ExecutedTranches = make(map[int]bool)
			}
			if plan.OriginalTakeProfit == 0 {
				plan.OriginalTakeProfit = plan.TakeProfit
			}
		}
	}

	// 恢复统计数据
	if persistentData.Statistics != nil {
		tradeStatsLock.Lock()
		tradeStats = persistentData.Statistics
		tradeStatsLock.Unlock()
	}

	// 恢复收益率序列
	if persistentData.Returns != nil {
		returnsLock.Lock()
		returnsSeries = persistentData.Returns
		returnsLock.Unlock()
	}

	// 🆕 恢复已平仓交易记录
	if persistentData.ClosedTrades != nil {
		closedTradesLock.Lock()
		closedTrades = persistentData.ClosedTrades
		closedTradesLock.Unlock()
	}

	return nil
}

// saveToFile 保存计划到文件
// decision/persistence.go - 修改 saveToFile

func (m *TradePlanManager) saveToFile() error {
	m.mu.RLock()
	plansCopy := make(map[string]*TradePlan)
	for k, v := range m.plans {
		plansCopy[k] = v
	}
	m.mu.RUnlock()

	tradeStatsLock.RLock()
	statsCopy := *tradeStats
	tradeStatsLock.RUnlock()

	returnsLock.RLock()
	returnsCopy := make([]float64, len(returnsSeries))
	copy(returnsCopy, returnsSeries)
	returnsLock.RUnlock()

	// 🆕 获取已平仓交易记录
	closedTradesLock.RLock()
	closedTradesCopy := make([]ClosedTradeRecord, len(closedTrades))
	copy(closedTradesCopy, closedTrades)
	closedTradesLock.RUnlock()

	persistentData := PersistentData{
		Plans:        plansCopy,
		Statistics:   &statsCopy,
		Returns:      returnsCopy,
		ClosedTrades: closedTradesCopy, // 🆕 新增
		UpdatedAt:    time.Now(),
	}

	data, err := json.MarshalIndent(persistentData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	tempFile := m.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := os.Rename(tempFile, m.filePath); err != nil {
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	return nil
}

// autoSaveIfEnabled 自动保存
func (m *TradePlanManager) autoSaveIfEnabled() {
	if !m.autoSave {
		return
	}
	if err := m.saveToFile(); err != nil {
		m.lastSaveErr = err
		log.Printf("⚠️ 自动保存失败: %v", err)
	}
}

// GetPlan 获取交易计划
func (m *TradePlanManager) GetPlan(symbol string) *TradePlan {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if plan, ok := m.plans[symbol]; ok {
		// ✅ 返回深拷贝，避免外部无锁修改
		planCopy := *plan
		if plan.ParsedInvalidationCondition != nil {
			condCopy := *plan.ParsedInvalidationCondition
			planCopy.ParsedInvalidationCondition = &condCopy
		}
		// 🔧 深拷贝 ExecutedTranches map
		if plan.ExecutedTranches != nil {
			planCopy.ExecutedTranches = make(map[int]bool)
			for k, v := range plan.ExecutedTranches {
				planCopy.ExecutedTranches[k] = v
			}
		}
		return &planCopy
	}
	return nil
}

// GetPlanUnsafe 获取原始指针（仅供内部使用，调用者需持有锁）
func (m *TradePlanManager) GetPlanUnsafe(symbol string) *TradePlan {
	return m.plans[symbol]
}

// UpdatePlan 更新计划（线程安全）
func (m *TradePlanManager) UpdatePlan(symbol string, updateFn func(*TradePlan)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if plan, ok := m.plans[symbol]; ok {
		updateFn(plan)
	}
}

// SetPlan 设置交易计划（自动持久化）
func (m *TradePlanManager) SetPlan(plan *TradePlan) {
	m.mu.Lock()

	// 🔧 确保 ExecutedTranches 已初始化
	if plan.ExecutedTranches == nil {
		plan.ExecutedTranches = make(map[int]bool)
	}

	// 🔧 记录原始止盈价
	if plan.OriginalTakeProfit == 0 {
		plan.OriginalTakeProfit = plan.TakeProfit
	}

	m.plans[plan.Symbol] = plan
	m.mu.Unlock()

	// 格式化输出失效条件
	invalidationDisplay := "无"
	if plan.InvalidationCondition != "" {
		invalidationDisplay = FormatInvalidationCondition(plan.InvalidationCondition)
	}

	log.Printf("📋 创建交易计划: %s %s @ %.4f, SL=%.4f, TP=%.4f, 最小持仓=%d分钟",
		plan.Symbol, plan.Direction, plan.EntryPrice, plan.StopLoss, plan.TakeProfit, plan.MinHoldMinutes)
	log.Printf("   └─ 失效条件: %s", invalidationDisplay)

	m.autoSaveIfEnabled()
}

// RemovePlan 移除交易计划（自动持久化）
func (m *TradePlanManager) RemovePlan(symbol string) {
	m.mu.Lock()
	if plan, exists := m.plans[symbol]; exists {
		log.Printf("📋 移除交易计划: %s (状态: %s, 峰值盈利: %.2f%%)",
			symbol, plan.Status, plan.PeakPnLPercent)
		delete(m.plans, symbol)
	}
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// UpdatePlanStopLoss 更新计划止损（自动持久化）
func (m *TradePlanManager) UpdatePlanStopLoss(symbol string, newSL float64) {
	m.mu.Lock()
	if plan, exists := m.plans[symbol]; exists {
		oldSL := plan.CurrentStopLoss
		if oldSL == 0 {
			oldSL = plan.StopLoss
		}
		plan.CurrentStopLoss = newSL
		plan.TrailingStopActive = true
		log.Printf("📋 更新 %s 止损: %.4f → %.4f", symbol, oldSL, newSL)
	}
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// ForceSave 强制保存
func (m *TradePlanManager) ForceSave() error {
	return m.saveToFile()
}

// ============================================================================
// 🆕 优化2: 夏普比率计算
// ============================================================================

var (
	returnsSeries []float64 // 收益率序列
	returnsLock   sync.RWMutex
	riskFreeRate  = 0.0 // 年化无风险利率（可配置）
)

// SharpeConfig 夏普比率配置
type SharpeConfig struct {
	RiskFreeRate     float64 // 年化无风险利率
	AnnualizeFactor  float64 // 年化因子（日收益用252，小时收益用8760）
	MinTradesForCalc int     // 最小交易数量才计算
}

var sharpeConfig = SharpeConfig{
	RiskFreeRate:     0.0,
	AnnualizeFactor:  252, // 假设每日一笔交易
	MinTradesForCalc: 10,
}

// SetSharpeConfig 设置夏普比率配置
func SetSharpeConfig(config SharpeConfig) {
	sharpeConfig = config
}

// AddReturn 添加收益率记录
func AddReturn(returnPct float64) {
	returnsLock.Lock()
	//defer returnsLock.Unlock()

	returnsSeries = append(returnsSeries, returnPct)

	// 保留最近1000笔
	if len(returnsSeries) > 1000 {
		returnsSeries = returnsSeries[len(returnsSeries)-1000:]
	}

	returnsLock.Unlock() // ← 先释放锁

	// 自动保存
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// ============================================================================
// 无锁版本（供内部使用，调用者需确保已持有 returnsLock）
// ============================================================================

// calculateSharpeRatioUnlocked 计算夏普比率（无锁版本）
// 注意：调用此函数前，调用者必须已经持有 returnsLock.RLock() 或 returnsLock.Lock()
func calculateSharpeRatioUnlocked() float64 {
	if len(returnsSeries) < sharpeConfig.MinTradesForCalc {
		return 0
	}

	// 计算平均收益率
	sum := 0.0
	for _, r := range returnsSeries {
		sum += r
	}
	meanReturn := sum / float64(len(returnsSeries))

	// 计算标准差
	sumSquaredDiff := 0.0
	for _, r := range returnsSeries {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSquaredDiff / float64(len(returnsSeries)))

	if stdDev == 0 {
		return 0
	}

	// 计算周期无风险利率
	periodicRiskFree := sharpeConfig.RiskFreeRate / sharpeConfig.AnnualizeFactor

	// 夏普比率 = (平均收益 - 无风险收益) / 标准差 * sqrt(年化因子)
	sharpe := (meanReturn - periodicRiskFree) / stdDev * math.Sqrt(sharpeConfig.AnnualizeFactor)

	return sharpe
}

// calculateSortinoRatioUnlocked 计算索提诺比率（无锁版本）
// 注意：调用此函数前，调用者必须已经持有 returnsLock.RLock() 或 returnsLock.Lock()
func calculateSortinoRatioUnlocked() float64 {
	if len(returnsSeries) < sharpeConfig.MinTradesForCalc {
		return 0
	}

	// 计算平均收益率
	sum := 0.0
	for _, r := range returnsSeries {
		sum += r
	}
	meanReturn := sum / float64(len(returnsSeries))

	// 计算下行标准差（只计算负收益）
	sumSquaredNegative := 0.0
	negativeCount := 0
	for _, r := range returnsSeries {
		if r < 0 {
			sumSquaredNegative += r * r
			negativeCount++
		}
	}

	if negativeCount == 0 {
		return 10.0 // 没有负收益，返回较高值
	}

	downwardStdDev := math.Sqrt(sumSquaredNegative / float64(len(returnsSeries)))

	if downwardStdDev == 0 {
		return 0
	}

	periodicRiskFree := sharpeConfig.RiskFreeRate / sharpeConfig.AnnualizeFactor
	sortino := (meanReturn - periodicRiskFree) / downwardStdDev * math.Sqrt(sharpeConfig.AnnualizeFactor)

	return sortino
}

// ============================================================================
// 带锁版本（供外部调用）
// ============================================================================

// CalculateSharpeRatio 计算夏普比率（带锁版本，供外部调用）
func CalculateSharpeRatio() float64 {
	returnsLock.RLock()
	defer returnsLock.RUnlock()
	return calculateSharpeRatioUnlocked()
}

// CalculateSortinoRatio 计算索提诺比率（带锁版本，供外部调用）
func CalculateSortinoRatio() float64 {
	returnsLock.RLock()
	defer returnsLock.RUnlock()
	return calculateSortinoRatioUnlocked()
}

// GetReturnsStats 获取收益率统计（带锁版本）
func GetReturnsStats() map[string]float64 {
	returnsLock.RLock()
	defer returnsLock.RUnlock()

	if len(returnsSeries) == 0 {
		return map[string]float64{
			"count":         0,
			"sharpe_ratio":  0,
			"sortino_ratio": 0,
		}
	}

	sum := 0.0
	positiveSum := 0.0
	negativeSum := 0.0
	positiveCount := 0
	maxReturn := returnsSeries[0]
	minReturn := returnsSeries[0]

	for _, r := range returnsSeries {
		sum += r
		if r > 0 {
			positiveSum += r
			positiveCount++
		} else {
			negativeSum += r
		}
		if r > maxReturn {
			maxReturn = r
		}
		if r < minReturn {
			minReturn = r
		}
	}

	meanReturn := sum / float64(len(returnsSeries))
	winRate := float64(positiveCount) / float64(len(returnsSeries))

	return map[string]float64{
		"count":         float64(len(returnsSeries)),
		"mean_return":   meanReturn,
		"total_return":  sum,
		"max_return":    maxReturn,
		"min_return":    minReturn,
		"win_rate":      winRate,
		"sharpe_ratio":  calculateSharpeRatioUnlocked(),  // ← 使用无锁版本
		"sortino_ratio": calculateSortinoRatioUnlocked(), // ← 使用无锁版本
	}
}

// ============================================================================
// 🆕 优化3: 使用正则表达式优化JSON解析（修复版）
// ============================================================================

// JSONExtractor JSON提取器
type JSONExtractor struct {
	arrayPattern  *regexp.Regexp
	objectPattern *regexp.Regexp
}

var jsonExtractor *JSONExtractor

// 中文引号的Unicode常量
const (
	LeftDoubleQuote  = '\u201c' // "
	RightDoubleQuote = '\u201d' // "
	LeftSingleQuote  = '\u2018' // '
	RightSingleQuote = '\u2019' // '
)

func init() {
	jsonExtractor = &JSONExtractor{
		arrayPattern:  regexp.MustCompile(`(?s)\[[\s\S]*?\]`),
		objectPattern: regexp.MustCompile(`(?s)\{[^{}]*\}`),
	}

	// 初始化默认的计划管理器
	if planManager == nil {
		planManager = &TradePlanManager{
			plans:    make(map[string]*TradePlan),
			filePath: filepath.Join(defaultDataDir, plansFileName),
			autoSave: false,
		}
	}
}

// cleanText 清理文本（修复版）
func (e *JSONExtractor) cleanText(text string) string {
	result := text

	// 移除markdown代码块标记
	codeBlockStart := regexp.MustCompile("(?s)```json\\s*")
	codeBlockEnd := regexp.MustCompile("(?s)```\\s*")
	result = codeBlockStart.ReplaceAllString(result, "")
	result = codeBlockEnd.ReplaceAllString(result, "")

	// 替换中文引号为英文引号（使用rune转换）
	result = strings.Map(func(r rune) rune {
		switch r {
		case LeftDoubleQuote, RightDoubleQuote:
			return '"'
		case LeftSingleQuote, RightSingleQuote:
			return '\''
		default:
			return r
		}
	}, result)

	return result
}

// ExtractJSONArray 从文本中提取JSON数组
func (e *JSONExtractor) ExtractJSONArray(text string) (string, error) {
	// 第一步：清理文本
	cleaned := e.cleanText(text)

	// 第二步：找到所有可能的JSON数组
	matches := e.findJSONArrays(cleaned)
	if len(matches) == 0 {
		return "", fmt.Errorf("未找到JSON数组")
	}

	// 第三步：验证并返回第一个有效的JSON数组
	for _, match := range matches {
		fixed := e.fixJSON(match)
		if json.Valid([]byte(fixed)) {
			return fixed, nil
		}
	}

	// 如果没有有效的，尝试修复第一个
	fixed := e.fixJSON(matches[0])
	return fixed, nil
}

// findJSONArrays 查找所有JSON数组
func (e *JSONExtractor) findJSONArrays(text string) []string {
	var results []string

	for i := 0; i < len(text); i++ {
		if text[i] == '[' {
			end := e.findMatchingBracket(text, i)
			if end > i {
				results = append(results, text[i:end+1])
			}
		}
	}

	return results
}

// findMatchingBracket 查找匹配的括号
func (e *JSONExtractor) findMatchingBracket(s string, start int) int {
	if start >= len(s) || s[start] != '[' {
		return -1
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		char := s[i]

		if escaped {
			escaped = false
			continue
		}

		if char == '\\' && inString {
			escaped = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch char {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

// fixJSON 修复常见的JSON问题
func (e *JSONExtractor) fixJSON(jsonStr string) string {
	result := jsonStr

	// 1. 移除尾部逗号
	trailingComma := regexp.MustCompile(`,(\s*[\]\}])`)
	result = trailingComma.ReplaceAllString(result, "$1")

	// 2. 修复无引号的key
	unquotedKey := regexp.MustCompile(`([{\[,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)(\s*:)`)
	result = unquotedKey.ReplaceAllString(result, `$1"$2"$3`)

	// 3. 替换特殊值为null
	result = regexp.MustCompile(`\bNaN\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\bInfinity\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\b-Infinity\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\bundefined\b`).ReplaceAllString(result, "null")

	// 4. 修复单引号字符串
	singleQuote := regexp.MustCompile(`'([^']*)'`)
	result = singleQuote.ReplaceAllString(result, `"$1"`)

	// 5. 移除单行注释
	lineComment := regexp.MustCompile(`//[^\n]*`)
	result = lineComment.ReplaceAllString(result, "")

	// 6. 移除多行注释
	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	result = blockComment.ReplaceAllString(result, "")

	return result
}

// fixMissingQuotes 修复引号问题（使用字节替换）
func fixMissingQuotes(jsonStr string) string {
	// 使用 strings.Map 替换中文引号
	result := strings.Map(func(r rune) rune {
		switch r {
		case '\u201c', '\u201d': // 中文双引号
			return '"'
		case '\u2018', '\u2019': // 中文单引号
			return '\''
		default:
			return r
		}
	}, jsonStr)

	return result
}

// ExtractDecisionsRobust 健壮的决策提取（修复版）
func ExtractDecisionsRobust(response string) ([]Decision, string, error) {
	// 如果响应为空，返回空结果
	if strings.TrimSpace(response) == "" {
		return nil, "", fmt.Errorf("响应为空")
	}

	// 使用JSON提取器
	jsonStr, err := jsonExtractor.ExtractJSONArray(response)
	if err != nil {
		return extractDecisionsFallback(response)
	}

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonStr), &decisions); err != nil {
		decisions, err = parseDecisionsOneByOne(jsonStr)
		if err != nil {
			return nil, "", fmt.Errorf("JSON解析失败: %w", err)
		}
	}

	// 提取AI分析部分（JSON之前的文本）
	cotTrace := extractAnalysisPart(response)

	return decisions, cotTrace, nil
}

// 🆕 提取分析部分（不包含JSON）
func extractAnalysisPart(response string) string {
	// 找到JSON数组开始位置
	arrayStart := strings.Index(response, "[")
	if arrayStart <= 0 {
		return ""
	}

	// 提取JSON之前的内容
	analysisPart := strings.TrimSpace(response[:arrayStart])

	// 清理markdown代码块标记
	analysisPart = strings.TrimSuffix(analysisPart, "```json")
	analysisPart = strings.TrimSuffix(analysisPart, "```")
	analysisPart = strings.TrimSpace(analysisPart)

	return analysisPart
}

// parseDecisionsOneByOne 逐个解析决策对象
func parseDecisionsOneByOne(jsonStr string) ([]Decision, error) {
	var decisions []Decision

	objectPattern := regexp.MustCompile(`(?s)\{[^{}]*\}`)
	matches := objectPattern.FindAllString(jsonStr, -1)

	for _, match := range matches {
		var d Decision
		if err := json.Unmarshal([]byte(match), &d); err != nil {
			log.Printf("⚠️ 跳过无效决策对象: %v", err)
			continue
		}
		if d.Symbol != "" && d.Action != "" {
			decisions = append(decisions, d)
		}
	}

	if len(decisions) == 0 {
		return nil, fmt.Errorf("未能解析出任何有效决策")
	}

	return decisions, nil
}

// extractDecisionsFallback 回退解析方法
func extractDecisionsFallback(response string) ([]Decision, string, error) {
	cotTrace := extractCoTTrace(response)
	decisions, err := extractDecisions(response)
	return decisions, cotTrace, err
}

// ============================================================================
// 🆕 开仓前失效条件预检查
// ============================================================================

// PreOpenInvalidationChecker 开仓前失效条件检查器
type PreOpenInvalidationChecker struct {
	Symbol       string
	Direction    string // "long" or "short"
	MarketData   *market.Data
	CurrentPrice float64
}

// CheckInvalidationPrice 检查失效价格是否已触发
func (c *PreOpenInvalidationChecker) CheckInvalidationPrice(invalidationPrice float64) (bool, string) {
	if invalidationPrice <= 0 {
		return false, ""
	}

	if c.Direction == "long" {
		// 做多时，如果当前价格已经低于失效价格，则不应开仓
		if c.CurrentPrice < invalidationPrice {
			return true, fmt.Sprintf("当前价格(%.4f)已低于失效价格(%.4f)，多单计划已失效",
				c.CurrentPrice, invalidationPrice)
		}
	} else {
		// 做空时，如果当前价格已经高于失效价格，则不应开仓
		if c.CurrentPrice > invalidationPrice {
			return true, fmt.Sprintf("当前价格(%.4f)已高于失效价格(%.4f)，空单计划已失效",
				c.CurrentPrice, invalidationPrice)
		}
	}

	return false, ""
}

// CheckInvalidationCondition 检查失效条件是否已触发
func (c *PreOpenInvalidationChecker) CheckInvalidationCondition(conditionStr string) (bool, string) {
	if conditionStr == "" {
		return false, ""
	}

	// 解析失效条件
	parsed := ParseInvalidationCondition(conditionStr)
	if !parsed.IsValid {
		log.Printf("⚠️ 开仓前检查: 失效条件解析失败: %s - %s", conditionStr, parsed.ParseError)
		return false, "" // 解析失败不阻止开仓，但记录警告
	}

	// 获取对应时间框架的上下文
	ctx := c.getContextForTimeframe(parsed.Timeframe)
	if ctx == nil {
		log.Printf("⚠️ 开仓前检查: 无法获取 %s 时间框架数据", parsed.Timeframe)
		return false, ""
	}

	// 根据条件类型检查
	switch parsed.Type {
	case ICT_EMA_CROSS_DOWN:
		return c.checkEMACrossDown(ctx, parsed)
	case ICT_EMA_CROSS_UP:
		return c.checkEMACrossUp(ctx, parsed)
	case ICT_PRICE_BELOW:
		return c.checkPriceBelow(ctx, parsed)
	case ICT_PRICE_ABOVE:
		return c.checkPriceAbove(ctx, parsed)
	case ICT_RSI_ABOVE:
		return c.checkRSIAbove(ctx, parsed)
	case ICT_RSI_BELOW:
		return c.checkRSIBelow(ctx, parsed)
	case ICT_ADX_BELOW:
		return c.checkADXBelow(ctx, parsed)
	case ICT_MACD_CROSS:
		return c.checkMACDCross(ctx, parsed)
	case ICT_TREND_REVERSAL:
		return c.checkTrendReversal(ctx, parsed)
	default:
		return false, ""
	}
}

// getContextForTimeframe 获取指定时间框架的上下文
func (c *PreOpenInvalidationChecker) getContextForTimeframe(timeframe string) *InvalidationCheckContext {
	if c.MarketData == nil {
		return nil
	}

	ctx := &InvalidationCheckContext{
		CurrentPrice: c.CurrentPrice,
		Timeframe:    timeframe,
	}

	switch strings.ToUpper(timeframe) {
	case "4H":
		if c.MarketData.LongerTermContext != nil {
			ltc := c.MarketData.LongerTermContext
			ctx.EMA20 = ltc.EMA20
			ctx.EMA50 = ltc.EMA50
			ctx.BBUpper = ltc.BollingerUpper
			ctx.BBLower = ltc.BollingerLower
			ctx.RSI14 = market.GetLastValue(ltc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(ltc.ADXValues)
			ctx.DIPlus = market.GetLastValue(ltc.DIPlus)
			ctx.DIMinus = market.GetLastValue(ltc.DIMinus)
			ctx.MACD = market.GetLastValue(ltc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(ltc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(ltc.MACDHist)
		}

	case "1H":
		if c.MarketData.MidTermSeries1h != nil {
			mtc := c.MarketData.MidTermSeries1h
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
		} else {
			ctx.EMA20 = c.MarketData.CurrentEMA20
			ctx.EMA50 = c.MarketData.CurrentEMA50
			ctx.RSI14 = c.MarketData.CurrentRSI14
			ctx.ADX14 = c.MarketData.CurrentADX
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
			ctx.MACD = c.MarketData.CurrentMACD
		}

	case "15M", "30M":
		if c.MarketData.MidTermSeries15m != nil {
			mtc := c.MarketData.MidTermSeries15m
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
		} else {
			ctx.EMA20 = c.MarketData.CurrentEMA20
			ctx.EMA50 = c.MarketData.CurrentEMA50
			ctx.RSI14 = c.MarketData.CurrentRSI14
			ctx.ADX14 = c.MarketData.CurrentADX
			ctx.DIPlus = c.MarketData.CurrentDIPlus
			ctx.DIMinus = c.MarketData.CurrentDIMinus
			ctx.MACD = c.MarketData.CurrentMACD
		}

	default:
		ctx.EMA20 = c.MarketData.CurrentEMA20
		ctx.EMA50 = c.MarketData.CurrentEMA50
		ctx.RSI14 = c.MarketData.CurrentRSI14
		ctx.ADX14 = c.MarketData.CurrentADX
		ctx.DIPlus = c.MarketData.CurrentDIPlus
		ctx.DIMinus = c.MarketData.CurrentDIMinus
		ctx.MACD = c.MarketData.CurrentMACD
		if c.MarketData.IntradaySeries != nil {
			ctx.MACDSignal = market.GetLastValue(c.MarketData.IntradaySeries.MACDSignal)
			ctx.MACDHist = market.GetLastValue(c.MarketData.IntradaySeries.MACDHist)
		}
	}

	return ctx
}

// checkEMACrossDown 检查EMA死叉（开仓前）
func (c *PreOpenInvalidationChecker) checkEMACrossDown(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := c.getEMAValue(ctx, cond.Indicator)
	ema2 := c.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 对于多单，EMA死叉是失效信号
	if c.Direction == "long" && ema1 < ema2 {
		return true, fmt.Sprintf("%s EMA已死叉: %s(%.4f) < %s(%.4f)，多单计划已失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

// checkEMACrossUp 检查EMA金叉（开仓前）
func (c *PreOpenInvalidationChecker) checkEMACrossUp(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := c.getEMAValue(ctx, cond.Indicator)
	ema2 := c.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 对于空单，EMA金叉是失效信号
	if c.Direction == "short" && ema1 > ema2 {
		return true, fmt.Sprintf("%s EMA已金叉: %s(%.4f) > %s(%.4f)，空单计划已失效",
			cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
	}

	return false, ""
}

// checkPriceBelow 检查价格跌破（开仓前）
func (c *PreOpenInvalidationChecker) checkPriceBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = c.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	// 对于多单，价格跌破是失效信号
	if c.Direction == "long" && ctx.CurrentPrice < targetPrice {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)已跌破%s(%.4f)，多单计划已失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

// checkPriceAbove 检查价格突破（开仓前）
func (c *PreOpenInvalidationChecker) checkPriceAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = c.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	// 对于空单，价格突破是失效信号
	if c.Direction == "short" && ctx.CurrentPrice > targetPrice {
		indicator := cond.Indicator
		if cond.Threshold > 0 {
			indicator = fmt.Sprintf("%.4f", cond.Threshold)
		}
		return true, fmt.Sprintf("%s 价格(%.4f)已突破%s(%.4f)，空单计划已失效",
			cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
	}

	return false, ""
}

// checkRSIAbove 检查RSI超过阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkRSIAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 > cond.Threshold {
		// RSI超买：对于多单，极度超买(>80)是获利了结信号
		if c.Direction == "long" && cond.Threshold >= 80 {
			return true, fmt.Sprintf("%s RSI(%.1f) > %.0f 极度超买，多单计划已失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 注意：RSI超买对空单是有利的，不是失效信号
	}

	return false, ""
}

// checkRSIBelow 检查RSI低于阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkRSIBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 < cond.Threshold {
		// RSI超卖：对于空单，极度超卖(<20)是获利了结信号
		if c.Direction == "short" && cond.Threshold <= 20 {
			return true, fmt.Sprintf("%s RSI(%.1f) < %.0f 极度超卖，空单计划已失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 注意：RSI超卖对多单是有利的，不是失效信号
	}

	return false, ""
}

// checkADXBelow 检查ADX低于阈值（开仓前）
func (c *PreOpenInvalidationChecker) checkADXBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.ADX14 < cond.Threshold {
		return true, fmt.Sprintf("%s ADX(%.1f) < %.0f 趋势过弱，计划已失效",
			cond.Timeframe, ctx.ADX14, cond.Threshold)
	}

	return false, ""
}

// checkMACDCross 检查MACD交叉（开仓前）
func (c *PreOpenInvalidationChecker) checkMACDCross(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.MACD == 0 && ctx.MACDSignal == 0 {
		return false, ""
	}

	if cond.Direction == "DOWN" {
		// MACD死叉
		if ctx.MACD < ctx.MACDSignal && ctx.MACDHist < 0 {
			if c.Direction == "long" {
				return true, fmt.Sprintf("%s MACD已死叉(MACD=%.4f < Signal=%.4f)，多单计划已失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	} else if cond.Direction == "UP" {
		// MACD金叉
		if ctx.MACD > ctx.MACDSignal && ctx.MACDHist > 0 {
			if c.Direction == "short" {
				return true, fmt.Sprintf("%s MACD已金叉(MACD=%.4f > Signal=%.4f)，空单计划已失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	}

	return false, ""
}

// checkTrendReversal 检查趋势反转（开仓前）
func (c *PreOpenInvalidationChecker) checkTrendReversal(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 {
		return false, ""
	}

	// ADX > 25 表示有明确趋势
	if ctx.ADX14 > 25 {
		if c.Direction == "long" && ctx.DIMinus > ctx.DIPlus {
			return true, fmt.Sprintf("%s 趋势已反转: DI-(%.1f) > DI+(%.1f)，多单计划已失效",
				cond.Timeframe, ctx.DIMinus, ctx.DIPlus)
		}
		if c.Direction == "short" && ctx.DIPlus > ctx.DIMinus {
			return true, fmt.Sprintf("%s 趋势已反转: DI+(%.1f) > DI-(%.1f)，空单计划已失效",
				cond.Timeframe, ctx.DIPlus, ctx.DIMinus)
		}
	}

	return false, ""
}

// getEMAValue 获取EMA值（开仓前检查器）
func (c *PreOpenInvalidationChecker) getEMAValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	default:
		return 0
	}
}

// getIndicatorValue 获取指标值（开仓前检查器）
func (c *PreOpenInvalidationChecker) getIndicatorValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	case "VWAP":
		return ctx.VWAP
	case "BB_UPPER":
		return ctx.BBUpper
	case "BB_LOWER":
		return ctx.BBLower
	default:
		return 0
	}
}

// CheckPreOpenInvalidation 开仓前综合检查失效条件
// 返回: (是否已失效, 失效原因)
func CheckPreOpenInvalidation(d *Decision, marketData *market.Data) (bool, string) {
	if marketData == nil {
		return false, ""
	}

	direction := "long"
	if d.Action == "open_short" {
		direction = "short"
	}

	checker := &PreOpenInvalidationChecker{
		Symbol:       d.Symbol,
		Direction:    direction,
		MarketData:   marketData,
		CurrentPrice: marketData.CurrentPrice,
	}

	// 1. 检查失效价格
	if invalidated, reason := checker.CheckInvalidationPrice(d.InvalidationPrice); invalidated {
		return true, reason
	}

	// 2. 检查失效条件
	if invalidated, reason := checker.CheckInvalidationCondition(d.InvalidationCondition); invalidated {
		return true, reason
	}

	return false, ""
}

// ============================================================================
// 持仓评估器
// ============================================================================

// PositionEvaluator 持仓评估器
type PositionEvaluator struct {
	Position   *PositionInfo
	Plan       *TradePlan
	MarketData *market.Data
	Symbol     string // 🆕 新增: 保存symbol用于更新
}

// EvaluationResult 评估结果
type EvaluationResult struct {
	Action            string
	Reason            string
	NewStopLoss       float64
	NewTakeProfit     float64 // 🆕 新增
	ClosePercentage   float64
	IsHardStop        bool
	IsPlanInvalidated bool
	TrancheIndex      int  // 🆕 新增: 分批止盈档位索引
	ShouldUpdatePeak  bool // 🆕 新增: 是否需要更新峰值
}

// Evaluate 评估持仓
func (e *PositionEvaluator) Evaluate() *EvaluationResult {
	result := &EvaluationResult{
		Action:           "hold",
		Reason:           "继续持有",
		ShouldUpdatePeak: true, // 默认需要更新峰值
		TrancheIndex:     -1,
	}

	if e.Position == nil || e.MarketData == nil {
		return result
	}

	currentPrice := e.MarketData.CurrentPrice
	holdingMinutes := e.getHoldingMinutes()

	// ========================================================================
	// 第一优先级：硬性止损检查
	// ========================================================================
	if e.Plan != nil {
		effectiveSL := e.getEffectiveStopLoss()

		if e.Plan.Direction == "long" && currentPrice <= effectiveSL {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🛑 触发止损: 当前价%.4f <= 止损价%.4f", currentPrice, effectiveSL),
				IsHardStop: true,
			}
		}
		if e.Plan.Direction == "short" && currentPrice >= effectiveSL {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🛑 触发止损: 当前价%.4f >= 止损价%.4f", currentPrice, effectiveSL),
				IsHardStop: true,
			}
		}
	}

	// ========================================================================
	// 第二优先级：固定止盈检查
	// ========================================================================
	if e.Plan != nil && e.Plan.TakeProfit > 0 {
		if e.Plan.Direction == "long" && currentPrice >= e.Plan.TakeProfit {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🎯 触发止盈: 当前价%.4f >= 止盈价%.4f", currentPrice, e.Plan.TakeProfit),
				IsHardStop: true,
			}
		}
		if e.Plan.Direction == "short" && currentPrice <= e.Plan.TakeProfit {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("🎯 触发止盈: 当前价%.4f <= 止盈价%.4f", currentPrice, e.Plan.TakeProfit),
				IsHardStop: true,
			}
		}
	}

	// ========================================================================
	// 第三优先级：最小持仓时间保护
	// ========================================================================
	minHoldMinutes := 30
	if e.Plan != nil && e.Plan.MinHoldMinutes > 0 {
		minHoldMinutes = e.Plan.MinHoldMinutes
	}

	if holdingMinutes < int64(minHoldMinutes) {
		// 保护期内只有极端亏损才平仓
		if e.Position.UnrealizedPnLPct < -3.0 {
			return &EvaluationResult{
				Action:     "close",
				Reason:     fmt.Sprintf("⚠️ 保护期内极端亏损(%.2f%% < -3%%)，紧急平仓", e.Position.UnrealizedPnLPct),
				IsHardStop: true,
			}
		}
		result.Reason = fmt.Sprintf("📋 持仓保护期(%d/%d分钟)，继续持有", holdingMinutes, minHoldMinutes)
		return result
	}

	// ========================================================================
	// 第四优先级：利润保护机制（防止大幅回撤）
	// ========================================================================
	tpConfig := GetTakeProfitConfig()
	if tpConfig.EnableProfitProtect && e.Plan != nil {
		if protectResult := e.checkProfitProtectionFixed(tpConfig); protectResult != nil {
			return protectResult
		}
	}

	// ========================================================================
	// 第五优先级：ATR跟踪止盈
	// ========================================================================
	if tpConfig.EnableATRTrailing && e.Position.UnrealizedPnLPct > 5.0 && e.Plan != nil {
		if atrResult := e.evaluateATRTrailingTakeProfitFixed(tpConfig); atrResult != nil {
			return atrResult
		}
	}

	// ========================================================================
	// 第六优先级：智能分批止盈
	// ========================================================================
	if tpConfig.EnableScaledExit && e.Plan != nil && e.Position.UnrealizedPnLPct > 0 {
		if scaledResult := e.evaluateScaledExitFixed(); scaledResult != nil {
			return scaledResult
		}
	}

	// ========================================================================
	// 第七优先级：移动止损（保护盈利）
	// ========================================================================
	if e.Position.UnrealizedPnLPct > 0 && e.Plan != nil {
		if trailingResult := e.evaluateTrailingStop(); trailingResult != nil {
			return trailingResult
		}
	}

	// ========================================================================
	// 第八优先级：动态止盈调整
	// ========================================================================
	if tpConfig.EnableDynamicTP && e.Plan != nil {
		if newTP := e.calculateDynamicTakeProfit(tpConfig); newTP > 0 {
			result.NewTakeProfit = newTP
		}
	}

	// ========================================================================
	// 第九优先级：计划失效条件检查
	// ========================================================================
	if holdingMinutes >= 60 && e.Plan != nil {
		if invalidated, reason := e.checkPlanInvalidation(); invalidated {
			return &EvaluationResult{
				Action:            "close",
				Reason:            reason,
				IsPlanInvalidated: true,
			}
		}
	}

	return result
}

// getHoldingMinutes 获取持仓时长
func (e *PositionEvaluator) getHoldingMinutes() int64 {
	if e.Position.UpdateTime <= 0 {
		return 0
	}
	return (time.Now().UnixMilli() - e.Position.UpdateTime) / (1000 * 60)
}

// calculateTrailingStop 计算移动止损（增强版：基于ATR动态计算）
func (e *PositionEvaluator) calculateTrailingStop() float64 {
	if e.Plan == nil || e.MarketData == nil {
		return 0
	}

	pnlPct := e.Position.UnrealizedPnLPct
	entryPrice := e.Plan.EntryPrice
	currentPrice := e.MarketData.CurrentPrice

	// 获取ATR用于动态计算安全边际
	atr := e.getATR()

	// 安全边际：至少0.3%或0.5倍ATR，取较大值
	safetyMarginPct := 0.003
	safetyMarginATR := 0.5 * atr / currentPrice
	safetyMargin := math.Max(safetyMarginPct, safetyMarginATR)

	var newSL float64
	var targetSLReason string

	if e.Plan.Direction == "long" {
		// 多单移动止损逻辑
		if pnlPct >= 20 {
			newSL = entryPrice * 1.10 // 保护10%利润
			targetSLReason = "保护10%利润"
		} else if pnlPct >= 15 {
			newSL = entryPrice * 1.05 // 保护5%利润
			targetSLReason = "保护5%利润"
		} else if pnlPct >= 10 {
			newSL = entryPrice * 1.02 // 保护2%利润
			targetSLReason = "保护2%利润"
		} else if pnlPct >= 7 {
			newSL = entryPrice // 保本
			targetSLReason = "保本"
		} else {
			return 0 // 盈利不足，不移动止损
		}

		// 计算允许的最大止损价格（留出安全边际）
		maxAllowedSL := currentPrice * (1 - safetyMargin)

		if newSL >= maxAllowedSL {
			effectiveSL := e.getEffectiveStopLoss()
			if maxAllowedSL > effectiveSL {
				log.Printf("  ⚠️ %s: %s止损 %.4f 高于安全线 %.4f，调整为 %.4f",
					e.Plan.Symbol, targetSLReason, newSL, maxAllowedSL, maxAllowedSL)
				newSL = maxAllowedSL
			} else {
				return 0 // 无法有效更新
			}
		}

	} else {
		// 空单移动止损逻辑
		if pnlPct >= 20 {
			newSL = entryPrice * 0.90 // 保护10%利润
			targetSLReason = "保护10%利润"
		} else if pnlPct >= 15 {
			newSL = entryPrice * 0.95 // 保护5%利润
			targetSLReason = "保护5%利润"
		} else if pnlPct >= 10 {
			newSL = entryPrice * 0.98 // 保护2%利润
			targetSLReason = "保护2%利润"
		} else if pnlPct >= 7 {
			newSL = entryPrice // 保本
			targetSLReason = "保本"
		} else {
			return 0
		}

		// 计算允许的最小止损价格（留出安全边际）
		minAllowedSL := currentPrice * (1 + safetyMargin)

		if newSL <= minAllowedSL {
			effectiveSL := e.getEffectiveStopLoss()
			if minAllowedSL < effectiveSL {
				log.Printf("  ⚠️ %s: %s止损 %.4f 低于安全线 %.4f，调整为 %.4f",
					e.Plan.Symbol, targetSLReason, newSL, minAllowedSL, minAllowedSL)
				newSL = minAllowedSL
			} else {
				return 0 // 无法有效更新
			}
		}
	}

	return newSL
}

// checkPlanInvalidation 检查计划是否失效
func (e *PositionEvaluator) checkPlanInvalidation() (bool, string) {
	if e.MarketData == nil {
		return false, ""
	}
	// ========== 1. 检查结构化失效条件 ==========
	if e.Plan.ParsedInvalidationCondition != nil && e.Plan.ParsedInvalidationCondition.IsValid {
		if invalidated, reason := e.checkParsedInvalidationCondition(); invalidated {
			return true, reason
		}
	} else if e.Plan.InvalidationCondition != "" {
		// 尝试解析并检查
		parsed := ParseInvalidationCondition(e.Plan.InvalidationCondition)
		if parsed.IsValid {
			e.Plan.ParsedInvalidationCondition = parsed
			if invalidated, reason := e.checkParsedInvalidationCondition(); invalidated {
				return true, reason
			}
		}
	}

	// ========== 2. 默认的趋势反转检查 ==========
	if market.Is4HTrendReversed(e.MarketData, e.Plan.Direction) {
		adx, diPlus, diMinus := market.GetTrendInfo(e.MarketData)

		if e.Plan.Direction == "long" {
			return true, fmt.Sprintf("4H趋势反转(ADX=%.1f, DI-=%.1f > DI+=%.1f)，计划失效",
				adx, diMinus, diPlus)
		} else {
			return true, fmt.Sprintf("4H趋势反转(ADX=%.1f, DI+=%.1f > DI-=%.1f)，计划失效",
				adx, diPlus, diMinus)
		}
	}

	// ========== 3. EMA交叉检查 ==========
	if e.MarketData.LongerTermContext != nil {
		ctx := e.MarketData.LongerTermContext
		if e.Plan.Direction == "long" && ctx.EMA20 < ctx.EMA50 {
			return true, fmt.Sprintf("4H EMA死叉(EMA20=%.2f < EMA50=%.2f)，计划失效",
				ctx.EMA20, ctx.EMA50)
		}
		if e.Plan.Direction == "short" && ctx.EMA20 > ctx.EMA50 {
			return true, fmt.Sprintf("4H EMA金叉(EMA20=%.2f > EMA50=%.2f)，计划失效",
				ctx.EMA20, ctx.EMA50)
		}
	}

	// ========== 4. 价格失效线检查 ==========
	if e.Plan.InvalidationPrice > 0 {
		currentPrice := e.MarketData.CurrentPrice
		if e.Plan.Direction == "long" && currentPrice < e.Plan.InvalidationPrice {
			return true, fmt.Sprintf("价格跌破失效线(%.4f < %.4f)，计划失效",
				currentPrice, e.Plan.InvalidationPrice)
		}
		if e.Plan.Direction == "short" && currentPrice > e.Plan.InvalidationPrice {
			return true, fmt.Sprintf("价格突破失效线(%.4f > %.4f)，计划失效",
				currentPrice, e.Plan.InvalidationPrice)
		}
	}

	return false, ""
}

// checkParsedInvalidationCondition 检查解析后的失效条件
func (e *PositionEvaluator) checkParsedInvalidationCondition() (bool, string) {
	cond := e.Plan.ParsedInvalidationCondition
	if cond == nil || !cond.IsValid {
		return false, ""
	}

	// 获取对应时间框架的数据
	ctx := e.getContextForTimeframe(cond.Timeframe)
	if ctx == nil {
		log.Printf("⚠️ 无法获取 %s 时间框架数据", cond.Timeframe)
		return false, ""
	}

	switch cond.Type {
	case ICT_EMA_CROSS_DOWN:
		return e.checkEMACrossDown(ctx, cond)

	case ICT_EMA_CROSS_UP:
		return e.checkEMACrossUp(ctx, cond)

	case ICT_PRICE_BELOW:
		return e.checkPriceBelow(ctx, cond)

	case ICT_PRICE_ABOVE:
		return e.checkPriceAbove(ctx, cond)

	case ICT_RSI_ABOVE:
		return e.checkRSIAbove(ctx, cond)

	case ICT_RSI_BELOW:
		return e.checkRSIBelow(ctx, cond)

	case ICT_ADX_BELOW:
		return e.checkADXBelow(ctx, cond)

	case ICT_MACD_CROSS:
		return e.checkMACDCross(ctx, cond)

	case ICT_TREND_REVERSAL:
		return e.checkTrendReversal(ctx, cond)

	default:
		log.Printf("⚠️ 未知的失效条件类型: %s", cond.Type)
		return false, ""
	}
}

// InvalidationCheckContext 失效条件检查上下文
type InvalidationCheckContext struct {
	EMA20        float64
	EMA50        float64
	RSI14        float64
	ADX14        float64
	DIPlus       float64
	DIMinus      float64
	MACD         float64
	MACDSignal   float64
	MACDHist     float64
	CurrentPrice float64
	VWAP         float64
	BBUpper      float64
	BBLower      float64
	Timeframe    string
}

// getContextForTimeframe 获取指定时间框架的上下文
func (e *PositionEvaluator) getContextForTimeframe(timeframe string) *InvalidationCheckContext {
	if e.MarketData == nil {
		return nil
	}

	ctx := &InvalidationCheckContext{
		CurrentPrice: e.MarketData.CurrentPrice,
		Timeframe:    timeframe,
	}

	// 根据时间框架选择数据源
	switch strings.ToUpper(timeframe) {
	case "4H":
		if e.MarketData.LongerTermContext != nil {
			ltc := e.MarketData.LongerTermContext
			ctx.EMA20 = ltc.EMA20
			ctx.EMA50 = ltc.EMA50
			ctx.BBUpper = ltc.BollingerUpper
			ctx.BBLower = ltc.BollingerLower

			// 从切片获取最新值
			ctx.RSI14 = market.GetLastValue(ltc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(ltc.ADXValues)
			ctx.DIPlus = market.GetLastValue(ltc.DIPlus)
			ctx.DIMinus = market.GetLastValue(ltc.DIMinus)
			ctx.MACD = market.GetLastValue(ltc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(ltc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(ltc.MACDHist)
		}

	case "1H":
		if e.MarketData.MidTermSeries1h != nil {
			mtc := e.MarketData.MidTermSeries1h
			// 从切片获取最新值
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			// 注意：MidTermData1h 没有 DIPlus/DIMinus，使用顶层数据
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
		} else {
			// 回退到顶层 Current* 数据
			ctx.EMA20 = e.MarketData.CurrentEMA20
			ctx.EMA50 = e.MarketData.CurrentEMA50
			ctx.RSI14 = e.MarketData.CurrentRSI14
			ctx.ADX14 = e.MarketData.CurrentADX
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
			ctx.MACD = e.MarketData.CurrentMACD
		}

	case "15M", "30M":
		if e.MarketData.MidTermSeries15m != nil {
			mtc := e.MarketData.MidTermSeries15m
			// 从切片获取最新值
			ctx.EMA20 = market.GetLastValue(mtc.EMA20Values)
			ctx.EMA50 = market.GetLastValue(mtc.EMA50Values)
			ctx.RSI14 = market.GetLastValue(mtc.RSI14Values)
			ctx.ADX14 = market.GetLastValue(mtc.ADXValues)
			ctx.MACD = market.GetLastValue(mtc.MACDValues)
			ctx.MACDSignal = market.GetLastValue(mtc.MACDSignal)
			ctx.MACDHist = market.GetLastValue(mtc.MACDHist)
			// MidTermData15m 没有 DIPlus/DIMinus，使用顶层数据
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
		} else {
			// 回退到顶层数据
			ctx.EMA20 = e.MarketData.CurrentEMA20
			ctx.EMA50 = e.MarketData.CurrentEMA50
			ctx.RSI14 = e.MarketData.CurrentRSI14
			ctx.ADX14 = e.MarketData.CurrentADX
			ctx.DIPlus = e.MarketData.CurrentDIPlus
			ctx.DIMinus = e.MarketData.CurrentDIMinus
			ctx.MACD = e.MarketData.CurrentMACD
		}

	default:
		// 默认使用顶层 Current* 数据（基于3分钟最新数据计算）
		ctx.EMA20 = e.MarketData.CurrentEMA20
		ctx.EMA50 = e.MarketData.CurrentEMA50
		ctx.RSI14 = e.MarketData.CurrentRSI14
		ctx.ADX14 = e.MarketData.CurrentADX
		ctx.DIPlus = e.MarketData.CurrentDIPlus
		ctx.DIMinus = e.MarketData.CurrentDIMinus
		ctx.MACD = e.MarketData.CurrentMACD
		// 尝试从 IntradaySeries 获取 MACD 信号线和柱状图
		if e.MarketData.IntradaySeries != nil {
			ctx.MACDSignal = market.GetLastValue(e.MarketData.IntradaySeries.MACDSignal)
			ctx.MACDHist = market.GetLastValue(e.MarketData.IntradaySeries.MACDHist)
		}
	}

	return ctx
}

// checkEMACrossDown 检查EMA死叉
func (e *PositionEvaluator) checkEMACrossDown(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := e.getEMAValue(ctx, cond.Indicator)
	ema2 := e.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 检查是否死叉（短期EMA低于长期EMA）
	if ema1 < ema2 {
		// 对于多单，EMA死叉是失效信号
		if e.Plan.Direction == "long" {
			return true, fmt.Sprintf("%s EMA死叉: %s(%.4f) < %s(%.4f)，计划失效",
				cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
		}
	}

	return false, ""
}

// checkEMACrossUp 检查EMA金叉
func (e *PositionEvaluator) checkEMACrossUp(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	ema1 := e.getEMAValue(ctx, cond.Indicator)
	ema2 := e.getEMAValue(ctx, cond.Indicator2)

	if ema1 == 0 || ema2 == 0 {
		return false, ""
	}

	// 检查是否金叉（短期EMA高于长期EMA）
	if ema1 > ema2 {
		// 对于空单，EMA金叉是失效信号
		if e.Plan.Direction == "short" {
			return true, fmt.Sprintf("%s EMA金叉: %s(%.4f) > %s(%.4f)，计划失效",
				cond.Timeframe, cond.Indicator, ema1, cond.Indicator2, ema2)
		}
	}

	return false, ""
}

// checkPriceBelow 检查价格跌破
func (e *PositionEvaluator) checkPriceBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = e.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	if ctx.CurrentPrice < targetPrice {
		// 对于多单，价格跌破是失效信号
		if e.Plan.Direction == "long" {
			indicator := cond.Indicator
			if cond.Threshold > 0 {
				indicator = fmt.Sprintf("%.4f", cond.Threshold)
			}
			return true, fmt.Sprintf("%s 价格(%.4f)跌破%s(%.4f)，计划失效",
				cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
		}
	}

	return false, ""
}

// checkPriceAbove 检查价格突破
func (e *PositionEvaluator) checkPriceAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	var targetPrice float64

	if cond.Threshold > 0 {
		targetPrice = cond.Threshold
	} else {
		targetPrice = e.getIndicatorValue(ctx, cond.Indicator)
	}

	if targetPrice == 0 {
		return false, ""
	}

	if ctx.CurrentPrice > targetPrice {
		// 对于空单，价格突破是失效信号
		if e.Plan.Direction == "short" {
			indicator := cond.Indicator
			if cond.Threshold > 0 {
				indicator = fmt.Sprintf("%.4f", cond.Threshold)
			}
			return true, fmt.Sprintf("%s 价格(%.4f)突破%s(%.4f)，计划失效",
				cond.Timeframe, ctx.CurrentPrice, indicator, targetPrice)
		}
	}

	return false, ""
}

// checkRSIAbove 检查RSI超过阈值
func (e *PositionEvaluator) checkRSIAbove(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 > cond.Threshold {
		// RSI超买，对空单可能是失效信号
		if e.Plan.Direction == "short" && cond.Threshold >= 70 {
			return true, fmt.Sprintf("%s RSI(%.1f) > %.0f 超买，计划失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 也可用于多单的获利了结信号
		if e.Plan.Direction == "long" && cond.Threshold >= 80 {
			return true, fmt.Sprintf("%s RSI(%.1f) > %.0f 极度超买，建议获利了结",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
	}

	return false, ""
}

// checkRSIBelow 检查RSI低于阈值
func (e *PositionEvaluator) checkRSIBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.RSI14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.RSI14 < cond.Threshold {
		// RSI超卖，对多单可能是失效信号
		if e.Plan.Direction == "long" && cond.Threshold <= 30 {
			return true, fmt.Sprintf("%s RSI(%.1f) < %.0f 超卖，计划失效",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
		// 也可用于空单的获利了结信号
		if e.Plan.Direction == "short" && cond.Threshold <= 20 {
			return true, fmt.Sprintf("%s RSI(%.1f) < %.0f 极度超卖，建议获利了结",
				cond.Timeframe, ctx.RSI14, cond.Threshold)
		}
	}

	return false, ""
}

// checkADXBelow 检查ADX低于阈值（趋势减弱）
func (e *PositionEvaluator) checkADXBelow(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 || cond.Threshold == 0 {
		return false, ""
	}

	if ctx.ADX14 < cond.Threshold {
		return true, fmt.Sprintf("%s ADX(%.1f) < %.0f 趋势减弱，计划失效",
			cond.Timeframe, ctx.ADX14, cond.Threshold)
	}

	return false, ""
}

// checkMACDCross 检查MACD交叉
func (e *PositionEvaluator) checkMACDCross(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.MACD == 0 && ctx.MACDSignal == 0 {
		return false, ""
	}

	if cond.Direction == "DOWN" {
		// MACD死叉：MACD线下穿信号线
		if ctx.MACD < ctx.MACDSignal && ctx.MACDHist < 0 {
			if e.Plan.Direction == "long" {
				return true, fmt.Sprintf("%s MACD死叉(MACD=%.4f < Signal=%.4f)，计划失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	} else if cond.Direction == "UP" {
		// MACD金叉：MACD线上穿信号线
		if ctx.MACD > ctx.MACDSignal && ctx.MACDHist > 0 {
			if e.Plan.Direction == "short" {
				return true, fmt.Sprintf("%s MACD金叉(MACD=%.4f > Signal=%.4f)，计划失效",
					cond.Timeframe, ctx.MACD, ctx.MACDSignal)
			}
		}
	}

	return false, ""
}

// checkTrendReversal 检查趋势反转
func (e *PositionEvaluator) checkTrendReversal(ctx *InvalidationCheckContext, cond *ParsedInvalidationCondition) (bool, string) {
	if ctx.ADX14 == 0 {
		return false, ""
	}

	// ADX > 25 表示有明确趋势
	if ctx.ADX14 > 25 {
		if e.Plan.Direction == "long" && ctx.DIMinus > ctx.DIPlus {
			return true, fmt.Sprintf("%s 趋势反转: DI-(%.1f) > DI+(%.1f)，计划失效",
				cond.Timeframe, ctx.DIMinus, ctx.DIPlus)
		}
		if e.Plan.Direction == "short" && ctx.DIPlus > ctx.DIMinus {
			return true, fmt.Sprintf("%s 趋势反转: DI+(%.1f) > DI-(%.1f)，计划失效",
				cond.Timeframe, ctx.DIPlus, ctx.DIMinus)
		}
	}

	return false, ""
}

// getEMAValue 获取EMA值
func (e *PositionEvaluator) getEMAValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	default:
		// 尝试解析EMAxx格式
		if strings.HasPrefix(indicator, "EMA") {
			// 如果是其他EMA，返回0表示不支持
			log.Printf("⚠️ 不支持的EMA指标: %s", indicator)
		}
		return 0
	}
}

// getIndicatorValue 获取指标值
func (e *PositionEvaluator) getIndicatorValue(ctx *InvalidationCheckContext, indicator string) float64 {
	indicator = strings.ToUpper(indicator)
	switch indicator {
	case "EMA20":
		return ctx.EMA20
	case "EMA50":
		return ctx.EMA50
	case "VWAP":
		return ctx.VWAP
	case "BB_UPPER":
		return ctx.BBUpper
	case "BB_LOWER":
		return ctx.BBLower
	default:
		return 0
	}
}

// ============================================================================
// Context 交易上下文
// ============================================================================

// Context 交易上下文
type Context struct {
	CurrentTime         string                      `json:"current_time"`
	RuntimeMinutes      int                         `json:"runtime_minutes"`
	CallCount           int                         `json:"call_count"`
	Account             AccountInfo                 `json:"account"`
	Positions           []PositionInfo              `json:"positions"`
	CandidateCoins      []CandidateCoin             `json:"candidate_coins"`
	MarketDataMap       map[string]*market.Data     `json:"-"`
	OITopDataMap        map[string]*OITopData       `json:"-"`
	CorrelationMap      map[string]*CorrelationData `json:"-"`
	CircuitBreaker      *CircuitBreakerState        `json:"-"`
	Performance         interface{}                 `json:"-"`
	BTCETHLeverage      int                         `json:"-"`
	AltcoinLeverage     int                         `json:"-"`
	MaxRiskPerTrade     float64                     `json:"-"`
	TotalRiskBudget     float64                     `json:"-"`
	LastAnalysisTime    time.Time                   `json:"-"`
	AnalysisIntervalMin int                         `json:"-"`
}

// Decision AI的交易决策
type Decision struct {
	Symbol                string  `json:"symbol"`
	Action                string  `json:"action"`
	Leverage              int     `json:"leverage,omitempty"`
	PositionSizeUSD       float64 `json:"position_size_usd,omitempty"`
	StopLoss              float64 `json:"stop_loss,omitempty"`
	TakeProfit            float64 `json:"take_profit,omitempty"`
	NewStopLoss           float64 `json:"new_stop_loss,omitempty"`
	NewTakeProfit         float64 `json:"new_take_profit,omitempty"`
	ClosePercentage       float64 `json:"close_percentage,omitempty"`
	Confidence            int     `json:"confidence,omitempty"`
	RiskUSD               float64 `json:"risk_usd,omitempty"`
	Reasoning             string  `json:"reasoning"`
	InvalidationPrice     float64 `json:"invalidation_price,omitempty"`
	InvalidationCondition string  `json:"invalidation_condition,omitempty"`
	MinHoldMinutes        int     `json:"min_hold_minutes,omitempty"`
	TrancheIndex          int     `json:"tranche_index,omitempty"` // 🆕 分批止盈档位
}

// FullDecision AI的完整决策
type FullDecision struct {
	UserPrompt string     `json:"user_prompt"`
	CoTTrace   string     `json:"cot_trace"`
	Decisions  []Decision `json:"decisions"`
	Timestamp  time.Time  `json:"timestamp"`
}

// ============================================================================
// 核心决策函数
// ============================================================================

// GetFullDecision 获取AI的完整交易决策
func GetFullDecision(ctx *Context, mcpClient *mcp.Client) (*FullDecision, error) {
	initializeDefaults(ctx)

	if result := checkCircuitBreaker(ctx); result != nil {
		return result, nil
	}

	if err := fetchMarketDataForContext(ctx); err != nil {
		return nil, fmt.Errorf("获取市场数据失败: %w", err)
	}

	if shouldTriggerCircuitBreaker(ctx) {
		return &FullDecision{
			CoTTrace: "🛑 触发熔断保护，暂停交易",
			Decisions: []Decision{{
				Symbol:    "ALL",
				Action:    "wait",
				Reasoning: ctx.CircuitBreaker.TriggerReason,
			}},
			Timestamp: time.Now(),
		}, nil
	}

	calculateCorrelationMatrix(ctx)

	// 评估现有持仓
	positionDecisions := evaluateExistingPositions(ctx)

	shouldCallAI := shouldCallAIForNewOpportunities(ctx)

	var aiDecisions []Decision
	var cotTrace string

	if shouldCallAI {
		remainingBudget := calculateRemainingRiskBudget(ctx)
		if remainingBudget <= 0 {
			log.Printf("⚠️ 风险预算已用尽(剩余%.2f%%)，跳过新机会搜索", remainingBudget*100)
		} else {
			systemPrompt := buildSystemPromptOptimized(ctx)
			userPrompt := buildUserPromptOptimized(ctx, remainingBudget)

			aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
			if err != nil {
				log.Printf("⚠️ 调用AI API失败: %v", err)
			} else {
				aiDecisions, cotTrace, _ = ExtractDecisionsRobust(aiResponse)

				var validDecisions []Decision
				for _, d := range aiDecisions {
					if d.Action == "open_long" || d.Action == "open_short" {
						if err := validateOpenDecision(&d, ctx); err != nil {
							log.Printf("⚠️ 开仓决策验证失败: %v", err)
							continue
						}
						validDecisions = append(validDecisions, d)
					} else if d.Action == "wait" {
						validDecisions = append(validDecisions, d)
					}
				}
				aiDecisions = validDecisions
			}
		}

		ctx.LastAnalysisTime = time.Now()
	}

	allDecisions := mergeDecisions(positionDecisions, aiDecisions)

	if err := validateFinalDecisions(allDecisions, ctx); err != nil {
		log.Printf("⚠️ 决策验证警告: %v", err)
	}

	// 🆕 统一生成完整的 CoTTrace（包含所有决策来源）
	finalCoTTrace := buildFinalCoTTrace(cotTrace, positionDecisions, aiDecisions, allDecisions)

	return &FullDecision{
		CoTTrace:  finalCoTTrace,
		Decisions: allDecisions,
		Timestamp: time.Now(),
	}, nil
}

// 🆕 新增：构建完整的思维链（统一处理所有决策来源）
func buildFinalCoTTrace(aiCotTrace string, positionDecisions, aiDecisions, allDecisions []Decision) string {
	var sb strings.Builder

	// 1. 如果有AI分析，先添加AI的思维链
	if aiCotTrace != "" {
		sb.WriteString(aiCotTrace)
		sb.WriteString("\n\n")
	}

	// 2. 如果有持仓管理决策，添加说明
	if len(positionDecisions) > 0 {
		hasNonHold := false
		for _, d := range positionDecisions {
			if d.Action != "hold" {
				hasNonHold = true
				break
			}
		}

		if hasNonHold {
			sb.WriteString("**📊 持仓管理决策**:\n")
			for _, d := range positionDecisions {
				if d.Action == "hold" {
					continue
				}
				sb.WriteString(fmt.Sprintf("- %s: %s - %s\n", d.Symbol, d.Action, d.Reasoning))
			}
			sb.WriteString("\n")
		}
	}

	// 3. 添加最终决策的JSON格式
	if len(allDecisions) > 0 {
		sb.WriteString("**📋 决策JSON**:\n```json\n")
		jsonBytes, err := json.MarshalIndent(allDecisions, "", "  ")
		if err == nil {
			sb.WriteString(string(jsonBytes))
		}
		sb.WriteString("\n```")
	}

	result := sb.String()
	if result == "" {
		return "无决策输出"
	}

	return result
}

// initializeDefaults 初始化默认参数
func initializeDefaults(ctx *Context) {
	if ctx.MaxRiskPerTrade == 0 {
		ctx.MaxRiskPerTrade = 0.02
	}
	if ctx.TotalRiskBudget == 0 {
		ctx.TotalRiskBudget = 0.08
	}
	if ctx.AnalysisIntervalMin == 0 {
		ctx.AnalysisIntervalMin = 15
	}
}

// checkCircuitBreaker 检查熔断状态
func checkCircuitBreaker(ctx *Context) *FullDecision {
	if ctx.CircuitBreaker != nil && ctx.CircuitBreaker.IsTriggered {
		cooldownEnd := ctx.CircuitBreaker.TriggerTime.Add(
			time.Duration(ctx.CircuitBreaker.CooldownMinutes) * time.Minute)
		if time.Now().Before(cooldownEnd) {
			remainingMinutes := int(cooldownEnd.Sub(time.Now()).Minutes())
			return &FullDecision{
				CoTTrace: fmt.Sprintf("⚠️ 熔断中: %s | 剩余冷却时间: %d分钟",
					ctx.CircuitBreaker.TriggerReason, remainingMinutes),
				Decisions: []Decision{{
					Symbol:    "ALL",
					Action:    "wait",
					Reasoning: fmt.Sprintf("熔断保护触发: %s", ctx.CircuitBreaker.TriggerReason),
				}},
				Timestamp: time.Now(),
			}
		}
		ctx.CircuitBreaker.IsTriggered = false
	}
	return nil
}

// evaluateExistingPositions 基于计划评估现有持仓
func evaluateExistingPositions(ctx *Context) []Decision {
	var decisions []Decision

	for _, pos := range ctx.Positions {
		plan := planManager.GetPlan(pos.Symbol)
		marketData := ctx.MarketDataMap[pos.Symbol]

		evaluator := &PositionEvaluator{
			Position:   &pos,
			Plan:       plan,
			MarketData: marketData,
			Symbol:     pos.Symbol, // 🔧 保存symbol
		}

		result := evaluator.Evaluate()

		// 🔧 新增: 更新峰值数据（如果需要）
		if result.ShouldUpdatePeak && marketData != nil {
			planManager.UpdatePlanPeakData(pos.Symbol, marketData.CurrentPrice, pos.UnrealizedPnLPct)
		}

		// 🔧 新增: 更新入场ATR（首次）
		if plan != nil && plan.EntryATR == 0 && marketData != nil {
			atr := 0.0
			if marketData.LongerTermContext != nil {
				atr = marketData.LongerTermContext.ATR14
			}
			if atr > 0 {
				planManager.UpdatePlanEntryATR(pos.Symbol, atr)
			}
		}

		// 🔧 新增: 更新动态止盈
		if result.NewTakeProfit > 0 {
			planManager.UpdatePlanTakeProfit(pos.Symbol, result.NewTakeProfit)
		}

		switch result.Action {
		case "close":
			action := "close_long"
			if pos.Side == "short" {
				action = "close_short"
			}
			decisions = append(decisions, Decision{
				Symbol:    pos.Symbol,
				Action:    action,
				Reasoning: result.Reason,
			})
			if result.IsPlanInvalidated && plan != nil {
				planManager.UpdatePlan(pos.Symbol, func(p *TradePlan) {
					p.Status = "INVALIDATED"
				})
			}
			planManager.RemovePlan(pos.Symbol)

		case "partial_close":
			decisions = append(decisions, Decision{
				Symbol:          pos.Symbol,
				Action:          "partial_close",
				ClosePercentage: result.ClosePercentage,
				NewStopLoss:     result.NewStopLoss,
				Reasoning:       result.Reason,
			})

			// 🔧 修复: 使用档位索引标记已执行
			if result.TrancheIndex >= 0 {
				planManager.MarkTrancheExecuted(pos.Symbol, result.TrancheIndex, result.ClosePercentage)
			}

			// 🔧 同时更新止损
			if result.NewStopLoss > 0 {
				planManager.UpdatePlanStopLoss(pos.Symbol, result.NewStopLoss)
			}

			planManager.autoSaveIfEnabled()

		case "update_stop_loss":
			decisions = append(decisions, Decision{
				Symbol:      pos.Symbol,
				Action:      "update_stop_loss",
				NewStopLoss: result.NewStopLoss,
				Reasoning:   result.Reason,
			})
			planManager.UpdatePlanStopLoss(pos.Symbol, result.NewStopLoss)

		case "hold":
			decisions = append(decisions, Decision{
				Symbol:    pos.Symbol,
				Action:    "hold",
				Reasoning: result.Reason,
			})
		}
	}

	return decisions
}

// shouldCallAIForNewOpportunities 判断是否应该调用AI寻找新机会
func shouldCallAIForNewOpportunities(ctx *Context) bool {
	if !ctx.LastAnalysisTime.IsZero() {
		elapsed := time.Since(ctx.LastAnalysisTime).Minutes()
		if elapsed < float64(ctx.AnalysisIntervalMin) {
			log.Printf("📊 距离上次分析%.1f分钟，跳过AI调用(间隔%d分钟)", elapsed, ctx.AnalysisIntervalMin)
			return false
		}
	}

	if ctx.Account.PositionCount >= 3 {
		log.Printf("📊 持仓已满(%d/3)，跳过新机会搜索", ctx.Account.PositionCount)
		return false
	}

	remainingBudget := calculateRemainingRiskBudget(ctx)
	if remainingBudget <= 0.01 {
		log.Printf("📊 风险预算不足(剩余%.2f%%)，跳过新机会搜索", remainingBudget*100)
		return false
	}

	return true
}

// calculateRemainingRiskBudget 计算剩余风险预算
func calculateRemainingRiskBudget(ctx *Context) float64 {
	usedRisk := calculateUsedRisk(ctx)
	return ctx.TotalRiskBudget - usedRisk
}

// mergeDecisions 合并决策
func mergeDecisions(positionDecisions, aiDecisions []Decision) []Decision {
	decisionMap := make(map[string]Decision)

	for _, d := range positionDecisions {
		decisionMap[d.Symbol] = d
	}

	for _, d := range aiDecisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			if existing, exists := decisionMap[d.Symbol]; exists {
				if existing.Action == "hold" || existing.Action == "wait" {
					continue
				}
			}
			decisionMap[d.Symbol] = d
		} else if d.Action == "wait" && len(positionDecisions) == 0 {
			decisionMap[d.Symbol] = d
		}
	}

	var result []Decision
	for _, d := range decisionMap {
		result = append(result, d)
	}

	return result
}

// validateOpenDecision 验证开仓决策（🆕 增加失效条件预检查）
func validateOpenDecision(d *Decision, ctx *Context) error {
	// ========== 🆕 新增：开仓前失效条件预检查 ==========
	marketData, ok := ctx.MarketDataMap[d.Symbol]
	if !ok {
		return fmt.Errorf("缺少 %s 市场数据", d.Symbol)
	}

	// 🆕 检查失效条件是否已触发
	if invalidated, reason := CheckPreOpenInvalidation(d, marketData); invalidated {
		return fmt.Errorf("开仓前失效条件检查失败: %s", reason)
	}

	// ========== 以下为原有验证逻辑 ==========
	for _, pos := range ctx.Positions {
		if pos.Symbol == d.Symbol {
			return fmt.Errorf("%s 已有持仓，不能重复开仓", d.Symbol)
		}
	}

	remainingBudget := calculateRemainingRiskBudget(ctx)
	estimatedRisk := d.RiskUSD / ctx.Account.TotalEquity
	if estimatedRisk > remainingBudget {
		return fmt.Errorf("风险预算不足: 需要%.2f%%, 剩余%.2f%%", estimatedRisk*100, remainingBudget*100)
	}

	maxLeverage := ctx.AltcoinLeverage
	if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
		maxLeverage = ctx.BTCETHLeverage
	}
	if d.Leverage <= 0 || d.Leverage > maxLeverage {
		return fmt.Errorf("杠杆必须在1-%d之间: %d", maxLeverage, d.Leverage)
	}

	if d.PositionSizeUSD <= 0 {
		return fmt.Errorf("仓位大小必须>0")
	}

	maxPositionValue := ctx.Account.AvailableBalance * float64(maxLeverage) * 0.9
	if d.PositionSizeUSD > maxPositionValue {
		log.Printf("⚠️ 自动调整仓位: %.0f → %.0f USD", d.PositionSizeUSD, maxPositionValue*0.9)
		d.PositionSizeUSD = maxPositionValue * 0.9
	}

	if corr, ok := ctx.CorrelationMap[d.Symbol]; ok && corr.IsHighCorr {
		adjustedSize := d.PositionSizeUSD * corr.RiskWeight
		log.Printf("⚠️ 高相关性调整: %.0f → %.0f USD", d.PositionSizeUSD, adjustedSize)
		d.PositionSizeUSD = adjustedSize
	}

	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return fmt.Errorf("止损止盈必须>0")
	}

	marketData, ok = ctx.MarketDataMap[d.Symbol]
	if !ok {
		return fmt.Errorf("缺少 %s 市场数据", d.Symbol)
	}

	currentPrice := marketData.CurrentPrice
	var riskPct, rewardPct float64
	if d.Action == "open_long" {
		if d.StopLoss >= currentPrice || d.TakeProfit <= currentPrice {
			return fmt.Errorf("做多止损必须<当前价<止盈")
		}
		riskPct = (currentPrice - d.StopLoss) / currentPrice * 100
		rewardPct = (d.TakeProfit - currentPrice) / currentPrice * 100
	} else {
		if d.StopLoss <= currentPrice || d.TakeProfit >= currentPrice {
			return fmt.Errorf("做空止损必须>当前价>止盈")
		}
		riskPct = (d.StopLoss - currentPrice) / currentPrice * 100
		rewardPct = (currentPrice - d.TakeProfit) / currentPrice * 100
	}

	tradingCost := 0.2
	netRewardPct := rewardPct - tradingCost
	riskRewardRatio := netRewardPct / riskPct

	if riskRewardRatio < 2.5 {
		return fmt.Errorf("风险回报比过低(%.2f:1 < 2.5:1)", riskRewardRatio)
	}

	positionRiskUSD := d.PositionSizeUSD * (riskPct / 100)
	maxRiskUSD := ctx.Account.TotalEquity * ctx.MaxRiskPerTrade
	if positionRiskUSD > maxRiskUSD*1.01 {
		return fmt.Errorf("单笔风险(%.2f USD)超过上限(%.2f USD)", positionRiskUSD, maxRiskUSD)
	}

	d.RiskUSD = positionRiskUSD
	return nil
}

// validateFinalDecisions 验证最终决策
func validateFinalDecisions(decisions []Decision, ctx *Context) error {
	newPositions := 0
	for _, d := range decisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			newPositions++
		}
	}

	totalPositions := ctx.Account.PositionCount + newPositions
	if totalPositions > 3 {
		return fmt.Errorf("总持仓数量(%d)超过上限(3)", totalPositions)
	}

	return nil
}

// CreateTradePlanFromDecision 从决策创建交易计划
func CreateTradePlanFromDecision(d *Decision, actualEntryPrice float64) *TradePlan {
	direction := "long"
	if d.Action == "open_short" {
		direction = "short"
	}

	// 解析失效条件
	var parsedCondition *ParsedInvalidationCondition
	if d.InvalidationCondition != "" {
		parsedCondition = ParseInvalidationCondition(d.InvalidationCondition)
		if !parsedCondition.IsValid {
			log.Printf("⚠️ 失效条件解析失败: %s - %s", d.InvalidationCondition, parsedCondition.ParseError)
		}
	}

	//百分比方式调整入场价
	var adjustedSL, adjustedTP float64
	if direction == "long" {
		// 保持相同的止损百分比
		slPct := (d.StopLoss - actualEntryPrice) / actualEntryPrice
		tpPct := (d.TakeProfit - actualEntryPrice) / actualEntryPrice

		// 如果滑点导致入场价变化，按比例调整
		if actualEntryPrice != d.StopLoss && d.StopLoss > 0 {
			adjustedSL = actualEntryPrice * (1 + slPct)
			adjustedTP = actualEntryPrice * (1 + tpPct)
		} else {
			adjustedSL = d.StopLoss
			adjustedTP = d.TakeProfit
		}
	} else {
		slPct := (d.StopLoss - actualEntryPrice) / actualEntryPrice
		tpPct := (d.TakeProfit - actualEntryPrice) / actualEntryPrice
		adjustedSL = actualEntryPrice * (1 + slPct)
		adjustedTP = actualEntryPrice * (1 + tpPct)
	}

	// 使用原始值如果调整后的值无效
	if adjustedSL <= 0 {
		adjustedSL = d.StopLoss
	}
	if adjustedTP <= 0 {
		adjustedTP = d.TakeProfit
	}

	plan := &TradePlan{
		ID:                          fmt.Sprintf("%s_%d", d.Symbol, time.Now().UnixNano()),
		Symbol:                      d.Symbol,
		Direction:                   direction,
		EntryPrice:                  actualEntryPrice,
		ActualEntry:                 actualEntryPrice,
		StopLoss:                    adjustedSL,
		TakeProfit:                  adjustedTP,
		OriginalTakeProfit:          adjustedTP, // 🆕 记录原始止盈
		CurrentStopLoss:             adjustedSL,
		PositionSizeUSD:             d.PositionSizeUSD,
		Leverage:                    d.Leverage,
		EntryReason:                 d.Reasoning,
		InvalidationCondition:       d.InvalidationCondition,
		ParsedInvalidationCondition: parsedCondition,
		InvalidationPrice:           d.InvalidationPrice,
		MinHoldMinutes:              d.MinHoldMinutes,
		CreatedAt:                   time.Now(),
		Status:                      "ACTIVE",
		Confidence:                  d.Confidence,
		RiskUSD:                     d.RiskUSD,
		ExecutedTranches:            make(map[int]bool), // 🆕 初始化分批止盈记录
	}

	if plan.MinHoldMinutes == 0 {
		plan.MinHoldMinutes = 30
	}

	planManager.SetPlan(plan)
	return plan
}

// ============================================================================
// 熔断机制
// ============================================================================

func shouldTriggerCircuitBreaker(ctx *Context) bool {
	if ctx.CircuitBreaker == nil {
		ctx.CircuitBreaker = &CircuitBreakerState{}
	}

	if btcData, ok := ctx.MarketDataMap["BTCUSDT"]; ok {
		if btcData.PriceChange1h < -5.0 {
			ctx.CircuitBreaker.IsTriggered = true
			ctx.CircuitBreaker.TriggerReason = fmt.Sprintf("BTC 1小时暴跌 %.2f%%", btcData.PriceChange1h)
			ctx.CircuitBreaker.TriggerTime = time.Now()
			ctx.CircuitBreaker.CooldownMinutes = 30
			log.Printf("🛑 熔断触发: %s", ctx.CircuitBreaker.TriggerReason)
			return true
		}
	}

	if ctx.Account.TotalPnLPct < -10.0 {
		ctx.CircuitBreaker.IsTriggered = true
		ctx.CircuitBreaker.TriggerReason = fmt.Sprintf("账户回撤 %.2f%% 超过10%%", ctx.Account.TotalPnLPct)
		ctx.CircuitBreaker.TriggerTime = time.Now()
		ctx.CircuitBreaker.CooldownMinutes = 60
		log.Printf("🛑 熔断触发: %s", ctx.CircuitBreaker.TriggerReason)
		return true
	}

	if ctx.Account.MarginUsedPct > 95.0 {
		ctx.CircuitBreaker.IsTriggered = true
		ctx.CircuitBreaker.TriggerReason = fmt.Sprintf("保证金使用率 %.2f%% 过高", ctx.Account.MarginUsedPct)
		ctx.CircuitBreaker.TriggerTime = time.Now()
		ctx.CircuitBreaker.CooldownMinutes = 15
		log.Printf("🛑 熔断触发: %s", ctx.CircuitBreaker.TriggerReason)
		return true
	}

	return false
}

// ============================================================================
// 相关性计算
// ============================================================================

func calculateCorrelationMatrix(ctx *Context) {
	ctx.CorrelationMap = make(map[string]*CorrelationData)

	btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]
	if !hasBTC || btcData.MidTermSeries1h == nil {
		return
	}
	btcPrices := btcData.MidTermSeries1h.MidPrices

	for symbol, data := range ctx.MarketDataMap {
		if symbol == "BTCUSDT" {
			ctx.CorrelationMap[symbol] = &CorrelationData{
				Symbol:     symbol,
				BTCCorr:    1.0,
				IsHighCorr: true,
				RiskWeight: 1.0,
			}
			continue
		}

		if data.MidTermSeries1h == nil {
			continue
		}

		prices := data.MidTermSeries1h.MidPrices
		corr := market.CalculateCorrelation(btcPrices, prices)

		isHighCorr := math.Abs(corr) > 0.8
		riskWeight := 1.0
		if isHighCorr {
			riskWeight = 0.7
		} else if math.Abs(corr) < 0.5 {
			riskWeight = 1.0
		} else {
			riskWeight = 0.85
		}

		ctx.CorrelationMap[symbol] = &CorrelationData{
			Symbol:     symbol,
			BTCCorr:    corr,
			IsHighCorr: isHighCorr,
			RiskWeight: riskWeight,
		}
	}
}

// ============================================================================
// 市场数据获取
// ============================================================================

func fetchMarketDataForContext(ctx *Context) error {
	ctx.MarketDataMap = make(map[string]*market.Data)
	ctx.OITopDataMap = make(map[string]*OITopData)

	symbolSet := make(map[string]bool)
	symbolSet["BTCUSDT"] = true

	for _, pos := range ctx.Positions {
		symbolSet[pos.Symbol] = true
	}

	for _, coin := range ctx.CandidateCoins {
		symbolSet[coin.Symbol] = true
	}

	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	for symbol := range symbolSet {
		data, err := market.Get(symbol)
		if err != nil {
			log.Printf("⚠️ 获取 %s 数据失败: %v", symbol, err)
			continue
		}

		isExistingPosition := positionSymbols[symbol]
		if !isExistingPosition && data.OIValueUSD > 0 {
			oiValueInMillions := data.OIValueUSD / 1_000_000
			if oiValueInMillions < 15 {
				log.Printf("⚠️ %s OI价值过低(%.2fM USD < 15M)，跳过", symbol, oiValueInMillions)
				continue
			}
		}

		ctx.MarketDataMap[symbol] = data
	}

	oiPositions, err := pool.GetOITopPositions()
	if err == nil {
		for _, pos := range oiPositions {
			ctx.OITopDataMap[pos.Symbol] = &OITopData{
				Rank:              pos.Rank,
				OIDeltaPercent:    pos.OIDeltaPercent,
				OIDeltaValue:      pos.OIDeltaValue,
				PriceDeltaPercent: pos.PriceDeltaPercent,
				NetLong:           pos.NetLong,
				NetShort:          pos.NetShort,
			}
		}
	}

	return nil
}

// ============================================================================
// 风险计算
// ============================================================================
// calculateUsedRisk 计算已用风险（修复版）
func calculateUsedRisk(ctx *Context) float64 {
	if ctx.Account.TotalEquity <= 0 {
		return 0
	}

	totalRisk := 0.0

	for _, pos := range ctx.Positions {
		// 🆕 获取该持仓的交易计划
		plan := planManager.GetPlan(pos.Symbol)

		var riskUSD float64

		if plan != nil {
			// ✅ 方法1：基于计划中的止损计算真实风险
			effectiveSL := plan.CurrentStopLoss
			if effectiveSL == 0 {
				effectiveSL = plan.StopLoss
			}

			var stopDistancePct float64
			if plan.Direction == "long" {
				stopDistancePct = (pos.MarkPrice - effectiveSL) / pos.MarkPrice
			} else {
				stopDistancePct = (effectiveSL - pos.MarkPrice) / pos.MarkPrice
			}

			// 确保止损距离为正数
			if stopDistancePct < 0 {
				stopDistancePct = 0 // 已经过了止损价，风险为0（应该触发止损）
			}

			positionValue := pos.Quantity * pos.MarkPrice
			riskUSD = positionValue * stopDistancePct

		} else {
			// ✅ 方法2：无计划时，使用保守估计（假设5%止损）
			positionValue := pos.Quantity * pos.MarkPrice
			riskUSD = positionValue * 0.05 // 假设5%止损距离
		}

		posRisk := riskUSD / ctx.Account.TotalEquity
		totalRisk += posRisk

		log.Printf("📊 %s 风险计算: 仓位价值=%.2f, 风险=%.2f USD (%.2f%%)",
			pos.Symbol, pos.Quantity*pos.MarkPrice, riskUSD, posRisk*100)
	}

	return totalRisk
}

// ============================================================================
// JSON解析辅助函数（保留旧方法作为回退）
// ============================================================================

func extractCoTTrace(response string) string {
	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		return strings.TrimSpace(response[:jsonStart])
	}
	return strings.TrimSpace(response)
}

func extractDecisions(response string) ([]Decision, error) {
	arrayStart := strings.Index(response, "[")
	if arrayStart == -1 {
		return nil, fmt.Errorf("无法找到JSON数组起始")
	}

	arrayEnd := findMatchingBracket(response, arrayStart)
	if arrayEnd == -1 {
		return nil, fmt.Errorf("无法找到JSON数组结束")
	}

	jsonContent := strings.TrimSpace(response[arrayStart : arrayEnd+1])
	jsonContent = fixMissingQuotes(jsonContent)

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w\nJSON内容: %s", err, jsonContent)
	}

	return decisions, nil
}

func findMatchingBracket(s string, start int) int {
	if start >= len(s) || s[start] != '[' {
		return -1
	}

	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// ============================================================================
// System/User Prompt 构建
// ============================================================================

func buildSystemPromptOptimized(ctx *Context) string {
	var sb strings.Builder

	availableBalance := ctx.Account.AvailableBalance
	btcEthLeverage := ctx.BTCETHLeverage
	altcoinLeverage := ctx.AltcoinLeverage

	// 获取当前夏普比率
	sharpeRatio := CalculateSharpeRatio()

	sb.WriteString("你是专业的加密货币交易AI，核心目标是**最大化夏普比率**。\n\n")

	// 添加当前夏普比率状态
	if sharpeRatio != 0 {
		sb.WriteString(fmt.Sprintf("**当前策略夏普比率**: %.2f\n", sharpeRatio))
		if sharpeRatio < 0 {
			sb.WriteString("⚠️ 夏普比率为负，需要更加保守的策略\n")
		} else if sharpeRatio > 1.5 {
			sb.WriteString("✅ 夏普比率良好，可以适当增加交易频率\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("# 🎯 你的核心职责\n\n")
	sb.WriteString("**只负责寻找新的开仓机会**。持仓管理由系统自动执行。\n\n")
	sb.WriteString("**量化标准**:\n")
	sb.WriteString("- 每天2-4笔开仓\n")
	sb.WriteString("- 只输出高置信度(≥80)的开仓决策\n")
	sb.WriteString("- 没有好机会时，输出 `wait`\n\n")

	maxPositionForAltcoin := availableBalance * float64(altcoinLeverage) * 0.9
	maxPositionForBTCETH := availableBalance * float64(btcEthLeverage) * 0.9

	sb.WriteString("# ⚖️ 硬约束\n\n")
	sb.WriteString("| 约束 | 值 |\n")
	sb.WriteString("|------|----|\n")
	sb.WriteString("| 风险回报比 | ≥ 1:3 |\n")
	sb.WriteString("| 单笔风险 | ≤ 账户净值的2% |\n")
	sb.WriteString(fmt.Sprintf("| 仓位上限 | 山寨币 %.0f USD / BTC&ETH %.0f USD |\n", maxPositionForAltcoin, maxPositionForBTCETH))
	sb.WriteString("| OI价值 | ≥ 15M USD |\n\n")

	sb.WriteString("# 📋 开仓决策流程\n\n")
	sb.WriteString("1. **评估BTC趋势** → 确定大方向\n")
	sb.WriteString("2. **筛选候选币种** → ADX>25 + 趋势方向一致\n")
	sb.WriteString("3. **多时间框架确认** → 4h/1h/15m 信号对齐\n")
	sb.WriteString("4. **计算仓位** → ATR自适应 + 相关性调整\n")
	sb.WriteString("5. **设置止损止盈** → 止损=ATR×2.5, RR≥1:3.5\n")
	sb.WriteString("6. **定义失效条件** → 什么情况下计划失效\n\n")

	// 🆕 新增：失效条件格式说明
	sb.WriteString("# 🚫 失效条件格式（重要！）\n\n")
	sb.WriteString("使用**结构化格式**定义失效条件，系统会自动监控并触发平仓。\n\n")
	sb.WriteString("**格式**: `时间框架:条件类型:参数1:参数2`\n\n")
	sb.WriteString("**支持的条件类型**:\n")
	sb.WriteString("| 类型 | 格式 | 说明 | 示例 |\n")
	sb.WriteString("|------|------|------|------|\n")
	sb.WriteString("| EMA死叉 | `TF:EMA_CROSS_DOWN:EMA短:EMA长` | 短期EMA下穿长期EMA | `4H:EMA_CROSS_DOWN:EMA20:EMA50` |\n")
	sb.WriteString("| EMA金叉 | `TF:EMA_CROSS_UP:EMA短:EMA长` | 短期EMA上穿长期EMA | `4H:EMA_CROSS_UP:EMA20:EMA50` |\n")
	sb.WriteString("| 价格跌破 | `TF:PRICE_BELOW:指标或价格` | 价格跌破指定位置 | `4H:PRICE_BELOW:EMA50` |\n")
	sb.WriteString("| 价格突破 | `TF:PRICE_ABOVE:指标或价格` | 价格突破指定位置 | `1H:PRICE_ABOVE:95000` |\n")
	sb.WriteString("| RSI超买 | `TF:RSI_ABOVE:阈值` | RSI超过阈值 | `4H:RSI_ABOVE:70` |\n")
	sb.WriteString("| RSI超卖 | `TF:RSI_BELOW:阈值` | RSI低于阈值 | `4H:RSI_BELOW:30` |\n")
	sb.WriteString("| ADX减弱 | `TF:ADX_BELOW:阈值` | ADX低于阈值 | `4H:ADX_BELOW:20` |\n")
	sb.WriteString("| MACD交叉 | `TF:MACD_CROSS:方向` | MACD交叉 | `4H:MACD_CROSS:DOWN` |\n")
	sb.WriteString("| 趋势反转 | `TF:TREND_REVERSAL` | DI反转 | `4H:TREND_REVERSAL` |\n\n")
	sb.WriteString("**时间框架**: `4H`, `1H`, `15M`, `30M`\n\n")

	sb.WriteString("# 💵 波动率自适应仓位\n\n")
	sb.WriteString("```\n")
	sb.WriteString("止损距离 = ATR14 × 倍数（山寨2.5，BTC/ETH 1.8）\n")
	sb.WriteString("仓位大小 = (账户净值 × 2%) / 止损百分比\n")
	sb.WriteString("```\n\n")

	sb.WriteString("# 📐 相关性控制\n\n")
	sb.WriteString("- 高相关(ρ>0.8)：仓位×0.7\n")
	sb.WriteString("- 中相关(0.5<ρ<0.8)：仓位×0.85\n")
	sb.WriteString("- 同方向高相关持仓不超过2个\n\n")

	sb.WriteString("# 📤 输出格式\n\n")
	sb.WriteString("**第一步**: 简短分析（3-5句话）\n")
	sb.WriteString("**第二步**: JSON决策数组\n\n")

	sb.WriteString("**开仓决策JSON**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[\n")
	sb.WriteString("  {\n")
	sb.WriteString("    \"symbol\": \"BTCUSDT\",\n")
	sb.WriteString("    \"action\": \"open_long\",\n")
	sb.WriteString(fmt.Sprintf("    \"leverage\": %d,\n", btcEthLeverage))
	sb.WriteString("    \"position_size_usd\": 100,\n")
	sb.WriteString("    \"stop_loss\": 95000,\n")
	sb.WriteString("    \"take_profit\": 105000,\n")
	sb.WriteString("    \"confidence\": 85,\n")
	sb.WriteString("    \"risk_usd\": 10,\n")
	sb.WriteString("    \"invalidation_price\": 96000,\n")
	sb.WriteString("    \"invalidation_condition\": \"4H:EMA_CROSS_DOWN:EMA20:EMA50\",\n")
	sb.WriteString("    \"min_hold_minutes\": 30,\n")
	sb.WriteString("    \"reasoning\": \"BTC强势+4H突破+资金费率中性\"\n")
	sb.WriteString("  }\n")
	sb.WriteString("]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**空单示例**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[\n")
	sb.WriteString("  {\n")
	sb.WriteString("    \"symbol\": \"ETHUSDT\",\n")
	sb.WriteString("    \"action\": \"open_short\",\n")
	sb.WriteString(fmt.Sprintf("    \"leverage\": %d,\n", btcEthLeverage))
	sb.WriteString("    \"position_size_usd\": 80,\n")
	sb.WriteString("    \"stop_loss\": 3500,\n")
	sb.WriteString("    \"take_profit\": 3200,\n")
	sb.WriteString("    \"confidence\": 82,\n")
	sb.WriteString("    \"risk_usd\": 8,\n")
	sb.WriteString("    \"invalidation_price\": 3450,\n")
	sb.WriteString("    \"invalidation_condition\": \"4H:EMA_CROSS_UP:EMA20:EMA50\",\n")
	sb.WriteString("    \"min_hold_minutes\": 30,\n")
	sb.WriteString("    \"reasoning\": \"ETH弱势+4H跌破支撑+资金费率偏高\"\n")
	sb.WriteString("  }\n")
	sb.WriteString("]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("**无机会时**:\n")
	sb.WriteString("```json\n")
	sb.WriteString("[{\"symbol\": \"ALL\", \"action\": \"wait\", \"reasoning\": \"无符合条件的机会\"}]\n")
	sb.WriteString("```\n\n")

	sb.WriteString("---\n")
	sb.WriteString("**核心原则**: 宁可错过，不可做错 | 风险回报比≥1:3 | BTC是龙头 | 失效条件必须明确\n")

	return sb.String()
}

func buildUserPromptOptimized(ctx *Context, remainingBudget float64) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("**时间**: %s | **周期**: #%d\n\n", ctx.CurrentTime, ctx.CallCount))

	// BTC状态
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		marketState, stateConfidence := market.GetMarketState(btcData)
		sb.WriteString("## 🪙 BTC状态\n")
		sb.WriteString(fmt.Sprintf("**价格**: %.2f | **趋势**: **%s** (置信度%d%%)\n",
			btcData.CurrentPrice, marketState, stateConfidence))
		sb.WriteString(fmt.Sprintf("**ADX**: %.1f | **DI+**: %.1f | **DI-**: %.1f\n",
			btcData.CurrentADX, btcData.CurrentDIPlus, btcData.CurrentDIMinus))
		sb.WriteString(fmt.Sprintf("**MACD**: %.4f | **RSI14**: %.1f | **资金费率**: %.4f%%\n\n",
			btcData.CurrentMACD, btcData.CurrentRSI14, btcData.FundingRate*100))
	}

	// 账户状态（包含夏普比率）
	sb.WriteString("## 💰 账户状态\n")
	sb.WriteString(fmt.Sprintf("**净值**: %.2f USDT | **可用**: %.2f USDT\n",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("**剩余风险预算**: **%.1f%%** (可开仓风险额度)\n",
		remainingBudget*100))
	sb.WriteString(fmt.Sprintf("**当前持仓**: %d/3\n", ctx.Account.PositionCount))

	// 显示夏普比率
	sharpeRatio := CalculateSharpeRatio()
	sortinoRatio := CalculateSortinoRatio()
	if sharpeRatio != 0 || sortinoRatio != 0 {
		sb.WriteString(fmt.Sprintf("**夏普比率**: %.2f | **索提诺比率**: %.2f\n", sharpeRatio, sortinoRatio))
	}
	sb.WriteString("\n")

	// 当前持仓
	if len(ctx.Positions) > 0 {
		sb.WriteString("## 📊 当前持仓（仅供参考，不需要管理）\n")
		for _, pos := range ctx.Positions {
			sb.WriteString(fmt.Sprintf("- %s %s: 盈亏 %+.2f%%\n",
				pos.Symbol, strings.ToUpper(pos.Side), pos.UnrealizedPnLPct))
		}
		sb.WriteString("\n")
	}

	// 候选币种
	sb.WriteString("## 🔍 候选币种\n\n")
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		if coin.Symbol == "BTCUSDT" {
			continue
		}
		hasPosition := false
		for _, pos := range ctx.Positions {
			if pos.Symbol == coin.Symbol {
				hasPosition = true
				break
			}
		}
		if hasPosition {
			continue
		}

		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		corrInfo := ""
		if corr, ok := ctx.CorrelationMap[coin.Symbol]; ok {
			corrInfo = fmt.Sprintf(" | BTC相关性: %.2f", corr.BTCCorr)
			if corr.IsHighCorr {
				corrInfo += "(高)"
			}
		}

		marketState, _ := market.GetMarketState(marketData)

		isAltcoin := coin.Symbol != "BTCUSDT" && coin.Symbol != "ETHUSDT"
		atr14 := 0.0
		if marketData.LongerTermContext != nil {
			atr14 = marketData.LongerTermContext.ATR14
		}
		suggestedSize, stopDist := market.CalculateAdaptivePositionSize(
			ctx.Account.TotalEquity,
			atr14,
			marketData.CurrentPrice,
			ctx.MaxRiskPerTrade,
			isAltcoin,
		)

		sb.WriteString(fmt.Sprintf("### %d. %s\n", displayedCount, coin.Symbol))
		sb.WriteString(fmt.Sprintf("**趋势**: %s%s\n", marketState, corrInfo))
		sb.WriteString(fmt.Sprintf("**建议仓位**: %.0f USD | **止损距离**: %.4f\n",
			suggestedSize, stopDist))
		sb.WriteString(market.FormatCompact(marketData))
		sb.WriteString("\n")

		if displayedCount >= 5 {
			break
		}
	}

	// 绩效指标
	stats := GetReturnsStats()
	if stats["count"] >= 5 {
		sb.WriteString("## 📈 绩效指标\n")
		sb.WriteString(fmt.Sprintf("**交易数**: %.0f | **胜率**: %.1f%%\n",
			stats["count"], stats["win_rate"]*100))
		sb.WriteString(fmt.Sprintf("**夏普比率**: %.2f | **索提诺比率**: %.2f\n\n",
			stats["sharpe_ratio"], stats["sortino_ratio"]))

		if stats["sharpe_ratio"] < -0.5 {
			sb.WriteString("⚠️ **夏普<-0.5**: 极其保守，只做置信度≥90的交易\n\n")
		} else if stats["sharpe_ratio"] < 0 {
			sb.WriteString("⚠️ **夏普<0**: 保守策略，只做置信度≥85的交易\n\n")
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("请分析并输出开仓决策（简短分析 + JSON）\n")

	return sb.String()
}

// ============================================================================
// 交易执行回调
// ============================================================================

// OnPositionOpened 开仓成功后调用（修复版）
func OnPositionOpened(decision *Decision, actualEntryPrice float64, actualQuantity float64) error {
	// ✅ 验证入场价
	if actualEntryPrice <= 0 {
		return fmt.Errorf("无效的入场价格: %.4f", actualEntryPrice)
	}

	plan := CreateTradePlanFromDecision(decision, actualEntryPrice)

	// ✅ 更新实际数量
	if actualQuantity > 0 {
		planManager.UpdatePlan(decision.Symbol, func(p *TradePlan) {
			p.ActualQuantity = actualQuantity
			p.PositionSizeUSD = actualQuantity * actualEntryPrice
		})
	}

	log.Printf("✅ 开仓成功，交易计划已创建: %s %s @ %.4f (数量: %.6f)",
		plan.Symbol, plan.Direction, actualEntryPrice, actualQuantity)

	return nil
}

// OnPositionClosedSimple 简化版平仓回调（向后兼容）
func OnPositionClosedSimple(symbol string, reason string) {
	plan := planManager.GetPlan(symbol)
	peakPnL := 0.0
	if plan != nil {
		peakPnL = plan.PeakPnLPercent
	}

	planManager.RemovePlan(symbol)
	log.Printf("✅ 平仓成功，交易计划已移除: %s (峰值盈利: %.2f%%, 原因: %s)",
		symbol, peakPnL, reason)
}

// OnPartialClose 部分平仓成功后调用
func OnPartialClose(symbol string, trancheIndex int, percentage float64, newStopLoss float64) {
	// 标记档位已执行
	if trancheIndex >= 0 {
		planManager.MarkTrancheExecuted(symbol, trancheIndex, percentage)
	}

	// 更新止损
	if newStopLoss > 0 {
		planManager.UpdatePlanStopLoss(symbol, newStopLoss)
	}

	log.Printf("✅ %s 部分平仓%.0f%% (档位%d), 新止损: %.4f",
		symbol, percentage, trancheIndex+1, newStopLoss)
}

// OnStopLossUpdated 止损更新成功后调用
func OnStopLossUpdated(symbol string, newStopLoss float64) {
	planManager.UpdatePlanStopLoss(symbol, newStopLoss)
	log.Printf("✅ %s 止损已更新至 %.4f", symbol, newStopLoss)
}

// ============================================================================
// 计划同步
// ============================================================================

// SyncPlansFromPositions 从现有持仓同步计划
func SyncPlansFromPositions(positions []PositionInfo, marketDataMap map[string]*market.Data) {
	for _, pos := range positions {
		if planManager.GetPlan(pos.Symbol) != nil {
			continue
		}

		marketData, ok := marketDataMap[pos.Symbol]
		if !ok {
			log.Printf("⚠️ 无法为 %s 创建恢复计划：缺少市场数据", pos.Symbol)
			continue
		}

		atr := 0.0
		if marketData.LongerTermContext != nil {
			atr = marketData.LongerTermContext.ATR14
		}

		isAltcoin := pos.Symbol != "BTCUSDT" && pos.Symbol != "ETHUSDT"
		multiplier := 1.8
		if isAltcoin {
			multiplier = 2.5
		}
		stopDistance := atr * multiplier

		var stopLoss, takeProfit float64
		if pos.Side == "long" {
			stopLoss = pos.EntryPrice - stopDistance
			takeProfit = pos.EntryPrice + stopDistance*3.5
		} else {
			stopLoss = pos.EntryPrice + stopDistance
			takeProfit = pos.EntryPrice - stopDistance*3.5
		}

		if pos.StopLoss > 0 {
			stopLoss = pos.StopLoss
		}
		if pos.TakeProfit > 0 {
			takeProfit = pos.TakeProfit
		}

		plan := &TradePlan{
			ID:              fmt.Sprintf("recovered_%s_%d", pos.Symbol, time.Now().UnixNano()),
			Symbol:          pos.Symbol,
			Direction:       pos.Side,
			EntryPrice:      pos.EntryPrice,
			StopLoss:        stopLoss,
			TakeProfit:      takeProfit,
			CurrentStopLoss: stopLoss,
			PositionSizeUSD: pos.MarginUsed * float64(pos.Leverage),
			Leverage:        pos.Leverage,
			EntryReason:     "从现有持仓恢复",
			MinHoldMinutes:  0,
			CreatedAt:       time.UnixMilli(pos.UpdateTime),
			Status:          "ACTIVE",
		}

		planManager.SetPlan(plan)
		log.Printf("📋 从持仓恢复交易计划: %s %s @ %.4f, SL=%.4f, TP=%.4f",
			pos.Symbol, pos.Side, pos.EntryPrice, stopLoss, takeProfit)
	}
}

// ============================================================================
// 计划状态查询
// ============================================================================

// GetAllPlans 获取所有活跃计划
func GetAllPlans() []*TradePlan {
	planManager.mu.RLock()
	defer planManager.mu.RUnlock()

	var plans []*TradePlan
	for _, plan := range planManager.plans {
		plans = append(plans, plan)
	}
	return plans
}

// GetPlanStatus 获取计划状态摘要
func GetPlanStatus() string {
	plans := GetAllPlans()
	if len(plans) == 0 {
		return "无活跃交易计划"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("活跃计划: %d个\n", len(plans)))

	for _, plan := range plans {
		holdingTime := time.Since(plan.CreatedAt).Minutes()

		// 🆕 显示峰值信息
		peakInfo := ""
		if plan.PeakPnLPercent > 0 {
			peakInfo = fmt.Sprintf(", 峰值盈利=%.2f%%", plan.PeakPnLPercent)
		}
		if plan.PeakPrice > 0 {
			peakInfo += fmt.Sprintf(", 峰值价=%.4f", plan.PeakPrice)
		}

		// 🆕 显示分批止盈进度
		trancheInfo := ""
		if plan.TotalClosedPercent > 0 {
			trancheInfo = fmt.Sprintf(", 已平仓%.0f%%", plan.TotalClosedPercent)
		}

		sb.WriteString(fmt.Sprintf("  - %s %s: 持仓%.0f分钟, SL=%.4f, TP=%.4f%s%s\n",
			plan.Symbol, plan.Direction, holdingTime,
			plan.CurrentStopLoss, plan.TakeProfit,
			peakInfo, trancheInfo))

		if plan.InvalidationCondition != "" {
			formattedCond := FormatInvalidationCondition(plan.InvalidationCondition)
			sb.WriteString(fmt.Sprintf("    └─ 失效条件: %s\n", formattedCond))
		}
	}
	return sb.String()
}

// GetPlanDetails 获取计划详情（用于日志和调试）
func GetPlanDetails(symbol string) string {
	plan := planManager.GetPlan(symbol)
	if plan == nil {
		return fmt.Sprintf("未找到 %s 的交易计划", symbol)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("═══ %s 交易计划详情 ═══\n", symbol))
	sb.WriteString(fmt.Sprintf("方向: %s\n", plan.Direction))
	sb.WriteString(fmt.Sprintf("入场价: %.4f\n", plan.EntryPrice))
	sb.WriteString(fmt.Sprintf("止损: %.4f (当前: %.4f)\n", plan.StopLoss, plan.CurrentStopLoss))
	sb.WriteString(fmt.Sprintf("止盈: %.4f (原始: %.4f)\n", plan.TakeProfit, plan.OriginalTakeProfit))
	sb.WriteString(fmt.Sprintf("仓位: %.2f USD\n", plan.PositionSizeUSD))
	sb.WriteString(fmt.Sprintf("杠杆: %dx\n", plan.Leverage))
	sb.WriteString(fmt.Sprintf("置信度: %d%%\n", plan.Confidence))
	sb.WriteString(fmt.Sprintf("最小持仓: %d分钟\n", plan.MinHoldMinutes))
	sb.WriteString(fmt.Sprintf("创建时间: %s\n", plan.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("状态: %s\n\n", plan.Status))

	// 🆕 峰值追踪信息
	sb.WriteString("--- 峰值追踪 ---\n")
	sb.WriteString(fmt.Sprintf("峰值价格: %.4f\n", plan.PeakPrice))
	sb.WriteString(fmt.Sprintf("峰值盈利: %.2f%%\n", plan.PeakPnLPercent))
	sb.WriteString(fmt.Sprintf("入场ATR: %.4f\n\n", plan.EntryATR))

	// 🆕 分批止盈进度
	sb.WriteString("--- 分批止盈进度 ---\n")
	sb.WriteString(fmt.Sprintf("累计平仓: %.0f%%\n", plan.TotalClosedPercent))
	sb.WriteString(fmt.Sprintf("最后执行档位: %d\n", plan.LastExecutedTranche))
	if plan.ExecutedTranches != nil {
		for i, tranche := range defaultExitTranches {
			status := "⏳ 待执行"
			if plan.ExecutedTranches[i] {
				status = "✅ 已执行"
			}
			sb.WriteString(fmt.Sprintf("  档位%d (RR %.1f): %s\n", i+1, tranche.TriggerRR, status))
		}
	}
	sb.WriteString("\n")

	// 失效条件
	sb.WriteString("--- 失效条件 ---\n")
	if plan.InvalidationPrice > 0 {
		sb.WriteString(fmt.Sprintf("失效价格: %.4f\n", plan.InvalidationPrice))
	}
	if plan.InvalidationCondition != "" {
		sb.WriteString(fmt.Sprintf("原始条件: %s\n", plan.InvalidationCondition))
		sb.WriteString(fmt.Sprintf("格式化: %s\n", FormatInvalidationCondition(plan.InvalidationCondition)))
	}
	sb.WriteString("\n")

	sb.WriteString("--- 入场理由 ---\n")
	sb.WriteString(plan.EntryReason + "\n")

	return sb.String()
}

// ============================================================================
// 失效条件验证工具
// ============================================================================

// ValidateInvalidationCondition 验证失效条件格式是否正确
func ValidateInvalidationCondition(condition string) (bool, string) {
	if condition == "" {
		return false, "失效条件为空"
	}

	parsed := ParseInvalidationCondition(condition)
	if !parsed.IsValid {
		return false, fmt.Sprintf("解析失败: %s", parsed.ParseError)
	}

	// 检查时间框架
	validTimeframes := map[string]bool{"4H": true, "1H": true, "15M": true, "30M": true, "1D": true}
	if parsed.Timeframe != "" && !validTimeframes[parsed.Timeframe] {
		return false, fmt.Sprintf("不支持的时间框架: %s", parsed.Timeframe)
	}

	return true, FormatInvalidationCondition(condition)
}

// GetSupportedInvalidationConditions 获取支持的失效条件类型说明
func GetSupportedInvalidationConditions() string {
	return `
支持的失效条件格式:
═══════════════════════════════════════════════════════════════

1. EMA死叉 (多单失效)
   格式: 4H:EMA_CROSS_DOWN:EMA20:EMA50
   说明: 当4小时EMA20下穿EMA50时触发

2. EMA金叉 (空单失效)
   格式: 4H:EMA_CROSS_UP:EMA20:EMA50
   说明: 当4小时EMA20上穿EMA50时触发

3. 价格跌破 (多单失效)
   格式: 4H:PRICE_BELOW:EMA50 或 4H:PRICE_BELOW:95000
   说明: 当价格跌破指定EMA或价格时触发

4. 价格突破 (空单失效)
   格式: 1H:PRICE_ABOVE:EMA20 或 1H:PRICE_ABOVE:100000
   说明: 当价格突破指定EMA或价格时触发

5. RSI超买 (空单失效)
   格式: 4H:RSI_ABOVE:70
   说明: 当RSI超过指定阈值时触发

6. RSI超卖 (多单失效)
   格式: 4H:RSI_BELOW:30
   说明: 当RSI低于指定阈值时触发

7. ADX减弱 (任意方向)
   格式: 4H:ADX_BELOW:20
   说明: 当ADX低于阈值表示趋势减弱

8. MACD交叉
   格式: 4H:MACD_CROSS:DOWN 或 4H:MACD_CROSS:UP
   说明: MACD死叉/金叉

9. 趋势反转
   格式: 4H:TREND_REVERSAL
   说明: DI+/DI-反转

═══════════════════════════════════════════════════════════════
时间框架: 4H, 1H, 30M, 15M, 1D
`
}

// GetPlanBySymbol 根据symbol获取计划
func GetPlanBySymbol(symbol string) *TradePlan {
	return planManager.GetPlan(symbol)
}

// ============================================================================
// 性能统计
// ============================================================================

// TradeStatistics 交易统计
type TradeStatistics struct {
	TotalTrades     int       `json:"total_trades"`
	WinningTrades   int       `json:"winning_trades"`
	LosingTrades    int       `json:"losing_trades"`
	TotalPnL        float64   `json:"total_pnl"`
	AverageWin      float64   `json:"average_win"`
	AverageLoss     float64   `json:"average_loss"`
	WinRate         float64   `json:"win_rate"`
	ProfitFactor    float64   `json:"profit_factor"`
	SharpeRatio     float64   `json:"sharpe_ratio"`
	SortinoRatio    float64   `json:"sortino_ratio"`
	MaxDrawdown     float64   `json:"max_drawdown"`
	AverageHoldTime float64   `json:"average_hold_time_minutes"`
	LastUpdated     time.Time `json:"last_updated"`
}

var (
	tradeStats     = &TradeStatistics{}
	tradeStatsLock sync.RWMutex
)

// UpdateStatistics 更新统计（同时更新夏普比率）
func UpdateStatistics(pnlPercent float64, holdTimeMinutes float64) {
	// Step 1: 更新统计数据（持有锁）
	tradeStatsLock.Lock()

	tradeStats.TotalTrades++
	tradeStats.TotalPnL += pnlPercent

	if pnlPercent > 0 {
		tradeStats.WinningTrades++
		tradeStats.AverageWin = (tradeStats.AverageWin*float64(tradeStats.WinningTrades-1) + pnlPercent) / float64(tradeStats.WinningTrades)
	} else {
		tradeStats.LosingTrades++
		tradeStats.AverageLoss = (tradeStats.AverageLoss*float64(tradeStats.LosingTrades-1) + math.Abs(pnlPercent)) / float64(tradeStats.LosingTrades)
	}

	if tradeStats.TotalTrades > 0 {
		tradeStats.WinRate = float64(tradeStats.WinningTrades) / float64(tradeStats.TotalTrades)
	}

	if tradeStats.AverageLoss > 0 && tradeStats.WinRate < 1 {
		tradeStats.ProfitFactor = (tradeStats.AverageWin * tradeStats.WinRate) / (tradeStats.AverageLoss * (1 - tradeStats.WinRate))
	}

	tradeStats.AverageHoldTime = (tradeStats.AverageHoldTime*float64(tradeStats.TotalTrades-1) + holdTimeMinutes) / float64(tradeStats.TotalTrades)
	tradeStats.LastUpdated = time.Now()

	// Step 2: 计算夏普比率和索提诺比率
	// 需要同时持有两把锁，按固定顺序获取避免死锁
	returnsLock.RLock()
	tradeStats.SharpeRatio = calculateSharpeRatioUnlocked()
	tradeStats.SortinoRatio = calculateSortinoRatioUnlocked()
	returnsLock.RUnlock()

	// 复制日志需要的数据
	totalTrades := tradeStats.TotalTrades
	winRate := tradeStats.WinRate
	profitFactor := tradeStats.ProfitFactor
	sharpeRatio := tradeStats.SharpeRatio

	tradeStatsLock.Unlock() // ← 先释放锁

	// Step 3: 日志输出（锁外）
	log.Printf("📊 统计更新: 总交易=%d, 胜率=%.1f%%, 盈亏因子=%.2f, 夏普=%.2f",
		totalTrades, winRate*100, profitFactor, sharpeRatio)

	// Step 4: 在锁外调用自动保存（避免死锁）
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// GetStatistics 获取统计信息（带锁版本）
func GetStatistics() *TradeStatistics {
	tradeStatsLock.RLock()
	defer tradeStatsLock.RUnlock()
	statsCopy := *tradeStats
	return &statsCopy
}

// ResetStatistics 重置统计（修复死锁版本）
func ResetStatistics() {
	tradeStatsLock.Lock()
	tradeStats = &TradeStatistics{}
	tradeStatsLock.Unlock() // ← 先释放锁

	returnsLock.Lock()
	returnsSeries = nil
	returnsLock.Unlock() // ← 先释放锁

	log.Printf("📊 统计已重置")

	// 在锁外调用自动保存
	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// ============================================================================
// 导出函数（供外部调用）
// ============================================================================

// CalculateOptimalPosition 计算最优仓位
func CalculateOptimalPosition(ctx *Context, symbol string, side string) (positionSize, stopLoss, takeProfit float64, err error) {
	marketData, ok := ctx.MarketDataMap[symbol]
	if !ok {
		return 0, 0, 0, fmt.Errorf("缺少 %s 市场数据", symbol)
	}

	currentPrice := marketData.CurrentPrice
	atr14 := 0.0
	if marketData.LongerTermContext != nil {
		atr14 = marketData.LongerTermContext.ATR14
	}

	isAltcoin := symbol != "BTCUSDT" && symbol != "ETHUSDT"

	suggestedSize, stopDistance := market.CalculateAdaptivePositionSize(
		ctx.Account.TotalEquity,
		atr14,
		currentPrice,
		ctx.MaxRiskPerTrade,
		isAltcoin,
	)

	if corr, ok := ctx.CorrelationMap[symbol]; ok {
		suggestedSize *= corr.RiskWeight
	}

	if side == "long" {
		stopLoss = currentPrice - stopDistance
		takeProfit = currentPrice + stopDistance*3.5
	} else {
		stopLoss = currentPrice + stopDistance
		takeProfit = currentPrice - stopDistance*3.5
	}

	maxLeverage := ctx.AltcoinLeverage
	if !isAltcoin {
		maxLeverage = ctx.BTCETHLeverage
	}
	maxPositionValue := ctx.Account.AvailableBalance * float64(maxLeverage) * 0.9

	if suggestedSize > maxPositionValue {
		suggestedSize = maxPositionValue
	}

	return suggestedSize, stopLoss, takeProfit, nil
}

// GetTradingRecommendation 获取交易建议
func GetTradingRecommendation(ctx *Context, symbol string) string {
	marketData, ok := ctx.MarketDataMap[symbol]
	if !ok {
		return "缺少市场数据"
	}

	var recommendations []string

	state, confidence := market.GetMarketState(marketData)
	recommendations = append(recommendations, fmt.Sprintf("市场状态: %s (置信度%d%%)", state, confidence))

	if marketData.LongerTermContext != nil {
		if marketData.LongerTermContext.EMA20 > marketData.LongerTermContext.EMA50 {
			recommendations = append(recommendations, "4小时EMA: 多头排列")
		} else {
			recommendations = append(recommendations, "4小时EMA: 空头排列")
		}
	}

	fundingPct := marketData.FundingRate * 100
	if fundingPct > 0.05 {
		recommendations = append(recommendations, fmt.Sprintf("资金费率: %.4f%% (做空有利)", fundingPct))
	} else if fundingPct < -0.05 {
		recommendations = append(recommendations, fmt.Sprintf("资金费率: %.4f%% (做多有利)", fundingPct))
	}

	if corr, ok := ctx.CorrelationMap[symbol]; ok {
		if corr.IsHighCorr {
			recommendations = append(recommendations, fmt.Sprintf("BTC相关性: %.2f (高，需降低仓位)", corr.BTCCorr))
		}
	}

	return strings.Join(recommendations, " | ")
}

// ============================================================================
// 决策执行器（供主程序调用）
// ============================================================================

// DecisionExecutor 执行决策的接口定义
type DecisionExecutor interface {
	OpenPosition(symbol, side string, leverage int, sizeUSD, stopLoss, takeProfit float64) (*OpenPositionResult, error)
	ClosePosition(symbol string, percentage float64) error
	UpdateStopLoss(symbol string, newStopLoss float64) error
}

// ProcessDecisions 处理决策列表
func ProcessDecisions(decisions []Decision, executor DecisionExecutor, marketData map[string]*market.Data) error {
	for _, d := range decisions {
		var err error

		switch d.Action {
		case "open_long":
			// ✅ 执行开仓并获取实际成交信息
			result, err := executor.OpenPosition(d.Symbol, "long", d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
			if err == nil {
				OnPositionOpened(&d, result.EntryPrice, result.Quantity)
			}

		case "open_short":
			result, err := executor.OpenPosition(d.Symbol, "short", d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
			if err == nil {
				OnPositionOpened(&d, result.EntryPrice, result.Quantity)
			}

		case "close_long", "close_short":
			err = executor.ClosePosition(d.Symbol, 100)
			if err == nil {
				// 🔧 使用增强版回调（如果有市场数据可以计算盈亏）
				if md, ok := marketData[d.Symbol]; ok {
					plan := planManager.GetPlan(d.Symbol)
					if plan != nil {
						var pnlPct float64
						if plan.Direction == "long" {
							pnlPct = (md.CurrentPrice - plan.EntryPrice) / plan.EntryPrice * 100
						} else {
							pnlPct = (plan.EntryPrice - md.CurrentPrice) / plan.EntryPrice * 100
						}
						OnPositionClosed(d.Symbol, md.CurrentPrice, pnlPct, 0, d.Reasoning)
					} else {
						OnPositionClosedSimple(d.Symbol, d.Reasoning)
					}
				} else {
					OnPositionClosedSimple(d.Symbol, d.Reasoning)
				}
			}

		case "partial_close":
			err = executor.ClosePosition(d.Symbol, d.ClosePercentage)
			if err == nil {
				// 🔧 使用档位索引
				OnPartialClose(d.Symbol, d.TrancheIndex, d.ClosePercentage, d.NewStopLoss)
			}

		case "update_stop_loss":
			err = executor.UpdateStopLoss(d.Symbol, d.NewStopLoss)
			if err == nil {
				OnStopLossUpdated(d.Symbol, d.NewStopLoss)
			}

		case "hold", "wait":
			continue
		}

		if err != nil {
			log.Printf("❌ 执行 %s %s 失败: %v", d.Symbol, d.Action, err)
		}
	}

	return nil
}

// OpenPositionResult 开仓结果
type OpenPositionResult struct {
	EntryPrice float64
	Quantity   float64
	OrderID    string
	Timestamp  time.Time
}

// ============================================================================
// 调试和监控
// ============================================================================

// DebugContext 输出调试信息
func DebugContext(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("=== 决策系统状态 ===\n")
	sb.WriteString(fmt.Sprintf("时间: %s\n", ctx.CurrentTime))
	sb.WriteString(fmt.Sprintf("运行时间: %d分钟\n", ctx.RuntimeMinutes))
	sb.WriteString(fmt.Sprintf("调用次数: %d\n", ctx.CallCount))
	sb.WriteString(fmt.Sprintf("上次分析: %s\n", ctx.LastAnalysisTime.Format("15:04:05")))
	sb.WriteString(fmt.Sprintf("分析间隔: %d分钟\n\n", ctx.AnalysisIntervalMin))

	sb.WriteString("=== 账户状态 ===\n")
	sb.WriteString(fmt.Sprintf("净值: %.2f USDT\n", ctx.Account.TotalEquity))
	sb.WriteString(fmt.Sprintf("可用: %.2f USDT\n", ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("保证金使用率: %.1f%%\n", ctx.Account.MarginUsedPct))
	sb.WriteString(fmt.Sprintf("持仓数: %d\n\n", ctx.Account.PositionCount))

	sb.WriteString("=== 风险预算 ===\n")
	usedRisk := calculateUsedRisk(ctx)
	remainingRisk := ctx.TotalRiskBudget - usedRisk
	sb.WriteString(fmt.Sprintf("总预算: %.1f%%\n", ctx.TotalRiskBudget*100))
	sb.WriteString(fmt.Sprintf("已用: %.1f%%\n", usedRisk*100))
	sb.WriteString(fmt.Sprintf("剩余: %.1f%%\n\n", remainingRisk*100))

	sb.WriteString("=== 绩效指标 ===\n")
	stats := GetStatistics()
	sb.WriteString(fmt.Sprintf("总交易: %d | 胜率: %.1f%%\n", stats.TotalTrades, stats.WinRate*100))
	sb.WriteString(fmt.Sprintf("夏普比率: %.2f | 索提诺比率: %.2f\n", stats.SharpeRatio, stats.SortinoRatio))
	sb.WriteString(fmt.Sprintf("盈亏因子: %.2f | 平均持仓: %.0f分钟\n\n", stats.ProfitFactor, stats.AverageHoldTime))

	sb.WriteString("=== 交易计划 ===\n")
	sb.WriteString(GetPlanStatus())

	if ctx.CircuitBreaker != nil && ctx.CircuitBreaker.IsTriggered {
		sb.WriteString("\n=== 熔断状态 ===\n")
		sb.WriteString(fmt.Sprintf("触发原因: %s\n", ctx.CircuitBreaker.TriggerReason))
		sb.WriteString(fmt.Sprintf("触发时间: %s\n", ctx.CircuitBreaker.TriggerTime.Format("15:04:05")))
		sb.WriteString(fmt.Sprintf("冷却时间: %d分钟\n", ctx.CircuitBreaker.CooldownMinutes))
	}

	return sb.String()
}

// ============================================================================
// 快捷函数
// ============================================================================

// QuickAnalyze 快速分析（不调用AI）
func QuickAnalyze(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("=== 快速市场分析 ===\n\n")

	if btcData, ok := ctx.MarketDataMap["BTCUSDT"]; ok {
		state, conf := market.GetMarketState(btcData)
		sb.WriteString(fmt.Sprintf("**BTC**: %.2f | %s (%d%%)\n", btcData.CurrentPrice, state, conf))
		sb.WriteString(fmt.Sprintf("  ADX=%.1f | DI+=%.1f | DI-=%.1f | RSI=%.1f\n\n",
			btcData.CurrentADX, btcData.CurrentDIPlus, btcData.CurrentDIMinus, btcData.CurrentRSI14))
	}

	if len(ctx.Positions) > 0 {
		sb.WriteString("**持仓状态**:\n")
		for _, pos := range ctx.Positions {
			plan := planManager.GetPlan(pos.Symbol)
			planInfo := "无计划"
			if plan != nil {
				holdMin := time.Since(plan.CreatedAt).Minutes()
				planInfo = fmt.Sprintf("持仓%.0f分钟, SL=%.4f", holdMin, plan.CurrentStopLoss)
			}
			sb.WriteString(fmt.Sprintf("  %s %s: %+.2f%% | %s\n",
				pos.Symbol, pos.Side, pos.UnrealizedPnLPct, planInfo))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("**候选币评分**:\n")
	for _, coin := range ctx.CandidateCoins {
		if coin.Symbol == "BTCUSDT" {
			continue
		}
		if data, ok := ctx.MarketDataMap[coin.Symbol]; ok {
			state, conf := market.GetMarketState(data)
			score := calculateCoinScore(data, ctx.CorrelationMap[coin.Symbol])
			sb.WriteString(fmt.Sprintf("  %s: %s(%d%%) | 评分=%.1f\n",
				coin.Symbol, state, conf, score))
		}
	}

	return sb.String()
}

// calculateCoinScore 计算币种评分
func calculateCoinScore(data *market.Data, corr *CorrelationData) float64 {
	score := 50.0

	if data.CurrentADX > 25 {
		score += 15
	} else if data.CurrentADX > 20 {
		score += 8
	}

	if data.CurrentRSI14 > 30 && data.CurrentRSI14 < 70 {
		score += 10
	}

	if data.LongerTermContext != nil {
		pos := data.LongerTermContext.PricePosition
		if pos > 0.2 && pos < 0.8 {
			score += 10
		}
	}

	if corr != nil && corr.IsHighCorr {
		score -= 10
	}

	oiMil := data.OIValueUSD / 1_000_000
	if oiMil > 50 {
		score += 15
	} else if oiMil > 30 {
		score += 10
	} else if oiMil > 15 {
		score += 5
	}

	return score
}

// ============================================================================
// 初始化函数
// ============================================================================

// Config 配置结构
type Config struct {
	MaxRiskPerTrade     float64 `json:"max_risk_per_trade"`
	TotalRiskBudget     float64 `json:"total_risk_budget"`
	AnalysisIntervalMin int     `json:"analysis_interval_min"`
	BTCETHLeverage      int     `json:"btc_eth_leverage"`
	AltcoinLeverage     int     `json:"altcoin_leverage"`
	DataDir             string  `json:"data_dir"`
	RiskFreeRate        float64 `json:"risk_free_rate"`
}

// Initialize 初始化决策模块
func Initialize(config *Config) error {
	if config == nil {
		config = &Config{
			MaxRiskPerTrade:     0.02,
			TotalRiskBudget:     0.08,
			AnalysisIntervalMin: 15,
			BTCETHLeverage:      10,
			AltcoinLeverage:     5,
			DataDir:             defaultDataDir,
			RiskFreeRate:        0.0,
		}
	}

	// 初始化计划管理器（带持久化）
	if err := InitPlanManager(config.DataDir); err != nil {
		return fmt.Errorf("初始化计划管理器失败: %w", err)
	}

	// 设置夏普比率配置
	SetSharpeConfig(SharpeConfig{
		RiskFreeRate:     config.RiskFreeRate,
		AnnualizeFactor:  252,
		MinTradesForCalc: 10,
	})

	log.Printf("📊 决策模块初始化: 单笔风险=%.1f%%, 总预算=%.1f%%, 分析间隔=%d分钟, 数据目录=%s",
		config.MaxRiskPerTrade*100, config.TotalRiskBudget*100, config.AnalysisIntervalMin, config.DataDir)

	// 输出当前统计
	stats := GetStatistics()
	if stats.TotalTrades > 0 {
		log.Printf("📊 恢复历史统计: 总交易=%d, 胜率=%.1f%%, 夏普=%.2f",
			stats.TotalTrades, stats.WinRate*100, stats.SharpeRatio)
	}

	return nil
}

// Shutdown 关闭决策模块（确保数据保存）
func Shutdown() error {
	if planManager != nil {
		if err := planManager.ForceSave(); err != nil {
			return fmt.Errorf("保存数据失败: %w", err)
		}
		log.Printf("📂 决策模块数据已保存")
	}
	return nil
}

// ============================================================================
// 额外工具函数
// ============================================================================

// GetPerformanceReport 获取完整绩效报告
func GetPerformanceReport() string {
	var sb strings.Builder

	stats := GetStatistics()
	returnsStats := GetReturnsStats()

	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("           📊 绩效报告                  \n")
	sb.WriteString("═══════════════════════════════════════\n\n")

	sb.WriteString("【交易统计】\n")
	sb.WriteString(fmt.Sprintf("  总交易数: %d\n", stats.TotalTrades))
	sb.WriteString(fmt.Sprintf("  盈利交易: %d\n", stats.WinningTrades))
	sb.WriteString(fmt.Sprintf("  亏损交易: %d\n", stats.LosingTrades))
	sb.WriteString(fmt.Sprintf("  胜率: %.2f%%\n\n", stats.WinRate*100))

	sb.WriteString("【盈亏分析】\n")
	sb.WriteString(fmt.Sprintf("  总盈亏: %.2f%%\n", stats.TotalPnL))
	sb.WriteString(fmt.Sprintf("  平均盈利: %.2f%%\n", stats.AverageWin))
	sb.WriteString(fmt.Sprintf("  平均亏损: %.2f%%\n", stats.AverageLoss))
	sb.WriteString(fmt.Sprintf("  盈亏因子: %.2f\n\n", stats.ProfitFactor))

	sb.WriteString("【风险调整收益】\n")
	sb.WriteString(fmt.Sprintf("  夏普比率: %.2f\n", returnsStats["sharpe_ratio"]))
	sb.WriteString(fmt.Sprintf("  索提诺比率: %.2f\n", returnsStats["sortino_ratio"]))
	sb.WriteString(fmt.Sprintf("  最大回撤: %.2f%%\n\n", stats.MaxDrawdown))

	sb.WriteString("【其他指标】\n")
	sb.WriteString(fmt.Sprintf("  平均持仓时间: %.0f 分钟\n", stats.AverageHoldTime))
	sb.WriteString(fmt.Sprintf("  最后更新: %s\n", stats.LastUpdated.Format("2006-01-02 15:04:05")))

	sb.WriteString("\n═══════════════════════════════════════\n")

	// 夏普比率解读
	sharpe := returnsStats["sharpe_ratio"]
	sb.WriteString("\n【夏普比率解读】\n")
	if sharpe > 2.0 {
		sb.WriteString("  ✅ 优秀 (>2.0): 策略表现非常好\n")
	} else if sharpe > 1.0 {
		sb.WriteString("  ✅ 良好 (1.0-2.0): 策略表现不错\n")
	} else if sharpe > 0 {
		sb.WriteString("  ⚠️ 一般 (0-1.0): 策略有改进空间\n")
	} else {
		sb.WriteString("  ❌ 较差 (<0): 策略需要调整\n")
	}

	return sb.String()
}

// ExportData 导出所有数据为JSON
func ExportData() ([]byte, error) {
	planManager.mu.RLock()
	plansCopy := make(map[string]*TradePlan)
	for k, v := range planManager.plans {
		plansCopy[k] = v
	}
	planManager.mu.RUnlock()

	tradeStatsLock.RLock()
	statsCopy := *tradeStats
	tradeStatsLock.RUnlock()

	returnsLock.RLock()
	returnsCopy := make([]float64, len(returnsSeries))
	copy(returnsCopy, returnsSeries)
	returnsLock.RUnlock()

	exportData := struct {
		Plans        map[string]*TradePlan `json:"plans"`
		Statistics   *TradeStatistics      `json:"statistics"`
		Returns      []float64             `json:"returns"`
		ReturnsStats map[string]float64    `json:"returns_stats"`
		ExportedAt   time.Time             `json:"exported_at"`
	}{
		Plans:        plansCopy,
		Statistics:   &statsCopy,
		Returns:      returnsCopy,
		ReturnsStats: GetReturnsStats(),
		ExportedAt:   time.Now(),
	}

	return json.MarshalIndent(exportData, "", "  ")
}

// ImportData 导入数据
func ImportData(data []byte) error {
	var importData struct {
		Plans      map[string]*TradePlan `json:"plans"`
		Statistics *TradeStatistics      `json:"statistics"`
		Returns    []float64             `json:"returns"`
	}

	if err := json.Unmarshal(data, &importData); err != nil {
		return fmt.Errorf("解析导入数据失败: %w", err)
	}

	if importData.Plans != nil {
		planManager.mu.Lock()
		planManager.plans = importData.Plans
		planManager.mu.Unlock()
	}

	if importData.Statistics != nil {
		tradeStatsLock.Lock()
		tradeStats = importData.Statistics
		tradeStatsLock.Unlock()
	}

	if importData.Returns != nil {
		returnsLock.Lock()
		returnsSeries = importData.Returns
		returnsLock.Unlock()
	}

	// 保存到文件
	if err := planManager.ForceSave(); err != nil {
		return fmt.Errorf("保存导入数据失败: %w", err)
	}

	log.Printf("📂 成功导入数据: %d个计划, %d笔交易记录",
		len(importData.Plans), len(importData.Returns))

	return nil
}

// ============================================================================
// 止盈配置结构
// ============================================================================

// TakeProfitEngineConfig 止盈引擎配置
type TakeProfitEngineConfig struct {
	EnableDynamicTP      bool    `json:"enable_dynamic_tp"`
	EnableScaledExit     bool    `json:"enable_scaled_exit"`
	EnableATRTrailing    bool    `json:"enable_atr_trailing"`
	EnableProfitProtect  bool    `json:"enable_profit_protect"`
	PriorityMode         string  `json:"priority_mode"` // "aggressive", "conservative", "balanced"
	MinimumProfitLock    float64 `json:"minimum_profit_lock"`
	ATRTrailingMult      float64 `json:"atr_trailing_mult"`
	ProfitProtectTrigger float64 `json:"profit_protect_trigger"`
	ProfitProtectRatio   float64 `json:"profit_protect_ratio"`
}

// ExitTranche 分批止盈档位
type ExitTranche struct {
	TriggerRR        float64 `json:"trigger_rr"`
	ClosePercent     float64 `json:"close_percent"`
	MoveStopTo       string  `json:"move_stop_to"`
	RequiresMomentum bool    `json:"requires_momentum"`
}

// 默认配置
var defaultTPConfig = &TakeProfitEngineConfig{
	EnableDynamicTP:      true,
	EnableScaledExit:     true,
	EnableATRTrailing:    true,
	EnableProfitProtect:  true,
	PriorityMode:         "balanced",
	MinimumProfitLock:    5.0,
	ATRTrailingMult:      2.5,
	ProfitProtectTrigger: 8.0,
	ProfitProtectRatio:   0.5,
}

// 默认分批止盈配置
var defaultExitTranches = []ExitTranche{
	{TriggerRR: 2.0, ClosePercent: 25, MoveStopTo: "breakeven", RequiresMomentum: false},
	{TriggerRR: 3.0, ClosePercent: 25, MoveStopTo: "lock_1r", RequiresMomentum: false},
	{TriggerRR: 5.0, ClosePercent: 25, MoveStopTo: "lock_2r", RequiresMomentum: true},
	{TriggerRR: 8.0, ClosePercent: 25, MoveStopTo: "lock_3r", RequiresMomentum: true},
}

// GetTakeProfitConfig 获取止盈配置（可从外部配置加载）
func GetTakeProfitConfig() *TakeProfitEngineConfig {
	return defaultTPConfig
}

// ============================================================================
// 辅助方法
// ============================================================================

// getEffectiveStopLoss 获取有效止损价
func (e *PositionEvaluator) getEffectiveStopLoss() float64 {
	if e.Plan == nil {
		return 0
	}
	if e.Plan.CurrentStopLoss > 0 {
		return e.Plan.CurrentStopLoss
	}
	return e.Plan.StopLoss
}

// calculateCurrentRR 计算当前风险回报比
func (e *PositionEvaluator) calculateCurrentRR() float64 {
	if e.Plan == nil {
		return 0
	}

	riskDistance := math.Abs(e.Plan.EntryPrice - e.Plan.StopLoss)
	if riskDistance == 0 {
		return 0
	}

	currentDistance := math.Abs(e.MarketData.CurrentPrice - e.Plan.EntryPrice)
	return currentDistance / riskDistance
}

// getATR 获取ATR值
func (e *PositionEvaluator) getATR() float64 {
	if e.MarketData.LongerTermContext != nil && e.MarketData.LongerTermContext.ATR14 > 0 {
		return e.MarketData.LongerTermContext.ATR14
	}
	// 默认使用价格的2%作为ATR估算
	return e.MarketData.CurrentPrice * 0.02
}

// updatePeakData 更新峰值数据
func (e *PositionEvaluator) updatePeakData() {
	if e.Plan == nil {
		return
	}

	currentPrice := e.MarketData.CurrentPrice
	pnlPct := e.Position.UnrealizedPnLPct

	// 更新峰值价格
	if e.Plan.Direction == "long" {
		if currentPrice > e.Plan.PeakPrice || e.Plan.PeakPrice == 0 {
			e.Plan.PeakPrice = currentPrice
		}
	} else {
		if currentPrice < e.Plan.PeakPrice || e.Plan.PeakPrice == 0 {
			e.Plan.PeakPrice = currentPrice
		}
	}

	// 更新峰值盈亏
	if pnlPct > e.Plan.PeakPnLPercent {
		e.Plan.PeakPnLPercent = pnlPct
	}
}

// ============================================================================
// 利润保护机制
// ============================================================================

// checkProfitProtection 检查利润保护条件
func (e *PositionEvaluator) checkProfitProtection(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	pnlPct := e.Position.UnrealizedPnLPct
	peakPnL := e.Plan.PeakPnLPercent

	// 如果从未盈利超过触发阈值，不触发保护
	if peakPnL < config.ProfitProtectTrigger {
		return nil
	}

	// 计算保护线：峰值盈利的一定比例
	protectLine := peakPnL * config.ProfitProtectRatio

	// 如果当前盈利跌破保护线
	if pnlPct < protectLine {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 峰值盈利%.2f%%, 当前%.2f%%, 保护线%.2f%%",
				peakPnL, pnlPct, protectLine),
		}
	}

	// 如果曾经盈利很多但现在接近回本，也触发保护
	if peakPnL >= 10.0 && pnlPct < 2.0 {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 曾盈利%.2f%%，现仅剩%.2f%%，保护残余利润",
				peakPnL, pnlPct),
		}
	}

	return nil
}

// ============================================================================
// ATR跟踪止盈
// ============================================================================

// evaluateATRTrailingTakeProfit ATR跟踪止盈评估
func (e *PositionEvaluator) evaluateATRTrailingTakeProfit(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentPrice := e.MarketData.CurrentPrice
	atr := e.getATR()
	trailingDistance := atr * config.ATRTrailingMult

	if e.Plan.Direction == "long" {
		// 多单：从最高点回落超过ATR距离则止盈
		peakPrice := e.Plan.PeakPrice
		if peakPrice == 0 {
			peakPrice = currentPrice
		}

		trailingTP := peakPrice - trailingDistance

		// 确保跟踪止盈价高于入场价（保证盈利）
		if trailingTP > e.Plan.EntryPrice && currentPrice <= trailingTP {
			profit := (trailingTP - e.Plan.EntryPrice) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📈 ATR跟踪止盈: 从高点%.4f回落%.4f (%.1fATR), 锁定利润%.2f%%",
					peakPrice, trailingDistance, config.ATRTrailingMult, profit),
			}
		}
	} else {
		// 空单：从最低点反弹超过ATR距离则止盈
		peakPrice := e.Plan.PeakPrice
		if peakPrice == 0 {
			peakPrice = currentPrice
		}

		trailingTP := peakPrice + trailingDistance

		// 确保跟踪止盈价低于入场价（保证盈利）
		if trailingTP < e.Plan.EntryPrice && currentPrice >= trailingTP {
			profit := (e.Plan.EntryPrice - trailingTP) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📉 ATR跟踪止盈: 从低点%.4f反弹%.4f (%.1fATR), 锁定利润%.2f%%",
					peakPrice, trailingDistance, config.ATRTrailingMult, profit),
			}
		}
	}

	return nil
}

// ============================================================================
// 智能分批止盈
// ============================================================================

// evaluateScaledExit 智能分批止盈评估
func (e *PositionEvaluator) evaluateScaledExit() *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentRR := e.calculateCurrentRR()

	// 初始化已执行档位记录
	if e.Plan.ExecutedTranches == nil {
		e.Plan.ExecutedTranches = make(map[int]bool)
	}

	for i, tranche := range defaultExitTranches {
		// 检查是否已执行该档
		if e.Plan.ExecutedTranches[i] {
			continue
		}

		if currentRR >= tranche.TriggerRR {
			// 检查动量条件
			if tranche.RequiresMomentum && !e.checkMomentumConfirmation() {
				log.Printf("📊 %s 达到RR %.1f但动量不支持，等待更好时机", e.Plan.Symbol, currentRR)
				continue
			}

			// 计算新止损价
			newStopLoss := e.calculateStopLossForTranche(tranche.MoveStopTo)

			// 标记该档已执行
			e.Plan.ExecutedTranches[i] = true

			return &EvaluationResult{
				Action:          "partial_close",
				ClosePercentage: tranche.ClosePercent,
				NewStopLoss:     newStopLoss,
				Reason: fmt.Sprintf("📊 分批止盈第%d档: RR %.2f:1, 平仓%.0f%%, 止损移至%s",
					i+1, currentRR, tranche.ClosePercent, tranche.MoveStopTo),
			}
		}
	}

	return nil
}

// checkMomentumConfirmation 检查动量确认
func (e *PositionEvaluator) checkMomentumConfirmation() bool {
	// 1. RSI检查
	rsi := e.MarketData.CurrentRSI14
	if e.Plan.Direction == "long" {
		// 多单：RSI超买区（>75）动量减弱
		if rsi > 75 {
			return false
		}
	} else {
		// 空单：RSI超卖区（<25）动量减弱
		if rsi < 25 {
			return false
		}
	}

	// 2. MACD柱状图检查
	if e.MarketData.IntradaySeries != nil {
		macdHist := market.GetLastValue(e.MarketData.IntradaySeries.MACDHist)
		prevHist := e.getSecondLastMACDHist()

		if e.Plan.Direction == "long" {
			// 多单：MACD柱状图应该为正且扩张
			if macdHist < 0 || macdHist < prevHist {
				return false
			}
		} else {
			// 空单：MACD柱状图应该为负且扩张（更负）
			if macdHist > 0 || macdHist > prevHist {
				return false
			}
		}
	}

	// 3. ADX趋势强度检查
	if e.MarketData.CurrentADX < 20 {
		return false // 趋势太弱
	}

	return true
}

// getSecondLastMACDHist 获取倒数第二个MACD柱状图值
func (e *PositionEvaluator) getSecondLastMACDHist() float64 {
	if e.MarketData.IntradaySeries == nil {
		return 0
	}
	hist := e.MarketData.IntradaySeries.MACDHist
	if len(hist) < 2 {
		return 0
	}
	return hist[len(hist)-2]
}

// calculateStopLossForTranche 根据档位计算新止损价
func (e *PositionEvaluator) calculateStopLossForTranche(moveStopTo string) float64 {
	if e.Plan == nil {
		return 0
	}

	entryPrice := e.Plan.EntryPrice
	riskDistance := math.Abs(entryPrice - e.Plan.StopLoss)

	switch moveStopTo {
	case "breakeven":
		// 移动到保本
		return entryPrice

	case "lock_1r":
		// 锁定1倍风险距离的利润
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance
		}
		return entryPrice - riskDistance

	case "lock_2r":
		// 锁定2倍风险距离的利润
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance*2
		}
		return entryPrice - riskDistance*2

	case "lock_3r":
		// 锁定3倍风险距离的利润
		if e.Plan.Direction == "long" {
			return entryPrice + riskDistance*3
		}
		return entryPrice - riskDistance*3

	default:
		return e.getEffectiveStopLoss()
	}
}

// ============================================================================
// 移动止损
// ============================================================================

// evaluateTrailingStop 评估移动止损
func (e *PositionEvaluator) evaluateTrailingStop() *EvaluationResult {
	newSL := e.calculateTrailingStop()

	if newSL <= 0 {
		return nil
	}

	effectiveSL := e.getEffectiveStopLoss()
	currentPrice := e.MarketData.CurrentPrice

	shouldUpdate := false

	if e.Plan.Direction == "long" {
		// 多单：新止损必须高于原止损，且低于当前价格
		if newSL > effectiveSL && newSL < currentPrice {
			shouldUpdate = true
		}
	} else {
		// 空单：新止损必须低于原止损，且高于当前价格
		if newSL < effectiveSL && newSL > currentPrice {
			shouldUpdate = true
		}
	}

	if shouldUpdate {
		return &EvaluationResult{
			Action:           "update_stop_loss",
			NewStopLoss:      newSL,
			Reason:           fmt.Sprintf("📈 移动止损: %.4f → %.4f (盈利%.2f%%)", effectiveSL, newSL, e.Position.UnrealizedPnLPct),
			ShouldUpdatePeak: true,
		}
	}

	return nil
}

// ============================================================================
// 动态止盈调整
// ============================================================================

// adjustDynamicTakeProfit 动态调整止盈价格
func (e *PositionEvaluator) adjustDynamicTakeProfit(config *TakeProfitEngineConfig) {
	if e.Plan == nil {
		return
	}

	baseTP := e.Plan.TakeProfit
	entryPrice := e.Plan.EntryPrice

	// 1. 获取市场状态调整因子
	marketState, confidence := market.GetMarketState(e.MarketData)
	stateMultiplier := e.getStateMultiplier(marketState, confidence)

	// 2. 波动率调整因子
	currentATR := e.getATR()
	// 如果没有记录入场ATR，使用当前ATR
	entryATR := e.Plan.EntryATR
	if entryATR == 0 {
		entryATR = currentATR
		e.Plan.EntryATR = currentATR
	}

	volAdjustment := 1.0
	if entryATR > 0 {
		volatilityRatio := currentATR / entryATR
		if volatilityRatio > 1.3 {
			volAdjustment = 1.2 // 波动率大幅上升，扩大目标
		} else if volatilityRatio < 0.7 {
			volAdjustment = 0.85 // 波动率下降，收紧目标
		}
	}

	// 3. 时间衰减因子
	timeAdjustment := 1.0
	holdHours := time.Since(e.Plan.CreatedAt).Hours()
	maxHoldHours := 72.0 // 最大持仓72小时

	if holdHours > maxHoldHours {
		// 超过最大持仓时间，逐步降低止盈目标
		decayFactor := 1.0 - (holdHours-maxHoldHours)/maxHoldHours*0.3
		timeAdjustment = math.Max(0.7, decayFactor)
	}

	// 计算调整后的止盈价
	originalDistance := math.Abs(baseTP - entryPrice)
	adjustedDistance := originalDistance * stateMultiplier * volAdjustment * timeAdjustment

	var newTP float64
	if e.Plan.Direction == "long" {
		newTP = entryPrice + adjustedDistance
	} else {
		newTP = entryPrice - adjustedDistance
	}

	// 只有变化超过0.5%才更新
	if math.Abs(newTP-e.Plan.TakeProfit)/e.Plan.TakeProfit > 0.005 {
		log.Printf("📊 %s 动态止盈调整: %.4f → %.4f (状态:%.2f, 波动:%.2f, 时间:%.2f)",
			e.Plan.Symbol, e.Plan.TakeProfit, newTP, stateMultiplier, volAdjustment, timeAdjustment)
		e.Plan.TakeProfit = newTP
		planManager.autoSaveIfEnabled()
	}
}

// getStateMultiplier 获取市场状态乘数
func (e *PositionEvaluator) getStateMultiplier(state string, confidence int) float64 {
	switch state {
	case "STRONG_UPTREND":
		if e.Plan.Direction == "long" && confidence >= 80 {
			return 1.5 // 强上升趋势做多，扩大目标
		}
		return 1.2
	case "STRONG_DOWNTREND":
		if e.Plan.Direction == "short" && confidence >= 80 {
			return 1.5 // 强下降趋势做空，扩大目标
		}
		return 1.2
	case "UPTREND":
		if e.Plan.Direction == "long" {
			return 1.3
		}
		return 0.9 // 上升趋势中的空单，收紧目标
	case "DOWNTREND":
		if e.Plan.Direction == "short" {
			return 1.3
		}
		return 0.9 // 下降趋势中的多单，收紧目标
	case "RANGING", "CONSOLIDATION":
		return 0.8 // 震荡市，收紧目标
	default:
		return 1.0
	}
}

// 🔧 新增: UpdatePlanPeakData 更新峰值数据（专用方法）
func (m *TradePlanManager) UpdatePlanPeakData(symbol string, currentPrice float64, currentPnLPct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.plans[symbol]
	if !ok {
		return
	}

	updated := false

	// 更新峰值盈亏百分比
	if currentPnLPct > plan.PeakPnLPercent {
		plan.PeakPnLPercent = currentPnLPct
		updated = true
	}

	// 更新峰值价格
	if plan.Direction == "long" {
		if currentPrice > plan.PeakPrice || plan.PeakPrice == 0 {
			plan.PeakPrice = currentPrice
			updated = true
		}
	} else {
		if plan.PeakPrice == 0 || currentPrice < plan.PeakPrice {
			plan.PeakPrice = currentPrice
			updated = true
		}
	}

	if updated {
		log.Printf("📊 %s 峰值更新: PeakPrice=%.4f, PeakPnL=%.2f%%",
			symbol, plan.PeakPrice, plan.PeakPnLPercent)
	}
}

// 🔧 新增: UpdatePlanTakeProfit 更新动态止盈价格
func (m *TradePlanManager) UpdatePlanTakeProfit(symbol string, newTP float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.plans[symbol]
	if !ok {
		return
	}

	// 记录原始止盈价（如果还没记录）
	if plan.OriginalTakeProfit == 0 {
		plan.OriginalTakeProfit = plan.TakeProfit
	}

	oldTP := plan.TakeProfit
	plan.TakeProfit = newTP
	plan.LastTPAdjustTime = time.Now()

	log.Printf("📈 %s 动态止盈调整: %.4f → %.4f", symbol, oldTP, newTP)
}

// 🔧 新增: MarkTrancheExecuted 标记分批止盈档位已执行
func (m *TradePlanManager) MarkTrancheExecuted(symbol string, trancheIndex int, closePercent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.plans[symbol]
	if !ok {
		return
	}

	if plan.ExecutedTranches == nil {
		plan.ExecutedTranches = make(map[int]bool)
	}

	plan.ExecutedTranches[trancheIndex] = true
	plan.LastExecutedTranche = trancheIndex
	plan.TotalClosedPercent += closePercent

	log.Printf("📊 %s 分批止盈: 档位%d已执行, 累计平仓%.0f%%",
		symbol, trancheIndex+1, plan.TotalClosedPercent)
}

// 🔧 新增: UpdatePlanEntryATR 更新入场时ATR
func (m *TradePlanManager) UpdatePlanEntryATR(symbol string, atr float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if plan, ok := m.plans[symbol]; ok {
		if plan.EntryATR == 0 {
			plan.EntryATR = atr
			log.Printf("📊 %s 记录入场ATR: %.4f", symbol, atr)
		}
	}
}

// 🔧 新增: GetPlanPeakData 获取峰值数据（用于日志和显示）
func (m *TradePlanManager) GetPlanPeakData(symbol string) (peakPrice, peakPnL float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if plan, ok := m.plans[symbol]; ok {
		return plan.PeakPrice, plan.PeakPnLPercent
	}
	return 0, 0
}

// 🔧 新增: IsTrancheExecuted 检查档位是否已执行
func (m *TradePlanManager) IsTrancheExecuted(symbol string, trancheIndex int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if plan, ok := m.plans[symbol]; ok {
		if plan.ExecutedTranches == nil {
			return false
		}
		return plan.ExecutedTranches[trancheIndex]
	}
	return false
}

// 🔧 修复: checkProfitProtectionFixed 利润保护检查（使用Plan中的峰值）
func (e *PositionEvaluator) checkProfitProtectionFixed(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	pnlPct := e.Position.UnrealizedPnLPct
	peakPnL := e.Plan.PeakPnLPercent

	// 如果当前盈利超过记录的峰值，说明峰值需要更新（但这里只做检查，不修改）
	// 实际更新由外部 evaluateExistingPositions 处理
	if pnlPct > peakPnL {
		peakPnL = pnlPct // 使用当前值作为临时峰值进行计算
	}

	// 如果从未盈利超过触发阈值，不触发保护
	if peakPnL < config.ProfitProtectTrigger {
		return nil
	}

	// 计算保护线：峰值盈利的一定比例
	protectLine := peakPnL * config.ProfitProtectRatio

	// 如果当前盈利跌破保护线
	if pnlPct < protectLine {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 峰值盈利%.2f%%, 当前%.2f%%, 保护线%.2f%%",
				peakPnL, pnlPct, protectLine),
			ShouldUpdatePeak: false, // 平仓不需要更新峰值
		}
	}

	// 如果曾经盈利很多但现在接近回本，也触发保护
	if peakPnL >= 10.0 && pnlPct < 2.0 {
		return &EvaluationResult{
			Action: "close",
			Reason: fmt.Sprintf("🛡️ 利润保护触发: 曾盈利%.2f%%，现仅剩%.2f%%，保护残余利润",
				peakPnL, pnlPct),
			ShouldUpdatePeak: false,
		}
	}

	return nil
}

// 🔧 修复: evaluateATRTrailingTakeProfitFixed ATR跟踪止盈（使用Plan中的峰值价格）
func (e *PositionEvaluator) evaluateATRTrailingTakeProfitFixed(config *TakeProfitEngineConfig) *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentPrice := e.MarketData.CurrentPrice
	atr := e.getATR()
	trailingDistance := atr * config.ATRTrailingMult

	// 使用Plan中记录的峰值价格
	peakPrice := e.Plan.PeakPrice

	// 如果当前价格创新高/新低，更新临时峰值（实际更新由外部处理）
	if e.Plan.Direction == "long" {
		if peakPrice == 0 || currentPrice > peakPrice {
			peakPrice = currentPrice
		}
	} else {
		if peakPrice == 0 || currentPrice < peakPrice {
			peakPrice = currentPrice
		}
	}

	if e.Plan.Direction == "long" {
		trailingTP := peakPrice - trailingDistance

		// 确保跟踪止盈价高于入场价（保证盈利）
		if trailingTP > e.Plan.EntryPrice && currentPrice <= trailingTP {
			profit := (trailingTP - e.Plan.EntryPrice) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📈 ATR跟踪止盈: 从高点%.4f回落%.4f (%.1fATR), 锁定利润%.2f%%",
					peakPrice, trailingDistance, config.ATRTrailingMult, profit),
				ShouldUpdatePeak: false,
			}
		}
	} else {
		trailingTP := peakPrice + trailingDistance

		if trailingTP < e.Plan.EntryPrice && currentPrice >= trailingTP {
			profit := (e.Plan.EntryPrice - trailingTP) / e.Plan.EntryPrice * 100
			return &EvaluationResult{
				Action: "close",
				Reason: fmt.Sprintf("📉 ATR跟踪止盈: 从低点%.4f反弹%.4f (%.1fATR), 锁定利润%.2f%%",
					peakPrice, trailingDistance, config.ATRTrailingMult, profit),
				ShouldUpdatePeak: false,
			}
		}
	}

	return nil
}

// 🔧 修复: evaluateScaledExitFixed 智能分批止盈（通过planManager检查档位）
func (e *PositionEvaluator) evaluateScaledExitFixed() *EvaluationResult {
	if e.Plan == nil {
		return nil
	}

	currentRR := e.calculateCurrentRR()

	for i, tranche := range defaultExitTranches {
		// 🔧 通过 planManager 检查档位是否已执行
		if planManager.IsTrancheExecuted(e.Symbol, i) {
			continue
		}

		if currentRR >= tranche.TriggerRR {
			// 检查动量条件
			if tranche.RequiresMomentum && !e.checkMomentumConfirmation() {
				log.Printf("📊 %s 达到RR %.1f但动量不支持，等待更好时机", e.Symbol, currentRR)
				continue
			}

			// 计算新止损价
			newStopLoss := e.calculateStopLossForTranche(tranche.MoveStopTo)

			return &EvaluationResult{
				Action:          "partial_close",
				ClosePercentage: tranche.ClosePercent,
				NewStopLoss:     newStopLoss,
				TrancheIndex:    i, // 🔧 记录档位索引
				Reason: fmt.Sprintf("📊 分批止盈第%d档: RR %.2f:1, 平仓%.0f%%, 止损移至%s",
					i+1, currentRR, tranche.ClosePercent, tranche.MoveStopTo),
				ShouldUpdatePeak: false,
			}
		}
	}

	return nil
}

// 🔧 新增: calculateDynamicTakeProfit 计算动态止盈价格（返回新值而不是直接修改）
func (e *PositionEvaluator) calculateDynamicTakeProfit(config *TakeProfitEngineConfig) float64 {
	if e.Plan == nil {
		return 0
	}

	// 使用原始止盈价作为基准
	baseTP := e.Plan.OriginalTakeProfit
	if baseTP == 0 {
		baseTP = e.Plan.TakeProfit
	}
	entryPrice := e.Plan.EntryPrice

	// 1. 获取市场状态调整因子
	marketState, confidence := market.GetMarketState(e.MarketData)
	stateMultiplier := e.getStateMultiplier(marketState, confidence)

	// 2. 波动率调整因子
	currentATR := e.getATR()
	entryATR := e.Plan.EntryATR
	if entryATR == 0 {
		entryATR = currentATR
	}

	volAdjustment := 1.0
	if entryATR > 0 {
		volatilityRatio := currentATR / entryATR
		if volatilityRatio > 1.3 {
			volAdjustment = 1.2
		} else if volatilityRatio < 0.7 {
			volAdjustment = 0.85
		}
	}

	// 3. 时间衰减因子
	timeAdjustment := 1.0
	holdHours := time.Since(e.Plan.CreatedAt).Hours()
	maxHoldHours := 72.0

	if holdHours > maxHoldHours {
		decayFactor := 1.0 - (holdHours-maxHoldHours)/maxHoldHours*0.3
		timeAdjustment = math.Max(0.7, decayFactor)
	}

	// 计算调整后的止盈价
	originalDistance := math.Abs(baseTP - entryPrice)
	adjustedDistance := originalDistance * stateMultiplier * volAdjustment * timeAdjustment

	var newTP float64
	if e.Plan.Direction == "long" {
		newTP = entryPrice + adjustedDistance
	} else {
		newTP = entryPrice - adjustedDistance
	}

	// 只有变化超过0.5%才返回新值
	if math.Abs(newTP-e.Plan.TakeProfit)/e.Plan.TakeProfit > 0.005 {
		return newTP
	}

	return 0 // 返回0表示不需要更新
}

// ============================================================================
// 🔧 修复6: 持久化结构更新
// ============================================================================

// PersistentData 持久化数据结构
type PersistentData struct {
	Plans        map[string]*TradePlan `json:"plans"`
	Statistics   *TradeStatistics      `json:"statistics"`
	Returns      []float64             `json:"returns"`
	ClosedTrades []ClosedTradeRecord   `json:"closed_trades"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

// ============================================================================
// 数据结构定义
// ============================================================================

// ClosedTradeRecord 已平仓交易记录
type ClosedTradeRecord struct {
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	CloseReason    string    `json:"close_reason"`
	EntryPrice     float64   `json:"entry_price"`
	ExitPrice      float64   `json:"exit_price"`
	Quantity       float64   `json:"quantity"`
	Leverage       int       `json:"leverage"`
	RealizedPnL    float64   `json:"realized_pnl"`
	PnLPercent     float64   `json:"pnl_percent"`
	HoldingMinutes int64     `json:"holding_minutes"`
	EntryTime      time.Time `json:"entry_time"`
	ExitTime       time.Time `json:"exit_time"`
	Commission     float64   `json:"commission"`
	Direction      string    `json:"direction"`
	PnLUSD         float64   `json:"pnl_usd"`
	ExitReason     string    `json:"exit_reason"`
	PeakPnLPercent float64   `json:"peak_pnl_percent"` // 🆕 记录峰值盈利
	ClosedAt       time.Time `json:"closed_at"`
}

var (
	closedTrades     []ClosedTradeRecord
	closedTradesLock sync.RWMutex
)

// ============================================================================
// 🔧 修复7: 平仓回调增强
// ============================================================================

// OnPositionClosed 平仓成功后调用（增强版，记录峰值信息）
func OnPositionClosed(symbol string, exitPrice float64, pnlPercent float64, pnlUSD float64, reason string) {
	// 获取计划信息用于记录
	plan := planManager.GetPlan(symbol)

	var record ClosedTradeRecord
	record.Symbol = symbol
	record.ExitPrice = exitPrice
	record.PnLPercent = pnlPercent
	record.PnLUSD = pnlUSD
	record.ExitReason = reason
	record.ClosedAt = time.Now()

	if plan != nil {
		record.Direction = plan.Direction
		record.EntryPrice = plan.EntryPrice
		record.PeakPnLPercent = plan.PeakPnLPercent
		record.HoldingMinutes = int64(time.Since(plan.CreatedAt).Minutes())
	}

	// 记录已平仓交易
	closedTradesLock.Lock()
	closedTrades = append(closedTrades, record)
	// 保留最近100笔
	if len(closedTrades) > 100 {
		closedTrades = closedTrades[len(closedTrades)-100:]
	}
	closedTradesLock.Unlock()

	// 更新统计
	UpdateStatistics(pnlPercent, float64(record.HoldingMinutes))
	// 记录收益率用于夏普比率计算（returnsLock）
	AddReturn(pnlPercent)

	// 移除计划
	planManager.RemovePlan(symbol)

	log.Printf("✅ 平仓成功: %s 盈亏%.2f%% (峰值%.2f%%), 原因: %s",
		symbol, pnlPercent, record.PeakPnLPercent, reason)
}

// ============================================================================
// 🔧 修复11: 单元测试辅助函数
// ============================================================================

// TestEvaluateTakeProfit 测试止盈逻辑（供单元测试使用）
func TestEvaluateTakeProfit(
	symbol string,
	direction string,
	entryPrice float64,
	currentPrice float64,
	stopLoss float64,
	takeProfit float64,
	peakPrice float64,
	peakPnLPct float64,
	pnlPct float64,
	executedTranches map[int]bool,
) *EvaluationResult {

	// 创建模拟的Plan
	plan := &TradePlan{
		Symbol:             symbol,
		Direction:          direction,
		EntryPrice:         entryPrice,
		StopLoss:           stopLoss,
		TakeProfit:         takeProfit,
		OriginalTakeProfit: takeProfit,
		CurrentStopLoss:    stopLoss,
		PeakPrice:          peakPrice,
		PeakPnLPercent:     peakPnLPct,
		ExecutedTranches:   executedTranches,
		CreatedAt:          time.Now().Add(-2 * time.Hour), // 假设持仓2小时
		MinHoldMinutes:     30,
	}

	if plan.ExecutedTranches == nil {
		plan.ExecutedTranches = make(map[int]bool)
	}

	// 创建模拟的Position
	position := &PositionInfo{
		Symbol:           symbol,
		Side:             direction,
		EntryPrice:       entryPrice,
		MarkPrice:        currentPrice,
		UnrealizedPnLPct: pnlPct,
		UpdateTime:       time.Now().Add(-2 * time.Hour).UnixMilli(),
	}

	// 创建模拟的MarketData
	marketData := &market.Data{
		CurrentPrice: currentPrice,
		CurrentRSI14: 50, // 中性
		CurrentADX:   30, // 有趋势
	}

	// 临时设置Plan用于测试
	if planManager == nil {
		planManager = &TradePlanManager{
			plans:    make(map[string]*TradePlan),
			autoSave: false,
		}
	}
	planManager.mu.Lock()
	planManager.plans[symbol] = plan
	planManager.mu.Unlock()

	// 创建评估器并评估
	evaluator := &PositionEvaluator{
		Position:   position,
		Plan:       plan,
		MarketData: marketData,
		Symbol:     symbol,
	}

	result := evaluator.Evaluate()

	// 清理
	planManager.mu.Lock()
	delete(planManager.plans, symbol)
	planManager.mu.Unlock()

	return result
}
