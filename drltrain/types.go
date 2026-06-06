package drltrain

import (
	"time"

	"nofx/storage"
)

type Config struct {
	Layout         storage.Layout
	PythonBin      string
	TrainScript    string
	EvaluateScript string
	MaxConcurrency int
	LogTailBytes   int64
}

type EnvironmentStatus struct {
	Ready                   bool     `json:"ready"`
	PythonBin               string   `json:"python_bin"`
	PythonPath              string   `json:"python_path,omitempty"`
	PythonAvailable         bool     `json:"python_available"`
	TrainScript             string   `json:"train_script"`
	TrainScriptAvailable    bool     `json:"train_script_available"`
	EvaluateScript          string   `json:"evaluate_script,omitempty"`
	EvaluateScriptAvailable bool     `json:"evaluate_script_available"`
	Errors                  []string `json:"errors,omitempty"`
}

type JobStatus string

const (
	JobPending     JobStatus = "pending"
	JobRunning     JobStatus = "running"
	JobCompleted   JobStatus = "completed"
	JobFailed      JobStatus = "failed"
	JobCancelled   JobStatus = "cancelled"
	JobInterrupted JobStatus = "interrupted"
)

type TrainRequest struct {
	Source              string  `json:"source,omitempty"`
	Symbol              string  `json:"symbol"`
	Timeframe           string  `json:"timeframe"`
	Start               string  `json:"start"`
	End                 string  `json:"end"`
	TotalTimesteps      int     `json:"total_timesteps"`
	NSteps              int     `json:"n_steps,omitempty"`
	BatchSize           int     `json:"batch_size,omitempty"`
	NEpochs             int     `json:"n_epochs,omitempty"`
	ObservationWindow   int     `json:"observation_window"`
	InitialBalance      float64 `json:"initial_balance"`
	TakerFee            float64 `json:"taker_fee"`
	MakerFee            float64 `json:"maker_fee"`
	Slippage            float64 `json:"slippage"`
	Rolling             bool    `json:"rolling"`
	OutputModelName     string  `json:"output_model_name"`
	AllowIncompleteData bool    `json:"allow_incomplete_data,omitempty"`
}

type TrainProgress struct {
	Timesteps      int       `json:"timesteps,omitempty"`
	TotalTimesteps int       `json:"total_timesteps"`
	LastLogAt      time.Time `json:"last_log_at,omitempty"`
	RuntimeSeconds int64     `json:"runtime_seconds"`
	Stalled        bool      `json:"stalled"`
}

type ModelArtifacts struct {
	ModelID            string   `json:"model_id,omitempty"`
	ModelVersion       string   `json:"model_version,omitempty"`
	ZipPath            string   `json:"zip_path,omitempty"`
	ONNXPath           string   `json:"onnx_path,omitempty"`
	RollingSummaryPath string   `json:"rolling_summary_path,omitempty"`
	WindowModelPaths   []string `json:"window_model_paths,omitempty"`
	MetadataPath       string   `json:"metadata_path,omitempty"`
	EvaluationPath     string   `json:"evaluation_path,omitempty"`
}

type JobMetadata struct {
	JobID        string         `json:"job_id"`
	Type         string         `json:"type"`
	Status       JobStatus      `json:"status"`
	Request      TrainRequest   `json:"request"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	EndedAt      time.Time      `json:"ended_at,omitempty"`
	PID          int            `json:"pid,omitempty"`
	Progress     TrainProgress  `json:"progress,omitempty"`
	JobDir       string         `json:"job_dir"`
	StdoutPath   string         `json:"stdout_path"`
	StderrPath   string         `json:"stderr_path"`
	ProgressPath string         `json:"progress_path"`
	Error        string         `json:"error,omitempty"`
	ExitCode     int            `json:"exit_code,omitempty"`
	Artifacts    ModelArtifacts `json:"artifacts,omitempty"`
	SummaryPath  string         `json:"summary_path,omitempty"`
}

type LogChunk struct {
	JobID      string `json:"job_id"`
	Stream     string `json:"stream"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Content    string `json:"content"`
	EOF        bool   `json:"eof"`
}
