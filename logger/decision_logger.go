package logger

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DecisionRecord 决策记录
type DecisionRecord struct {
	Timestamp           time.Time           `json:"timestamp"`       // 决策时间
	SourcePath          string              `json:"-"`               // 离线读取时的源文件路径
	CycleNumber         int                 `json:"cycle_number"`    // 周期编号
	InputPrompt         string              `json:"input_prompt"`    // 发送给AI的输入prompt
	CoTTrace            string              `json:"cot_trace"`       // AI思维链（输出）
	DecisionJSON        string              `json:"decision_json"`   // 决策JSON
	AccountState        AccountSnapshot     `json:"account_state"`   // 账户状态快照
	Positions           []PositionSnapshot  `json:"positions"`       // 持仓快照
	CandidateCoins      []string            `json:"candidate_coins"` // 候选币种列表
	CandidateDetails    []CandidateSnapshot `json:"candidate_details,omitempty"`
	Decisions           []DecisionAction    `json:"decisions"`     // 执行的决策
	ExecutionLog        []string            `json:"execution_log"` // 执行日志
	Success             bool                `json:"success"`       // 是否成功
	ErrorMessage        string              `json:"error_message"` // 错误信息（如果有）
	RiskState           *RiskStateSnapshot  `json:"risk_state,omitempty"`
	DecisionMode        string              `json:"decision_mode,omitempty"`
	StrategyName        string              `json:"strategy_name,omitempty"`
	StrategyVersion     string              `json:"strategy_version,omitempty"`
	ConfigHash          string              `json:"config_hash,omitempty"`
	StrategyParams      map[string]any      `json:"strategy_params,omitempty"`
	StrategyDiagnostics map[string]any      `json:"strategy_diagnostics,omitempty"`
}

// RiskStateSnapshot 记录本周期可观测风险状态，保持旧日志兼容。
type RiskStateSnapshot struct {
	TraderID                 string                   `json:"trader_id,omitempty"`
	Exchange                 string                   `json:"exchange,omitempty"`
	MaxRiskPerTrade          float64                  `json:"max_risk_per_trade,omitempty"`
	EffectiveMaxRiskPerTrade float64                  `json:"effective_max_risk_per_trade,omitempty"`
	TotalRiskBudget          float64                  `json:"total_risk_budget,omitempty"`
	RemainingRiskBudget      float64                  `json:"remaining_risk_budget,omitempty"`
	MaxDailyLossPct          float64                  `json:"max_daily_loss_pct,omitempty"`
	MaxAccountDrawdownPct    float64                  `json:"max_account_drawdown_pct,omitempty"`
	AIBackoffUntil           string                   `json:"ai_backoff_until,omitempty"`
	ConsecutiveAIFails       int                      `json:"consecutive_ai_fails,omitempty"`
	OpenGateReasons          []string                 `json:"open_gate_reasons,omitempty"`
	OpenGateDiagnostics      []map[string]any         `json:"open_gate_diagnostics,omitempty"`
	FrequencyPolicy          *FrequencyPolicySnapshot `json:"frequency_policy,omitempty"`
	FrequencyState           *FrequencyStateSnapshot  `json:"frequency_state,omitempty"`
	LossMode                 *LossModeSnapshot        `json:"loss_mode,omitempty"`
}

// FrequencyPolicySnapshot 是日志层的开仓频率策略快照，避免 logger 依赖 decision 包。
type FrequencyPolicySnapshot struct {
	Mode                    string  `json:"mode"`
	EffectiveMode           string  `json:"effective_mode,omitempty"`
	AnalysisIntervalMin     int     `json:"analysis_interval_min"`
	PromptCandidateLimit    int     `json:"prompt_candidate_limit"`
	DailyOpenLimit          int     `json:"daily_open_limit,omitempty"`
	RollbackWindowHours     int     `json:"rollback_window_hours,omitempty"`
	RollbackMinProfitFactor float64 `json:"rollback_min_profit_factor,omitempty"`
	RollbackMaxDrawdownPct  float64 `json:"rollback_max_drawdown_pct,omitempty"`
	HighADXReportOnly       bool    `json:"high_adx_report_only"`
	RRReportOnly            bool    `json:"rr_report_only"`
	RollingGateReportOnly   bool    `json:"rolling_gate_report_only"`
}

