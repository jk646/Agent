package filewriter

import "errors"

var (
	ErrOutsideWorkspace    = errors.New("path is outside workspace")
	ErrSymlink             = errors.New("symbolic links are not allowed")
	ErrNotFound            = errors.New("file was not found")
	ErrAlreadyExists       = errors.New("file already exists")
	ErrInvalidRequest      = errors.New("invalid write request")
	ErrTooLarge            = errors.New("write limit exceeded")
	ErrStaleFile           = errors.New("file changed since expected SHA-256")
	ErrUnsupportedType     = errors.New("target is not a regular file")
	ErrCapacity            = errors.New("write capacity reached")
	ErrDuplicateWrite      = errors.New("write ID is already active")
	ErrWriteNotFound       = errors.New("active write was not found")
	ErrRollbackConflict    = errors.New("file changed after transaction")
	ErrTransactionNotFound = errors.New("transaction was not found")
)
