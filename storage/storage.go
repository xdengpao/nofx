package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"nofx/config"
)

const (
	DefaultNativeRoot    = "/Volumes/light2/nofx"
	DefaultContainerRoot = "/app/runtime"
	DefaultHostMountPath = "./runtime"
)

type RootSource string

const (
	RootSourceEnv     RootSource = "env"
	RootSourceConfig  RootSource = "config"
	RootSourceDefault RootSource = "default"
)

type RuntimeConfig struct {
	Root               string     `json:"root"`
	RootSource         RootSource `json:"root_source"`
	HostMountPath      string     `json:"host_mount_path,omitempty"`
	HostMountSource    string     `json:"host_mount_source,omitempty"`
	ContainerRoot      string     `json:"container_root,omitempty"`
	AllowExternalPaths bool       `json:"allow_external_paths"`
	DiskTotalBytes     int64      `json:"disk_total_bytes,omitempty"`
	DiskFreeBytes      int64      `json:"disk_free_bytes,omitempty"`
	DiskSource         string     `json:"disk_source,omitempty"`
}

type Layout struct {
	Root               string `json:"root"`
	BacktestData       string `json:"backtest_data"`
	HistoryDB          string `json:"history_db"`
	ModelsDRL          string `json:"models_drl"`
	TrainLogs          string `json:"train_logs"`
	TrainJobs          string `json:"train_jobs"`
	BacktestRuns       string `json:"backtest_runs"`
	Data               string `json:"data"`
	DecisionLogs       string `json:"decision_logs"`
	CoinPoolCache      string `json:"coin_pool_cache"`
	Tmp                string `json:"tmp"`
	Trash              string `json:"trash"`
	AllowExternalPaths bool   `json:"allow_external_paths"`
}

type DirectoryDiagnostic struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Bytes    int64  `json:"bytes"`
	Error    string `json:"error,omitempty"`
}

