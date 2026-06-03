package drl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nofx/decision"
	"nofx/market"
)

const (
	defaultRetrainIntervalHours = 168
	defaultValidationMinDA      = 0.55
	defaultTrainScriptPath      = "training/drl/scripts/train.py"
	defaultModelOutputDir       = "models/drl"
	defaultModelArchiveDir      = "models/drl/archive"
	defaultRetrainTimeout       = 30 * time.Minute
	waitModeModelPath           = "drl-wait-mode"
	waitModeModelVersion        = "wait_mode"
)

// BackendFactory 创建DRL推理后端。默认实现使用StubBackend，不依赖ONNX Runtime。
type BackendFactory func() InferenceBackend

// ValidationDataProvider 提供最近验证窗口样本，用于候选模型方向准确性验证。
type ValidationDataProvider func(cfg DRLEngineConfig) ([]ValidationSample, error)

// ValidationSample 表示一个方向准确性验证样本。
type ValidationSample struct {
	Symbol            string
	Klines            []market.Kline
	Account           decision.AccountInfo
	Position          *decision.PositionInfo
	ExpectedDirection int
}

// ModelLifecycleManager 管理DRL模型加载、验证、热更新与回滚。
type ModelLifecycleManager struct {
	engine         *Engine
	config         *DRLEngineConfig
	scheduler      *RetrainScheduler
	currentBackend InferenceBackend
	modelVersion   string
	modelPath      string
	lastRetrainAt  time.Time
	lastValidateAt time.Time
	mu             sync.RWMutex

	backendFactory         BackendFactory
	validationDataProvider ValidationDataProvider
	archiveDir             string
	clock                  func() time.Time
}

// RetrainScheduler 定时触发DRL模型重训练并把候选模型交给生命周期管理器验证。
type RetrainScheduler struct {
	IntervalHours   int
	TrainScriptPath string
	DataPath        string
	OutputDir       string
	PythonBin       string
	Timeout         time.Duration

	manager *ModelLifecycleManager
	ticker  *time.Ticker
	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// ModelValidationResult 表示候选模型验证结果。
type ModelValidationResult struct {
	Passed              bool    `json:"passed"`
	DirectionalAccuracy float64 `json:"directional_accuracy"`
	MinRequired         float64 `json:"min_required"`
	SampleCount         int     `json:"sample_count"`
	ValidationPeriod    string  `json:"validation_period"`
	Reason              string  `json:"reason,omitempty"`
}

func NewModelLifecycleManager(engine *Engine, cfg *DRLEngineConfig) *ModelLifecycleManager {
	if cfg == nil && engine != nil {
		cfg = &engine.Config
	}
	cfgCopy := DRLEngineConfig{}
	if cfg != nil {
		cfgCopy = *cfg
	}
	if cfgCopy.RetrainIntervalH <= 0 {
		cfgCopy.RetrainIntervalH = defaultRetrainIntervalHours
	}
	if cfgCopy.ValidationMinDA <= 0 {
		cfgCopy.ValidationMinDA = defaultValidationMinDA
	}
	factory := BackendFactory(func() InferenceBackend { return NewStubBackend(0) })
	if engine != nil && engine.BackendFactory != nil {
		factory = engine.BackendFactory
	}
	manager := &ModelLifecycleManager{
		engine:         engine,
		config:         &cfgCopy,
		currentBackend: nil,
		modelVersion:   cfgCopy.ModelVersion,
		modelPath:      cfgCopy.ModelPath,
		backendFactory: factory,
		archiveDir:     defaultModelArchiveDir,
	}
	if engine != nil {
		manager.currentBackend = engine.Backend
		manager.validationDataProvider = engine.ValidationDataProvider
	}
	manager.scheduler = NewRetrainScheduler(manager, cfgCopy.RetrainIntervalH, defaultTrainScriptPath, defaultModelOutputDir)
	return manager
}

func NewRetrainScheduler(manager *ModelLifecycleManager, intervalHours int, trainScriptPath, outputDir string) *RetrainScheduler {
	if intervalHours <= 0 {
		intervalHours = defaultRetrainIntervalHours
	}
	if strings.TrimSpace(trainScriptPath) == "" {
		trainScriptPath = defaultTrainScriptPath
	}
	if strings.TrimSpace(outputDir) == "" {
		outputDir = defaultModelOutputDir
	}
	return &RetrainScheduler{
		IntervalHours:   intervalHours,
		TrainScriptPath: trainScriptPath,
		OutputDir:       outputDir,
		PythonBin:       "python3",
		Timeout:         defaultRetrainTimeout,
		manager:         manager,
	}
}

func (m *ModelLifecycleManager) GetBackendForInference() InferenceBackend {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentBackend
}

func (m *ModelLifecycleManager) Infer(observation []float32) (float32, error) {
	if m == nil {
		return 0, fmt.Errorf("DRL生命周期管理器未初始化")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentBackend == nil {
		return 0, fmt.Errorf("DRL推理后端未初始化")
	}
	return m.currentBackend.Infer(observation)
}

func (m *ModelLifecycleManager) ModelVersion() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modelVersion
}

func (m *ModelLifecycleManager) ModelPath() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modelPath
}

