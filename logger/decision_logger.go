package logger

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DecisionRecord 决策记录
type DecisionRecord struct {
	Timestamp        time.Time           `json:"timestamp"`       // 决策时间
	SourcePath       string              `json:"-"`               // 离线读取时的源文件路径
	CycleNumber      int                 `json:"cycle_number"`    // 周期编号
	InputPrompt      string              `json:"input_prompt"`    // 发送给AI的输入prompt
	CoTTrace         string              `json:"cot_trace"`       // AI思维链（输出）
	DecisionJSON     string              `json:"decision_json"`   // 决策JSON
	AccountState     AccountSnapshot     `json:"account_state"`   // 账户状态快照
	Positions        []PositionSnapshot  `json:"positions"`       // 持仓快照
	CandidateCoins   []string            `json:"candidate_coins"` // 候选币种列表
	CandidateDetails []CandidateSnapshot `json:"candidate_details,omitempty"`
	Decisions        []DecisionAction    `json:"decisions"`     // 执行的决策
	ExecutionLog     []string            `json:"execution_log"` // 执行日志
	Success          bool                `json:"success"`       // 是否成功
	ErrorMessage     string              `json:"error_message"` // 错误信息（如果有）
	RiskState        *RiskStateSnapshot  `json:"risk_state,omitempty"`
}

// RiskStateSnapshot 记录本周期可观测风险状态，保持旧日志兼容。
type RiskStateSnapshot struct {
	TraderID                 string           `json:"trader_id,omitempty"`
	Exchange                 string           `json:"exchange,omitempty"`
	MaxRiskPerTrade          float64          `json:"max_risk_per_trade,omitempty"`
	EffectiveMaxRiskPerTrade float64          `json:"effective_max_risk_per_trade,omitempty"`
	TotalRiskBudget          float64          `json:"total_risk_budget,omitempty"`
	RemainingRiskBudget      float64          `json:"remaining_risk_budget,omitempty"`
	MaxDailyLossPct          float64          `json:"max_daily_loss_pct,omitempty"`
	MaxAccountDrawdownPct    float64          `json:"max_account_drawdown_pct,omitempty"`
	AIBackoffUntil           string           `json:"ai_backoff_until,omitempty"`
	ConsecutiveAIFails       int              `json:"consecutive_ai_fails,omitempty"`
	OpenGateReasons          []string         `json:"open_gate_reasons,omitempty"`
	OpenGateDiagnostics      []map[string]any `json:"open_gate_diagnostics,omitempty"`
}

// AccountSnapshot 账户状态快照
type AccountSnapshot struct {
	TotalBalance          float64 `json:"total_balance"`
	AvailableBalance      float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount         int     `json:"position_count"`
	MarginUsedPct         float64 `json:"margin_used_pct"`
}

// PositionSnapshot 持仓快照
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

// CandidateSnapshot 记录候选币来源、数据质量和是否进入 prompt。
type CandidateSnapshot struct {
	Symbol           string   `json:"symbol"`
	Sources          []string `json:"sources,omitempty"`
	Score            float64  `json:"score,omitempty"`
	MarketState      string   `json:"market_state,omitempty"`
	StateConfidence  int      `json:"state_confidence,omitempty"`
	DataQuality      string   `json:"data_quality,omitempty"`
	FilterReason     string   `json:"filter_reason,omitempty"`
	IncludedInPrompt bool     `json:"included_in_prompt"`
	Warnings         []string `json:"warnings,omitempty"`
}

// DecisionAction 决策动作
type DecisionAction struct {
	Action    string    `json:"action"`    // open_long, open_short, close_long, close_short
	Symbol    string    `json:"symbol"`    // 币种
	Quantity  float64   `json:"quantity"`  // 数量
	Leverage  int       `json:"leverage"`  // 杠杆（开仓时）
	Price     float64   `json:"price"`     // 执行价格
	OrderID   int64     `json:"order_id"`  // 订单ID
	Timestamp time.Time `json:"timestamp"` // 执行时间
	Success   bool      `json:"success"`   // 是否成功
	Error     string    `json:"error"`     // 错误信息
	Reasoning string    `json:"reasoning,omitempty"`

	RiskUSD              float64        `json:"risk_usd,omitempty"`
	GateState            string         `json:"gate_state,omitempty"`
	GateReasons          []string       `json:"gate_reasons,omitempty"`
	GateDiagnostics      map[string]any `json:"gate_diagnostics,omitempty"`
	ExecutionRisk        string         `json:"execution_risk,omitempty"`
	StopLossSet          *bool          `json:"stop_loss_set,omitempty"`
	TakeProfitSet        *bool          `json:"take_profit_set,omitempty"`
	ProtectionError      string         `json:"protection_error,omitempty"`
	HighRisk             bool           `json:"high_risk,omitempty"`
	HighRiskReason       string         `json:"high_risk_reason,omitempty"`
	RemainingPositionUSD float64        `json:"remaining_position_usd,omitempty"`
}

// DecisionLogger 决策日志记录器
type DecisionLogger struct {
	logDir      string
	cycleNumber int
}

// NewDecisionLogger 创建决策日志记录器
func NewDecisionLogger(logDir string) *DecisionLogger {
	if logDir == "" {
		logDir = "decision_logs"
	}

	// 确保日志目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("⚠ 创建日志目录失败: %v\n", err)
	}

	return &DecisionLogger{
		logDir:      logDir,
		cycleNumber: 0,
	}
}

