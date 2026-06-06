package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateCopiesKnownSources(t *testing.T) {
	sourceRoot := t.TempDir()
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	writeFile(t, filepath.Join(sourceRoot, "backtest_data", "nofx_history.sqlite"), "db")

	report, err := Migrate(MigrationOptions{SourceRoot: sourceRoot, Layout: layout, Sources: []string{"backtest_data"}})
	if err != nil {
		t.Fatalf("迁移失败: %v report=%+v", err, report)
	}
	if report.CopiedFiles != 1 || report.CopiedBytes != 2 {
		t.Fatalf("迁移摘要异常: %+v", report)
	}
	if got, err := os.ReadFile(filepath.Join(layout.BacktestData, "nofx_history.sqlite")); err != nil || string(got) != "db" {
		t.Fatalf("目标文件异常: %q err=%v", string(got), err)
	}
}

func TestMigrateConflictDoesNotOverwriteByDefault(t *testing.T) {
	sourceRoot := t.TempDir()
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	writeFile(t, filepath.Join(sourceRoot, "data", "state.json"), "new")
	writeFile(t, filepath.Join(layout.Data, "state.json"), "old")

	report, err := Migrate(MigrationOptions{SourceRoot: sourceRoot, Layout: layout, Sources: []string{"data"}})
	if err != nil {
		t.Fatalf("冲突不应导致迁移错误: %v", err)
	}
	if len(report.Conflicts) != 1 || report.Skipped != 1 {
		t.Fatalf("冲突报告异常: %+v", report)
	}
	got, _ := os.ReadFile(filepath.Join(layout.Data, "state.json"))
	if string(got) != "old" {
		t.Fatalf("默认不应覆盖目标文件: %q", string(got))
	}
}

func TestMigrateOverwriteBacksUpTarget(t *testing.T) {
	sourceRoot := t.TempDir()
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	writeFile(t, filepath.Join(sourceRoot, "data", "state.json"), "new")
	writeFile(t, filepath.Join(layout.Data, "state.json"), "old")
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	report, err := Migrate(MigrationOptions{
		SourceRoot: sourceRoot,
		Layout:     layout,
		Sources:    []string{"data"},
		Overwrite:  true,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("覆盖迁移失败: %v report=%+v", err, report)
	}
	got, _ := os.ReadFile(filepath.Join(layout.Data, "state.json"))
	if string(got) != "new" {
		t.Fatalf("目标文件应被覆盖: %q", string(got))
	}
	backup := filepath.Join(layout.Trash, "migration_backup_20260606_120000", "data", "state.json")
	backed, err := os.ReadFile(backup)
	if err != nil || string(backed) != "old" {
		t.Fatalf("备份文件异常: %q err=%v", string(backed), err)
	}
}

func TestMigrateRejectsUnknownSource(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	report, err := Migrate(MigrationOptions{SourceRoot: t.TempDir(), Layout: layout, Sources: []string{"../secret"}})
	if err == nil || report.Status != "failed" || len(report.Errors) != 1 {
		t.Fatalf("未知源应失败: report=%+v err=%v", report, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
