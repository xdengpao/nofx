package optimize

import "strings"

var DefaultProposalRequirements = []string{
	"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10", "R11", "R12", "R13", "R14", "R15",
}

func BuildRequirementCoverage(requirements []string, evidence map[string][]EvidenceRef) RequirementCoverageSummary {
	summary := RequirementCoverageSummary{}
	for _, requirement := range requirements {
		refs := evidence[requirement]
		status := "missing"
		severity := "high"
		if len(refs) > 0 {
			status = "satisfied"
			severity = ""
		}
		summary.Items = append(summary.Items, RequirementCoverageItem{
			Requirement: requirement,
			Status:      status,
			Severity:    severity,
			Evidence:    refs,
		})
	}
	return summary
}

func CoverageHasHighMissing(summary RequirementCoverageSummary) bool {
	for _, item := range summary.Items {
		if item.Status == "missing" && item.Severity == "high" {
			return true
		}
	}
	return false
}

func NewProposalChecklist(proposalID string, defects []DefectEntry, baseline, candidate *RunMetrics, gate GateResult, rollout GradualRolloutConfig) OptimizationProposal {
	proposal := OptimizationProposal{
		ProposalID:     proposalID,
		BaselineRunID:  runIDOrEmpty(baseline),
		CandidateRunID: runIDOrEmpty(candidate),
		DataHash:       firstNonEmpty(dataHashOrEmpty(candidate), dataHashOrEmpty(baseline)),
		GateResult:     gate.Verdict,
		Rollout:        rollout,
		RollbackSwitches: []string{
			"programmatic_strategy.enabled",
			"programmatic_strategy.entry_timing.enabled",
			"strategy_risk.enabled",
		},
	}
	for _, defect := range defects {
		if defect.DefectCode != "" && !containsString(proposal.DefectCodes, defect.DefectCode) {
			proposal.DefectCodes = append(proposal.DefectCodes, defect.DefectCode)
		}
		proposal.EvidenceRefs = append(proposal.EvidenceRefs, defect.EvidenceRefs...)
	}
	return proposal
}

func ValidateProposalChecklist(proposal OptimizationProposal, coverage RequirementCoverageSummary) []string {
	var reasons []string
	if strings.TrimSpace(proposal.ProposalID) == "" {
		reasons = append(reasons, "proposal_id缺失")
	}
	if strings.TrimSpace(proposal.BaselineRunID) == "" || strings.TrimSpace(proposal.CandidateRunID) == "" {
		reasons = append(reasons, "baseline_run_id或candidate_run_id缺失")
	}
	if strings.TrimSpace(proposal.DataHash) == "" {
		reasons = append(reasons, "data_hash缺失")
	}
	if CoverageHasHighMissing(coverage) {
		reasons = append(reasons, "存在High severity requirement coverage missing")
	}
	return reasons
}

func runIDOrEmpty(metrics *RunMetrics) string {
	if metrics == nil {
		return ""
	}
	return metrics.RunID
}

func dataHashOrEmpty(metrics *RunMetrics) string {
	if metrics == nil {
		return ""
	}
	return metrics.DataHash
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
