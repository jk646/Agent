package filetool

import "errors"

var (
	ErrOutsideWorkspace = errors.New("path is outside workspace")
	ErrStaleFile        = errors.New("file hash does not match")
	ErrMatchNotFound    = errors.New("replacement text was not found")
	ErrMatchCount       = errors.New("replacement occurrence count does not match")
	ErrTooLarge         = errors.New("file operation exceeds configured limit")
	ErrBinaryFile       = errors.New("binary file cannot be edited as text")
	ErrAlreadyExists    = errors.New("target already exists")
	ErrSymlink          = errors.New("symbolic links are not allowed")
	ErrRollbackConflict = errors.New("rollback conflicts with current workspace state")
	ErrNotFound         = errors.New("path was not found")
	ErrInvalidOperation = errors.New("invalid file operation")
	ErrCapacity         = errors.New("file transaction capacity reached")
)
