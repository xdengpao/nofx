package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"nofx/logger"
	"os"
	"time"
)

func main() {
	logDir := flag.String("log-dir", "decision_logs", "decision log directory, supports trader subdirectories")
	output := flag.String("output", "", "optional output JSON path")
	reportOnly := flag.Bool("report-only", true, "calculate gate effects without changing live trading behavior")
	dryRun := flag.Bool("dry-run", true, "mark report as dry-run/paper analysis")
	includeBackups := flag.Bool("include-backups", false, "include .bak/backup decision log directories")
	traderID := flag.String("trader", "", "optional trader id filter")
	fromText := flag.String("from", "", "optional inclusive start time (RFC3339 or YYYY-MM-DD)")
	toText := flag.String("to", "", "optional inclusive end time (RFC3339 or YYYY-MM-DD)")
	flag.Parse()

	records, err := logger.LoadDecisionRecordsRecursive(*logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay失败: %v\n", err)
		os.Exit(1)
	}
	from, err := parseReplayTime(*fromText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析from失败: %v\n", err)
		os.Exit(1)
	}
	to, err := parseReplayTime(*toText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析to失败: %v\n", err)
		os.Exit(1)
	}
	records = logger.FilterReplayRecords(records, logger.ReplayFilter{
		IncludeBackups: *includeBackups,
		TraderID:       *traderID,
		From:           from,
		To:             to,
	})
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

func parseReplayTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("时间格式必须是 RFC3339 或 YYYY-MM-DD: %s", value)
}
