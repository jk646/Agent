package output

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Emitter func(method string, params any) error

type Stream struct {
	mu         sync.Mutex
	requestID  string
	streamName string
	limit      int64
	total      int64
	inline     int64
	sequence   uint64
	emitter    Emitter
	file       *os.File
	path       string
	truncated  bool
	tail       []byte
}

type Summary struct {
	TotalBytes int64  `json:"total_bytes"`
	Truncated  bool   `json:"truncated"`
	LogPath    string `json:"log_path,omitempty"`
	TailBase64 string `json:"tail_base64,omitempty"`
}

type ChunkEvent struct {
	RequestID  string `json:"request_id"`
	Sequence   uint64 `json:"sequence"`
	Stream     string `json:"stream"`
	DataBase64 string `json:"data_base64"`
	Timestamp  string `json:"timestamp"`
}
type TruncatedEvent struct {
	RequestID  string `json:"request_id"`
	Sequence   uint64 `json:"sequence"`
	LogPath    string `json:"log_path"`
	TotalBytes int64  `json:"total_bytes"`
	Timestamp  string `json:"timestamp"`
}

func New(requestID, streamName, tempDir string, limit int64, emitter Emitter) (*Stream, error) {
	if limit <= 0 {
		return nil, errors.New("output limit must be positive")
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(tempDir, "exec-*.log")
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, err
	}
	return &Stream{requestID: requestID, streamName: streamName, limit: limit, emitter: emitter, file: file, path: filepath.Clean(file.Name())}, nil
}

func (s *Stream) Write(stream string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.file.Write(data); err != nil {
		return err
	}
	s.total += int64(len(data))
	s.appendTail(data)
	remaining := s.limit - s.inline
	if remaining > 0 {
		count := int64(len(data))
		if count > remaining {
			count = remaining
		}
		if count > 0 {
			if err := s.emitChunk(stream, data[:count]); err != nil {
				return err
			}
			s.inline += count
		}
	}
	if s.inline >= s.limit && s.total > s.limit && !s.truncated {
		s.truncated = true
		s.sequence++
		if err := s.emitter("exec.truncated", TruncatedEvent{RequestID: s.requestID, Sequence: s.sequence, LogPath: s.path, TotalBytes: s.total, Timestamp: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Stream) emitChunk(stream string, data []byte) error {
	s.sequence++
	return s.emitter("exec.output", ChunkEvent{RequestID: s.requestID, Sequence: s.sequence, Stream: stream, DataBase64: base64.StdEncoding.EncodeToString(data), Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Stream) Close() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.file.Close()
	summary := Summary{TotalBytes: s.total, Truncated: s.truncated}
	if s.truncated {
		summary.LogPath = s.path
		summary.TailBase64 = base64.StdEncoding.EncodeToString(s.tail)
	} else {
		_ = os.Remove(s.path)
	}
	return summary
}

func (s *Stream) appendTail(data []byte) {
	const tailLimit = 4096
	if len(data) >= tailLimit {
		s.tail = append(s.tail[:0], data[len(data)-tailLimit:]...)
		return
	}
	if len(s.tail)+len(data) > tailLimit {
		drop := len(s.tail) + len(data) - tailLimit
		copy(s.tail, s.tail[drop:])
		s.tail = s.tail[:len(s.tail)-drop]
	}
	s.tail = append(s.tail, data...)
}
