package optimize

import (
	"fmt"
	"math"
	"strings"
)

func EvaluateGate(input GateInput) (GateResult, error) {
	cfg := input.Config
	if cfg == nil {
		cfg = DefaultOptimizationConfig()
	}
	ApplyOptimizationDefaults(cfg)
	result := GateResult{
		Verdict:                 "approved",
		ByTraderExchangeVerdict: map[string]string{},
	}
	if input.Policy != nil {
		result.PolicyRef = input.Policy.PolicyRef
		result.PolicyCommit = input.Policy.PolicyCommit
	}
	if cfg.PolicyRef != "" {
		result.PolicyRef = cfg.PolicyRef
		result.PolicyCommit = cfg.PolicyCommit
		result.OverrideReason = cfg.OverrideReason
		result.OverrideBy = cfg.OverrideBy
	}
	if input.Baseline == nil || input.Candidate == nil {
		result.Verdict = "invalid_input"
		result.Reasons = append(result.Reasons, "baseline或candidate为空")
		return result, ErrIncomparableRuns
	}
	if err := validatePolicy(cfg, input.Policy); err != nil {
		result.Verdict = "invalid_input"
		result.Reasons = append(result.Reasons, err.Error())
		return result, err
	}
	if reasons := incomparableReasons(input.Baseline, input.Candidate); len(reasons) > 0 {
		result.Verdict = "invalid_input"
		result.Reasons = append(result.Reasons, reasons...)
		return result, ErrIncomparableRuns
	}
	if coverageMissing(input.RequirementCoverage) {
		result.Verdict = "invalid_input"
		result.Reasons = append(result.Reasons, "存在High severity requirement coverage missing")
		return result, nil
	}
	result.Comparisons = append(result.Comparisons,
		attachCandidateCI(relativeComparison("max_drawdown_pct", input.Baseline.MaxDrawdownPct, input.Candidate.MaxDrawdownPct, cfg.MaxDrawdownRelativeTolerance, true), input.Candidate),
		attachCandidateCI(relativeComparison("profit_factor", input.Baseline.ProfitFactor, input.Candidate.ProfitFactor, cfg.ProfitFactorRelativeFloor, false), input.Candidate),
		attachCandidateCI(absoluteDeltaComparison("min_notional_rejection_rate", input.Baseline.MinNotionalRejectionRate, input.Candidate.MinNotionalRejectionRate, cfg.MinNotionalRejectionTolerance), input.Candidate),
		attachCandidateCI(absoluteDeltaComparison("rejection_rate", input.Baseline.RejectionRate, input.Candidate.RejectionRate, cfg.RejectionRateAbsoluteTolerance), input.Candidate),
		attachCandidateCI(absoluteDeltaComparison("circuit_breaker_frequency_per_day", input.Baseline.CircuitBreakerFrequencyPerDay, input.Candidate.CircuitBreakerFrequencyPerDay, cfg.CircuitBreakerFrequencyTolerance), input.Candidate),
	)
	for _, cmp := range result.Comparisons {
		switch cmp.Metric {
		case "max_drawdown_pct", "profit_factor", "min_notional_rejection_rate":
			if !cmp.Passed {
				result.Verdict = "rejected"
				result.Reasons = append(result.Reasons, fmt.Sprintf("%s未通过门控", cmp.Metric))
			}
		case "rejection_rate", "circuit_breaker_frequency_per_day":
			if !cmp.Passed && result.Verdict != "rejected" {
				result.Verdict = "manual_review"
				result.Reasons = append(result.Reasons, fmt.Sprintf("%s需要人工复核", cmp.Metric))
			}
		}
	}
	for key, metrics := range input.ByTraderExchange {
		if metrics == nil {
			continue
		}
		verdict := "approved"
		if metrics.MinNotionalRejectionRate-input.Baseline.MinNotionalRejectionRate > cfg.MinNotionalRejectionTolerance {
			verdict = "rejected"
			result.Verdict = "rejected"
		}
		result.ByTraderExchangeVerdict[key] = verdict
	}
	if len(result.Reasons) == 0 {
		result.Reasons = append(result.Reasons, "所有核心门控通过")
	}
	result.Passed = result.Verdict == "approved"
	return result, nil
}

