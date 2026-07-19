package filetool

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	maxFiles int
}

type journalManifest struct {
	ID         string          `json:"id"`
	CreatedAt  time.Time       `json:"created_at"`
	RolledBack bool            `json:"rolled_back"`
	Records    []journalRecord `json:"records"`
}

type journalRecord struct {
	Path         string `json:"path"`
	Snapshot     string `json:"snapshot,omitempty"`
	BeforeExists bool   `json:"before_exists"`
	BeforeType   string `json:"before_type,omitempty"`
	BeforeMode   uint32 `json:"before_mode,omitempty"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterExists  bool   `json:"after_exists"`
	AfterType    string `json:"after_type,omitempty"`
	AfterSHA256  string `json:"after_sha256,omitempty"`
}

type copyBudget struct {
	bytes    int64
	files    int
	maxBytes int64
	maxFiles int
}

func newJournalManager(tempDir string, ttl time.Duration, maxBytes int64, maxFiles int) *journalManager {
	return &journalManager{root: filepath.Join(tempDir, "transactions"), ttl: ttl, maxBytes: maxBytes, maxFiles: maxFiles}
}

func (j *journalManager) create(workspace string, paths []string) (*journalManifest, string, error) {
	if err := os.MkdirAll(j.root, 0o700); err != nil {
		return nil, "", err
	}
	id := randomTransactionID()
	directory := filepath.Join(j.root, id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return nil, "", err
	}
	manifest := &journalManifest{ID: id, CreatedAt: time.Now().UTC()}
	budget := &copyBudget{maxBytes: j.maxBytes, maxFiles: j.maxFiles}
	for index, relative := range reducePaths(paths) {
		source := filepath.Join(workspace, filepath.FromSlash(relative))
		record := journalRecord{Path: relative, Snapshot: filepath.ToSlash(filepath.Join("before", fmt.Sprintf("%04d", index)))}
		state, err := inspectPath(source)
		if err != nil {
			os.RemoveAll(directory)
			return nil, "", err
		}
		record.BeforeExists = state.Exists
		record.BeforeType = state.Type
		record.BeforeMode = uint32(state.Mode)
		record.BeforeSHA256 = state.SHA256
		if state.Exists {
			destination := filepath.Join(directory, filepath.FromSlash(record.Snapshot))
			if err := copyPath(source, destination, budget); err != nil {
				os.RemoveAll(directory)
				return nil, "", err
			}
		}
		manifest.Records = append(manifest.Records, record)
	}
	if err := j.writeManifest(directory, manifest); err != nil {
		os.RemoveAll(directory)
		return nil, "", err
	}
	return manifest, directory, nil
}

func (j *journalManager) finalize(directory, root string, manifest *journalManifest) error {
	for index := range manifest.Records {
		state, err := inspectPath(filepath.Join(root, filepath.FromSlash(manifest.Records[index].Path)))
		if err != nil {
			return err
		}
		manifest.Records[index].AfterExists = state.Exists
		manifest.Records[index].AfterType = state.Type
		manifest.Records[index].AfterSHA256 = state.SHA256
	}
	return j.writeManifest(directory, manifest)
}

func (j *journalManager) load(id string) (*journalManifest, string, error) {
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return nil, "", ErrNotFound
	}
	directory := filepath.Join(j.root, id)
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if os.IsNotExist(err) {
		return nil, "", ErrNotFound
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

func (j *journalManager) restore(root, directory string, manifest *journalManifest) error {
	for _, record := range manifest.Records {
		target := filepath.Join(root, filepath.FromSlash(record.Path))
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if !record.BeforeExists {
			continue
		}
		source := filepath.Join(directory, filepath.FromSlash(record.Snapshot))
		budget := &copyBudget{maxBytes: j.maxBytes, maxFiles: j.maxFiles}
		if err := copyPath(source, target, budget); err != nil {
			return err
		}
	}
	return nil
}

func (j *journalManager) remove(directory string) { _ = os.RemoveAll(directory) }

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

func (j *journalManager) writeManifest(directory string, manifest *journalManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(directory, "manifest.json"), append(data, '\n'), 0o600)
}

type pathState struct {
	Exists bool
	Type   string
	Mode   os.FileMode
	SHA256 string
}

func inspectPath(path string) (pathState, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return pathState{}, nil
	}
	if err != nil {
		return pathState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return pathState{}, ErrSymlink
	}
	digest, err := digestPath(path)
	if err != nil {
		return pathState{}, err
	}
	return pathState{Exists: true, Type: fileType(info), Mode: info.Mode().Perm(), SHA256: digest}, nil
}

func digestPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrSymlink
	}
	if info.Mode().IsRegular() {
		return hashFile(path)
	}
	if !info.IsDir() {
		return fmt.Sprintf("other:%o:%d", info.Mode(), info.Size()), nil
	}
	hash := sha256.New()
	err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrSymlink
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative)+"\x00")
		if entry.Type().IsRegular() {
			fileDigest, err := hashFile(current)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, fileDigest)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func copyPath(source, destination string, budget *copyBudget) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrSymlink
	}
	budget.files++
	if budget.files > budget.maxFiles {
		return ErrTooLarge
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()), budget); err != nil {
				return err
			}
		}
		return os.Chmod(destination, info.Mode().Perm())
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: unsupported file type", ErrInvalidOperation)
	}
	budget.bytes += info.Size()
	if budget.bytes > budget.maxBytes {
		return ErrTooLarge
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func reducePaths(paths []string) []string {
	paths = uniqueSorted(paths)
	result := make([]string, 0, len(paths))
	for _, candidate := range paths {
		covered := false
		for _, parent := range result {
			if candidate == parent || strings.HasPrefix(candidate, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, candidate)
		}
	}
	return result
}

func randomTransactionID() string {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("filetx-%d", time.Now().UnixNano())
	}
	return "filetx-" + base64.RawURLEncoding.EncodeToString(buffer)
}
