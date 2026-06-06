package drltrain

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"nofx/backtest"
	"nofx/historydb"
	"nofx/storage"
)

var (
	symbolPattern    = regexp.MustCompile(`^[A-Z0-9]{2,30}USDT$`)
	modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)
)

type Manager struct {
	cfg     Config
	mu      sync.RWMutex
	jobs    map[string]*managedJob
	running int
	now     func() time.Time
}

type managedJob struct {
	meta   *JobMetadata
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

func NewManager(cfg Config) (*Manager, error) {
	if strings.TrimSpace(cfg.Layout.Root) == "" {
		return nil, fmt.Errorf("Storage Layout未配置")
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 1
	}
	if strings.TrimSpace(cfg.PythonBin) == "" {
		cfg.PythonBin = "python3"
	}
	if strings.TrimSpace(cfg.TrainScript) == "" {
		cfg.TrainScript = "training/drl/scripts/train.py"
	}
	if cfg.LogTailBytes <= 0 {
		cfg.LogTailBytes = 256 * 1024
	}
	m := &Manager{cfg: cfg, jobs: map[string]*managedJob{}, now: time.Now}
	if err := os.MkdirAll(cfg.Layout.TrainJobs, 0o755); err != nil {
		return nil, fmt.Errorf("创建训练job目录失败: %w", err)
	}
	if err := m.Recover(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Start(ctx context.Context, req TrainRequest) (*JobMetadata, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	req = normalizeRequest(req)
	if err := m.CheckTrainingEnvironment(); err != nil {
		return nil, err
	}
	if !req.AllowIncompleteData {
		if err := m.checkCoverage(ctx, req); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	if m.running >= m.cfg.MaxConcurrency {
		m.mu.Unlock()
		return nil, fmt.Errorf("已有DRL-PPO训练任务运行，当前并发上限为%d", m.cfg.MaxConcurrency)
	}
	jobID := "drl_ppo_train_" + m.now().UTC().Format("20060102_150405_000000000")
	jobDir := filepath.Join(m.cfg.Layout.TrainJobs, jobID)
	meta := &JobMetadata{
		JobID:        jobID,
		Type:         "drl_ppo_train",
		Status:       JobPending,
		Request:      req,
		JobDir:       jobDir,
		StdoutPath:   filepath.Join(jobDir, "stdout.log"),
		StderrPath:   filepath.Join(jobDir, "stderr.log"),
		ProgressPath: filepath.Join(jobDir, "progress.jsonl"),
		SummaryPath:  filepath.Join(jobDir, "summary.json"),
		Progress:     TrainProgress{TotalTimesteps: req.TotalTimesteps},
	}
	m.jobs[jobID] = &managedJob{meta: meta}
	m.running++
	m.mu.Unlock()

	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		m.finish(jobID, JobFailed, fmt.Errorf("创建训练job目录失败: %w", err), -1)
		return nil, err
	}
	if err := m.persist(meta); err != nil {
		m.finish(jobID, JobFailed, err, -1)
		return nil, err
	}
	go m.runJob(context.Background(), jobID)
	return meta, nil
}

func (m *Manager) EnvironmentStatus() EnvironmentStatus {
	status := EnvironmentStatus{}
	if m == nil {
		status.Errors = append(status.Errors, "DRL-PPO训练管理器未初始化")
		return status
	}
	status.PythonBin = strings.TrimSpace(m.cfg.PythonBin)
	if status.PythonBin == "" {
		status.PythonBin = "python3"
	}
	if pythonPath, err := exec.LookPath(status.PythonBin); err == nil {
		status.PythonPath = pythonPath
		status.PythonAvailable = true
	} else {
		status.Errors = append(status.Errors, fmt.Sprintf("找不到Python可执行文件: %s", status.PythonBin))
	}

	status.TrainScript = strings.TrimSpace(m.cfg.TrainScript)
	if status.TrainScript == "" {
		status.TrainScript = "training/drl/scripts/train.py"
	}
	if scriptExists(status.TrainScript) {
		status.TrainScriptAvailable = true
	} else {
		status.Errors = append(status.Errors, fmt.Sprintf("训练脚本不存在: %s", status.TrainScript))
	}

	status.EvaluateScript = strings.TrimSpace(m.cfg.EvaluateScript)
	if status.EvaluateScript == "" {
		status.EvaluateScript = "training/drl/scripts/evaluate.py"
	}
	if scriptExists(status.EvaluateScript) {
		status.EvaluateScriptAvailable = true
	} else {
		status.Errors = append(status.Errors, fmt.Sprintf("评估脚本不存在: %s", status.EvaluateScript))
	}

	status.Ready = status.PythonAvailable && status.TrainScriptAvailable
	return status
}

func (m *Manager) CheckTrainingEnvironment() error {
	status := m.EnvironmentStatus()
	if status.Ready {
		return nil
	}
	return fmt.Errorf("DRL-PPO训练环境不可用: %s", strings.Join(status.Errors, "；"))
}

func scriptExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (m *Manager) List() []*JobMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*JobMetadata, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, cloneMeta(job.meta))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func (m *Manager) Get(jobID string) (*JobMetadata, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[jobID]
	if job == nil {
		return nil, false
	}
	m.refreshProgressLocked(job.meta)
	_ = m.persist(job.meta)
	return cloneMeta(job.meta), true
}

func (m *Manager) Cancel(jobID string) error {
	m.mu.Lock()
	job := m.jobs[jobID]
	if job == nil {
		m.mu.Unlock()
		return fmt.Errorf("训练任务不存在")
	}
	if job.meta.Status != JobRunning && job.meta.Status != JobPending {
		m.mu.Unlock()
		return fmt.Errorf("训练任务不可取消")
	}
	if job.cmd != nil && job.cmd.Process != nil {
		_ = job.cmd.Process.Signal(syscall.SIGTERM)
	}
	if job.cancel != nil {
		job.cancel()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Logs(jobID, stream string, offset, tailBytes int64) (LogChunk, error) {
	m.mu.RLock()
	job := m.jobs[jobID]
	m.mu.RUnlock()
	if job == nil {
		return LogChunk{}, fmt.Errorf("训练任务不存在")
	}
	path := job.meta.StdoutPath
	if stream == "stderr" {
		path = job.meta.StderrPath
	} else {
		stream = "stdout"
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LogChunk{JobID: jobID, Stream: stream, Offset: offset, NextOffset: offset, EOF: true}, nil
		}
		return LogChunk{}, err
	}
	if tailBytes > 0 {
		offset = info.Size() - tailBytes
		if offset < 0 {
			offset = 0
		}
	} else if offset < 0 {
		offset = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return LogChunk{}, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return LogChunk{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return LogChunk{}, err
	}
	next := offset + int64(len(data))
	return LogChunk{JobID: jobID, Stream: stream, Offset: offset, NextOffset: next, Content: string(data), EOF: next >= info.Size()}, nil
}

func (m *Manager) Recover() error {
	entries, err := os.ReadDir(m.cfg.Layout.TrainJobs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(m.cfg.Layout.TrainJobs, entry.Name(), "job.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var meta JobMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if meta.Status == JobRunning || meta.Status == JobPending {
			meta.Status = JobInterrupted
			meta.EndedAt = m.now().UTC()
			meta.Error = "后端重启，训练进程状态未知"
			_ = m.persist(&meta)
		}
		m.jobs[meta.JobID] = &managedJob{meta: &meta}
	}
	return nil
}

func (m *Manager) runJob(parent context.Context, jobID string) {
	m.mu.Lock()
	job := m.jobs[jobID]
	if job == nil {
		m.mu.Unlock()
		return
	}
	meta := job.meta
	meta.Status = JobRunning
	meta.StartedAt = m.now().UTC()
	args, artifacts, err := m.buildCommand(meta)
	if err != nil {
		m.mu.Unlock()
		m.finish(jobID, JobFailed, err, -1)
		return
	}
	meta.Artifacts = artifacts
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, m.cfg.PythonBin, args...)
	stdout, err := os.Create(meta.StdoutPath)
	if err != nil {
		cancel()
		m.mu.Unlock()
		m.finish(jobID, JobFailed, err, -1)
		return
	}
	stderr, err := os.Create(meta.StderrPath)
	if err != nil {
		_ = stdout.Close()
		cancel()
		m.mu.Unlock()
		m.finish(jobID, JobFailed, err, -1)
		return
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	job.cmd = cmd
	job.cancel = cancel
	_ = m.persist(meta)
	m.mu.Unlock()

	err = cmd.Start()
	m.mu.Lock()
	if err == nil {
		meta.PID = cmd.Process.Pid
		_ = m.persist(meta)
	}
	m.mu.Unlock()
	if err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		m.finish(jobID, JobFailed, err, -1)
		return
	}
	waitErr := cmd.Wait()
	contextErr := ctx.Err()
	_ = stdout.Close()
	_ = stderr.Close()
	cancel()
	exitCode := 0
	if waitErr != nil {
		exitCode = -1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if contextErr != nil {
			m.finish(jobID, JobCancelled, nil, exitCode)
			return
		}
		m.finish(jobID, JobFailed, waitErr, exitCode)
		return
	}
	if err := m.writeCompletionSummary(jobID); err != nil {
		m.finish(jobID, JobFailed, err, exitCode)
		return
	}
	m.finish(jobID, JobCompleted, nil, exitCode)
}

func (m *Manager) buildCommand(meta *JobMetadata) ([]string, ModelArtifacts, error) {
	req := meta.Request
	modelID := sanitizeModelID(req.OutputModelName)
	if modelID == "" {
		modelID = strings.ToLower(req.Symbol) + "_" + req.Timeframe + "_" + meta.JobID
	}
	modelDir := filepath.Join(m.cfg.Layout.ModelsDRL, modelID)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return nil, ModelArtifacts{}, err
	}
	artifacts := ModelArtifacts{ModelID: modelID, ModelVersion: modelID}
	outputPath := filepath.Join(modelDir, "model.onnx")
	if req.Rolling {
		outputPath = filepath.Join(modelDir, "rolling_summary.json")
		artifacts.RollingSummaryPath = storage.RelForDisplay(m.cfg.Layout, outputPath)
	} else {
		artifacts.ONNXPath = storage.RelForDisplay(m.cfg.Layout, outputPath)
		artifacts.ZipPath = storage.RelForDisplay(m.cfg.Layout, strings.TrimSuffix(outputPath, filepath.Ext(outputPath))+".zip")
	}
	metadataPath := filepath.Join(modelDir, "metadata.json")
	artifacts.MetadataPath = storage.RelForDisplay(m.cfg.Layout, metadataPath)
	args := []string{
		m.cfg.TrainScript,
		"--data-path", m.cfg.Layout.HistoryDB,
		"--source", req.Source,
		"--symbol", req.Symbol,
		"--timeframe", req.Timeframe,
		"--start", req.Start,
		"--end", req.End,
		"--output", outputPath,
		"--progress-path", meta.ProgressPath,
		"--total-timesteps", fmt.Sprintf("%d", req.TotalTimesteps),
		"--observation-window", fmt.Sprintf("%d", req.ObservationWindow),
		"--initial-balance", fmt.Sprintf("%g", req.InitialBalance),
		"--taker-fee", fmt.Sprintf("%g", req.TakerFee),
		"--maker-fee", fmt.Sprintf("%g", req.MakerFee),
		"--slippage", fmt.Sprintf("%g", req.Slippage),
	}
	if req.NSteps > 0 {
		args = append(args, "--n-steps", fmt.Sprintf("%d", req.NSteps))
	}
	if req.BatchSize > 0 {
		args = append(args, "--batch-size", fmt.Sprintf("%d", req.BatchSize))
	}
	if req.NEpochs > 0 {
		args = append(args, "--n-epochs", fmt.Sprintf("%d", req.NEpochs))
	}
	if req.Rolling {
		args = append(args, "--rolling")
	}
	return args, artifacts, nil
}

func (m *Manager) finish(jobID string, status JobStatus, err error, exitCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[jobID]
	if job == nil {
		return
	}
	meta := job.meta
	if meta.Status == JobRunning || meta.Status == JobPending {
		m.running--
		if m.running < 0 {
			m.running = 0
		}
	}
	meta.Status = status
	meta.EndedAt = m.now().UTC()
	meta.PID = 0
	meta.ExitCode = exitCode
	if err != nil {
		meta.Error = err.Error()
	}
	m.refreshProgressLocked(meta)
	_ = m.persist(meta)
}

func (m *Manager) writeCompletionSummary(jobID string) error {
	m.mu.RLock()
	job := m.jobs[jobID]
	if job == nil {
		m.mu.RUnlock()
		return fmt.Errorf("训练任务不存在")
	}
	meta := cloneMeta(job.meta)
	m.mu.RUnlock()
	modelMeta := map[string]any{
		"model_id":           meta.Artifacts.ModelID,
		"model_version":      meta.Artifacts.ModelVersion,
		"symbol":             meta.Request.Symbol,
		"timeframe":          meta.Request.Timeframe,
		"source":             meta.Request.Source,
		"data_from":          meta.Request.Start,
		"data_to":            meta.Request.End,
		"total_timesteps":    meta.Request.TotalTimesteps,
		"observation_window": meta.Request.ObservationWindow,
		"created_at":         time.Now().UTC().Format(time.RFC3339),
		"zip_path":           meta.Artifacts.ZipPath,
		"onnx_path":          meta.Artifacts.ONNXPath,
		"deployable":         meta.Artifacts.ONNXPath != "",
		"runtime_note":       "当前默认DRL推理后端为stub，真实ONNX Runtime依赖build tag",
	}
	modelDir := filepath.Join(m.cfg.Layout.ModelsDRL, meta.Artifacts.ModelID)
	metadataPath := filepath.Join(modelDir, "metadata.json")
	if err := writeJSON(metadataPath, modelMeta); err != nil {
		return err
	}
	return writeJSON(meta.SummaryPath, meta)
}

func (m *Manager) persist(meta *JobMetadata) error {
	if meta == nil {
		return nil
	}
	return writeJSON(filepath.Join(meta.JobDir, "job.json"), meta)
}

func (m *Manager) refreshProgressLocked(meta *JobMetadata) {
	if meta == nil {
		return
	}
	progress := meta.Progress
	progress.TotalTimesteps = meta.Request.TotalTimesteps
	if meta.StartedAt.IsZero() {
		meta.Progress = progress
		return
	}
	end := m.now()
	if !meta.EndedAt.IsZero() {
		end = meta.EndedAt
	}
	progress.RuntimeSeconds = int64(end.Sub(meta.StartedAt).Seconds())
	if entry, ok := readLastProgress(meta.ProgressPath); ok {
		progress.Timesteps = entry.Timesteps
		if entry.TotalTimesteps > 0 {
			progress.TotalTimesteps = entry.TotalTimesteps
		}
		progress.LastLogAt = entry.Timestamp
	} else if ts := newestModTime(meta.StdoutPath, meta.StderrPath); !ts.IsZero() {
		progress.LastLogAt = ts
	}
	if meta.Status == JobRunning && !progress.LastLogAt.IsZero() {
		progress.Stalled = m.now().Sub(progress.LastLogAt) > 10*time.Minute
	}
	meta.Progress = progress
}

func (m *Manager) checkCoverage(ctx context.Context, req TrainRequest) error {
	store, err := historydb.Open(m.cfg.Layout.HistoryDB)
	if err != nil {
		return err
	}
	defer store.Close()
	loc, _ := time.LoadLocation(backtest.DefaultTimezone)
	from, err := backtest.ParseConfigTime(req.Start, loc)
	if err != nil {
		return err
	}
	to, err := backtest.ParseConfigTime(req.End, loc)
	if err != nil {
		return err
	}
	ok, detail, err := store.HasKlineCoverage(ctx, req.Source, req.Symbol, req.Timeframe, from, to)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s %s 训练数据覆盖不足: %s，请先补数据或显式允许不完整数据训练", req.Symbol, req.Timeframe, detail)
	}
	return nil
}

type progressEntry struct {
	Timesteps      int       `json:"timesteps"`
	TotalTimesteps int       `json:"total_timesteps"`
	Timestamp      time.Time `json:"timestamp"`
}

func readLastProgress(path string) (progressEntry, bool) {
	f, err := os.Open(path)
	if err != nil {
		return progressEntry{}, false
	}
	defer f.Close()
	var last progressEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry progressEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
			last = entry
		}
	}
	return last, !last.Timestamp.IsZero() || last.Timesteps > 0
}

