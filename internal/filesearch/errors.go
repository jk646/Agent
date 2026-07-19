package filesearch

import "errors"

var (
	ErrOutsideWorkspace = errors.New("path is outside workspace")
	ErrSymlink          = errors.New("symbolic links are not allowed")
	ErrNotFound         = errors.New("path was not found")
	ErrInvalidRequest   = errors.New("invalid search request")
	ErrTooLarge         = errors.New("search limit exceeded")
	ErrCapacity         = errors.New("search capacity reached")
	ErrDuplicateSearch  = errors.New("search ID is already active")
	ErrSearchNotFound   = errors.New("active search was not found")
)
