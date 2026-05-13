package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ============================================================================
// 常量和全局变量
// ============================================================================

const defaultDataDir = "./data"
const plansFileName = "trade_plans.json"

var (
	planManager      *TradePlanManager
	tradeStats       = &TradeStatistics{}
	tradeStatsLock   sync.RWMutex
	returnsSeries    []float64
	returnsLock      sync.RWMutex
	closedTrades     []ClosedTradeRecord
	closedTradesLock sync.RWMutex
	sharpeConfig     = SharpeConfig{
		RiskFreeRate:     0.0,
		AnnualizeFactor:  252,
		MinTradesForCalc: 10,
	}
)

// ============================================================================
// 初始化
// ============================================================================

// InitPlanManager 初始化计划管理器
func InitPlanManager(dataDir string) error {
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	filePath := filepath.Join(dataDir, plansFileName)

	planManager = &TradePlanManager{
		plans:              make(map[string]*TradePlan),
		positionStartTimes: make(map[string]int64),
		filePath:           filePath,
		autoSave:           true,
	}

	InitConditionParser()

	if err := planManager.loadFromFile(); err != nil {
		log.Printf("⚠️ 加载交易计划失败: %v", err)
	} else {
		log.Printf("📂 成功加载 %d 个交易计划", len(planManager.plans))
	}

	return nil
}

// ============================================================================
// 计划管理器方法
// ============================================================================

func (m *TradePlanManager) loadFromFile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var persistentData PersistentData
	if err := json.Unmarshal(data, &persistentData); err != nil {
		return fmt.Errorf("解析JSON失败: %w", err)
	}

	if persistentData.Plans != nil {
		m.plans = persistentData.Plans

		for _, plan := range m.plans {
			if plan.ExecutedTranches == nil {
				plan.ExecutedTranches = make(map[int]bool)
			}
			if plan.OriginalTakeProfit == 0 {
				plan.OriginalTakeProfit = plan.TakeProfit
			}
		}
		if m.migrateLegacyScopedPlanKeys() {
			backupPath := m.filePath + ".legacy.bak"
			if err := os.WriteFile(backupPath, data, 0644); err != nil {
				log.Printf("⚠️ 备份旧交易计划失败: %v", err)
			} else {
				log.Printf("📦 已备份旧交易计划: %s", backupPath)
			}
		}
	}
	if persistentData.PositionStartTimes != nil {
		m.positionStartTimes = persistentData.PositionStartTimes
	}
	if m.positionStartTimes == nil {
		m.positionStartTimes = make(map[string]int64)
	}

	if persistentData.Statistics != nil {
		tradeStatsLock.Lock()
		tradeStats = persistentData.Statistics
		tradeStatsLock.Unlock()
	}

	if persistentData.Returns != nil {
		returnsLock.Lock()
		returnsSeries = persistentData.Returns
		returnsLock.Unlock()
	}

	if persistentData.ClosedTrades != nil {
		closedTradesLock.Lock()
		closedTrades = persistentData.ClosedTrades
		closedTradesLock.Unlock()
	}

	// 恢复熔断状态
	if persistentData.CircuitBreaker != nil && persistentData.CircuitBreaker.IsTriggered {
		cooldownEnd := persistentData.CircuitBreaker.TriggerTime.Add(
			time.Duration(persistentData.CircuitBreaker.CooldownMinutes) * time.Minute)
		if time.Now().Before(cooldownEnd) {
			// 冷却未过期，恢复熔断状态
			SetCircuitBreakerState(persistentData.CircuitBreaker)
			log.Printf("🔄 恢复熔断状态: %s (剩余冷却 %d 分钟)",
				persistentData.CircuitBreaker.TriggerReason,
				int(cooldownEnd.Sub(time.Now()).Minutes()))
		}
		// 冷却已过期则忽略，状态保持默认未触发
	}

	return nil
}

