//go:build !drl

package drl

func newDefaultBackend() InferenceBackend {
	return NewStubBackend(0)
}
