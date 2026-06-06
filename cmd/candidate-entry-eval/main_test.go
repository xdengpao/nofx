package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeSnapshotPathRedirectsRuntimeDataPath(t *testing.T) {
	got := safeSnapshotPath(filepath.Join("data", "dynamic_candidate_pool.json"))
	if !strings.HasPrefix(got, os.TempDir()) {
		t.Fatalf("data目录snapshot path应改写到临时目录: %s", got)
	}
	if filepath.Base(got) != "dynamic_candidate_pool.json" {
		t.Fatalf("应保留snapshot文件名: %s", got)
	}
}
