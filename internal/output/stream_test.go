package output

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestStreamTruncatesAndKeepsFullLog(t *testing.T) {
	var methods []string
	stream, err := New("req-1", "exec", t.TempDir(), 4, func(method string, params any) error { methods = append(methods, method); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Write("stdout", []byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	summary := stream.Close()
	if !summary.Truncated || summary.TotalBytes != 8 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	content, err := os.ReadFile(summary.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abcdefgh" {
		t.Fatalf("unexpected log: %q", content)
	}
	tail, err := base64.StdEncoding.DecodeString(summary.TailBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(tail) != "abcdefgh" {
		t.Fatalf("unexpected tail: %q", tail)
	}
	if len(methods) != 2 || methods[0] != "exec.output" || methods[1] != "exec.truncated" {
		t.Fatalf("unexpected methods: %v", methods)
	}
}

func TestStreamRemovesSmallLog(t *testing.T) {
	stream, err := New("req-2", "exec", t.TempDir(), 32, func(string, any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Write("stdout", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	summary := stream.Close()
	if summary.Truncated || summary.LogPath != "" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
