package filereader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ResolvedPath struct {
	Relative string
	Absolute string
}

type Resolver struct {
	root string
}

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
		return nil, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory")
	}
	return &Resolver{root: filepath.Clean(canonical)}, nil
}

func (r *Resolver) Root() string { return r.root }

func (r *Resolver) Resolve(value string) (ResolvedPath, error) {
	if strings.IndexByte(value, 0) >= 0 || filepath.IsAbs(value) {
		return ResolvedPath{}, ErrOutsideWorkspace
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." {
		clean = ""
	}
	target := filepath.Join(r.root, clean)
	if err := ensureWithin(r.root, target); err != nil {
		return ResolvedPath{}, err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return ResolvedPath{}, ErrNotFound
	}
	if err != nil {
		return ResolvedPath{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ResolvedPath{}, ErrSymlink
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		return ResolvedPath{}, err
	}
	if err := ensureWithin(r.root, canonical); err != nil {
		return ResolvedPath{}, err
	}
	if canonical != target {
		return ResolvedPath{}, ErrSymlink
	}
	relative, err := filepath.Rel(r.root, target)
	if err != nil {
		return ResolvedPath{}, err
	}
	if relative == "." {
		relative = ""
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
