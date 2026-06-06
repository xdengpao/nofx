package drl

import (
	"fmt"
	"math"
	"os"
)

// StubBackend 是默认构建后端，不依赖ONNX Runtime。
type StubBackend struct {
	FixedOutput          float32
	Loaded               bool
	ModelPath            string
	InputShape           []int64
	RequireModelFile     bool
	SkipModelFileForTest bool
	LoadErr              error
	InferErr             error
	Closed               bool
}

func NewStubBackend(fixedOutput float32) *StubBackend {
	return &StubBackend{
		FixedOutput:      fixedOutput,
		RequireModelFile: true,
	}
}

func (b *StubBackend) Load(modelPath string, inputShape []int64) error {
	if b == nil {
		return fmt.Errorf("DRL stub后端为空")
	}
	if b.LoadErr != nil {
		return b.LoadErr
	}
	if b.RequireModelFile && !b.SkipModelFileForTest {
		if _, err := os.Stat(modelPath); err != nil {
			return fmt.Errorf("DRL模型文件不可用: %w", err)
		}
	}
	b.ModelPath = modelPath
	b.InputShape = append([]int64(nil), inputShape...)
	b.Loaded = true
	b.Closed = false
	return nil
}

func (b *StubBackend) Infer(observation []float32) (float32, error) {
	if b == nil {
		return 0, fmt.Errorf("DRL stub后端为空")
	}
	if b.InferErr != nil {
		return 0, b.InferErr
	}
	if !b.Loaded {
		return 0, fmt.Errorf("DRL stub后端未加载")
	}
	return clipFloat32(b.FixedOutput, -1, 1), nil
}

func (b *StubBackend) Close() error {
	if b != nil {
		b.Closed = true
		b.Loaded = false
	}
	return nil
}

func clipFloat32(value, minValue, maxValue float32) float32 {
	return float32(math.Max(float64(minValue), math.Min(float64(maxValue), float64(value))))
}
