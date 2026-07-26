package filewriter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type journalManager struct {
	root     string
	ttl      time.Duration
	maxBytes int64
}
type journalManifest struct {
	ID         string          `json:"id"`
	CreatedAt  time.Time       `json:"created_at"`
	RolledBack bool            `json:"rolled_back"`
	Records    []journalRecord `json:"records"`
}
type journalRecord struct {
	Path         string `json:"path"`
	BeforeExists bool   `json:"before_exists"`
	BeforeMode   uint32 `json:"before_mode,omitempty"`
	BeforeSize   int64  `json:"before_size,omitempty"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterSHA256  string `json:"after_sha256"`
	Snapshot     string `json:"snapshot,omitempty"`
}

func newJournalManager(tempDir string, ttl time.Duration, maxBytes int64) *journalManager {
	return &journalManager{root: filepath.Join(tempDir, "transactions"), ttl: ttl, maxBytes: maxBytes}
}

func (j *journalManager) create(root string, prepared []preparedWrite) (*journalManifest, string, error) {
	if err := os.MkdirAll(j.root, 0o700); err != nil {
		return nil, "", err
	}
	id, err := randomID("writetx-")
	if err != nil {
		return nil, "", err
	}
	directory := filepath.Join(j.root, id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return nil, "", err
	}
	manifest := &journalManifest{ID: id, CreatedAt: time.Now().UTC()}
	var copied int64
	for index, item := range prepared {
		record := journalRecord{Path: item.path.Relative, BeforeExists: item.exists, BeforeMode: uint32(item.mode.Perm()), BeforeSize: int64(len(item.before)), BeforeSHA256: item.beforeSHA, AfterSHA256: item.afterSHA}
		if item.exists {
			copied += int64(len(item.before))
			if copied > j.maxBytes {
				os.RemoveAll(directory)
				return nil, "", ErrTooLarge
			}
			record.Snapshot = filepath.ToSlash(filepath.Join("before", fmt.Sprintf("%04d", index)))
			target := filepath.Join(directory, filepath.FromSlash(record.Snapshot))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				os.RemoveAll(directory)
				return nil, "", err
			}
			if err := os.WriteFile(target, item.before, 0o600); err != nil {
				os.RemoveAll(directory)
				return nil, "", err
			}
		}
		manifest.Records = append(manifest.Records, record)
	}
	if err := j.write(directory, manifest); err != nil {
		os.RemoveAll(directory)
		return nil, "", err
	}
	return manifest, directory, nil
}

func (j *journalManager) load(id string) (*journalManifest, string, error) {
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return nil, "", ErrTransactionNotFound
	}
	directory := filepath.Join(j.root, id)
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if os.IsNotExist(err) {
		return nil, "", ErrTransactionNotFound
	}
	if err != nil {
		return nil, "", err
	}
	var manifest journalManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, "", err
	}
	return &manifest, directory, nil
}

func (j *journalManager) write(directory string, manifest *journalManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := prepareAtomic(filepath.Join(directory, "manifest.json"), data, 0o600)
	if err != nil {
		return err
	}
	return commitAtomic(temporary, filepath.Join(directory, "manifest.json"))
}

func copySnapshot(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), ".write-rollback-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return commitAtomic(name, target)
}

func (j *journalManager) reap() {
	entries, err := os.ReadDir(j.root)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-j.ttl)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(deadline) {
			_ = os.RemoveAll(filepath.Join(j.root, entry.Name()))
		}
	}
}
