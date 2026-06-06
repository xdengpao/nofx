package storage

import (
	"os"
	"path/filepath"
	"testing"

	"nofx/config"
)

func TestResolveRuntimeConfigPriority(t *testing.T) {
	cwd := t.TempDir()
	cfg := config.StorageConfig{Root: "from-config"}
	runtime, err := ResolveRuntimeConfig(cfg, map[string]string{"NOFX_STORAGE_ROOT": "/tmp/from-env"}, cwd)
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig失败: %v", err)
	}
	if runtime.Root != "/tmp/from-env" || runtime.RootSource != RootSourceEnv {
		t.Fatalf("env应优先: %+v", runtime)
	}

	runtime, err = ResolveRuntimeConfig(cfg, nil, cwd)
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig失败: %v", err)
	}
	want := filepath.Join(cwd, "from-config")
	if runtime.Root != want || runtime.RootSource != RootSourceConfig {
		t.Fatalf("config root异常: got=%+v want=%s", runtime, want)
	}

	runtime, err = ResolveRuntimeConfig(config.StorageConfig{}, nil, cwd)
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig失败: %v", err)
	}
	if runtime.Root != DefaultNativeRoot || runtime.RootSource != RootSourceDefault {
		t.Fatalf("默认root异常: %+v", runtime)
	}
}

func TestResolveRuntimeConfigComposeEnv(t *testing.T) {
	runtime, err := ResolveRuntimeConfig(config.StorageConfig{}, map[string]string{
		"NOFX_STORAGE_ROOT":             "/app/runtime",
		"NOFX_CONTAINER_STORAGE_ROOT":   "/app/runtime",
		"NOFX_RUNTIME_HOST_PATH":        "./runtime",
		"NOFX_STORAGE_DISK_TOTAL_BYTES": "1000",
		"NOFX_STORAGE_DISK_FREE_BYTES":  "400",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRuntimeConfig失败: %v", err)
	}
	if runtime.Root != "/app/runtime" || runtime.ContainerRoot != "/app/runtime" || runtime.HostMountPath != "./runtime" {
		t.Fatalf("Compose env解析异常: %+v", runtime)
	}
	if runtime.DiskTotalBytes != 1000 || runtime.DiskFreeBytes != 400 || runtime.DiskSource != "host_env" {
		t.Fatalf("宿主机容量覆盖解析异常: %+v", runtime)
	}
}

func TestEnsureLayoutRequiresExistingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err == nil {
		t.Fatal("Root不存在时应失败")
	}
}

func TestEnsureLayoutCreatesSubdirs(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	for _, path := range []string{layout.BacktestData, layout.ModelsDRL, layout.TrainLogs, layout.TrainJobs, layout.BacktestRuns, layout.Data, layout.DecisionLogs, layout.CoinPoolCache, layout.Tmp, layout.Trash} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("目录未创建: %s err=%v", path, err)
		}
	}
}

func TestResolveUnderRoot(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	got, err := ResolveUnderRoot(layout, "models/drl/model.onnx")
	if err != nil {
		t.Fatalf("相对路径解析失败: %v", err)
	}
	if got != filepath.Join(root, "models", "drl", "model.onnx") {
		t.Fatalf("相对路径异常: %s", got)
	}
	if _, err := ResolveUnderRoot(layout, filepath.Join(t.TempDir(), "model.onnx")); err == nil {
		t.Fatal("Root外绝对路径应拒绝")
	}
	layout.AllowExternalPaths = true
	if _, err := ResolveUnderRoot(layout, filepath.Join(t.TempDir(), "model.onnx")); err != nil {
		t.Fatalf("开发开关开启后应允许外部路径: %v", err)
	}
}

func TestRelForDisplay(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(RuntimeConfig{Root: root})
	path := filepath.Join(root, "models", "drl", "model.onnx")
	if got := RelForDisplay(layout, path); got != filepath.Join("models", "drl", "model.onnx") {
		t.Fatalf("相对展示异常: %s", got)
	}
	if got := RelForDisplay(layout, "/outside/secret/model.onnx"); got != "model.onnx" {
		t.Fatalf("Root外路径应只展示文件名: %s", got)
	}
}

func TestDiskUsageUsesRuntimeDiskOverride(t *testing.T) {
	root := t.TempDir()
	runtime := RuntimeConfig{
		Root:           root,
		HostMountPath:  "/Volumes/light2/nofx",
		ContainerRoot:  "/app/runtime",
		DiskTotalBytes: 1000,
		DiskFreeBytes:  250,
		DiskSource:     "host_env",
	}
	layout := NewLayout(runtime)
	if err := EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	diag, err := DiskUsage(runtime, layout)
	if err != nil {
		t.Fatalf("DiskUsage失败: %v", err)
	}
	if diag.Disk.TotalBytes != 1000 || diag.Disk.FreeBytes != 250 || diag.Disk.UsedBytes != 750 || diag.Disk.Source != "host_env" {
		t.Fatalf("应使用runtime容量覆盖: %+v", diag.Disk)
	}
	if len(diag.Warnings) != 0 {
		t.Fatalf("使用宿主机容量覆盖时不应提示statfs估算: %+v", diag.Warnings)
	}
}
