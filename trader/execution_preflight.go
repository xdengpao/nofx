package trader

import "fmt"

const minPreflightOrderValueUSDT = 10.0

// ExecutionPreflightInput 是下单前确定性检查输入。
type ExecutionPreflightInput struct {
	Symbol        string
	Side          string
	Quantity      float64
	Price         float64
	Leverage      int
	MinOrderValue float64
	Positions     []map[string]interface{}
	Intent        string // open 或 add
}

// ExecutionPreflightResult 是下单前检查结果。
type ExecutionPreflightResult struct {
	Allowed       bool     `json:"allowed"`
	NotionalValue float64  `json:"notional_value"`
	Reasons       []string `json:"reasons,omitempty"`
}

// EvaluateExecutionPreflight 检查数量、名义额、价格、杠杆和重复持仓。
func EvaluateExecutionPreflight(input ExecutionPreflightInput) ExecutionPreflightResult {
	result := ExecutionPreflightResult{Allowed: true}
	minOrderValue := input.MinOrderValue
	if minOrderValue <= 0 {
		minOrderValue = minPreflightOrderValueUSDT
	}

	if input.Symbol == "" {
		result.reject("symbol不能为空")
	}
	if input.Side != "long" && input.Side != "short" {
		result.reject("side必须是long或short")
	}
	if input.Quantity <= 0 {
		result.reject("数量必须大于0")
	}
	if input.Price <= 0 {
		result.reject("价格必须大于0")
	}
	if input.Leverage <= 0 {
		result.reject("杠杆必须大于0")
	}

	result.NotionalValue = input.Quantity * input.Price
	if result.NotionalValue > 0 && result.NotionalValue < minOrderValue {
		result.reject(fmt.Sprintf("订单名义额 %.4f USDT 低于最小值 %.2f USDT", result.NotionalValue, minOrderValue))
	}

	hasSameSidePosition := false
	for _, pos := range input.Positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if symbol != input.Symbol || !positionAmountNonZero(pos) {
			continue
		}
		if side == input.Side {
			if input.Intent == "add" {
				hasSameSidePosition = true
				continue
			}
			result.reject(fmt.Sprintf("%s 已有%s仓位", input.Symbol, input.Side))
			break
		}
		result.reject(fmt.Sprintf("%s 已有反向%s仓位", input.Symbol, side))
		break
	}
	if input.Intent == "add" && !hasSameSidePosition {
		result.reject(fmt.Sprintf("%s 没有%s仓位，不能加仓", input.Symbol, input.Side))
	}

	return result
}

func positionAmountNonZero(pos map[string]interface{}) bool {
	if amount, ok := pos["positionAmt"].(float64); ok {
		return amount != 0
	}
	return true
}

func (result *ExecutionPreflightResult) reject(reason string) {
	result.Allowed = false
	result.Reasons = append(result.Reasons, reason)
}
