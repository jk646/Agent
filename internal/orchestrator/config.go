package orchestrator

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func LoadToolsFile(path string) (ToolsConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultToolsConfig(), nil
	}
	if err != nil {
		return ToolsConfig{}, err
	}
	var cfg ToolsConfig
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return ToolsConfig{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ToolsConfig{}, fmt.Errorf("%w: trailing JSON content", ErrInvalidConfig)
	}
	if err := validateToolsConfig(cfg); err != nil {
		return ToolsConfig{}, err
	}
	return cfg, nil
}

func validateToolsConfig(cfg ToolsConfig) error {
	if cfg.ProtocolVersion != "" && cfg.ProtocolVersion != "1" {
		return fmt.Errorf("%w: unsupported protocol_version %q", ErrInvalidConfig, cfg.ProtocolVersion)
	}
	seen := make(map[string]struct{})
	for _, tool := range cfg.Tools {
		if !toolNamePattern.MatchString(tool.Name) || !filepath.IsAbs(tool.Socket) || strings.IndexByte(tool.Socket, 0) >= 0 {
			return fmt.Errorf("%w: invalid tool %q", ErrInvalidConfig, tool.Name)
		}
		if _, exists := seen[tool.Name]; exists {
			return fmt.Errorf("%w: duplicate tool %q", ErrInvalidConfig, tool.Name)
		}
		seen[tool.Name] = struct{}{}
		for _, prefix := range tool.MethodPrefixes {
			if prefix == "" || strings.HasPrefix(prefix, "system.") || strings.IndexByte(prefix, 0) >= 0 {
				return fmt.Errorf("%w: invalid prefix for %s", ErrInvalidConfig, tool.Name)
			}
		}
	}
	return nil
}

func DefaultToolsConfig() ToolsConfig {
	return ToolsConfig{ProtocolVersion: "1", Tools: []ToolSpec{
		{Name: "shell", Socket: "/run/agent/shell-tool.sock", MethodPrefixes: []string{"exec.", "session."}, Required: true, Description: "Linux command execution and PTY sessions"},
		{Name: "file-edit", Socket: "/run/agent/file-tool.sock", MethodPrefixes: []string{"file."}, Description: "Transactional file editing"},
		{Name: "file-search", Socket: "/run/agent/file-search-tool.sock", MethodPrefixes: []string{"search."}, Description: "File name and metadata search"},
		{Name: "read-file", Socket: "/run/agent/read-file-tool.sock", MethodPrefixes: []string{"read."}, Description: "Bounded file reading"},
		{Name: "read-folder", Socket: "/run/agent/read-folder-tool.sock", MethodPrefixes: []string{"read_folder."}, Description: "Folder structure reading"},
		{Name: "search-text", Socket: "/run/agent/search-text-tool.sock", MethodPrefixes: []string{"search_text."}, Description: "Text and regular expression search"},
		{Name: "write-file", Socket: "/run/agent/write-file-tool.sock", MethodPrefixes: []string{"write_file."}, Description: "Atomic complete-file writing"},
	}}
}
