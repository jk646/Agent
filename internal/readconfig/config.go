package readconfig

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Transport       string
	SocketPath      string
	Workspace       string
	MaxMessageBytes int
	MaxTextBytes    int64
	MaxChunkBytes   int
	MaxHashBytes    int64
	MaxConcurrent   int
	MaxBatchItems   int
	ShutdownGrace   time.Duration
}

func Load() Config {
	return Config{
		Transport:       envString("READ_FILE_TOOL_TRANSPORT", "unix"),
		SocketPath:      envString("READ_FILE_TOOL_SOCKET", "/run/agent/read-file-tool.sock"),
		Workspace:       envString("READ_FILE_TOOL_WORKSPACE", "/workspace"),
		MaxMessageBytes: int(envInt64("READ_FILE_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxTextBytes:    envInt64("READ_FILE_TOOL_MAX_TEXT_BYTES", 8<<20),
		MaxChunkBytes:   int(envInt64("READ_FILE_TOOL_MAX_CHUNK_BYTES", 1<<20)),
		MaxHashBytes:    envInt64("READ_FILE_TOOL_MAX_HASH_BYTES", 1<<30),
		MaxConcurrent:   int(envInt64("READ_FILE_TOOL_MAX_CONCURRENT", 8)),
		MaxBatchItems:   int(envInt64("READ_FILE_TOOL_MAX_BATCH_ITEMS", 20)),
		ShutdownGrace:   envDuration("READ_FILE_TOOL_SHUTDOWN_GRACE", 5*time.Second),
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