// FrequencyStateSnapshot 是日志层的开仓频率运行时状态快照。
type FrequencyStateSnapshot struct {
	OpenCount24h       int     `json:"open_count_24h"`
	ClosedTrades24h    int     `json:"closed_trades_24h"`
	ProfitFactor24h    float64 `json:"profit_factor_24h"`
	Drawdown24hPct     float64 `json:"drawdown_24h_pct"`
	AutoRollbackActive bool    `json:"auto_rollback_active"`
	AutoRollbackReason string  `json:"auto_rollback_reason,omitempty"`
}

// LossModeSnapshot 是日志层的亏损模式状态快照，避免 logger 依赖 decision 包。
type LossModeSnapshot struct {
	Active          bool    `json:"active"`
	Reason          string  `json:"reason,omitempty"`
	CooldownUntil   string  `json:"cooldown_until,omitempty"`
	MaxRiskPerTrade float64 `json:"max_risk_per_trade,omitempty"`
	MaxPositions    int     `json:"max_positions,omitempty"`
	DailyOpenLimit  int     `json:"daily_open_limit,omitempty"`
	MinConfidence   int     `json:"min_confidence,omitempty"`
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
	Tier             string   `json:"tier,omitempty"`
	PoolScore        float64  `json:"pool_score,omitempty"`
	PoolReasons      []string `json:"pool_reasons,omitempty"`
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

	RiskUSD                  float64                           `json:"risk_usd,omitempty"`
	RequestedPositionSizeUSD float64                           `json:"requested_position_size_usd,omitempty"`
	AdjustedPositionSizeUSD  float64                           `json:"adjusted_position_size_usd,omitempty"`
	SizingAdjusted           bool                              `json:"sizing_adjusted,omitempty"`
	SizingReason             string                            `json:"sizing_reason,omitempty"`
	StopDistancePct          float64                           `json:"stop_distance_pct,omitempty"`
	StopDistanceRatio        float64                           `json:"stop_distance_ratio,omitempty"`
	StopDistancePercent      float64                           `json:"stop_distance_percent,omitempty"`
	TakeProfitRatio          float64                           `json:"take_profit_ratio,omitempty"`
	TakeProfitPercent        float64                           `json:"take_profit_percent,omitempty"`
	RequestedStopLoss        float64                           `json:"requested_stop_loss,omitempty"`
	RequestedTakeProfit      float64                           `json:"requested_take_profit,omitempty"`
	EffectiveStopLoss        float64                           `json:"effective_stop_loss,omitempty"`
	EffectiveTakeProfit      float64                           `json:"effective_take_profit,omitempty"`
	ExchangeFullTakeProfit   float64                           `json:"exchange_full_take_profit,omitempty"`
	ExchangeFullTPMode       string                            `json:"exchange_full_tp_mode,omitempty"`
	NetRR                    float64                           `json:"net_rr,omitempty"`
	ProfileName              string                            `json:"profile_name,omitempty"`
	FeeSlippageReserveUSD    float64                           `json:"fee_slippage_reserve_usd,omitempty"`
	TotalRiskUSD             float64                           `json:"total_risk_usd,omitempty"`
	TotalRiskPct             float64                           `json:"total_risk_pct,omitempty"`
	RiskCapReason            string                            `json:"risk_cap_reason,omitempty"`
	RiskNormalization        interface{}                       `json:"risk_normalization,omitempty"`
	EffectiveRiskPct         float64                           `json:"effective_risk_pct,omitempty"`
	GateState                string                            `json:"gate_state,omitempty"`
	GateReasons              []string                          `json:"gate_reasons,omitempty"`
	GateDiagnostics          map[string]any                    `json:"gate_diagnostics,omitempty"`
	Simulations              []OpenFrequencySimulationSnapshot `json:"simulations,omitempty"`
	ExecutionRisk            string                            `json:"execution_risk,omitempty"`
	StopLossSet              *bool                             `json:"stop_loss_set,omitempty"`
	TakeProfitSet            *bool                             `json:"take_profit_set,omitempty"`
	ProtectionError          string                            `json:"protection_error,omitempty"`
	HighRisk                 bool                              `json:"high_risk,omitempty"`
	HighRiskReason           string                            `json:"high_risk_reason,omitempty"`
	RemainingPositionUSD     float64                           `json:"remaining_position_usd,omitempty"`
	CloseSource              string                            `json:"close_source,omitempty"`
	ExchangeMetadata         bool                              `json:"exchange_metadata,omitempty"`
	CountedInStats           *bool                             `json:"counted_in_stats,omitempty"`
	StrategyMode             string                            `json:"strategy_mode,omitempty"`
	StrategyName             string                            `json:"strategy_name,omitempty"`
	StrategyVersion          string                            `json:"strategy_version,omitempty"`
	ConfigHash               string                            `json:"config_hash,omitempty"`
	SignalID                 string                            `json:"signal_id,omitempty"`
	SignalType               string                            `json:"signal_type,omitempty"`
	SignalTimeframe          string                            `json:"signal_timeframe,omitempty"`
	StructureTarget          float64                           `json:"structure_target,omitempty"`
	TradeIntent              string                            `json:"trade_intent,omitempty"`
	SignalCloseTime          int64                             `json:"signal_close_time,omitempty"`
	DecisionCloseTime        int64                             `json:"decision_close_time,omitempty"`
	StrategyMetadata         map[string]any                    `json:"strategy_metadata,omitempty"`
	StrategyDiagnostics      map[string]any                    `json:"strategy_diagnostics,omitempty"`
	RequestedClosePercentage float64                           `json:"requested_close_percentage,omitempty"`
	ExecutedClosePercentage  float64                           `json:"executed_close_percentage,omitempty"`
	FinalAction              string                            `json:"final_action,omitempty"`
	CloseQuantity            float64                           `json:"close_quantity,omitempty"`
	Explanation              any                               `json:"explanation,omitempty"`
}