type DiskDiagnostic struct {
	TotalBytes int64  `json:"total_bytes"`
	FreeBytes  int64  `json:"free_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
	Source     string `json:"source,omitempty"`
}

type Diagnostics struct {
	Root               string                `json:"root"`
	RootSource         RootSource            `json:"root_source"`
	HostMountPath      string                `json:"host_mount_path,omitempty"`
	HostMountSource    string                `json:"host_mount_source,omitempty"`
	ContainerRoot      string                `json:"container_root,omitempty"`
	AllowExternalPaths bool                  `json:"allow_external_paths"`
	Directories        []DirectoryDiagnostic `json:"directories"`
	Disk               DiskDiagnostic        `json:"disk"`
	Warnings           []string              `json:"warnings,omitempty"`
}

func EnvMapFromOS() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func ResolveRuntimeConfig(cfg config.StorageConfig, env map[string]string, cwd string) (RuntimeConfig, error) {
	if env == nil {
		env = EnvMapFromOS()
	}
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("获取当前目录失败: %w", err)
		}
	}
	root := strings.TrimSpace(env["NOFX_STORAGE_ROOT"])
	source := RootSourceEnv
	if root == "" {
		root = strings.TrimSpace(cfg.Root)
		source = RootSourceConfig
	}
	if root == "" {
		root = DefaultNativeRoot
		source = RootSourceDefault
	}
	root = cleanRoot(root, cwd)

	allowExternal := cfg.AllowExternalPaths
	if value := strings.TrimSpace(env["NOFX_STORAGE_ALLOW_EXTERNAL_PATHS"]); value != "" {
		allowExternal = parseEnvBool(value)
	}

	hostMountPath := strings.TrimSpace(env["NOFX_RUNTIME_HOST_PATH"])
	hostMountSource := ""
	if hostMountPath != "" {
		hostMountSource = "env"
	} else if strings.TrimSpace(env["NOFX_STORAGE_ROOT"]) == DefaultContainerRoot || strings.TrimSpace(env["NOFX_CONTAINER_STORAGE_ROOT"]) != "" {
		hostMountPath = DefaultHostMountPath
		hostMountSource = "default"
	}
	containerRoot := strings.TrimSpace(env["NOFX_CONTAINER_STORAGE_ROOT"])
	diskTotal, _ := parseOptionalEnvInt64(env["NOFX_STORAGE_DISK_TOTAL_BYTES"])
	diskFree, _ := parseOptionalEnvInt64(env["NOFX_STORAGE_DISK_FREE_BYTES"])
	diskSource := ""
	if diskTotal > 0 && diskFree >= 0 && diskFree <= diskTotal {
		diskSource = "host_env"
	}

	return RuntimeConfig{
		Root:               root,
		RootSource:         source,
		HostMountPath:      hostMountPath,
		HostMountSource:    hostMountSource,
		ContainerRoot:      containerRoot,
		AllowExternalPaths: allowExternal,
		DiskTotalBytes:     diskTotal,
		DiskFreeBytes:      diskFree,
		DiskSource:         diskSource,
	}, nil
}

func NewLayout(runtime RuntimeConfig) Layout {
	root := filepath.Clean(runtime.Root)
	models := filepath.Join(root, "models", "drl")
	trainLogs := filepath.Join(root, "logs", "drl_ppo_train")
	return Layout{
		Root:               root,
		BacktestData:       filepath.Join(root, "backtest_data"),
		HistoryDB:          filepath.Join(root, "backtest_data", "nofx_history.sqlite"),
		ModelsDRL:          models,
		TrainLogs:          trainLogs,
		TrainJobs:          filepath.Join(trainLogs, "jobs"),
		BacktestRuns:       filepath.Join(root, "backtest_runs"),
		Data:               filepath.Join(root, "data"),
		DecisionLogs:       filepath.Join(root, "decision_logs"),
		CoinPoolCache:      filepath.Join(root, "coin_pool_cache"),
		Tmp:                filepath.Join(root, "tmp"),
		Trash:              filepath.Join(root, "trash"),
		AllowExternalPaths: runtime.AllowExternalPaths,
	}
}

func EnsureLayout(layout Layout) error {
	if strings.TrimSpace(layout.Root) == "" {
		return fmt.Errorf("Storage Root不能为空")
	}
	info, err := os.Stat(layout.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Storage Root不存在，请先创建目录或修改NOFX_STORAGE_ROOT: %s", layout.Root)
		}
		return fmt.Errorf("检查Storage Root失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Storage Root不是目录: %s", layout.Root)
	}
	if !isWritable(layout.Root) {
		return fmt.Errorf("Storage Root不可写: %s", layout.Root)
	}
	for _, dir := range layoutDirs(layout) {
		if err := os.MkdirAll(dir.path, 0o755); err != nil {
			return fmt.Errorf("创建运行时目录失败(%s): %w", dir.name, err)
		}
	}
	return nil
}

func ResolveUnderRoot(layout Layout, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	var path string
	if filepath.IsAbs(value) {
		path = filepath.Clean(value)
	} else {
		path = filepath.Join(layout.Root, value)
	}
	if layout.AllowExternalPaths {
		return path, nil
	}
	if !IsUnderRoot(layout.Root, path) {
		return "", fmt.Errorf("路径必须位于Storage Root内: %s", value)
	}
	return path, nil
}

func IsUnderRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != ".." && !filepath.IsAbs(rel))
}

func RelForDisplay(layout Layout, path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	if IsUnderRoot(layout.Root, path) {
		if rel, err := filepath.Rel(layout.Root, path); err == nil {
			return rel
		}
	}
	return filepath.Base(path)
}

func DiskUsage(runtime RuntimeConfig, layout Layout) (*Diagnostics, error) {
	diag := &Diagnostics{
		Root:               layout.Root,
		RootSource:         runtime.RootSource,
		HostMountPath:      runtime.HostMountPath,
		HostMountSource:    runtime.HostMountSource,
		ContainerRoot:      runtime.ContainerRoot,
		AllowExternalPaths: layout.AllowExternalPaths,
	}
	if layout.AllowExternalPaths {
		diag.Warnings = append(diag.Warnings, "已允许Storage Root外路径，仅建议开发环境使用")
	}
	diskOverride := runtimeDiskOverride(runtime)
	if diskOverride == nil && strings.TrimSpace(runtime.HostMountPath) != "" && strings.TrimSpace(runtime.ContainerRoot) != "" {
		diag.Warnings = append(diag.Warnings, "检测到Docker bind mount，磁盘容量为容器视角估算；macOS Docker Desktop下可能不同于宿主机df结果")
	}
	for _, dir := range layoutDirs(layout) {
		item := DirectoryDiagnostic{Name: dir.name, Path: RelForDisplay(layout, dir.path)}
		if info, err := os.Stat(dir.path); err == nil && info.IsDir() {
			item.Exists = true
			item.Writable = isWritable(dir.path)
			item.Bytes, item.Error = dirSize(dir.path)
		} else if err != nil {
			item.Error = err.Error()
		}
		diag.Directories = append(diag.Directories, item)
	}
	if diskOverride != nil {
		diag.Disk = *diskOverride
	} else {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(layout.Root, &stat); err == nil {
			total := int64(stat.Blocks) * int64(stat.Bsize)
			free := int64(stat.Bavail) * int64(stat.Bsize)
			diag.Disk = DiskDiagnostic{TotalBytes: total, FreeBytes: free, UsedBytes: total - free, Source: "statfs"}
		}
	}
	return diag, nil
}

func runtimeDiskOverride(runtime RuntimeConfig) *DiskDiagnostic {
	if runtime.DiskTotalBytes <= 0 || runtime.DiskFreeBytes < 0 || runtime.DiskFreeBytes > runtime.DiskTotalBytes {
		return nil
	}
	source := strings.TrimSpace(runtime.DiskSource)
	if source == "" {
		source = "env"
	}
	return &DiskDiagnostic{
		TotalBytes: runtime.DiskTotalBytes,
		FreeBytes:  runtime.DiskFreeBytes,
		UsedBytes:  runtime.DiskTotalBytes - runtime.DiskFreeBytes,
		Source:     source,
	}
}

type namedDir struct {
	name string
	path string
}

func layoutDirs(layout Layout) []namedDir {
	return []namedDir{
		{name: "backtest_data", path: layout.BacktestData},
		{name: "models_drl", path: layout.ModelsDRL},
		{name: "train_logs", path: layout.TrainLogs},
		{name: "train_jobs", path: layout.TrainJobs},
		{name: "backtest_runs", path: layout.BacktestRuns},
		{name: "data", path: layout.Data},
		{name: "decision_logs", path: layout.DecisionLogs},
		{name: "coin_pool_cache", path: layout.CoinPoolCache},
		{name: "tmp", path: layout.Tmp},
		{name: "trash", path: layout.Trash},
	}
}

func cleanRoot(root, cwd string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return root
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root)
	}
	return filepath.Clean(filepath.Join(cwd, root))
}

func parseEnvBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enabled":
		return true
	default:
		return false
	}
}

func parseOptionalEnvInt64(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("环境变量必须是非负整数: %s", value)
	}
	return parsed, nil
}

func isWritable(dir string) bool {
	file, err := os.CreateTemp(dir, ".nofx-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func dirSize(path string) (int64, string) {
	var total int64
	var firstErr string
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			return nil
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		} else if firstErr == "" {
			firstErr = infoErr.Error()
		}
		return nil
	})
	if err != nil && firstErr == "" {
		firstErr = err.Error()
	}
	return total, firstErr
}