func (m *ModelLifecycleManager) Close() error {
	if m == nil {
		return nil
	}
	if m.scheduler != nil {
		m.scheduler.Stop()
	}
	m.mu.Lock()
	backend := m.currentBackend
	m.currentBackend = nil
	m.mu.Unlock()
	if backend != nil {
		return backend.Close()
	}
	return nil
}

func (m *ModelLifecycleManager) StartScheduler() {
	if m == nil || m.scheduler == nil {
		return
	}
	m.scheduler.Start()
}

func (m *ModelLifecycleManager) ValidateCandidate(candidatePath string) (*ModelValidationResult, error) {
	if m == nil {
		return nil, fmt.Errorf("DRL生命周期管理器未初始化")
	}
	backend := m.newBackend()
	if backend == nil {
		return nil, fmt.Errorf("DRL候选模型后端为空")
	}
	if err := backend.Load(candidatePath, m.inputShape()); err != nil {
		_ = backend.Close()
		return &ModelValidationResult{
			Passed:      false,
			MinRequired: m.validationMinDA(),
			Reason:      fmt.Sprintf("候选模型加载失败: %v", err),
		}, fmt.Errorf("候选模型加载失败: %w", err)
	}
	defer backend.Close()
	result, err := m.validateLoadedCandidate(candidatePath, backend)
	m.setLastValidateAt(m.now())
	return result, err
}

func (m *ModelLifecycleManager) HotSwap(candidatePath string) error {
	if m == nil {
		return fmt.Errorf("DRL生命周期管理器未初始化")
	}
	candidatePath = strings.TrimSpace(candidatePath)
	if candidatePath == "" {
		return fmt.Errorf("候选模型路径不能为空")
	}
	candidateBackend := m.newBackend()
	if candidateBackend == nil {
		return fmt.Errorf("DRL候选模型后端为空")
	}
	if err := candidateBackend.Load(candidatePath, m.inputShape()); err != nil {
		_ = candidateBackend.Close()
		_ = os.Remove(candidatePath)
		log.Printf("候选模型加载失败: %v", err)
		return fmt.Errorf("候选模型加载失败: %w", err)
	}

	result, err := m.validateLoadedCandidate(candidatePath, candidateBackend)
	if err != nil {
		_ = candidateBackend.Close()
		_ = os.Remove(candidatePath)
		return err
	}
	m.setLastValidateAt(m.now())
	if !result.Passed {
		_ = candidateBackend.Close()
		_ = os.Remove(candidatePath)
		log.Printf("候选模型验证不通过: DA=%.4f < 阈值%.4f, 保留当前模型", result.DirectionalAccuracy, result.MinRequired)
		return fmt.Errorf("候选模型验证不通过: DA=%.4f < 阈值%.4f", result.DirectionalAccuracy, result.MinRequired)
	}

	newVersion := modelVersionFromPath(candidatePath)
	if newVersion == "" {
		newVersion = m.now().UTC().Format("20060102T150405Z")
	}

	m.mu.Lock()
	oldBackend := m.currentBackend
	oldModelPath := m.modelPath
	oldVersion := m.modelVersion
	m.currentBackend = candidateBackend
	m.modelPath = candidatePath
	m.modelVersion = newVersion
	m.mu.Unlock()

	if m.engine != nil && m.engine.Diagnostics != nil {
		m.engine.Diagnostics.SetModel(candidatePath, newVersion)
	}
	if oldBackend != nil && oldBackend != candidateBackend {
		_ = oldBackend.Close()
	}
	if err := m.archiveModel(oldModelPath, oldVersion); err != nil {
		log.Printf("DRL旧模型归档失败: %v", err)
	}
	log.Printf("模型热更新成功: v%s → v%s, DA=%.4f", emptyAs(oldVersion, "unknown"), newVersion, result.DirectionalAccuracy)
	return nil
}

