package filetool

import (
	"fmt"
	"os"
	"path/filepath"
)

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".file-tool-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode.Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace target: %w", err)
	}
	if handle, err := os.Open(directory); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}
