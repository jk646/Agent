package filereader

import "errors"

var (
	ErrOutsideWorkspace    = errors.New("path is outside workspace")
	ErrSymlink             = errors.New("symbolic links are not allowed")
	ErrNotFound            = errors.New("path was not found")
	ErrNotRegular          = errors.New("path is not a regular file")
	ErrInvalidRequest      = errors.New("invalid read request")
	ErrTooLarge            = errors.New("read limit exceeded")
	ErrUnsupportedEncoding = errors.New("unsupported or binary text encoding")
	ErrCapacity            = errors.New("read capacity reached")
	ErrDuplicateRead       = errors.New("read ID is already active")
	ErrReadNotFound        = errors.New("active read was not found")
	ErrFileChanged         = errors.New("file changed while being read")
)