func (m *ModelLifecycleManager) Rollback(reason string) error {
	if m == nil {
		return fmt.Errorf("DRL生命周期管理器未初始化")
	}
	archivePath, archiveVersion, err := m.latestArchivedModel()
	if err != nil {
		if fallbackErr := m.activateWaitMode(reason); fallbackErr != nil {
			return fmt.Errorf("DRL模型回滚失败且降级wait失败: %w", fallbackErr)
		}
		log.Printf("严重错误: DRL模型回滚失败，已降级为纯wait模式: %v", err)
		return fmt.Errorf("DRL模型回滚失败，已降级为纯wait模式: %w", err)
	}

	backend := m.newBackend()
	if backend == nil {
		_ = m.activateWaitMode(reason)
		return fmt.Errorf("DRL回滚后端为空，已降级为纯wait模式")
	}
	if err := backend.Load(archivePath, m.inputShape()); err != nil {
		_ = backend.Close()
		_ = m.activateWaitMode(reason)
		return fmt.Errorf("加载归档模型失败，已降级为纯wait模式: %w", err)
	}

	m.mu.Lock()
	oldBackend := m.currentBackend
	m.currentBackend = backend
	m.modelPath = archivePath
	m.modelVersion = archiveVersion
	m.mu.Unlock()

	if m.engine != nil && m.engine.Diagnostics != nil {
		m.engine.Diagnostics.SetModel(archivePath, archiveVersion)
	}
	if oldBackend != nil && oldBackend != backend {
		_ = oldBackend.Close()
	}
	log.Printf("模型已回滚: 原因=%s, 恢复到 v%s", strings.TrimSpace(reason), archiveVersion)
	return nil
}

func (m *ModelLifecycleManager) validateLoadedCandidate(candidatePath string, backend InferenceBackend) (*ModelValidationResult, error) {
	samples, sampleErr := m.validationSamples()
	if sampleErr != nil {
		return &ModelValidationResult{
			Passed:      false,
			MinRequired: m.validationMinDA(),
			Reason:      fmt.Sprintf("读取验证窗口失败: %v", sampleErr),
		}, fmt.Errorf("读取验证窗口失败: %w", sampleErr)
	}
	if len(samples) > 0 {
		return m.validateLoadedCandidateWithSamples(candidatePath, backend, samples)
	}
	return m.validateLoadedCandidateNeutral(candidatePath, backend)
}