// LogDecision 记录决策
func (l *DecisionLogger) LogDecision(record *DecisionRecord) error {
	l.cycleNumber++
	record.CycleNumber = l.cycleNumber
	record.Timestamp = time.Now()

	// 生成文件名：decision_YYYYMMDD_HHMMSS_cycleN.json
	filename := fmt.Sprintf("decision_%s_cycle%d.json",
		record.Timestamp.Format("20060102_150405"),
		record.CycleNumber)

	filepath := filepath.Join(l.logDir, filename)

	// 序列化为JSON（带缩进，方便阅读）
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化决策记录失败: %w", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("写入决策记录失败: %w", err)
	}

	fmt.Printf("📝 决策记录已保存: %s\n", filename)
	return nil
}

// GetLatestRecords 获取最近N条记录（按时间正序：从旧到新）
func (l *DecisionLogger) GetLatestRecords(n int) ([]*DecisionRecord, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	// 读取所有记录
	var allRecords []*DecisionRecord
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fp := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(fp)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		allRecords = append(allRecords, &record)
	}

	// 按时间戳升序排序（确保正确的时间顺序，不依赖文件名排序）
	for i := 1; i < len(allRecords); i++ {
		for j := i; j > 0 && allRecords[j].Timestamp.Before(allRecords[j-1].Timestamp); j-- {
			allRecords[j], allRecords[j-1] = allRecords[j-1], allRecords[j]
		}
	}

	// 取最近 n 条（升序排列中的最后 n 条）
	if len(allRecords) <= n {
		return allRecords, nil
	}
	return allRecords[len(allRecords)-n:], nil
}

// GetRecordByDate 获取指定日期的所有记录
func (l *DecisionLogger) GetRecordByDate(date time.Time) ([]*DecisionRecord, error) {
	dateStr := date.Format("20060102")
	pattern := filepath.Join(l.logDir, fmt.Sprintf("decision_%s_*.json", dateStr))

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("查找日志文件失败: %w", err)
	}

	var records []*DecisionRecord
	for _, filepath := range files {
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
	}

	return records, nil
}

// CleanOldRecords 清理N天前的旧记录
func (l *DecisionLogger) CleanOldRecords(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return fmt.Errorf("读取日志目录失败: %w", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if file.ModTime().Before(cutoffTime) {
			filepath := filepath.Join(l.logDir, file.Name())
			if err := os.Remove(filepath); err != nil {
				fmt.Printf("⚠ 删除旧记录失败 %s: %v\n", file.Name(), err)
				continue
			}
			removedCount++
		}
	}

	if removedCount > 0 {
		fmt.Printf("🗑️ 已清理 %d 条旧记录（%d天前）\n", removedCount, days)
	}

	return nil
}

// GetStatistics 获取统计信息
func (l *DecisionLogger) GetStatistics() (*Statistics, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	stats := &Statistics{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		stats.TotalCycles++

		for _, action := range record.Decisions {
			if action.Success {
				switch action.Action {
				case "open_long", "open_short":
					stats.TotalOpenPositions++
				case "close_long", "close_short":
					stats.TotalClosePositions++
				}
			}
		}

		if record.Success {
			stats.SuccessfulCycles++
		} else {
			stats.FailedCycles++
		}
	}

	return stats, nil
}

// Statistics 统计信息
type Statistics struct {
	TotalCycles         int `json:"total_cycles"`
	SuccessfulCycles    int `json:"successful_cycles"`
	FailedCycles        int `json:"failed_cycles"`
	TotalOpenPositions  int `json:"total_open_positions"`
	TotalClosePositions int `json:"total_close_positions"`
}

// TradeOutcome 单笔交易结果
type TradeOutcome struct {
	Symbol        string    `json:"symbol"`         // 币种
	Side          string    `json:"side"`           // long/short
	Quantity      float64   `json:"quantity"`       // 仓位数量
	Leverage      int       `json:"leverage"`       // 杠杆倍数
	OpenPrice     float64   `json:"open_price"`     // 开仓价
	ClosePrice    float64   `json:"close_price"`    // 平仓价
	PositionValue float64   `json:"position_value"` // 仓位价值（quantity × openPrice）
	MarginUsed    float64   `json:"margin_used"`    // 保证金使用（positionValue / leverage）
	PnL           float64   `json:"pn_l"`           // 盈亏（USDT）
	PnLPct        float64   `json:"pn_l_pct"`       // 盈亏百分比（相对保证金）
	Duration      string    `json:"duration"`       // 持仓时长
	OpenTime      time.Time `json:"open_time"`      // 开仓时间
	CloseTime     time.Time `json:"close_time"`     // 平仓时间
	WasStopLoss   bool      `json:"was_stop_loss"`  // 是否止损
	OpenReason    string    `json:"open_reason,omitempty"`
	CloseReason   string    `json:"close_reason,omitempty"`
}

