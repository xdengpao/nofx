package drl

import (
	"sync"
	"time"
)

type DiagnosticsCollector struct {
	mu                 sync.RWMutex
	modelPath          string
	modelVersion       string
	lastInferenceAt    time.Time
	lastInferenceTime  time.Duration
	inferenceCount     int64
	totalInferenceTime time.Duration
	maxInferenceTime   time.Duration
	lastRawAction      float32
	lastMappedAction   string
	lastSymbol         string
	lastFeatureStats   FeatureStats
	lastRawFeatures    []float64
	lastFeatures       []float32
	lastError          string
}

type Status struct {
	ModelPath              string    `json:"model_path"`
	ModelVersion           string    `json:"model_version"`
	LastInferenceAt        time.Time `json:"last_inference_at,omitempty"`
	InferenceCount         int64     `json:"inference_count"`
	AverageInferenceTimeMS float64   `json:"average_inference_time_ms"`
	MaxInferenceTimeMS     float64   `json:"max_inference_time_ms"`
	LastRawAction          float32   `json:"last_raw_action,omitempty"`
	LastMappedAction       string    `json:"last_mapped_action,omitempty"`
	LastSymbol             string    `json:"last_symbol,omitempty"`
	LastError              string    `json:"last_error,omitempty"`
}

func NewDiagnosticsCollector(cfg DRLEngineConfig) *DiagnosticsCollector {
	return &DiagnosticsCollector{
		modelPath:    cfg.ModelPath,
		modelVersion: cfg.ModelVersion,
	}
}

func (d *DiagnosticsCollector) SetModel(modelPath, modelVersion string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.modelPath = modelPath
	d.modelVersion = modelVersion
}

func (d *DiagnosticsCollector) Record(symbol string, rawFeatures []float64, normalizedFeatures []float32, stats FeatureStats, rawAction float32, mappedAction string, elapsed time.Duration, err error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastInferenceAt = time.Now()
	d.lastInferenceTime = elapsed
	d.inferenceCount++
	d.totalInferenceTime += elapsed
	if elapsed > d.maxInferenceTime {
		d.maxInferenceTime = elapsed
	}
	d.lastRawAction = rawAction
	d.lastMappedAction = mappedAction
	d.lastSymbol = symbol
	d.lastFeatureStats = stats
	d.lastRawFeatures = append(d.lastRawFeatures[:0], rawFeatures...)
	d.lastFeatures = append(d.lastFeatures[:0], normalizedFeatures...)
	if err != nil {
		d.lastError = err.Error()
	} else {
		d.lastError = ""
	}
}

func (d *DiagnosticsCollector) Status() Status {
	if d == nil {
		return Status{}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	avg := 0.0
	if d.inferenceCount > 0 {
		avg = float64(d.totalInferenceTime.Microseconds()) / 1000.0 / float64(d.inferenceCount)
	}
	return Status{
		ModelPath:              d.modelPath,
		ModelVersion:           d.modelVersion,
		LastInferenceAt:        d.lastInferenceAt,
		InferenceCount:         d.inferenceCount,
		AverageInferenceTimeMS: avg,
		MaxInferenceTimeMS:     float64(d.maxInferenceTime.Microseconds()) / 1000.0,
		LastRawAction:          d.lastRawAction,
		LastMappedAction:       d.lastMappedAction,
		LastSymbol:             d.lastSymbol,
		LastError:              d.lastError,
	}
}

func (d *DiagnosticsCollector) FeatureSnapshot() map[string]any {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]any{
		"symbol":              d.lastSymbol,
		"stats":               d.lastFeatureStats,
		"dimension":           len(d.lastFeatures),
		"raw_dimension":       len(d.lastRawFeatures),
		"raw_features":        append([]float64(nil), d.lastRawFeatures...),
		"normalized_features": append([]float32(nil), d.lastFeatures...),
		"last_raw":            d.lastRawAction,
		"mapped_action":       d.lastMappedAction,
	}
}

func (d *DiagnosticsCollector) StrategyDiagnostics() map[string]any {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	avg := 0.0
	if d.inferenceCount > 0 {
		avg = float64(d.totalInferenceTime.Microseconds()) / 1000.0 / float64(d.inferenceCount)
	}
	return map[string]any{
		"model_path":                d.modelPath,
		"model_version":             d.modelVersion,
		"last_symbol":               d.lastSymbol,
		"raw_action":                d.lastRawAction,
		"mapped_action":             d.lastMappedAction,
		"observation_summary":       d.lastFeatureStats,
		"inference_time_ms":         float64(d.lastInferenceTime.Microseconds()) / 1000.0,
		"inference_count":           d.inferenceCount,
		"average_inference_time_ms": avg,
		"max_inference_time_ms":     float64(d.maxInferenceTime.Microseconds()) / 1000.0,
		"last_error":                d.lastError,
	}
}
