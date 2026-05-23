//go:build cgo

package chanlunv2

/*
#cgo LDFLAGS: -L${SRCDIR}/../../lib -lchanlun_v2 -lm
#include <stdlib.h>
#include <string.h>

extern int chanlun_analyze(const char* input, char* output, int output_len);
extern void chanlun_free_string(char* ptr);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

const outputBufSize = 2 * 1024 * 1024 // 2MB

// AnalyzeKlines 调用 Rust 缠论库进行全量分析
func AnalyzeKlines(input *AnalysisInput) (*AnalysisOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	cInput := C.CString(string(inputJSON))
	defer C.free(unsafe.Pointer(cInput))

	outputBuf := (*C.char)(C.malloc(C.size_t(outputBufSize)))
	defer C.free(unsafe.Pointer(outputBuf))

	ret := C.chanlun_analyze(cInput, outputBuf, C.int(outputBufSize))
	if ret < 0 {
		return nil, fmt.Errorf("chanlun_analyze error code: %d", int(ret))
	}

	outputBytes := C.GoBytes(unsafe.Pointer(outputBuf), ret)
	var output AnalysisOutput
	if err := json.Unmarshal(outputBytes, &output); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w", err)
	}
	if !output.Success {
		return nil, fmt.Errorf("chanlun error: %s", output.Error)
	}
	return &output, nil
}
