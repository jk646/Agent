package textsearchconfig

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Transport       string
	SocketPath      string
	Workspace       string
	MaxMessageBytes int
	MaxDepth        int
	MaxScannedFiles int
	MaxFileBytes    int64
	MaxResults      int
	DefaultLimit    int
	MaxMatchesFile  int
	MaxContextLines int
	MaxConcurrent   int
	MaxBatchItems   int
	IgnoredNames    []string
	ShutdownGrace   time.Duration
}

func Load() Config {
	return Config{
		Transport:       envString("SEARCH_TEXT_TOOL_TRANSPORT", "unix"),
		SocketPath:      envString("SEARCH_TEXT_TOOL_SOCKET", "/run/agent/search-text-tool.sock"),
		Workspace:       envString("SEARCH_TEXT_TOOL_WORKSPACE", "/workspace"),
		MaxMessageBytes: int(envInt64("SEARCH_TEXT_TOOL_MAX_MESSAGE_BYTES", 4<<20)),
		MaxDepth:        int(envInt64("SEARCH_TEXT_TOOL_MAX_DEPTH", 32)),
		MaxScannedFiles: int(envInt64("SEARCH_TEXT_TOOL_MAX_SCANNED_FILES", 100000)),
		MaxFileBytes:    envInt64("SEARCH_TEXT_TOOL_MAX_FILE_BYTES", 8<<20),
		MaxResults:      int(envInt64("SEARCH_TEXT_TOOL_MAX_RESULTS", 1000)),
		DefaultLimit:    int(envInt64("SEARCH_TEXT_TOOL_DEFAULT_LIMIT", 100)),
		MaxMatchesFile:  int(envInt64("SEARCH_TEXT_TOOL_MAX_MATCHES_PER_FILE", 100)),
		MaxContextLines: int(envInt64("SEARCH_TEXT_TOOL_MAX_CONTEXT_LINES", 20)),
		MaxConcurrent:   int(envInt64("SEARCH_TEXT_TOOL_MAX_CONCURRENT", 8)),
		MaxBatchItems:   int(envInt64("SEARCH_TEXT_TOOL_MAX_BATCH_ITEMS", 20)),
		IgnoredNames:    envList("SEARCH_TEXT_TOOL_IGNORED_NAMES", []string{".git", "node_modules", "vendor", ".idea", ".vscode", "dist", "build"}),
		ShutdownGrace:   envDuration("SEARCH_TEXT_TOOL_SHUTDOWN_GRACE", 5*time.Second),
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

func envList(name string, fallback []string) []string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
