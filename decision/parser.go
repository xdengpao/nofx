package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

// ============================================================================
// AI响应解析器
// ============================================================================

// AIResponseParser AI响应解析器
type AIResponseParser struct {
	strictMode bool
	maxRetries int
	lastError  error
}

// ParsedAIResponse 解析后的AI响应
type ParsedAIResponse struct {
	Decisions   []Decision
	Analysis    string
	Confidence  float64
	ParseMethod string
	Warnings    []string
}

// JSONExtractor JSON提取器
type JSONExtractor struct {
	arrayPattern  *regexp.Regexp
	objectPattern *regexp.Regexp
}

var jsonExtractor *JSONExtractor

// 初始化
func init() {
	jsonExtractor = &JSONExtractor{
		arrayPattern:  regexp.MustCompile(`(?s)\[[\s\S]*?\]`),
		objectPattern: regexp.MustCompile(`(?s)\{[^{}]*\}`),
	}
}

// NewAIResponseParser 创建解析器
func NewAIResponseParser() *AIResponseParser {
	return &AIResponseParser{
		strictMode: false,
		maxRetries: 3,
	}
}

// ParseAIResponse 解析AI响应
func (p *AIResponseParser) ParseAIResponse(response string) (*ParsedAIResponse, error) {
	result := &ParsedAIResponse{
		Decisions: []Decision{},
		Warnings:  []string{},
	}

	if strings.TrimSpace(response) == "" {
		return result, fmt.Errorf("空响应")
	}

	// 方法1: 标准JSON解析
	decisions, analysis, err := p.parseStandard(response)
	if err == nil && len(decisions) > 0 {
		result.Decisions = decisions
		result.Analysis = analysis
		result.ParseMethod = "standard"
		return result, nil
	}
	result.Warnings = append(result.Warnings, fmt.Sprintf("标准解析失败: %v", err))

	// 方法2: 模糊JSON解析
	decisions, analysis, err = p.parseFuzzy(response)
	if err == nil && len(decisions) > 0 {
		result.Decisions = decisions
		result.Analysis = analysis
		result.ParseMethod = "fuzzy"
		return result, nil
	}
	result.Warnings = append(result.Warnings, fmt.Sprintf("模糊解析失败: %v", err))

	// 方法3: 从文本中提取
	decisions, analysis = p.parseFromText(response)
	if len(decisions) > 0 {
		result.Decisions = decisions
		result.Analysis = analysis
		result.ParseMethod = "text_extraction"
		result.Warnings = append(result.Warnings, "使用文本提取模式")
		return result, nil
	}

	// 方法4: 返回默认wait决策
	result.Decisions = []Decision{{
		Symbol:    "ALL",
		Action:    "wait",
		Reasoning: "AI响应解析失败，默认等待",
	}}
	result.Analysis = response
	result.ParseMethod = "fallback"
	result.Warnings = append(result.Warnings, "所有解析方法失败")

	return result, nil
}

func (p *AIResponseParser) parseStandard(response string) ([]Decision, string, error) {
	jsonStr, err := jsonExtractor.ExtractJSONArray(response)
	if err != nil {
		return nil, "", err
	}

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonStr), &decisions); err != nil {
		return nil, "", fmt.Errorf("JSON解析失败: %w", err)
	}

	analysis := extractAnalysisPart(response)
	return decisions, analysis, nil
}

func (p *AIResponseParser) parseFuzzy(response string) ([]Decision, string, error) {
	// 清理响应
	cleaned := cleanResponseText(response)

	jsonStr, err := jsonExtractor.ExtractJSONArray(cleaned)
	if err != nil {
		return nil, "", err
	}

	// 修复JSON
	fixed := fixJSON(jsonStr)

	var decisions []Decision
	if err := json.Unmarshal([]byte(fixed), &decisions); err != nil {
		// 尝试逐个解析
		decisions, err = parseDecisionsOneByOne(fixed)
		if err != nil {
			return nil, "", err
		}
	}

	analysis := extractAnalysisPart(response)
	return decisions, analysis, nil
}