func makePlanKey(traderID, symbol, side string) string {
	if traderID == "" || symbol == "" || side == "" {
		return symbol
	}
	return traderID + ":" + symbol + ":" + side
}

func planSide(plan *TradePlan) string {
	if plan == nil {
		return ""
	}
	if plan.Direction != "" {
		return plan.Direction
	}
	return ""
}

func (m *TradePlanManager) candidatePlanKeys(traderID, symbol, side string) []string {
	var keys []string
	if traderID != "" && symbol != "" && side != "" {
		keys = append(keys, makePlanKey(traderID, symbol, side))
	}
	if traderID != "" && symbol != "" && side == "" {
		keys = append(keys, makePlanKey(traderID, symbol, "long"), makePlanKey(traderID, symbol, "short"))
	}
	if side != "" {
		keys = append(keys, makePlanKey("", symbol, side))
	}
	keys = append(keys, symbol)
	seen := make(map[string]bool)
	var result []string
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, key)
	}
	return result
}

func makePositionStartKey(traderID, symbol, side string) string {
	if traderID != "" && symbol != "" && side != "" {
		return traderID + ":" + symbol + ":" + side
	}
	if symbol != "" && side != "" {
		return symbol + ":" + side
	}
	return symbol
}

func (m *TradePlanManager) candidatePositionStartKeys(traderID, symbol, side string) []string {
	keys := []string{}
	if traderID != "" && symbol != "" && side != "" {
		keys = append(keys, makePositionStartKey(traderID, symbol, side))
	}
	if traderID != "" && symbol != "" && side == "" {
		keys = append(keys,
			makePositionStartKey(traderID, symbol, "long"),
			makePositionStartKey(traderID, symbol, "short"),
		)
	}
	if symbol != "" && side != "" {
		keys = append(keys, makePositionStartKey("", symbol, side))
	}
	if symbol != "" && side == "" {
		keys = append(keys,
			makePositionStartKey("", symbol, "long"),
			makePositionStartKey("", symbol, "short"),
		)
	}
	if symbol != "" {
		keys = append(keys, symbol)
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, key)
	}
	return result
}

func (m *TradePlanManager) migrateLegacyScopedPlanKeys() bool {
	migrated := false
	for key, plan := range m.plans {
		if plan == nil || plan.TraderID == "" || plan.Symbol == "" || plan.Direction == "" {
			continue
		}
		scopedKey := makePlanKey(plan.TraderID, plan.Symbol, plan.Direction)
		if key == scopedKey {
			continue
		}
		if _, exists := m.plans[scopedKey]; exists {
			continue
		}
		m.plans[scopedKey] = plan
		delete(m.plans, key)
		migrated = true
	}
	return migrated
}

