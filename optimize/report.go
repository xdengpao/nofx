package optimize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type OptimizationReport struct {
	GeneratedAt         time.Time                   `json:"generated_at"`
	DefectCatalog       *DefectCatalog              `json:"defect_catalog,omitempty"`
	BaselineMetrics     *RunMetrics                 `json:"baseline_metrics,omitempty"`
	CandidateMetrics    *RunMetrics                 `json:"candidate_metrics,omitempty"`
	GateResult          *GateResult                 `json:"gate_result,omitempty"`
	SignalQualityReport *SignalQualityReport        `json:"signal_quality_report,omitempty"`
	EquityCurveAnalysis *EquityCurveAnalysis        `json:"equity_curve_analysis,omitempty"`
	ConsistencyResult   *ConsistencyResult          `json:"consistency_result,omitempty"`
	ProposalChecklist   *OptimizationProposal       `json:"proposal_checklist,omitempty"`
	RequirementCoverage *RequirementCoverageSummary `json:"requirement_coverage,omitempty"`
}

func WriteOptimizationReport(outputDir string, report OptimizationReport) error {
	if outputDir == "" {
		return fmt.Errorf("输出目录为空")
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "optimization_report.json"), data, 0644)
}
