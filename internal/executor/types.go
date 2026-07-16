package executor

import "time"

type StartParams struct {
	RequestID        string            `json:"request_id"`
	Command          string            `json:"command"`
	Cwd              string            `json:"cwd,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	TimeoutMS        int64             `json:"timeout_ms,omitempty"`
	OutputLimitBytes int64             `json:"output_limit_bytes,omitempty"`
	Shell            string            `json:"shell,omitempty"`
	EnableStdin      bool              `json:"enable_stdin,omitempty"`
}
type StartResult struct {
	RequestID string `json:"request_id"`
	Accepted  bool   `json:"accepted"`
}
type CancelParams struct {
	RequestID string `json:"request_id"`
}
type WriteParams struct {
	RequestID  string `json:"request_id"`
	DataBase64 string `json:"data_base64,omitempty"`
	Close      bool   `json:"close,omitempty"`
}
type StartedEvent struct {
	RequestID string `json:"request_id"`
	PID       int    `json:"pid"`
	Timestamp string `json:"timestamp"`
}
type FailedEvent struct {
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}
type ExitedEvent struct {
	RequestID        string `json:"request_id"`
	ExitCode         int    `json:"exit_code"`
	Signal           string `json:"signal,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
	TimedOut         bool   `json:"timed_out"`
	Canceled         bool   `json:"canceled"`
	TotalOutputBytes int64  `json:"total_output_bytes"`
	Truncated        bool   `json:"truncated"`
	LogPath          string `json:"log_path,omitempty"`
	TailBase64       string `json:"tail_base64,omitempty"`
	Timestamp        string `json:"timestamp"`
}

type Config struct {
	DefaultShell     string
	DefaultTimeout   time.Duration
	KillGrace        time.Duration
	OutputLimitBytes int64
	MaxConcurrent    int
	TempDir          string
}
