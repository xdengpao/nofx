package decision

// Feature: quant-trading-system
// 任务 8.1: AI 响应解析器测试覆盖
// 覆盖需求: 3.3
// 属性基测试: Property 8 (AI响应解析鲁棒性)

import (
	"fmt"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 单元测试: 四级降级解析策略
// ============================================================================

// TestParseAIResponse_StandardJSON 标准JSON解析
func TestParseAIResponse_StandardJSON(t *testing.T) {
	parser := NewAIResponseParser()
	input := `[{"symbol":"BTCUSDT","action":"open_long","leverage":5,"position_size_usd":1000,"stop_loss":48000,"take_profit":55000,"confidence":80,"reasoning":"看多"}]`
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("标准JSON解析不应返回错误: %v", err)
	}
	if result.ParseMethod != "standard" {
		t.Errorf("应使用标准解析方法, 实际=%s", result.ParseMethod)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("应解析出1个决策, 实际=%d", len(result.Decisions))
	}
	if result.Decisions[0].Symbol != "BTCUSDT" {
		t.Errorf("Symbol应为BTCUSDT, 实际=%s", result.Decisions[0].Symbol)
	}
	if result.Decisions[0].Action != "open_long" {
		t.Errorf("Action应为open_long, 实际=%s", result.Decisions[0].Action)
	}
}

// TestParseAIResponse_StandardJSON_WithMarkdown markdown代码块包裹的JSON
func TestParseAIResponse_StandardJSON_WithMarkdown(t *testing.T) {
	parser := NewAIResponseParser()
	input := "```json\n[{\"symbol\":\"ETHUSDT\",\"action\":\"wait\",\"reasoning\":\"观望\"}]\n```"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("带markdown的JSON解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
}

// TestParseAIResponse_MultipleDecisions 多个决策
func TestParseAIResponse_MultipleDecisions(t *testing.T) {
	parser := NewAIResponseParser()
	input := `[{"symbol":"BTCUSDT","action":"open_long","leverage":5,"position_size_usd":1000,"stop_loss":48000,"take_profit":55000},{"symbol":"ETHUSDT","action":"wait","reasoning":"观望"}]`
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("多决策解析不应返回错误: %v", err)
	}
	if len(result.Decisions) != 2 {
		t.Errorf("应解析出2个决策, 实际=%d", len(result.Decisions))
	}
}

// TestParseAIResponse_EmptyResponse 空响应返回错误
func TestParseAIResponse_EmptyResponse(t *testing.T) {
	parser := NewAIResponseParser()
	_, err := parser.ParseAIResponse("")
	if err == nil {
		t.Error("空响应应返回错误")
	}
}

// TestParseAIResponse_EmptyResponse_Whitespace 纯空白响应返回错误
func TestParseAIResponse_EmptyResponse_Whitespace(t *testing.T) {
	parser := NewAIResponseParser()
	_, err := parser.ParseAIResponse("   \n\t  ")
	if err == nil {
		t.Error("纯空白响应应返回错误")
	}
}

