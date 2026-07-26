package filewriter

import (
	"context"
	"os"
	"path/filepath"
)

func (m *Manager) Rollback(ctx context.Context, params RollbackParams) (RollbackResult, error) {
	manifest, directory, err := m.journal.load(params.TransactionID)
	if err != nil {
		return RollbackResult{}, err
	}
	if manifest.RolledBack {
		return RollbackResult{}, ErrTransactionNotFound
	}
	paths := make([]string, 0, len(manifest.Records))
	for _, record := range manifest.Records {
		paths = append(paths, record.Path)
	}
	unlock := m.locks.lock(paths)
	defer unlock()
	for _, record := range manifest.Records {
		if err := ctx.Err(); err != nil {
			return RollbackResult{}, err
		}
		resolved, err := m.resolver.Resolve(record.Path)
		if err != nil {
			return RollbackResult{}, err
		}
		info, err := os.Lstat(resolved.Absolute)
		if err != nil {
			return RollbackResult{}, ErrRollbackConflict
		}
		if !info.Mode().IsRegular() {
			return RollbackResult{}, ErrRollbackConflict
		}
		digest, err := hashFile(resolved.Absolute)
		if err != nil || digest != record.AfterSHA256 {
			return RollbackResult{}, ErrRollbackConflict
		}
	}
	if err := m.restoreUnchecked(directory, manifest, len(manifest.Records)); err != nil {
		return RollbackResult{}, err
	}
	manifest.RolledBack = true
	if err := m.journal.write(directory, manifest); err != nil {
		return RollbackResult{}, err
	}
	files := make([]FileChange, 0, len(manifest.Records))
	for _, record := range manifest.Records {
		action := "deleted"
		if record.BeforeExists {
			action = "restored"
		}
		files = append(files, FileChange{Path: record.Path, Action: action, Size: record.BeforeSize, BeforeSHA256: record.AfterSHA256, AfterSHA256: record.BeforeSHA256})
	}
	return RollbackResult{TransactionID: manifest.ID, RolledBack: true, Files: files}, nil
}

func (m *Manager) restoreUnchecked(directory string, manifest *journalManifest, count int) error {
	if count > len(manifest.Records) {
		count = len(manifest.Records)
	}
	for index := count - 1; index >= 0; index-- {
		record := manifest.Records[index]
		resolved, err := m.resolver.Resolve(record.Path)
		if err != nil {
			return err
		}
		if !record.BeforeExists {
			if err := os.Remove(resolved.Absolute); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		snapshot := filepath.Join(directory, filepath.FromSlash(record.Snapshot))
		if err := copySnapshot(snapshot, resolved.Absolute, os.FileMode(record.BeforeMode)); err != nil {
			return err
		}
	}
	return nil
}
