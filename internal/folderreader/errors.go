package folderreader

import "errors"

var (
	ErrOutsideWorkspace = errors.New("path is outside workspace")
	ErrSymlink          = errors.New("symbolic links are not allowed")
	ErrNotFound         = errors.New("path was not found")
	ErrNotFolder        = errors.New("path is not a folder")
	ErrInvalidRequest   = errors.New("invalid folder read request")
	ErrTooLarge         = errors.New("folder read limit exceeded")
	ErrCapacity         = errors.New("folder read capacity reached")
	ErrDuplicateRead    = errors.New("folder read ID is already active")
	ErrReadNotFound     = errors.New("active folder read was not found")
)