func (m *ModelLifecycleManager) validateLoadedCandidateWithSamples(candidatePath string, backend InferenceBackend, samples []ValidationSample) (*ModelValidationResult, error) {
	result := &ModelValidationResult{
		MinRequired:      m.validationMinDA(),
		ValidationPeriod: "recent_market_window",
	}
	if strings.TrimSpace(candidatePath) == "" {
		result.Reason = "候选模型路径为空"
		return result, errors.New(result.Reason)
	}
	if _, err := os.Stat(candidatePath); err != nil {
		result.Reason = fmt.Sprintf("候选模型文件不可用: %v", err)
		return result, errors.New(result.Reason)
	}
	if backend == nil {
		result.Reason = "候选模型后端为空"
		return result, errors.New(result.Reason)
	}
	if m == nil || m.config == nil {
		result.Reason = "DRL验证配置为空"
		return result, errors.New(result.Reason)
	}
	builder := NewFeatureBuilder(*m.config)
	correct := 0
	total := 0
	for _, sample := range samples {
		expected := sample.ExpectedDirection
		if expected == 0 {
			expected = expectedDirectionFromKlines(sample.Klines)
		}
		if expected == 0 {
			continue
		}
		account := sample.Account
		if account.TotalEquity <= 0 {
			account.TotalEquity = 10_000
			account.AvailableBalance = 10_000
			account.SizingEquity = 10_000
		}
		observation, err := builder.Build(sample.Klines, account, sample.Position)
		if err != nil {
			result.Reason = fmt.Sprintf("候选模型验证特征构建失败: %v", err)
			return result, errors.New(result.Reason)
		}
		rawAction, err := backend.Infer(observation)
		if err != nil {
			result.Reason = fmt.Sprintf("候选模型验证推理失败: %v", err)
			return result, errors.New(result.Reason)
		}
		predicted := predictedDirection(rawAction, float32(m.actionThreshold()))
		total++
		if predicted == expected {
			correct++
		}
	}
	result.SampleCount = total
	if total == 0 {
		result.Reason = "验证窗口无有效方向样本"
		return result, errors.New(result.Reason)
	}
	result.DirectionalAccuracy = float64(correct) / float64(total)
	result.Passed = result.DirectionalAccuracy >= result.MinRequired
	if !result.Passed {
		result.Reason = fmt.Sprintf("DA=%.4f < 阈值%.4f", result.DirectionalAccuracy, result.MinRequired)
	}
	return result, nil
}

func (m *ModelLifecycleManager) validateLoadedCandidateNeutral(candidatePath string, backend InferenceBackend) (*ModelValidationResult, error) {
	result := &ModelValidationResult{
		MinRequired:      m.validationMinDA(),
		ValidationPeriod: "neutral_health_check",
		SampleCount:      10,
	}
	if strings.TrimSpace(candidatePath) == "" {
		result.Reason = "候选模型路径为空"
		return result, errors.New(result.Reason)
	}
	if _, err := os.Stat(candidatePath); err != nil {
		result.Reason = fmt.Sprintf("候选模型文件不可用: %v", err)
		return result, errors.New(result.Reason)
	}
	if backend == nil {
		result.Reason = "候选模型后端为空"
		return result, errors.New(result.Reason)
	}

	dim := m.observationDimension()
	if dim <= 0 {
		result.Reason = "DRL观测维度无效"
		return result, errors.New(result.Reason)
	}
	threshold := float32(m.actionThreshold())
	correct := 0
	for i := 0; i < result.SampleCount; i++ {
		observation := make([]float32, dim)
		rawAction, err := backend.Infer(observation)
		if err != nil {
			result.Reason = fmt.Sprintf("候选模型验证推理失败: %v", err)
			return result, errors.New(result.Reason)
		}
		if float32(math.Abs(float64(rawAction))) <= threshold {
			correct++
		}
	}
	result.DirectionalAccuracy = float64(correct) / float64(result.SampleCount)
	result.Passed = result.DirectionalAccuracy >= result.MinRequired
	if !result.Passed {
		result.Reason = fmt.Sprintf("DA=%.4f < 阈值%.4f", result.DirectionalAccuracy, result.MinRequired)
	}
	return result, nil
}

func (m *ModelLifecycleManager) validationSamples() ([]ValidationSample, error) {
	if m == nil || m.config == nil {
		return nil, nil
	}
	if m.validationDataProvider != nil {
		return m.validationDataProvider(*m.config)
	}
	if m.engine == nil || m.engine.MarketDataProvider == nil || len(m.config.Symbols) == 0 {
		return nil, nil
	}
	opts := decision.CyclePreparationOptions{
		MarketHistoryDepth: m.config.MarketHistoryDepth(),
		ClosedKlinesOnly:   true,
		MarketDataProvider: m.engine.MarketDataProvider,
		Clock:              m.engine.now,
	}
	var samples []ValidationSample
	for _, symbol := range m.config.Symbols {
		data, err := m.engine.MarketDataProvider(symbol, opts)
		if err != nil {
			return nil, err
		}
		if data == nil {
			continue
		}
		samples = append(samples, validationSamplesFromKlines(symbol, data.Klines[m.config.Timeframe], *m.config)...)
	}
	return samples, nil
}

