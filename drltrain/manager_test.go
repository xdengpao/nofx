package drltrain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nofx/storage"
)

func TestManagerStartCompletesWithFakeTrainer(t *testing.T) {
	m := newTestManager(t, fakeTrainer(t, 0))
	meta, err := m.Start(context.Background(), validTrainRequest())
	if err != nil {
		t.Fatalf("Start失败: %v", err)
	}
	meta = waitJob(t, m, meta.JobID, JobCompleted, 5*time.Second)
	if meta.Artifacts.ONNXPath == "" || meta.Artifacts.ZipPath == "" {
		t.Fatalf("普通训练应记录zip和onnx产物: %+v", meta.Artifacts)
	}
	logs, err := m.Logs(meta.JobID, "stdout", 0, 0)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if logs.Content == "" {
		t.Fatal("stdout日志不能为空")
	}
	if meta.Progress.Timesteps == 0 {
		t.Fatalf("应读取结构化进度: %+v", meta.Progress)
	}
}

func TestManagerConcurrencyLimit(t *testing.T) {
	m := newTestManager(t, fakeTrainer(t, time.Second))
	first, err := m.Start(context.Background(), validTrainRequest())
	if err != nil {
		t.Fatalf("Start失败: %v", err)
	}
	if _, err := m.Start(context.Background(), validTrainRequest()); err == nil {
		t.Fatal("并发上限为1时第二个训练任务应失败")
	}
	_ = m.Cancel(first.JobID)
	_ = waitJob(t, m, first.JobID, JobCancelled, 2*time.Second)
}

func TestManagerStartRejectsMissingPythonBeforeCreatingJob(t *testing.T) {
	layout := testLayout(t)
	m, err := NewManager(Config{
		Layout:         layout,
		PythonBin:      "python-not-exist-for-nofx-test",
		TrainScript:    fakeTrainScript(t),
		EvaluateScript: "fake-evaluate.py",
		MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("NewManager失败: %v", err)
	}
	_, err = m.Start(context.Background(), validTrainRequest())
	if err == nil || !strings.Contains(err.Error(), "训练环境不可用") {
		t.Fatalf("Python缺失时应直接返回训练环境错误: %v", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("环境不可用时不应创建failed job: %+v", got)
	}
}

func TestManagerCancel(t *testing.T) {
	m := newTestManager(t, fakeTrainer(t, 5*time.Second))
	meta, err := m.Start(context.Background(), validTrainRequest())
	if err != nil {
		t.Fatalf("Start失败: %v", err)
	}
	waitJob(t, m, meta.JobID, JobRunning, time.Second)
	if err := m.Cancel(meta.JobID); err != nil {
		t.Fatalf("Cancel失败: %v", err)
	}
	_ = waitJob(t, m, meta.JobID, JobCancelled, 2*time.Second)
}

func TestManagerRecoverMarksRunningInterrupted(t *testing.T) {
	layout := testLayout(t)
	jobDir := filepath.Join(layout.TrainJobs, "old_job")
	meta := JobMetadata{JobID: "old_job", Status: JobRunning, JobDir: jobDir, Type: "drl_ppo_train"}
	if err := writeJSON(filepath.Join(jobDir, "job.json"), meta); err != nil {
		t.Fatalf("写入job fixture失败: %v", err)
	}
	m, err := NewManager(Config{Layout: layout, PythonBin: "python3"})
	if err != nil {
		t.Fatalf("NewManager失败: %v", err)
	}
	got, ok := m.Get("old_job")
	if !ok || got.Status != JobInterrupted {
		t.Fatalf("running job应恢复为interrupted: %+v ok=%v", got, ok)
	}
}

func newTestManager(t *testing.T, trainer string) *Manager {
	t.Helper()
	layout := testLayout(t)
	m, err := NewManager(Config{
		Layout:         layout,
		PythonBin:      trainer,
		TrainScript:    fakeTrainScript(t),
		EvaluateScript: "fake-evaluate.py",
		MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("NewManager失败: %v", err)
	}
	return m
}

func testLayout(t *testing.T) storage.Layout {
	t.Helper()
	layout := storage.NewLayout(storage.RuntimeConfig{Root: t.TempDir()})
	if err := storage.EnsureLayout(layout); err != nil {
		t.Fatalf("EnsureLayout失败: %v", err)
	}
	return layout
}

func fakeTrainScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-train.py")
	if err := os.WriteFile(path, []byte("# fake train script\n"), 0o644); err != nil {
		t.Fatalf("写fake train script失败: %v", err)
	}
	return path
}

func fakeTrainer(t *testing.T, sleep time.Duration) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-trainer.sh")
	content := `#!/bin/sh
set -eu
shift
output=""
progress=""
rolling=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    --progress-path) progress="$2"; shift 2 ;;
    --rolling) rolling=1; shift ;;
    *) shift ;;
  esac
done
echo "fake trainer started"
if [ -n "$progress" ]; then
  mkdir -p "$(dirname "$progress")"
  echo '{"timesteps":10,"total_timesteps":1000,"timestamp":"2026-06-06T00:00:00Z"}' > "$progress"
fi
sleep ` + formatShellSleep(sleep) + `
if [ -n "$output" ]; then
  mkdir -p "$(dirname "$output")"
  if [ "$rolling" = "1" ]; then
    echo '[]' > "$output"
  else
    echo 'onnx' > "$output"
    echo 'zip' > "${output%.*}.zip"
  fi
fi
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("写fake trainer失败: %v", err)
	}
	return path
}

func formatShellSleep(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return "1"
}

func validTrainRequest() TrainRequest {
	return TrainRequest{
		Source:              "binance-futures",
		Symbol:              "BTCUSDT",
		Timeframe:           "4h",
		Start:               "2025-01-01",
		End:                 "2025-02-01",
		TotalTimesteps:      1000,
		ObservationWindow:   60,
		InitialBalance:      10000,
		TakerFee:            0.0005,
		MakerFee:            0.0002,
		Slippage:            0.0003,
		OutputModelName:     "test_model",
		AllowIncompleteData: true,
	}
}

func waitJob(t *testing.T, m *Manager, jobID string, want JobStatus, timeout time.Duration) *JobMetadata {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *JobMetadata
	for time.Now().Before(deadline) {
		meta, ok := m.Get(jobID)
		if !ok {
			t.Fatalf("job不存在: %s", jobID)
		}
		last = meta
		if meta.Status == want {
			return meta
		}
		if want == JobRunning && meta.Status != JobPending {
			return meta
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待job状态超时 want=%s last=%+v", want, last)
	return nil
}
