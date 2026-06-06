//go:build drl

package drl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestONNXRuntimeBackendLoadMissingModel(t *testing.T) {
	withONNXRuntimeForTest(t)
	backend := NewONNXRuntimeBackend()
	err := backend.Load(filepath.Join(t.TempDir(), "missing.onnx"), []int64{1, 10})
	if err == nil {
		t.Fatalf("模型不存在时应返回错误")
	}
	if !contains(err.Error(), "DRL模型文件不存在或不可读") {
		t.Fatalf("缺失模型错误不清晰: %v", err)
	}
}

func TestONNXRuntimeBackendInferAndClip(t *testing.T) {
	modelPath := onnxFixtureForTest(t)
	withONNXRuntimeForTest(t)
	backend := NewONNXRuntimeBackend()
	if err := backend.Load(modelPath, []int64{1, 10}); err != nil {
		t.Fatalf("加载ONNX模型失败: %v", err)
	}
	defer backend.Close()
	raw, err := backend.Infer([]float32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	if err != nil {
		t.Fatalf("ONNX推理失败: %v", err)
	}
	if raw != 1 {
		t.Fatalf("输出应clip到1: %v", raw)
	}
	_, err = backend.Infer([]float32{1, 2})
	if err == nil || !contains(err.Error(), "DRL观测维度错误") {
		t.Fatalf("维度错误应返回清晰错误: %v", err)
	}
}

func TestONNXRuntimeBackendConcurrentInfer(t *testing.T) {
	modelPath := onnxFixtureForTest(t)
	withONNXRuntimeForTest(t)
	backend := NewONNXRuntimeBackend()
	if err := backend.Load(modelPath, []int64{1, 10}); err != nil {
		t.Fatalf("加载ONNX模型失败: %v", err)
	}
	defer backend.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := backend.Infer(make([]float32, 10)); err != nil {
					t.Errorf("并发推理失败: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func withONNXRuntimeForTest(t *testing.T) {
	t.Helper()
	if os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH") != "" {
		return
	}
	path, err := onnxRuntimeLibFromModuleCache()
	if err != nil {
		t.Skipf("未找到ONNX Runtime共享库，跳过 -tags drl 测试: %v", err)
	}
	t.Setenv("ONNXRUNTIME_SHARED_LIBRARY_PATH", path)
}

func onnxFixtureForTest(t *testing.T) string {
	t.Helper()
	path, err := onnxRuntimeModulePath()
	if err != nil {
		t.Skipf("未找到onnxruntime_go模块fixture: %v", err)
	}
	fixture := filepath.Join(path, "test_data", "example_dynamic_axes.onnx")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("未找到ONNX测试模型fixture: %v", err)
	}
	return fixture
}

func onnxRuntimeLibFromModuleCache() (string, error) {
	path, err := onnxRuntimeModulePath()
	if err != nil {
		return "", err
	}
	var lib string
	switch runtime.GOOS {
	case "darwin":
		lib = fmt.Sprintf("onnxruntime_%s.dylib", runtime.GOARCH)
	case "linux":
		lib = fmt.Sprintf("onnxruntime_%s.so", runtime.GOARCH)
	case "windows":
		lib = "onnxruntime.dll"
	default:
		return "", fmt.Errorf("不支持的平台: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	full := filepath.Join(path, "test_data", lib)
	if _, err := os.Stat(full); err != nil {
		return "", err
	}
	return full, nil
}

func onnxRuntimeModulePath() (string, error) {
	candidates := []string{}
	if gomodcache := os.Getenv("GOMODCACHE"); gomodcache != "" {
		candidates = append(candidates, filepath.Join(gomodcache, "github.com", "yalue", "onnxruntime_go@v1.30.1"))
	}
	for _, gopath := range filepath.SplitList(os.Getenv("GOPATH")) {
		if gopath != "" {
			candidates = append(candidates, filepath.Join(gopath, "pkg", "mod", "github.com", "yalue", "onnxruntime_go@v1.30.1"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "go", "pkg", "mod", "github.com", "yalue", "onnxruntime_go@v1.30.1"))
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", errors.New("module cache missing github.com/yalue/onnxruntime_go@v1.30.1")
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