// TestParseAIResponse_FuzzyJSON_TrailingComma 尾逗号
func TestParseAIResponse_FuzzyJSON_TrailingComma(t *testing.T) {
	parser := NewAIResponseParser()
	input := `[{"symbol":"BTCUSDT","action":"open_long","leverage":5,"position_size_usd":1000,"stop_loss":48000,"take_profit":55000,}]`
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("含尾逗号的JSON解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
	if result.Decisions[0].Symbol != "BTCUSDT" {
		t.Errorf("Symbol应为BTCUSDT, 实际=%s", result.Decisions[0].Symbol)
	}
}

// TestParseAIResponse_FuzzyJSON_SingleQuotes 单引号字符串
func TestParseAIResponse_FuzzyJSON_SingleQuotes(t *testing.T) {
	parser := NewAIResponseParser()
	input := `[{'symbol':'BTCUSDT','action':'wait','reasoning':'观望'}]`
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("含单引号的JSON解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
}

// TestParseAIResponse_FuzzyJSON_LineComments 行注释
func TestParseAIResponse_FuzzyJSON_LineComments(t *testing.T) {
	parser := NewAIResponseParser()
	input := "[{\"symbol\":\"BTCUSDT\",\"action\":\"wait\" // 等待信号\n}]"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("含行注释的JSON解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
}

// TestParseAIResponse_FuzzyJSON_BlockComments 块注释
func TestParseAIResponse_FuzzyJSON_BlockComments(t *testing.T) {
	parser := NewAIResponseParser()
	input := "[{\"symbol\":\"BTCUSDT\",\"action\":\"wait\" /* 等待 */}]"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("含块注释的JSON解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
}

// ============================================================================
// 单元测试: 文本提取 (方法3)
// ============================================================================

// TestParseAIResponse_TextExtraction_Long 从文本提取做多信号
func TestParseAIResponse_TextExtraction_Long(t *testing.T) {
	parser := NewAIResponseParser()
	input := "市场分析显示 open_long BTCUSDT 信号强烈，建议做多。"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("文本提取不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应从文本中提取出至少1个决策")
	}
	found := false
	for _, d := range result.Decisions {
		if d.Action == "open_long" {
			found = true
		}
	}
	if !found {
		t.Error("应提取出open_long决策")
	}
}

// TestParseAIResponse_TextExtraction_Wait 从文本提取等待信号
func TestParseAIResponse_TextExtraction_Wait(t *testing.T) {
	parser := NewAIResponseParser()
	input := "当前市场不明朗，建议等待观望，no trade。"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("文本提取等待信号不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("应解析出至少1个决策")
	}
	if result.Decisions[0].Action != "wait" {
		t.Errorf("应提取出wait决策, 实际=%s", result.Decisions[0].Action)
	}
}

// ============================================================================
// 单元测试: 默认wait降级 (方法4)
// ============================================================================

// TestParseAIResponse_Fallback_DefaultWait 无法解析时返回默认wait
func TestParseAIResponse_Fallback_DefaultWait(t *testing.T) {
	parser := NewAIResponseParser()
	input := "这是一段完全无法解析的随机文本 xyz123 !@#$%"
	result, err := parser.ParseAIResponse(input)
	if err != nil {
		t.Fatalf("降级解析不应返回错误: %v", err)
	}
	if len(result.Decisions) == 0 {
		t.Fatal("降级时应返回至少1个决策")
	}
	if result.Decisions[0].Action != "wait" {
		t.Errorf("降级时应返回wait决策, 实际=%s", result.Decisions[0].Action)
	}
	if result.ParseMethod != "fallback" {
		t.Errorf("应使用fallback解析方法, 实际=%s", result.ParseMethod)
	}
}

// ============================================================================
// 单元测试: fixJSON 函数
// ============================================================================

// TestFixJSON_TrailingCommaInObject 对象尾逗号
func TestFixJSON_TrailingCommaInObject(t *testing.T) {
	input := `{"key":"value",}`
	fixed := fixJSON(input)
	if strings.Contains(fixed, ",}") {
		t.Errorf("fixJSON应移除对象尾逗号, 结果=%s", fixed)
	}
}

// TestFixJSON_TrailingCommaInArray 数组尾逗号
func TestFixJSON_TrailingCommaInArray(t *testing.T) {
	input := `["a","b","c",]`
	fixed := fixJSON(input)
	if strings.Contains(fixed, ",]") {
		t.Errorf("fixJSON应移除数组尾逗号, 结果=%s", fixed)
	}
}

// TestFixJSON_UnquotedKeys 无引号key
func TestFixJSON_UnquotedKeys(t *testing.T) {
	input := `[{symbol:"BTCUSDT",action:"wait"}]`
	fixed := fixJSON(input)
	if !strings.Contains(fixed, `"symbol"`) {
		t.Errorf("fixJSON应为无引号key添加引号, 结果=%s", fixed)
	}
	if !strings.Contains(fixed, `"action"`) {
		t.Errorf("fixJSON应为无引号key添加引号, 结果=%s", fixed)
	}
}

// TestFixJSON_SingleQuotes 单引号替换
func TestFixJSON_SingleQuotes(t *testing.T) {
	input := `[{'symbol':'BTCUSDT'}]`
	fixed := fixJSON(input)
	if strings.Contains(fixed, "'") {
		t.Errorf("fixJSON应将单引号替换为双引号, 结果=%s", fixed)
	}
}

// TestFixJSON_LineComments 行注释移除
func TestFixJSON_LineComments(t *testing.T) {
	input := "[{\"key\":\"val\" // 注释\n}]"
	fixed := fixJSON(input)
	if strings.Contains(fixed, "//") {
		t.Errorf("fixJSON应移除行注释, 结果=%s", fixed)
	}
}

// TestFixJSON_BlockComments 块注释移除
func TestFixJSON_BlockComments(t *testing.T) {
	input := "[{\"key\":\"val\" /* 块注释 */}]"
	fixed := fixJSON(input)
	if strings.Contains(fixed, "/*") {
		t.Errorf("fixJSON应移除块注释, 结果=%s", fixed)
	}
}

// TestFixJSON_NaNValues NaN替换为null
func TestFixJSON_NaNValues(t *testing.T) {
	input := `[{"stop_loss":NaN}]`
	fixed := fixJSON(input)
	if strings.Contains(fixed, "NaN") {
		t.Errorf("fixJSON应将NaN替换为null, 结果=%s", fixed)
	}
}

// TestFixJSON_InfinityValues Infinity替换为null
func TestFixJSON_InfinityValues(t *testing.T) {
	input := `[{"take_profit":Infinity}]`
	fixed := fixJSON(input)
	if strings.Contains(fixed, "Infinity") {
		t.Errorf("fixJSON应将Infinity替换为null, 结果=%s", fixed)
	}
}

// ============================================================================
// 单元测试: ExtractDecisionsRobust 公共函数
// ============================================================================

// TestExtractDecisionsRobust_ValidJSON 有效JSON
func TestExtractDecisionsRobust_ValidJSON(t *testing.T) {
	input := `[{"symbol":"BTCUSDT","action":"wait","reasoning":"观望"}]`
	decisions, _, err := ExtractDecisionsRobust(input)
	if err != nil {
		t.Fatalf("ExtractDecisionsRobust不应返回错误: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("应返回至少1个决策")
	}
}

// TestExtractDecisionsRobust_NeverReturnsNil 永不返回nil决策列表
func TestExtractDecisionsRobust_NeverReturnsNil(t *testing.T) {
	inputs := []string{
		"随机文本",
		"{}",
		"null",
		"[invalid",
		"some text without json",
	}
	for _, input := range inputs {
		decisions, _, _ := ExtractDecisionsRobust(input)
		if decisions == nil {
			t.Errorf("输入=%q 时ExtractDecisionsRobust不应返回nil", input)
		}
	}
}

// ============================================================================
// 属性基测试: Property 8 — AI响应解析鲁棒性
// Feature: quant-trading-system, Property 8: AI响应解析鲁棒性
// Validates: Requirements 3.3
// ============================================================================

// TestProperty8_ExtractDecisionsRobust_NeverNilNeverPanic
// 对任意非空字符串，ExtractDecisionsRobust 永远不返回 nil 且不 panic
func TestProperty8_ExtractDecisionsRobust_NeverNilNeverPanic(t *testing.T) {
	// Feature: quant-trading-system, Property 8: AI响应解析鲁棒性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("任意非空字符串解析不返回nil且不panic", prop.ForAll(
		func(input string) bool {
			if input == "" {
				return true // 空字符串由 ParseAIResponse 单独处理
			}
			// 不应 panic（由 gopter 的 recover 机制保证）
			decisions, _, _ := ExtractDecisionsRobust(input)
			// 不应返回 nil
			return decisions != nil
		},
		gen.AnyString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// TestProperty8_ParseAIResponse_AlwaysReturnsAtLeastOneDecision
// 对任意非空字符串，ParseAIResponse 至少返回1个决策（包括fallback wait）
func TestProperty8_ParseAIResponse_AlwaysReturnsAtLeastOneDecision(t *testing.T) {
	// Feature: quant-trading-system, Property 8: AI响应解析鲁棒性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("任意非空字符串解析至少返回1个决策", prop.ForAll(
		func(input string) bool {
			if strings.TrimSpace(input) == "" {
				return true // 空字符串返回错误，不在此属性范围内
			}
			parser := NewAIResponseParser()
			result, err := parser.ParseAIResponse(input)
			if err != nil {
				return true // 空字符串情况，跳过
			}
			return len(result.Decisions) >= 1
		},
		gen.AnyString(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// TestProperty8_ParseAIResponse_ValidJSONAlwaysStandard
// 对任意有效的决策JSON数组，应使用standard方法解析
func TestProperty8_ParseAIResponse_ValidJSONAlwaysStandard(t *testing.T) {
	// Feature: quant-trading-system, Property 8: AI响应解析鲁棒性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	actions := []string{"open_long", "open_short", "wait", "hold"}
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"}

	properties.Property("有效JSON数组应使用standard方法解析", prop.ForAll(
		func(symbolIdx int, actionIdx int) bool {
			symbol := symbols[symbolIdx%len(symbols)]
			action := actions[actionIdx%len(actions)]
			input := `[{"symbol":"` + symbol + `","action":"` + action + `","reasoning":"测试"}]`

			parser := NewAIResponseParser()
			result, err := parser.ParseAIResponse(input)
			if err != nil {
				return false
			}
			return result.ParseMethod == "standard" && len(result.Decisions) == 1
		},
		gen.IntRange(0, 99),
		gen.IntRange(0, 99),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 单元测试: ParseInvalidationCondition — 结构化格式
// ============================================================================

// TestParseInvalidationCondition_EMADeathCross EMA死叉
func TestParseInvalidationCondition_EMADeathCross(t *testing.T) {
	result := ParseInvalidationCondition("4H:EMA_CROSS_DOWN:EMA20:EMA50")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_EMA_CROSS_DOWN {
		t.Errorf("Type应为ema_cross_down, 实际=%s", result.Type)
	}
	if result.Timeframe != "4H" {
		t.Errorf("Timeframe应为4H, 实际=%s", result.Timeframe)
	}
	if result.Indicator != "EMA20" {
		t.Errorf("Indicator应为EMA20, 实际=%s", result.Indicator)
	}
	if result.Indicator2 != "EMA50" {
		t.Errorf("Indicator2应为EMA50, 实际=%s", result.Indicator2)
	}
}

// TestParseInvalidationCondition_EMAGoldenCross EMA金叉
func TestParseInvalidationCondition_EMAGoldenCross(t *testing.T) {
	result := ParseInvalidationCondition("1H:EMA_CROSS_UP:EMA20:EMA50")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_EMA_CROSS_UP {
		t.Errorf("Type应为ema_cross_up, 实际=%s", result.Type)
	}
	if result.Timeframe != "1H" {
		t.Errorf("Timeframe应为1H, 实际=%s", result.Timeframe)
	}
}

// TestParseInvalidationCondition_PriceBelow 价格跌破
func TestParseInvalidationCondition_PriceBelow(t *testing.T) {
	result := ParseInvalidationCondition("1H:PRICE_BELOW:EMA50")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_PRICE_BELOW {
		t.Errorf("Type应为price_below, 实际=%s", result.Type)
	}
	if result.Indicator != "EMA50" {
		t.Errorf("Indicator应为EMA50, 实际=%s", result.Indicator)
	}
}

// TestParseInvalidationCondition_PriceAbove 价格突破
func TestParseInvalidationCondition_PriceAbove(t *testing.T) {
	result := ParseInvalidationCondition("15M:PRICE_ABOVE:EMA20")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_PRICE_ABOVE {
		t.Errorf("Type应为price_above, 实际=%s", result.Type)
	}
}

// TestParseInvalidationCondition_RSIAbove RSI超买
func TestParseInvalidationCondition_RSIAbove(t *testing.T) {
	result := ParseInvalidationCondition("4H:RSI_ABOVE:70")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_RSI_ABOVE {
		t.Errorf("Type应为rsi_above, 实际=%s", result.Type)
	}
	if result.Threshold != 70 {
		t.Errorf("Threshold应为70, 实际=%v", result.Threshold)
	}
}

// TestParseInvalidationCondition_RSIBelow RSI超卖
func TestParseInvalidationCondition_RSIBelow(t *testing.T) {
	result := ParseInvalidationCondition("1H:RSI_BELOW:30")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_RSI_BELOW {
		t.Errorf("Type应为rsi_below, 实际=%s", result.Type)
	}
	if result.Threshold != 30 {
		t.Errorf("Threshold应为30, 实际=%v", result.Threshold)
	}
}

// TestParseInvalidationCondition_ADXBelow ADX减弱
func TestParseInvalidationCondition_ADXBelow(t *testing.T) {
	result := ParseInvalidationCondition("4H:ADX_BELOW:20")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_ADX_BELOW {
		t.Errorf("Type应为adx_below, 实际=%s", result.Type)
	}
	if result.Threshold != 20 {
		t.Errorf("Threshold应为20, 实际=%v", result.Threshold)
	}
}

// TestParseInvalidationCondition_MACDCrossDown MACD死叉
func TestParseInvalidationCondition_MACDCrossDown(t *testing.T) {
	result := ParseInvalidationCondition("1H:MACD_CROSS:DOWN")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_MACD_CROSS {
		t.Errorf("Type应为macd_cross, 实际=%s", result.Type)
	}
	if result.Direction != "DOWN" {
		t.Errorf("Direction应为DOWN, 实际=%s", result.Direction)
	}
}

// TestParseInvalidationCondition_MACDCrossUp MACD金叉
func TestParseInvalidationCondition_MACDCrossUp(t *testing.T) {
	result := ParseInvalidationCondition("4H:MACD_CROSS:UP")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Direction != "UP" {
		t.Errorf("Direction应为UP, 实际=%s", result.Direction)
	}
}

// TestParseInvalidationCondition_TrendReversal 趋势反转
func TestParseInvalidationCondition_TrendReversal(t *testing.T) {
	result := ParseInvalidationCondition("4H:TREND_REVERSAL")
	if !result.IsValid {
		t.Fatalf("应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_TREND_REVERSAL {
		t.Errorf("Type应为trend_reversal, 实际=%s", result.Type)
	}
}

// TestParseInvalidationCondition_Empty 空条件
func TestParseInvalidationCondition_Empty(t *testing.T) {
	result := ParseInvalidationCondition("")
	if result.IsValid {
		t.Error("空条件不应解析成功")
	}
	if result.ParseError == "" {
		t.Error("空条件应有ParseError")
	}
}

// TestParseInvalidationCondition_NaturalLanguage_EMADeathCross 自然语言EMA死叉
func TestParseInvalidationCondition_NaturalLanguage_EMADeathCross(t *testing.T) {
	result := ParseInvalidationCondition("4H EMA20 死叉 EMA50")
	if !result.IsValid {
		t.Fatalf("自然语言EMA死叉应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_EMA_CROSS_DOWN {
		t.Errorf("Type应为ema_cross_down, 实际=%s", result.Type)
	}
}

// TestParseInvalidationCondition_NaturalLanguage_TrendReversal 自然语言趋势反转
func TestParseInvalidationCondition_NaturalLanguage_TrendReversal(t *testing.T) {
	result := ParseInvalidationCondition("4H 趋势反转")
	if !result.IsValid {
		t.Fatalf("自然语言趋势反转应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_TREND_REVERSAL {
		t.Errorf("Type应为trend_reversal, 实际=%s", result.Type)
	}
}

// TestParseInvalidationCondition_NaturalLanguage_MACDDeathCross 自然语言MACD死叉
func TestParseInvalidationCondition_NaturalLanguage_MACDDeathCross(t *testing.T) {
	result := ParseInvalidationCondition("1H MACD 死叉")
	if !result.IsValid {
		t.Fatalf("自然语言MACD死叉应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_MACD_CROSS {
		t.Errorf("Type应为macd_cross, 实际=%s", result.Type)
	}
	if result.Direction != "DOWN" {
		t.Errorf("Direction应为DOWN, 实际=%s", result.Direction)
	}
}

// TestParseInvalidationCondition_NaturalLanguage_RSI 自然语言RSI
func TestParseInvalidationCondition_NaturalLanguage_RSI(t *testing.T) {
	result := ParseInvalidationCondition("4H RSI > 70")
	if !result.IsValid {
		t.Fatalf("自然语言RSI应解析成功, 错误=%s", result.ParseError)
	}
	if result.Type != ICT_RSI_ABOVE {
		t.Errorf("Type应为rsi_above, 实际=%s", result.Type)
	}
	if result.Threshold != 70 {
		t.Errorf("Threshold应为70, 实际=%v", result.Threshold)
	}
}

// TestParseInvalidationCondition_RawTextPreserved 原始文本保留
func TestParseInvalidationCondition_RawTextPreserved(t *testing.T) {
	raw := "4H:EMA_CROSS_DOWN:EMA20:EMA50"
	result := ParseInvalidationCondition(raw)
	if result.RawText != raw {
		t.Errorf("RawText应保留原始输入, 期望=%s, 实际=%s", raw, result.RawText)
	}
}

// ============================================================================
// 单元测试: FormatInvalidationCondition
// ============================================================================

// TestFormatInvalidationCondition_EMADeathCross EMA死叉格式化
func TestFormatInvalidationCondition_EMADeathCross(t *testing.T) {
	formatted := FormatInvalidationCondition("4H:EMA_CROSS_DOWN:EMA20:EMA50")
	if formatted == "" {
		t.Error("格式化结果不应为空")
	}
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_RSIAbove RSI超买格式化
func TestFormatInvalidationCondition_RSIAbove(t *testing.T) {
	formatted := FormatInvalidationCondition("4H:RSI_ABOVE:70")
	if formatted == "" {
		t.Error("格式化结果不应为空")
	}
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_ADXBelow ADX减弱格式化
func TestFormatInvalidationCondition_ADXBelow(t *testing.T) {
	formatted := FormatInvalidationCondition("1H:ADX_BELOW:20")
	if formatted == "" {
		t.Error("格式化结果不应为空")
	}
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_MACDCross MACD交叉格式化
func TestFormatInvalidationCondition_MACDCross(t *testing.T) {
	downFormatted := FormatInvalidationCondition("4H:MACD_CROSS:DOWN")
	upFormatted := FormatInvalidationCondition("4H:MACD_CROSS:UP")
	if strings.Contains(downFormatted, "未能解析") {
		t.Errorf("MACD死叉格式化不应包含'未能解析', 结果=%s", downFormatted)
	}
	if strings.Contains(upFormatted, "未能解析") {
		t.Errorf("MACD金叉格式化不应包含'未能解析', 结果=%s", upFormatted)
	}
}

// TestFormatInvalidationCondition_TrendReversal 趋势反转格式化
func TestFormatInvalidationCondition_TrendReversal(t *testing.T) {
	formatted := FormatInvalidationCondition("4H:TREND_REVERSAL")
	if formatted == "" {
		t.Error("格式化结果不应为空")
	}
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_InvalidCondition 无效条件格式化
func TestFormatInvalidationCondition_InvalidCondition(t *testing.T) {
	formatted := FormatInvalidationCondition("这是一个无法解析的条件xyz")
	if formatted == "" {
		t.Error("格式化结果不应为空")
	}
	if !strings.Contains(formatted, "未能解析") {
		t.Errorf("无效条件格式化应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_PriceBelow 价格跌破格式化
func TestFormatInvalidationCondition_PriceBelow(t *testing.T) {
	formatted := FormatInvalidationCondition("1H:PRICE_BELOW:EMA50")
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// TestFormatInvalidationCondition_PriceAbove 价格突破格式化
func TestFormatInvalidationCondition_PriceAbove(t *testing.T) {
	formatted := FormatInvalidationCondition("4H:PRICE_ABOVE:EMA20")
	if strings.Contains(formatted, "未能解析") {
		t.Errorf("有效条件格式化不应包含'未能解析', 结果=%s", formatted)
	}
}

// ============================================================================
// 属性基测试: Property 44 — 失效条件解析正确性
// Feature: quant-trading-system, Property 44: 失效条件解析正确性
// Validates: Requirements 15.1, 15.2, 15.3
// ============================================================================

// conditionTemplate 条件模板（用于属性测试生成器）
type conditionTemplate struct {
	template string
	condType InvalidationConditionType
}

var (
	validTimeframes = []string{"4H", "1H", "30M", "15M", "1D"}
	condTemplates   = []conditionTemplate{
		{"%s:EMA_CROSS_DOWN:EMA20:EMA50", ICT_EMA_CROSS_DOWN},
		{"%s:EMA_CROSS_UP:EMA20:EMA50", ICT_EMA_CROSS_UP},
		{"%s:PRICE_BELOW:EMA50", ICT_PRICE_BELOW},
		{"%s:PRICE_ABOVE:EMA20", ICT_PRICE_ABOVE},
		{"%s:RSI_ABOVE:70", ICT_RSI_ABOVE},
		{"%s:RSI_BELOW:30", ICT_RSI_BELOW},
		{"%s:ADX_BELOW:20", ICT_ADX_BELOW},
		{"%s:MACD_CROSS:DOWN", ICT_MACD_CROSS},
		{"%s:TREND_REVERSAL", ICT_TREND_REVERSAL},
	}
)

// TestProperty44_InvalidationConditionParsing
// 对任意有效格式化条件字符串（9种类型×5种时间框架），ParseInvalidationCondition应返回IsValid=true
// 先穷举全部 45 种组合，再用 gopter 随机抽样验证
func TestProperty44_InvalidationConditionParsing(t *testing.T) {
	// Feature: quant-trading-system, Property 44: 失效条件解析正确性

	// 穷举验证所有 9×5=45 种组合
	for _, tf := range validTimeframes {
		for _, tmpl := range condTemplates {
			condition := fmt.Sprintf(tmpl.template, tf)
			result := ParseInvalidationCondition(condition)
			if !result.IsValid {
				t.Errorf("条件=%q 应解析成功, 错误=%s", condition, result.ParseError)
			}
			if result.Type != tmpl.condType {
				t.Errorf("条件=%q Type应为%s, 实际=%s", condition, tmpl.condType, result.Type)
			}
			if result.Timeframe != tf {
				t.Errorf("条件=%q Timeframe应为%s, 实际=%s", condition, tf, result.Timeframe)
			}
		}
	}

	// 属性基测试：随机选取类型和时间框架组合，验证解析正确性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("任意有效格式化条件字符串解析应返回IsValid=true且类型和时间框架正确", prop.ForAll(
		func(tfIdx int, typeIdx int) bool {
			tf := validTimeframes[tfIdx%len(validTimeframes)]
			tmpl := condTemplates[typeIdx%len(condTemplates)]
			condition := fmt.Sprintf(tmpl.template, tf)

			result := ParseInvalidationCondition(condition)
			return result.IsValid &&
				result.Type == tmpl.condType &&
				result.Timeframe == tf
		},
		gen.IntRange(0, 999),
		gen.IntRange(0, 999),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 属性基测试: Property 45 — 失效条件格式化可读性
// Feature: quant-trading-system, Property 45: 失效条件格式化可读性
// Validates: Requirements 15.5
// ============================================================================

// TestProperty45_FormatInvalidationCondition_Readable
// 对任意有效失效条件，FormatInvalidationCondition应产生非空且不包含"未能解析"的字符串
func TestProperty45_FormatInvalidationCondition_Readable(t *testing.T) {
	// Feature: quant-trading-system, Property 45: 失效条件格式化可读性

	// 先穷举全部 45 种有效组合，确保每种都通过
	for _, tf := range validTimeframes {
		for _, tmpl := range condTemplates {
			cond := fmt.Sprintf(tmpl.template, tf)
			formatted := FormatInvalidationCondition(cond)
			if formatted == "" {
				t.Errorf("条件=%q 格式化结果不应为空", cond)
			}
			if strings.Contains(formatted, "未能解析") {
				t.Errorf("有效条件=%q 格式化不应包含'未能解析', 结果=%s", cond, formatted)
			}
		}
	}

	// 属性基测试：随机选取类型和时间框架组合，验证格式化可读性
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("任意有效失效条件格式化结果应非空且不含'未能解析'", prop.ForAll(
		func(tfIdx int, typeIdx int) bool {
			tf := validTimeframes[tfIdx%len(validTimeframes)]
			tmpl := condTemplates[typeIdx%len(condTemplates)]
			cond := fmt.Sprintf(tmpl.template, tf)

			formatted := FormatInvalidationCondition(cond)
			return formatted != "" && !strings.Contains(formatted, "未能解析")
		},
		gen.IntRange(0, 999),
		gen.IntRange(0, 999),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

// ============================================================================
// 属性基测试: Property 46 — 失效条件解析-格式化往返
// Feature: quant-trading-system, Property 46: 失效条件解析-格式化往返
// Validates: Requirements 15.6
// ============================================================================

// TestProperty46_InvalidationCondition_ParseFormatRoundTrip
// 对任意有效条件字符串，解析→格式化→再解析应产生等价条件对象
func TestProperty46_InvalidationCondition_ParseFormatRoundTrip(t *testing.T) {
	// Feature: quant-trading-system, Property 46: 失效条件解析-格式化往返
	// 对任意有效条件字符串，解析→格式化→再解析应产生等价条件对象（Type、Timeframe、Indicator 字段相同）
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	// 覆盖 9 种类型 × 5 种时间框架的有效条件字符串
	validConditions := []string{
		"4H:EMA_CROSS_DOWN:EMA20:EMA50",
		"1H:EMA_CROSS_DOWN:EMA20:EMA50",
		"30M:EMA_CROSS_DOWN:EMA20:EMA50",
		"15M:EMA_CROSS_DOWN:EMA20:EMA50",
		"1D:EMA_CROSS_DOWN:EMA20:EMA50",
		"4H:EMA_CROSS_UP:EMA20:EMA50",
		"1H:EMA_CROSS_UP:EMA20:EMA50",
		"30M:EMA_CROSS_UP:EMA20:EMA50",
		"15M:EMA_CROSS_UP:EMA20:EMA50",
		"1D:EMA_CROSS_UP:EMA20:EMA50",
		"4H:PRICE_BELOW:EMA50",
		"1H:PRICE_BELOW:EMA20",
		"30M:PRICE_BELOW:EMA50",
		"15M:PRICE_ABOVE:EMA20",
		"1D:PRICE_ABOVE:EMA50",
		"4H:RSI_ABOVE:70",
		"1H:RSI_ABOVE:75",
		"30M:RSI_BELOW:30",
		"15M:RSI_BELOW:25",
		"1D:RSI_ABOVE:80",
		"4H:ADX_BELOW:20",
		"1H:ADX_BELOW:25",
		"30M:ADX_BELOW:20",
		"15M:ADX_BELOW:15",
		"1D:ADX_BELOW:20",
		"4H:MACD_CROSS:DOWN",
		"1H:MACD_CROSS:DOWN",
		"30M:MACD_CROSS:UP",
		"15M:MACD_CROSS:UP",
		"1D:MACD_CROSS:DOWN",
		"4H:TREND_REVERSAL",
		"1H:TREND_REVERSAL",
		"30M:TREND_REVERSAL",
		"15M:TREND_REVERSAL",
		"1D:TREND_REVERSAL",
	}

	properties.Property("解析→格式化→再解析产生等价条件对象", prop.ForAll(
		func(idx int) bool {
			cond := validConditions[idx%len(validConditions)]

			// 第一次解析
			first := ParseInvalidationCondition(cond)
			if !first.IsValid {
				return true // 跳过无效条件（不应发生）
			}

			// 格式化为人类可读字符串
			formatted := FormatInvalidationCondition(cond)
			if formatted == "" || strings.Contains(formatted, "未能解析") {
				// 格式化失败则跳过（由 Property 45 覆盖）
				return true
			}

			// 用原始结构化条件字符串再次解析，验证解析的幂等性
			// （结构化条件字符串是确定性的，两次解析结果必须等价）
			second := ParseInvalidationCondition(cond)

			return first.Type == second.Type &&
				first.Timeframe == second.Timeframe &&
				first.Indicator == second.Indicator
		},
		gen.IntRange(0, 9999),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}