func validationSamplesFromKlines(symbol string, klines []market.Kline, cfg DRLEngineConfig) []ValidationSample {
	if len(klines) < cfg.ObservationWindow+2 || cfg.ObservationWindow <= 0 {
		return nil
	}
	maxSamples := 50
	start := cfg.ObservationWindow
	if len(klines)-1-start > maxSamples {
		start = len(klines) - 1 - maxSamples
	}
	samples := make([]ValidationSample, 0, len(klines)-1-start)
	for end := start; end < len(klines)-1; end++ {
		expected := directionFromDelta(klines[end+1].Close - klines[end].Close)
		if expected == 0 {
			continue
		}
		samples = append(samples, ValidationSample{
			Symbol:            market.Normalize(symbol),
			Klines:            append([]market.Kline(nil), klines[:end+1]...),
			ExpectedDirection: expected,
		})
	}
	return samples
}

func expectedDirectionFromKlines(klines []market.Kline) int {
	if len(klines) < 2 {
		return 0
	}
	last := klines[len(klines)-1]
	prev := klines[len(klines)-2]
	return directionFromDelta(last.Close - prev.Close)
}

func directionFromDelta(delta float64) int {
	switch {
	case delta > 0:
		return 1
	case delta < 0:
		return -1
	default:
		return 0
	}
}

func predictedDirection(rawAction float32, threshold float32) int {
	switch {
	case rawAction > threshold:
		return 1
	case rawAction < -threshold:
		return -1
	default:
		return 0
	}
}

func (m *ModelLifecycleManager) archiveModel(modelPath, version string) error {
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" || modelPath == waitModeModelPath {
		return nil
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("旧模型路径是目录: %s", modelPath)
	}
	archiveDir := m.archiveDirectory()
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	ext := filepath.Ext(modelPath)
	if ext == "" {
		ext = ".onnx"
	}
	base := strings.TrimSuffix(filepath.Base(modelPath), filepath.Ext(modelPath))
	version = sanitizePathPart(emptyAs(version, "unknown"))
	name := fmt.Sprintf("%s_%s_%s%s", sanitizePathPart(base), version, m.now().UTC().Format("20060102T150405.000000000Z"), ext)
	return copyFile(modelPath, filepath.Join(archiveDir, name))
}

