package filetool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

func applyOperations(root string, operations []Operation, cfg Config) error {
	for _, operation := range operations {
		if err := applyOperation(root, operation, cfg); err != nil {
			return fmt.Errorf("%s: %w", operation.Kind, err)
		}
	}
	return nil
}

func applyOperation(root string, operation Operation, cfg Config) error {
	switch operation.Kind {
	case "replace":
		return applyReplace(root, operation, cfg)
	case "create":
		return applyCreate(root, operation, cfg)
	case "mkdir":
		return applyMkdir(root, operation)
	case "copy":
		return applyCopy(root, operation, cfg)
	case "move":
		return applyMove(root, operation, cfg)
	case "delete":
		return applyDelete(root, operation)
	case "chmod":
		return applyChmod(root, operation)
	default:
		return fmt.Errorf("%w: unsupported kind %q", ErrInvalidOperation, operation.Kind)
	}
}

func applyReplace(root string, operation Operation, cfg Config) error {
	path := rootedPath(root, operation.Path)
	info, err := os.Stat(path)
	if err != nil {
		return mapPathError(err)
	}
	if !info.Mode().IsRegular() || info.Size() > cfg.MaxFileBytes {
		return ErrTooLarge
	}
	if err := verifyDigest(path, operation.ExpectedSHA256, true); err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return ErrBinaryFile
	}
	updated := string(content)
	if len(operation.Replacements) == 0 {
		return fmt.Errorf("%w: replacements are required", ErrInvalidOperation)
	}
	for _, replacement := range operation.Replacements {
		if replacement.OldText == "" {
			return fmt.Errorf("%w: old_text cannot be empty", ErrInvalidOperation)
		}
		expected := replacement.ExpectedOccurrences
		if expected <= 0 {
			expected = 1
		}
		count := strings.Count(updated, replacement.OldText)
		if count == 0 {
			return ErrMatchNotFound
		}
		if count != expected {
			return fmt.Errorf("%w: expected %d, found %d", ErrMatchCount, expected, count)
		}
		updated = strings.Replace(updated, replacement.OldText, replacement.NewText, expected)
	}
	if int64(len(updated)) > cfg.MaxFileBytes {
		return ErrTooLarge
	}
	return atomicWrite(path, []byte(updated), info.Mode().Perm())
}

func applyCreate(root string, operation Operation, cfg Config) error {
	path := rootedPath(root, operation.Path)
	if int64(len(operation.Content)) > cfg.MaxFileBytes || !utf8.ValidString(operation.Content) {
		return ErrTooLarge
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !operation.Overwrite {
			return ErrAlreadyExists
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: overwrite target is not a regular file", ErrInvalidOperation)
		}
		if err := verifyDigest(path, operation.ExpectedTargetSHA256, true); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if operation.CreateParents {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return mapPathError(err)
	}
	mode, err := parseMode(operation.Mode, 0o644)
	if err != nil {
		return err
	}
	return atomicWrite(path, []byte(operation.Content), mode)
}

func applyMkdir(root string, operation Operation) error {
	path := rootedPath(root, operation.Path)
	if _, err := os.Lstat(path); err == nil {
		return ErrAlreadyExists
	} else if !os.IsNotExist(err) {
		return err
	}
	mode, err := parseMode(operation.Mode, 0o755)
	if err != nil {
		return err
	}
	if operation.CreateParents {
		return os.MkdirAll(path, mode)
	}
	return os.Mkdir(path, mode)
}

func applyCopy(root string, operation Operation, cfg Config) error {
	source := rootedPath(root, operation.From)
	destination := rootedPath(root, operation.To)
	if err := verifyDigest(source, operation.ExpectedSHA256, true); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		if !operation.Overwrite {
			return ErrAlreadyExists
		}
		if err := verifyDigest(destination, operation.ExpectedTargetSHA256, true); err != nil {
			return err
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if operation.CreateParents {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Dir(destination)); err != nil {
		return mapPathError(err)
	}
	return copyPath(source, destination, &copyBudget{maxBytes: cfg.MaxTransactionBytes, maxFiles: cfg.MaxTransactionFiles})
}

func applyMove(root string, operation Operation, cfg Config) error {
	source := rootedPath(root, operation.From)
	destination := rootedPath(root, operation.To)
	if err := verifyDigest(source, operation.ExpectedSHA256, true); err != nil {
		return err
	}
	if strings.HasPrefix(operation.To+"/", operation.From+"/") {
		return fmt.Errorf("%w: cannot move a directory into itself", ErrInvalidOperation)
	}
	if _, err := os.Lstat(destination); err == nil {
		if !operation.Overwrite {
			return ErrAlreadyExists
		}
		if err := verifyDigest(destination, operation.ExpectedTargetSHA256, true); err != nil {
			return err
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if operation.CreateParents {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Dir(destination)); err != nil {
		return mapPathError(err)
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	if err := copyPath(source, destination, &copyBudget{maxBytes: cfg.MaxTransactionBytes, maxFiles: cfg.MaxTransactionFiles}); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

func applyDelete(root string, operation Operation) error {
	path := rootedPath(root, operation.Path)
	if err := verifyDigest(path, operation.ExpectedSHA256, true); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return mapPathError(err)
	}
	if info.IsDir() && !operation.Recursive {
		return os.Remove(path)
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func applyChmod(root string, operation Operation) error {
	path := rootedPath(root, operation.Path)
	if err := verifyDigest(path, operation.ExpectedSHA256, true); err != nil {
		return err
	}
	mode, err := parseMode(operation.Mode, 0)
	if err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func verifyDigest(path, expected string, required bool) error {
	if expected == "" {
		if required {
			return fmt.Errorf("%w: expected_sha256 is required", ErrInvalidOperation)
		}
		return nil
	}
	actual, err := digestPath(path)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%w: expected %s, found %s", ErrStaleFile, expected, actual)
	}
	return nil
}

func parseMode(value string, fallback os.FileMode) (os.FileMode, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil || parsed > 0o7777 {
		return 0, fmt.Errorf("%w: invalid mode", ErrInvalidOperation)
	}
	return os.FileMode(parsed), nil
}

func rootedPath(root, relative string) string {
	return filepath.Join(root, filepath.FromSlash(relative))
}
