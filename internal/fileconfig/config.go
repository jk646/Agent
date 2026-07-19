package fileconfig

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Transport           string
	SocketPath          string
	Workspace           string
	TempDir             string
	MaxMessageBytes     int
	MaxFileBytes        int64
	MaxReadBytes        int
	MaxEntries          int
	MaxSearchMatches    int
	MaxTransactionFiles int
	MaxTransactionBytes int64
	MaxDiffBytes        int
	MaxConcurrent       int
	JournalTTL          time.Duration
	ShutdownGrace       time.Duration
}

func Load() Config {
	return Config{
		Transport:           envString("FILE_TOOL_TRANSPORT", "unix"),
		SocketPath:          envString("FILE_TOOL_SOCKET", "/run/agent/file-tool.sock"),
		Workspace:           envString("FILE_TOOL_WORKSPACE", "/workspace"),
		TempDir:             envString("FILE_TOOL_TEMP_DIR", "/tmp/agent-file-tool"),
		MaxMessageBytes:     int(envInt64("FILE_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxFileBytes:        envInt64("FILE_TOOL_MAX_FILE_BYTES", 8<<20),
		MaxReadBytes:        int(envInt64("FILE_TOOL_MAX_READ_BYTES", 256<<10)),
		MaxEntries:          int(envInt64("FILE_TOOL_MAX_ENTRIES", 1000)),
		MaxSearchMatches:    int(envInt64("FILE_TOOL_MAX_SEARCH_MATCHES", 500)),
		MaxTransactionFiles: int(envInt64("FILE_TOOL_MAX_TRANSACTION_FILES", 100)),
		MaxTransactionBytes: envInt64("FILE_TOOL_MAX_TRANSACTION_BYTES", 64<<20),
		MaxDiffBytes:        int(envInt64("FILE_TOOL_MAX_DIFF_BYTES", 1<<20)),
		MaxConcurrent:       int(envInt64("FILE_TOOL_MAX_CONCURRENT", 8)),
		JournalTTL:          envDuration("FILE_TOOL_JOURNAL_TTL", 15*time.Minute),
		ShutdownGrace:       envDuration("FILE_TOOL_SHUTDOWN_GRACE", 5*time.Second),
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
