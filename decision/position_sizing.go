package decision

import "math"

// PositionSizingInput 描述以账户风险为中心的仓位 sizing 输入。
type PositionSizingInput struct {
	AccountEquity            float64
	AvailableBalance         float64
	CurrentPrice             float64
	StopLoss                 float64
	Leverage                 int
	EffectiveRiskPct         float64
	RemainingRiskBudgetPct   float64
	FeeSlippagePct           float64
	MinOrderValueUSDT        float64
	RequestedPositionSizeUSD float64
	PartialClosePct          float64
}

// PositionSizingResult 是统一仓位 sizing 的结果。
type PositionSizingResult struct {
	Executable         bool     `json:"executable"`
	PositionSizeUSD    float64  `json:"position_size_usd"`
	MaxPositionSizeUSD float64  `json:"max_position_size_usd"`
	RiskUSD            float64  `json:"risk_usd"`
	RiskPct            float64  `json:"risk_pct"`
	MarginRequiredUSD  float64  `json:"margin_required_usd"`
	StopDistancePct    float64  `json:"stop_distance_pct"`
	CanPartialExit     bool     `json:"can_partial_exit"`
	Reasons            []string `json:"reasons,omitempty"`
}

const (
	defaultFeeSlippagePct    = 0.002
	defaultMinOrderValueUSDT = 10.0
	defaultPartialClosePct   = 20.0
)

// CalculatePositionSizing 将 AI 给出的仓位与账户风险、手续费滑点、保证金和最小名义额统一校验。
func CalculatePositionSizing(input PositionSizingInput) PositionSizingResult {
	result := PositionSizingResult{}

	if input.AccountEquity <= 0 {
		result.Reasons = append(result.Reasons, "账户净值必须大于0")
		return result
	}
	if input.AvailableBalance <= 0 {
		result.Reasons = append(result.Reasons, "可用余额必须大于0")
		return result
	}
	if input.CurrentPrice <= 0 || input.StopLoss <= 0 {
		result.Reasons = append(result.Reasons, "当前价和止损价必须大于0")
		return result
	}
	if input.Leverage <= 0 {
		result.Reasons = append(result.Reasons, "杠杆必须大于0")
		return result
	}
	if input.EffectiveRiskPct <= 0 {
		input.EffectiveRiskPct = 0.02
	}
	if input.FeeSlippagePct <= 0 {
		input.FeeSlippagePct = defaultFeeSlippagePct
	}
	if input.MinOrderValueUSDT <= 0 {
		input.MinOrderValueUSDT = defaultMinOrderValueUSDT
	}
	if input.PartialClosePct <= 0 {
		input.PartialClosePct = defaultPartialClosePct
	}

	stopDistancePct := math.Abs(input.CurrentPrice-input.StopLoss) / input.CurrentPrice
	if stopDistancePct <= 0 {
		result.Reasons = append(result.Reasons, "止损距离必须大于0")
		return result
	}
	result.StopDistancePct = stopDistancePct

	effectiveRiskPct := input.EffectiveRiskPct
	if input.RemainingRiskBudgetPct > 0 && input.RemainingRiskBudgetPct < effectiveRiskPct {
		effectiveRiskPct = input.RemainingRiskBudgetPct
	}
	riskBudgetUSD := input.AccountEquity * effectiveRiskPct
	maxByRisk := riskBudgetUSD / (stopDistancePct + input.FeeSlippagePct)
	maxByMargin := input.AvailableBalance * float64(input.Leverage) * 0.9
	result.MaxPositionSizeUSD = math.Min(maxByRisk, maxByMargin)

	positionSize := result.MaxPositionSizeUSD
	if input.RequestedPositionSizeUSD > 0 {
		positionSize = math.Min(input.RequestedPositionSizeUSD, result.MaxPositionSizeUSD)
	}
	result.PositionSizeUSD = positionSize
	result.RiskUSD = positionSize * stopDistancePct
	result.RiskPct = result.RiskUSD / input.AccountEquity
	result.MarginRequiredUSD = positionSize / float64(input.Leverage)

	if positionSize < input.MinOrderValueUSDT {
		result.Reasons = append(result.Reasons, "仓位名义额低于最小下单额")
		return result
	}
	if result.MarginRequiredUSD > input.AvailableBalance {
		result.Reasons = append(result.Reasons, "可用保证金不足")
		return result
	}

	partialCloseValue := positionSize * input.PartialClosePct / 100
	remainingValue := positionSize - partialCloseValue
	result.CanPartialExit = partialCloseValue >= input.MinOrderValueUSDT && remainingValue >= input.MinOrderValueUSDT
	if !result.CanPartialExit {
		result.Reasons = append(result.Reasons, "仓位过小，无法可靠分批退出")
	}

	result.Executable = len(result.Reasons) == 0
	return result
}
