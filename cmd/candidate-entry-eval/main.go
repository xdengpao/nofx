package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nofx/config"
	"nofx/diagnostics"
	"nofx/logger"
	"nofx/pool"
)

func main() {
	logDir := flag.String("log-dir", "decision_logs", "decision log directory, supports trader subdirectories")
	traderID := flag.String("trader", "", "optional trader id filter")
	configPath := flag.String("config", "config.json", "optional NOFX config path")
	fromText := flag.String("from", "", "optional inclusive start time (RFC3339 or YYYY-MM-DD), defaults to now-48h")
	toText := flag.String("to", "", "optional inclusive end time (RFC3339 or YYYY-MM-DD), defaults to now")
	output := flag.String("output", "", "optional output JSON path")
	snapshotPath := flag.String("snapshot-path", filepath.Join(os.TempDir(), "nofx_dynamic_candidate_pool_preview.json"), "dynamic preview snapshot path when -write-snapshot=true")
	dynamicPoolDryRun := flag.Bool("dynamic-pool-dry-run", true, "generate read-only dynamic candidate pool preview")
	writeSnapshot := flag.Bool("write-snapshot", false, "write dynamic preview snapshot; defaults to false")
	flag.Parse()

	now := time.Now()
	from, err := parseEvalTime(*fromText)
	if err != nil {
		exitf("解析from失败: %v", err)
	}
	if from.IsZero() {
		from = now.Add(-48 * time.Hour)
	}
	to, err := parseEvalTime(*toText)
	if err != nil {
		exitf("解析to失败: %v", err)
	}
	if to.IsZero() {
		to = now
	}

	records, err := logger.LoadDecisionRecordsRecursive(*logDir)
	if err != nil {
		exitf("读取决策日志失败: %v", err)
	}
	records = logger.FilterReplayRecords(records, logger.ReplayFilter{
		TraderID: strings.TrimSpace(*traderID),
		From:     from,
		To:       to,
	})

	cfg, cfgErr := loadEvalConfig(*configPath)
	notes := []string{
		"历史日志事实来自过滤后的最近48小时窗口；动态池预览使用命令运行时的公共行情/API快照",
		"final_rr_threshold=2.5 kept: 本命令不降低最终RR阈值",
		"btc_hard_veto kept: 本命令不关闭或削弱BTC hard veto",
	}
	if cfgErr != nil {
		notes = append(notes, "config_load_warning: "+cfgErr.Error())
	}

	var snapshot *pool.DynamicCandidatePool
	var merged *pool.MergedCoinPool
	var previewErr error
	if *dynamicPoolDryRun {
		previewSnapshotPath := safeSnapshotPath(*snapshotPath)
		snapshot, merged, previewErr = pool.PreviewDynamicCandidatePool(pool.DynamicPoolPreviewOptions{
			AI500Limit:      20,
			PositionSymbols: latestPositionSymbols(records),
			PoolConfig:      buildPreviewPoolConfig(cfg, previewSnapshotPath),
			SourceConfig:    buildSourceConfig(cfg),
			SnapshotPath:    previewSnapshotPath,
			WriteSnapshot:   *writeSnapshot,
			ForceRefresh:    true,
		})
	} else {
		notes = append(notes, "dynamic_pool_dry_run=false: 已跳过动态候选池预览")
	}

	report := diagnostics.BuildCandidateEntryEvaluationReport(records, diagnostics.CandidateEntryEvaluationOptions{
		TraderID:           strings.TrimSpace(*traderID),
		DryRun:             true,
		ConfigSource:       configSource(cfg, *configPath),
		StaticSymbols:      staticSymbols(cfg),
		DynamicSnapshot:    snapshot,
		DynamicMergedPool:  merged,
		DynamicPreviewErr:  previewErr,
		GeneratedAt:        now,
		EnabledTraderCount: enabledTraderCount(cfg),
		Notes:              notes,
	})
	writeJSONOutput(report, *output)
}

func loadEvalConfig(path string) (*config.Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func buildPreviewPoolConfig(cfg *config.Config, snapshotPath string) pool.DynamicCandidatePoolConfig {
	preview := pool.DynamicCandidatePoolConfig{
		Enabled:      true,
		SnapshotPath: safeSnapshotPath(snapshotPath),
		ShortSideCoverage: pool.DynamicCandidateShortSideCoverageConfig{
			Enabled:    true,
			ReportOnly: true,
		},
	}
	if cfg == nil {
		return preview
	}
	source := cfg.DynamicCandidatePool
	preview.RefreshHour = source.RefreshHour
	preview.TTLHours = source.TTLHours
	preview.MinPoolSize = source.MinPoolSize
	preview.MaxPoolSize = source.MaxPoolSize
	preview.PromptCandidateLimit = source.PromptCandidateLimit
	preview.CoreSymbols = append([]string(nil), source.CoreSymbols...)
	preview.MinOIValueUSD = source.MinOIValueUSD
	preview.MinQuoteVolume24hUSD = source.MinQuoteVolume24hUSD
	preview.CooldownDaysAfterLosses = source.CooldownDaysAfterLosses
	preview.ExchangeVolumeTopLimit = source.ExchangeVolumeTopLimit
	return preview
}

func buildSourceConfig(cfg *config.Config) pool.CoinPoolSourceConfig {
	if cfg == nil {
		return pool.CoinPoolSourceConfig{
			UseDefaultCoins: true,
			CacheDir:        filepath.Join(os.TempDir(), "nofx_candidate_entry_eval_cache"),
			Timeout:         30 * time.Second,
		}
	}
	return pool.CoinPoolSourceConfig{
		CoinPoolAPIURL:  cfg.CoinPoolAPIURL,
		OITopAPIURL:     cfg.OITopAPIURL,
		UseDefaultCoins: cfg.UseDefaultCoins,
		DefaultCoins:    append([]string(nil), cfg.DefaultCoins...),
		CacheDir:        filepath.Join(os.TempDir(), "nofx_candidate_entry_eval_cache"),
		Timeout:         30 * time.Second,
	}
}

func staticSymbols(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return append([]string(nil), cfg.DefaultCoins...)
}

func latestPositionSymbols(records []*logger.DecisionRecord) []string {
	seen := map[string]bool{}
	var symbols []string
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if record == nil || len(record.Positions) == 0 {
			continue
		}
		for _, position := range record.Positions {
			if position.PositionAmt == 0 {
				continue
			}
			symbol := strings.ToUpper(strings.TrimSpace(position.Symbol))
			if symbol == "" || seen[symbol] {
				continue
			}
			seen[symbol] = true
			symbols = append(symbols, symbol)
		}
		break
	}
	return symbols
}

func enabledTraderCount(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	count := 0
	for _, trader := range cfg.Traders {
		if trader.Enabled {
			count++
		}
	}
	return count
}

func configSource(cfg *config.Config, path string) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(path)
}

func safeSnapshotPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return filepath.Join(os.TempDir(), "nofx_dynamic_candidate_pool_preview.json")
	}
	clean := filepath.Clean(path)
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == "data" {
			return filepath.Join(os.TempDir(), filepath.Base(clean))
		}
	}
	return clean
}

func parseEvalTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
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

func writeJSONOutput(value any, output string) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		exitf("序列化评估报告失败: %v", err)
	}
	if strings.TrimSpace(output) == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(output, data, 0644); err != nil {
		exitf("写入评估报告失败: %v", err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
