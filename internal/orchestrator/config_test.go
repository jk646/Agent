package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadToolsFileRejectsUnknownAndTrailingData(t *testing.T) {
	for name, content := range map[string]string{
		"unknown":  `{"tools":[],"unexpected":true}`,
		"trailing": `{"tools":[]} {}`,
		"relative": `{"tools":[{"name":"demo","socket":"demo.sock"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tools.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadToolsFile(path); err == nil {
				t.Fatal("expected invalid configuration error")
			}
		})
	}
}

func TestMissingToolsFileUsesDefaults(t *testing.T) {
	cfg, err := LoadToolsFile(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) < 7 {
		t.Fatalf("unexpected default registry: %+v", cfg)
	}
}
