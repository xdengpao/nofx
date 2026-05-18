package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"nofx/backtest"
	"nofx/historydb"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch":
		err = runFetch(os.Args[2:])
	case "inspect":
		err = runInspect(os.Args[2:])
	case "gaps":
		err = runGaps(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	dbPath := fs.String("db", backtest.DefaultHistoryDBPath, "历史数据库路径")
	sourceName := fs.String("source", backtest.DefaultSource, "数据源")
	symbols := fs.String("symbols", "", "逗号分隔symbol")
	timeframes := fs.String("timeframes", "3m,15m,1h,4h", "逗号分隔timeframe")
	dataFrom := fs.String("data-from", "", "抓取起始时间")
	dataTo := fs.String("data-to", "", "抓取结束时间")
	timezone := fs.String("timezone", backtest.DefaultTimezone, "timezone")
	limit := fs.Int("limit", 1000, "分页limit")
	rpm := fs.Int("requests-per-minute", 120, "每分钟请求数")
	concurrency := fs.Int("concurrency", 1, "并发数")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loc, err := time.LoadLocation(*timezone)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*dataFrom) == "" {
		return fmt.Errorf("必须指定data-from")
	}
	from, err := backtest.ParseConfigTime(*dataFrom, loc)
	if err != nil {
		return err
	}
	to := time.Now().In(loc)
	if strings.TrimSpace(*dataTo) != "" {
		to, err = backtest.ParseConfigTime(*dataTo, loc)
		if err != nil {
			return err
		}
	}
	if !from.Before(to) {
		return fmt.Errorf("data-from必须早于data-to")
	}
	store, err := historydb.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	var source historydb.KlineSource
	switch strings.ToLower(*sourceName) {
	case "binance-futures", "":
		source = historydb.NewBinanceFuturesKlineSource()
	default:
		return fmt.Errorf("不支持的数据源: %s", *sourceName)
	}
	summary, err := historydb.FetchToStore(context.Background(), historydb.FetchOptions{
		Source:     source,
		Store:      store,
		Symbols:    splitCSV(*symbols),
		Timeframes: splitCSV(*timeframes),
		DataFrom:   from,
		DataTo:     to,
		RateLimit: historydb.RateLimitProfile{
			RequestsPerMinute: *rpm,
			Concurrency:       *concurrency,
			PageLimit:         *limit,
		},
	})
	printJSON(summary)
	return err
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	dbPath := fs.String("db", backtest.DefaultHistoryDBPath, "历史数据库路径")
	source := fs.String("source", backtest.DefaultSource, "数据源")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := historydb.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	coverage, err := store.Inspect(context.Background(), *source)
	if err != nil {
		return err
	}
	printJSON(coverage)
	return nil
}

func runGaps(args []string) error {
	fs := flag.NewFlagSet("gaps", flag.ExitOnError)
	dbPath := fs.String("db", backtest.DefaultHistoryDBPath, "历史数据库路径")
	source := fs.String("source", backtest.DefaultSource, "数据源")
	symbol := fs.String("symbol", "", "symbol")
	timeframe := fs.String("timeframe", "3m", "timeframe")
	fromRaw := fs.String("from", "1970-01-01", "起始时间")
	toRaw := fs.String("to", time.Now().Format(time.RFC3339), "结束时间")
	timezone := fs.String("timezone", backtest.DefaultTimezone, "timezone")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loc, err := time.LoadLocation(*timezone)
	if err != nil {
		return err
	}
	from, err := backtest.ParseConfigTime(*fromRaw, loc)
	if err != nil {
		return err
	}
	to, err := backtest.ParseConfigTime(*toRaw, loc)
	if err != nil {
		return err
	}
	store, err := historydb.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	gaps, err := store.DetectGaps(context.Background(), *source, *symbol, *timeframe, from, to)
	if err != nil {
		return err
	}
	printJSON(gaps)
	return nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func printJSON(value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(data))
}

func usage() {
	fmt.Println("usage: history-data fetch|inspect|gaps [flags]")
}
