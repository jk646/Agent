package writeconfig

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Transport        string
	SocketPath       string
	Workspace        string
	TempDir          string
	MaxMessageBytes  int
	MaxFileBytes     int64
	MaxBatchFiles    int
	MaxBatchBytes    int64
	MaxRollbackBytes int64
	MaxConcurrent    int
	JournalTTL       time.Duration
	ShutdownGrace    time.Duration
}

func Load() Config {
	return Config{
		Transport:        envString("WRITE_FILE_TOOL_TRANSPORT", "unix"),
		SocketPath:       envString("WRITE_FILE_TOOL_SOCKET", "/run/agent/write-file-tool.sock"),
		Workspace:        envString("WRITE_FILE_TOOL_WORKSPACE", "/workspace"),
		TempDir:          envString("WRITE_FILE_TOOL_TEMP_DIR", "/tmp/agent-write-file"),
		MaxMessageBytes:  int(envInt64("WRITE_FILE_TOOL_MAX_MESSAGE_BYTES", 16<<20)),
		MaxFileBytes:     envInt64("WRITE_FILE_TOOL_MAX_FILE_BYTES", 8<<20),
		MaxBatchFiles:    int(envInt64("WRITE_FILE_TOOL_MAX_BATCH_FILES", 100)),
		MaxBatchBytes:    envInt64("WRITE_FILE_TOOL_MAX_BATCH_BYTES", 64<<20),
		MaxRollbackBytes: envInt64("WRITE_FILE_TOOL_MAX_ROLLBACK_BYTES", 256<<20),
		MaxConcurrent:    int(envInt64("WRITE_FILE_TOOL_MAX_CONCURRENT", 8)),
		JournalTTL:       envDuration("WRITE_FILE_TOOL_JOURNAL_TTL", 15*time.Minute),
		ShutdownGrace:    envDuration("WRITE_FILE_TOOL_SHUTDOWN_GRACE", 5*time.Second),
	}
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
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
