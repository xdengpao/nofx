package decision

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// 格式化工具
// ============================================================================

// FormatDuration 格式化时长
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f秒", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0f分钟", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f小时", d.Hours())
	}
	return fmt.Sprintf("%.1f天", d.Hours()/24)
}

// FormatPnL 格式化盈亏
func FormatPnL(pnl float64) string {
	if pnl > 0 {
		return fmt.Sprintf("+%.2f%%", pnl)
	}
	return fmt.Sprintf("%.2f%%", pnl)
}

// FormatPrice 格式化价格
func FormatPrice(price float64) string {
	if price >= 1000 {
		return fmt.Sprintf("%.2f", price)
	}
	if price >= 1 {
		return fmt.Sprintf("%.4f", price)
	}
	return fmt.Sprintf("%.6f", price)
}

// FormatUSD 格式化USD金额
func FormatUSD(amount float64) string {
	if amount >= 1000000 {
		return fmt.Sprintf("%.2fM", amount/1000000)
	}
	if amount >= 1000 {
		return fmt.Sprintf("%.2fK", amount/1000)
	}
	return fmt.Sprintf("%.2f", amount)
}

// ============================================================================
// 字符串工具
// ============================================================================

// TruncateString 截断字符串
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// CleanSymbol 清理交易对符号
func CleanSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !strings.HasSuffix(symbol, "USDT") {
		symbol += "USDT"
	}
	return symbol
}

// ============================================================================
// 验证工具
// ============================================================================

