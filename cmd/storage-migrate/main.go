package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"nofx/config"
	"nofx/storage"
)

func main() {
	root := flag.String("root", "", "Storage Root，默认读取NOFX_STORAGE_ROOT，未设置时使用原生默认值")
	sourceRoot := flag.String("source-root", ".", "待迁移的源码工作区目录")
	sourcesRaw := flag.String("sources", "", "逗号分隔的迁移源，默认迁移全部支持目录")
	overwrite := flag.Bool("overwrite", false, "允许覆盖目标同名文件，覆盖前会备份到trash")
	dryRun := flag.Bool("dry-run", false, "只生成迁移报告，不写入文件")
	reportPath := flag.String("report", "", "迁移报告输出路径，默认输出到stdout")
	flag.Parse()

	runtime, err := storage.ResolveRuntimeConfig(config.StorageConfig{Root: *root}, storage.EnvMapFromOS(), "")
	if err != nil {
		exitErr(err)
	}
	layout := storage.NewLayout(runtime)
	if err := storage.EnsureLayout(layout); err != nil {
		exitErr(err)
	}
	report, err := storage.Migrate(storage.MigrationOptions{
		SourceRoot: *sourceRoot,
		Layout:     layout,
		Sources:    splitCSV(*sourcesRaw),
		Overwrite:  *overwrite,
		DryRun:     *dryRun,
	})
	if writeErr := writeReport(*reportPath, report); writeErr != nil {
		exitErr(writeErr)
	}
	if err != nil {
		exitErr(err)
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func writeReport(path string, report storage.MigrationReport) error {
	if strings.TrimSpace(path) == "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func exitErr(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