// OpenFrequencySimulationSnapshot 是日志层的 report-only 开仓频率模拟结果。
type OpenFrequencySimulationSnapshot struct {
	Scenario        string         `json:"scenario"`
	Source          string         `json:"source,omitempty"`
	WouldAllow      bool           `json:"would_allow"`
	Reason          string         `json:"reason,omitempty"`
	OriginalState   string         `json:"original_state,omitempty"`
	SimulatedState  string         `json:"simulated_state,omitempty"`
	MinConfidence   int            `json:"min_confidence,omitempty"`
	EffectiveRisk   float64        `json:"effective_risk,omitempty"`
	AdjustedSizeUSD float64        `json:"adjusted_size_usd,omitempty"`
	Diagnostics     map[string]any `json:"diagnostics,omitempty"`
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
				case "open_long", "open_short", "add_long", "add_short":
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

	EventType                string  `json:"event_type,omitempty"` // full_close, auto_close, partial_close
	IsPartial                bool    `json:"is_partial,omitempty"`
	CloseQuantity            float64 `json:"close_quantity,omitempty"`
	RemainingQuantity        float64 `json:"remaining_quantity,omitempty"`
	RequestedClosePercentage float64 `json:"requested_close_percentage,omitempty"`
	ExecutedClosePercentage  float64 `json:"executed_close_percentage,omitempty"`
	OrderID                  int64   `json:"order_id,omitempty"`
	SignalID                 string  `json:"signal_id,omitempty"`
	StrategyName             string  `json:"strategy_name,omitempty"`
	StrategyVersion          string  `json:"strategy_version,omitempty"`
	Commission               float64 `json:"commission,omitempty"`
	PnLSource                string  `json:"pnl_source,omitempty"`
	Reconciled               *bool   `json:"reconciled,omitempty"`
	ReconciliationStatus     string  `json:"reconciliation_status,omitempty"`
	ReconciliationReason     string  `json:"reconciliation_reason,omitempty"`
}

// RecentClosedTradeStats 汇总指定窗口内闭合交易表现。
type RecentClosedTradeStats struct {
	ClosedTrades   int     `json:"closed_trades"`
	GrossProfit    float64 `json:"gross_profit"`
	GrossLoss      float64 `json:"gross_loss"`
	NetPnL         float64 `json:"net_pn_l"`
	ProfitFactor   float64 `json:"profit_factor"`
	MaxDrawdownUSD float64 `json:"max_drawdown_usd"`
}

