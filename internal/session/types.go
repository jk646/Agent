package session

import "time"

type Config struct {
	DefaultShell     string
	DefaultTimeout   time.Duration
	IdleTTL          time.Duration
	DetachTTL        time.Duration
	KillGrace        time.Duration
	OutputLimitBytes int64
	MaxSessions      int
	TempDir          string
}
type OpenParams struct {
	SessionID string            `json:"session_id"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Shell     string            `json:"shell,omitempty"`
	Rows      uint16            `json:"rows,omitempty"`
	Cols      uint16            `json:"cols,omitempty"`
}
type OpenResult struct {
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}
type RunParams struct {
	SessionID        string `json:"session_id"`
	RunID            string `json:"run_id,omitempty"`
	Command          string `json:"command"`
	TimeoutMS        int64  `json:"timeout_ms,omitempty"`
	OutputLimitBytes int64  `json:"output_limit_bytes,omitempty"`
}
type RunResult struct {
	SessionID        string `json:"session_id"`
	RunID            string `json:"run_id"`
	ExitCode         int    `json:"exit_code"`
	DurationMS       int64  `json:"duration_ms"`
	TimedOut         bool   `json:"timed_out"`
	TotalOutputBytes int64  `json:"total_output_bytes"`
	Truncated        bool   `json:"truncated"`
	LogPath          string `json:"log_path,omitempty"`
	TailBase64       string `json:"tail_base64,omitempty"`
}
type WriteParams struct {
	SessionID  string `json:"session_id"`
	DataBase64 string `json:"data_base64"`
}
type ResizeParams struct {
	SessionID string `json:"session_id"`
	Rows      uint16 `json:"rows"`
	Cols      uint16 `json:"cols"`
}
type IDParams struct {
	SessionID string `json:"session_id"`
}
type Info struct {
	SessionID    string `json:"session_id"`
	PID          int    `json:"pid"`
	CreatedAt    string `json:"created_at"`
	LastActiveAt string `json:"last_active_at"`
	Attached     bool   `json:"attached"`
	Running      bool   `json:"running"`
}
type OutputEvent struct {
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id,omitempty"`
	Sequence   uint64 `json:"sequence"`
	DataBase64 string `json:"data_base64"`
	Timestamp  string `json:"timestamp"`
}
type TruncatedEvent struct {
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id"`
	Sequence   uint64 `json:"sequence"`
	LogPath    string `json:"log_path"`
	TotalBytes int64  `json:"total_bytes"`
	Timestamp  string `json:"timestamp"`
}
