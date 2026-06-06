package drltrain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nofx/storage"
)

type ModelMetadata struct {
	ModelID           string         `json:"model_id"`
	ModelVersion      string         `json:"model_version,omitempty"`
	Symbol            string         `json:"symbol,omitempty"`
	Timeframe         string         `json:"timeframe,omitempty"`
	Source            string         `json:"source,omitempty"`
	DataFrom          string         `json:"data_from,omitempty"`
	DataTo            string         `json:"data_to,omitempty"`
	TotalTimesteps    int            `json:"total_timesteps,omitempty"`
	ObservationWindow int            `json:"observation_window,omitempty"`
	CreatedAt         string         `json:"created_at,omitempty"`
	ZipPath           string         `json:"zip_path,omitempty"`
	ONNXPath          string         `json:"onnx_path,omitempty"`
	EvaluationPath    string         `json:"evaluation_path,omitempty"`
	Deployable        bool           `json:"deployable"`
	RuntimeNote       string         `json:"runtime_note,omitempty"`
	Raw               map[string]any `json:"raw,omitempty"`
	dir               string
}

type EvaluationRequest struct {
	Symbol            string `json:"symbol,omitempty"`
	Source            string `json:"source,omitempty"`
	Timeframe         string `json:"timeframe,omitempty"`
	Start             string `json:"start,omitempty"`
	End               string `json:"end,omitempty"`
	ObservationWindow int    `json:"observation_window,omitempty"`
}

func (m *Manager) Models() ([]ModelMetadata, error) {
	var models []ModelMetadata
	err := filepath.WalkDir(m.cfg.Layout.ModelsDRL, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || filepath.Base(path) != "metadata.json" {
			return nil
		}
		model, parseErr := m.readModelMetadata(path)
		if parseErr == nil {
			models = append(models, model)
		}
		return nil
	})
	return models, err
}

func (m *Manager) Evaluate(ctx context.Context, modelID string, req EvaluationRequest) (map[string]any, error) {
	model, err := m.findModel(modelID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.cfg.EvaluateScript) == "" {
		return nil, fmt.Errorf("evaluate脚本未配置")
	}
	req = mergeEvaluationRequest(model, req)
	modelPath := model.ZipPath
	if strings.TrimSpace(modelPath) == "" {
		return nil, fmt.Errorf("模型缺少SB3 zip，无法使用当前evaluate.py评估")
	}
	absModelPath, err := storage.ResolveUnderRoot(m.cfg.Layout, modelPath)
	if err != nil {
		return nil, err
	}
	args := []string{
		m.cfg.EvaluateScript,
		"--data-path", m.cfg.Layout.HistoryDB,
		"--model-path", absModelPath,
		"--symbol", req.Symbol,
		"--source", req.Source,
		"--timeframe", req.Timeframe,
		"--observation-window", fmt.Sprintf("%d", req.ObservationWindow),
	}
	if strings.TrimSpace(req.Start) != "" {
		args = append(args, "--start", req.Start)
	}
	if strings.TrimSpace(req.End) != "" {
		args = append(args, "--end", req.End)
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, m.cfg.PythonBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("评估失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("解析评估输出失败: %w", err)
	}
	result["recommended_for_deploy"] = recommendedForDeploy(result)
	result["thresholds"] = map[string]any{"directional_accuracy": 0.55, "max_drawdown": 0.2}
	evalPath := filepath.Join(model.dir, "evaluation.json")
	if err := writeJSON(evalPath, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) StagingConfig(modelID string) (map[string]any, error) {
	model, err := m.findModel(modelID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(model.ONNXPath) == "" {
		return nil, fmt.Errorf("模型缺少ONNX产物，暂不可生成部署配置")
	}
	return map[string]any{
		"trader_patch": map[string]any{
			"decision_mode": "drl",
			"drl_strategy": map[string]any{
				"model_path":         model.ONNXPath,
				"model_version":      model.ModelVersion,
				"timeframe":          model.Timeframe,
				"symbols":            []string{model.Symbol},
				"observation_window": model.ObservationWindow,
			},
		},
		"warnings": []string{
			"当前为配置建议，不会自动修改实盘trader",
			"当前默认推理后端为stub，需确认ONNX Runtime build tag",
		},
	}, nil
}

func (m *Manager) findModel(modelID string) (ModelMetadata, error) {
	models, err := m.Models()
	if err != nil {
		return ModelMetadata{}, err
	}
	for _, model := range models {
		if model.ModelID == modelID {
			return model, nil
		}
	}
	return ModelMetadata{}, fmt.Errorf("模型不存在: %s", modelID)
}

func (m *Manager) readModelMetadata(path string) (ModelMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelMetadata{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return ModelMetadata{}, err
	}
	dir := filepath.Dir(path)
	model := ModelMetadata{
		ModelID:           stringValue(raw, "model_id", filepath.Base(dir)),
		ModelVersion:      stringValue(raw, "model_version", filepath.Base(dir)),
		Symbol:            stringValue(raw, "symbol", ""),
		Timeframe:         stringValue(raw, "timeframe", ""),
		Source:            stringValue(raw, "source", ""),
		DataFrom:          stringValue(raw, "data_from", ""),
		DataTo:            stringValue(raw, "data_to", ""),
		TotalTimesteps:    intValue(raw, "total_timesteps"),
		ObservationWindow: intValue(raw, "observation_window"),
		CreatedAt:         stringValue(raw, "created_at", ""),
		ZipPath:           stringValue(raw, "zip_path", ""),
		ONNXPath:          stringValue(raw, "onnx_path", ""),
		Deployable:        boolValue(raw, "deployable"),
		RuntimeNote:       stringValue(raw, "runtime_note", ""),
		Raw:               raw,
		dir:               dir,
	}
	evalPath := filepath.Join(dir, "evaluation.json")
	if _, err := os.Stat(evalPath); err == nil {
		model.EvaluationPath = storage.RelForDisplay(m.cfg.Layout, evalPath)
	}
	return model, nil
}

func mergeEvaluationRequest(model ModelMetadata, req EvaluationRequest) EvaluationRequest {
	if req.Symbol == "" {
		req.Symbol = model.Symbol
	}
	if req.Source == "" {
		req.Source = model.Source
	}
	if req.Source == "" {
		req.Source = "binance-futures"
	}
	if req.Timeframe == "" {
		req.Timeframe = model.Timeframe
	}
	if req.Start == "" {
		req.Start = model.DataFrom
	}
	if req.End == "" {
		req.End = model.DataTo
	}
	if req.ObservationWindow <= 0 {
		req.ObservationWindow = model.ObservationWindow
	}
	if req.ObservationWindow <= 0 {
		req.ObservationWindow = 60
	}
	return req
}

func recommendedForDeploy(result map[string]any) bool {
	da, _ := result["directional_accuracy"].(float64)
	dd, _ := result["max_drawdown"].(float64)
	return da >= 0.55 && dd <= 0.2
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func intValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func boolValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}
