package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MigrationOptions struct {
	SourceRoot string
	Layout     Layout
	Sources    []string
	Overwrite  bool
	DryRun     bool
	Now        func() time.Time
}

type MigrationConflict struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

type MigrationError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type MigrationReport struct {
	MigrationID string              `json:"migration_id"`
	Status      string              `json:"status"`
	Root        string              `json:"root"`
	CopiedFiles int                 `json:"copied_files"`
	CopiedBytes int64               `json:"copied_bytes"`
	Skipped     int                 `json:"skipped"`
	Conflicts   []MigrationConflict `json:"conflicts,omitempty"`
	Errors      []MigrationError    `json:"errors,omitempty"`
	StartedAt   time.Time           `json:"started_at"`
	EndedAt     time.Time           `json:"ended_at,omitempty"`
	DryRun      bool                `json:"dry_run,omitempty"`
}

func Migrate(opts MigrationOptions) (MigrationReport, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sourceRoot := strings.TrimSpace(opts.SourceRoot)
	if sourceRoot == "" {
		var err error
		sourceRoot, err = os.Getwd()
		if err != nil {
			return MigrationReport{}, fmt.Errorf("获取源目录失败: %w", err)
		}
	}
	sourceRoot = filepath.Clean(sourceRoot)
	report := MigrationReport{
		MigrationID: "migration_" + now().UTC().Format("20060102_150405"),
		Status:      "running",
		Root:        opts.Layout.Root,
		StartedAt:   now().UTC(),
		DryRun:      opts.DryRun,
	}
	if strings.TrimSpace(opts.Layout.Root) == "" {
		return report, fmt.Errorf("Storage Layout未配置")
	}
	sources := opts.Sources
	if len(sources) == 0 {
		sources = defaultMigrationSources()
	}
	for _, source := range sources {
		spec, ok := migrationTarget(opts.Layout, source)
		if !ok {
			report.Errors = append(report.Errors, MigrationError{Path: source, Error: "不支持的迁移源"})
			continue
		}
		src := filepath.Join(sourceRoot, filepath.Clean(spec.source))
		if !IsUnderRoot(sourceRoot, src) {
			report.Errors = append(report.Errors, MigrationError{Path: spec.source, Error: "迁移源路径非法"})
			continue
		}
		if samePath(src, spec.target) {
			report.Skipped++
			continue
		}
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				report.Skipped++
				continue
			}
			report.Errors = append(report.Errors, MigrationError{Path: spec.source, Error: err.Error()})
			continue
		}
		if err := migratePath(opts, sourceRoot, src, spec.target, &report); err != nil {
			report.Errors = append(report.Errors, MigrationError{Path: spec.source, Error: err.Error()})
		}
	}
	report.EndedAt = now().UTC()
	if len(report.Errors) > 0 {
		report.Status = "failed"
		return report, fmt.Errorf("迁移完成但存在%d个错误", len(report.Errors))
	}
	report.Status = "completed"
	return report, nil
}

type migrationSpec struct {
	source string
	target string
}

func defaultMigrationSources() []string {
	return []string{"backtest_data", "models/drl", "logs", "backtest_runs", "data", "decision_logs", "coin_pool_cache"}
}

func migrationTarget(layout Layout, source string) (migrationSpec, bool) {
	source = filepath.ToSlash(filepath.Clean(strings.TrimSpace(source)))
	switch source {
	case "backtest_data":
		return migrationSpec{source: "backtest_data", target: layout.BacktestData}, true
	case "models/drl":
		return migrationSpec{source: filepath.Join("models", "drl"), target: layout.ModelsDRL}, true
	case "logs":
		return migrationSpec{source: "logs", target: filepath.Join(layout.Root, "logs")}, true
	case "backtest_runs":
		return migrationSpec{source: "backtest_runs", target: layout.BacktestRuns}, true
	case "data":
		return migrationSpec{source: "data", target: layout.Data}, true
	case "decision_logs":
		return migrationSpec{source: "decision_logs", target: layout.DecisionLogs}, true
	case "coin_pool_cache":
		return migrationSpec{source: "coin_pool_cache", target: layout.CoinPoolCache}, true
	default:
		return migrationSpec{}, false
	}
}

func migratePath(opts MigrationOptions, sourceRoot, src, dst string, report *MigrationReport) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			report.Errors = append(report.Errors, MigrationError{Path: displaySourcePath(sourceRoot, path), Error: err.Error()})
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			report.Errors = append(report.Errors, MigrationError{Path: displaySourcePath(sourceRoot, path), Error: err.Error()})
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if opts.DryRun {
				return nil
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				report.Errors = append(report.Errors, MigrationError{Path: displaySourcePath(sourceRoot, path), Error: err.Error()})
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			report.Errors = append(report.Errors, MigrationError{Path: displaySourcePath(sourceRoot, path), Error: err.Error()})
			return nil
		}
		if _, err := os.Stat(target); err == nil {
			if !opts.Overwrite {
				report.Conflicts = append(report.Conflicts, MigrationConflict{Source: displaySourcePath(sourceRoot, path), Target: RelForDisplay(opts.Layout, target), Reason: "target_exists"})
				report.Skipped++
				return nil
			}
			if same, hashErr := sameFileHash(path, target); hashErr == nil && same {
				report.Skipped++
				return nil
			}
			if !opts.DryRun {
				if err := backupExisting(opts, target); err != nil {
					report.Errors = append(report.Errors, MigrationError{Path: RelForDisplay(opts.Layout, target), Error: err.Error()})
					return nil
				}
			}
		} else if err != nil && !os.IsNotExist(err) {
			report.Errors = append(report.Errors, MigrationError{Path: RelForDisplay(opts.Layout, target), Error: err.Error()})
			return nil
		}
		if !opts.DryRun {
			if err := copyFileWithMode(path, target, info.Mode()); err != nil {
				report.Errors = append(report.Errors, MigrationError{Path: displaySourcePath(sourceRoot, path), Error: err.Error()})
				return nil
			}
		}
		report.CopiedFiles++
		report.CopiedBytes += info.Size()
		return nil
	})
}

func backupExisting(opts MigrationOptions, target string) error {
	rel, err := filepath.Rel(opts.Layout.Root, target)
	if err != nil {
		return err
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	backup := filepath.Join(opts.Layout.Trash, "migration_backup_"+now().UTC().Format("20060102_150405"), rel)
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.MkdirAll(backup, info.Mode())
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	return os.Rename(target, backup)
}

func copyFileWithMode(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func sameFileHash(a, b string) (bool, error) {
	ha, err := fileHash(a)
	if err != nil {
		return false, err
	}
	hb, err := fileHash(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func displaySourcePath(sourceRoot, path string) string {
	if rel, err := filepath.Rel(sourceRoot, path); err == nil {
		return rel
	}
	return filepath.Base(path)
}

func samePath(a, b string) bool {
	ar, aErr := filepath.Abs(a)
	br, bErr := filepath.Abs(b)
	if aErr == nil && bErr == nil {
		return filepath.Clean(ar) == filepath.Clean(br)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
