//go:build !cgo

package chanlunv2

import "fmt"

// AnalyzeKlines stub: CGO 未启用时返回错误
func AnalyzeKlines(input *AnalysisInput) (*AnalysisOutput, error) {
	return nil, fmt.Errorf("chanlun_v2: CGO not enabled, Rust library unavailable")
}