// TradeEventStats 汇总完整平仓与部分平仓成交事件。
type TradeEventStats struct {
	TotalEvents              int     `json:"total_events"`
	FullCloseEvents          int     `json:"full_close_events"`
	AutoCloseEvents          int     `json:"auto_close_events"`
	PartialCloseEvents       int     `json:"partial_close_events"`
	PartialCloseRealizedPnL  float64 `json:"partial_close_realized_pnl"`
	PartialCloseEstimatedPnL float64 `json:"partial_close_estimated_pnl"`
	PartialCloseReconciled   int     `json:"partial_close_reconciled"`
	PartialClosePending      int     `json:"partial_close_pending"`
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
	TotalTrades       int                           `json:"total_trades"`   // 总交易数
	WinningTrades     int                           `json:"winning_trades"` // 盈利交易数
	LosingTrades      int                           `json:"losing_trades"`  // 亏损交易数
	WinRate           float64                       `json:"win_rate"`       // 胜率
	AvgWin            float64                       `json:"avg_win"`        // 平均盈利
	AvgLoss           float64                       `json:"avg_loss"`       // 平均亏损
	ProfitFactor      float64                       `json:"profit_factor"`  // 盈亏比
	SharpeRatio       float64                       `json:"sharpe_ratio"`   // 夏普比率（风险调整后收益）
	RecentTrades      []TradeOutcome                `json:"recent_trades"`  // 最近N笔交易
	RecentTradeEvents []TradeOutcome                `json:"recent_trade_events"`
	TradeEventStats   *TradeEventStats              `json:"trade_event_stats,omitempty"`
	Unmatched         []UnmatchedAction             `json:"unmatched,omitempty"`
	Rolling           *RollingPerformanceSnapshot   `json:"rolling,omitempty"`
	Execution         ExecutionQualityStats         `json:"execution_quality"`
	SymbolStats       map[string]*SymbolPerformance `json:"symbol_stats"` // 各币种表现
	BestSymbol        string                        `json:"best_symbol"`  // 表现最好的币种
	WorstSymbol       string                        `json:"worst_symbol"` // 表现最差的币种
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
	symbol            string
	side              string
	price             float64
	time              time.Time
	quantity          float64
	remainingQuantity float64
	leverage          int
	reasoning         string
}

type closedLotFragment struct {
	open     openPositionTrace
	quantity float64
}

// TradeReplayResult 同时保存完整闭合交易和可展示成交事件。
type TradeReplayResult struct {
	FullOutcomes []TradeOutcome
	Events       []TradeOutcome
	Unmatched    []UnmatchedAction
}

