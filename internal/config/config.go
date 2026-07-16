package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Transport        string
	SocketPath       string
	DefaultShell     string
	DefaultTimeout   time.Duration
	KillGrace        time.Duration
	SessionIdleTTL   time.Duration
	SessionDetachTTL time.Duration
	ShutdownGrace    time.Duration
	OutputLimitBytes int64
	MaxMessageBytes  int
	MaxExec          int
	MaxSessions      int
	TempDir          string
}

func Load() Config {
	return Config{
		Transport: envString("SHELL_TOOL_TRANSPORT", "unix"), SocketPath: envString("SHELL_TOOL_SOCKET", "/run/agent/shell-tool.sock"),
		DefaultShell: envString("SHELL_TOOL_DEFAULT_SHELL", "/bin/bash"), DefaultTimeout: envDuration("SHELL_TOOL_DEFAULT_TIMEOUT", 10*time.Minute),
		KillGrace: envDuration("SHELL_TOOL_KILL_GRACE", 3*time.Second), SessionIdleTTL: envDuration("SHELL_TOOL_SESSION_IDLE_TTL", 15*time.Minute),
		SessionDetachTTL: envDuration("SHELL_TOOL_SESSION_DETACH_TTL", 30*time.Second), ShutdownGrace: envDuration("SHELL_TOOL_SHUTDOWN_GRACE", 5*time.Second),
		OutputLimitBytes: envInt64("SHELL_TOOL_OUTPUT_LIMIT_BYTES", 1<<20), MaxMessageBytes: int(envInt64("SHELL_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxExec: int(envInt64("SHELL_TOOL_MAX_EXEC", 8)), MaxSessions: int(envInt64("SHELL_TOOL_MAX_SESSIONS", 4)), TempDir: envString("SHELL_TOOL_TEMP_DIR", "/tmp/agent-shell"),
	}
}
func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
func envInt64(name string, fallback int64) int64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
