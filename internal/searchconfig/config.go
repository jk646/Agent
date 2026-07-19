package searchconfig

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
	MaxFileBytes      int64
	MaxResults        int
	MaxScannedEntries int
	MaxDepth          int
	MaxConcurrent     int
	ShutdownGrace     time.Duration
	IgnoredNames      []string
}

func Load() Config {
	return Config{
		Transport:         envString("FILE_SEARCH_TOOL_TRANSPORT", "unix"),
		SocketPath:        envString("FILE_SEARCH_TOOL_SOCKET", "/run/agent/file-search-tool.sock"),
		Workspace:         envString("FILE_SEARCH_TOOL_WORKSPACE", "/workspace"),
		MaxMessageBytes:   int(envInt64("FILE_SEARCH_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxFileBytes:      envInt64("FILE_SEARCH_TOOL_MAX_FILE_BYTES", 8<<20),
		MaxResults:        int(envInt64("FILE_SEARCH_TOOL_MAX_RESULTS", 1000)),
		MaxScannedEntries: int(envInt64("FILE_SEARCH_TOOL_MAX_SCANNED_ENTRIES", 200000)),
		MaxDepth:          int(envInt64("FILE_SEARCH_TOOL_MAX_DEPTH", 64)),
		MaxConcurrent:     int(envInt64("FILE_SEARCH_TOOL_MAX_CONCURRENT", 8)),
		ShutdownGrace:     envDuration("FILE_SEARCH_TOOL_SHUTDOWN_GRACE", 5*time.Second),
		IgnoredNames:      envList("FILE_SEARCH_TOOL_IGNORED_NAMES", ".git,node_modules,vendor,.idea,.vscode,dist,build"),
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

func envList(name, fallback string) []string {
	value := envString(name, fallback)
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