func (p *AIResponseParser) parseFromText(response string) ([]Decision, string) {
	var decisions []Decision

	patterns := map[string]*regexp.Regexp{
		"open_long":  regexp.MustCompile(`(?i)(买入|做多|open_long|long|buy)\s*([A-Z]+USDT?)`),
		"open_short": regexp.MustCompile(`(?i)(卖出|做空|open_short|short|sell)\s*([A-Z]+USDT?)`),
		"wait":       regexp.MustCompile(`(?i)(等待|观望|wait|no\s*trade|无机会)`),
	}

	// 检测wait
	if patterns["wait"].MatchString(response) {
		return []Decision{{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: "从文本检测到等待信号",
		}}, response
	}

	// 检测开多
	if matches := patterns["open_long"].FindAllStringSubmatch(response, -1); len(matches) > 0 {
		for _, match := range matches {
			if len(match) >= 3 {
				symbol := normalizeSymbol(match[2])
				decisions = append(decisions, Decision{
					Symbol:    symbol,
					Action:    "open_long",
					Reasoning: fmt.Sprintf("从文本提取: %s", match[0]),
				})
			}
		}
	}

	// 检测开空
	if matches := patterns["open_short"].FindAllStringSubmatch(response, -1); len(matches) > 0 {
		for _, match := range matches {
			if len(match) >= 3 {
				symbol := normalizeSymbol(match[2])
				decisions = append(decisions, Decision{
					Symbol:    symbol,
					Action:    "open_short",
					Reasoning: fmt.Sprintf("从文本提取: %s", match[0]),
				})
			}
		}
	}

	// 提取数值参数
	for i := range decisions {
		decisions[i] = p.extractNumericParams(response, decisions[i])
	}

	return decisions, response
}

func (p *AIResponseParser) extractNumericParams(text string, d Decision) Decision {
	// 提取止损
	slPattern := regexp.MustCompile(`(?i)(止损|stop\s*loss|sl)[:\s]*(\d+\.?\d*)`)
	if matches := slPattern.FindStringSubmatch(text); len(matches) > 2 {
		if val, err := strconv.ParseFloat(matches[2], 64); err == nil {
			d.StopLoss = val
		}
	}

	// 提取止盈
	tpPattern := regexp.MustCompile(`(?i)(止盈|take\s*profit|tp)[:\s]*(\d+\.?\d*)`)
	if matches := tpPattern.FindStringSubmatch(text); len(matches) > 2 {
		if val, err := strconv.ParseFloat(matches[2], 64); err == nil {
			d.TakeProfit = val
		}
	}

	// 提取仓位
	sizePattern := regexp.MustCompile(`(?i)(仓位|position|size)[:\s]*(\d+\.?\d*)\s*(usd|u)?`)
	if matches := sizePattern.FindStringSubmatch(text); len(matches) > 2 {
		if val, err := strconv.ParseFloat(matches[2], 64); err == nil {
			d.PositionSizeUSD = val
		}
	}

	// 提取置信度
	confPattern := regexp.MustCompile(`(?i)(置信度|confidence)[:\s]*(\d+)`)
	if matches := confPattern.FindStringSubmatch(text); len(matches) > 2 {
		if val, err := strconv.Atoi(matches[2]); err == nil {
			d.Confidence = val
		}
	}

	// 提取杠杆
	levPattern := regexp.MustCompile(`(?i)(杠杆|leverage)[:\s]*(\d+)`)
	if matches := levPattern.FindStringSubmatch(text); len(matches) > 2 {
		if val, err := strconv.Atoi(matches[2]); err == nil {
			d.Leverage = val
		}
	}

	return d
}

// ============================================================================
// JSON处理辅助函数
// ============================================================================

func (e *JSONExtractor) ExtractJSONArray(text string) (string, error) {
	cleaned := e.cleanText(text)

	matches := e.findJSONArrays(cleaned)
	if len(matches) == 0 {
		return "", fmt.Errorf("未找到JSON数组")
	}

	for _, match := range matches {
		fixed := fixJSON(match)
		if json.Valid([]byte(fixed)) {
			return fixed, nil
		}
	}

	fixed := fixJSON(matches[0])
	return fixed, nil
}

func (e *JSONExtractor) cleanText(text string) string {
	result := text

	// 移除markdown代码块标记
	codeBlockStart := regexp.MustCompile("(?s)```json\\s*")
	codeBlockEnd := regexp.MustCompile("(?s)```\\s*")
	result = codeBlockStart.ReplaceAllString(result, "")
	result = codeBlockEnd.ReplaceAllString(result, "")

	// 替换中文引号
	result = strings.Map(func(r rune) rune {
		switch r {
		case '\u201c', '\u201d':
			return '"'
		case '\u2018', '\u2019':
			return '\''
		default:
			return r
		}
	}, result)

	return result
}

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

