package drl

// InferenceBackend 是DRL推理后端抽象。
type InferenceBackend interface {
	Load(modelPath string, inputShape []int64) error
	Infer(observation []float32) (float32, error)
	Close() error
}