func (m *TradePlanManager) saveToFile() error {
	m.mu.RLock()
	plansCopy := make(map[string]*TradePlan)
	for k, v := range m.plans {
		plansCopy[k] = v
	}
	positionStartTimesCopy := make(map[string]int64)
	for k, v := range m.positionStartTimes {
		positionStartTimesCopy[k] = v
	}
	m.mu.RUnlock()

	tradeStatsLock.RLock()
	statsCopy := *tradeStats
	tradeStatsLock.RUnlock()

	returnsLock.RLock()
	returnsCopy := make([]float64, len(returnsSeries))
	copy(returnsCopy, returnsSeries)
	returnsLock.RUnlock()

	closedTradesLock.RLock()
	closedTradesCopy := make([]ClosedTradeRecord, len(closedTrades))
	copy(closedTradesCopy, closedTrades)
	closedTradesLock.RUnlock()

	persistentData := PersistentData{
		Plans:              plansCopy,
		PositionStartTimes: positionStartTimesCopy,
		Statistics:         &statsCopy,
		Returns:            returnsCopy,
		ClosedTrades:       closedTradesCopy,
		CircuitBreaker:     GetCircuitBreakerState(),
		UpdatedAt:          time.Now(),
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

func (m *TradePlanManager) autoSaveIfEnabled() {
	if !m.autoSave {
		return
	}
	if err := m.saveToFile(); err != nil {
		m.lastSaveErr = err
		log.Printf("⚠️ 自动保存失败: %v", err)
	}
}

func (m *TradePlanManager) GetPositionStartTimeScoped(traderID, symbol, side string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, key := range m.candidatePositionStartKeys(traderID, symbol, side) {
		if startTime, exists := m.positionStartTimes[key]; exists && startTime > 0 {
			return startTime
		}
	}
	return 0
}

func (m *TradePlanManager) SetPositionStartTimeScoped(traderID, symbol, side string, startTime int64) {
	if startTime <= 0 || symbol == "" {
		return
	}

	m.mu.Lock()
	if m.positionStartTimes == nil {
		m.positionStartTimes = make(map[string]int64)
	}
	m.positionStartTimes[makePositionStartKey(traderID, symbol, side)] = startTime
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

func (m *TradePlanManager) RemovePositionStartTimeScoped(traderID, symbol, side string) {
	m.mu.Lock()
	for _, key := range m.candidatePositionStartKeys(traderID, symbol, side) {
		delete(m.positionStartTimes, key)
	}
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// GetPlan 获取交易计划（返回深拷贝）
func (m *TradePlanManager) GetPlan(symbol string) *TradePlan {
	return m.GetPlanScoped("", symbol, "")
}

// GetPlanScoped 获取 trader/symbol/side 作用域计划，兼容旧 symbol key。
func (m *TradePlanManager) GetPlanScoped(traderID, symbol, side string) *TradePlan {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, ok := m.plans[key]; ok {
			planCopy := *plan
			if plan.ParsedInvalidationCondition != nil {
				condCopy := *plan.ParsedInvalidationCondition
				planCopy.ParsedInvalidationCondition = &condCopy
			}
			if plan.ExecutedTranches != nil {
				planCopy.ExecutedTranches = make(map[int]bool)
				for k, v := range plan.ExecutedTranches {
					planCopy.ExecutedTranches[k] = v
				}
			}
			return &planCopy
		}
	}
	return nil
}

// SetPlan 设置交易计划（续）
func (m *TradePlanManager) SetPlan(plan *TradePlan) {
	m.mu.Lock()

	if plan.ExecutedTranches == nil {
		plan.ExecutedTranches = make(map[int]bool)
	}

	if plan.OriginalTakeProfit == 0 {
		plan.OriginalTakeProfit = plan.TakeProfit
	}

	m.plans[makePlanKey(plan.TraderID, plan.Symbol, planSide(plan))] = plan
	m.mu.Unlock()

	invalidationDisplay := "无"
	if plan.InvalidationCondition != "" {
		invalidationDisplay = FormatInvalidationCondition(plan.InvalidationCondition)
	}

	log.Printf("📋 创建交易计划: %s %s @ %.4f, SL=%.4f, TP=%.4f, 最小持仓=%d分钟",
		plan.Symbol, plan.Direction, plan.EntryPrice, plan.StopLoss, plan.TakeProfit, plan.MinHoldMinutes)
	log.Printf("   └─ 失效条件: %s", invalidationDisplay)

	m.autoSaveIfEnabled()
}

// RemovePlan 移除交易计划
func (m *TradePlanManager) RemovePlan(symbol string) {
	m.RemovePlanScoped("", symbol, "")
}

// RemovePlanScoped 移除 trader/symbol/side 作用域计划，兼容旧 symbol key。
func (m *TradePlanManager) RemovePlanScoped(traderID, symbol, side string) {
	m.mu.Lock()
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, exists := m.plans[key]; exists {
			log.Printf("📋 移除交易计划: %s (状态: %s, 峰值盈利: %.2f%%)",
				symbol, plan.Status, plan.PeakPnLPercent)
			delete(m.plans, key)
			break
		}
	}
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// UpdatePlan 更新计划（线程安全）
func (m *TradePlanManager) UpdatePlan(symbol string, updateFn func(*TradePlan)) {
	m.UpdatePlanScoped("", symbol, "", updateFn)
}

// UpdatePlanScoped 更新 trader/symbol/side 作用域计划，兼容旧 symbol key。
func (m *TradePlanManager) UpdatePlanScoped(traderID, symbol, side string, updateFn func(*TradePlan)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, ok := m.plans[key]; ok {
			updateFn(plan)
			return
		}
	}
}

// UpdatePlanStopLoss 更新计划止损
func (m *TradePlanManager) UpdatePlanStopLoss(symbol string, newSL float64) {
	m.UpdatePlanStopLossScoped("", symbol, "", newSL)
}

func (m *TradePlanManager) UpdatePlanStopLossScoped(traderID, symbol, side string, newSL float64) {
	m.mu.Lock()
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, exists := m.plans[key]; exists {
			oldSL := plan.CurrentStopLoss
			if oldSL == 0 {
				oldSL = plan.StopLoss
			}
			plan.CurrentStopLoss = newSL
			plan.TrailingStopActive = true
			log.Printf("📋 更新 %s 止损: %.4f → %.4f", symbol, oldSL, newSL)
			break
		}
	}
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// UpdatePlanPeakData 更新峰值数据
func (m *TradePlanManager) UpdatePlanPeakData(symbol string, currentPrice float64, currentPnLPct float64) {
	m.UpdatePlanPeakDataScoped("", symbol, "", currentPrice, currentPnLPct)
}

func (m *TradePlanManager) UpdatePlanPeakDataScoped(traderID, symbol, side string, currentPrice float64, currentPnLPct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var plan *TradePlan
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if p, ok := m.plans[key]; ok {
			plan = p
			break
		}
	}
	if plan == nil {
		return
	}

	updated := false

	if currentPnLPct > plan.PeakPnLPercent {
		plan.PeakPnLPercent = currentPnLPct
		updated = true
	}

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

// UpdatePlanTakeProfit 更新动态止盈价格
func (m *TradePlanManager) UpdatePlanTakeProfit(symbol string, newTP float64) {
	m.UpdatePlanTakeProfitScoped("", symbol, "", newTP)
}

func (m *TradePlanManager) UpdatePlanTakeProfitScoped(traderID, symbol, side string, newTP float64) {
	m.mu.Lock()

	var plan *TradePlan
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if p, ok := m.plans[key]; ok {
			plan = p
			break
		}
	}
	if plan == nil {
		m.mu.Unlock()
		return
	}

	if plan.OriginalTakeProfit == 0 {
		plan.OriginalTakeProfit = plan.TakeProfit
	}

	oldTP := plan.TakeProfit
	plan.TakeProfit = newTP
	plan.LastTPAdjustTime = time.Now()

	log.Printf("📈 %s 动态止盈调整: %.4f → %.4f", symbol, oldTP, newTP)
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

func (m *TradePlanManager) MarkTakeProfitSyncedScoped(traderID, symbol, side string, syncedAt time.Time) {
	m.mu.Lock()

	var plan *TradePlan
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if p, ok := m.plans[key]; ok {
			plan = p
			break
		}
	}
	if plan == nil {
		m.mu.Unlock()
		return
	}

	if syncedAt.IsZero() {
		syncedAt = time.Now()
	}
	plan.LastTPSyncTime = syncedAt
	m.mu.Unlock()

	m.autoSaveIfEnabled()
}

// MarkTrancheExecuted 标记分批止盈档位已执行
func (m *TradePlanManager) MarkTrancheExecuted(symbol string, trancheIndex int, closePercent float64) {
	m.MarkTrancheExecutedScoped("", symbol, "", trancheIndex, closePercent)
}

func (m *TradePlanManager) MarkTrancheExecutedScoped(traderID, symbol, side string, trancheIndex int, closePercent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var plan *TradePlan
	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if p, ok := m.plans[key]; ok {
			plan = p
			break
		}
	}
	if plan == nil {
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

// UpdatePlanEntryATR 更新入场时ATR
func (m *TradePlanManager) UpdatePlanEntryATR(symbol string, atr float64) {
	m.UpdatePlanEntryATRScoped("", symbol, "", atr)
}

func (m *TradePlanManager) UpdatePlanEntryATRScoped(traderID, symbol, side string, atr float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, ok := m.plans[key]; ok {
			if plan.EntryATR == 0 {
				plan.EntryATR = atr
				log.Printf("📊 %s 记录入场ATR: %.4f", symbol, atr)
			}
			return
		}
	}
}

// IsTrancheExecuted 检查档位是否已执行
func (m *TradePlanManager) IsTrancheExecuted(symbol string, trancheIndex int) bool {
	return m.IsTrancheExecutedScoped("", symbol, "", trancheIndex)
}

func (m *TradePlanManager) IsTrancheExecutedScoped(traderID, symbol, side string, trancheIndex int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, key := range m.candidatePlanKeys(traderID, symbol, side) {
		if plan, ok := m.plans[key]; ok {
			if plan.ExecutedTranches == nil {
				return false
			}
			return plan.ExecutedTranches[trancheIndex]
		}
	}
	return false
}

// ForceSave 强制保存
func (m *TradePlanManager) ForceSave() error {
	return m.saveToFile()
}

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

func GetPositionStartTimeScoped(traderID, symbol, side string) int64 {
	if planManager == nil {
		return 0
	}
	return planManager.GetPositionStartTimeScoped(traderID, symbol, side)
}

func SetPositionStartTimeScoped(traderID, symbol, side string, startTime int64) {
	if planManager == nil {
		return
	}
	planManager.SetPositionStartTimeScoped(traderID, symbol, side, startTime)
}

func RemovePositionStartTimeScoped(traderID, symbol, side string) {
	if planManager == nil {
		return
	}
	planManager.RemovePositionStartTimeScoped(traderID, symbol, side)
}

// ============================================================================
// 夏普比率计算
// ============================================================================

// SetSharpeConfig 设置夏普比率配置
func SetSharpeConfig(config SharpeConfig) {
	sharpeConfig = config
}

// AddReturn 添加收益率记录
func AddReturn(returnPct float64) {
	returnsLock.Lock()
	returnsSeries = append(returnsSeries, returnPct)

	if len(returnsSeries) > 1000 {
		returnsSeries = returnsSeries[len(returnsSeries)-1000:]
	}
	returnsLock.Unlock()

	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// calculateSharpeRatioUnlocked 计算夏普比率（无锁版本）
func calculateSharpeRatioUnlocked() float64 {
	if len(returnsSeries) < sharpeConfig.MinTradesForCalc {
		return 0
	}

	sum := 0.0
	for _, r := range returnsSeries {
		sum += r
	}
	meanReturn := sum / float64(len(returnsSeries))

	sumSquaredDiff := 0.0
	for _, r := range returnsSeries {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSquaredDiff / float64(len(returnsSeries)))

	if stdDev == 0 {
		return 0
	}

	periodicRiskFree := sharpeConfig.RiskFreeRate / sharpeConfig.AnnualizeFactor
	sharpe := (meanReturn - periodicRiskFree) / stdDev * math.Sqrt(sharpeConfig.AnnualizeFactor)

	return sharpe
}

// calculateSortinoRatioUnlocked 计算索提诺比率（无锁版本）
func calculateSortinoRatioUnlocked() float64 {
	if len(returnsSeries) < sharpeConfig.MinTradesForCalc {
		return 0
	}

	sum := 0.0
	for _, r := range returnsSeries {
		sum += r
	}
	meanReturn := sum / float64(len(returnsSeries))

	sumSquaredNegative := 0.0
	negativeCount := 0
	for _, r := range returnsSeries {
		if r < 0 {
			sumSquaredNegative += r * r
			negativeCount++
		}
	}

	if negativeCount == 0 {
		return 10.0
	}

	downwardStdDev := math.Sqrt(sumSquaredNegative / float64(len(returnsSeries)))

	if downwardStdDev == 0 {
		return 0
	}

	periodicRiskFree := sharpeConfig.RiskFreeRate / sharpeConfig.AnnualizeFactor
	sortino := (meanReturn - periodicRiskFree) / downwardStdDev * math.Sqrt(sharpeConfig.AnnualizeFactor)

	return sortino
}

// CalculateSharpeRatio 计算夏普比率（带锁版本）
func CalculateSharpeRatio() float64 {
	returnsLock.RLock()
	defer returnsLock.RUnlock()
	return calculateSharpeRatioUnlocked()
}

// CalculateSortinoRatio 计算索提诺比率（带锁版本）
func CalculateSortinoRatio() float64 {
	returnsLock.RLock()
	defer returnsLock.RUnlock()
	return calculateSortinoRatioUnlocked()
}

// GetReturnsStats 获取收益率统计
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
		"sharpe_ratio":  calculateSharpeRatioUnlocked(),
		"sortino_ratio": calculateSortinoRatioUnlocked(),
	}
}