func newestModTime(paths ...string) time.Time {
	var newest time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

func validateRequest(req TrainRequest) error {
	req = normalizeRequest(req)
	if !symbolPattern.MatchString(req.Symbol) {
		return fmt.Errorf("symbol格式无效: %s", req.Symbol)
	}
	switch req.Timeframe {
	case "3m", "15m", "1h", "4h":
	default:
		return fmt.Errorf("timeframe必须是3m、15m、1h或4h: %s", req.Timeframe)
	}
	loc, _ := time.LoadLocation(backtest.DefaultTimezone)
	from, err := backtest.ParseConfigTime(req.Start, loc)
	if err != nil {
		return fmt.Errorf("start无效: %w", err)
	}
	to, err := backtest.ParseConfigTime(req.End, loc)
	if err != nil {
		return fmt.Errorf("end无效: %w", err)
	}
	if !from.Before(to) {
		return fmt.Errorf("start必须早于end")
	}
	if req.TotalTimesteps < 1000 || req.TotalTimesteps > 50_000_000 {
		return fmt.Errorf("total_timesteps必须在[1000,50000000]范围内")
	}
	if req.ObservationWindow < 10 || req.ObservationWindow > 200 {
		return fmt.Errorf("observation_window必须在[10,200]范围内")
	}
	if req.InitialBalance <= 0 {
		return fmt.Errorf("initial_balance必须大于0")
	}
	if req.TakerFee < 0 || req.MakerFee < 0 || req.Slippage < 0 || req.TakerFee > 0.1 || req.MakerFee > 0.1 || req.Slippage > 0.1 {
		return fmt.Errorf("fee/slippage必须在[0,0.1]范围内")
	}
	if strings.TrimSpace(req.OutputModelName) != "" && !modelNamePattern.MatchString(req.OutputModelName) {
		return fmt.Errorf("output_model_name只能包含字母、数字、_、-、.")
	}
	return nil
}

func normalizeRequest(req TrainRequest) TrainRequest {
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	if req.Source == "" {
		req.Source = backtest.DefaultSource
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.Timeframe = strings.ToLower(strings.TrimSpace(req.Timeframe))
	if req.Timeframe == "" {
		req.Timeframe = "4h"
	}
	if req.ObservationWindow <= 0 {
		req.ObservationWindow = 60
	}
	if req.InitialBalance <= 0 {
		req.InitialBalance = 10_000
	}
	if req.TakerFee == 0 {
		req.TakerFee = 0.0005
	}
	if req.MakerFee == 0 {
		req.MakerFee = 0.0002
	}
	if req.Slippage == 0 {
		req.Slippage = 0.0003
	}
	return req
}

func sanitizeModelID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !modelNamePattern.MatchString(value) {
		return ""
	}
	return strings.TrimSuffix(value, filepath.Ext(value))
}

func cloneMeta(meta *JobMetadata) *JobMetadata {
	if meta == nil {
		return nil
	}
	out := *meta
	out.Artifacts.WindowModelPaths = append([]string(nil), meta.Artifacts.WindowModelPaths...)
	return &out
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