// BuildTradeReplay 将决策日志中的开平仓动作还原为完整交易与成交事件。
func BuildTradeReplay(records []*DecisionRecord) TradeReplayResult {
	if len(records) == 0 {
		return TradeReplayResult{FullOutcomes: []TradeOutcome{}, Events: []TradeOutcome{}, Unmatched: []UnmatchedAction{}}
	}

	sortedRecords := append([]*DecisionRecord(nil), records...)
	sort.SliceStable(sortedRecords, func(i, j int) bool {
		return sortedRecords[i].Timestamp.Before(sortedRecords[j].Timestamp)
	})

	openPositions := make(map[string][]openPositionTrace)
	var result TradeReplayResult
	var unmatched []UnmatchedAction

	for _, record := range sortedRecords {
		reasoningByAction := extractDecisionReasoning(record.DecisionJSON)
		for _, action := range record.Decisions {
			if !action.Success {
				continue
			}

			if action.Symbol == "" {
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

			switch action.Action {
			case "open_long", "open_short", "add_long", "add_short":
				side, ok := actionSide(action.Action)
				if !ok || action.Quantity <= 0 {
					continue
				}
				key := action.Symbol + "_" + side
				openPositions[key] = append(openPositions[key], openPositionTrace{
					symbol:            action.Symbol,
					side:              side,
					price:             action.Price,
					time:              actionTime,
					quantity:          action.Quantity,
					remainingQuantity: action.Quantity,
					leverage:          action.Leverage,
					reasoning:         reasoning,
				})
			case "close_long", "close_short", "auto_close_long", "auto_close_short":
				side, ok := actionSide(action.Action)
				if !ok {
					continue
				}
				key := action.Symbol + "_" + side
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
					if open.remainingQuantity <= 0 {
						continue
					}
					outcome := buildTradeOutcome(open, action, actionTime, reasoning)
					if strings.HasPrefix(action.Action, "auto_close_") {
						outcome.EventType = "auto_close"
					} else {
						outcome.EventType = "full_close"
					}
					outcome.CloseQuantity = outcome.Quantity
					outcome.OrderID = action.OrderID
					outcome.SignalID = action.SignalID
					outcome.StrategyName = action.StrategyName
					outcome.StrategyVersion = action.StrategyVersion
					result.FullOutcomes = append(result.FullOutcomes, outcome)
					result.Events = append(result.Events, outcome)
				}
				delete(openPositions, key)
			case "partial_close":
				if isSkippedPartialClose(action) {
					continue
				}
				closeQuantity := effectiveCloseQuantity(action)
				if closeQuantity <= 0 {
					continue
				}
				side, ok := resolvePartialCloseSide(action, record, openPositions)
				if !ok {
					unmatched = append(unmatched, UnmatchedAction{
						Timestamp: actionTime,
						Symbol:    action.Symbol,
						Action:    action.Action,
						Reason:    "missing_side_for_partial_close",
					})
					continue
				}
				key := action.Symbol + "_" + side
				fragments, unmatchedQty := consumeOpenLots(openPositions, key, closeQuantity)
				if len(fragments) == 0 {
					unmatched = append(unmatched, UnmatchedAction{
						Timestamp: actionTime,
						Symbol:    action.Symbol,
						Side:      side,
						Action:    action.Action,
						Reason:    "missing_open_for_partial_close",
					})
					continue
				}
				if unmatchedQty > 0 {
					unmatched = append(unmatched, UnmatchedAction{
						Timestamp: actionTime,
						Symbol:    action.Symbol,
						Side:      side,
						Action:    action.Action,
						Reason:    "partial_close_exceeds_open_quantity",
					})
				}
				event := buildTradeEventFromFragments(fragments, action, actionTime, reasoning, "partial_close")
				event.RemainingQuantity = remainingOpenQuantity(openPositions[key])
				result.Events = append(result.Events, event)
			}
		}
	}

	for _, opens := range openPositions {
		for _, open := range opens {
			if open.remainingQuantity <= 0 {
				continue
			}
			unmatched = append(unmatched, UnmatchedAction{
				Timestamp: open.time,
				Symbol:    open.symbol,
				Side:      open.side,
				Action:    "open_" + open.side,
				Reason:    "missing_close",
			})
		}
	}

	result.Unmatched = unmatched
	if result.FullOutcomes == nil {
		result.FullOutcomes = []TradeOutcome{}
	}
	if result.Events == nil {
		result.Events = []TradeOutcome{}
	}
	if result.Unmatched == nil {
		result.Unmatched = []UnmatchedAction{}
	}
	return result
}

// BuildTradeOutcomes 将决策日志中的开平仓动作配对为可复盘的闭合交易。
func BuildTradeOutcomes(records []*DecisionRecord) ([]TradeOutcome, []UnmatchedAction) {
	result := BuildTradeReplay(records)
	return result.FullOutcomes, result.Unmatched
}

// BuildTradeEvents 返回完整平仓、自动平仓和部分平仓成交事件。
func BuildTradeEvents(records []*DecisionRecord) ([]TradeOutcome, []UnmatchedAction) {
	result := BuildTradeReplay(records)
	return result.Events, result.Unmatched
}

// CountSuccessfulOpens 统计窗口内成功新增开仓数。
func CountSuccessfulOpens(records []*DecisionRecord, since time.Time, traderID string) int {
	count := 0
	for _, record := range records {
		if record == nil {
			continue
		}
		if traderID != "" && !recordMatchesTrader(record, traderID) {
			continue
		}
		for _, action := range record.Decisions {
			if !isOpenAction(action.Action) || !action.Success {
				continue
			}
			actionTime := action.Timestamp
			if actionTime.IsZero() {
				actionTime = record.Timestamp
			}
			if !since.IsZero() && actionTime.Before(since) {
				continue
			}
			count++
		}
	}
	return count
}

