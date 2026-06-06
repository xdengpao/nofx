package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"nofx/backtest"
	"nofx/historydb"
	"nofx/optimize"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("用法: optimize <diagnose|baseline|compare|report> [flags]")
	}
	switch os.Args[1] {
	case "diagnose":
		runDiagnose(os.Args[2:])
	case "baseline":
		runBaseline(os.Args[2:])
	case "compare":
		runCompare(os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	default:
		fatalf("未知子命令: %s", os.Args[1])
	}
}

func runDiagnose(args []string) {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	backtestRun := fs.String("backtest-run", "", "backtest run目录")
	traderID := fs.String("trader", "backtest", "trader id")
	exchange := fs.String("exchange", "binance", "exchange")
	out := fs.String("output", "defect_catalog.json", "输出文件")
	_ = fs.Parse(args)
	artifacts, err := optimize.LoadRunArtifacts(*backtestRun)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("加载run失败: %v", err)
	}
	catalog, err := optimize.BuildDefectCatalog(nil, []optimize.BacktestRunInput{{
		Artifacts: artifacts,
		RunID:     artifacts.RunID,
		DataHash:  artifacts.Report.DataHash,
		TraderID:  *traderID,
		Exchange:  *exchange,
	}}, optimize.DefaultOptimizationConfig())
	if err != nil && err != optimize.ErrInsufficientEvidence {
		fatalf("诊断失败: %v", err)
	}
	writeJSON(*out, catalog)
}

func runBaseline(args []string) {
	fs := flag.NewFlagSet("baseline", flag.ExitOnError)
	configPath := fs.String("config", "", "backtest配置")
	traderID := fs.String("trader", "backtest", "trader id")
	exchange := fs.String("exchange", "binance", "exchange")
	_ = fs.Parse(args)
	cfg, err := backtest.LoadConfig(*configPath)
	if err != nil {
		fatalf("加载回测配置失败: %v", err)
	}
	store, err := historydb.Open(cfg.HistoryDB)
	if err != nil {
		fatalf("打开历史库失败: %v", err)
	}
	defer store.Close()
	artifacts, _, err := optimize.RunBaseline(context.Background(), cfg, store, *traderID, *exchange)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("baseline失败: %v", err)
	}
	fmt.Println(artifacts.OutputDir)
}

func runCompare(args []string) {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	baselineRun := fs.String("baseline-run", "", "baseline run目录")
	candidateRun := fs.String("candidate-run", "", "candidate run目录")
	configPath := fs.String("config", "", "optimization配置")
	out := fs.String("output", "gate_result.json", "输出文件")
	_ = fs.Parse(args)
	baseline, err := optimize.LoadRunArtifacts(*baselineRun)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("加载baseline失败: %v", err)
	}
	candidate, err := optimize.LoadRunArtifacts(*candidateRun)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("加载candidate失败: %v", err)
	}
	cfg, err := optimize.LoadOptimizationConfig(*configPath)
	if err != nil {
		fatalf("加载优化配置失败: %v", err)
	}
	result, err := optimize.EvaluateGate(optimize.GateInput{Baseline: baseline.Metrics, Candidate: candidate.Metrics, Config: cfg})
	if err != nil && result.Verdict == "" {
		fatalf("Gate失败: %v", err)
	}
	writeJSON(*out, result)
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	baselineRun := fs.String("baseline-run", "", "baseline run目录")
	candidateRun := fs.String("candidate-run", "", "candidate run目录")
	outputDir := fs.String("output-dir", "", "报告输出目录")
	_ = fs.Parse(args)
	baseline, err := optimize.LoadRunArtifacts(*baselineRun)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("加载baseline失败: %v", err)
	}
	candidate, err := optimize.LoadRunArtifacts(*candidateRun)
	if err != nil && err != optimize.ErrMissingStructureSnapshots {
		fatalf("加载candidate失败: %v", err)
	}
	gate, _ := optimize.EvaluateGate(optimize.GateInput{Baseline: baseline.Metrics, Candidate: candidate.Metrics, Config: optimize.DefaultOptimizationConfig()})
	report := optimize.OptimizationReport{
		BaselineMetrics:  baseline.Metrics,
		CandidateMetrics: candidate.Metrics,
		GateResult:       &gate,
	}
	if *outputDir == "" {
		*outputDir = candidate.OutputDir
	}
	if err := optimize.WriteOptimizationReport(*outputDir, report); err != nil {
		fatalf("写报告失败: %v", err)
	}
	fmt.Println(*outputDir)
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatalf("编码JSON失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fatalf("写文件失败: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
