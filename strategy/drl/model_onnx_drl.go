//go:build drl

package drl

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNXRuntimeBackend 是真实DRL模型推理后端，仅在 -tags drl 下编译。
type ONNXRuntimeBackend struct {
	mu           sync.Mutex
	session      *ort.AdvancedSession
	inputTensor  *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]
	inputName    string
	outputName   string
	modelPath    string
	inputShape   []int64
	loaded       bool
}

func NewONNXRuntimeBackend() *ONNXRuntimeBackend {
	return &ONNXRuntimeBackend{}
}

func newDefaultBackend() InferenceBackend {
	return NewONNXRuntimeBackend()
}

func (b *ONNXRuntimeBackend) Load(modelPath string, inputShape []int64) error {
	if b == nil {
		return fmt.Errorf("DRL ONNX后端为空")
	}
	modelPath = strings.TrimSpace(modelPath)
	if modelPath == "" {
		return fmt.Errorf("DRL模型路径不能为空")
	}
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("DRL模型文件不存在或不可读: %w", err)
	}
	if err := validateONNXInputShape(inputShape); err != nil {
		return err
	}
	if err := acquireONNXRuntime(); err != nil {
		return fmt.Errorf("初始化ONNX Runtime失败: %w", err)
	}
	loaded := false
	defer func() {
		if !loaded {
			releaseONNXRuntime()
		}
	}()

	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return fmt.Errorf("读取DRL模型输入输出信息失败: %w", err)
	}
	if len(inputs) == 0 || len(outputs) == 0 {
		return fmt.Errorf("DRL模型缺少输入或输出节点")
	}
	inputName := inputs[0].Name
	outputName := outputs[0].Name
	if inputs[0].OrtValueType == ort.ONNXTypeTensor && inputs[0].DataType != ort.TensorElementDataTypeFloat {
		return fmt.Errorf("DRL模型输入必须是float32张量: %s", inputs[0].String())
	}
	if outputs[0].OrtValueType == ort.ONNXTypeTensor && outputs[0].DataType != ort.TensorElementDataTypeFloat {
		return fmt.Errorf("DRL模型输出必须是float32张量: %s", outputs[0].String())
	}

	inputTensor, err := ort.NewEmptyTensor[float32](ort.Shape(append([]int64(nil), inputShape...)))
	if err != nil {
		return fmt.Errorf("创建DRL模型输入张量失败: %w", err)
	}
	outputShape := concreteOutputShape(outputs[0].Dimensions)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		_ = inputTensor.Destroy()
		return fmt.Errorf("创建DRL模型输出张量失败: %w", err)
	}
	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{inputName},
		[]string{outputName},
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		_ = outputTensor.Destroy()
		_ = inputTensor.Destroy()
		return fmt.Errorf("创建DRL模型推理session失败: %w", err)
	}

	b.mu.Lock()
	oldSession := b.session
	oldInput := b.inputTensor
	oldOutput := b.outputTensor
	b.session = session
	b.inputTensor = inputTensor
	b.outputTensor = outputTensor
	b.inputName = inputName
	b.outputName = outputName
	b.modelPath = modelPath
	b.inputShape = append([]int64(nil), inputShape...)
	b.loaded = true
	b.mu.Unlock()

	destroyONNXResources(oldSession, oldInput, oldOutput)
	loaded = true
	log.Printf("DRL ONNX模型加载成功: path=%s input=%s output=%s", modelPath, inputName, outputName)
	return nil
}

func (b *ONNXRuntimeBackend) Infer(observation []float32) (float32, error) {
	if b == nil {
		return 0, fmt.Errorf("DRL ONNX后端为空")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.loaded || b.session == nil || b.inputTensor == nil || b.outputTensor == nil {
		return 0, fmt.Errorf("DRL ONNX后端未加载")
	}
	inputData := b.inputTensor.GetData()
	if len(observation) != len(inputData) {
		return 0, fmt.Errorf("DRL观测维度错误: got=%d want=%d", len(observation), len(inputData))
	}
	copy(inputData, observation)
	if err := b.session.Run(); err != nil {
		return 0, fmt.Errorf("DRL ONNX推理失败: %w", err)
	}
	output := b.outputTensor.GetData()
	if len(output) == 0 {
		return 0, fmt.Errorf("DRL ONNX推理输出为空")
	}
	raw := output[0]
	clipped := clipFloat32(raw, -1, 1)
	if clipped != raw {
		log.Printf("DRL ONNX推理输出超出范围，已clip: raw=%.6f clipped=%.6f", raw, clipped)
	}
	return clipped, nil
}

func (b *ONNXRuntimeBackend) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	session := b.session
	input := b.inputTensor
	output := b.outputTensor
	wasLoaded := b.loaded
	b.session = nil
	b.inputTensor = nil
	b.outputTensor = nil
	b.loaded = false
	b.mu.Unlock()
	err := destroyONNXResources(session, input, output)
	if wasLoaded {
		releaseONNXRuntime()
	}
	return err
}

func validateONNXInputShape(inputShape []int64) error {
	if len(inputShape) == 0 {
		return fmt.Errorf("DRL ONNX输入shape不能为空")
	}
	for i, dim := range inputShape {
		if dim <= 0 {
			return fmt.Errorf("DRL ONNX输入shape包含非法维度: index=%d dim=%d", i, dim)
		}
	}
	return nil
}

func concreteOutputShape(shape ort.Shape) ort.Shape {
	if len(shape) == 0 {
		return ort.NewShape(1, 1)
	}
	out := make([]int64, len(shape))
	for i, dim := range shape {
		if dim <= 0 {
			dim = 1
		}
		out[i] = dim
	}
	return ort.Shape(out)
}

func destroyONNXResources(session *ort.AdvancedSession, input *ort.Tensor[float32], output *ort.Tensor[float32]) error {
	var firstErr error
	if session != nil {
		if err := session.Destroy(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if input != nil {
		if err := input.Destroy(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if output != nil {
		if err := output.Destroy(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

var onnxRuntimeState struct {
	mu          sync.Mutex
	refs        int
	initialized bool
}

func acquireONNXRuntime() error {
	onnxRuntimeState.mu.Lock()
	defer onnxRuntimeState.mu.Unlock()
	if onnxRuntimeState.initialized {
		onnxRuntimeState.refs++
		return nil
	}
	if libPath := strings.TrimSpace(os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH")); libPath != "" {
		ort.SetSharedLibraryPath(libPath)
	} else if libPath := strings.TrimSpace(os.Getenv("ORT_SHARED_LIBRARY_PATH")); libPath != "" {
		ort.SetSharedLibraryPath(libPath)
	}
	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return err
		}
	}
	onnxRuntimeState.initialized = true
	onnxRuntimeState.refs = 1
	return nil
}

func releaseONNXRuntime() {
	onnxRuntimeState.mu.Lock()
	defer onnxRuntimeState.mu.Unlock()
	if !onnxRuntimeState.initialized {
		return
	}
	onnxRuntimeState.refs--
	if onnxRuntimeState.refs > 0 {
		return
	}
	if ort.IsInitialized() {
		if err := ort.DestroyEnvironment(); err != nil {
			log.Printf("释放ONNX Runtime环境失败: %v", err)
		}
	}
	onnxRuntimeState.refs = 0
	onnxRuntimeState.initialized = false
}