// BuildRecentClosedTradeStats 统计窗口内闭合交易的 PF 和最大权益回撤。
func BuildRecentClosedTradeStats(outcomes []TradeOutcome, since time.Time) RecentClosedTradeStats {
	filtered := make([]TradeOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		if !since.IsZero() && outcome.CloseTime.Before(since) {
			continue
		}
		filtered = append(filtered, outcome)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CloseTime.Before(filtered[j].CloseTime)
	})

	var stats RecentClosedTradeStats
	var curve, peak float64
	for _, outcome := range filtered {
		stats.ClosedTrades++
		stats.NetPnL += outcome.PnL
		if outcome.PnL > 0 {
			stats.GrossProfit += outcome.PnL
		} else if outcome.PnL < 0 {
			stats.GrossLoss += -outcome.PnL
		}
		curve += outcome.PnL
		if curve > peak {
			peak = curve
		}
		drawdown := peak - curve
		if drawdown > stats.MaxDrawdownUSD {
			stats.MaxDrawdownUSD = drawdown
		}
	}
	if stats.GrossLoss > 0 {
		stats.ProfitFactor = stats.GrossProfit / stats.GrossLoss
	} else if stats.GrossProfit > 0 {
		stats.ProfitFactor = 999
	}
	return stats
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
	return action == "open_long" || action == "open_short" || action == "add_long" || action == "add_short"
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

func effectiveCloseQuantity(action DecisionAction) float64 {
	if value, ok := metadataFloat(action.StrategyMetadata, "filled_quantity"); ok && value > 0 {
		return value
	}
	if action.CloseQuantity > 0 {
		return action.CloseQuantity
	}
	return action.Quantity
}

func isSkippedPartialClose(action DecisionAction) bool {
	return action.FinalAction == "partial_close_skipped" || effectiveCloseQuantity(action) <= 0
}

func resolvePartialCloseSide(action DecisionAction, record *DecisionRecord, openPositions map[string][]openPositionTrace) (string, bool) {
	if side, ok := metadataString(action.StrategyMetadata, "side"); ok {
		side = strings.ToLower(side)
		if side == "long" || side == "short" {
			return side, true
		}
	}
	if record != nil {
		var found string
		for _, pos := range record.Positions {
			if pos.Symbol != action.Symbol || pos.Side == "" {
				continue
			}
			side := strings.ToLower(pos.Side)
			if side != "long" && side != "short" {
				continue
			}
			if found != "" && found != side {
				return "", false
			}
			found = side
		}
		if found != "" {
			return found, true
		}
	}

	var found string
	for _, side := range []string{"long", "short"} {
		for _, open := range openPositions[action.Symbol+"_"+side] {
			if open.remainingQuantity <= 0 {
				continue
			}
			if found != "" && found != side {
				return "", false
			}
			found = side
			break
		}
	}
	if found != "" {
		return found, true
	}
	return "", false
}

func consumeOpenLots(openPositions map[string][]openPositionTrace, key string, closeQuantity float64) ([]closedLotFragment, float64) {
	opens := openPositions[key]
	if closeQuantity <= 0 || len(opens) == 0 {
		return nil, closeQuantity
	}
	remainingToClose := closeQuantity
	fragments := make([]closedLotFragment, 0, len(opens))
	nextOpens := opens[:0]
	for _, open := range opens {
		if open.remainingQuantity <= 0 {
			continue
		}
		if remainingToClose <= 0 {
			nextOpens = append(nextOpens, open)
			continue
		}
		closed := math.Min(open.remainingQuantity, remainingToClose)
		if closed > 0 {
			fragments = append(fragments, closedLotFragment{open: open, quantity: closed})
			open.remainingQuantity -= closed
			remainingToClose -= closed
		}
		if open.remainingQuantity > 1e-12 {
			nextOpens = append(nextOpens, open)
		}
	}
	if len(nextOpens) == 0 {
		delete(openPositions, key)
	} else {
		openPositions[key] = nextOpens
	}
	return fragments, math.Max(0, remainingToClose)
}

