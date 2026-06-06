package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"nofx/config"
	"nofx/logger"
	"os"
	"strings"
	"time"
)

func main() {
	logDir := flag.String("log-dir", "decision_logs", "decision log directory, supports trader subdirectories")
	output := flag.String("output", "", "optional output JSON path")
	reportOnly := flag.Bool("report-only", true, "calculate gate effects without changing live trading behavior")
	dryRun := flag.Bool("dry-run", true, "mark report as dry-run/paper analysis")
	defectFixPack := flag.Bool("defect-fix-pack", false, "annotate replay as chanlun defect fix pack analysis")
	openRejectionDaily := flag.Bool("open-rejection-daily", false, "output read-only daily open rejection report instead of full replay report")
	nearMissLimit := flag.Int("near-miss-limit", 20, "maximum near-miss candidates in open rejection daily report")
	includeBackups := flag.Bool("include-backups", false, "include .bak/backup decision log directories")
	traderID := flag.String("trader", "", "optional trader id filter")
	configPath := flag.String("config", "", "optional NOFX config path for signal-type threshold audit")
	fromText := flag.String("from", "", "optional inclusive start time (RFC3339 or YYYY-MM-DD)")
	toText := flag.String("to", "", "optional inclusive end time (RFC3339 or YYYY-MM-DD)")
	exchangeCloseJSON := flag.String("exchange-close-json", "", "optional read-only JSON export of exchange close snapshots")
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
	if *openRejectionDaily {
		opts, err := openRejectionDailyOptions(*configPath, *traderID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取replay配置失败: %v\n", err)
			os.Exit(1)
		}
		writeJSONOutput(logger.BuildOpenRejectionDailyReportWithOptions(records, *nearMissLimit, opts), *output)
		return
	}
	report := logger.BuildReplayReport(records, *reportOnly, *dryRun)
	if *defectFixPack {
		report.Notes = append(report.Notes, "defect_fix_pack=true: 本报告用于程序化缠论缺陷修复包的离线验收")
	}
	if *exchangeCloseJSON != "" {
		data, err := os.ReadFile(*exchangeCloseJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取交易所平仓快照失败: %v\n", err)
			os.Exit(1)
		}
		var exchangeCloses []logger.ExchangeCloseSnapshot
		if err := json.Unmarshal(data, &exchangeCloses); err != nil {
			fmt.Fprintf(os.Stderr, "解析交易所平仓快照失败: %v\n", err)
			os.Exit(1)
		}
		logger.AttachExchangeCloseSnapshots(&report, records, exchangeCloses)
	}

	writeJSONOutput(report, *output)
}

func openRejectionDailyOptions(configPath, traderID string) (logger.OpenRejectionDailyOptions, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return logger.OpenRejectionDailyOptions{}, nil
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return logger.OpenRejectionDailyOptions{}, err
	}
	trader := selectReplayTraderConfig(cfg.Traders, traderID)
	if trader == nil {
		return logger.OpenRejectionDailyOptions{}, fmt.Errorf("未找到可用于replay审计的trader配置: %s", traderID)
	}
	thresholds := trader.ChanlunV2Strategy.EntryTiming.EntryZone.SignalTypeMinRR
	copied := make(map[string]float64, len(thresholds))
	for key, value := range thresholds {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" && value > 0 {
			copied[key] = value
		}
	}
	return logger.OpenRejectionDailyOptions{
		SignalTypeMinRR: copied,
		ConfigSource:    configPath,
	}, nil
}

func selectReplayTraderConfig(traders []config.TraderConfig, traderID string) *config.TraderConfig {
	traderID = strings.TrimSpace(traderID)
	for i := range traders {
		if traderID != "" && traders[i].ID == traderID {
			return &traders[i]
		}
	}
	if traderID != "" {
		return nil
	}
	for i := range traders {
		if traders[i].Enabled && strings.EqualFold(traders[i].DecisionMode, "chanlun_v2") {
			return &traders[i]
		}
	}
	for i := range traders {
		if strings.EqualFold(traders[i].DecisionMode, "chanlun_v2") {
			return &traders[i]
		}
	}
	return nil
}

func writeJSONOutput(value any, output string) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化replay输出失败: %v\n", err)
		os.Exit(1)
	}
	if output != "" {
		if err := os.WriteFile(output, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入replay输出失败: %v\n", err)
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