// IsValidSymbol 验证交易对是否有效
func IsValidSymbol(symbol string) bool {
	if len(symbol) < 5 {
		return false
	}
	if !strings.HasSuffix(symbol, "USDT") {
		return false
	}
	// 检查是否只包含字母和数字
	for _, c := range symbol {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// IsValidAction 验证动作是否有效
func IsValidAction(action string) bool {
	validActions := map[string]bool{
		"open_long":        true,
		"open_short":       true,
		"close_long":       true,
		"close_short":      true,
		"partial_close":    true,
		"update_stop_loss": true,
		"hold":             true,
		"wait":             true,
	}
	return validActions[action]
}

// IsValidDirection 验证方向是否有效
func IsValidDirection(direction string) bool {
	return direction == "long" || direction == "short"
}

// ============================================================================
// 时间工具
// ============================================================================

// GetCurrentTimeString 获取当前时间字符串
func GetCurrentTimeString() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// ParseTimeString 解析时间字符串
func ParseTimeString(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("无法解析时间: %s", s)
}

// IsWithinTradingHours 检查是否在交易时间内（加密货币24/7，主要用于避免低流动性时段）
func IsWithinTradingHours() bool {
	hour := time.Now().UTC().Hour()
	// 避免UTC 0-4点（流动性较低）
	return hour >= 4 || hour < 0
}

// ============================================================================
// 数学工具
// ============================================================================

// Min 返回最小值
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Max 返回最大值
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Clamp 限制值在范围内
func Clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// RoundToDecimal 四舍五入到指定小数位
func RoundToDecimal(value float64, decimals int) float64 {
	multiplier := 1.0
	for i := 0; i < decimals; i++ {
		multiplier *= 10
	}
	return float64(int(value*multiplier+0.5)) / multiplier
}

// ============================================================================
// 日志格式化
// ============================================================================

// LogDecision 格式化决策日志
func LogDecision(d *Decision) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📋 决策: %s %s\n", d.Symbol, d.Action))

	if d.Action == "open_long" || d.Action == "open_short" {
		sb.WriteString(fmt.Sprintf("   杠杆: %dx | 仓位: %.2f USD\n", d.Leverage, d.PositionSizeUSD))
		sb.WriteString(fmt.Sprintf("   止损: %s | 止盈: %s\n", FormatPrice(d.StopLoss), FormatPrice(d.TakeProfit)))
		sb.WriteString(fmt.Sprintf("   置信度: %d%% | 风险: %.2f USD\n", d.Confidence, d.RiskUSD))
	}

	if d.NewStopLoss > 0 {
		sb.WriteString(fmt.Sprintf("   新止损: %s\n", FormatPrice(d.NewStopLoss)))
	}

	if d.ClosePercentage > 0 {
		sb.WriteString(fmt.Sprintf("   平仓比例: %.0f%%\n", d.ClosePercentage))
	}

	sb.WriteString(fmt.Sprintf("   理由: %s\n", TruncateString(d.Reasoning, 100)))

	return sb.String()
}

// LogPlan 格式化计划日志
func LogPlan(p *TradePlan) string {
	var sb strings.Builder

	holdTime := time.Since(p.CreatedAt)

	sb.WriteString(fmt.Sprintf("📋 计划: %s %s\n", p.Symbol, p.Direction))
	sb.WriteString(fmt.Sprintf("   入场: %s | 当前止损: %s | 止盈: %s\n",
		FormatPrice(p.EntryPrice), FormatPrice(p.CurrentStopLoss), FormatPrice(p.TakeProfit)))
	sb.WriteString(fmt.Sprintf("   持仓时间: %s | 峰值盈利: %.2f%%\n",
		FormatDuration(holdTime), p.PeakPnLPercent))

	if p.TotalClosedPercent > 0 {
		sb.WriteString(fmt.Sprintf("   已平仓: %.0f%%\n", p.TotalClosedPercent))
	}

	if p.InvalidationCondition != "" {
		sb.WriteString(fmt.Sprintf("   失效条件: %s\n", FormatInvalidationCondition(p.InvalidationCondition)))
	}

	return sb.String()
}

// ============================================================================
// 状态格式化
// ============================================================================

// FormatAccountStatus 格式化账户状态
func FormatAccountStatus(account *AccountInfo) string {
	return fmt.Sprintf("净值: %s | 可用: %s | 保证金使用率: %.1f%% | 持仓: %d",
		FormatUSD(account.TotalEquity),
		FormatUSD(account.AvailableBalance),
		account.MarginUsedPct,
		account.PositionCount)
}

// FormatPositionStatus 格式化持仓状态
func FormatPositionStatus(pos *PositionInfo) string {
	return fmt.Sprintf("%s %s: 入场 %s | 当前 %s | 盈亏 %s",
		pos.Symbol,
		strings.ToUpper(pos.Side),
		FormatPrice(pos.EntryPrice),
		FormatPrice(pos.MarkPrice),
		FormatPnL(pos.UnrealizedPnLPct))
}

// ============================================================================
// 导出支持的失效条件
// ============================================================================

// GetSupportedInvalidationConditions 获取支持的失效条件说明
func GetSupportedInvalidationConditions() string {
	return `
支持的失效条件格式:
═══════════════════════════════════════════════════════════════

1. EMA死叉 (多单失效)
   格式: 4H:EMA_CROSS_DOWN:EMA20:EMA50

2. EMA金叉 (空单失效)
   格式: 4H:EMA_CROSS_UP:EMA20:EMA50

3. 价格跌破 (多单失效)
   格式: 4H:PRICE_BELOW:EMA50 或 4H:PRICE_BELOW:95000

4. 价格突破 (空单失效)
   格式: 1H:PRICE_ABOVE:EMA20 或 1H:PRICE_ABOVE:100000

5. RSI超买/超卖
   格式: 4H:RSI_ABOVE:70 或 4H:RSI_BELOW:30

6. ADX减弱
   格式: 4H:ADX_BELOW:20

7. MACD交叉
   格式: 4H:MACD_CROSS:DOWN 或 4H:MACD_CROSS:UP

8. 趋势反转
   格式: 4H:TREND_REVERSAL

═══════════════════════════════════════════════════════════════
时间框架: 4H, 1H, 30M, 15M, 1D
`
}

// ValidateInvalidationCondition 验证失效条件格式
func ValidateInvalidationCondition(condition string) (bool, string) {
	if condition == "" {
		return false, "失效条件为空"
	}

	parsed := ParseInvalidationCondition(condition)
	if !parsed.IsValid {
		return false, fmt.Sprintf("解析失败: %s", parsed.ParseError)
	}

	validTimeframes := map[string]bool{"4H": true, "1H": true, "15M": true, "30M": true, "1D": true}
	if parsed.Timeframe != "" && !validTimeframes[parsed.Timeframe] {
		return false, fmt.Sprintf("不支持的时间框架: %s", parsed.Timeframe)
	}

	return true, FormatInvalidationCondition(condition)
}
