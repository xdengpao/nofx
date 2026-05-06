package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"nofx/logger"
	"os"
)

func main() {
	logDir := flag.String("log-dir", "decision_logs", "decision log directory, supports trader subdirectories")
	output := flag.String("output", "", "optional output JSON path")
	reportOnly := flag.Bool("report-only", true, "calculate gate effects without changing live trading behavior")
	dryRun := flag.Bool("dry-run", true, "mark report as dry-run/paper analysis")
	flag.Parse()

	records, err := logger.LoadDecisionRecordsRecursive(*logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay失败: %v\n", err)
		os.Exit(1)
	}
	report := logger.BuildReplayReport(records, *reportOnly, *dryRun)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化replay报告失败: %v\n", err)
		os.Exit(1)
	}
	if *output != "" {
		if err := os.WriteFile(*output, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入replay报告失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Println(string(data))
}