// UnmatchedAction 表示无法配对为完整交易的执行动作。
type UnmatchedAction struct {
	Timestamp time.Time `json:"timestamp"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
}

// PerformanceAnalysis 交易表现分析
type PerformanceAnalysis struct {
	TotalTrades   int                           `json:"total_trades"`   // 总交易数
	WinningTrades int                           `json:"winning_trades"` // 盈利交易数
	LosingTrades  int                           `json:"losing_trades"`  // 亏损交易数
	WinRate       float64                       `json:"win_rate"`       // 胜率
	AvgWin        float64                       `json:"avg_win"`        // 平均盈利
	AvgLoss       float64                       `json:"avg_loss"`       // 平均亏损
	ProfitFactor  float64                       `json:"profit_factor"`  // 盈亏比
	SharpeRatio   float64                       `json:"sharpe_ratio"`   // 夏普比率（风险调整后收益）
	RecentTrades  []TradeOutcome                `json:"recent_trades"`  // 最近N笔交易
	Unmatched     []UnmatchedAction             `json:"unmatched,omitempty"`
	Rolling       *RollingPerformanceSnapshot   `json:"rolling,omitempty"`
	Execution     ExecutionQualityStats         `json:"execution_quality"`
	SymbolStats   map[string]*SymbolPerformance `json:"symbol_stats"` // 各币种表现
	BestSymbol    string                        `json:"best_symbol"`  // 表现最好的币种
	WorstSymbol   string                        `json:"worst_symbol"` // 表现最差的币种
}

// SymbolPerformance 币种表现统计
type SymbolPerformance struct {
	Symbol        string  `json:"symbol"`         // 币种
	TotalTrades   int     `json:"total_trades"`   // 交易次数
	WinningTrades int     `json:"winning_trades"` // 盈利次数
	LosingTrades  int     `json:"losing_trades"`  // 亏损次数
	WinRate       float64 `json:"win_rate"`       // 胜率
	TotalPnL      float64 `json:"total_pn_l"`     // 总盈亏
	AvgPnL        float64 `json:"avg_pn_l"`       // 平均盈亏
}

// RollingPerformanceSnapshot 表示策略门控所需的滚动绩效视图。
type RollingPerformanceSnapshot struct {
	GlobalGate               PerformanceGate            `json:"global_gate,omitempty"`
	SymbolGates              map[string]PerformanceGate `json:"symbol_gates"`
	SideGates                map[string]PerformanceGate `json:"side_gates"`
	Recent3                  RollingStats               `json:"recent_3"`
	Recent10                 RollingStats               `json:"recent_10"`
	Recent20                 RollingStats               `json:"recent_20"`
	RecentLossStreak         int                        `json:"recent_loss_streak,omitempty"`
	Recent3Losses            int                        `json:"recent_3_losses,omitempty"`
	EffectiveMaxRiskPerTrade float64                    `json:"effective_max_risk_per_trade"`
	Reasons                  []string                   `json:"reasons,omitempty"`
}

// PerformanceGate 是开仓验证层消费的 symbol/side 门控结果。
type PerformanceGate struct {
	Key            string    `json:"key"`
	Scope          string    `json:"scope"`
	State          string    `json:"state"` // allow, penalize, block
	TradeCount     int       `json:"trade_count"`
	TotalPnL       float64   `json:"total_pn_l"`
	WinRate        float64   `json:"win_rate"`
	ProfitFactor   float64   `json:"profit_factor"`
	MinConfidence  int       `json:"min_confidence"`
	RiskMultiplier float64   `json:"risk_multiplier"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// RollingStats 是一组交易窗口的基础统计。
type RollingStats struct {
	TradeCount   int     `json:"trade_count"`
	TotalPnL     float64 `json:"total_pn_l"`
	WinRate      float64 `json:"win_rate"`
	ProfitFactor float64 `json:"profit_factor"`
}

// ExecutionQualityStats 汇总执行失败质量指标。
type ExecutionQualityStats struct {
	TotalActions                 int                  `json:"total_actions"`
	OpenAttempts                 int                  `json:"open_attempts"`
	OpenFailures                 int                  `json:"open_failures"`
	OpenRejectedCount            int                  `json:"open_rejected_count"`
	PartialCloseAttempts         int                  `json:"partial_close_attempts"`
	PartialCloseFailures         int                  `json:"partial_close_failures"`
	PartialCloseFailureRate      float64              `json:"partial_close_failure_rate"`
	ProtectionOrderFailures      int                  `json:"protection_order_failures"`
	HighRiskExecutionFailures    int                  `json:"high_risk_execution_failures"`
	AIFailureCount               int                  `json:"ai_failure_count"`
	UnmatchedActionCount         int                  `json:"unmatched_action_count"`
	RecentHighRiskErrors         []ExecutionRiskEvent `json:"recent_high_risk_errors,omitempty"`
	RecentOpenRejectionReasons   []string             `json:"recent_open_rejection_reasons,omitempty"`
	ProtectionOrderFailureRate   float64              `json:"protection_order_failure_rate"`
	HighRiskExecutionFailureRate float64              `json:"high_risk_execution_failure_rate"`
}

// ExecutionRiskEvent 记录最近高危执行失败，供 API/前端快速巡检。
type ExecutionRiskEvent struct {
	Timestamp string `json:"timestamp"`
	Symbol    string `json:"symbol,omitempty"`
	Action    string `json:"action,omitempty"`
	RiskType  string `json:"risk_type"`
	Reason    string `json:"reason"`
}

type openPositionTrace struct {
	symbol    string
	side      string
	price     float64
	time      time.Time
	quantity  float64
	leverage  int
	reasoning string
}

// BuildTradeOutcomes 将决策日志中的开平仓动作配对为可复盘的闭合交易。
func BuildTradeOutcomes(records []*DecisionRecord) ([]TradeOutcome, []UnmatchedAction) {
	if len(records) == 0 {
		return []TradeOutcome{}, []UnmatchedAction{}
	}

	sortedRecords := append([]*DecisionRecord(nil), records...)
	sort.SliceStable(sortedRecords, func(i, j int) bool {
		return sortedRecords[i].Timestamp.Before(sortedRecords[j].Timestamp)
	})

	openPositions := make(map[string][]openPositionTrace)
	var outcomes []TradeOutcome
	var unmatched []UnmatchedAction

	for _, record := range sortedRecords {
		reasoningByAction := extractDecisionReasoning(record.DecisionJSON)
		for _, action := range record.Decisions {
			if !action.Success {
				continue
			}

			side, ok := actionSide(action.Action)
			if !ok || action.Symbol == "" {
				continue
			}

			actionTime := action.Timestamp
			if actionTime.IsZero() {
				actionTime = record.Timestamp
			}
			reasoning := action.Reasoning
			if reasoning == "" {
				reasoning = reasoningByAction[actionReasonKey(action.Symbol, action.Action)]
			}

			key := action.Symbol + "_" + side
			switch action.Action {
			case "open_long", "open_short":
				openPositions[key] = append(openPositions[key], openPositionTrace{
					symbol:    action.Symbol,
					side:      side,
					price:     action.Price,
					time:      actionTime,
					quantity:  action.Quantity,
					leverage:  action.Leverage,
					reasoning: reasoning,
				})
			case "close_long", "close_short", "auto_close_long", "auto_close_short":
				opens := openPositions[key]
				if len(opens) == 0 {
					unmatched = append(unmatched, UnmatchedAction{
						Timestamp: actionTime,
						Symbol:    action.Symbol,
						Side:      side,
						Action:    action.Action,
						Reason:    "missing_open",
					})
					continue
				}

				for _, open := range opens {
					outcomes = append(outcomes, buildTradeOutcome(open, action, actionTime, reasoning))
				}
				delete(openPositions, key)
			}
		}
	}

	for _, opens := range openPositions {
		for _, open := range opens {
			unmatched = append(unmatched, UnmatchedAction{
				Timestamp: open.time,
				Symbol:    open.symbol,
				Side:      open.side,
				Action:    "open_" + open.side,
				Reason:    "missing_close",
			})
		}
	}

	return outcomes, unmatched
}

var historicallyWeakSymbols = map[string]string{
	"BCHUSDT":   "历史滚动表现偏弱，开仓需降权",
	"ASTERUSDT": "历史滚动表现偏弱，开仓需降权",
	"LTCUSDT":   "历史滚动表现偏弱，开仓需降权",
	"XRPUSDT":   "历史滚动表现偏弱，开仓需降权",
}

// BuildRollingPerformance 基于闭合交易生成 symbol/side 门控和动态风险建议。
func BuildRollingPerformance(outcomes []TradeOutcome, now time.Time) *RollingPerformanceSnapshot {
	snapshot := &RollingPerformanceSnapshot{
		SymbolGates:              make(map[string]PerformanceGate),
		SideGates:                make(map[string]PerformanceGate),
		Recent3:                  rollingStats(lastTrades(outcomes, 3)),
		Recent10:                 rollingStats(lastTrades(outcomes, 10)),
		Recent20:                 rollingStats(lastTrades(outcomes, 20)),
		RecentLossStreak:         recentLossStreak(outcomes),
		Recent3Losses:            countLosses(lastTrades(outcomes, 3)),
		EffectiveMaxRiskPerTrade: 0.02,
	}
	if now.IsZero() {
		now = time.Now()
	}

	snapshot.GlobalGate = buildGlobalGate(outcomes, snapshot, now)
	if snapshot.GlobalGate.Reason != "" && snapshot.GlobalGate.State != "" && snapshot.GlobalGate.State != "allow" {
		snapshot.Reasons = append(snapshot.Reasons, snapshot.GlobalGate.Reason)
	}

	if snapshot.Recent20.TradeCount >= 20 && snapshot.Recent20.ProfitFactor < 0.8 {
		snapshot.EffectiveMaxRiskPerTrade = 0.005
		snapshot.Reasons = append(snapshot.Reasons, "最近20笔PF低于0.8，单笔风险降至0.5%")
	} else if snapshot.Recent10.TradeCount >= 10 && snapshot.Recent10.ProfitFactor < 1.0 {
		snapshot.EffectiveMaxRiskPerTrade = 0.01
		snapshot.Reasons = append(snapshot.Reasons, "最近10笔PF低于1.0，单笔风险降至1%")
	}
	if snapshot.RecentLossStreak >= 2 && snapshot.EffectiveMaxRiskPerTrade > 0.01 {
		snapshot.EffectiveMaxRiskPerTrade = 0.01
		snapshot.Reasons = append(snapshot.Reasons, "最近2笔连续亏损，下一笔单笔风险降至1%")
	}

	symbols := make(map[string]struct{})
	for _, outcome := range outcomes {
		if outcome.Symbol != "" {
			symbols[outcome.Symbol] = struct{}{}
		}
	}
	for symbol := range historicallyWeakSymbols {
		symbols[symbol] = struct{}{}
	}
	for symbol := range symbols {
		trades := filterTrades(outcomes, func(outcome TradeOutcome) bool {
			return outcome.Symbol == symbol
		})
		snapshot.SymbolGates[symbol] = buildSymbolGate(symbol, trades, now)
	}

	for _, side := range []string{"long", "short"} {
		trades := filterTrades(outcomes, func(outcome TradeOutcome) bool {
			return outcome.Side == side
		})
		snapshot.SideGates[side] = buildSideGate(side, trades)
	}
	applyGlobalRecentLossGate(snapshot)

	return snapshot
}

func buildGlobalGate(outcomes []TradeOutcome, snapshot *RollingPerformanceSnapshot, now time.Time) PerformanceGate {
	gate := PerformanceGate{
		Key:            "ALL",
		Scope:          "global",
		State:          "allow",
		RiskMultiplier: 1,
	}
	if snapshot == nil {
		return gate
	}

	latestClose := latestTradeCloseTime(outcomes)
	if latestClose.IsZero() {
		latestClose = now
	}

	if snapshot.Recent20.TradeCount >= 20 &&
		snapshot.Recent20.ProfitFactor < 0.8 &&
		snapshot.Recent20.WinRate < 35 {
		gate.TradeCount = snapshot.Recent20.TradeCount
		gate.TotalPnL = snapshot.Recent20.TotalPnL
		gate.WinRate = snapshot.Recent20.WinRate
		gate.ProfitFactor = snapshot.Recent20.ProfitFactor
		gate.MinConfidence = 95
		gate.CooldownUntil = latestClose.Add(24 * time.Hour)
		gate.Reason = "最近20笔PF低于0.8且胜率低于35%，全局暂停新开仓24小时"
		if now.Before(gate.CooldownUntil) {
			gate.State = "block"
			gate.RiskMultiplier = 0
			return gate
		}
		gate.State = "penalize"
		gate.RiskMultiplier = 0.5
		return gate
	}

	if snapshot.Recent3.TradeCount >= 3 &&
		snapshot.Recent3Losses >= 3 &&
		snapshot.Recent3.TotalPnL < 0 {
		gate.TradeCount = snapshot.Recent3.TradeCount
		gate.TotalPnL = snapshot.Recent3.TotalPnL
		gate.WinRate = snapshot.Recent3.WinRate
		gate.ProfitFactor = snapshot.Recent3.ProfitFactor
		gate.MinConfidence = 90
		gate.CooldownUntil = latestClose.Add(12 * time.Hour)
		gate.Reason = "最近3笔连续亏损且总PnL为负，全局暂停新开仓12小时"
		if now.Before(gate.CooldownUntil) {
			gate.State = "block"
			gate.RiskMultiplier = 0
			return gate
		}
		gate.State = "penalize"
		gate.RiskMultiplier = 0.5
	}

	return gate
}

func buildSymbolGate(symbol string, trades []TradeOutcome, now time.Time) PerformanceGate {
	gate := PerformanceGate{
		Key:            symbol,
		Scope:          "symbol",
		State:          "allow",
		RiskMultiplier: 1,
	}

	recent8 := rollingStats(lastTrades(trades, 8))
	recent5 := rollingStats(lastTrades(trades, 5))
	stats := recent8
	if stats.TradeCount == 0 {
		stats = recent5
	}
	gate.TradeCount = stats.TradeCount
	gate.TotalPnL = stats.TotalPnL
	gate.WinRate = stats.WinRate
	gate.ProfitFactor = stats.ProfitFactor

	if recent8.TradeCount >= 8 && recent8.ProfitFactor < 0.5 {
		cooldownUntil := latestTradeCloseTime(trades)
		if cooldownUntil.IsZero() {
			cooldownUntil = now
		}
		cooldownUntil = cooldownUntil.Add(24 * time.Hour)
		if now.Before(cooldownUntil) {
			gate.State = "block"
			gate.MinConfidence = 95
			gate.RiskMultiplier = 0
			gate.CooldownUntil = cooldownUntil
			gate.Reason = "最近8笔PF低于0.5，进入24小时禁交易冷却"
			return gate
		}
		gate.State = "penalize"
		gate.MinConfidence = 90
		gate.RiskMultiplier = 0.5
		gate.Reason = "最近8笔PF低于0.5，禁交易冷却已过但仍需降权"
		return gate
	}
	if recent5.TradeCount >= 5 && recent5.ProfitFactor < 0.8 && recent5.TotalPnL < 0 {
		gate.State = "penalize"
		gate.MinConfidence = 85
		gate.RiskMultiplier = 0.5
		gate.Reason = "最近5笔PF低于0.8且总PnL为负"
		return gate
	}
	if reason, ok := historicallyWeakSymbols[symbol]; ok {
		gate.State = "penalize"
		gate.MinConfidence = 85
		gate.RiskMultiplier = 0.5
		gate.Reason = reason
	}
	return gate
}

func buildSideGate(side string, trades []TradeOutcome) PerformanceGate {
	stats := rollingStats(lastTrades(trades, 20))
	gate := PerformanceGate{
		Key:            side,
		Scope:          "side",
		State:          "allow",
		TradeCount:     stats.TradeCount,
		TotalPnL:       stats.TotalPnL,
		WinRate:        stats.WinRate,
		ProfitFactor:   stats.ProfitFactor,
		RiskMultiplier: 1,
	}

	if stats.TradeCount >= 20 && stats.ProfitFactor < 0.8 {
		gate.State = "penalize"
		gate.MinConfidence = 90
		gate.RiskMultiplier = 0.5
		gate.Reason = "最近20笔同方向PF低于0.8"
		return gate
	}
	if side == "short" {
		gate.State = "penalize"
		gate.MinConfidence = 90
		gate.RiskMultiplier = 0.5
		gate.Reason = "历史空单侧亏损贡献较大，默认降权"
	}
	return gate
}

func rollingStats(trades []TradeOutcome) RollingStats {
	stats := RollingStats{TradeCount: len(trades)}
	var wins, losses int
	var grossWin, grossLoss float64
	for _, trade := range trades {
		stats.TotalPnL += trade.PnL
		if trade.PnL > 0 {
			wins++
			grossWin += trade.PnL
		} else if trade.PnL < 0 {
			losses++
			grossLoss += -trade.PnL
		}
	}
	if stats.TradeCount > 0 {
		stats.WinRate = float64(wins) / float64(stats.TradeCount) * 100
	}
	if grossLoss > 0 {
		stats.ProfitFactor = grossWin / grossLoss
	} else if grossWin > 0 {
		stats.ProfitFactor = 999
	}
	_ = losses
	return stats
}

func recentLossStreak(trades []TradeOutcome) int {
	streak := 0
	for i := len(trades) - 1; i >= 0; i-- {
		if trades[i].PnL < 0 {
			streak++
			continue
		}
		break
	}
	return streak
}

func countLosses(trades []TradeOutcome) int {
	losses := 0
	for _, trade := range trades {
		if trade.PnL < 0 {
			losses++
		}
	}
	return losses
}

func applyGlobalRecentLossGate(snapshot *RollingPerformanceSnapshot) {
	if snapshot == nil || snapshot.Recent3.TradeCount < 3 ||
		snapshot.Recent3Losses < 2 || snapshot.Recent3.TotalPnL >= 0 {
		return
	}
	snapshot.Reasons = append(snapshot.Reasons, "最近3笔中至少2笔亏损且总PnL为负，提高下一笔开仓门槛")
	for _, side := range []string{"long", "short"} {
		gate := snapshot.SideGates[side]
		if gate.Key == "" {
			gate = PerformanceGate{Key: side, Scope: "side", State: "allow", RiskMultiplier: 1}
		}
		if gate.State == "" || gate.State == "allow" {
			gate.State = "penalize"
		}
		if gate.MinConfidence < 85 {
			gate.MinConfidence = 85
		}
		if gate.RiskMultiplier <= 0 || gate.RiskMultiplier > 0.75 {
			gate.RiskMultiplier = 0.75
		}
		if gate.Reason == "" {
			gate.Reason = "最近3笔中至少2笔亏损且总PnL为负"
		}
		snapshot.SideGates[side] = gate
	}
}

func lastTrades(trades []TradeOutcome, n int) []TradeOutcome {
	if n <= 0 || len(trades) <= n {
		return append([]TradeOutcome(nil), trades...)
	}
	return append([]TradeOutcome(nil), trades[len(trades)-n:]...)
}

func filterTrades(trades []TradeOutcome, keep func(TradeOutcome) bool) []TradeOutcome {
	var filtered []TradeOutcome
	for _, trade := range trades {
		if keep(trade) {
			filtered = append(filtered, trade)
		}
	}
	return filtered
}

func latestTradeCloseTime(trades []TradeOutcome) time.Time {
	var latest time.Time
	for _, trade := range trades {
		if latest.IsZero() || trade.CloseTime.After(latest) {
			latest = trade.CloseTime
		}
	}
	return latest
}

func BuildExecutionQuality(records []*DecisionRecord, unmatchedCount int) ExecutionQualityStats {
	var stats ExecutionQualityStats
	for _, record := range records {
		if record == nil {
			continue
		}
		if !record.Success && isAIFailureText(record.ErrorMessage) {
			stats.AIFailureCount++
		}
		for _, action := range record.Decisions {
			stats.TotalActions++

			if isOpenAction(action.Action) {
				stats.OpenAttempts++
				if !action.Success {
					stats.OpenFailures++
				}
			}
			if action.Action == "open_rejected" || (isOpenAction(action.Action) && isOpenRejection(action)) {
				stats.OpenRejectedCount++
				stats.RecentOpenRejectionReasons = appendLimitedString(stats.RecentOpenRejectionReasons, actionFailureReason(action), 5)
			}

			if action.Action == "partial_close" {
				stats.PartialCloseAttempts++
				if !action.Success {
					stats.PartialCloseFailures++
				}
			}

			if isProtectionFailure(action) {
				stats.ProtectionOrderFailures++
				stats.RecentHighRiskErrors = appendLimitedRiskEvent(stats.RecentHighRiskErrors, buildRiskEvent(record, action, "protection_order_failure"), 5)
			}

			if isHighRiskExecutionFailure(action) {
				stats.HighRiskExecutionFailures++
				stats.RecentHighRiskErrors = appendLimitedRiskEvent(stats.RecentHighRiskErrors, buildRiskEvent(record, action, "high_risk_execution_failure"), 5)
			}
		}
	}
	if stats.PartialCloseAttempts > 0 {
		stats.PartialCloseFailureRate = float64(stats.PartialCloseFailures) / float64(stats.PartialCloseAttempts) * 100
	}
	if stats.OpenAttempts > 0 {
		stats.ProtectionOrderFailureRate = float64(stats.ProtectionOrderFailures) / float64(stats.OpenAttempts) * 100
	}
	if stats.TotalActions > 0 {
		stats.HighRiskExecutionFailureRate = float64(stats.HighRiskExecutionFailures) / float64(stats.TotalActions) * 100
	}
	stats.UnmatchedActionCount = unmatchedCount
	return stats
}

func isOpenAction(action string) bool {
	return action == "open_long" || action == "open_short"
}

func isAIFailureText(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "ai") ||
		strings.Contains(value, "获取ai决策") ||
		strings.Contains(value, "api调用失败") ||
		strings.Contains(value, "响应解析失败")
}