func attachCandidateCI(cmp MetricComparison, candidate *RunMetrics) MetricComparison {
	if candidate == nil {
		return cmp
	}
	key := cmp.Metric
	if cmp.Metric == "max_drawdown_pct" {
		key = "max_drawdown_pct"
	}
	if ci, ok := candidate.BootstrapCIs[key]; ok {
		cmp.ConfidenceInterval = ci.Interval
		cmp.CIStatus = ci.Status
		return cmp
	}
	if candidate.CIStatus != "" {
		cmp.CIStatus = candidate.CIStatus
	}
	return cmp
}

func validatePolicy(cfg *OptimizationConfig, policy *CommittedGatePolicy) error {
	if hasThresholdOverride(cfg) {
		if strings.TrimSpace(cfg.PolicyRef) == "" || strings.TrimSpace(cfg.PolicyCommit) == "" {
			return ErrUncommittedPolicyOverride
		}
		if policy == nil || policy.PolicyRef != cfg.PolicyRef || policy.PolicyCommit != cfg.PolicyCommit {
			return ErrUncommittedPolicyOverride
		}
	}
	return nil
}

func incomparableReasons(base, candidate *RunMetrics) []string {
	var reasons []string
	check := func(name, a, b string) {
		if strings.TrimSpace(a) != "" && strings.TrimSpace(b) != "" && a != b {
			reasons = append(reasons, fmt.Sprintf("%s不一致: baseline=%s candidate=%s", name, a, b))
		}
	}
	check("data_hash", base.DataHash, candidate.DataHash)
	check("timezone", base.Timezone, candidate.Timezone)
	check("symbol_set_hash", base.SymbolSetHash, candidate.SymbolSetHash)
	check("fee_model_hash", base.FeeModelHash, candidate.FeeModelHash)
	check("slippage_model_hash", base.SlippageModelHash, candidate.SlippageModelHash)
	check("funding_mode", base.FundingMode, candidate.FundingMode)
	check("liquidation_mode", base.LiquidationMode, candidate.LiquidationMode)
	check("execution_model_hash", base.ExecutionModelHash, candidate.ExecutionModelHash)
	if base.InitialEquity > 0 && candidate.InitialEquity > 0 && math.Abs(base.InitialEquity-candidate.InitialEquity) > 1e-9 {
		reasons = append(reasons, "initial_equity不一致")
	}
	return reasons
}

func coverageMissing(summary *RequirementCoverageSummary) bool {
	if summary == nil {
		return false
	}
	for _, item := range summary.Items {
		if item.Status == "missing" && item.Severity == "high" {
			return true
		}
	}
	return false
}

func relativeComparison(metric string, baseline, candidate, threshold float64, upperBound bool) MetricComparison {
	relative := 0.0
	if baseline != 0 {
		relative = candidate / baseline
	} else if candidate > 0 {
		relative = math.Inf(1)
	}
	passed := relative <= threshold
	if !upperBound {
		passed = relative >= threshold
	}
	return MetricComparison{
		Metric:         metric,
		BaselineValue:  baseline,
		CandidateValue: candidate,
		AbsoluteDelta:  candidate - baseline,
		RelativeDelta:  relative,
		Threshold:      threshold,
		CIStatus:       "disabled",
		Passed:         passed,
	}
}

func absoluteDeltaComparison(metric string, baseline, candidate, threshold float64) MetricComparison {
	delta := candidate - baseline
	return MetricComparison{
		Metric:         metric,
		BaselineValue:  baseline,
		CandidateValue: candidate,
		AbsoluteDelta:  delta,
		RelativeDelta:  delta,
		Threshold:      threshold,
		CIStatus:       "disabled",
		Passed:         delta <= threshold,
	}
}
