package drl

import (
	"nofx/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleHotSwapSuccessArchivesOldModel(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	initialBackend := NewStubBackend(0)
	engine, err := NewEngineWithBackend(cfg, initialBackend)
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.archiveDir = t.TempDir()
	manager.backendFactory = func() InferenceBackend { return NewStubBackend(0) }

	candidatePath := writeTempModel(t)
	if err := manager.HotSwap(candidatePath); err != nil {
		t.Fatalf("热更新应成功: %v", err)
	}
	if !initialBackend.Closed {
		t.Fatalf("热更新成功后应关闭旧后端")
	}
	status := engine.Status()
	if status.ModelPath != candidatePath || status.ModelVersion != modelVersionFromPath(candidatePath) {
		t.Fatalf("热更新后状态未更新: %+v", status)
	}
	entries, err := os.ReadDir(manager.archiveDir)
	if err != nil {
		t.Fatalf("读取归档目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("热更新成功后应归档旧模型")
	}
	backend := manager.GetBackendForInference()
	if backend == nil {
		t.Fatalf("热更新后活跃后端不能为空")
	}
}

func TestLifecycleHotSwapRejectsCandidateAndDeletesFile(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	initialBackend := NewStubBackend(0)
	engine, err := NewEngineWithBackend(cfg, initialBackend)
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.archiveDir = t.TempDir()
	var candidateBackend *StubBackend
	manager.backendFactory = func() InferenceBackend {
		candidateBackend = NewStubBackend(1)
		return candidateBackend
	}

	candidatePath := writeTempModel(t)
	err = manager.HotSwap(candidatePath)
	if err == nil || !strings.Contains(err.Error(), "候选模型验证不通过") {
		t.Fatalf("验证失败应返回清晰错误: %v", err)
	}
	if candidateBackend == nil || !candidateBackend.Closed {
		t.Fatalf("验证失败应关闭候选后端")
	}
	if initialBackend.Closed {
		t.Fatalf("验证失败不应关闭当前后端")
	}
	if _, statErr := os.Stat(candidatePath); !os.IsNotExist(statErr) {
		t.Fatalf("验证失败应删除候选模型，statErr=%v", statErr)
	}
	if manager.ModelPath() != modelPath {
		t.Fatalf("验证失败应保留当前模型: %s", manager.ModelPath())
	}
}

func TestLifecycleValidateCandidateUsesDirectionalSamples(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	engine, err := NewEngineWithBackend(cfg, NewStubBackend(0))
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.backendFactory = func() InferenceBackend { return NewStubBackend(1) }
	manager.validationDataProvider = func(cfg DRLEngineConfig) ([]ValidationSample, error) {
		return []ValidationSample{{
			Symbol:            "BTCUSDT",
			Klines:            makeTestKlines(cfg.ObservationWindow + 1),
			ExpectedDirection: 1,
		}}, nil
	}

	result, err := manager.ValidateCandidate(writeTempModel(t))
	if err != nil {
		t.Fatalf("有方向样本时验证应成功: %v", err)
	}
	if !result.Passed || result.ValidationPeriod != "recent_market_window" || result.DirectionalAccuracy != 1 {
		t.Fatalf("方向样本DA计算异常: %+v", result)
	}
}

func TestLifecycleRollbackUsesLatestArchive(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	initialBackend := NewStubBackend(0)
	engine, err := NewEngineWithBackend(cfg, initialBackend)
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.archiveDir = t.TempDir()
	manager.backendFactory = func() InferenceBackend { return NewStubBackend(0) }

	oldArchive := writeArchiveModel(t, manager.archiveDir, "archive_old.onnx", time.Now().Add(-time.Hour))
	newArchive := writeArchiveModel(t, manager.archiveDir, "archive_new.onnx", time.Now())
	if oldArchive == newArchive {
		t.Fatalf("测试归档fixture异常")
	}
	if err := manager.Rollback("测试回滚"); err != nil {
		t.Fatalf("回滚到归档模型应成功: %v", err)
	}
	if manager.ModelPath() != newArchive {
		t.Fatalf("应选择最近归档模型: got=%s want=%s", manager.ModelPath(), newArchive)
	}
	if manager.ModelVersion() != "archive_new" {
		t.Fatalf("回滚版本标识异常: %s", manager.ModelVersion())
	}
	if !initialBackend.Closed {
		t.Fatalf("回滚成功后应关闭旧后端")
	}
}

func TestLifecycleRollbackFallsBackToWaitMode(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	engine, err := NewEngineWithBackend(cfg, NewStubBackend(0))
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.archiveDir = t.TempDir()

	err = manager.Rollback("无归档")
	if err == nil || !strings.Contains(err.Error(), "已降级为纯wait模式") {
		t.Fatalf("无归档时应返回降级错误: %v", err)
	}
	if manager.ModelPath() != waitModeModelPath || manager.ModelVersion() != waitModeModelVersion {
		t.Fatalf("无归档时应切到wait模式: path=%s version=%s", manager.ModelPath(), manager.ModelVersion())
	}
	rawAction, inferErr := manager.Infer(make([]float32, engine.Config.ObservationDimension()))
	if inferErr != nil || rawAction != 0 {
		t.Fatalf("wait模式应稳定返回0: raw=%v err=%v", rawAction, inferErr)
	}
}

func TestLifecycleConcurrentInferenceDuringHotSwap(t *testing.T) {
	modelPath := writeTempModel(t)
	cfg := lifecycleTestConfig(modelPath)
	engine, err := NewEngineWithBackend(cfg, NewStubBackend(0))
	if err != nil {
		t.Fatalf("创建DRL engine失败: %v", err)
	}
	manager := NewModelLifecycleManager(engine, &engine.Config)
	manager.archiveDir = t.TempDir()
	manager.backendFactory = func() InferenceBackend { return NewStubBackend(0) }
	observation := make([]float32, engine.Config.ObservationDimension())

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					if _, err := manager.Infer(observation); err != nil {
						t.Errorf("并发推理不应失败: %v", err)
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if err := manager.HotSwap(writeTempModel(t)); err != nil {
			t.Fatalf("并发热更新应成功: %v", err)
		}
	}
	close(stopCh)
	wg.Wait()
}

func TestRetrainSchedulerRunOnceTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "train.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatalf("写训练脚本fixture失败: %v", err)
	}
	manager := &ModelLifecycleManager{
		config: &DRLEngineConfig{
			ObservationWindow: 10,
			ActionThreshold:   0.1,
			InputShape:        []int64{1, 163},
		},
	}
	scheduler := NewRetrainScheduler(manager, 1, scriptPath, tmpDir)
	scheduler.PythonBin = "/bin/sh"
	scheduler.Timeout = 10 * time.Millisecond

	err := scheduler.RunOnce()
	if err == nil || !strings.Contains(err.Error(), "DRL 自动重训练超时（30分钟）") {
		t.Fatalf("训练超时应返回中文错误: %v", err)
	}
}

func lifecycleTestConfig(modelPath string) config.DRLStrategyConfig {
	return config.DRLStrategyConfig{
		ModelPath:         modelPath,
		ModelVersion:      "lifecycle-v1",
		ObservationWindow: 10,
		Timeframe:         "4h",
		Symbols:           []string{"BTCUSDT"},
		ActionThreshold:   0.1,
		MaxPositionPct:    0.3,
		DefaultLeverage:   5,
		StopLossATRMult:   2,
		TakeProfitATRMult: 3,
		ValidationMinDA:   0.55,
	}
}

func writeArchiveModel(t *testing.T, dir, name string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatalf("写归档模型fixture失败: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("设置归档模型时间失败: %v", err)
	}
	return path
}