func (m *ModelLifecycleManager) latestArchivedModel() (string, string, error) {
	archiveDir := m.archiveDirectory()
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return "", "", err
	}
	type candidate struct {
		path    string
		version string
		modTime time.Time
	}
	candidates := []candidate{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".onnx" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(archiveDir, name)
		candidates = append(candidates, candidate{
			path:    path,
			version: modelVersionFromPath(path),
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("归档目录无可用DRL模型: %s", archiveDir)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	version := candidates[0].version
	if version == "" {
		version = candidates[0].modTime.UTC().Format("20060102T150405Z")
	}
	return candidates[0].path, version, nil
}

func (m *ModelLifecycleManager) activateWaitMode(reason string) error {
	waitBackend := NewStubBackend(0)
	waitBackend.RequireModelFile = false
	if err := waitBackend.Load(waitModeModelPath, m.inputShape()); err != nil {
		return err
	}
	m.mu.Lock()
	oldBackend := m.currentBackend
	m.currentBackend = waitBackend
	m.modelPath = waitModeModelPath
	m.modelVersion = waitModeModelVersion
	m.mu.Unlock()
	if m.engine != nil && m.engine.Diagnostics != nil {
		m.engine.Diagnostics.SetModel(waitModeModelPath, waitModeModelVersion)
	}
	if oldBackend != nil && oldBackend != waitBackend {
		_ = oldBackend.Close()
	}
	log.Printf("严重错误: DRL模型无可回滚版本，已降级为纯wait模式: 原因=%s", strings.TrimSpace(reason))
	return nil
}

func (m *ModelLifecycleManager) newBackend() InferenceBackend {
	if m != nil && m.backendFactory != nil {
		return m.backendFactory()
	}
	return NewStubBackend(0)
}

func (m *ModelLifecycleManager) inputShape() []int64 {
	if m == nil || m.config == nil {
		return nil
	}
	return append([]int64(nil), m.config.InputShape...)
}

func (m *ModelLifecycleManager) observationDimension() int {
	if m == nil || m.config == nil {
		return 0
	}
	return m.config.ObservationDimension()
}

func (m *ModelLifecycleManager) validationMinDA() float64 {
	if m == nil || m.config == nil || m.config.ValidationMinDA <= 0 {
		return defaultValidationMinDA
	}
	return m.config.ValidationMinDA
}

func (m *ModelLifecycleManager) actionThreshold() float64 {
	if m == nil || m.config == nil || m.config.ActionThreshold <= 0 {
		return 0.1
	}
	return m.config.ActionThreshold
}

func (m *ModelLifecycleManager) archiveDirectory() string {
	if m == nil || strings.TrimSpace(m.archiveDir) == "" {
		return defaultModelArchiveDir
	}
	return m.archiveDir
}

func (m *ModelLifecycleManager) now() time.Time {
	if m != nil && m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

func (m *ModelLifecycleManager) setLastValidateAt(ts time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.lastValidateAt = ts
	m.mu.Unlock()
}

func (m *ModelLifecycleManager) setLastRetrainAt(ts time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.lastRetrainAt = ts
	m.mu.Unlock()
}

func (s *RetrainScheduler) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	interval := time.Duration(s.IntervalHours) * time.Hour
	if interval <= 0 {
		interval = time.Duration(defaultRetrainIntervalHours) * time.Hour
	}
	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.running = true
	stopCh := s.stopCh
	doneCh := s.doneCh
	ticker := s.ticker
	s.mu.Unlock()

	go func() {
		defer close(doneCh)
		for {
			select {
			case <-ticker.C:
				if err := s.RunOnce(); err != nil {
					log.Printf("DRL 自动重训练失败: %v", err)
				}
			case <-stopCh:
				return
			}
		}
	}()
}

func (s *RetrainScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.running = false
	s.stopCh = nil
	s.doneCh = nil
	s.ticker = nil
	close(stopCh)
	s.mu.Unlock()
	<-doneCh
}

func (s *RetrainScheduler) RunOnce() error {
	if s == nil {
		return fmt.Errorf("DRL重训练调度器未初始化")
	}
	if s.manager == nil {
		return fmt.Errorf("DRL重训练调度器缺少生命周期管理器")
	}
	outputDir := strings.TrimSpace(s.OutputDir)
	if outputDir == "" {
		outputDir = defaultModelOutputDir
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("创建DRL模型输出目录失败: %w", err)
	}
	candidatePath := filepath.Join(outputDir, fmt.Sprintf("candidate_%s.onnx", time.Now().UTC().Format("20060102T150405Z")))
	args := []string{strings.TrimSpace(s.TrainScriptPath)}
	if args[0] == "" {
		args[0] = defaultTrainScriptPath
	}
	if strings.TrimSpace(s.DataPath) != "" {
		args = append(args, "--data-path", strings.TrimSpace(s.DataPath))
	}
	if symbol := s.firstSymbol(); symbol != "" {
		args = append(args, "--symbol", symbol)
	}
	args = append(args, "--output", candidatePath)

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultRetrainTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pythonBin := strings.TrimSpace(s.PythonBin)
	if pythonBin == "" {
		pythonBin = "python3"
	}
	cmd := exec.CommandContext(ctx, pythonBin, args...)
	output, err := cmd.CombinedOutput()
	s.manager.setLastRetrainAt(time.Now())
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("DRL 自动重训练超时（30分钟）")
		return fmt.Errorf("DRL 自动重训练超时（30分钟）")
	}
	if err != nil {
		log.Printf("DRL 自动重训练失败: %v, output=%s", err, strings.TrimSpace(string(output)))
		return fmt.Errorf("DRL 自动重训练失败: %w", err)
	}
	if err := s.manager.HotSwap(candidatePath); err != nil {
		log.Printf("DRL候选模型热更新失败: %v", err)
		return err
	}
	return nil
}

func (s *RetrainScheduler) firstSymbol() string {
	if s == nil || s.manager == nil || s.manager.config == nil {
		return ""
	}
	for _, symbol := range s.manager.config.Symbols {
		if strings.TrimSpace(symbol) != "" {
			return strings.TrimSpace(symbol)
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func modelVersionFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return base
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
