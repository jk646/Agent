package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ResolvedPath struct{ Relative, Absolute string }
type Resolver struct{ root string }

func NewResolver(root string) (*Resolver, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory")
	}
	return &Resolver{root: filepath.Clean(canonical)}, nil
}
func (r *Resolver) Root() string { return r.root }

func (r *Resolver) Resolve(value string) (ResolvedPath, error) {
	if value == "" || strings.IndexByte(value, 0) >= 0 || filepath.IsAbs(value) {
		return ResolvedPath{}, ErrOutsideWorkspace
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == "" {
		return ResolvedPath{}, ErrInvalidRequest
	}
	target := filepath.Join(r.root, clean)
	if err := ensureWithin(r.root, target); err != nil {
		return ResolvedPath{}, err
	}
	ancestor := target
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return ResolvedPath{}, ErrSymlink
			}
			canonical, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return ResolvedPath{}, err
			}
			if err := ensureWithin(r.root, canonical); err != nil {
				return ResolvedPath{}, err
			}
			if canonical != ancestor {
				return ResolvedPath{}, ErrSymlink
			}
			break
		}
		if !os.IsNotExist(err) {
			return ResolvedPath{}, err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return ResolvedPath{}, ErrOutsideWorkspace
		}
		ancestor = parent
	}
	relative, err := filepath.Rel(r.root, target)
	if err != nil {
		return ResolvedPath{}, err
	}
	return ResolvedPath{Relative: filepath.ToSlash(relative), Absolute: target}, nil
}

func ensureWithin(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrOutsideWorkspace
	}
	return nil
}
