package orchestrator

import (
	"encoding/json"
	"time"
)

type ToolSpec struct {
	Name           string   `json:"name"`
	Socket         string   `json:"socket"`
	MethodPrefixes []string `json:"method_prefixes,omitempty"`
	Required       bool     `json:"required,omitempty"`
	Description    string   `json:"description,omitempty"`
}

type ToolsConfig struct {
	ProtocolVersion string     `json:"protocol_version"`
	Tools           []ToolSpec `json:"tools"`
}
type ToolInfo struct {
	ToolSpec
	Connected    bool            `json:"connected"`
	LastError    string          `json:"last_error,omitempty"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
}
type CallParams struct {
	Tool      string          `json:"tool,omitempty"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	TimeoutMS int64           `json:"timeout_ms,omitempty"`
}
type CallResult struct {
	Tool       string          `json:"tool"`
	Method     string          `json:"method"`
	Result     json.RawMessage `json:"result"`
	DurationMS int64           `json:"duration_ms"`
}
type BatchParams struct {
	Calls    []CallParams `json:"calls"`
	Parallel bool         `json:"parallel,omitempty"`
	FailFast bool         `json:"fail_fast,omitempty"`
}
type BatchItem struct {
	Index  int         `json:"index"`
	Result *CallResult `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}
type BatchResult struct {
	Results    []BatchItem `json:"results"`
	DurationMS int64       `json:"duration_ms"`
}
type ToolEvent struct {
	Tool      string          `json:"tool"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}
type HealthResult struct {
	Status     string       `json:"status"`
	Tools      []ToolHealth `json:"tools"`
	DurationMS int64        `json:"duration_ms"`
}
type ToolHealth struct {
	Name     string          `json:"name"`
	Required bool            `json:"required"`
	Online   bool            `json:"online"`
	Health   json.RawMessage `json:"health,omitempty"`
	Error    string          `json:"error,omitempty"`
}