func remainingOpenQuantity(opens []openPositionTrace) float64 {
	total := 0.0
	for _, open := range opens {
		if open.remainingQuantity > 0 {
			total += open.remainingQuantity
		}
	}
	return total
}

func buildTradeEventFromFragments(fragments []closedLotFragment, action DecisionAction, closeTime time.Time, closeReason string, eventType string) TradeOutcome {
	if len(fragments) == 0 {
		return TradeOutcome{}
	}
	closePrice := action.Price
	if value, ok := metadataFloat(action.StrategyMetadata, "avg_fill_price"); ok && value > 0 {
		closePrice = value
	}

	totalQty := 0.0
	positionValue := 0.0
	estimatedPnL := 0.0
	weightedOpenPrice := 0.0
	leverage := fragments[0].open.leverage
	if leverage <= 0 {
		leverage = 1
	}
	openTime := fragments[0].open.time
	openReason := fragments[0].open.reasoning
	symbol := fragments[0].open.symbol
	side := fragments[0].open.side
	for _, fragment := range fragments {
		qty := fragment.quantity
		if qty <= 0 {
			continue
		}
		totalQty += qty
		positionValue += qty * fragment.open.price
		weightedOpenPrice += qty * fragment.open.price
		if fragment.open.time.Before(openTime) {
			openTime = fragment.open.time
		}
		if openReason == "" {
			openReason = fragment.open.reasoning
		}
		if side == "long" {
			estimatedPnL += qty * (closePrice - fragment.open.price)
		} else {
			estimatedPnL += qty * (fragment.open.price - closePrice)
		}
	}
	openPrice := 0.0
	if totalQty > 0 {
		openPrice = weightedOpenPrice / totalQty
	}

	pnl := estimatedPnL
	pnlSource := "estimated"
	reconciled := false
	reconciliationStatus := "estimated_from_decision_log"
	reconciliationReason := ""
	if value, ok := metadataFloat(action.StrategyMetadata, "realized_pnl"); ok {
		pnl = value
		pnlSource = "exchange"
	}
	if value, ok := metadataBool(action.StrategyMetadata, "reconciled"); ok {
		reconciled = value
		if value && pnlSource == "exchange" {
			reconciliationStatus = "matched"
		}
	}
	if status, ok := metadataString(action.StrategyMetadata, "reconciliation_status"); ok {
		reconciliationStatus = status
	}
	if reason, ok := metadataString(action.StrategyMetadata, "reconciliation_reason"); ok {
		reconciliationReason = reason
	}
	commission, _ := metadataFloat(action.StrategyMetadata, "commission")

	marginUsed := 0.0
	if leverage > 0 {
		marginUsed = positionValue / float64(leverage)
	}
	pnlPct := 0.0
	if marginUsed > 0 {
		pnlPct = (pnl / marginUsed) * 100
	}

	return TradeOutcome{
		Symbol:                   symbol,
		Side:                     side,
		Quantity:                 totalQty,
		Leverage:                 leverage,
		OpenPrice:                openPrice,
		ClosePrice:               closePrice,
		PositionValue:            positionValue,
		MarginUsed:               marginUsed,
		PnL:                      pnl,
		PnLPct:                   pnlPct,
		Duration:                 closeTime.Sub(openTime).String(),
		OpenTime:                 openTime,
		CloseTime:                closeTime,
		WasStopLoss:              pnl < 0,
		OpenReason:               openReason,
		CloseReason:              closeReason,
		EventType:                eventType,
		IsPartial:                eventType == "partial_close",
		CloseQuantity:            totalQty,
		RequestedClosePercentage: action.RequestedClosePercentage,
		ExecutedClosePercentage:  action.ExecutedClosePercentage,
		OrderID:                  action.OrderID,
		SignalID:                 action.SignalID,
		StrategyName:             action.StrategyName,
		StrategyVersion:          action.StrategyVersion,
		Commission:               commission,
		PnLSource:                pnlSource,
		Reconciled:               &reconciled,
		ReconciliationStatus:     reconciliationStatus,
		ReconciliationReason:     reconciliationReason,
	}
}