func fixJSON(jsonStr string) string {
	result := jsonStr

	// 移除尾部逗号
	trailingComma := regexp.MustCompile(`,(\s*[\]\}])`)
	result = trailingComma.ReplaceAllString(result, "$1")

	// 修复无引号的key
	unquotedKey := regexp.MustCompile(`([{\[,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)(\s*:)`)
	result = unquotedKey.ReplaceAllString(result, `$1"$2"$3`)

	// 替换特殊值
	result = regexp.MustCompile(`\bNaN\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\bInfinity\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\b-Infinity\b`).ReplaceAllString(result, "null")
	result = regexp.MustCompile(`\bundefined\b`).ReplaceAllString(result, "null")

	// 修复单引号字符串
	singleQuote := regexp.MustCompile(`'([^']*)'`)
	result = singleQuote.ReplaceAllString(result, `"$1"`)

	// 移除注释
	lineComment := regexp.MustCompile(`//[^\n]*`)
	result = lineComment.ReplaceAllString(result, "")

	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	result = blockComment.ReplaceAllString(result, "")

	return result
}

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

func cleanResponseText(response string) string {
	// 移除markdown代码块
	codeBlockPattern := regexp.MustCompile("(?s)```[a-z]*\\s*")
	result := codeBlockPattern.ReplaceAllString(response, "")
	result = strings.ReplaceAll(result, "```", "")

	// 替换中文引号
	result = strings.Map(func(r rune) rune {
		switch r {
		case '\u201c', '\u201d':
			return '"'
		case '\u2018', '\u2019':
			return '\''
		default:
			return r
		}
	}, result)

	return result
}

func extractAnalysisPart(response string) string {
	arrayStart := strings.Index(response, "[")
	if arrayStart <= 0 {
		return ""
	}

	analysisPart := strings.TrimSpace(response[:arrayStart])
	analysisPart = strings.TrimSuffix(analysisPart, "```json")
	analysisPart = strings.TrimSuffix(analysisPart, "```")
	analysisPart = strings.TrimSpace(analysisPart)

	return analysisPart
}

func normalizeSymbol(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if !strings.HasSuffix(symbol, "USDT") {
		symbol += "USDT"
	}
	return symbol
}

// ============================================================================
// 健壮的决策提取（公共函数）
// ============================================================================

// ExtractDecisionsRobust 健壮的决策提取
func ExtractDecisionsRobust(response string) ([]Decision, string, error) {
	parser := NewAIResponseParser()
	result, err := parser.ParseAIResponse(response)
	if err != nil {
		return nil, "", err
	}

	if len(result.Warnings) > 0 {
		for _, w := range result.Warnings {
			log.Printf("⚠️ 解析警告: %s", w)
		}
	}

	return result.Decisions, result.Analysis, nil
}

// ============================================================================
// 失效条件解析
// ============================================================================

var conditionParser *InvalidationConditionParser

// InitConditionParser 初始化条件解析器
func InitConditionParser() {
	conditionParser = &InvalidationConditionParser{
		patterns: make(map[InvalidationConditionType]*regexp.Regexp),
	}

	conditionParser.patterns[ICT_EMA_CROSS_DOWN] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?EMA_CROSS_DOWN:?(EMA\d+):?(EMA\d+)?`)
	conditionParser.patterns[ICT_EMA_CROSS_UP] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?EMA_CROSS_UP:?(EMA\d+):?(EMA\d+)?`)
	conditionParser.patterns[ICT_PRICE_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?PRICE_BELOW:?(EMA\d+|VWAP|BB_LOWER|\d+\.?\d*)`)
	conditionParser.patterns[ICT_PRICE_ABOVE] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?PRICE_ABOVE:?(EMA\d+|VWAP|BB_UPPER|\d+\.?\d*)`)
	conditionParser.patterns[ICT_RSI_ABOVE] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?RSI_ABOVE:?(\d+)`)
	conditionParser.patterns[ICT_RSI_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?RSI_BELOW:?(\d+)`)
	conditionParser.patterns[ICT_ADX_BELOW] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?ADX_BELOW:?(\d+)`)
	conditionParser.patterns[ICT_MACD_CROSS] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?MACD_CROSS:?(UP|DOWN)`)
	conditionParser.patterns[ICT_TREND_REVERSAL] = regexp.MustCompile(`(?i)(\d+[HhMmDd]):?TREND_REVERSAL`)
}

// ParseInvalidationCondition 解析失效条件
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

	// 尝试自然语言解析
	result = parseNaturalLanguageCondition(condition)
	return result
}

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

// FormatInvalidationCondition 格式化失效条件
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
