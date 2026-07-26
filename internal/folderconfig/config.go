package folderconfig

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Transport         string
	SocketPath        string
	Workspace         string
	MaxMessageBytes   int
	MaxDepth          int
	MaxScannedEntries int
	MaxResults        int
	DefaultLimit      int
	MaxHashBytes      int64
	MaxConcurrent     int
	MaxBatchItems     int
	IgnoredNames      []string
	ShutdownGrace     time.Duration
}

func Load() Config {
	return Config{
		Transport:         envString("READ_FOLDER_TOOL_TRANSPORT", "unix"),
		SocketPath:        envString("READ_FOLDER_TOOL_SOCKET", "/run/agent/read-folder-tool.sock"),
		Workspace:         envString("READ_FOLDER_TOOL_WORKSPACE", "/workspace"),
		MaxMessageBytes:   int(envInt64("READ_FOLDER_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxDepth:          int(envInt64("READ_FOLDER_TOOL_MAX_DEPTH", 32)),
		MaxScannedEntries: int(envInt64("READ_FOLDER_TOOL_MAX_SCANNED_ENTRIES", 100000)),
		MaxResults:        int(envInt64("READ_FOLDER_TOOL_MAX_RESULTS", 5000)),
		DefaultLimit:      int(envInt64("READ_FOLDER_TOOL_DEFAULT_LIMIT", 100)),
		MaxHashBytes:      envInt64("READ_FOLDER_TOOL_MAX_HASH_BYTES", 64<<20),
		MaxConcurrent:     int(envInt64("READ_FOLDER_TOOL_MAX_CONCURRENT", 8)),
		MaxBatchItems:     int(envInt64("READ_FOLDER_TOOL_MAX_BATCH_ITEMS", 20)),
		IgnoredNames:      envList("READ_FOLDER_TOOL_IGNORED_NAMES"),
		ShutdownGrace:     envDuration("READ_FOLDER_TOOL_SHUTDOWN_GRACE", 5*time.Second),
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

func envList(name string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(os.Getenv(name), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