// ============================================================================
// 统计更新
// ============================================================================

// UpdateStatistics 更新统计
func UpdateStatistics(pnlPercent float64, holdTimeMinutes float64) {
	tradeStatsLock.Lock()

	tradeStats.TotalTrades++
	tradeStats.TotalPnL += pnlPercent

	if pnlPercent > 0 {
		tradeStats.WinningTrades++
		tradeStats.ConsecutiveWins++
		tradeStats.ConsecutiveLosses = 0
		tradeStats.AverageWin = (tradeStats.AverageWin*float64(tradeStats.WinningTrades-1) + pnlPercent) / float64(tradeStats.WinningTrades)
	} else {
		tradeStats.LosingTrades++
		tradeStats.ConsecutiveLosses++
		tradeStats.ConsecutiveWins = 0
		tradeStats.AverageLoss = (tradeStats.AverageLoss*float64(tradeStats.LosingTrades-1) + math.Abs(pnlPercent)) / float64(tradeStats.LosingTrades)

		if tradeStats.ConsecutiveLosses > tradeStats.MaxConsecLosses {
			tradeStats.MaxConsecLosses = tradeStats.ConsecutiveLosses
		}
	}

	if tradeStats.TotalTrades > 0 {
		tradeStats.WinRate = float64(tradeStats.WinningTrades) / float64(tradeStats.TotalTrades)
	}

	if tradeStats.AverageLoss > 0 && tradeStats.WinRate < 1 {
		tradeStats.ProfitFactor = (tradeStats.AverageWin * tradeStats.WinRate) / (tradeStats.AverageLoss * (1 - tradeStats.WinRate))
	}

	tradeStats.AverageHoldTime = (tradeStats.AverageHoldTime*float64(tradeStats.TotalTrades-1) + holdTimeMinutes) / float64(tradeStats.TotalTrades)
	tradeStats.LastUpdated = time.Now()

	// 计算夏普比率
	returnsLock.RLock()
	tradeStats.SharpeRatio = calculateSharpeRatioUnlocked()
	tradeStats.SortinoRatio = calculateSortinoRatioUnlocked()
	returnsLock.RUnlock()

	totalTrades := tradeStats.TotalTrades
	winRate := tradeStats.WinRate
	profitFactor := tradeStats.ProfitFactor
	sharpeRatio := tradeStats.SharpeRatio

	tradeStatsLock.Unlock()

	log.Printf("📊 统计更新: 总交易=%d, 胜率=%.1f%%, 盈亏因子=%.2f, 夏普=%.2f",
		totalTrades, winRate*100, profitFactor, sharpeRatio)

	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// GetStatistics 获取统计信息
func GetStatistics() *TradeStatistics {
	tradeStatsLock.RLock()
	defer tradeStatsLock.RUnlock()
	statsCopy := *tradeStats
	return &statsCopy
}

// ResetStatistics 重置统计
func ResetStatistics() {
	tradeStatsLock.Lock()
	tradeStats = &TradeStatistics{}
	tradeStatsLock.Unlock()

	returnsLock.Lock()
	returnsSeries = nil
	returnsLock.Unlock()

	log.Printf("📊 统计已重置")

	if planManager != nil {
		planManager.autoSaveIfEnabled()
	}
}

// ============================================================================
// 平仓回调
// ============================================================================

// OnPositionClosed 平仓成功后调用
func OnPositionClosed(symbol string, exitPrice float64, pnlPercent float64, pnlUSD float64, reason string) {
	OnPositionClosedScoped("", symbol, "", exitPrice, pnlPercent, pnlUSD, reason)
}

// OnPositionClosedScoped 平仓成功后调用，带 trader/side 作用域。
func OnPositionClosedScoped(traderID, symbol, side string, exitPrice float64, pnlPercent float64, pnlUSD float64, reason string) {
	plan := planManager.GetPlanScoped(traderID, symbol, side)

	now := time.Now()
	var record ClosedTradeRecord
	record.Symbol = symbol
	record.ExitPrice = exitPrice
	record.PnLPercent = pnlPercent
	record.PnLUSD = pnlUSD
	record.RealizedPnL = pnlUSD
	record.ExitReason = reason
	record.CloseReason = reason
	record.ClosedAt = now
	record.ExitTime = now

	if plan != nil {
		record.Direction = plan.Direction
		record.EntryPrice = plan.EntryPrice
		record.PeakPnLPercent = plan.PeakPnLPercent
		record.HoldingMinutes = int64(time.Since(plan.CreatedAt).Minutes())
		record.EntryTime = plan.CreatedAt
		record.Quantity = plan.ActualQuantity
		record.Leverage = plan.Leverage
		// 从 Direction 映射 Side 字段
		if plan.Direction == "long" {
			record.Side = "long"
		} else if plan.Direction == "short" {
			record.Side = "short"
		}
	}

	closedTradesLock.Lock()
	closedTrades = append(closedTrades, record)
	if len(closedTrades) > 100 {
		closedTrades = closedTrades[len(closedTrades)-100:]
	}
	closedTradesLock.Unlock()

	UpdateStatistics(pnlPercent, float64(record.HoldingMinutes))
	AddReturn(pnlPercent)

	planManager.RemovePlanScoped(traderID, symbol, side)
	RemovePositionStartTimeScoped(traderID, symbol, side)

	log.Printf("✅ 平仓成功: %s 盈亏%.2f%% (峰值%.2f%%), 原因: %s",
		symbol, pnlPercent, record.PeakPnLPercent, reason)
}

// OnPositionClosedSimple 简化版平仓回调
func OnPositionClosedSimple(symbol string, reason string) {
	OnPositionClosedSimpleScoped("", symbol, "", reason)
}

// OnPositionClosedSimpleScoped 简化版平仓回调，带 trader/side 作用域。
func OnPositionClosedSimpleScoped(traderID, symbol, side string, reason string) {
	plan := planManager.GetPlanScoped(traderID, symbol, side)
	peakPnL := 0.0
	if plan != nil {
		peakPnL = plan.PeakPnLPercent
	}

	planManager.RemovePlanScoped(traderID, symbol, side)
	RemovePositionStartTimeScoped(traderID, symbol, side)
	log.Printf("✅ 平仓成功，交易计划已移除: %s (峰值盈利: %.2f%%, 原因: %s)",
		symbol, peakPnL, reason)
}

// OnPartialClose 部分平仓成功后调用
func OnPartialClose(symbol string, trancheIndex int, percentage float64, newStopLoss float64) {
	OnPartialCloseScoped("", symbol, "", trancheIndex, percentage, newStopLoss)
}

// OnPartialCloseScoped 部分平仓成功后调用，带 trader/side 作用域。
func OnPartialCloseScoped(traderID, symbol, side string, trancheIndex int, percentage float64, newStopLoss float64) {
	if trancheIndex >= 0 {
		planManager.MarkTrancheExecutedScoped(traderID, symbol, side, trancheIndex, percentage)
	}

	if newStopLoss > 0 {
		planManager.UpdatePlanStopLossScoped(traderID, symbol, side, newStopLoss)
	}

	log.Printf("✅ %s 部分平仓%.0f%% (档位%d), 新止损: %.4f",
		symbol, percentage, trancheIndex+1, newStopLoss)
}

// OnStopLossUpdated 止损更新成功后调用
func OnStopLossUpdated(symbol string, newStopLoss float64) {
	OnStopLossUpdatedScoped("", symbol, "", newStopLoss)
}

// OnStopLossUpdatedScoped 止损更新成功后调用，带 trader/side 作用域。
func OnStopLossUpdatedScoped(traderID, symbol, side string, newStopLoss float64) {
	planManager.UpdatePlanStopLossScoped(traderID, symbol, side, newStopLoss)
	log.Printf("✅ %s 止损已更新至 %.4f", symbol, newStopLoss)
}

// OnTakeProfitUpdated 止盈更新成功后调用
func OnTakeProfitUpdated(symbol string, newTakeProfit float64) {
	OnTakeProfitUpdatedScoped("", symbol, "", newTakeProfit)
}

// OnTakeProfitUpdatedScoped 止盈更新成功后调用，带 trader/side 作用域。
func OnTakeProfitUpdatedScoped(traderID, symbol, side string, newTakeProfit float64) {
	planManager.UpdatePlanTakeProfitScoped(traderID, symbol, side, newTakeProfit)
	planManager.MarkTakeProfitSyncedScoped(traderID, symbol, side, time.Now())
	log.Printf("✅ %s 止盈已更新并同步至 %.4f", symbol, newTakeProfit)
}

// OnPositionOpened 开仓成功后调用
func OnPositionOpened(decision *Decision, actualEntryPrice float64, actualQuantity float64) error {
	return OnPositionOpenedScoped("", decision, actualEntryPrice, actualQuantity)
}

// OnPositionOpenedScoped 开仓成功后调用，带 trader 作用域。
func OnPositionOpenedScoped(traderID string, decision *Decision, actualEntryPrice float64, actualQuantity float64) error {
	if actualEntryPrice <= 0 {
		return fmt.Errorf("无效的入场价格: %.4f", actualEntryPrice)
	}

	plan := CreateTradePlanFromDecision(decision, actualEntryPrice)
	plan.TraderID = traderID
	planManager.SetPlan(plan)
	SetPositionStartTimeScoped(traderID, plan.Symbol, plan.Direction, plan.CreatedAt.UnixMilli())

	if actualQuantity > 0 {
		planManager.UpdatePlanScoped(traderID, decision.Symbol, plan.Direction, func(p *TradePlan) {
			p.ActualQuantity = actualQuantity
			p.PositionSizeUSD = actualQuantity * actualEntryPrice
		})
	}

	log.Printf("✅ 开仓成功，交易计划已创建: %s %s @ %.4f (数量: %.6f)",
		plan.Symbol, plan.Direction, actualEntryPrice, actualQuantity)

	return nil
}

// ============================================================================
// 导出导入
// ============================================================================

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

	if err := planManager.ForceSave(); err != nil {
		return fmt.Errorf("保存导入数据失败: %w", err)
	}

	log.Printf("📂 成功导入数据: %d个计划, %d笔交易记录",
		len(importData.Plans), len(importData.Returns))

	return nil
}
