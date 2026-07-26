package filewriter

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func hashBytes(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func hashFile(path string) (string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func prepareAtomic(path string, data []byte, mode os.FileMode) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".write-file-*")
	if err != nil {
		return "", err
	}
	name := temporary.Name()
	cleanup := func() { temporary.Close(); os.Remove(name) }
	if err := temporary.Chmod(mode.Perm()); err != nil {
		cleanup()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func commitAtomic(temporary, target string) error {
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