func isOpenRejection(action DecisionAction) bool {
	if action.GateState == "block" || action.GateState == "reject" {
		return true
	}
	text := strings.ToLower(actionFailureReason(action))
	keywords := []string{"拒绝", "阻止", "gate", "验证失败", "风险预算", "rr", "置信度", "开仓前"}
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func isProtectionFailure(action DecisionAction) bool {
	if action.ProtectionError != "" {
		return true
	}
	if action.StopLossSet != nil && !*action.StopLossSet {
		return true
	}
	if action.TakeProfitSet != nil && !*action.TakeProfitSet {
		return true
	}
	text := strings.ToLower(action.Error + " " + action.HighRiskReason)
	return strings.Contains(text, "止损") && strings.Contains(text, "失败") ||
		strings.Contains(text, "止盈") && strings.Contains(text, "失败") ||
		strings.Contains(text, "protect") ||
		strings.Contains(text, "unprotected")
}

func isHighRiskExecutionFailure(action DecisionAction) bool {
	if action.HighRisk {
		return true
	}
	if action.ExecutionRisk == "high" || action.ExecutionRisk == "critical" {
		return true
	}
	text := strings.ToLower(action.Error + " " + action.ProtectionError + " " + action.HighRiskReason)
	return strings.Contains(text, "高危") ||
		strings.Contains(text, "裸仓") ||
		strings.Contains(text, "unprotected") ||
		strings.Contains(text, "emergency")
}

func actionFailureReason(action DecisionAction) string {
	if action.HighRiskReason != "" {
		return action.HighRiskReason
	}
	if action.ProtectionError != "" {
		return action.ProtectionError
	}
	if action.Error != "" {
		return action.Error
	}
	if len(action.GateReasons) > 0 {
		return strings.Join(action.GateReasons, "; ")
	}
	return action.Reasoning
}

func buildRiskEvent(record *DecisionRecord, action DecisionAction, riskType string) ExecutionRiskEvent {
	eventTime := action.Timestamp
	if eventTime.IsZero() {
		eventTime = record.Timestamp
	}
	return ExecutionRiskEvent{
		Timestamp: eventTime.Format(time.RFC3339),
		Symbol:    action.Symbol,
		Action:    action.Action,
		RiskType:  riskType,
		Reason:    actionFailureReason(action),
	}
}

func appendLimitedRiskEvent(items []ExecutionRiskEvent, item ExecutionRiskEvent, limit int) []ExecutionRiskEvent {
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func appendLimitedString(items []string, item string, limit int) []string {
	if item == "" {
		return items
	}
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func buildTradeOutcome(open openPositionTrace, closeAction DecisionAction, closeTime time.Time, closeReason string) TradeOutcome {
	leverage := open.leverage
	if leverage <= 0 {
		leverage = 1
	}

	var pnl float64
	if open.side == "long" {
		pnl = open.quantity * (closeAction.Price - open.price)
	} else {
		pnl = open.quantity * (open.price - closeAction.Price)
	}

	positionValue := open.quantity * open.price
	marginUsed := positionValue / float64(leverage)
	pnlPct := 0.0
	if marginUsed > 0 {
		pnlPct = (pnl / marginUsed) * 100
	}

	return TradeOutcome{
		Symbol:        open.symbol,
		Side:          open.side,
		Quantity:      open.quantity,
		Leverage:      leverage,
		OpenPrice:     open.price,
		ClosePrice:    closeAction.Price,
		PositionValue: positionValue,
		MarginUsed:    marginUsed,
		PnL:           pnl,
		PnLPct:        pnlPct,
		Duration:      closeTime.Sub(open.time).String(),
		OpenTime:      open.time,
		CloseTime:     closeTime,
		WasStopLoss:   pnl < 0,
		OpenReason:    open.reasoning,
		CloseReason:   closeReason,
	}
}

func actionSide(action string) (string, bool) {
	switch action {
	case "open_long", "close_long", "auto_close_long":
		return "long", true
	case "open_short", "close_short", "auto_close_short":
		return "short", true
	default:
		return "", false
	}
}

func extractDecisionReasoning(decisionJSON string) map[string]string {
	result := make(map[string]string)
	if decisionJSON == "" {
		return result
	}

	var decisions []struct {
		Symbol    string `json:"symbol"`
		Action    string `json:"action"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(decisionJSON), &decisions); err != nil {
		return result
	}

	for _, d := range decisions {
		if d.Symbol == "" || d.Action == "" || d.Reasoning == "" {
			continue
		}
		result[actionReasonKey(d.Symbol, d.Action)] = d.Reasoning
	}
	return result
}

func actionReasonKey(symbol, action string) string {
	return symbol + "\x00" + action
}

func earliestRecordEventTime(records []*DecisionRecord) time.Time {
	var earliest time.Time
	for _, record := range records {
		if record == nil {
			continue
		}
		if earliest.IsZero() || record.Timestamp.Before(earliest) {
			earliest = record.Timestamp
		}
		for _, action := range record.Decisions {
			actionTime := action.Timestamp
			if actionTime.IsZero() {
				actionTime = record.Timestamp
			}
			if !actionTime.IsZero() && (earliest.IsZero() || actionTime.Before(earliest)) {
				earliest = actionTime
			}
		}
	}
	return earliest
}

// AnalyzePerformance 分析最近N个周期的交易表现
func (l *DecisionLogger) AnalyzePerformance(lookbackCycles int) (*PerformanceAnalysis, error) {
	records, err := l.GetLatestRecords(lookbackCycles)
	if err != nil {
		return nil, fmt.Errorf("读取历史记录失败: %w", err)
	}

	if len(records) == 0 {
		return &PerformanceAnalysis{
			RecentTrades: []TradeOutcome{},
			SymbolStats:  make(map[string]*SymbolPerformance),
		}, nil
	}

	analysis := &PerformanceAnalysis{
		RecentTrades: []TradeOutcome{},
		SymbolStats:  make(map[string]*SymbolPerformance),
	}

	// 为了避免开仓记录在窗口外导致匹配失败，需要先从所有历史记录中找出未平仓的持仓
	// 获取更多历史记录来构建完整的持仓状态（使用更大的窗口）
	allRecords, err := l.GetLatestRecords(lookbackCycles * 3) // 扩大3倍窗口
	if err != nil || len(allRecords) == 0 {
		allRecords = records
	}

	outcomes, unmatched := BuildTradeOutcomes(allRecords)
	analysis.Rolling = BuildRollingPerformance(outcomes, time.Now())
	analysis.Execution = BuildExecutionQuality(allRecords, len(unmatched))
	windowStart := earliestRecordEventTime(records)
	for _, outcome := range outcomes {
		if !windowStart.IsZero() && outcome.CloseTime.Before(windowStart) {
			continue
		}
		analysis.RecentTrades = append(analysis.RecentTrades, outcome)
		analysis.TotalTrades++

		// 分类交易：盈利、亏损、持平（避免将pnl=0算入亏损）
		if outcome.PnL > 0 {
			analysis.WinningTrades++
			analysis.AvgWin += outcome.PnL
		} else if outcome.PnL < 0 {
			analysis.LosingTrades++
			analysis.AvgLoss += outcome.PnL
		}

		// 更新币种统计
		if _, exists := analysis.SymbolStats[outcome.Symbol]; !exists {
			analysis.SymbolStats[outcome.Symbol] = &SymbolPerformance{
				Symbol: outcome.Symbol,
			}
		}
		stats := analysis.SymbolStats[outcome.Symbol]
		stats.TotalTrades++
		stats.TotalPnL += outcome.PnL
		if outcome.PnL > 0 {
			stats.WinningTrades++
		} else if outcome.PnL < 0 {
			stats.LosingTrades++
		}
	}
	for _, item := range unmatched {
		if windowStart.IsZero() || !item.Timestamp.Before(windowStart) {
			analysis.Unmatched = append(analysis.Unmatched, item)
		}
	}

	// 计算统计指标
	if analysis.TotalTrades > 0 {
		analysis.WinRate = (float64(analysis.WinningTrades) / float64(analysis.TotalTrades)) * 100

		// 计算总盈利和总亏损
		totalWinAmount := analysis.AvgWin   // 当前是累加的总和
		totalLossAmount := analysis.AvgLoss // 当前是累加的总和（负数）

		if analysis.WinningTrades > 0 {
			analysis.AvgWin /= float64(analysis.WinningTrades)
		}
		if analysis.LosingTrades > 0 {
			analysis.AvgLoss /= float64(analysis.LosingTrades)
		}

		// Profit Factor = 总盈利 / 总亏损（绝对值）
		// 注意：totalLossAmount 是负数，所以取负号得到绝对值
		if totalLossAmount != 0 {
			analysis.ProfitFactor = totalWinAmount / (-totalLossAmount)
		} else if totalWinAmount > 0 {
			// 只有盈利没有亏损的情况，设置为一个很大的值表示完美策略
			analysis.ProfitFactor = 999.0
		}
	}

	// 计算各币种胜率和平均盈亏
	bestPnL := -999999.0
	worstPnL := 999999.0
	for symbol, stats := range analysis.SymbolStats {
		if stats.TotalTrades > 0 {
			stats.WinRate = (float64(stats.WinningTrades) / float64(stats.TotalTrades)) * 100
			stats.AvgPnL = stats.TotalPnL / float64(stats.TotalTrades)

			if stats.TotalPnL > bestPnL {
				bestPnL = stats.TotalPnL
				analysis.BestSymbol = symbol
			}
			if stats.TotalPnL < worstPnL {
				worstPnL = stats.TotalPnL
				analysis.WorstSymbol = symbol
			}
		}
	}

	// 只保留最近的交易（倒序：最新的在前）
	if len(analysis.RecentTrades) > 10 {
		// 反转数组，让最新的在前
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
		analysis.RecentTrades = analysis.RecentTrades[:10]
	} else if len(analysis.RecentTrades) > 0 {
		// 反转数组
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
	}

	// 计算夏普比率（需要至少2个数据点）
	analysis.SharpeRatio = l.calculateSharpeRatio(records)

	return analysis, nil
}

// calculateSharpeRatio 计算夏普比率
// 基于账户净值的变化计算风险调整后收益
func (l *DecisionLogger) calculateSharpeRatio(records []*DecisionRecord) float64 {
	if len(records) < 2 {
		return 0.0
	}

	// 提取每个周期的账户净值
	// 注意：TotalBalance字段实际存储的是TotalEquity（账户总净值）
	// TotalUnrealizedProfit字段实际存储的是TotalPnL（相对初始余额的盈亏）
	var equities []float64
	for _, record := range records {
		// 直接使用TotalBalance，因为它已经是完整的账户净值
		equity := record.AccountState.TotalBalance
		if equity > 0 {
			equities = append(equities, equity)
		}
	}

	if len(equities) < 2 {
		return 0.0
	}

	// 计算周期收益率（period returns）
	var returns []float64
	for i := 1; i < len(equities); i++ {
		if equities[i-1] > 0 {
			periodReturn := (equities[i] - equities[i-1]) / equities[i-1]
			returns = append(returns, periodReturn)
		}
	}

	if len(returns) == 0 {
		return 0.0
	}

	// 计算平均收益率
	sumReturns := 0.0
	for _, r := range returns {
		sumReturns += r
	}
	meanReturn := sumReturns / float64(len(returns))

	// 计算收益率标准差
	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	variance := sumSquaredDiff / float64(len(returns))
	stdDev := math.Sqrt(variance)

	// 避免除以零
	if stdDev == 0 {
		if meanReturn > 0 {
			return 999.0 // 无波动的正收益
		} else if meanReturn < 0 {
			return -999.0 // 无波动的负收益
		}
		return 0.0
	}

	// 计算夏普比率（假设无风险利率为0）
	// 注：直接返回周期级别的夏普比率（非年化），正常范围 -2 到 +2
	sharpeRatio := meanReturn / stdDev
	return sharpeRatio
}