func buildTradeOutcome(open openPositionTrace, closeAction DecisionAction, closeTime time.Time, closeReason string) TradeOutcome {
	leverage := open.leverage
	if leverage <= 0 {
		leverage = 1
	}
	quantity := open.remainingQuantity
	if quantity <= 0 {
		quantity = open.quantity
	}

	var pnl float64
	if open.side == "long" {
		pnl = quantity * (closeAction.Price - open.price)
	} else {
		pnl = quantity * (open.price - closeAction.Price)
	}

	positionValue := quantity * open.price
	marginUsed := positionValue / float64(leverage)
	pnlPct := 0.0
	if marginUsed > 0 {
		pnlPct = (pnl / marginUsed) * 100
	}

	return TradeOutcome{
		Symbol:        open.symbol,
		Side:          open.side,
		Quantity:      quantity,
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

func metadataString(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	default:
		text := fmt.Sprint(v)
		if strings.TrimSpace(text) == "" {
			return "", false
		}
		return text, true
	}
}

func metadataFloat(values map[string]any, key string) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func metadataBool(values map[string]any, key string) (bool, bool) {
	if len(values) == 0 {
		return false, false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false, false
	}
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return parsed, err == nil
	default:
		return false, false
	}
}

func actionSide(action string) (string, bool) {
	switch action {
	case "open_long", "add_long", "close_long", "auto_close_long":
		return "long", true
	case "open_short", "add_short", "close_short", "auto_close_short":
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
			RecentTrades:      []TradeOutcome{},
			RecentTradeEvents: []TradeOutcome{},
			TradeEventStats:   &TradeEventStats{},
			SymbolStats:       make(map[string]*SymbolPerformance),
		}, nil
	}

	analysis := &PerformanceAnalysis{
		RecentTrades:      []TradeOutcome{},
		RecentTradeEvents: []TradeOutcome{},
		TradeEventStats:   &TradeEventStats{},
		SymbolStats:       make(map[string]*SymbolPerformance),
	}

	// 为了避免开仓记录在窗口外导致匹配失败，需要先从所有历史记录中找出未平仓的持仓
	// 获取更多历史记录来构建完整的持仓状态（使用更大的窗口）
	allRecords, err := l.GetLatestRecords(lookbackCycles * 3) // 扩大3倍窗口
	if err != nil || len(allRecords) == 0 {
		allRecords = records
	}

	replay := BuildTradeReplay(allRecords)
	outcomes := replay.FullOutcomes
	unmatched := replay.Unmatched
	analysis.Rolling = BuildRollingPerformance(outcomes, time.Now())
	analysis.Execution = BuildExecutionQuality(allRecords, len(unmatched))
	windowStart := earliestRecordEventTime(records)
	windowEvents := make([]TradeOutcome, 0, len(replay.Events))
	for _, event := range replay.Events {
		if !windowStart.IsZero() && event.CloseTime.Before(windowStart) {
			continue
		}
		windowEvents = append(windowEvents, event)
	}
	stats := BuildTradeEventStats(windowEvents)
	analysis.TradeEventStats = &stats
	analysis.RecentTradeEvents = recentEventsDescending(windowEvents, 100)

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

func BuildTradeEventStats(events []TradeOutcome) TradeEventStats {
	var stats TradeEventStats
	for _, event := range events {
		stats.TotalEvents++
		switch event.EventType {
		case "partial_close":
			stats.PartialCloseEvents++
			if event.PnLSource == "exchange" {
				stats.PartialCloseRealizedPnL += event.PnL
			} else {
				stats.PartialCloseEstimatedPnL += event.PnL
			}
			if event.Reconciled != nil && *event.Reconciled {
				stats.PartialCloseReconciled++
			} else {
				stats.PartialClosePending++
			}
		case "auto_close":
			stats.AutoCloseEvents++
		default:
			stats.FullCloseEvents++
		}
	}
	return stats
}

func recentEventsDescending(events []TradeOutcome, limit int) []TradeOutcome {
	if len(events) == 0 {
		return []TradeOutcome{}
	}
	result := append([]TradeOutcome(nil), events...)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CloseTime.After(result[j].CloseTime)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
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
