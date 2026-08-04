package orchestratorconfig

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Transport       string
	SocketPath      string
	ToolsFile       string
	MaxMessageBytes int
	MaxConcurrent   int
	DefaultTimeout  time.Duration
	ShutdownGrace   time.Duration
}

func Load() Config {
	return Config{
		Transport:       envString("AGENT_ORCHESTRATOR_TRANSPORT", "unix"),
		SocketPath:      envString("AGENT_ORCHESTRATOR_SOCKET", "/run/agent/orchestrator.sock"),
		ToolsFile:       envString("AGENT_ORCHESTRATOR_TOOLS_FILE", "/etc/agent/orchestrator-tools.json"),
		MaxMessageBytes: int(envInt64("AGENT_ORCHESTRATOR_MAX_MESSAGE_BYTES", 16<<20)),
		MaxConcurrent:   int(envInt64("AGENT_ORCHESTRATOR_MAX_CONCURRENT", 32)),
		DefaultTimeout:  envDuration("AGENT_ORCHESTRATOR_DEFAULT_TIMEOUT", 2*time.Minute),
		ShutdownGrace:   envDuration("AGENT_ORCHESTRATOR_SHUTDOWN_GRACE", 5*time.Second),
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
