package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"nofx/backtest"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runSingle(os.Args[2:])
	case "batch":
		err = runBatch(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func runSingle(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "", "回测配置JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("必须指定-config")
	}
	cfg, err := backtest.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	runner, err := backtest.NewRunner(cfg, nil)
	if err != nil {
		return err
	}
	defer runner.Store.Close()
	result, err := runner.Run(context.Background())
	printJSON(map[string]any{
		"run_id":      result.RunID,
		"output_dir":  result.OutputDir,
		"summary":     result.Report.Summary,
		"assumptions": result.Report.Assumptions,
	})
	return err
}

func runBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	configPath := fs.String("config", "", "批量回测配置JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("必须指定-config")
	}
	cfg, err := backtest.LoadBatchConfig(*configPath)
	if err != nil {
		return err
	}
	result, err := (&backtest.BatchRunner{Config: *cfg}).Run(context.Background())
	printJSON(result)
	return err
}

func printJSON(value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(data))
}

func usage() {
	fmt.Println("usage: backtest run|batch -config <file>")
}
