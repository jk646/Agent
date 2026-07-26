package textsearch

import "errors"

var (
	ErrOutsideWorkspace    = errors.New("path is outside workspace")
	ErrSymlink             = errors.New("symbolic links are not allowed")
	ErrNotFound            = errors.New("path was not found")
	ErrInvalidRequest      = errors.New("invalid text search request")
	ErrLimitExceeded       = errors.New("text search limit exceeded")
	ErrUnsupportedEncoding = errors.New("binary or unsupported text encoding")
	ErrCapacity            = errors.New("text search capacity reached")
	ErrDuplicateSearch     = errors.New("search ID is already active")
	ErrSearchNotFound      = errors.New("active search was not found")
)
